package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepCoreFlowDynamicProbeName = "sleep-core-flow-dynamic.sl"

const sleepCoreFlowDynamicProbe = `inline done_inline {
    $stale = "inline-done-stale";
    done;
}
sub done_outer {
    println("done-outer-before");
    done_inline();
    println("done-outer-after");
}
inline halt_inline {
    $stale = "inline-halt-stale";
    halt;
}
sub halt_outer {
    println("halt-outer-before");
    halt_inline();
    println("halt-outer-after");
}
println("done=[" . done_outer() . "]");
println("halt=[" . halt_outer() . "]");
try {
    println("explicit-null-throw-before");
    throw $null;
    println("explicit-null-throw-after");
}
catch $error {
    println("explicit-null-throw-caught");
}
try {
    println("bare-throw-before");
    throw;
    println("bare-throw-after");
}
catch $error {
    println("bare-throw-caught");
}
sub expr_zero {
    println("expr-before");
    expr();
    println("expr-after");
}
expr_zero();
println("expr-caller");
println("callcc-before");
callcc;
println("callcc-after");
`

const sleepCoreFlowDynamicProbeOutput = `done-outer-before
done=[1]
halt-outer-before
halt=[2]
explicit-null-throw-before
explicit-null-throw-after
bare-throw-before
bare-throw-after
expr-before
Warning: internal error - class java.util.EmptyStackException at sleep-core-flow-dynamic.sl:39
expr-caller
callcc-before
Warning: callcc requires a function: $null at sleep-core-flow-dynamic.sl:45
`

func TestSleepCoreFlowDynamicCompatibility(t *testing.T) {
	got := runSleepCoreFlowDynamicProbe(t)
	if !bytes.Equal(got, []byte(sleepCoreFlowDynamicProbeOutput)) {
		t.Fatalf("core flow/dynamic output mismatch\nwant:\n%sgot:\n%s", sleepCoreFlowDynamicProbeOutput, got)
	}
}

func TestSleepCoreFlowDynamicOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, sleepCoreFlowDynamicProbeName)
	if err := os.WriteFile(path, []byte(sleepCoreFlowDynamicProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep core flow/dynamic probe: %v\n%s", err, want)
	}

	got := runSleepCoreFlowDynamicProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep core flow/dynamic output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepCoreFlowDynamicProbe(t *testing.T) []byte {
	t.Helper()

	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepCoreFlowDynamicProbeName, sleepCoreFlowDynamicProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
