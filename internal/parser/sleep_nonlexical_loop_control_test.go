package parser_test

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/ast"
	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestParseSleepBreakAndContinueOptionalOperands(t *testing.T) {
	t.Parallel()

	result := parser.Parse(lexer.NewSource("detached-loop-control.sl", []byte(`
break mark("break");
continue mark("continue");
break;
continue;
`)))
	assertNoErrors(t, result)
	if got, want := len(result.Script.Statements), 4; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}
	first, ok := result.Script.Statements[0].(*ast.BreakStmt)
	if !ok || first.Value == nil {
		t.Fatalf("first statement = %#v, want break with operand", result.Script.Statements[0])
	}
	second, ok := result.Script.Statements[1].(*ast.ContinueStmt)
	if !ok || second.Value == nil {
		t.Fatalf("second statement = %#v, want continue with operand", result.Script.Statements[1])
	}
	third := result.Script.Statements[2].(*ast.BreakStmt)
	fourth := result.Script.Statements[3].(*ast.ContinueStmt)
	if third.Value != nil || fourth.Value != nil {
		t.Fatalf("bare flow operands = %#v/%#v, want nil/nil", third.Value, fourth.Value)
	}
}
