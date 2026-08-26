package opfor

import (
	"strings"
	"testing"
)

func TestSleepJavaRegexSingleLetterProperties(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "positive", pattern: `\pL`, input: "é", want: true},
		{name: "positive_negative_input", pattern: `\pL`, input: "1", want: false},
		{name: "complement", pattern: `\PL`, input: "1", want: true},
		{name: "followed_by_literal", pattern: `\pLL`, input: "AL", want: true},
		{name: "inside_class", pattern: `[\pL]`, input: "A", want: true},
		{name: "complement_inside_class", pattern: `[\PL]`, input: "1", want: true},
	}
	assertJavaRegexPropertyMatches(t, tests)

	for _, pattern := range []string{`\p`, `\P`, `\p{}`, `\P{}`, `\pQ`} {
		if _, err := compileSleepRegex(pattern, false); err == nil {
			t.Errorf("compileSleepRegex(%q) unexpectedly succeeded", pattern)
		}
	}
}

func TestSleepJavaRegexExactForPropertyAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "ld_letter", pattern: `\p{LD}`, input: "A", want: true},
		{name: "ld_decimal", pattern: `\p{LD}`, input: "١", want: true},
		{name: "ld_other_number", pattern: `\p{LD}`, input: "²", want: false},
		{name: "gc_ld", pattern: `\p{gc=LD}`, input: "９", want: true},
		{name: "latin1_upper_edge", pattern: `\p{L1}`, input: "ÿ", want: true},
		{name: "latin1_outside", pattern: `\p{L1}`, input: "Ā", want: false},
		{name: "gc_latin1", pattern: `\p{gc=L1}`, input: "\u0085", want: true},
		{name: "all_supplementary", pattern: `\p{all}`, input: "😀", want: true},
		{name: "all_line_terminator", pattern: `\p{general_category=all}`, input: "\n", want: true},
	}
	assertJavaRegexPropertyMatches(t, tests)
}

func TestSleepJavaRegexHexDigitMatchesOpenJDK(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "ascii_alias", pattern: `\p{IsHexDigit}`, input: "F", want: true},
		{name: "underscore_alias", pattern: `\p{IsHex_Digit}`, input: "f", want: true},
		{name: "unicode_posix_alias", pattern: `\p{IsXDigit}`, input: "١", want: true},
		{name: "unicode_posix_mode", pattern: `(?U)\p{XDigit}`, input: "Ａ", want: true},
		{name: "arabic_decimal", pattern: `\p{IsHexDigit}`, input: "١", want: true},
		{name: "mathematical_decimal", pattern: `\p{IsHexDigit}`, input: "𝟡", want: true},
		{name: "fullwidth_upper", pattern: `\p{IsHexDigit}`, input: "Ｆ", want: true},
		{name: "fullwidth_lower", pattern: `\p{IsHexDigit}`, input: "ｆ", want: true},
		{name: "fullwidth_non_hex", pattern: `\p{IsHexDigit}`, input: "Ｇ", want: false},
		{name: "other_number", pattern: `\p{IsHexDigit}`, input: "²", want: false},
		{name: "ascii_default_excludes_fullwidth", pattern: `\p{XDigit}`, input: "Ａ", want: false},
	}
	assertJavaRegexPropertyMatches(t, tests)

	if _, err := compileSleepRegex(`\p{IsHex-Digit}`, false); err == nil {
		t.Fatal("non-OpenJDK Hex_Digit spelling unexpectedly compiled")
	}
}

func TestSleepJavaRegexPropertySpellingFollowsPatternFamily(t *testing.T) {
	t.Parallel()
	accepted := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "category_exact", pattern: `\p{Lu}`, input: "A", want: true},
		{name: "general_category_key", pattern: `\p{General_Category=Lu}`, input: "A", want: true},
		{name: "script_case_insensitive", pattern: `\p{script=greek}`, input: "α", want: true},
		{name: "script_iso_alias", pattern: `\p{sc=Grek}`, input: "α", want: true},
		{name: "unicode_posix_case_insensitive", pattern: `(?U)\p{lower}`, input: "é", want: true},
		{name: "unicode_binary_case_insensitive", pattern: `\p{Iswhite_space}`, input: "\u2003", want: true},
		{name: "root_upper_expansion", pattern: `\p{IsAßigned}`, input: "A", want: true},
		{name: "root_upper_simple_mapping", pattern: `\p{IsWhıtespace}`, input: "\u2003", want: true},
		{name: "gc_delegates_to_for_property", pattern: `\p{gc=javaDigit}`, input: "١", want: true},
		{name: "gc_accepts_posix_property", pattern: `\p{gc=Lower}`, input: "a", want: true},
	}
	assertJavaRegexPropertyMatches(t, accepted)

	for _, pattern := range []string{
		`\p{lower}`,
		`\p{lu}`,
		`\p{Lowercase_Letter}`,
		`\p{lower-case_letter}`,
		`\p{javalowercase}`,
		`\p{java-Lower_Case}`,
		`\p{generalcategory=Lu}`,
		`\p{general-category=Lu}`,
		`\p{SCRİPT=Greek}`,
		`\p{gc=lu}`,
		`\p{sc=Old-Italic}`,
		`\p{IsWhite-Space}`,
		`\p{IsCased}`,
		`\p{IsOther_Alphabetic}`,
		`\p{IsJava_Whitespace}`,
		`\p{isLatin}`,
		`\p{LD }`,
	} {
		if _, err := compileSleepRegex(pattern, false); err == nil {
			t.Errorf("non-Pattern.family spelling %q unexpectedly compiled", pattern)
		}
	}
}

func TestJavaRegexPropertyMarkerExpansionIsSinglePass(t *testing.T) {
	t.Parallel()
	const markerCount = 2048
	markers := newJavaRegexPropertyMarkers("")
	var pattern, want strings.Builder
	for range markerCount {
		atom, err := markers.add(`[A]`)
		if err != nil {
			t.Fatal(err)
		}
		pattern.WriteString(atom)
		want.WriteString(`(?-aj:[A])`)
	}
	got, err := markers.expand(pattern.String())
	if err != nil {
		t.Fatal(err)
	}
	if got != want.String() {
		t.Fatalf("expanded marker pattern length = %d, want %d", len(got), want.Len())
	}
}

func TestJavaRegexPropertyMarkerIntegrityErrors(t *testing.T) {
	t.Parallel()
	markers := newJavaRegexPropertyMarkers("")
	atom, err := markers.add(`[A]`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markers.expand(""); err == nil || !strings.Contains(err.Error(), "lost") {
		t.Fatalf("lost-marker error = %v", err)
	}
	if _, err := markers.expand(atom + atom); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate-marker error = %v", err)
	}
}

func TestJavaRegexPropertyMarkerExhaustion(t *testing.T) {
	var source strings.Builder
	for _, current := range javaRegexPrivateUseRanges {
		for character := current.first; character <= current.last; character++ {
			source.WriteRune(character)
		}
	}
	markers := newJavaRegexPropertyMarkers(source.String())
	if _, err := markers.add(`[A]`); err == nil || !strings.Contains(err.Error(), "exhausts") {
		t.Fatalf("private-use exhaustion error = %v", err)
	}
}

func TestJavaRegexTranslatedPatternLimit(t *testing.T) {
	t.Parallel()
	t.Run("unexpanded", func(t *testing.T) {
		markers := newJavaRegexPropertyMarkers("")
		if _, err := markers.expand(strings.Repeat("x", javaRegexTranslatedPatternLimit+1)); err == nil ||
			!strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("oversize pattern error = %v", err)
		}
	})
	t.Run("property_expansion", func(t *testing.T) {
		const marker = '\ue000'
		atom := "[" + string(marker) + "]"
		markers := &javaRegexPropertyMarkers{markers: []javaRegexPropertyMarker{{
			character:   marker,
			atom:        atom,
			replacement: strings.Repeat("x", javaRegexTranslatedPatternLimit+1),
		}}}
		if _, err := markers.expand(atom); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("oversize expansion error = %v", err)
		}
	})
	t.Run("property_budget", func(t *testing.T) {
		markers := newJavaRegexPropertyMarkers("")
		if _, err := markers.add(strings.Repeat("x", javaRegexTranslatedPatternLimit)); err == nil ||
			!strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("oversize property budget error = %v", err)
		}
	})
	t.Run("compile_path", func(t *testing.T) {
		fragment, ok := javaRegexJavaProperty("javaDefined", false)
		if !ok {
			t.Fatal("javaDefined property is unavailable")
		}
		replacementSize := len(fragment) + len(`(?-aj:)`)
		pattern := strings.Repeat(`\p{javaDefined}`, javaRegexTranslatedPatternLimit/replacementSize+1)
		if _, err := compileSleepRegex(pattern, false); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("compileSleepRegex oversize expansion error = %v", err)
		}
	})
	t.Run("possessive_translation", func(t *testing.T) {
		pattern := strings.Repeat(`a++`, javaRegexTranslatedPatternLimit/4)
		if _, err := compileSleepRegex(pattern, false); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("compileSleepRegex oversize possessive error = %v", err)
		}
	})
	t.Run("whole_match_wrapper", func(t *testing.T) {
		pattern := strings.Repeat("a", javaRegexTranslatedPatternLimit-7)
		if _, err := compileSleepRegex(pattern, true); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("compileSleepRegex oversize whole-match error = %v", err)
		}
	})
}

func assertJavaRegexPropertyMatches(t *testing.T, tests []struct {
	name    string
	pattern string
	input   string
	want    bool
}) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expression, err := compileSleepRegex(test.pattern, true)
			if err != nil {
				t.Fatalf("compileSleepRegex(%q): %v", test.pattern, err)
			}
			indices, err := expression.FindStringSubmatchIndex(test.input)
			if err != nil {
				t.Fatalf("match %q against %q: %v", test.pattern, test.input, err)
			}
			if got := indices != nil; got != test.want {
				t.Fatalf("match %q against %q = %t, want %t", test.pattern, test.input, got, test.want)
			}
		})
	}
}
