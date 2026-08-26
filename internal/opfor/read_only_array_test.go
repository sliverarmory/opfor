package opfor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadOnlyArrayRejectsStructuralAndIndexedMutation(t *testing.T) {
	array := NewReadOnlyArray(String("one"), String("two"))
	if err := array.appendValues(String("three")); !errors.Is(err, ErrReadOnlyArray) {
		t.Fatalf("append error = %v, want ErrReadOnlyArray", err)
	}
	if err := array.Set(0, String("changed")); !errors.Is(err, ErrReadOnlyArray) {
		t.Fatalf("set error = %v, want ErrReadOnlyArray", err)
	}
	if got := array.Values(); len(got) != 2 || got[0].String() != "one" || got[1].String() != "two" {
		t.Fatalf("values after rejected mutations = %v", got)
	}
}

func TestReadOnlyFilesystemArrayCanonicalCompatibility(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "wo.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "wo.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("wo.sl", programBytes))
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
