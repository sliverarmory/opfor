package lexer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sliverarmory/opfor/internal/envspec"
)

func TestLexOfficialStyleSample(t *testing.T) {
	t.Parallel()

	source := NewSource("sample.cna", []byte(`# greet a synchronized client
$x = 3;
$z = @(1, 2, 0x10, 077L, 5.4, 1e3);
$a = %(a => "apple", b => 'bat');
sub addTwoValues { println($1 + $2); }
on ready { println("Hello $1"); }
`))
	result := Lex(source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}

	want := []Kind{
		Comment,
		Scalar, Operator, Integer, Semicolon,
		Scalar, Operator, Array, LeftParen, Integer, Comma, Integer, Comma, Integer, Comma, Long, Comma, Double, Comma, Double, RightParen, Semicolon,
		Scalar, Operator, Hash, LeftParen, Identifier, Operator, DoubleString, Comma, Identifier, Operator, SingleString, RightParen, Semicolon,
		Keyword, Identifier, LeftBrace, Identifier, LeftParen, Scalar, Operator, Scalar, RightParen, Semicolon, RightBrace,
		Keyword, Identifier, LeftBrace, Identifier, LeftParen, DoubleString, RightParen, Semicolon, RightBrace,
		EOF,
	}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, want) {
		t.Fatalf("token kinds mismatch\n got: %v\nwant: %v", got, want)
	}

	if got := result.Tokens[1].Text; got != "x" {
		t.Fatalf("scalar text = %q, want x", got)
	}
	var lastDoubleQuoted Token
	for _, token := range result.Tokens {
		if token.Kind == DoubleString {
			lastDoubleQuoted = token
		}
	}
	if got := lastDoubleQuoted.Text; got != "Hello $1" {
		t.Fatalf("string text = %q, want raw interpolation text", got)
	}
}

func TestLexTokenForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		kind   Kind
		text   string
		lexeme string
	}{
		{name: "scalar", input: `$name`, kind: Scalar, text: "name", lexeme: `$name`},
		{name: "positional scalar", input: `$0`, kind: Scalar, text: "0", lexeme: `$0`},
		{name: "interpolation concatenator", input: `$+`, kind: Scalar, text: "+", lexeme: `$+`},
		{name: "null scalar", input: `$null`, kind: Scalar, text: "null", lexeme: `$null`},
		{name: "array", input: `@items`, kind: Array, text: "items", lexeme: `@items`},
		{name: "array constructor", input: `@()`, kind: Array, text: "", lexeme: `@`},
		{name: "hash", input: `%lookup`, kind: Hash, text: "lookup", lexeme: `%lookup`},
		{name: "function with hyphen", input: `&foo-bar`, kind: Function, text: "foo-bar", lexeme: `&foo-bar`},
		{name: "function with plus", input: `&foo+bar`, kind: Function, text: "foo+bar", lexeme: `&foo+bar`},
		{name: "class", input: `^java.lang.String`, kind: Class, text: "java.lang.String", lexeme: `^java.lang.String`},
		{name: "single quoted", input: `'raw\n'`, kind: SingleString, text: `raw\n`, lexeme: `'raw\n'`},
		{name: "double quoted alignment", input: `"$[10]name"`, kind: DoubleString, text: `$[10]name`, lexeme: `"$[10]name"`},
		{name: "hex integer", input: `0xff`, kind: Integer, text: "0xff", lexeme: `0xff`},
		{name: "octal integer", input: `077`, kind: Integer, text: "077", lexeme: `077`},
		{name: "long", input: `42L`, kind: Long, text: "42L", lexeme: `42L`},
		{name: "double", input: `5.25`, kind: Double, text: "5.25", lexeme: `5.25`},
		{name: "keyword", input: `foreach`, kind: Keyword, text: "foreach", lexeme: `foreach`},
		{name: "word operator", input: `eq`, kind: Operator, text: "eq", lexeme: `eq`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := Lex(NewSource(test.name, []byte(test.input)))
			if test.kind == Invalid {
				if len(result.Diagnostics) != 1 {
					t.Fatalf("diagnostics = %v, want one", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
			}
			got := result.Tokens[0]
			if got.Kind != test.kind || got.Text != test.text || got.Lexeme != test.lexeme {
				t.Fatalf("token = %+v, want kind=%s text=%q lexeme=%q", got, test.kind, test.text, test.lexeme)
			}
		})
	}
}

func TestLexValidatesParsedLiteralHexEscapes(t *testing.T) {
	t.Parallel()

	valid := Lex(NewSource("valid.sl", []byte(`"\u0041\x41\\u004\cE" 'literal \u004'`)))
	if len(valid.Diagnostics) != 0 {
		t.Fatalf("valid diagnostics = %v", valid.Diagnostics)
	}

	malformed := Lex(NewSource("malformed.sl", []byte(`"\u004 test \u004 \x4 test \xAZ"`)))
	want := []string{
		"invalid unicode escape \\u004  - must be hex digits",
		"invalid unicode escape \\u004  - must be hex digits",
		"invalid unicode escape \\x4  - must be hex digits",
		"invalid unicode escape \\xAZ - must be hex digits",
	}
	if len(malformed.Diagnostics) != len(want) {
		t.Fatalf("diagnostics = %v, want %d", malformed.Diagnostics, len(want))
	}
	for index, diagnostic := range malformed.Diagnostics {
		if diagnostic.Code != diagnosticMalformedEscape || diagnostic.Message != want[index] {
			t.Errorf("diagnostic %d = %#v, want %q", index, diagnostic, want[index])
		}
	}

	// Spaces are consumed as part of malformed escapes, so verify truly
	// truncated spellings separately.
	short := Lex(NewSource("short.sl", []byte("\"\\u004\" \"\\x4\"")))
	wantShort := []string{
		"not enough remaining characters for \\uXXXX",
		"not enough remaining characters for \\xXX",
	}
	if len(short.Diagnostics) != len(wantShort) {
		t.Fatalf("short diagnostics = %v, want %d", short.Diagnostics, len(wantShort))
	}
	for index, diagnostic := range short.Diagnostics {
		if diagnostic.Message != wantShort[index] {
			t.Errorf("short diagnostic %d = %q, want %q", index, diagnostic.Message, wantShort[index])
		}
	}
}

func TestLexSymbolicOperatorsLongestFirst(t *testing.T) {
	t.Parallel()

	operators := []string{
		"!iswm", "!isa", "!is", "!in",
		"<<=", ">>=", "**=", "<=>", "===", "!==", "!=~",
		"=>", "==", "!=", "<=", ">=", "&&", "||", "++", "--",
		"+=", "-=", "*=", "/=", "%=", ".=", "&=", "|=", "^=",
		"<<", ">>", "**", "=~", "!~", "::", "->",
		"=", "+", "-", "*", "/", "%", ".", "<", ">", "!", "~", "|", "^", "?", "&",
	}
	for _, operator := range operators {
		operator := operator
		t.Run(operator, func(t *testing.T) {
			t.Parallel()
			result := Lex(NewSource("operator", []byte(operator)))
			if len(result.Diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
			}
			if got := result.Tokens[0]; !got.Is(Operator, operator) {
				t.Fatalf("token = %+v, want operator %q", got, operator)
			}
		})
	}
}

func TestLexExtendedKeywordsAndPredicates(t *testing.T) {
	t.Parallel()

	keywordInput := "callcc done halt yield ssh_alias report when from true false set new"
	keywordsResult := Lex(NewSource("keywords", []byte(keywordInput)))
	if len(keywordsResult.Diagnostics) != 0 {
		t.Fatalf("keyword diagnostics: %v", keywordsResult.Diagnostics)
	}
	for _, token := range keywordsResult.Tokens[:len(keywordsResult.Tokens)-1] {
		if token.Kind != Keyword {
			t.Errorf("%q kind = %s, want keyword", token.Lexeme, token.Kind)
		}
	}

	operatorInput := "cmp isin ismatch hasmatch -eof !-eof -custom-predicate !-custom-predicate"
	operatorsResult := Lex(NewSource("predicates", []byte(operatorInput)))
	if len(operatorsResult.Diagnostics) != 0 {
		t.Fatalf("operator diagnostics: %v", operatorsResult.Diagnostics)
	}
	for _, token := range operatorsResult.Tokens[:len(operatorsResult.Tokens)-1] {
		if token.Kind != Operator {
			t.Errorf("%q kind = %s, want operator", token.Lexeme, token.Kind)
		}
	}
}

func TestLexEnvironmentSpecificationKeywordBoundaries(t *testing.T) {
	t.Parallel()

	for _, spec := range envspec.Builtins() {
		spec := spec
		t.Run(spec.Keyword, func(t *testing.T) {
			t.Parallel()
			result := Lex(NewSource("environment-keyword", []byte(spec.Keyword)))
			if len(result.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %v", result.Diagnostics)
			}
			want := Identifier
			if spec.LexicalKeyword {
				want = Keyword
			}
			if got := result.Tokens[0].Kind; got != want {
				t.Fatalf("%q kind = %s, want %s", spec.Keyword, got, want)
			}

			mixed := strings.ToUpper(spec.Keyword[:1]) + spec.Keyword[1:]
			mixedResult := Lex(NewSource("mixed-environment-keyword", []byte(mixed)))
			if got := mixedResult.Tokens[0].Kind; got != Identifier {
				t.Fatalf("mixed-case %q kind = %s, want identifier", mixed, got)
			}
		})
	}

	report := Lex(NewSource("generic-host-extension", []byte("report")))
	if got := report.Tokens[0].Kind; got != Keyword {
		t.Fatalf("report kind = %s, want keyword", got)
	}
}

func TestLexHashConstructorModuloAndImportWildcard(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("ambiguity.cna", []byte(`%(a => 1); $n % (2 + 1); import java.awt.*; "a"."b";`)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}

	percentTokens := make([]Token, 0, 2)
	dotTokens := make([]Token, 0, 3)
	var wildcard Token
	for _, token := range result.Tokens {
		switch token.Lexeme {
		case "%":
			percentTokens = append(percentTokens, token)
		case ".":
			dotTokens = append(dotTokens, token)
		case "*":
			wildcard = token
		}
	}
	if len(percentTokens) != 2 {
		t.Fatalf("percent tokens = %v, want constructor and modulo", percentTokens)
	}
	if percentTokens[0].Kind != Hash || percentTokens[0].TrailingWhitespace {
		t.Fatalf("%%( token = %+v, want adjacent hash constructor", percentTokens[0])
	}
	if percentTokens[1].Kind != Operator || !percentTokens[1].LeadingWhitespace || !percentTokens[1].TrailingWhitespace {
		t.Fatalf("%% ( token = %+v, want whitespace-separated modulo", percentTokens[1])
	}
	if !wildcard.Is(Operator, "*") || wildcard.LeadingWhitespace {
		t.Fatalf("import wildcard token = %+v", wildcard)
	}
	if len(dotTokens) != 3 {
		t.Fatalf("dot tokens = %v, want two import selectors and one concat dot", dotTokens)
	}
	for _, token := range dotTokens {
		if token.LeadingWhitespace || token.TrailingWhitespace {
			t.Errorf("dot exception must retain adjacency without a lexer error: %+v", token)
		}
	}
}

func TestLexPreservesOperatorAdjacency(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("adjacency.cna", []byte("$x=1+2; $y = 1 + 2;")))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}

	wantLexemes := []string{"$x", "=", "1", "+", "2", ";", "$y", "=", "1", "+", "2", ";", ""}
	if got := lexemes(result.Tokens); !reflect.DeepEqual(got, wantLexemes) {
		t.Fatalf("lexemes mismatch\n got: %q\nwant: %q", got, wantLexemes)
	}

	for _, index := range []int{1, 3} {
		token := result.Tokens[index]
		if token.LeadingWhitespace || token.TrailingWhitespace {
			t.Errorf("invalid-expression operator %q unexpectedly separated: %+v", token.Lexeme, token)
		}
	}
	for _, index := range []int{7, 9} {
		token := result.Tokens[index]
		if !token.LeadingWhitespace || !token.TrailingWhitespace {
			t.Errorf("valid-expression operator %q lacks whitespace signal: %+v", token.Lexeme, token)
		}
	}
	if !result.Tokens[0].Adjacent(result.Tokens[1]) || !result.Tokens[1].Adjacent(result.Tokens[2]) {
		t.Fatal("touching invalid-expression tokens must report adjacency")
	}
}

func TestLexQuotedStringsRemainRaw(t *testing.T) {
	t.Parallel()

	input := "\"line\\n\\c4red\\Uunder\\U\\o $x \\\"quoted\\\"\" 'it\\'s' `first\nsecond`"
	result := Lex(NewSource("strings.cna", []byte(input)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}

	wantKinds := []Kind{DoubleString, SingleString, BacktickString, EOF}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds = %v, want %v", got, wantKinds)
	}
	wantText := []string{
		`line\n\c4red\Uunder\U\o $x \"quoted\"`,
		`it\'s`,
		"first\nsecond",
	}
	for index, want := range wantText {
		if got := result.Tokens[index].Text; got != want {
			t.Errorf("token %d text = %q, want %q", index, got, want)
		}
	}
	if got := result.Tokens[2].Span.End.Line; got != 2 {
		t.Fatalf("multiline string end line = %d, want 2", got)
	}
}

func TestLexQuotedStringRetainsInteriorSourceRange(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("string-range.sl", []byte("before \"first\n$missing\" after")))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	token := result.Tokens[1]
	if token.Kind != DoubleString {
		t.Fatalf("token kind = %s, want double-quoted string", token.Kind)
	}
	if got, want := token.TextSpan.Source, "string-range.sl"; got != want {
		t.Fatalf("text source = %q, want %q", got, want)
	}
	if got, want := token.TextSpan.Start, (Position{Offset: 8, Line: 1, Column: 9}); got != want {
		t.Fatalf("text start = %+v, want %+v", got, want)
	}
	if got, want := token.TextSpan.End, (Position{Offset: 22, Line: 2, Column: 9}); got != want {
		t.Fatalf("text end = %+v, want %+v", got, want)
	}
}

func TestLexReferencesClassesAndCompatibleIdentifiers(t *testing.T) {
	t.Parallel()

	input := `\$x \@items \%lookup \&handler ^java.lang.String ^java.util.Map$Entry[] foo-bar foo+bar $0 $+ $null @_ %infomap &callback`
	result := Lex(NewSource("names.cna", []byte(input)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}

	wantKinds := []Kind{
		Reference, Reference, Reference, Reference,
		Class, Class,
		Identifier, Identifier,
		Scalar, Scalar, Scalar, Array, Hash, Function,
		EOF,
	}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds mismatch\n got: %v\nwant: %v", got, wantKinds)
	}
	if got := result.Tokens[0].Text; got != "$x" {
		t.Errorf("reference text = %q, want $x", got)
	}
	if got := result.Tokens[5].Text; got != "java.util.Map$Entry[]" {
		t.Errorf("class text = %q", got)
	}
	if got := result.Tokens[7].Lexeme; got != "foo+bar" {
		t.Errorf("plus identifier = %q", got)
	}
}

func TestLexClassLiteralLeavesAdjacentConcatenation(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("class-concat.sl", []byte(`^java.util.List."text" ^java.lang.Character$Subset.$value`)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	wantKinds := []Kind{Class, Operator, DoubleString, Class, Operator, Scalar, EOF}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds mismatch\n got: %v\nwant: %v", got, wantKinds)
	}
	if got := result.Tokens[0].Text; got != "java.util.List" {
		t.Errorf("first class text = %q", got)
	}
	if got := result.Tokens[3].Text; got != "java.lang.Character$Subset" {
		t.Errorf("second class text = %q", got)
	}
}

func TestLexNumericForms(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("numbers.cna", []byte("0 42 077 12L 0xff 0XFFL 5.4 1. 1e3 2.5E-2 3d 3D 3f 3F")))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	wantKinds := []Kind{Integer, Integer, Integer, Long, Integer, Long, Double, Double, Double, Double, Double, Double, Double, Double, EOF}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds mismatch\n got: %v\nwant: %v", got, wantKinds)
	}
}

func TestLexSleepRejectsLowercaseLongAndLeadingDotNumericForms(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("numbers.cna", []byte("12l 0xffl .25")))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	wantKinds := []Kind{Integer, Identifier, Integer, Identifier, Operator, Integer, EOF}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds mismatch\n got: %v\nwant: %v", got, wantKinds)
	}
}

func TestLexSleepSpecialAndHexadecimalDoubleForms(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("special-numbers.cna", []byte(
		"NaN Infinity +NaN -NaN +Infinity -Infinity "+
			"0x1p0 0X1P2 0x1p+2 0x1p-2 0x1.p2 0x1.8p1 "+
			"0x1p2F 0x1p2f 0x1p2D 0x1p2d 0x1D 0x1F 0x1L",
	)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	wantKinds := []Kind{
		Identifier, Identifier, Double, Double, Double, Double,
		Double, Double, Double, Double, Double, Double,
		Double, Double, Double, Double,
		Integer, Integer, Long, EOF,
	}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds mismatch\n got: %v\nwant: %v", got, wantKinds)
	}
}

func TestLexSleepUnicodeIntegerAndHexadecimalForms(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("unicode-numbers.cna", []byte("١٢ １２ 0x١ 0xＦ 0xｆ 𝟙 0x𝟙")))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	wantKinds := []Kind{Integer, Integer, Integer, Integer, Integer, Integer, Integer, EOF}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds mismatch\n got: %v\nwant: %v", got, wantKinds)
	}
}

func TestLexSleepHexadecimalDotUsesFinalDecimalDigit(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("hex-dot.cna", []byte(`0x9.Ap1 0x1F."x" 0x12."x" 0xＦ."x" 0xＦ９."x"`)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	wantKinds := []Kind{
		Double,
		Integer, Operator, DoubleString,
		Identifier, DoubleString,
		Integer, Operator, DoubleString,
		Identifier, DoubleString,
		EOF,
	}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds mismatch\n got: %v\nwant: %v", got, wantKinds)
	}
}

func TestLexSleepAdjacentNumericIdentifierTermsRemainDetectable(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("numeric-identifiers.cna", []byte(
		"1ticks ١ticks １２ticks 0x1ticks 0xＦticks 1Lticks 1.2ticks 1e2ticks 0x1p2ticks 1 ticks",
	)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	wantKinds := []Kind{
		Integer, Identifier, Integer, Identifier, Integer, Identifier,
		Integer, Identifier, Integer, Identifier, Long, Identifier,
		Double, Identifier, Double, Identifier, Double, Identifier,
		Integer, Identifier, EOF,
	}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds mismatch\n got: %v\nwant: %v", got, wantKinds)
	}
	for index := 0; index < 18; index += 2 {
		if !result.Tokens[index].Adjacent(result.Tokens[index+1]) {
			t.Errorf("tokens %d and %d are not adjacent: %v %v", index, index+1, result.Tokens[index], result.Tokens[index+1])
		}
	}
	if result.Tokens[18].Adjacent(result.Tokens[19]) {
		t.Fatalf("whitespace control tokens unexpectedly adjacent: %v %v", result.Tokens[18], result.Tokens[19])
	}
}

func TestLexNestedIndices(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("indices.cna", []byte(`$rows[0]['user'][@indices[1]]`)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	want := []Kind{
		Scalar, LeftBracket, Integer, RightBracket,
		LeftBracket, SingleString, RightBracket,
		LeftBracket, Array, LeftBracket, Integer, RightBracket, RightBracket,
		EOF,
	}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestLexPositionsAndComments(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("positions.cna", []byte("# one\r\nprintln(\"λ\"); # two\n")))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	if got := result.Tokens[1].Span.Start; got != (Position{Offset: 7, Line: 2, Column: 1}) {
		t.Fatalf("println start = %+v", got)
	}
	stringToken := result.Tokens[3]
	if got := stringToken.Span.End.Column; got != 12 {
		t.Fatalf("UTF-8 string end column = %d, want 12", got)
	}
	if got := result.Tokens[len(result.Tokens)-1].Span.Start.Line; got != 3 {
		t.Fatalf("EOF line = %d, want 3", got)
	}
}

func TestLexBareCarriageReturnDoesNotAdvanceLine(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("carriage-return.sl", []byte("println(1);\rprintln(2);")))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	second := -1
	for index, token := range result.Tokens {
		if token.Kind == Identifier && token.Text == "println" {
			if second >= 0 {
				second = index
				break
			}
			second = index
		}
	}
	if second < 0 || result.Tokens[second].Span.Start.Line != 1 {
		t.Fatalf("second println position = %+v, want line 1", result.Tokens[second].Span.Start)
	}

	commented := Lex(NewSource("comment-cr.sl", []byte("# comment\rstill comment\nprintln(3);")))
	if len(commented.Diagnostics) != 0 {
		t.Fatalf("comment diagnostics: %v", commented.Diagnostics)
	}
	if got := commented.Tokens[1].Text; got != "println" {
		t.Fatalf("first token after comment = %q, want println", got)
	}
	if got := commented.Tokens[1].Span.Start.Line; got != 2 {
		t.Fatalf("println line = %d, want 2", got)
	}
}

func TestLexMalformedInputProducesDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		code string
	}{
		{name: "unterminated string", text: `"never closes`, code: diagnosticUnterminatedQuote},
		{name: "bare scalar sigil", text: `$`, code: diagnosticMalformedSigil},
		{name: "bad reference", text: `\name`, code: diagnosticMalformedSigil},
		{name: "bad hexadecimal", text: `0x`, code: diagnosticMalformedNumber},
		{name: "invalid character", text: "§", code: diagnosticInvalidCharacter},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := Lex(NewSource(test.name, []byte(test.text)))
			if len(result.Diagnostics) != 1 {
				t.Fatalf("diagnostics = %v, want one", result.Diagnostics)
			}
			if got := result.Diagnostics[0].Code; got != test.code {
				t.Fatalf("diagnostic code = %q, want %q", got, test.code)
			}
			if result.Tokens[0].Kind != Invalid || result.Tokens[len(result.Tokens)-1].Kind != EOF {
				t.Fatalf("tokens = %v, want invalid then EOF", result.Tokens)
			}
		})
	}
}

func TestLeadingDotNumberTokensRemainSeparateFromAdjacentConcatenation(t *testing.T) {
	t.Parallel()

	result := Lex(NewSource("dot.cna", []byte(`@(.5, "x".1, $x .5, (.25));`)))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
	wantKinds := []Kind{
		Array, LeftParen, Operator, Integer, Comma,
		DoubleString, Operator, Integer, Comma,
		Scalar, Operator, Integer, Comma, LeftParen, Operator, Integer, RightParen,
		RightParen, Semicolon, EOF,
	}
	if got := kinds(result.Tokens); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds = %v, want %v", got, wantKinds)
	}
	for _, index := range []int{2, 6, 10, 14} {
		if got := result.Tokens[index].Lexeme; got != "." {
			t.Errorf("dot token %d = %q, want dot", index, got)
		}
	}
}

func kinds(tokens []Token) []Kind {
	result := make([]Kind, len(tokens))
	for index, token := range tokens {
		result[index] = token.Kind
	}
	return result
}

func lexemes(tokens []Token) []string {
	result := make([]string, len(tokens))
	for index, token := range tokens {
		result[index] = token.Lexeme
	}
	return result
}
