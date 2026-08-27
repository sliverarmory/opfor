package opfor

import (
	"context"
	"testing"

	"github.com/sliverarmory/opfor/internal/ast"
	"github.com/sliverarmory/opfor/internal/bytecode"
)

func TestCompilePrecomputesLiteralAndClosureTemplates(t *testing.T) {
	program, err := CompileString("templates.sl", `
$plain = 123;
$single = 'single\'quote';
$double = "plain\ntext";
$dynamic = "value=$plain";
$outer = { return { return 7; }; };
return @($plain, $single, $double, $dynamic);
`)
	if err != nil {
		t.Fatal(err)
	}

	var number *ast.NumberExpr
	stringsByText := make(map[string]*ast.StringExpr)
	ast.Inspect(program.tree, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.NumberExpr:
			if node.Text == "123" {
				number = node
			}
		case *ast.StringExpr:
			stringsByText[node.Text] = node
		}
		return true
	})
	if number == nil || program.numberLiterals[number].value.Int32() != 123 {
		t.Fatalf("compiled number template = %#v", program.numberLiterals[number])
	}
	if template := program.stringLiterals[stringsByText["plain\\ntext"]]; !template.static || template.value.String() != "plain\ntext" {
		t.Fatalf("static double-quoted template = %#v", template)
	}
	if template := program.stringLiterals[stringsByText["value=$plain"]]; template.static || template.decoded.text != "value=$plain" {
		t.Fatalf("dynamic double-quoted template = %#v", template)
	}
	if got := countClosureTemplates(program.function); got != 2 {
		t.Fatalf("compiled closure template count = %d, want 2", got)
	}

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if len(values) != 4 || values[0].Int32() != 123 || values[1].String() != "single'quote" || values[2].String() != "plain\ntext" || values[3].String() != "value=123" {
		t.Fatalf("result values = %#v", values)
	}
}

func countClosureTemplates(function *bytecode.Function) int {
	if function == nil {
		return 0
	}
	count := len(function.ClosureTemplates)
	for _, nested := range function.ClosureTemplates {
		count += countClosureTemplates(nested)
	}
	return count
}
