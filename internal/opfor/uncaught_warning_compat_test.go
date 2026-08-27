package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const includeZeroArgumentTryProbe = `try {
    include();
    println("after");
}
catch $error {
    println("caught=" . $error);
}
println("tail");
`

const includeZeroArgumentTryProbeOutput = "Warning: internal error - class java.util.EmptyStackException at uncaught-warning.sl:2\n" +
	"tail\n"

func TestUncaughtScriptWarningBypassesTryCatch(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "uncaught-warning.sl", includeZeroArgumentTryProbe); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != includeZeroArgumentTryProbeOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", includeZeroArgumentTryProbeOutput, got)
	}
}

func TestUncaughtScriptWarningOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, "uncaught-warning.sl")
	if err := os.WriteFile(path, []byte(includeZeroArgumentTryProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep warning probe: %v\n%s", err, want)
	}

	var got bytes.Buffer
	runtimeInstance, err := New(WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), filepath.Base(path), includeZeroArgumentTryProbe); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("official Sleep warning output mismatch\nwant:\n%sgot:\n%s", want, got.Bytes())
	}
}
