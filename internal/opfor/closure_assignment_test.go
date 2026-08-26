package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClosureAssignmentMistakeCanonicalCompatibility(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "clmistake.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "clmistake.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("clmistake.sl", programBytes))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != string(want) {
		t.Fatalf("output mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}
