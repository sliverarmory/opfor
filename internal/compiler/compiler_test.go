package compiler

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/bytecode"
	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestCompileControlFlow(t *testing.T) {
	t.Parallel()

	parsed := parser.Parse(lexer.NewSource("flow.cna", []byte(`
$x = 0;
while ($x < 3) {
    if ($x == 1) { $x++; continue; }
    println($x);
    $x++;
}
on ready { println("ready"); }
`)))
	if parsed.HasErrors() {
		t.Fatalf("parse diagnostics: %v", parsed.Diagnostics)
	}
	compiled := Compile(parsed.Script)
	if len(compiled.Diagnostics) != 0 {
		t.Fatalf("compiler diagnostics: %v", compiled.Diagnostics)
	}
	if compiled.Function == nil || len(compiled.Function.Instructions) == 0 {
		t.Fatal("compiler returned no instructions")
	}
	if got := compiled.Function.Instructions[len(compiled.Function.Instructions)-1].Op; got != bytecode.OpEnd {
		t.Fatalf("last opcode = %s, want end", got)
	}

	for index, instruction := range compiled.Function.Instructions {
		if (instruction.Op == bytecode.OpJump || instruction.Op == bytecode.OpJumpFalse) && instruction.Target < 0 {
			t.Errorf("instruction %d has unpatched target: %+v", index, instruction)
		}
	}
}

func TestCompilePreservesBreakAndContinueOutsideLoops(t *testing.T) {
	t.Parallel()

	parsed := parser.Parse(lexer.NewSource("detached-flow.sl", []byte("break mark('break'); continue mark('continue');")))
	compiled := Compile(parsed.Script)
	if len(compiled.Diagnostics) != 0 {
		t.Fatalf("compiler diagnostics: %v", compiled.Diagnostics)
	}
	if got, want := len(compiled.Function.Instructions), 3; got != want {
		t.Fatalf("instruction count = %d, want %d", got, want)
	}
	for index, want := range []bytecode.Op{bytecode.OpBreak, bytecode.OpContinue} {
		instruction := compiled.Function.Instructions[index]
		if instruction.Op != want || instruction.Expr == nil {
			t.Errorf("instruction %d = %+v, want %s with operand", index, instruction, want)
		}
	}
}

func TestCompileUnknownBarewordUsesCanonicalDescription(t *testing.T) {
	t.Parallel()

	parsed := parser.Parse(lexer.NewSource("bareword.sl", []byte(`println(unknown);`)))
	if parsed.HasErrors() {
		t.Fatalf("parse diagnostics: %v", parsed.Diagnostics)
	}
	compiled := Compile(parsed.Script)
	if got, want := len(compiled.Diagnostics), 1; got != want {
		t.Fatalf("diagnostics = %v, want %d", compiled.Diagnostics, want)
	}
	diagnostic := compiled.Diagnostics[0]
	if diagnostic.Code != diagnosticUnknownExpression || diagnostic.Message != "Unknown expression" ||
		diagnostic.Span.Start.Line != 1 || diagnostic.Span.Start.Column != 9 {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
}

func TestCompileRejectsNonSleepBareWordLiterals(t *testing.T) {
	t.Parallel()

	for _, word := range []string{"TRUE", "False", "null", "NULL"} {
		parsed := parser.Parse(lexer.NewSource("word-literal.sl", []byte("println("+word+");")))
		if parsed.HasErrors() {
			t.Errorf("Parse(%q) diagnostics = %+v", word, parsed.Diagnostics)
			continue
		}
		compiled := Compile(parsed.Script)
		if got, want := len(compiled.Diagnostics), 1; got != want {
			t.Errorf("Compile(%q) diagnostics = %+v, want %d", word, compiled.Diagnostics, want)
			continue
		}
		if diagnostic := compiled.Diagnostics[0]; diagnostic.Code != diagnosticUnknownExpression || diagnostic.Message != "Unknown expression" {
			t.Errorf("Compile(%q) diagnostic = %+v", word, diagnostic)
		}
	}
}

func TestCompileAllowsUnaryPredicatesOnlyInPredicateRoles(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`if (-NaNfoo 1) { println("yes"); }`,
		`if (-InfinityD 1) { println("yes"); }`,
		`if (-predicate 1) { println("yes"); }`,
		`if ((-NaNfoo 1)) { println("yes"); }`,
		`if (((-NaNfoo 1))) { println("yes"); }`,
		`if (true && -NaNfoo 1) { println("yes"); }`,
		`if (-NaNfoo 1 && true) { println("yes"); }`,
		`if (!-NaNfoo 1) { println("yes"); }`,
		`if (!true) { println("yes"); }`,
		`println(not(15));`,
		`println(iff(-isnumber 15, "yes", "no"));`,
		`println(?(-isnumber 15, "yes", "no"));`,
		`$value = %(-foo "x" => "v");`,
		`$value = %(-foo ticks() => "v");`,
		`$value = %(-foo @(1) => "v");`,
		`$value = %(-foo (1 + 2) => "v");`,
		`$value = %(not(15) => "v");`,
		`$value = %(!true => "v");`,
		`$value = %(~1 => "v");`,
		`$value = %(not true => "v");`,
		`$value = 1; $value++;`,
	} {
		parsed := parser.Parse(lexer.NewSource("predicate-role-valid.sl", []byte(source)))
		if parsed.HasErrors() {
			t.Errorf("Parse(%q) diagnostics = %+v", source, parsed.Diagnostics)
			continue
		}
		compiled := Compile(parsed.Script)
		if len(compiled.Diagnostics) != 0 {
			t.Errorf("Compile(%q) diagnostics = %+v, want none", source, compiled.Diagnostics)
		}
	}
}

func TestCompileRejectsUnaryPredicatesInValueRoles(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`println(-NaNfoo "x");`,
		`println(-InfinityD "x");`,
		`println(!-NaNF "x");`,
		`println(-isnumber 1);`,
		`println(-foo 1);`,
		`if (println(-NaNfoo "x")) { println("yes"); }`,
		`if (1 + -NaNfoo 1) { println("yes"); }`,
		`if (-NaNfoo 1 + 1) { println("yes"); }`,
		`if ((-NaNfoo 1) == true) { println("yes"); }`,
		`if (true == (-NaNfoo 1)) { println("yes"); }`,
		`if (!(-NaNfoo 1)) { println("yes"); }`,
		`$value = -NaNfoo "x";`,
		`$value = @(-NaNfoo "x");`,
		`$value = %("key" => -NaNfoo "x");`,
		`$values = @("z"); if ($values[-NaNfoo "x"]) { println("yes"); }`,
		`if ({ println(-NaNfoo "x"); }) { println("yes"); }`,
		`println(!true);`,
		`if (not true) { println("yes"); }`,
		`println(~1);`,
		`if (~1) { println("yes"); }`,
	} {
		parsed := parser.Parse(lexer.NewSource("predicate-role-invalid.sl", []byte(source)))
		if parsed.HasErrors() {
			t.Errorf("Parse(%q) diagnostics = %+v, want compiler-owned validation", source, parsed.Diagnostics)
			continue
		}
		compiled := Compile(parsed.Script)
		if !hasCompilerDiagnostic(compiled.Diagnostics, diagnosticUnknownExpression, "Unknown expression") {
			t.Errorf("Compile(%q) diagnostics = %+v, want %s Unknown expression", source, compiled.Diagnostics, diagnosticUnknownExpression)
		}
	}
}

func hasCompilerDiagnostic(diagnostics []lexer.Diagnostic, code, message string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.Message == message {
			return true
		}
	}
	return false
}
