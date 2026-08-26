package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTraceBranchesCanonicalCompatibility(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "tracebr.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "tracebr.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("tracebr.sl", programBytes))
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

func TestAssignWhilePredicateTraceCanonicalCompatibility(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "debug64.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "debug64.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("debug64.sl", programBytes))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := runtime.Execute(ctx, program); err != nil {
		t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}
