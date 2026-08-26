package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestSleepMissingReturnTerminatorDiagnostics(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"noterm", "noterm2"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := filepath.Join("..", "..", "testdata", "upstream", "sleep-2.1")
			source, err := os.ReadFile(filepath.Join(root, "programs", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			golden, err := os.ReadFile(filepath.Join(root, "golden", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}

			result := parser.Parse(lexer.NewSource(name+".sl", source))
			if got, want := len(result.Diagnostics), 1; got != want {
				t.Fatalf("diagnostics = %v, want %d", result.Diagnostics, want)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Severity != lexer.SeverityError || diagnostic.Code != "PAR003" {
				t.Fatalf("diagnostic = %+v, want PAR003 error", diagnostic)
			}
			if got := renderSleepMissingTerminator(source, diagnostic); got != string(golden) {
				t.Fatalf("rendered diagnostic mismatch\nwant:\n%s\ngot:\n%s", golden, got)
			}
		})
	}
}

func TestSleepExplicitTerminatorDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		source     string
		want       string
	}{
		{
			name:       "assert at end of input",
			sourceName: "assert-no-eot.sl",
			source:     `assert $ready : "not ready"`,
			want:       `assert-no-eot.sl:1:8-28: error PAR003: Missing terminator`,
		},
		{
			name:       "assert before closing brace",
			sourceName: "assert-no-eot.sl",
			source: `sub check {
    assert $ready
}`,
			want: `assert-no-eot.sl:2:12-18: error PAR003: Missing terminator`,
		},
		{
			name:       "import at end of input",
			sourceName: "import-no-eot.sl",
			source:     `import java.util.*`,
			want:       `import-no-eot.sl:1:8-19: error PAR003: Missing terminator`,
		},
		{
			name:       "import from at end of input",
			sourceName: "import-no-eot.sl",
			source:     `import pkg.Widget from: libs/widget.jar`,
			want:       `import-no-eot.sl:1:25-40: error PAR003: Missing terminator`,
		},
		{
			name:       "import before closing brace",
			sourceName: "import-no-eot.sl",
			source: `sub load {
    import java.util.*
}`,
			want: `import-no-eot.sl:2:12-23: error PAR003: Missing terminator`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := parser.Parse(lexer.NewSource(test.sourceName, []byte(test.source)))
			if got, want := len(result.Diagnostics), 1; got != want {
				t.Fatalf("diagnostics = %v, want %d", result.Diagnostics, want)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Severity != lexer.SeverityError || diagnostic.Code != "PAR003" || diagnostic.Message != "Missing terminator" {
				t.Fatalf("diagnostic = %+v, want exact PAR003 missing-terminator error", diagnostic)
			}
			if got := diagnostic.Error(); got != test.want {
				t.Fatalf("diagnostic = %q, want %q", got, test.want)
			}
		})
	}
}

// renderSleepMissingTerminator mirrors the small piece of the upstream
// YourCodeSucksException formatter exercised by these two pinned fixtures. The
// parser span selects the return expression; TokenParser appends one space per
// expression token while constructing its diagnostic snippet.
func renderSleepMissingTerminator(source []byte, diagnostic lexer.Diagnostic) string {
	start := diagnostic.Span.Start.Offset
	end := diagnostic.Span.End.Offset
	if start < 0 || start > len(source) || end < start || end > len(source) {
		return ""
	}
	snippet := string(source[start:end])
	if snippet != "" {
		snippet += " "
	}
	return fmt.Sprintf("Error: %s at line %d\n       %s\n", diagnostic.Message, diagnostic.Span.Start.Line, snippet)
}
