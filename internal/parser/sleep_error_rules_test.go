package parser

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/lexer"
)

func TestSleepInvalidObjectAccessRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		message string
	}{
		{
			name:    "comma cannot introduce arguments",
			source:  `["value" substring, 1];`,
			message: "Object Access: parameter separator is :",
		},
		{
			name:    "colon requires arguments",
			source:  `[$value substring:];`,
			message: "Object Access: can not specify empty arg list after :",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := Parse(lexer.NewSource("object.sl", []byte(test.source)))
			if got, want := len(result.Diagnostics), 1; got != want {
				t.Fatalf("diagnostics = %v, want %d", result.Diagnostics, want)
			}
			if diagnostic := result.Diagnostics[0]; diagnostic.Code != diagnosticInvalidObject || diagnostic.Message != test.message {
				t.Fatalf("diagnostic = %+v", diagnostic)
			}
		})
	}

	valid := Parse(lexer.NewSource("object-valid.sl", []byte(`[$value substring: 1, 2];`)))
	if valid.HasErrors() {
		t.Fatalf("valid colon arguments diagnostics = %v", valid.Diagnostics)
	}
}

func TestSleepParsedLiteralAlignmentDiagnostics(t *testing.T) {
	t.Parallel()

	result := Parse(lexer.NewSource("alignment.sl", []byte(`println("$[]name $[4]");`)))
	if got, want := len(result.Diagnostics), 2; got != want {
		t.Fatalf("diagnostics = %v, want %d", result.Diagnostics, want)
	}
	wantMessages := []string{
		"Empty alignment specification for $name",
		"can not align an empty variable",
	}
	for index, message := range wantMessages {
		if diagnostic := result.Diagnostics[index]; diagnostic.Code != diagnosticInvalidParsedLiteral || diagnostic.Message != message {
			t.Errorf("diagnostic %d = %+v, want %q", index, diagnostic, message)
		}
	}

	for _, source := range []string{
		`println("$[4]name");`,
		`println("literal \$[]name");`,
	} {
		valid := Parse(lexer.NewSource("alignment-valid.sl", []byte(source)))
		if valid.HasErrors() {
			t.Errorf("%q diagnostics = %v, want none", source, valid.Diagnostics)
		}
	}
}

func TestSleepSyntaxErrorConsumesOneMalformedStatement(t *testing.T) {
	t.Parallel()

	result := Parse(lexer.NewSource("invalid.sl", []byte(`<widget name="x">
<child />`)))
	if got, want := len(result.Diagnostics), 1; got != want {
		t.Fatalf("diagnostics = %v, want %d", result.Diagnostics, want)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != diagnosticUnexpectedToken || diagnostic.Message != "Syntax error" ||
		diagnostic.Span.Start.Line != 1 || diagnostic.Span.End.Line != 2 {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
}
