package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSleepDebugPropertyCompatibility(t *testing.T) {
	programRoot := filepath.Join("testdata", "upstream", "sleep-2.1", "programs")
	source, err := os.ReadFile(filepath.Join(programRoot, "debugce.sl"))
	if err != nil {
		t.Fatal(err)
	}
	wantBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "debugce.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("debugce.sl", source))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output), WithDebugFlags(3))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	absoluteRoot, err := filepath.Abs(programRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(absoluteRoot)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}

	got := strings.ReplaceAll(output.String(), filepath.Join(absoluteRoot, "fjasjkfajskfjasfjksakjfsjkfjksafjk.txt"), "<cwd>/fjasjkfajskfjasfjksakjfsjkfjksafjk.txt")
	want := strings.ReplaceAll(string(wantBytes), "/root/sleep/tests/fjasjkfajskfjasfjksakjfsjkfjksafjk.txt", "<cwd>/fjasjkfajskfjasfjksakjfsjkfjksafjk.txt")
	fileError := regexp.MustCompile(`(java\.io\.FileNotFoundException: <cwd>/fjasjkfajskfjasfjksakjfsjkfjksafjk\.txt) \([^\r\n]*\)`)
	got = fileError.ReplaceAllString(got, `$1 (No such file or directory)`)
	if got != want {
		t.Fatalf("normalized output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}
