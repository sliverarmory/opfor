package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTryBreakCanonicalCompatibility(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "trybreak.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "trybreak.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("trybreak.sl", programBytes))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}
