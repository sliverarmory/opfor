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
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for warning control-flow verification")
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

	directory := t.TempDir()
	path := filepath.Join(directory, "uncaught-warning.sl")
	if err := os.WriteFile(path, []byte(includeZeroArgumentTryProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(java, "-jar", jar, path)
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
