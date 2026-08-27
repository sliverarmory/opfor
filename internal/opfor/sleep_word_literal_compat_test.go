package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepWordLiteralProbeName = "sleep-word-literal-probe.sl"

const sleepWordLiteralProbe = `sub true {
    return "lower-true-call";
}
sub false {
    return "lower-false-call";
}
sub TRUE {
    return "upper-true-call";
}
sub False {
    return "mixed-false-call";
}
sub null {
    return "lower-null-call";
}
sub NULL {
    return "upper-null-call";
}
println(true());
println(false());
println(TRUE());
println(False());
println(null());
println(NULL());
println("bare=" . true . "|" . false);
`

const sleepWordLiteralProbeOutput = `lower-true-call
lower-false-call
upper-true-call
mixed-false-call
lower-null-call
upper-null-call
bare=1|
`

func TestSleepWordLiteralCallCompatibility(t *testing.T) {
	if got := runSleepWordLiteralProbe(t); got != sleepWordLiteralProbeOutput {
		t.Fatalf("word-literal output mismatch\nwant:\n%sgot:\n%s", sleepWordLiteralProbeOutput, got)
	}
}

func TestSleepWordLiteralCallOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepWordLiteralProbeName)
	if err := os.WriteFile(path, []byte(sleepWordLiteralProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep word-literal probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepWordLiteralProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep word-literal output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepWordLiteralProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepWordLiteralProbeName, sleepWordLiteralProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
