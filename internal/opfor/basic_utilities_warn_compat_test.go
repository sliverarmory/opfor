package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepWarnStackProbeName = "basic-utilities-warn-stack.sl"

// CodeGenerator injects each lexical warn call's source line beneath its user
// arguments. BasicUtilities then consumes only the first value as the message
// and the second as the warning line, leaving any remaining values unused.
const sleepWarnStackProbe = `warn();
println("after-zero");
warn("one");
println("after-one");
warn("first", 91);
println("after-explicit-line");
warn("kept", "bogus", "ignored");
println("after-extra");
warn($null, 17);
println("after-null");
warn(@("x", "y"), -4);
println("after-array");
$warn_handle = function("&warn");
setf("&warn", $warn_handle);
warn();
println("after-reset-zero");
warn("reset-one");
println("after-reset-one");
setf("&zwarn", $warn_handle);
zwarn();
println("after-alias-zero");
sub warn { println("script-warn|" . $1 . "|" . $2); }
warn();
warn("script");
`

const sleepWarnStackOutput = `Warning: 1 at basic-utilities-warn-stack.sl:-1
after-zero
Warning: one at basic-utilities-warn-stack.sl:3
after-one
Warning: first at basic-utilities-warn-stack.sl:91
after-explicit-line
Warning: kept at basic-utilities-warn-stack.sl:0
after-extra
Warning:  at basic-utilities-warn-stack.sl:17
after-null
Warning: @('x', 'y') at basic-utilities-warn-stack.sl:-4
after-array
Warning: 15 at basic-utilities-warn-stack.sl:-1
after-reset-zero
Warning: reset-one at basic-utilities-warn-stack.sl:17
after-reset-one
after-alias-zero
script-warn|23|
script-warn|script|24
`

func TestSleepBasicUtilitiesWarnStackContract(t *testing.T) {
	got := runSleepWarnStackProbe(t)
	if !bytes.Equal(got, []byte(sleepWarnStackOutput)) {
		t.Fatalf("warn stack output mismatch\nwant:\n%s\ngot:\n%s", sleepWarnStackOutput, got)
	}
}

func TestSleepBasicUtilitiesWarnStackOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	path := filepath.Join(t.TempDir(), sleepWarnStackProbeName)
	if err := os.WriteFile(path, []byte(sleepWarnStackProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep warn stack probe: %v\n%s", err, want)
	}
	got := runSleepWarnStackProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("warn stack mismatch\nofficial:\n%s\nopfor:\n%s", want, got)
	}
}

func TestSleepWarnInjectsLineBeforeImporterResolution(t *testing.T) {
	var calls [][]Value
	runtimeInstance, err := New(WithFunction("warn", func(_ context.Context, invocation Invocation) (Value, error) {
		calls = append(calls, invocation.Values())
		return Null(), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	if _, err := runtimeInstance.Eval(context.Background(), "warn-importer.sl", "warn();\nwarn(\"message\", 77, \"extra\");\n"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("importer warn calls = %d, want 2", len(calls))
	}
	if len(calls[0]) != 1 || calls[0][0].Int32() != 1 {
		t.Fatalf("zero-argument importer frame = %#v, want injected line 1", calls[0])
	}
	if len(calls[1]) != 4 || calls[1][0].String() != "message" || calls[1][1].Int32() != 77 ||
		calls[1][2].String() != "extra" || calls[1][3].Int32() != 2 {
		t.Fatalf("multi-argument importer frame = %#v, want message/77/extra/injected line 2", calls[1])
	}
}

func runSleepWarnStackProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepWarnStackProbeName, sleepWarnStackProbe); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}
