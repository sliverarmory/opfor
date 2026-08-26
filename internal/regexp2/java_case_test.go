package regexp2

import (
	"strings"
	"testing"
)

func compileFull(t *testing.T, pattern string, options RegexOptions) *Regexp {
	t.Helper()
	re, err := Compile(`\A(?:`+pattern+`)\z`, options)
	if err != nil {
		t.Fatalf("Compile(%q): %v", pattern, err)
	}
	return re
}

func assertMatch(t *testing.T, pattern, input string, want bool) {
	t.Helper()
	re := compileFull(t, pattern, None)
	got, err := re.MatchString(input)
	if err != nil {
		t.Fatalf("MatchString(%q, %q): %v", pattern, input, err)
	}
	if got != want {
		t.Fatalf("MatchString(%q, %q) = %v, want %v", pattern, input, got, want)
	}
}

func TestJavaASCIIAndUnicodeLiterals(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		{`(?a-j:a)`, "A", true},
		{`(?a-j:\u00e9)`, "É", false},
		{`(?a-j:k)`, "K", false},
		{`(?j-a:\u00e9)`, "É", true},
		{`(?j-a:k)`, "K", true},
		{`(?j-a:s)`, "ſ", true},
		{`(?j-a:i)`, "ı", true},
		{`(?j-a:\u03c3)`, "ς", true},
		{`(?j-a:\x{10400})`, "𐐨", true},
		{`(?j-a:\x{10428})`, "𐐀", true},
		{`(?j-a:\u00e9k)`, "ÉK", true},
		{`(?a-j:a{3})`, "AaA", true},
		{`(?j-a:s+)`, "sſS", true},
	}

	for _, test := range tests {
		t.Run(test.pattern+"/"+test.input, func(t *testing.T) {
			assertMatch(t, test.pattern, test.input, test.want)
		})
	}
}

func TestJavaCaseExplicitSetsAndRanges(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		{`(?a-j:[a-z])`, "A", true},
		{`(?a-j:[a-z])`, "K", false},
		{`(?a-j:[^\u00e9])`, "É", true},
		{`(?j-a:[a-z])`, "K", true},
		{`(?j-a:[a-z])`, "ſ", true},
		{`(?j-a:[a-z])`, "ı", true},
		{`(?j-a:[ı])`, "i", true},
		{`(?j-a:[ς])`, "Σ", true},
		{`(?j-a:[\x{10400}-\x{10427}])`, "𐐨", true},
		{`(?j-a:[^\u00e9])`, "É", false},
		{`(?j-a:[^a-z])`, "K", false},
		{`(?j-a:[a-z-[k]])`, "K", false},
		{`(?j-a:[a-z]+)`, "AKſı", true},
	}

	for _, test := range tests {
		t.Run(test.pattern+"/"+test.input, func(t *testing.T) {
			assertMatch(t, test.pattern, test.input, test.want)
		})
	}
}

func TestJavaCaseBackreferencesUseReferenceMode(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		{`(?a-j:(a)\1)`, "aA", true},
		{`(?a-j:(\u00e9)\1)`, "éÉ", false},
		{`(?j-a:(\u00e9)\1)`, "éÉ", true},
		{`(?j-a:(k)\1)`, "kK", true},
		{`(a)(?a-j:\1)`, "aA", true},
		{`(\u00e9)(?a-j:\1)`, "éÉ", false},
		{`(\u00e9)(?j-a:\1)`, "éÉ", true},
		{`(?<x>\u00e9)(?j-a:\k<x>)`, "éÉ", true},
		{`(?<x>\u00e9)(?a-j:\k<x>)`, "éÉ", false},
		{`(?j-a:(?<x>\u00e9))(?-aj:\k<x>)`, "Éé", false},
		{`(?j-a:(\x{10400})\1)`, "𐐀𐐨", true},
	}

	for _, test := range tests {
		t.Run(test.pattern+"/"+test.input, func(t *testing.T) {
			assertMatch(t, test.pattern, test.input, test.want)
		})
	}
}

func TestJavaCaseInlineTransitionsAndScopedRestoration(t *testing.T) {
	assertMatch(t, `(?j-a)k(?a-j)k(?-aj)k`, "KKk", true)
	assertMatch(t, `(?j-a:k)(?a-j:k)(?-aj:k)`, "KKk", true)
	assertMatch(t, `(?j-a:(?a-j:k)k)k`, "KKk", true)

	// These pairs must not be reduced into one Multi or Set across modes.
	assertMatch(t, `(?a-j:K)(?-aj:k)`, "KK", false)
	assertMatch(t, `(?:(?a-j:K)|(?-aj:k))`, "K", false)
}

func TestJavaCaseCompileOptionsAndOpcodes(t *testing.T) {
	for _, test := range []struct {
		option RegexOptions
		input  string
		want   bool
	}{
		{JavaASCII, "É", false},
		{JavaUnicode, "É", true},
	} {
		re := compileFull(t, `\u00e9`, test.option)
		got, err := re.MatchString(test.input)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("option %#x match = %v, want %v", test.option, got, test.want)
		}
		if re.code.FcPrefix != nil || re.code.BmPrefix != nil {
			t.Fatalf("custom case mode unexpectedly enabled prefix optimization")
		}
	}

	if _, err := Compile(`a`, JavaASCII|JavaUnicode); err == nil {
		t.Fatal("Compile accepted mutually exclusive Java case options")
	}
	if _, err := Compile(`a`, IgnoreCase|JavaASCII); err == nil {
		t.Fatal("Compile accepted stock and Java ASCII case options together")
	}
	if _, err := Compile(`a`, IgnoreCase|JavaUnicode); err == nil {
		t.Fatal("Compile accepted stock and Java Unicode case options together")
	}
	if _, err := Compile(`(?A:a)`, None); err == nil {
		t.Fatal("Compile accepted uppercase alias for private Java ASCII mode")
	}

	re := compileFull(t, `(?a-j:a)(?j-a:b)`, None)
	dump := re.code.Dump()
	if !strings.Contains(dump, "-CiASCII") || !strings.Contains(dump, "-CiJava") {
		t.Fatalf("opcode dump lacks distinct Java mode bits:\n%s", dump)
	}
}

func TestJavaCaseSearchAndRightToLeft(t *testing.T) {
	re := MustCompile(`(?j-a:k)`, None)
	matched, err := re.MatchString("prefix-K-suffix")
	if err != nil || !matched {
		t.Fatalf("unanchored Java Unicode search = %v, %v", matched, err)
	}

	for _, test := range []struct {
		option RegexOptions
		want   bool
	}{
		{RightToLeft | JavaASCII, false},
		{RightToLeft | JavaUnicode, true},
	} {
		re = compileFull(t, `k`, test.option)
		matched, err = re.MatchString("K")
		if err != nil || matched != test.want {
			t.Fatalf("right-to-left option %#x match = %v, %v; want %v", test.option, matched, err, test.want)
		}
	}
}

func TestJavaCaseLoneSurrogates(t *testing.T) {
	for _, test := range []struct {
		pattern string
		input   []rune
		want    bool
	}{
		{`(?a-j:\uD800)`, []rune{0xD800}, true},
		{`(?a-j:\uD800)`, []rune{0xD801}, false},
		{`(?j-a:\uD800)`, []rune{0xD800}, true},
		{`(?j-a:\uD800)`, []rune{0xD801}, false},
		{`(?j-a:(\uD800)\1)`, []rune{0xD800, 0xD800}, true},
		{`(?j-a:(\uD800)\1)`, []rune{0xD800, 0xD801}, false},
	} {
		re := compileFull(t, test.pattern, None)
		match, err := re.FindRunesMatch(test.input)
		if err != nil {
			t.Fatalf("FindRunesMatch(%q): %v", test.pattern, err)
		}
		if got := match != nil; got != test.want {
			t.Fatalf("FindRunesMatch(%q, %U) = %v, want %v", test.pattern, test.input, got, test.want)
		}
	}
}

func TestStockIgnoreCaseUnchanged(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "one", pattern: `(?i:\u00e9)`, input: "É", want: true},
		{name: "multi", pattern: `(?i:\u00e9cho)`, input: "ÉCHO", want: true},
		{name: "set", pattern: `(?i:[\u00e9])`, input: "É", want: true},
		{name: "negated_set", pattern: `(?i:[^\u00e9])`, input: "É", want: false},
		{name: "fixed_repetition", pattern: `(?i:\u00e9{2})`, input: "éÉ", want: true},
		{name: "greedy_set_repetition", pattern: `(?i:[\u00e9]+)`, input: "ÉéÉ", want: true},
		{name: "lazy_set_repetition", pattern: `(?i:[\u00e9]+?)`, input: "É", want: true},
		{name: "backreference", pattern: `(?i:(\u00e9)\1)`, input: "éÉ", want: true},
		{name: "legacy_long_s", pattern: `(?i:s)`, input: "ſ", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMatch(t, test.pattern, test.input, test.want)
		})
	}

	re := MustCompile(`(?i:\u00e9cho)`, None)
	matched, err := re.MatchString("prefix-ÉCHO-suffix")
	if err != nil || !matched {
		t.Fatalf("stock IgnoreCase unanchored prefix search = %v, %v", matched, err)
	}
}
