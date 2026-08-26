package opfor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSemaphoreCanonicalCompatibility(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "sync.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "sync.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("sync.sl", programBytes))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := runtime.Execute(ctx, program); err != nil {
		t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func TestSemaphoreDefaultCountAndString(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Invoke(context.Background(), "semaphore")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), "[Semaphore: 1]"; got != want {
		t.Fatalf("semaphore string = %q, want %q", got, want)
	}
	if _, err := runtime.Invoke(context.Background(), "acquire", value); err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), "[Semaphore: 0]"; got != want {
		t.Fatalf("acquired semaphore string = %q, want %q", got, want)
	}
	if _, err := runtime.Invoke(context.Background(), "release", value); err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), "[Semaphore: 1]"; got != want {
		t.Fatalf("released semaphore string = %q, want %q", got, want)
	}
}

func TestSemaphoreAcquireHonorsCancellation(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Invoke(context.Background(), "semaphore", Int(0))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := runtime.Invoke(ctx, "acquire", value); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire error = %v, want context deadline", err)
	}
}

func TestSemaphoreRejectsNonSemaphore(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Invoke(context.Background(), "acquire", String("wrong")); err == nil {
		t.Fatal("acquire accepted a non-semaphore value")
	}
	if _, err := runtime.Invoke(context.Background(), "release", Null()); err == nil {
		t.Fatal("release accepted a non-semaphore value")
	}
}
