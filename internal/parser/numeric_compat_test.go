package parser

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/ast"
	"github.com/sliverarmory/opfor/internal/lexer"
)

func TestParseSleepNumericContextAndFallback(t *testing.T) {
	t.Parallel()

	result := Parse(lexer.NewSource("numeric-context.sl", []byte(`
sub NaN { return "function"; }
NaN();
println(NaN);
println(09);
println(-2147483648);
println(-0x80000000);
`)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	if len(result.Script.Statements) != 6 {
		t.Fatalf("statement count = %d, want 6", len(result.Script.Statements))
	}

	callStatement, ok := result.Script.Statements[1].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("function-call statement = %T, want *ast.ExprStmt", result.Script.Statements[1])
	}
	call, ok := callStatement.Expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("function-call expression = %T, want *ast.CallExpr", callStatement.Expr)
	}
	callee, ok := call.Callee.(*ast.IdentifierExpr)
	if !ok || callee.Name != "NaN" {
		t.Fatalf("function-call callee = %#v, want identifier NaN", call.Callee)
	}

	wantKinds := []ast.NumberKind{ast.DoubleNumber, ast.DoubleNumber, ast.IntegerNumber, ast.IntegerNumber}
	for index, want := range wantKinds {
		statement := result.Script.Statements[index+2].(*ast.ExprStmt)
		call := statement.Expr.(*ast.CallExpr)
		number, ok := call.Args[0].(*ast.NumberExpr)
		if !ok || number.Kind != want {
			t.Errorf("statement %d argument = %#v, want number kind %v", index+3, call.Args[0], want)
		}
	}
}

func TestParseSleepRejectsDetachedArithmeticUnary(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`println(- 1);`,
		`println(+ 1);`,
		`println(-(1));`,
		`println(-$value);`,
		`println(- $value);`,
	} {
		result := Parse(lexer.NewSource("detached-sign.sl", []byte(source)))
		if !result.HasErrors() || !hasParserDiagnostic(result.Diagnostics, diagnosticExpectedExpression, "Unknown expression") {
			t.Errorf("Parse(%q) diagnostics = %+v, want Unknown expression", source, result.Diagnostics)
		}
	}
}

func TestParseSleepRejectsDigitEndingDottedNumbers(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`println(1.2."x");`,
		`println(1e2."x");`,
		`println(0x1p2."x");`,
		`println(-1e2."x");`,
		`println(-0x12."x");`,
		`println(+0x12."x");`,
		`println(-0xＦ９."x");`,
		`println(+0xＦ９."x");`,
		`println(-0x1p "x");`,
		`println(-0x1.0 "x");`,
	} {
		result := Parse(lexer.NewSource("dotted-number.sl", []byte(source)))
		if !result.HasErrors() || !hasParserDiagnostic(result.Diagnostics, diagnosticExpectedExpression, "Unknown expression") {
			t.Errorf("Parse(%q) diagnostics = %+v, want Unknown expression", source, result.Diagnostics)
		}
	}
}

func TestParseSleepRejectsAdjacentNumericIdentifierTerms(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`println(1ticks());`,
		`println(١ticks());`,
		`println(１２ticks());`,
		`println(0x1ticks());`,
		`println(0xＦticks());`,
		`println(-1ticks());`,
		`println(+1ticks());`,
		`println(1Lticks());`,
		`println(1.2ticks());`,
		`println(1e2ticks());`,
		`println(0x1p2ticks());`,
		`println(1true);`,
		`println(-1true);`,
		`println(1e2true);`,
		`println(0x1true);`,
		`println(0x1p2true);`,
		`println(１２true);`,
		`println(0xＦtrue);`,
		`println(1Ffoo());`,
		`println(1Dfoo());`,
	} {
		result := Parse(lexer.NewSource("numeric-identifier.sl", []byte(source)))
		if !result.HasErrors() || !hasParserDiagnostic(result.Diagnostics, diagnosticExpectedExpression, "Unknown expression") {
			t.Errorf("Parse(%q) diagnostics = %+v, want Unknown expression", source, result.Diagnostics)
		}
	}
}

func TestParseSleepRejectsAdjacentNumericSigilTerms(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`println(1$foo);`,
		`println(1@foo);`,
		`println(1%foo);`,
		`println(1&println);`,
		`println(1^String);`,
		`println(1\$foo);`,
		`println(1@(2));`,
		`println(1%("key" => 2));`,
		`println(NaN$foo);`,
		`println(Infinity@foo);`,
		`println(NaN%foo);`,
		`println(Infinity&println);`,
		`println(NaN^String);`,
		`println(Infinity\$foo);`,
		`println(-NaN$foo);`,
		`println(+Infinity@foo);`,
	} {
		result := Parse(lexer.NewSource("numeric-sigil.sl", []byte(source)))
		if !result.HasErrors() || !hasParserDiagnostic(result.Diagnostics, diagnosticExpectedExpression, "Unknown expression") {
			t.Errorf("Parse(%q) diagnostics = %+v, want Unknown expression", source, result.Diagnostics)
		}
	}
}

func TestParseSleepSpecialConstructorCallNames(t *testing.T) {
	t.Parallel()

	result := Parse(lexer.NewSource("special-constructor-call.sl", []byte(`println(NaN@(2)); println(Infinity%(a => 2));`)))
	if result.HasErrors() {
		t.Fatalf("parse diagnostics = %+v", result.Diagnostics)
	}
	want := []string{"NaN@", "Infinity%"}
	for index, name := range want {
		statement := result.Script.Statements[index].(*ast.ExprStmt)
		outer := statement.Expr.(*ast.CallExpr)
		inner := outer.Args[0].(*ast.CallExpr)
		callee, ok := inner.Callee.(*ast.IdentifierExpr)
		if !ok || callee.Name != name {
			t.Errorf("call %d callee = %#v, want identifier %q", index, inner.Callee, name)
		}
	}
}

func TestParseSleepNumericIdentifierWhitespaceControl(t *testing.T) {
	t.Parallel()

	source := `
sub ticks { return "tick"; }
println(1 ticks());
println(1 true);
println(1 $foo);
println(1 @foo);
println(1 %foo);
println(1 &println);
println(1 ^String);
println(1 \$foo);
println(1 @(2));
println(1 %("key" => 2));
println(1 "text");
println(1 { return 2; });
println(1());
println(NaN $foo);
println(Infinity @foo);
println(NaN %("key" => 2));
println(-NaN $foo);
println(+Infinity @foo);
println(NaN());
println(Infinity());
println(NaN@(2));
println(Infinity%("key" => 2));
`
	result := Parse(lexer.NewSource("numeric-identifier-space.sl", []byte(source)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Parse(%q) diagnostics = %+v, want accepted separate arguments", source, result.Diagnostics)
	}
}

func TestParseSleepPreservesSignedPredicateTermsForCompiler(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`println(-NaNF "x");`,
		`println(-NaNfoo "x");`,
		`println(-InfinityD "x");`,
		`println(-Infinityfoo "x");`,
		`println(!-NaNF "x");`,
	} {
		result := Parse(lexer.NewSource("malformed-special.sl", []byte(source)))
		if result.HasErrors() {
			t.Errorf("Parse(%q) diagnostics = %+v, want compiler-owned predicate-role validation", source, result.Diagnostics)
		}
	}

	result := Parse(lexer.NewSource("malformed-positive-special.sl", []byte(`println(+NaNF "x");`)))
	if !result.HasErrors() || !hasParserDiagnostic(result.Diagnostics, diagnosticExpectedExpression, "Unknown expression") {
		t.Errorf("positive malformed special diagnostics = %+v, want Unknown expression", result.Diagnostics)
	}
}

func TestParseSleepSignedSpecialPrefixPredicatesInConditions(t *testing.T) {
	t.Parallel()

	source := `
if (-NaNfoo 1) { println("nan"); }
if (-InfinityD 1) { println("infinity"); }
if (-predicate 1) { println("ordinary"); }
`
	result := Parse(lexer.NewSource("special-predicate.sl", []byte(source)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Parse predicate controls diagnostics = %+v, want none", result.Diagnostics)
	}

	groupedAndLogical := `
if ((-NaNfoo 1)) { println("grouped"); }
if (true && -InfinityD 1) { println("logical"); }
if (!-NaNfoo 1) { println("negated"); }
`
	result = Parse(lexer.NewSource("special-predicate-grouped.sl", []byte(groupedAndLogical)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Parse grouped/logical predicate controls diagnostics = %+v, want none", result.Diagnostics)
	}
}

func TestParseSleepPreservesNestedPredicateTermsForCompiler(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`if (println(-NaNfoo "x")) { println("yes"); } println("done");`,
		`if (@(-NaNfoo "x")) { println("yes"); } println("done");`,
		`if ({ println(-NaNfoo "x"); }) { println("yes"); } println("done");`,
		`$a = @("z"); if ($a[-NaNfoo "x"]) { println("yes"); } println("done");`,
	} {
		result := Parse(lexer.NewSource("predicate-value.sl", []byte(source)))
		if result.HasErrors() {
			t.Errorf("Parse(%q) diagnostics = %+v, want compiler-owned predicate-role validation", source, result.Diagnostics)
		}
	}
}

func hasParserDiagnostic(diagnostics []lexer.Diagnostic, code, message string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.Message == message {
			return true
		}
	}
	return false
}
