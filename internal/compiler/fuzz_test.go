package compiler

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/bytecode"
	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

const maximumCompilerFuzzBytes = 64 << 10

func FuzzCompilePipeline(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`sub twice { return $1 * 2; } return twice(21);`),
		[]byte(`while $value (next_value()) { if ($value > 3) { break; } }`),
		[]byte(`try { foreach $item (@items) { yield $item; } } catch $error { return $error; }`),
		[]byte(`on ready { println($1); } popup beacon { item "x" { return 1; } }`),
		[]byte("\x00\xff\n}}}}"),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maximumCompilerFuzzBytes {
			t.Skip()
		}
		options := parser.CompatibilityOptions()
		options.MaximumDiagnostics = 64
		parsed := parser.ParseWithOptions(lexer.NewSource("<compiler-fuzz>", data), options)
		// The compiler's input contract is a parser-accepted tree. The public
		// Compile pipeline returns diagnostics before lowering a best-effort AST.
		if parsed.HasErrors() {
			return
		}
		compiled := Compile(parsed.Script)
		if parsed.Script != nil && compiled.Function == nil {
			t.Fatal("compiler returned a nil function for a non-nil script")
		}
		validateFuzzFunction(t, compiled.Function)
	})
}

func validateFuzzFunction(t *testing.T, function *bytecode.Function) {
	t.Helper()
	if function == nil {
		return
	}
	if len(function.Instructions) == 0 || function.Instructions[len(function.Instructions)-1].Op != bytecode.OpEnd {
		t.Fatalf("function %q lacks terminal end instruction", function.Name)
	}
	for index, recovery := range function.BlockRecoveries {
		if recovery.Start < 0 || recovery.Start > recovery.End || recovery.End > len(function.Instructions) ||
			recovery.Target < 0 || recovery.Target >= len(function.Instructions) {
			t.Fatalf("function %q block recovery %d is out of range: %#v with %d instructions",
				function.Name, index, recovery, len(function.Instructions))
		}
	}
	for index, recovery := range function.LoopRecoveries {
		if recovery.Start < 0 || recovery.Start > recovery.BodyStart || recovery.BodyStart > recovery.BodyEnd ||
			recovery.BodyEnd > recovery.End || recovery.End > len(function.Instructions) ||
			recovery.BreakTarget < 0 || recovery.BreakTarget >= len(function.Instructions) ||
			recovery.ContinueTarget < 0 || recovery.ContinueTarget >= len(function.Instructions) {
			t.Fatalf("function %q loop recovery %d is out of range: %#v with %d instructions",
				function.Name, index, recovery, len(function.Instructions))
		}
	}
	for _, instruction := range function.Instructions {
		validateFuzzFunction(t, instruction.Body)
	}
}
