package opfor

import (
	"bytes"
	"reflect"
	"testing"
)

const sleepJavaGraphemeProbeSource = `
$text = "a" . chr(0x0301) . "b";
@clusters = matches($text, '(\X)');
println("clusters:" . size(@clusters) . ":" . strlen(@clusters[0]) . ":" . strlen(@clusters[1]));
@parts = split('\b{g}', $text, -1);
println("split:" . size(@parts) . ":" . strlen(@parts[0]) . ":" . strlen(@parts[1]) . ":" . strlen(@parts[2]));
println("replace:" . replace($text, '\X', "x"));
println("find:" . find($text, '\b{g}', 1));
`

const sleepJavaGraphemeProbeOutput = "clusters:2:2:1\n" +
	"split:3:2:1:0\n" +
	"replace:xx\n" +
	"find:2\n"

func TestSleepJavaRegexGraphemeEntryPoints(t *testing.T) {
	got := runPureGoJavaRegexProbe(t, sleepJavaGraphemeProbeSource)
	if !bytes.Equal(got, []byte(sleepJavaGraphemeProbeOutput)) {
		t.Fatalf("Java grapheme probe mismatch\nwant:\n%sgot:\n%s", sleepJavaGraphemeProbeOutput, got)
	}
}

func TestSleepJavaRegexExtendedGraphemeMatcher(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "ascii", input: "ab", want: []string{"a", "b"}},
		{name: "crlf", input: "\r\n", want: []string{"\r\n"}},
		{name: "combining", input: "a\u0308b", want: []string{"a\u0308", "b"}},
		{name: "hangul", input: "\u1100\u1161\u11a8", want: []string{"\u1100\u1161\u11a8"}},
		{name: "regional_indicators", input: "🇺🇸🇨", want: []string{"🇺🇸", "🇨"}},
		{name: "emoji_zwj", input: "👩\u200d💻", want: []string{"👩\u200d💻"}},
		{name: "openjdk_prepend_then_emoji_zwj", input: "\u0600👩\u200d💻", want: []string{"\u0600👩\u200d", "💻"}},
		{name: "indic_conjunct", input: "क्ष", want: []string{"क्ष"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			expression, err := compileSleepRegex(`\X`, false)
			if err != nil {
				t.Fatal(err)
			}
			indices, err := expression.FindAllStringSubmatchIndex(test.input, -1)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, len(indices))
			for index, match := range indices {
				got[index] = test.input[match[0]:match[1]]
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("matches = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSleepJavaRegexGraphemeBoundary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []int
	}{
		{name: "combining", input: "a\u0301b", want: []int{0, 3, 4}},
		{name: "regional_indicators", input: "🇺🇸🇨", want: []int{0, 8, 12}},
		{name: "emoji_zwj", input: "👩\u200d💻x", want: []int{0, 11, 12}},
		{name: "indic_conjunct", input: "क्षx", want: []int{0, 9, 10}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			expression, err := compileSleepRegex(`\b{g}`, false)
			if err != nil {
				t.Fatal(err)
			}
			matches, err := expression.FindAllStringSubmatchIndex(test.input, -1)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]int, len(matches))
			for index, match := range matches {
				got[index] = match[0]
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("boundaries = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSleepJavaRegexGraphemeBoundaryRetainsMatcherLast(t *testing.T) {
	t.Parallel()
	expression, err := compileSleepRegex(`(\b{g}.)`, false)
	if err != nil {
		t.Fatal(err)
	}
	input := "🇺🇸🇨"
	matches, err := expression.FindAllStringSubmatchIndex(input, -1)
	if err != nil {
		t.Fatal(err)
	}
	// The first match ends inside the initial RI pair. OpenJDK seeds the next
	// grapheme-boundary search from Matcher.last, so the two remaining RIs are
	// paired relative to that offset and no second match is available.
	if want := [][]int{{0, 4, 0, 4}}; !reflect.DeepEqual(matches, want) {
		t.Fatalf("stateful grapheme matches = %v, want %v", matches, want)
	}

	// find(start) resets Matcher.last to zero before starting its search.
	match, err := expression.FindStringSubmatchIndexAt(input, 4)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{8, 12, 8, 12}; !reflect.DeepEqual(match, want) {
		t.Fatalf("reset grapheme match = %v, want %v", match, want)
	}
}

func TestSleepJavaRegexGraphemeIsAtomicAndContextual(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		{pattern: `\A\X{2}\z`, input: "a\u0301b", want: true},
		{pattern: `\A\X{3}\z`, input: "a\u0301b", want: false},
		// The dot consumes the first RI. X starts at the second and follows
		// OpenJDK by pairing from that actual node offset.
		{pattern: `\A.\X\z`, input: "🇺🇸🇨", want: true},
		{pattern: `\Aa\b{g}\u0301`, input: "a\u0301", want: false},
		{pattern: `\Aa\u0301\b{g}\z`, input: "a\u0301", want: true},
		{pattern: `\A\X(?<=\X)b\z`, input: "a\u0301b", want: true},
		{pattern: `\A.(?<=\X)\u0301\z`, input: "a\u0301", want: false},
	}
	for _, test := range tests {
		expression, err := compileSleepRegex(test.pattern, true)
		if err != nil {
			t.Fatalf("compile %q: %v", test.pattern, err)
		}
		match, err := expression.FindStringSubmatchIndex(test.input)
		if err != nil {
			t.Fatalf("match %q: %v", test.pattern, err)
		}
		if got := match != nil; got != test.want {
			t.Errorf("%q on %q = %v, want %v", test.pattern, test.input, got, test.want)
		}
	}
}

func TestSleepJavaRegexGraphemeLoneSurrogate(t *testing.T) {
	t.Parallel()
	input := sleepCanonicalString(sleepUTF16CharacterValue(0xd800)) + "a"
	expression, err := compileSleepRegex(`\X`, false)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := expression.FindAllStringSubmatchIndex(input, -1)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]int{{0, 3}, {3, 4}}; !reflect.DeepEqual(matches, want) {
		t.Fatalf("lone-surrogate matches = %v, want %v", matches, want)
	}
}

func TestSleepJavaRegexGraphemeSyntaxErrors(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{`[\X]`, `\b{g`, `\b{gX}`} {
		if _, err := compileSleepRegex(pattern, false); err == nil {
			t.Errorf("compileSleepRegex(%q) unexpectedly succeeded", pattern)
		}
	}
}

func TestSleepJavaRegexQuotedGraphemeSpellingIsLiteral(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		pattern string
		input   string
	}{
		{pattern: `\Q\X\E`, input: `\X`},
		{pattern: `\Q\b{g}\E`, input: `\b{g}`},
	} {
		expression, err := compileSleepRegex(test.pattern, true)
		if err != nil {
			t.Fatalf("compileSleepRegex(%q): %v", test.pattern, err)
		}
		match, err := expression.FindStringSubmatchIndex(test.input)
		if err != nil || match == nil {
			t.Errorf("literal %q with %q: match=%v error=%v", test.input, test.pattern, match, err)
		}
	}
}
