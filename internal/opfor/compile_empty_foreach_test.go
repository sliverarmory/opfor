package opfor

import (
	"errors"
	"testing"
)

func TestCompileRejectsEmptyForeachIterableWithoutPanicking(t *testing.T) {
	t.Parallel()

	program, err := CompileString("empty-foreach.sl", `foreach $item () { return $item; }`)
	if program != nil || err == nil {
		t.Fatalf("empty foreach compile = program %v, error %v; want nil/error", program != nil, err)
	}
	var compileErr *CompileError
	if !errors.As(err, &compileErr) {
		t.Fatalf("empty foreach compile error = %T %v, want *CompileError", err, err)
	}
	found := false
	for _, diagnostic := range compileErr.Diagnostics {
		if diagnostic.Code == "PAR002" && diagnostic.Message == "expected foreach iterable" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("empty foreach compile diagnostics = %#v, want PAR002", compileErr.Diagnostics)
	}
}
