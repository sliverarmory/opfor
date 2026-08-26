package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTaintModeBridgePoliciesAndCycles(t *testing.T) {
	source := func(_ context.Context, _ Invocation) (Value, error) { return String("outside"), nil }
	sanitize := func(_ context.Context, invocation Invocation) (Value, error) { return invocation.Arg(0), nil }
	sensitiveCalled := false
	sensitive := func(_ context.Context, _ Invocation) (Value, error) {
		sensitiveCalled = true
		return String("unsafe"), nil
	}
	runtime, err := New(
		WithTaintMode(true),
		WithTaintFunction("source", source, TaintSource),
		WithTaintFunction("sanitize", sanitize, TaintSanitizer),
		WithTaintFunction("sensitive", sensitive, TaintSensitive),
	)
	if err != nil {
		t.Fatal(err)
	}

	tainted, err := runtime.Invoke(context.Background(), "source")
	if err != nil || !tainted.IsTainted() || tainted.String() != "outside" {
		t.Fatalf("source = (%s, tainted %v, %v)", tainted.Describe(), tainted.IsTainted(), err)
	}
	clean, err := runtime.Invoke(context.Background(), "sanitize", tainted)
	if err != nil || clean.IsTainted() || !clean.IdentityEqual(tainted) {
		t.Fatalf("sanitize = (%s, tainted %v, %v)", clean.Describe(), clean.IsTainted(), err)
	}
	if _, err := runtime.Invoke(context.Background(), "sensitive", tainted); err == nil || !strings.Contains(err.Error(), "Insecure &sensitive: 'outside' is tainted") {
		t.Fatalf("sensitive error = %v", err)
	}
	if sensitiveCalled {
		t.Fatal("sensitive bridge was called with tainted input")
	}

	cycle := NewArray()
	cycle.Append(ArrayValue(cycle), String("leaf"))
	runtime.TaintAll(ArrayValue(cycle))
	if !ArrayValue(cycle).IsTainted() {
		t.Fatal("cycle containing a tainted leaf is not tainted")
	}
	leaf, _ := cycle.Get(1)
	if !leaf.IsTainted() {
		t.Fatal("TaintAll did not mark nested scalar")
	}
}

func TestTaintModeDisabledIsTransparent(t *testing.T) {
	runtime, err := New(WithTaintMode(false))
	if err != nil {
		t.Fatal(err)
	}
	value := String("outside")
	if got := runtime.Taint(value); got.IsTainted() || !got.IdentityEqual(value) {
		t.Fatalf("Taint while disabled = %s, tainted %v", got.Describe(), got.IsTainted())
	}

	program, err := CompileString("off.sl", `return taint("still clean");`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := runtime.Execute(context.Background(), program)
	if err != nil || got.IsTainted() || got.String() != "still clean" {
		t.Fatalf("disabled script taint = (%s, tainted %v, %v)", got.Describe(), got.IsTainted(), err)
	}
}

func TestSerializedReadUsesSourcePolicy(t *testing.T) {
	const code = `
$buffer = allocate();
writeObject($buffer, %(value => "secret"));
closef($buffer);
$decoded = readObject($buffer);
if (-istainted $decoded) { return 1; }
return 0;
`
	program, err := CompileString("serialized-taint.sl", code)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		enabled bool
		want    int32
	}{
		{name: "enabled", enabled: true, want: 1},
		{name: "disabled", enabled: false, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime, err := New(WithTaintMode(test.enabled))
			if err != nil {
				t.Fatal(err)
			}
			got, err := runtime.Execute(context.Background(), program)
			if err != nil {
				t.Fatal(err)
			}
			if got.Int32() != test.want {
				t.Fatalf("result = %s, want %d", got.Describe(), test.want)
			}
		})
	}
}

func TestSleepTaintGoldenConformance(t *testing.T) {
	for _, name := range []string{"taint1", "taint2", "taint3", "taint4", "taint5", "taint6", "taint7", "taint8", "taint9", "taint10", "taint11"} {
		name := name
		t.Run(name, func(t *testing.T) {
			programRoot := filepath.Join("testdata", "upstream", "sleep-2.1", "programs")
			source, err := os.ReadFile(filepath.Join(programRoot, name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(name+".sl", source))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			var output bytes.Buffer
			runtime, err := New(WithTaintMode(true), WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatal(err)
			}
			if name == "taint4" {
				absoluteRoot, err := filepath.Abs(programRoot)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := runtime.Invoke(context.Background(), "chdir", String(absoluteRoot)); err != nil {
					t.Fatalf("set runtime cwd: %v", err)
				}
			}
			arguments := []Value(nil)
			if name != "taint4" && name != "taint10" && name != "taint11" {
				arguments = []Value{String("2 + 2")}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, err = runtime.Execute(ctx, program, arguments...)
			cancel()
			if err != nil {
				t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
			}
			got := output.Bytes()
			if name == "taint7" {
				// Object.toString's identity hash is process-specific in both the
				// JVM and OPFOR. All other bytes remain exact.
				identity := regexp.MustCompile(`java[.]io[.]PrintStream@[0-9a-f]+`)
				got = identity.ReplaceAll(got, []byte("java.io.PrintStream@<identity>"))
				want = identity.ReplaceAll(want, []byte("java.io.PrintStream@<identity>"))
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}
