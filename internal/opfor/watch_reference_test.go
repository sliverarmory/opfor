package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWatchCanonicalGolden(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "watch.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "watch.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("watch.sl", programBytes))
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
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func TestScriptPositionalArgumentsPreserveScalarReferences(t *testing.T) {
	program, err := CompileString("references.sl", `$x = 3;
sub by_position { $1 = 7; }
by_position($x);
println($x);
sub by_argv { @_[0] = 9; }
by_argv($x);
println($x);
inline by_inline { $1 = 11; }
by_inline($x);
println($x);
by_position($x + 0);
println($x);
`)
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
	if got, want := output.String(), "7\n9\n11\n11\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
