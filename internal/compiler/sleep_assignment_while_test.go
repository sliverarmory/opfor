package compiler

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/bytecode"
	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestCompileSleepAssignmentWhilePreservesVariableSigils(t *testing.T) {
	t.Parallel()

	parsed := parser.Parse(lexer.NewSource("assignment-while.sl", []byte(`
while $scalar_value (next_scalar()) {}
while @array_value (next_array()) {}
while %hash_value (next_hash()) {}
`)))
	if parsed.HasErrors() {
		t.Fatalf("parse diagnostics: %v", parsed.Diagnostics)
	}
	compiled := Compile(parsed.Script)
	if len(compiled.Diagnostics) != 0 {
		t.Fatalf("compiler diagnostics: %v", compiled.Diagnostics)
	}

	var assignments []bytecode.Instruction
	for _, instruction := range compiled.Function.Instructions {
		if instruction.Op == bytecode.OpAssignWhile {
			assignments = append(assignments, instruction)
		}
	}
	want := []string{"$scalar_value", "@array_value", "%hash_value"}
	if len(assignments) != len(want) {
		t.Fatalf("assignment-while instruction count = %d, want %d", len(assignments), len(want))
	}
	for index, instruction := range assignments {
		if instruction.Name != want[index] {
			t.Errorf("instruction %d name = %q, want %q", index, instruction.Name, want[index])
		}
		if instruction.Target < 0 {
			t.Errorf("instruction %d has unpatched target: %+v", index, instruction)
		}
	}
}
