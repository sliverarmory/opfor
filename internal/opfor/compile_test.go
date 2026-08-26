package opfor

import (
	"errors"
	"testing"
)

func TestCompileCopiesSource(t *testing.T) {
	t.Parallel()

	data := []byte(`println("hello");`)
	program, err := Compile(NewSource("hello.cna", data))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	data[0] = '#'
	if got := string(program.Source().Data); got != `println("hello");` {
		t.Fatalf("program source mutated: %q", got)
	}
}

func TestCompileReturnsDiagnostics(t *testing.T) {
	t.Parallel()

	_, err := CompileString("bad.cna", `println("unterminated);`)
	var compileError *CompileError
	if !errors.As(err, &compileError) || len(compileError.Diagnostics) == 0 {
		t.Fatalf("Compile error = %v, want diagnostics", err)
	}
}

func TestCompileRetainsCompatibilityWarnings(t *testing.T) {
	t.Parallel()

	program, err := CompileString("legacy.cna", "println(1) println(2)", WithCompatibilityWarnings())
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	diagnostics := program.Diagnostics()
	if len(diagnostics) == 0 || diagnostics[0].Severity != SeverityWarning {
		t.Fatalf("Diagnostics = %+v, want a compatibility warning", diagnostics)
	}
	diagnostics[0].Message = "mutated"
	if program.Diagnostics()[0].Message == "mutated" {
		t.Fatal("Diagnostics returned shared mutable storage")
	}
}

func TestCompileRequiresSleepExplicitTerminators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "assert",
			source: `assert $ready`,
			want:   `assert-no-eot.sl:1:8-14: error PAR003: Missing terminator`,
		},
		{
			name:   "import",
			source: `import java.util.*`,
			want:   `import-no-eot.sl:1:8-19: error PAR003: Missing terminator`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := CompileString(test.name+"-no-eot.sl", test.source)
			var compileError *CompileError
			if !errors.As(err, &compileError) {
				t.Fatalf("CompileString error = %v, want *CompileError", err)
			}
			if got, want := len(compileError.Diagnostics), 1; got != want {
				t.Fatalf("diagnostics = %v, want %d", compileError.Diagnostics, want)
			}
			if got := compileError.Error(); got != test.want {
				t.Fatalf("CompileError = %q, want %q", got, test.want)
			}
		})
	}
}
