package parser_test

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/ast"
	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestParseSleepAssignmentWhileAcceptsAllVariableSigils(t *testing.T) {
	t.Parallel()

	result := parser.Parse(lexer.NewSource("assignment-while.sl", []byte(`
while $scalar_value (next_scalar()) {}
while @array_value (next_array()) {}
while %hash_value (next_hash()) {}
while (@(1)) { break; }
`)))
	assertNoErrors(t, result)

	want := []struct {
		raw  string
		kind ast.VariableKind
	}{
		{"$scalar_value", ast.ScalarVariable},
		{"@array_value", ast.ArrayVariable},
		{"%hash_value", ast.HashVariable},
	}
	for index, expected := range want {
		loop, ok := result.Script.Statements[index].(*ast.WhileStmt)
		if !ok {
			t.Fatalf("statement %d = %T, want *ast.WhileStmt", index, result.Script.Statements[index])
		}
		if loop.Binding == nil || loop.Binding.Raw != expected.raw || loop.Binding.Kind != expected.kind {
			t.Errorf("statement %d binding = %#v, want %q kind %d", index, loop.Binding, expected.raw, expected.kind)
		}
	}

	ordinary, ok := result.Script.Statements[3].(*ast.WhileStmt)
	if !ok {
		t.Fatalf("ordinary statement = %T, want *ast.WhileStmt", result.Script.Statements[3])
	}
	if ordinary.Binding != nil {
		t.Fatalf("ordinary array-literal condition binding = %#v, want nil", ordinary.Binding)
	}
	if _, ok := ordinary.Condition.(*ast.ArrayLiteralExpr); !ok {
		t.Fatalf("ordinary condition = %T, want *ast.ArrayLiteralExpr", ordinary.Condition)
	}
}
