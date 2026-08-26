package lexer

import "testing"

func TestSleepStructuralDelimiterDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		code    string
		message string
		line    int
		column  int
		count   int
	}{
		{
			name:    "missing close parenthesis",
			source:  "println(1;",
			code:    diagnosticMismatchedParenthesis,
			message: "Mismatched Parentheses - missing close paren",
			line:    1,
			column:  8,
			count:   1,
		},
		{
			name:    "reference cross-line index witness",
			source:  "@orphan]\n@valid[1]",
			code:    diagnosticMismatchedIndex,
			message: "Mismatched Indices - missing open index",
			line:    2,
			column:  9,
			count:   1,
		},
		{
			name:   "delimiters in comments and strings are ignored",
			source: "# } ] )\nprintln(\"{[(\");",
			count:  0,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := Lex(NewSource("delimiter.sl", []byte(test.source)))
			if got := len(result.Diagnostics); got != test.count {
				t.Fatalf("diagnostics = %v, want %d", result.Diagnostics, test.count)
			}
			if test.count == 0 {
				if result.HasStructuralErrors {
					t.Fatal("HasStructuralErrors = true, want false")
				}
				return
			}
			diagnostic := result.Diagnostics[0]
			if !result.HasStructuralErrors || diagnostic.Code != test.code || diagnostic.Message != test.message ||
				diagnostic.Span.Start.Line != test.line || diagnostic.Span.Start.Column != test.column {
				t.Fatalf("diagnostic = %+v, structural=%v", diagnostic, result.HasStructuralErrors)
			}
		})
	}
}
