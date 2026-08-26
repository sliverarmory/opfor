package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sliverarmory/opfor/internal/compiler"
	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestSleepParserDiagnosticsMatchGoldenOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want []sleepDiagnosticExpectation
	}{
		{
			name: "bareword",
			want: []sleepDiagnosticExpectation{{code: "CMP002", message: "Unknown expression", line: 5}},
		},
		{
			name: "errors1",
			want: []sleepDiagnosticExpectation{
				{code: "LEX006", message: "Mismatched Parentheses - missing close paren", line: 9},
				{code: "LEX007", message: "Mismatched Braces - missing close brace", line: 6},
				{code: "LEX002", message: "Runaway string", line: 9},
			},
		},
		{
			name: "errors2",
			want: []sleepDiagnosticExpectation{{code: "LEX007", message: "Mismatched Braces - missing open brace", line: 10}},
		},
		{
			name: "errors3",
			want: []sleepDiagnosticExpectation{{code: "PAR005", message: "Syntax error", line: 5}},
		},
		{
			name: "errors4",
			want: []sleepDiagnosticExpectation{
				{code: "PAR012", message: "Empty alignment specification for $test", line: 5},
				{code: "PAR012", message: "can not align an empty variable", line: 5},
				{code: "PAR012", message: "can not align an empty variable", line: 5},
			},
		},
		{
			name: "errors5",
			want: []sleepDiagnosticExpectation{{code: "LEX008", message: "Mismatched Indices - missing open index", line: 8}},
		},
		{
			name: "hoeserror",
			want: []sleepDiagnosticExpectation{{code: "PAR008", message: "Object Access: parameter separator is :", line: 5}},
		},
		{
			name: "sillysyntax",
			want: []sleepDiagnosticExpectation{{code: "PAR008", message: "Object Access: can not specify empty arg list after :", line: 5}},
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

			diagnostics := sleepCompileDiagnostics(test.name+".sl", source)
			if got, want := len(diagnostics), len(test.want); got != want {
				t.Fatalf("diagnostics = %v, want %d", diagnostics, want)
			}
			for index, want := range test.want {
				diagnostic := diagnostics[index]
				if diagnostic.Severity != lexer.SeverityError || diagnostic.Code != want.code ||
					diagnostic.Message != want.message || diagnostic.Span.Start.Line != want.line {
					t.Errorf("diagnostic %d = %+v, want %+v", index, diagnostic, want)
				}
			}

			if got := renderSleepDiagnostics(source, diagnostics); got != string(golden) {
				t.Fatalf("rendered diagnostic mismatch\nwant:\n%s\ngot:\n%s", golden, got)
			}
		})
	}
}

type sleepDiagnosticExpectation struct {
	code    string
	message string
	line    int
}

func sleepCompileDiagnostics(name string, source []byte) []lexer.Diagnostic {
	parsed := parser.Parse(lexer.NewSource(name, source))
	if parsed.HasErrors() {
		return parsed.Diagnostics
	}
	compiled := compiler.Compile(parsed.Script)
	return compiled.Diagnostics
}

// renderSleepDiagnostics mirrors the reference SyntaxError display using the
// generalized source span and diagnostic kind. It intentionally has no fixture
// names: snippets are reconstructed using the same line, parsed-literal, and
// TokenList boundaries that produced each diagnostic.
func renderSleepDiagnostics(source []byte, diagnostics []lexer.Diagnostic) string {
	var output strings.Builder
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(&output, "Error: %s at line %d\n", diagnostic.Message, diagnostic.Span.Start.Line)

		switch {
		case diagnostic.Message == "Syntax error":
			snippet := sourceForSpan(source, diagnostic.Span)
			fmt.Fprintf(&output, "       %s \n", formatSleepTokenList(snippet))

		case diagnostic.Message == "Object Access: parameter separator is :":
			snippet := sourceForSpan(source, diagnostic.Span)
			fmt.Fprintf(&output, "       %s \n", formatSleepTokenList(snippet))

		case diagnostic.Message == "Object Access: can not specify empty arg list after :":
			snippet := sourceForSpan(source, diagnostic.Span)
			fmt.Fprintf(&output, "       %s\n", formatEmptyObjectArguments(snippet))

		case diagnostic.Code == "PAR012":
			snippet, marker := parsedLiteralSnippet(source, diagnostic)
			fmt.Fprintf(&output, "       %s\n", snippet)
			fmt.Fprintf(&output, "       %s^\n", strings.Repeat(" ", marker-1))

		case diagnostic.Message == "Unknown expression":
			fmt.Fprintf(&output, "       %s \n", sourceForSpan(source, diagnostic.Span))

		default:
			line := sourceLine(source, diagnostic.Span.Start.Line)
			fmt.Fprintf(&output, "       %s\n", line)
			if delimiterDiagnosticHasMarker(diagnostic) {
				fmt.Fprintf(&output, "       %s^\n", strings.Repeat(" ", diagnostic.Span.Start.Column-1))
			}
		}
	}
	return output.String()
}

func delimiterDiagnosticHasMarker(diagnostic lexer.Diagnostic) bool {
	// The reference scanner constructs top-level unmatched brace closes
	// without a marker. Other delimiter witnesses retain their character.
	return diagnostic.Message != "Mismatched Braces - missing open brace"
}

func sourceForSpan(source []byte, span lexer.Span) string {
	if span.Start.Offset < 0 || span.End.Offset < span.Start.Offset || span.End.Offset > len(source) {
		return ""
	}
	return string(source[span.Start.Offset:span.End.Offset])
}

func sourceLine(source []byte, line int) string {
	lines := strings.Split(string(source), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimSuffix(lines[line-1], "\r")
}

func parsedLiteralSnippet(source []byte, diagnostic lexer.Diagnostic) (string, int) {
	lexed := lexer.Lex(lexer.NewSource("<diagnostic>", source))
	for _, token := range lexed.Tokens {
		if token.Kind != lexer.DoubleString || diagnostic.Span.Start.Offset < token.TextSpan.Start.Offset || diagnostic.Span.Start.Offset >= token.TextSpan.End.Offset {
			continue
		}
		line := diagnostic.Span.Start.Line - token.TextSpan.Start.Line
		parts := strings.Split(token.Text, "\n")
		if line < 0 || line >= len(parts) {
			return token.Text, 1
		}
		marker := diagnostic.Span.Start.Column
		if line == 0 {
			marker -= token.TextSpan.Start.Column - 1
		}
		return strings.TrimSuffix(parts[line], "\r"), marker
	}
	return "", 1
}

func formatSleepTokenList(source string) string {
	lexed := lexer.Lex(lexer.NewSource("<snippet>", []byte(source)))
	var output strings.Builder
	var previous lexer.Token
	havePrevious := false
	closingTag := false
	for _, token := range lexed.Tokens {
		if token.Kind == lexer.EOF || token.Kind == lexer.Comment {
			continue
		}

		attach := !havePrevious
		if havePrevious {
			switch {
			case closingTag:
				attach = true
			case previous.Lexeme == "<" && token.Lexeme == "/":
				attach = true
				closingTag = true
			case previous.Lexeme == "<":
				attach = true
			case token.Lexeme == "=":
				attach = true
			case previous.Lexeme == "/" && token.Lexeme == ">":
				attach = true
			}
		}
		if !attach {
			output.WriteByte(' ')
		}
		output.WriteString(token.Lexeme)
		if closingTag && token.Lexeme == ">" {
			closingTag = false
		}
		previous = token
		havePrevious = true
	}
	return output.String()
}

func formatEmptyObjectArguments(source string) string {
	colon := strings.LastIndexByte(source, ':')
	if colon < 0 {
		return source
	}
	before := strings.TrimRight(source[:colon], " \t")
	after := source[colon+1:]
	return before + " :<null>" + after
}
