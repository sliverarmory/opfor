package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sliverarmory/opfor/internal/ast"
	"github.com/sliverarmory/opfor/internal/envspec"
	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestParseCoreAndAggressorForms(t *testing.T) {
	t.Parallel()
	source := lexer.NewSource("forms.cna", []byte(`
import java.awt.*;
sub choose {
    local('$key $value');
    ($key, $value) = @_;
    if ($key eq "go") {
        while $value (next_value()) { yield $value; }
    }
    else if ($key ismatch 'g.*') { return [$callback: $value]; }
    else { throw "bad"; }
}
for ($i = 0; $i < 3; $i++) { println($i); }
foreach $key => $value (%values) { println("$key=$value"); }
try { callcc &choose; } catch $error { warn($error); }
on * { println($1); }
set BEACON_OUTPUT { return "[$1] $2"; }
popup beacon { item "Run" { [new java.awt.Point: 1, 2]; } }
`))

	result := parser.Parse(source)
	assertNoErrors(t, result)
	if got, want := len(result.Script.Statements), 8; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}

	if _, ok := result.Script.Statements[0].(*ast.ImportStmt); !ok {
		t.Fatalf("first statement is %T, want *ast.ImportStmt", result.Script.Statements[0])
	}
	declaration, ok := result.Script.Statements[1].(*ast.EnvironmentStmt)
	if !ok || declaration.Keyword != "sub" || declaration.Selectors[0].Value != "choose" {
		t.Fatalf("sub declaration = %#v", result.Script.Statements[1])
	}
	if got := result.Script.Span(); got.Source != "forms.cna" || got.Start.Line != 2 || got.End.Line != 17 {
		t.Fatalf("script span = %v", got)
	}
}

func TestExpressionTreeAndSpans(t *testing.T) {
	t.Parallel()
	result := parser.Parse(lexer.NewSource("expr.cna", []byte(`$out = data_query("metadata")["c2profile"];
$point = [new java.awt.Point: 1, 2];
$result = [$callback: $out];
`)))
	assertNoErrors(t, result)
	if got, want := len(result.Script.Statements), 3; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}

	first := result.Script.Statements[0].(*ast.ExprStmt).Expr.(*ast.AssignExpr)
	index, ok := first.Value.(*ast.IndexExpr)
	if !ok {
		t.Fatalf("assignment value is %T, want *ast.IndexExpr", first.Value)
	}
	if _, ok := index.Target.(*ast.CallExpr); !ok {
		t.Fatalf("index target is %T, want *ast.CallExpr", index.Target)
	}
	if span := first.Span(); span.Start.Line != 1 || span.Start.Column != 1 || span.End.Column != 43 {
		t.Fatalf("assignment span = %v", span)
	}

	second := result.Script.Statements[1].(*ast.ExprStmt).Expr.(*ast.AssignExpr)
	object := second.Value.(*ast.ObjectExpr)
	if target, ok := object.Target.(*ast.IdentifierExpr); !ok || target.Name != "new" {
		t.Fatalf("new target = %#v", object.Target)
	}
	if object.Message == nil || object.Message.Name != "java.awt.Point" || len(object.Args) != 2 {
		t.Fatalf("object expression = %#v", object)
	}

	third := result.Script.Statements[2].(*ast.ExprStmt).Expr.(*ast.AssignExpr)
	direct := third.Value.(*ast.ObjectExpr)
	if direct.Message != nil || len(direct.Args) != 1 {
		t.Fatalf("direct closure invocation = %#v", direct)
	}
}

func TestStrictAndCompatibilitySeparators(t *testing.T) {
	t.Parallel()
	source := lexer.NewSource("legacy.cna", []byte("local('$x')\nprintln($x 1)\n"))
	compatible := parser.Parse(source)
	assertNoErrors(t, compatible)

	strict := parser.ParseWithOptions(source, parser.StrictOptions())
	if !strict.HasErrors() {
		t.Fatal("strict parse unexpectedly accepted omitted separators")
	}
	var sawTerminator, sawComma bool
	for _, diagnostic := range strict.Diagnostics {
		switch diagnostic.Code {
		case "PAR003":
			sawTerminator = true
		case "PAR006":
			sawComma = true
		}
	}
	if !sawTerminator || !sawComma {
		t.Fatalf("strict diagnostics = %v; want missing terminator and comma", strict.Diagnostics)
	}
}

func TestSleepCallParameterTermsAndValueParentheses(t *testing.T) {
	t.Parallel()
	result := parser.Parse(lexer.NewSource("parameter-terms.sl", []byte(`sink(mark("a"), mark("b"));
sink(mark("a") mark("b"), mark("c"));
sink(1 + 2 3);
sink(1(2));
sink($value());
sink(f()());
sink(,,,);
`)))
	assertNoErrors(t, result)

	calls := make([]*ast.CallExpr, 0, len(result.Script.Statements))
	for _, statement := range result.Script.Statements {
		expression, ok := statement.(*ast.ExprStmt)
		if !ok {
			t.Fatalf("statement is %T, want *ast.ExprStmt", statement)
		}
		call, ok := expression.Expr.(*ast.CallExpr)
		if !ok {
			t.Fatalf("expression is %T, want *ast.CallExpr", expression.Expr)
		}
		calls = append(calls, call)
	}

	assertGroups := func(call *ast.CallExpr, wantArgs int, wantGroups ...int) {
		t.Helper()
		if got := len(call.Args); got != wantArgs {
			t.Fatalf("call args = %d, want %d: %#v", got, wantArgs, call.Args)
		}
		if got := fmt.Sprint(call.ArgGroups); got != fmt.Sprint(wantGroups) {
			t.Fatalf("call groups = %v, want %v", call.ArgGroups, wantGroups)
		}
	}
	assertGroups(calls[0], 2, 1, 1)
	assertGroups(calls[1], 3, 2, 1)
	assertGroups(calls[2], 1, 1)
	operator, ok := calls[2].Args[0].(*ast.ParameterOperatorExpr)
	if !ok || operator.Op != "+" || len(operator.Right) != 2 {
		t.Fatalf("three-idea parameter = %#v, want + parameter operator with two RHS ideas", calls[2].Args[0])
	}
	assertGroups(calls[3], 2, 2)
	assertGroups(calls[4], 1, 1)
	assertGroups(calls[5], 1, 1)
	assertGroups(calls[6], 0)
}

func TestSleepNonFunctionPostfixParenthesesAreNotStatementCalls(t *testing.T) {
	t.Parallel()
	valid := parser.Parse(lexer.NewSource("non-function-value.sl", []byte(`$value = 7(); sub value { return 9(); } println($value());`)))
	assertNoErrors(t, valid)
	for _, source := range []string{`1();`, `$value = 7; $value();`, `(1)();`} {
		result := parser.Parse(lexer.NewSource("non-function-call.sl", []byte(source)))
		if !result.HasErrors() {
			t.Errorf("Parse(%q) unexpectedly accepted a non-function call statement", source)
		}
	}
}

func TestSleepPredicateShapedPairKeysRetainNormalizedSource(t *testing.T) {
	t.Parallel()
	result := parser.Parse(lexer.NewSource("raw-pair-keys.sl", []byte(`$h = %(-foo    ticks() => "a", -foo @(1) => "b", -foo (1 + 2) => "c", not(15) => "d");`)))
	assertNoErrors(t, result)
	assignment := result.Script.Statements[0].(*ast.ExprStmt).Expr.(*ast.AssignExpr)
	hash := assignment.Value.(*ast.HashLiteralExpr)
	want := []string{`-foo ticks()`, `-foo @(1)`, `-foo (1 + 2)`, `not(15)`}
	for index, entry := range hash.Entries {
		pair, ok := entry.(*ast.PairExpr)
		if !ok {
			t.Fatalf("entry %d = %T, want *ast.PairExpr", index, entry)
		}
		if pair.RawKey != want[index] {
			t.Errorf("entry %d raw key = %q, want %q", index, pair.RawKey, want[index])
		}
	}
}

func TestCompatibilityAllowsSameLineStatementSeparation(t *testing.T) {
	t.Parallel()
	result := parser.Parse(lexer.NewSource("legacy-inline.cna", []byte(`println(1) println(2)`)))
	assertNoErrors(t, result)
	if got, want := len(result.Script.Statements), 2; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}
}

func TestOperatorWhitespace(t *testing.T) {
	t.Parallel()
	result := parser.Parse(lexer.NewSource("spacing.cna", []byte("$x=1+2;")))
	if !result.HasErrors() {
		t.Fatal("operators without separating whitespace were accepted")
	}
}

func TestSleepArbitrarySymbolicOperatorName(t *testing.T) {
	t.Parallel()
	result := parser.Parse(lexer.NewSource("custom-operator.sl", []byte(`$z = $z *8 30;`)))
	assertNoErrors(t, result)
	assignment := result.Script.Statements[0].(*ast.ExprStmt).Expr.(*ast.AssignExpr)
	binary, ok := assignment.Value.(*ast.BinaryExpr)
	if !ok || binary.Op != "*8" {
		t.Fatalf("custom operator expression = %#v, want *8 binary", assignment.Value)
	}
}

func TestAssignmentAllowsOneSidedWhitespaceLikeReferenceParser(t *testing.T) {
	t.Parallel()
	for _, source := range []string{`$x ="value";`, `$x= "value";`} {
		result := parser.Parse(lexer.NewSource("assignment-spacing.sl", []byte(source)))
		assertNoErrors(t, result)
	}
}

func TestConcatenationWhitespaceException(t *testing.T) {
	t.Parallel()
	result := parser.Parse(lexer.NewSource("concat.sl", []byte(`$x = "left".$right;`)))
	assertNoErrors(t, result)
	assignment := result.Script.Statements[0].(*ast.ExprStmt).Expr.(*ast.AssignExpr)
	if binary, ok := assignment.Value.(*ast.BinaryExpr); !ok || binary.Op != "." {
		t.Fatalf("concatenation expression = %#v", assignment.Value)
	}
}

func TestSleepOperatorPrecedence(t *testing.T) {
	t.Parallel()
	result := parser.Parse(lexer.NewSource("precedence.sl", []byte(`
$first = $a + $b * $c;
$second = $a * $b + $c;
$third = $a + $b == $c && $ready;
`)))
	assertNoErrors(t, result)

	first := result.Script.Statements[0].(*ast.ExprStmt).Expr.(*ast.AssignExpr).Value.(*ast.BinaryExpr)
	if first.Op != "+" {
		t.Fatalf("first root operator = %q, want +", first.Op)
	}
	if right, ok := first.Right.(*ast.BinaryExpr); !ok || right.Op != "*" {
		t.Fatalf("first right expression = %#v", first.Right)
	}
	second := result.Script.Statements[1].(*ast.ExprStmt).Expr.(*ast.AssignExpr).Value.(*ast.BinaryExpr)
	if second.Op != "+" {
		t.Fatalf("second root operator = %q, want +", second.Op)
	}
	if left, ok := second.Left.(*ast.BinaryExpr); !ok || left.Op != "*" {
		t.Fatalf("second left expression = %#v", second.Left)
	}
	third := result.Script.Statements[2].(*ast.ExprStmt).Expr.(*ast.AssignExpr).Value.(*ast.BinaryExpr)
	if third.Op != "&&" {
		t.Fatalf("logical root operator = %q, want &&", third.Op)
	}
	predicate, ok := third.Left.(*ast.BinaryExpr)
	if !ok || predicate.Op != "==" {
		t.Fatalf("logical left expression = %#v", third.Left)
	}
	if additive, ok := predicate.Left.(*ast.BinaryExpr); !ok || additive.Op != "+" {
		t.Fatalf("predicate left expression = %#v", predicate.Left)
	}
}

func TestCollectionsAssignmentsAndRemainingFlow(t *testing.T) {
	t.Parallel()
	result := parser.Parse(lexer.NewSource("coverage.sl", []byte(`
@values = @(1, 2, 3);
%values = %("one" => 1, "two" => 2);
$values["one"] += 2;
$value++;
($left, $right) = @values;
assert $left gt 0 : "positive";
while ($left) { continue; break; }
if (-eof $handle) { done; } else { halt; }
yield;
return;
`)))
	assertNoErrors(t, result)
	if got, want := len(result.Script.Statements), 10; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}

	arrayAssignment := result.Script.Statements[0].(*ast.ExprStmt).Expr.(*ast.AssignExpr)
	if literal, ok := arrayAssignment.Value.(*ast.ArrayLiteralExpr); !ok || len(literal.Elements) != 3 {
		t.Fatalf("array literal = %#v", arrayAssignment.Value)
	}
	hashAssignment := result.Script.Statements[1].(*ast.ExprStmt).Expr.(*ast.AssignExpr)
	if literal, ok := hashAssignment.Value.(*ast.HashLiteralExpr); !ok || len(literal.Entries) != 2 {
		t.Fatalf("hash literal = %#v", hashAssignment.Value)
	}
	compound := result.Script.Statements[2].(*ast.ExprStmt).Expr.(*ast.AssignExpr)
	if compound.Op != "+=" {
		t.Fatalf("compound operator = %q", compound.Op)
	}
	postfix := result.Script.Statements[3].(*ast.ExprStmt).Expr.(*ast.UnaryExpr)
	if !postfix.Postfix || postfix.Op != "++" {
		t.Fatalf("postfix expression = %#v", postfix)
	}
	tuple := result.Script.Statements[4].(*ast.ExprStmt).Expr.(*ast.AssignExpr)
	if target, ok := tuple.Target.(*ast.TupleExpr); !ok || len(target.Elements) != 2 {
		t.Fatalf("tuple target = %#v", tuple.Target)
	}
	conditional := result.Script.Statements[7].(*ast.IfStmt)
	unary, ok := conditional.Condition.(*ast.UnaryExpr)
	if !ok || unary.Op != "-eof" {
		t.Fatalf("unary predicate = %#v", conditional.Condition)
	}
}

func TestEnvironmentSelectorsAndImportFrom(t *testing.T) {
	t.Parallel()
	result := parser.Parse(lexer.NewSource("environment.cna", []byte(`
import custom.Widget from: "widgets.jar";
on * { println($1); }
item "Quoted item" { return; }
bind Ctrl+H { println("help"); }
ssh_alias portfwd { println($0); }
report custom selector { println("host extension"); }
`)))
	assertNoErrors(t, result)
	if got, want := len(result.Script.Statements), 6; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}
	imported := result.Script.Statements[0].(*ast.ImportStmt)
	if imported.Target != "custom.Widget" || imported.From == nil {
		t.Fatalf("import = %#v", imported)
	}

	wildcard := result.Script.Statements[1].(*ast.EnvironmentStmt).Selectors[0]
	if wildcard.Kind != ast.WildcardSelector || wildcard.Value != "*" {
		t.Fatalf("wildcard selector = %#v", wildcard)
	}
	quoted := result.Script.Statements[2].(*ast.EnvironmentStmt).Selectors[0]
	if quoted.Kind != ast.StringSelector || quoted.Value != "Quoted item" {
		t.Fatalf("quoted selector = %#v", quoted)
	}
	key := result.Script.Statements[3].(*ast.EnvironmentStmt).Selectors[0]
	if key.Raw != "Ctrl+H" || key.Kind != ast.IdentifierSelector {
		t.Fatalf("bind selector = %#v", key)
	}
	generic := result.Script.Statements[5].(*ast.EnvironmentStmt)
	if generic.Keyword != "report" || len(generic.Selectors) != 2 {
		t.Fatalf("generic environment = %#v", generic)
	}
}

func TestBuiltInEnvironmentSpecificationsParseAsDeclarations(t *testing.T) {
	t.Parallel()

	for _, spec := range envspec.Builtins() {
		spec := spec
		t.Run(spec.Keyword, func(t *testing.T) {
			t.Parallel()
			source := spec.Keyword + ` declaration_name { return; }`
			result := parser.Parse(lexer.NewSource("built-in-environment.cna", []byte(source)))
			assertNoErrors(t, result)
			if len(result.Script.Statements) != 1 {
				t.Fatalf("statement count = %d, want 1", len(result.Script.Statements))
			}
			declaration, ok := result.Script.Statements[0].(*ast.EnvironmentStmt)
			if !ok {
				t.Fatalf("statement = %T, want *ast.EnvironmentStmt", result.Script.Statements[0])
			}
			if declaration.Keyword != spec.Keyword || declaration.Form != ast.OrdinaryEnvironment ||
				len(declaration.Selectors) != 1 || declaration.Selectors[0].Value != "declaration_name" {
				t.Fatalf("declaration = %#v", declaration)
			}
		})
	}
}

func TestEnvironmentSpecificationLookupRemainsCaseInsensitiveInParser(t *testing.T) {
	t.Parallel()

	result := parser.Parse(lexer.NewSource("mixed-environment.cna", []byte(`On ready { return; }`)))
	assertNoErrors(t, result)
	declaration, ok := result.Script.Statements[0].(*ast.EnvironmentStmt)
	if !ok || declaration.Keyword != "On" || declaration.Form != ast.OrdinaryEnvironment {
		t.Fatalf("mixed-case declaration = %#v", result.Script.Statements[0])
	}
}

func TestRegisteredFilterAndPredicateEnvironmentForms(t *testing.T) {
	t.Parallel()
	options := parser.CompatibilityOptions()
	options.Environments = map[string]ast.EnvironmentForm{
		"route": ast.FilterEnvironment,
		"guard": ast.PredicateEnvironment,
	}
	result := parser.ParseWithOptions(lexer.NewSource("custom-environments.sl", []byte(`
route alert "raw  $value" { return; }
guard ($value eq "ready") { return; }
route "calc" (2 + 2) { return; }
`)), options)
	assertNoErrors(t, result)
	if got := len(result.Script.Statements); got != 3 {
		t.Fatalf("statement count = %d, want 3", got)
	}
	filter := result.Script.Statements[0].(*ast.EnvironmentStmt)
	if filter.Form != ast.FilterEnvironment || len(filter.Selectors) != 2 || filter.Selectors[0].Raw != "alert" || filter.Selectors[1].Raw != `"raw  $value"` {
		t.Fatalf("filter environment = %#v", filter)
	}
	predicate := result.Script.Statements[1].(*ast.EnvironmentStmt)
	if predicate.Form != ast.PredicateEnvironment || predicate.Predicate == nil || len(predicate.Selectors) != 1 || predicate.Selectors[0].Raw != `($value eq "ready")` {
		t.Fatalf("predicate environment = %#v", predicate)
	}
	calculation := result.Script.Statements[2].(*ast.EnvironmentStmt)
	if calculation.Form != ast.FilterEnvironment || len(calculation.Selectors) != 2 || calculation.Selectors[0].Raw != `"calc"` || calculation.Selectors[1].Raw != `(2 + 2)` {
		t.Fatalf("expression filter environment = %#v", calculation)
	}
}

func TestOfficialAggressorExamples(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "upstream", "aggressor-script-examples")
	paths, err := filepath.Glob(filepath.Join(root, "*.cna"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if got, want := len(paths), 18; got != want {
		t.Fatalf("official example count = %d, want %d", got, want)
	}

	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			result := parser.Parse(lexer.NewSource(filepath.Base(path), data))
			assertNoErrors(t, result)
		})
	}
}

func assertNoErrors(t *testing.T, result parser.Result) {
	t.Helper()
	if !result.HasErrors() {
		return
	}
	var messages []string
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity == lexer.SeverityError {
			messages = append(messages, diagnostic.Error())
		}
	}
	t.Fatalf("parse errors:\n%s", strings.Join(messages, "\n"))
}
