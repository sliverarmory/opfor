package parser

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/ast"
	"github.com/sliverarmory/opfor/internal/lexer"
)

func TestParseSleepBooleanCallPrecedenceAndExactCase(t *testing.T) {
	t.Parallel()

	result := Parse(lexer.NewSource("word-literals.sl", []byte(`
true;
false;
true();
false();
TRUE();
False();
null();
NULL();
TRUE;
False;
null;
NULL;
`)))
	if result.HasErrors() {
		t.Fatalf("parse diagnostics = %+v", result.Diagnostics)
	}
	if got, want := len(result.Script.Statements), 12; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}

	for index, want := range []bool{true, false} {
		statement := result.Script.Statements[index].(*ast.ExprStmt)
		literal, ok := statement.Expr.(*ast.BoolExpr)
		if !ok || literal.Value != want {
			t.Errorf("statement %d expression = %#v, want boolean %t", index+1, statement.Expr, want)
		}
	}

	for offset, want := range []string{"true", "false", "TRUE", "False", "null", "NULL"} {
		statement := result.Script.Statements[offset+2].(*ast.ExprStmt)
		call, ok := statement.Expr.(*ast.CallExpr)
		if !ok {
			t.Errorf("statement %d expression = %T, want *ast.CallExpr", offset+3, statement.Expr)
			continue
		}
		callee, ok := call.Callee.(*ast.IdentifierExpr)
		if !ok || callee.Name != want {
			t.Errorf("statement %d callee = %#v, want identifier %q", offset+3, call.Callee, want)
		}
	}

	for offset, want := range []string{"TRUE", "False", "null", "NULL"} {
		statement := result.Script.Statements[offset+8].(*ast.ExprStmt)
		identifier, ok := statement.Expr.(*ast.IdentifierExpr)
		if !ok || identifier.Name != want {
			t.Errorf("statement %d expression = %#v, want identifier %q", offset+9, statement.Expr, want)
		}
	}
}
