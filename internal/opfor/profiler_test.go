package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProfilerCanonicalCompatibility(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "profiler.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "profiler.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("profiler.sl", programBytes))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var output bytes.Buffer
	fixed := time.Unix(1_700_000_000, 0)
	runtime, err := New(
		WithStdout(&output),
		WithStderr(&output),
		WithClock(ClockFunc(func() time.Time { return fixed })),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}
