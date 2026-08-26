package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSleepNamedParametersGolden(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "nparams.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "nparams.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("nparams.sl", programBytes))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}
