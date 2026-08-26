package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestSleepMalformedExpressionDiagnosticsMatchGoldenOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		code    string
		message string
		line    int
	}{
		{
			name:    "argerr",
			code:    "PAR006",
			message: "key/value pair specified for '$a', did you forget a comma?",
			line:    6,
		},
		{
			name:    "concaterrs",
			code:    "PAR002",
			message: "Unknown expression",
			line:    12,
		},
		{
			name:    "keyvalueerr",
			code:    "PAR006",
			message: "key/value pair specified for 'c', did you forget a comma?",
			line:    3,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := filepath.Join("..", "..", "testdata", "upstream", "sleep-2.1")
			source, err := os.ReadFile(filepath.Join(root, "programs", test.name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			golden, err := os.ReadFile(filepath.Join(root, "golden", test.name+".sl"))
			if err != nil {
				t.Fatal(err)
			}

			result := parser.Parse(lexer.NewSource(test.name+".sl", source))
			if got, want := len(result.Diagnostics), 1; got != want {
				t.Fatalf("diagnostics = %v, want %d", result.Diagnostics, want)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Severity != lexer.SeverityError || diagnostic.Code != test.code ||
				diagnostic.Message != test.message || diagnostic.Span.Start.Line != test.line {
				t.Fatalf("diagnostic = %+v", diagnostic)
			}
			if got := renderSleepMalformedExpression(source, diagnostic); got != string(golden) {
				t.Fatalf("rendered diagnostic mismatch\nwant:\n%s\ngot:\n%s", golden, got)
			}
		})
	}
}

// The upstream TokenList formatter separates grouped terms in a diagnostic
// snippet with one space. These fixtures contain no significant repeated
// whitespace inside quoted terms, so folding the selected source span
// reproduces that formatter exactly.
func renderSleepMalformedExpression(source []byte, diagnostic lexer.Diagnostic) string {
	start := diagnostic.Span.Start.Offset
	end := diagnostic.Span.End.Offset
	if start < 0 || start > len(source) || end < start || end > len(source) {
		return ""
	}
	snippet := strings.Join(strings.Fields(string(source[start:end])), " ")
	// TokenParser's Unknown-expression path formats the whole TokenList and
	// therefore retains its trailing separator. CodeGenerator's missing-comma
	// path reports the already-grouped right-hand-side token without it.
	if snippet != "" && diagnostic.Message == "Unknown expression" {
		snippet += " "
	}
	return fmt.Sprintf("Error: %s at line %d\n       %s\n", diagnostic.Message, diagnostic.Span.Start.Line, snippet)
}

func TestSleepMalformedDottedNumberRuleIsNarrow(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`println("x".1.2);`,
		`println(1..2);`,
		`println(1.2 . 3);`,
	} {
		result := parser.Parse(lexer.NewSource("valid-concat.sl", []byte(source)))
		if result.HasErrors() {
			t.Errorf("%q diagnostics = %v, want none", source, result.Diagnostics)
		}
	}
}

func TestSleepLeadingDotNumberIsAnUnknownExpression(t *testing.T) {
	t.Parallel()

	result := parser.Parse(lexer.NewSource("leading-dot.sl", []byte(`println(.25);`)))
	if got, want := len(result.Diagnostics), 1; got != want {
		t.Fatalf("diagnostics = %v, want %d", result.Diagnostics, want)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "PAR002" || diagnostic.Message != "Unknown expression" ||
		diagnostic.Span.Start.Offset != 8 || diagnostic.Span.End.Offset != 11 {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
}
