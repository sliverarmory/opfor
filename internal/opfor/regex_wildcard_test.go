package opfor

import (
	"bytes"
	"context"
	"testing"
)

func TestSleepRegexCaptureState(t *testing.T) {
	t.Parallel()

	program, err := CompileString("matcher-state.sl", `
while ("a1b2" hasmatch '([0-9])') {
    println(matched());
}
println(matched());
while ("a1b2" hasmatch '([0-9])') {
    println(matched());
}
if ("ab" ismatch '(a)(b)') {
    println(matched());
}
if ("no" ismatch '(x)') { println("unreachable"); }
println(matched());
println(matches("a1b2", '([a-z])([0-9])'));
println(matches("a1b2", '([a-z])([0-9])', 1, 1));
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "@('1')\n@('2')\n@()\n@('1')\n@('2')\n@('a', 'b')\n@()\n@('a', '1', 'b', '2')\n@('b', '2')\n"
	if got := output.String(); got != want {
		t.Fatalf("matcher output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSleepRegexTranslation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		want    string
	}{
		{pattern: `\w++`, want: `(?>[A-Za-z0-9_]+)`},
		{pattern: `a*+b`, want: `(?>a*)b`},
		{pattern: `a{2,4}+`, want: `(?>a{2,4})`},
		{pattern: `\++`, want: `\++`},
		{pattern: `[a[b]]`, want: `(?:[a]|[b])`},
		{pattern: `(?<word>\w+)`, want: `([A-Za-z0-9_]+)`},
		{pattern: `\Q++[]\E`, want: `\+\+\[\]`},
		// Java quotes continue through end-of-pattern when \E is omitted.
		{pattern: `\Qunterminated`, want: `unterminated`},
	}
	for _, test := range tests {
		got, err := translateSleepRegex(test.pattern)
		if err != nil {
			t.Fatalf("translateSleepRegex(%q): %v", test.pattern, err)
		}
		if got != test.want {
			t.Fatalf("translateSleepRegex(%q) = %q, want %q", test.pattern, got, test.want)
		}
	}

	supported := []struct {
		pattern string
		text    string
	}{
		{pattern: `a(?=b)b`, text: "ab"},
		{pattern: `a(?<=a)b`, text: "ab"},
		{pattern: `(?>ab)`, text: "ab"},
		{pattern: `(a)\1`, text: "aa"},
		{pattern: `(?<x>a)\k<x>`, text: "aa"},
		{pattern: `[a-z&&[^bc]]`, text: "d"},
		{pattern: `[a[^b]]`, text: "c"},
		// OpenJDK ignores a single empty right-hand intersection operand.
		{pattern: `[a-z&&]`, text: "d"},
	}
	for _, test := range supported {
		expression, err := compileSleepRegex(test.pattern, true)
		if err != nil {
			t.Fatalf("compileSleepRegex(%q): %v", test.pattern, err)
		}
		match, err := expression.FindStringSubmatchIndex(test.text)
		if err != nil {
			t.Fatalf("match %q against %q: %v", test.pattern, test.text, err)
		}
		if match == nil {
			t.Fatalf("pattern %q did not match %q", test.pattern, test.text)
		}
	}

	for _, pattern := range []string{`(?<1bad>a)`, `[&&]`} {
		if _, err := compileSleepRegex(pattern, false); err == nil {
			t.Fatalf("compileSleepRegex(%q) unexpectedly succeeded", pattern)
		}
	}
}

func TestSleepAdvancedJavaRegexCompatibility(t *testing.T) {
	t.Parallel()

	program, err := CompileString("java-regex.sl", `
if ("ab" ismatch 'a(?=b)b') { println("lookahead"); }
if ("ab" hasmatch '(?<=a)b') { println("lookbehind:" . matched()); }
if ("aa" ismatch '(a)\1') { println("numeric:" . matched()); }
if ("abab" ismatch '(?<word>ab)\k<word>') { println("named:" . matched()); }
if (!("abc" ismatch '(?>ab|a)bc')) { println("atomic"); }
if (!("aaa" ismatch 'a*+a')) { println("possessive"); }
if ("d" ismatch '[a-z&&[^bc]]') { println("subtraction"); }
if ("e" ismatch '[a-z&&[def]]') { println("intersection"); }
if (("a" ismatch '[a[^b]]') && !("b" ismatch '[a[^b]]')) { println("nested-negation"); }
if ("++[]" ismatch '\Q++[]\E') { println("quoted"); }
println(replace("ab cb", '(?<=a)b', "B"));
println(replace("ab ab", '(?<pair>ab)', '<${pair}>', 1));
println(split('(?<=a)', "aba"));
println(find("zab", '(?<=a)b', 0));
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "lookahead\n" +
		"lookbehind:@()\n" +
		"numeric:@('a')\n" +
		"named:@('ab')\n" +
		"atomic\n" +
		"possessive\n" +
		"subtraction\n" +
		"intersection\n" +
		"nested-negation\n" +
		"quoted\n" +
		"aB cb\n" +
		"<ab> ab\n" +
		"@('a', 'ba')\n" +
		"2\n"
	if got := output.String(); got != want {
		t.Fatalf("advanced Java regex output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSleepWildcardPointerSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		{pattern: "*bb", value: "bbaabb", want: false},
		{pattern: "**bb", value: "bbaabb", want: true},
		{pattern: "aa*b", value: "aabbbb", want: false},
		{pattern: "aa**b", value: "aabbbb", want: true},
		{pattern: `aa\*bb`, value: "aa*bb", want: true},
		{pattern: `J\?ck`, value: "J?ck", want: true},
		{pattern: "?", value: "😀", want: false},
		{pattern: "??", value: "😀", want: true},
	}
	for _, test := range tests {
		if got := wildcardMatch(test.pattern, test.value); got != test.want {
			t.Errorf("wildcardMatch(%q, %q) = %v, want %v", test.pattern, test.value, got, test.want)
		}
	}
}
