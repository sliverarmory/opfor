package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
)

const sleepParameterTermProbe = `sub mark { println("E" . $1); return $1; }
sub args { println(join("|", @_)); }
args(mark("a"), mark("b"));
args(mark("a") mark("b"), mark("c"));
println(1());
$value = 7;
println($value());
$assigned = 9();
println($assigned);
sub f { return 7; }
sub returned { return 11(); }
println(returned());
args(1(2));
args(f()(8));
args(1 + 2 3);
args(,,,);
`

const sleepParameterTermOutput = `Eb
Ea
a|b
Ec
Ea
Eb
b|a|c
1
7
9
11
2|1
8|7
4

`

const sleepRawPairKeyProbe = `sub explode { println("EVAL"); return "bad"; }
$h = %(-foo    "x" => "v1", -foo explode() => "v2", -foo @(1) => "v3", -foo (1 + 2) => "v4", !true => "v5", ~1 => "v6", not(15) => "v7", not true => "v8");
println($h['-foo "x"']);
println($h['-foo explode()']);
println($h['-foo @(1)']);
println($h['-foo (1 + 2)']);
println($h['!true']);
println($h['~1']);
println($h['not(15)']);
println($h['not true']);
`

const sleepRawPairKeyOutput = `v1
v2
v3
v4
v5
v6
v7
v8
`

const sleepArbitraryParameterOperatorProbe = `sub args { println(join("|", @_)); }
args("a" "b" "c");
println("after");
`

func TestSleepParameterTermOrderAndNonFunctionParentheses(t *testing.T) {
	if got := runSleepParameterCompatibilityProbe(t, "parameter-terms.sl", sleepParameterTermProbe); got != sleepParameterTermOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepParameterTermOutput, got)
	}
}

func TestSleepPredicateShapedPairKeysAreRawData(t *testing.T) {
	if got := runSleepParameterCompatibilityProbe(t, "raw-pair-keys.sl", sleepRawPairKeyProbe); got != sleepRawPairKeyOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepRawPairKeyOutput, got)
	}
}

func TestSleepPredicateShapedNamedPairKeyReachesImporterRaw(t *testing.T) {
	var seen []Argument
	var output bytes.Buffer
	runtimeInstance, err := New(
		WithStdout(&output),
		WithStderr(&output),
		WithFunction("capture_raw_key", func(_ context.Context, invocation Invocation) (Value, error) {
			seen = append(seen, invocation.Arguments...)
			return Null(), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	source := `sub explode { println("EVAL"); return "bad"; } capture_raw_key(-foo explode() => "v");`
	if _, err := runtimeInstance.Eval(context.Background(), "raw-named-pair.sl", source); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "" {
		t.Fatalf("output = %q, want no evaluation side effect", got)
	}
	if len(seen) != 1 || seen[0].Name != "-foo explode()" || seen[0].Value.String() != "v" {
		t.Fatalf("importer arguments = %#v, want one raw named pair", seen)
	}
}

func TestSleepArbitraryParameterOperator(t *testing.T) {
	got := runSleepParameterCompatibilityProbe(t, "parameter-operator.sl", sleepArbitraryParameterOperatorProbe)
	want := "Warning: Attempting to use non-existent operator: '\"b\"' at parameter-operator.sl:2\n\n" +
		"after\n"
	if got != want {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestSleepParameterTermsAndRawPairKeysOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for parameter-term verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	for _, probe := range []struct {
		name   string
		source string
	}{
		{name: "parameter-terms.sl", source: sleepParameterTermProbe},
		{name: "raw-pair-keys.sl", source: sleepRawPairKeyProbe},
		{name: "parameter-operator.sl", source: sleepArbitraryParameterOperatorProbe},
	} {
		t.Run(probe.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, probe.name)
			if err := os.WriteFile(path, []byte(probe.source), 0o600); err != nil {
				t.Fatal(err)
			}
			command := osexec.Command(java, "-jar", jar, path)
			command.Dir = directory
			want, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("official Sleep parameter probe: %v\n%s", err, want)
			}
			got := []byte(runSleepParameterCompatibilityProbe(t, probe.name, probe.source))
			if !bytes.Equal(got, want) {
				t.Fatalf("official Sleep output mismatch\nwant:\n%sgot:\n%s", want, got)
			}
		})
	}
}

func runSleepParameterCompatibilityProbe(t *testing.T, name, source string) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), name, source); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
