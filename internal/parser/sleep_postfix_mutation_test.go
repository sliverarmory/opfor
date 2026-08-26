package parser

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/ast"
	"github.com/sliverarmory/opfor/internal/lexer"
)

func TestSleepPostfixMutationIsAnAdjacentScalarHack(t *testing.T) {
	t.Parallel()

	valid := []struct {
		source string
		op     string
		name   string
	}{
		{source: `$value++;`, op: "++", name: "value"},
		{source: `$value--;`, op: "--", name: "value"},
		{source: `$null++;`, op: "++", name: "null"},
	}
	for _, test := range valid {
		result := Parse(lexer.NewSource("postfix-valid.sl", []byte(test.source)))
		if result.HasErrors() {
			t.Errorf("Parse(%q) diagnostics = %v, want none", test.source, result.Diagnostics)
			continue
		}
		if got, want := len(result.Script.Statements), 1; got != want {
			t.Errorf("Parse(%q) statements = %d, want %d", test.source, got, want)
			continue
		}
		statement, ok := result.Script.Statements[0].(*ast.ExprStmt)
		if !ok {
			t.Errorf("Parse(%q) statement = %T, want *ast.ExprStmt", test.source, result.Script.Statements[0])
			continue
		}
		mutation, ok := statement.Expr.(*ast.UnaryExpr)
		if !ok || !mutation.Postfix || mutation.Op != test.op {
			t.Errorf("Parse(%q) expression = %#v, want postfix %s", test.source, statement.Expr, test.op)
			continue
		}
		variable, ok := mutation.Operand.(*ast.VariableExpr)
		if !ok || variable.Kind != ast.ScalarVariable || variable.Name != test.name {
			t.Errorf("Parse(%q) operand = %#v, want scalar %q", test.source, mutation.Operand, test.name)
		}
	}

	invalid := []string{
		`$value ++;`,
		`$value --;`,
		`($value)++;`,
		`($value)--;`,
		`@values[0]++;`,
		`%values['key']--;`,
		`$values['key']++;`,
	}
	for _, source := range invalid {
		result := Parse(lexer.NewSource("postfix-invalid.sl", []byte(source)))
		if got, want := len(result.Diagnostics), 1; got != want {
			t.Errorf("Parse(%q) diagnostics = %v, want %d", source, result.Diagnostics, want)
			continue
		}
		diagnostic := result.Diagnostics[0]
		if diagnostic.Severity != lexer.SeverityError ||
			diagnostic.Code != diagnosticUnexpectedToken ||
			diagnostic.Message != "Syntax error" ||
			diagnostic.Span.Start.Offset != 0 ||
			diagnostic.Span.End.Offset != len(source)-1 {
			t.Errorf("Parse(%q) diagnostic = %+v, want one full-expression PAR005 Syntax error", source, diagnostic)
		}
	}
}
