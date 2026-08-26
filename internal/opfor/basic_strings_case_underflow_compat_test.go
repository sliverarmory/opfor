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

const sleepCaseConversionUnderflowProbeName = "basic-strings-case-underflow.sl"

const sleepCaseConversionUnderflowProbe = `sub uc_underflow {
    uc();
    println("uc-tail");
}
uc_underflow();
println("uc-resume");
sub lc_underflow {
    lc();
    println("lc-tail");
}
lc_underflow();
println("lc-resume");
`

const sleepCaseConversionUnderflowOutput = `Warning: internal error - class java.util.EmptyStackException at basic-strings-case-underflow.sl:2
uc-resume
Warning: internal error - class java.util.EmptyStackException at basic-strings-case-underflow.sl:8
lc-resume
`

func TestSleepBasicStringsCaseConversionUnderflow(t *testing.T) {
	got := runSleepCaseConversionUnderflowProbe(t)
	if !bytes.Equal(got, []byte(sleepCaseConversionUnderflowOutput)) {
		t.Fatalf("case-conversion underflow output mismatch\nwant:\n%s\ngot:\n%s", sleepCaseConversionUnderflowOutput, got)
	}
}

func TestSleepBasicStringsCaseConversionUnderflowOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for case-conversion underflow verification")
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

	path := filepath.Join(t.TempDir(), sleepCaseConversionUnderflowProbeName)
	if err := os.WriteFile(path, []byte(sleepCaseConversionUnderflowProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep case-conversion underflow probe: %v\n%s", err, want)
	}
	got := runSleepCaseConversionUnderflowProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("case-conversion underflow mismatch\nofficial:\n%s\nopfor:\n%s", want, got)
	}
}

func runSleepCaseConversionUnderflowProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(
		context.Background(),
		sleepCaseConversionUnderflowProbeName,
		sleepCaseConversionUnderflowProbe,
	); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}
