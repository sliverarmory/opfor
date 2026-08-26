package opfor

import (
	"bytes"
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
)

type sleepAssertControlProbe struct {
	name    string
	source  string
	include string
	want    string
}

var sleepAssertControlProbes = []sleepAssertControlProbe{
	{
		name: "assert-true-lazy.sl",
		source: `sub side { println("message-side=" . $1); return $1; }
println("true-before");
assert true : side("not-evaluated");
println("true-after");
`,
		want: "true-before\ntrue-after\n",
	},
	{
		name: "assert-top-default.sl",
		source: `println("top-before");
assert false;
println("top-after");
`,
		want: "top-before\nWarning: assertion failed at assert-top-default.sl:2\n",
	},
	{
		name: "assert-sub-message.sl",
		source: `sub side { println("message-side=" . $1); return $1; }
sub fail { println("sub-before"); assert false : side("sub-message"); println("sub-after"); }
println("caller-before");
fail();
println("caller-after");
`,
		want: `caller-before
sub-before
message-side=sub-message
Warning: sub-message at assert-sub-message.sl:2
`,
	},
	{
		name: "assert-try-message.sl",
		source: `sub side { println("message-side=" . $1); return $1; }
println("try-before");
try {
    println("try-body-before");
    assert false : side("try-message");
    println("try-body-after");
}
catch $error { println("caught=" . $error); }
println("try-after");
`,
		want: `try-before
try-body-before
message-side=try-message
Warning: try-message at assert-try-message.sl:5
`,
	},
	{
		name: "assert-dynamic.sl",
		source: `sub side { println("message-side=" . $1); return $1; }
println("eval-call=[" . eval('println("eval-before"); assert false : side("eval-message"); println("eval-after");') . "]");
println("include-before");
include("assert-child.sl");
println("include-after");
`,
		include: `println("include-body-before");
assert false : side("include-message");
println("include-body-after");
`,
		want: `eval-before
message-side=eval-message
Warning: eval-message at eval:0
eval-call=[]
include-before
include-body-before
message-side=include-message
Warning: include-message at assert-child.sl:2
include-after
`,
	},
}

func TestSleepAssertControlCompatibility(t *testing.T) {
	for _, probe := range sleepAssertControlProbes {
		t.Run(probe.name, func(t *testing.T) {
			got, result := runSleepAssertControlProbe(t, probe)
			if got != probe.want {
				t.Fatalf("assert output mismatch\nwant:\n%sgot:\n%s", probe.want, got)
			}
			if !result.IsNull() {
				t.Fatalf("assert result = %s, want $null", result.Describe())
			}
		})
	}
}

func TestSleepAssertControlOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	for _, probe := range sleepAssertControlProbes {
		t.Run(probe.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, probe.name)
			if err := os.WriteFile(path, []byte(probe.source), 0o600); err != nil {
				t.Fatal(err)
			}
			if probe.include != "" {
				if err := os.WriteFile(filepath.Join(directory, "assert-child.sl"), []byte(probe.include), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			command := osexec.Command(java, "-jar", jar, path)
			command.Dir = directory
			want, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("official Sleep assert probe: %v\n%s", err, want)
			}
			if string(want) != probe.want {
				t.Fatalf("recorded official output mismatch\nrecorded:\n%sofficial:\n%s", probe.want, want)
			}
			got, _ := runSleepAssertControlProbe(t, probe)
			if !bytes.Equal([]byte(got), want) {
				t.Fatalf("official Sleep assert output mismatch\nwant:\n%sgot:\n%s", want, got)
			}
		})
	}
}

func runSleepAssertControlProbe(t *testing.T, probe sleepAssertControlProbe) (string, Value) {
	t.Helper()
	var output bytes.Buffer
	options := []Option{WithStdout(&output), WithStderr(&output)}
	if probe.include != "" {
		options = append(options, WithSourceResolver(SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
			if request.Name != "assert-child.sl" {
				return Source{}, os.ErrNotExist
			}
			return NewSource("assert-child.sl", []byte(probe.include)), nil
		})))
	}
	runtimeInstance, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Eval(context.Background(), probe.name, probe.source)
	if err != nil {
		t.Fatalf("Eval returned a fatal error: %v", err)
	}
	return output.String(), result
}
