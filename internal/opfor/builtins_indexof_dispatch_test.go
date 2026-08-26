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

const sleepArrayIndexOfProbe = `@values = @(1, 2, 3);
println("found=" . indexOf(@values, 2));
println("missing=" . indexOf(@values, 9));
`

func TestSleepIndexOfArrayUsesStringCoercion(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	array := ArrayValue(NewArray(Int(1), Int(2), Int(3)))
	found, err := runtimeInstance.Invoke(context.Background(), "indexOf", array, Int(2))
	if err != nil || found.Kind() != KindInt || found.Int32() != 5 {
		t.Fatalf("indexOf(@(1, 2, 3), 2) = (%s, %v), want Sleep string offset 5", found.Describe(), err)
	}
	missing, err := runtimeInstance.Invoke(context.Background(), "indexOf", array, Int(9))
	if err != nil || !missing.IsNull() {
		t.Fatalf("indexOf(@(1, 2, 3), 9) = (%s, %v), want null", missing.Describe(), err)
	}
}

func TestSleepIndexOfArrayOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for indexOf array-coercion verification")
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

	path := filepath.Join(t.TempDir(), "array-indexof.sl")
	if err := os.WriteFile(path, []byte(sleepArrayIndexOfProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep indexOf array probe: %v\n%s", err, want)
	}

	var got bytes.Buffer
	runtimeInstance, err := New(WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString(path, sleepArrayIndexOfProbe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("indexOf array coercion mismatch\nofficial:\n%s\nopfor:\n%s", want, got.Bytes())
	}
}
