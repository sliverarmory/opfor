package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

const sleepCallCCNativeFunctionProbeName = "callcc-native-function.sl"

// Cobalt-Strike/sleep@60ac3ff9dacc3e7b5a6c58be201c5830afbda398 pins
// this boundary in Return.java lines 66-77 and SleepUtils.java lines 388-393:
// callcc accepts only a SleepClosure, warns with "requires a function" for a
// native Function object, flags yield, and therefore abandons only the active
// closure invocation before its caller resumes.
const sleepCallCCNativeFunctionProbe = `sub probe {
    println("callee-before");
    callcc function("&println");
    println("callee-after");
}
probe();
println("caller-after");
`

const sleepCallCCNativeFunctionOutput = `callee-before
Warning: callcc requires a function: &println at callcc-native-function.sl:3
caller-after
`

var sleepCallCCNativeJVMFunctionPattern = regexp.MustCompile(`sleep\.bridges\.BasicIO\$println@[[:xdigit:]]+`)

func TestSleepCallCCRejectsNativeFunctionCompatibility(t *testing.T) {
	if got := runSleepCallCCNativeFunctionProbe(t); got != sleepCallCCNativeFunctionOutput {
		t.Fatalf("callcc native-function output mismatch\nwant:\n%sgot:\n%s", sleepCallCCNativeFunctionOutput, got)
	}
}

func TestSleepCallCCRejectsNativeFunctionOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepCallCCNativeFunctionProbeName)
	if err := os.WriteFile(path, []byte(sleepCallCCNativeFunctionProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep callcc native-function probe: %v\n%s", err, want)
	}
	if matches := sleepCallCCNativeJVMFunctionPattern.FindAll(want, -1); len(matches) != 1 {
		t.Fatalf("official Sleep native-function identities = %d, want 1\n%s", len(matches), want)
	}
	want = sleepCallCCNativeJVMFunctionPattern.ReplaceAll(want, []byte("<native-function>"))

	got := []byte(runSleepCallCCNativeFunctionProbe(t))
	if count := bytes.Count(got, []byte("&println")); count != 1 {
		t.Fatalf("OPFOR native-function descriptions = %d, want 1\n%s", count, got)
	}
	got = bytes.ReplaceAll(got, []byte("&println"), []byte("<native-function>"))
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep callcc native-function output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepCallCCNativeFunctionProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepCallCCNativeFunctionProbeName, sleepCallCCNativeFunctionProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
