package compiler

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/ast"
	"github.com/sliverarmory/opfor/internal/bytecode"
	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestCompileSleepLoopControlCarriesDynamicGotoBoundaries(t *testing.T) {
	t.Parallel()

	parsed := parser.Parse(lexer.NewSource("dynamic-loop-control.sl", []byte(`
inline escape { break mark("inline"); }
while (ready()) { escape(); continue mark("while"); }
for ($i = 0; $i < 2; $i++) { escape(); }
foreach $value (@values) { continue mark($value); }
`)))
	if parsed.HasErrors() {
		t.Fatalf("parse diagnostics: %v", parsed.Diagnostics)
	}
	compiled := Compile(parsed.Script)
	if len(compiled.Diagnostics) != 0 {
		t.Fatalf("compiler diagnostics: %v", compiled.Diagnostics)
	}
	if got, want := len(compiled.Function.LoopRecoveries), 3; got != want {
		t.Fatalf("loop recovery count = %d, want %d", got, want)
	}
	for index, recovery := range compiled.Function.LoopRecoveries {
		if recovery.Start >= recovery.End || recovery.BodyStart >= recovery.BodyEnd ||
			recovery.BreakTarget < 0 || recovery.ContinueTarget < 0 {
			t.Errorf("loop recovery %d is incomplete: %+v", index, recovery)
		}
	}

	bind := compiled.Function.Instructions[0]
	if bind.Op != bytecode.OpBind || bind.Body == nil || len(bind.Body.Instructions) < 2 {
		t.Fatalf("inline binding instruction = %+v", bind)
	}
	flow := bind.Body.Instructions[0]
	if flow.Op != bytecode.OpBreak || flow.Expr == nil {
		t.Fatalf("inline flow instruction = %+v, want break with operand", flow)
	}

	evaluatedOperands := 0
	clearingJumps := 0
	for index, instruction := range compiled.Function.Instructions {
		if instruction.Op == bytecode.OpBreak || instruction.Op == bytecode.OpContinue {
			t.Errorf("lexical instruction %d = %s, want evaluated operand plus static jump", index, instruction.Op)
		}
		if instruction.Op == bytecode.OpJump && instruction.Target < 0 {
			t.Errorf("lexical jump %d has unpatched target: %+v", index, instruction)
		}
		if instruction.Op == bytecode.OpJump && instruction.ClearResult {
			clearingJumps++
		}
		if instruction.Op == bytecode.OpEval {
			call, ok := instruction.Expr.(*ast.CallExpr)
			if !ok {
				continue
			}
			callee, ok := call.Callee.(*ast.IdentifierExpr)
			if ok && callee.Name == "mark" {
				evaluatedOperands++
			}
		}
	}
	if got, want := evaluatedOperands, 2; got != want {
		t.Errorf("lexical optional operands lowered to eval = %d, want %d", got, want)
	}
	if got, want := clearingJumps, 2; got != want {
		t.Errorf("lexical result-clearing jumps = %d, want %d", got, want)
	}
}
