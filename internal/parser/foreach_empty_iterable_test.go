package parser_test

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestForeachRequiresAnIterableExpression(t *testing.T) {
	t.Parallel()

	result := parser.Parse(lexer.NewSource("empty-foreach.sl", []byte(`foreach $item () { return $item; }`)))
	if !result.HasErrors() {
		t.Fatalf("empty foreach iterable diagnostics = %#v, want an error", result.Diagnostics)
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "PAR002" && diagnostic.Message == "expected foreach iterable" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("empty foreach iterable diagnostics = %#v, want PAR002", result.Diagnostics)
	}
}
