package opfor

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"
	"testing"
)

func TestSleepJavaRegexCaseFoldingModes(t *testing.T) {
	loneSurrogate := sleepCanonicalString(sleepUTF16CharacterValue(0xd800))
	const (
		lowerE       = "é"
		upperE       = "É"
		longS        = "ſ"
		dotlessI     = "ı"
		finalSigma   = "ς"
		deseretUpper = "𐐀"
		deseretLower = "𐐨"
	)

	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		// CASE_INSENSITIVE without UNICODE_CASE retains Java's ASCII
		// folding, including classes, POSIX properties, and backreferences.
		{name: "ascii_literal_global_i", pattern: `(?i)a`, input: "A", want: true},
		{name: "ascii_literal_scoped_i", pattern: `(?i:a)`, input: "A", want: true},
		{name: "ascii_class_global_i", pattern: `(?i)[a]`, input: "A", want: true},
		{name: "ascii_posix_property_global_i", pattern: `(?i)\p{Lower}`, input: "A", want: true},
		{name: "ascii_backreference_global_i", pattern: `(?i)(a)\1`, input: "aA", want: true},

		// Bare global and scoped i must not inherit regexp2's Unicode-wide
		// literal, class, POSIX-property, quoted-literal, or backreference folding.
		{name: "non_ascii_literal_global_i", pattern: `(?i)` + lowerE, input: upperE, want: false},
		{name: "non_ascii_literal_scoped_i", pattern: `(?i:` + lowerE + `)`, input: upperE, want: false},
		{name: "non_ascii_class_global_i", pattern: `(?i)[` + lowerE + `]`, input: upperE, want: false},
		{name: "non_ascii_class_scoped_i", pattern: `(?i:[` + lowerE + `])`, input: upperE, want: false},
		{name: "non_ascii_property_global_i", pattern: `(?i)\p{Lower}`, input: longS, want: false},
		{name: "non_ascii_property_scoped_i", pattern: `(?i:\p{Lower})`, input: longS, want: false},
		{name: "non_ascii_quoted_literal_global_i", pattern: `(?i)\Q` + lowerE + `\E`, input: upperE, want: false},
		{name: "non_ascii_unterminated_quote_global_i", pattern: `(?i)\Q` + lowerE, input: upperE, want: false},
		{name: "non_ascii_backreference_global_i", pattern: `(?i)(` + lowerE + `)\1`, input: lowerE + upperE, want: false},
		{name: "non_ascii_backreference_scoped_i", pattern: `(?i:(` + lowerE + `)\1)`, input: lowerE + upperE, want: false},

		// u can be combined with i or enabled before/after it. U implies u,
		// but U alone deliberately does not imply i.
		{name: "unicode_literal_iu", pattern: `(?iu)` + lowerE, input: upperE, want: true},
		{name: "unicode_literal_u_then_i", pattern: `(?u)(?i)` + lowerE, input: upperE, want: true},
		{name: "unicode_literal_i_then_u", pattern: `(?i)(?u)` + lowerE, input: upperE, want: true},
		{name: "unicode_literal_outer_u_scoped_i", pattern: `(?u)(?i:` + lowerE + `)`, input: upperE, want: true},
		{name: "unicode_literal_iU", pattern: `(?iU)` + lowerE, input: upperE, want: true},
		{name: "unicode_literal_U_without_i", pattern: `(?U)` + lowerE, input: upperE, want: false},
		{name: "unicode_class_iu", pattern: `(?iu)[` + lowerE + `]`, input: upperE, want: true},
		{name: "unicode_posix_property_iU", pattern: `(?iU)\p{Lower}`, input: upperE, want: true},
		{name: "unicode_quoted_literal_iu", pattern: `(?iu)\Q` + lowerE + `\E`, input: upperE, want: true},
		{name: "unicode_unterminated_quote_iu", pattern: `(?iu)\Q` + lowerE, input: upperE, want: true},
		{name: "unicode_backreference_iu", pattern: `(?iu)(` + lowerE + `)\1`, input: lowerE + upperE, want: true},
		{name: "unicode_long_s_iu", pattern: `(?iu)s`, input: longS, want: true},
		{name: "unicode_kelvin_iu", pattern: `(?iu)k`, input: "K", want: true},
		{name: "unicode_ascii_range_long_s_iu", pattern: `(?iu)[a-z]`, input: longS, want: true},
		{name: "unicode_ascii_range_kelvin_iu", pattern: `(?iu)[a-z]`, input: "K", want: true},
		{name: "unicode_ascii_range_dotless_i_iu", pattern: `(?iu)[a-z]`, input: dotlessI, want: true},
		{name: "unicode_final_sigma_iu", pattern: `(?iu)σ`, input: finalSigma, want: true},
		{name: "unicode_category_lower_i", pattern: `(?i)\p{Ll}`, input: upperE, want: true},
		{name: "unicode_category_upper_i", pattern: `(?i)\p{Lu}`, input: lowerE, want: true},
		{name: "unicode_negative_category_lower_i", pattern: `(?i)\P{Ll}`, input: upperE, want: false},
		{name: "unicode_negative_category_lower_i_nonletter", pattern: `(?i)\P{Ll}`, input: "1", want: true},
		// Java retains the provenance of a POSIX property token: u does not
		// turn ASCII \p{Lower} into a foldable [a-z] range. U changes the
		// property definition itself, as covered by unicode_posix_property_iU.
		{name: "unicode_posix_property_iu_does_not_fold_kelvin", pattern: `(?iu)\p{Lower}`, input: "K", want: false},
		{name: "unicode_ascii_property_iu_does_not_fold_kelvin", pattern: `(?iu)\p{ASCII}`, input: "K", want: false},
		{name: "unicode_block_iu_does_not_fold_kelvin", pattern: `(?iu)\p{InBasicLatin}`, input: "K", want: false},
		{name: "unicode_property_class_intersection_keeps_provenance", pattern: `(?iu)[\p{ASCII}&&[k]]`, input: "K", want: false},
		{name: "unicode_default_word_class_keeps_provenance", pattern: `(?iu)\w`, input: "K", want: false},
		{name: "unicode_U_word_class_contains_kelvin", pattern: `(?iU)\w`, input: "K", want: true},

		// Scoped disabling must affect only its group, then restore the outer
		// Unicode-aware case mode. Disabling U also disables the u it implied.
		{name: "scoped_disable_i", pattern: `(?iu)` + lowerE + `(?-i:` + lowerE + `)` + lowerE, input: upperE + lowerE + upperE, want: true},
		{name: "scoped_disable_i_rejects_upper", pattern: `(?iu)` + lowerE + `(?-i:` + lowerE + `)` + lowerE, input: upperE + upperE + upperE, want: false},
		{name: "scoped_disable_u", pattern: `(?iu)` + lowerE + `(?-u:` + lowerE + `)` + lowerE, input: upperE + lowerE + upperE, want: true},
		{name: "scoped_disable_u_rejects_upper", pattern: `(?iu)` + lowerE + `(?-u:` + lowerE + `)` + lowerE, input: upperE + upperE + upperE, want: false},
		{name: "scoped_disable_u_preserves_U_word_class", pattern: `(?iU)(?-u:\w)`, input: upperE, want: true},
		{name: "scoped_disable_U", pattern: `(?iU)` + lowerE + `(?-U:` + lowerE + `)` + lowerE, input: upperE + lowerE + upperE, want: true},
		{name: "scoped_disable_U_rejects_upper", pattern: `(?iU)` + lowerE + `(?-U:` + lowerE + `)` + lowerE, input: upperE + upperE + upperE, want: false},
		{name: "scoped_disable_U_removes_unicode_word_class", pattern: `(?iU)(?-U:\w)`, input: upperE, want: false},
		{name: "scoped_iu_does_not_leak", pattern: `(?iu:` + lowerE + `)` + lowerE, input: upperE + upperE, want: false},
		{name: "scoped_iu_restores_parent", pattern: `(?iu:` + lowerE + `)` + lowerE, input: upperE + lowerE, want: true},

		// Deseret case pairs exercise supplementary code points represented by
		// two Java UTF-16 code units. Lone surrogates remain matchable as one
		// canonical Sleep string element under either case mode.
		{name: "supplementary_literal_i", pattern: `(?i)` + deseretLower, input: deseretUpper, want: false},
		{name: "supplementary_literal_iu", pattern: `(?iu)` + deseretLower, input: deseretUpper, want: true},
		{name: "supplementary_class_i", pattern: `(?i)[` + deseretLower + `]`, input: deseretUpper, want: false},
		{name: "supplementary_class_iu", pattern: `(?iu)[` + deseretLower + `]`, input: deseretUpper, want: true},
		{name: "supplementary_backreference_i", pattern: `(?i)(` + deseretLower + `)\1`, input: deseretLower + deseretUpper, want: false},
		{name: "supplementary_backreference_iu", pattern: `(?iu)(` + deseretLower + `)\1`, input: deseretLower + deseretUpper, want: true},
		{name: "lone_surrogate_i", pattern: `(?i)` + loneSurrogate, input: loneSurrogate, want: true},
		{name: "lone_surrogate_iu", pattern: `(?iu)` + loneSurrogate, input: loneSurrogate, want: true},
	}

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

func TestSleepJavaRegexCaseModeTranslation(t *testing.T) {
	casedCategory := javaRegexCasedCategoryProperty()
	casedBinary := javaRegexCasedBinaryProperty()
	tests := []struct {
		pattern string
		want    string
	}{
		{pattern: `(?i)a`, want: `(?a-j)a`},
		{pattern: `(?iu)a`, want: `(?j-a)a`},
		{pattern: `(?u)(?i)a`, want: `(?j-a)a`},
		{pattern: `(?i)(?u)a`, want: `(?a-j)(?j-a)a`},
		{pattern: `(?U)(?i:a)`, want: `(?j-a:a)`},
		{pattern: `(?iU)a(?-i:b)c`, want: `(?j-a)a(?-aj:b)c`},
		{pattern: `(?iU)a(?-u:b)c`, want: `(?j-a)a(?a-j:b)c`},
		{pattern: `(?iU)a(?-U:b)c`, want: `(?j-a)a(?a-j:b)c`},
		{pattern: `(?iu)\p{Lower}`, want: `(?j-a)(?-aj:[A-Za-z])`},
		{pattern: `(?i)\p{Ll}`, want: `(?a-j)(?-aj:` + casedCategory + `)`},
		{pattern: `(?i)\P{Ll}`, want: `(?a-j)(?-aj:` + negateJavaRegexClass(casedCategory) + `)`},
		{pattern: `(?i)\p{Lt}`, want: `(?a-j)(?-aj:` + casedCategory + `)`},
		{pattern: `(?i)\p{IsLowercase}`, want: `(?a-j)(?-aj:` + casedBinary + `)`},
		{pattern: `(?i)\p{javaTitleCase}`, want: `(?a-j)(?-aj:` + casedBinary + `)`},
		{pattern: `(?iU)\p{Lower}`, want: `(?j-a)(?-aj:` + casedBinary + `)`},
		{pattern: `(?iu)\p{InBasicLatin}`, want: `(?j-a)(?-aj:[\x{0}-\x{7f}])`},
		{pattern: `(?iu)\w`, want: `(?j-a)(?-aj:[A-Za-z0-9_])`},
		{
			pattern: `(?iu)[\p{ASCII}&&[k]]`,
			want:    `(?j-a)(?:(?=(?-aj:[\x00-\x7f]))(?=[k])[\s\S])`,
		},
		{pattern: `(?i)\Qé`, want: `(?a-j)é`},
		{pattern: `(?iu)\Qé`, want: `(?j-a)é`},
	}
	for _, test := range tests {
		t.Run(test.pattern, func(t *testing.T) {
			got, err := translateSleepRegex(test.pattern)
			if err != nil {
				t.Fatalf("translateSleepRegex(%q): %v", test.pattern, err)
			}
			if got != test.want {
				t.Fatalf("translateSleepRegex(%q) = %q, want %q", test.pattern, got, test.want)
			}
		})
	}
}

func TestSleepJavaRegexUnicodeFlagTransitions(t *testing.T) {
	enableU := javaRegexInlineFlags{on: "U"}
	state := applyJavaRegexInlineFlags(javaRegexModes{}, enableU)
	if !state.unicodeClass || !state.unicodeCase {
		t.Fatalf("U state = %+v, want both unicodeClass and unicodeCase", state)
	}

	disableu := javaRegexInlineFlags{off: "u"}
	withoutCase := applyJavaRegexInlineFlags(state, disableu)
	if !withoutCase.unicodeClass || withoutCase.unicodeCase {
		t.Fatalf("U then -u state = %+v, want U retained and u cleared", withoutCase)
	}

	disableU := javaRegexInlineFlags{off: "U"}
	withoutClass := applyJavaRegexInlineFlags(state, disableU)
	if withoutClass.unicodeClass || withoutClass.unicodeCase {
		t.Fatalf("U then -U state = %+v, want both U and implied u cleared", withoutClass)
	}
}

func TestSleepJavaRegexRejectsPrivateCaseFlags(t *testing.T) {
	for _, pattern := range []string{
		`(?a)x`,
		`(?A)x`,
		`(?j:x)`,
		`(?J:x)`,
		`(?i-a:x)`,
		`(?ij)x`,
		`(?-j)x`,
	} {
		if _, err := translateSleepRegex(pattern); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("translateSleepRegex(%q) error = %v, want reserved-flag rejection", pattern, err)
		}
	}
	for _, pattern := range []string{`a`, `[aj]`, `\Q(?a)\E`, "(?x)# (?j)\na"} {
		if _, err := translateSleepRegex(pattern); err != nil {
			t.Errorf("translateSleepRegex(%q): %v", pattern, err)
		}
	}
}

func TestSleepJavaRegexPropertyMarkersAvoidSourceCharacters(t *testing.T) {
	const occupied = '\ue000'
	translated, markers, err := translateJavaRegexModes(string(occupied) + `\p{ASCII}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(markers.markers) != 1 {
		t.Fatalf("property markers = %d, want 1", len(markers.markers))
	}
	if got, want := markers.markers[0].atom, "["+string(occupied+1)+"]"; got != want {
		t.Fatalf("property marker = %q, want first absent private-use atom %q", got, want)
	}
	classes, err := translateJavaRegexClasses(translated)
	if err != nil {
		t.Fatal(err)
	}
	expanded, err := markers.expand(classes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.ContainsRune(expanded, occupied) || strings.ContainsRune(expanded, occupied+1) {
		t.Fatalf("expanded pattern leaked or replaced a marker: %q", expanded)
	}
}

// The probe uses RegexBridge's Sleep-facing operators and functions so the
// engine tests above cannot pass while replacement, split, or UTF-16 plumbing
// still applies the wrong case mode.
const sleepJavaRegexCaseProbeSource = `
sub u { return unpack("U", pack("H*", $1 . "0000"))[0]; }
sub wm { if ($2 ismatch $3) { println($1 . ":1"); } else { println($1 . ":0"); } }
sub same { if ($2 eq $3) { println($1 . ":1"); } else { println($1 . ":0"); } }

$lower = u("00e9");
$upper = u("00c9");
$longs = u("017f");
$kelvin = u("212a");
$dotless_i = u("0131");
$final_sigma = u("03c2");
$deseret_upper = u("d801dc00");
$deseret_lower = u("d801dc28");
$lone = chr(0xD800);

wm("ascii-literal-global-i", "A", '(?i)a');
wm("ascii-literal-scoped-i", "A", '(?i:a)');
wm("ascii-class-i", "A", '(?i)[a]');
wm("ascii-property-i", "A", '(?i)\p{Lower}');
wm("ascii-backreference-i", "aA", '(?i)(a)\1');

wm("literal-global-i", $upper, '(?i)' . $lower);
wm("literal-scoped-i", $upper, '(?i:' . $lower . ')');
wm("class-global-i", $upper, '(?i)[' . $lower . ']');
wm("class-scoped-i", $upper, '(?i:[' . $lower . '])');
wm("property-global-i", $longs, '(?i)\p{Lower}');
wm("property-scoped-i", $longs, '(?i:\p{Lower})');
wm("quoted-global-i", $upper, '(?i)\Q' . $lower . '\E');
wm("quoted-eof-global-i", $upper, '(?i)\Q' . $lower);
wm("backreference-global-i", $lower . $upper, '(?i)(' . $lower . ')\1');
wm("backreference-scoped-i", $lower . $upper, '(?i:(' . $lower . ')\1)');

wm("literal-iu", $upper, '(?iu)' . $lower);
wm("literal-u-plus-i", $upper, '(?u)(?i)' . $lower);
wm("literal-i-plus-u", $upper, '(?i)(?u)' . $lower);
wm("literal-u-scoped-i", $upper, '(?u)(?i:' . $lower . ')');
wm("literal-iU", $upper, '(?iU)' . $lower);
wm("literal-U-alone", $upper, '(?U)' . $lower);
wm("class-iu", $upper, '(?iu)[' . $lower . ']');
wm("property-iU", $upper, '(?iU)\p{Lower}');
wm("quoted-iu", $upper, '(?iu)\Q' . $lower . '\E');
wm("quoted-eof-iu", $upper, '(?iu)\Q' . $lower);
wm("backreference-iu", $lower . $upper, '(?iu)(' . $lower . ')\1');
wm("long-s-iu", $longs, '(?iu)s');
wm("kelvin-iu", $kelvin, '(?iu)k');
wm("range-long-s-iu", $longs, '(?iu)[a-z]');
wm("range-kelvin-iu", $kelvin, '(?iu)[a-z]');
wm("range-dotless-i-iu", $dotless_i, '(?iu)[a-z]');
wm("final-sigma-iu", $final_sigma, '(?iu)σ');
wm("property-kelvin-iu", $kelvin, '(?iu)\p{Lower}');
wm("category-lower-i", $upper, '(?i)\p{Ll}');
wm("ascii-property-kelvin-iu", $kelvin, '(?iu)\p{ASCII}');
wm("block-kelvin-iu", $kelvin, '(?iu)\p{InBasicLatin}');
wm("property-intersection-kelvin-iu", $kelvin, '(?iu)[\p{ASCII}&&[k]]');
wm("word-kelvin-iu", $kelvin, '(?iu)\w');
wm("word-kelvin-iU", $kelvin, '(?iU)\w');

wm("disable-i-pass", $upper . $lower . $upper, '(?iu)' . $lower . '(?-i:' . $lower . ')' . $lower);
wm("disable-i-reject", $upper . $upper . $upper, '(?iu)' . $lower . '(?-i:' . $lower . ')' . $lower);
wm("disable-u-pass", $upper . $lower . $upper, '(?iu)' . $lower . '(?-u:' . $lower . ')' . $lower);
wm("disable-u-reject", $upper . $upper . $upper, '(?iu)' . $lower . '(?-u:' . $lower . ')' . $lower);
wm("disable-u-keeps-U-word", $upper, '(?iU)(?-u:\w)');
wm("disable-U-pass", $upper . $lower . $upper, '(?iU)' . $lower . '(?-U:' . $lower . ')' . $lower);
wm("disable-U-reject", $upper . $upper . $upper, '(?iU)' . $lower . '(?-U:' . $lower . ')' . $lower);
wm("disable-U-removes-word", $upper, '(?iU)(?-U:\w)');
wm("scoped-no-leak-pass", $upper . $lower, '(?iu:' . $lower . ')' . $lower);
wm("scoped-no-leak-reject", $upper . $upper, '(?iu:' . $lower . ')' . $lower);

same("replace-bare-i", replace($upper . $lower, '(?i)' . $lower, "x"), $upper . "x");
same("replace-unicode-i", replace($upper . $lower, '(?iu)' . $lower, "x"), "xx");
@bare = split('(?i)' . $lower, "x" . $upper . "y" . $lower . "z", -1);
if ((size(@bare) == 2) && (@bare[0] eq "x" . $upper . "y") && (@bare[1] eq "z")) {
    println("split-bare-i:1");
} else { println("split-bare-i:0"); }
@unicode = split('(?iu)' . $lower, "x" . $upper . "y" . $lower . "z", -1);
if ((size(@unicode) == 3) && (@unicode[0] eq "x") && (@unicode[1] eq "y") && (@unicode[2] eq "z")) {
    println("split-unicode-i:1");
} else { println("split-unicode-i:0"); }

if (strlen($deseret_lower) == 2) { println("supplementary-utf16-length:1"); }
else { println("supplementary-utf16-length:0"); }
wm("supplementary-literal-i", $deseret_upper, '(?i)' . $deseret_lower);
wm("supplementary-literal-iu", $deseret_upper, '(?iu)' . $deseret_lower);
wm("supplementary-class-i", $deseret_upper, '(?i)[' . $deseret_lower . ']');
wm("supplementary-class-iu", $deseret_upper, '(?iu)[' . $deseret_lower . ']');
wm("supplementary-backreference-i", $deseret_lower . $deseret_upper, '(?i)(' . $deseret_lower . ')\1');
wm("supplementary-backreference-iu", $deseret_lower . $deseret_upper, '(?iu)(' . $deseret_lower . ')\1');
wm("lone-surrogate-i", $lone, '(?i)' . $lone);
wm("lone-surrogate-iu", $lone, '(?iu)' . $lone);
`

const sleepJavaRegexCaseProbeOutput = "ascii-literal-global-i:1\n" +
	"ascii-literal-scoped-i:1\n" +
	"ascii-class-i:1\n" +
	"ascii-property-i:1\n" +
	"ascii-backreference-i:1\n" +
	"literal-global-i:0\n" +
	"literal-scoped-i:0\n" +
	"class-global-i:0\n" +
	"class-scoped-i:0\n" +
	"property-global-i:0\n" +
	"property-scoped-i:0\n" +
	"quoted-global-i:0\n" +
	"quoted-eof-global-i:0\n" +
	"backreference-global-i:0\n" +
	"backreference-scoped-i:0\n" +
	"literal-iu:1\n" +
	"literal-u-plus-i:1\n" +
	"literal-i-plus-u:1\n" +
	"literal-u-scoped-i:1\n" +
	"literal-iU:1\n" +
	"literal-U-alone:0\n" +
	"class-iu:1\n" +
	"property-iU:1\n" +
	"quoted-iu:1\n" +
	"quoted-eof-iu:1\n" +
	"backreference-iu:1\n" +
	"long-s-iu:1\n" +
	"kelvin-iu:1\n" +
	"range-long-s-iu:1\n" +
	"range-kelvin-iu:1\n" +
	"range-dotless-i-iu:1\n" +
	"final-sigma-iu:1\n" +
	"property-kelvin-iu:0\n" +
	"category-lower-i:1\n" +
	"ascii-property-kelvin-iu:0\n" +
	"block-kelvin-iu:0\n" +
	"property-intersection-kelvin-iu:0\n" +
	"word-kelvin-iu:0\n" +
	"word-kelvin-iU:1\n" +
	"disable-i-pass:1\n" +
	"disable-i-reject:0\n" +
	"disable-u-pass:1\n" +
	"disable-u-reject:0\n" +
	"disable-u-keeps-U-word:1\n" +
	"disable-U-pass:1\n" +
	"disable-U-reject:0\n" +
	"disable-U-removes-word:0\n" +
	"scoped-no-leak-pass:1\n" +
	"scoped-no-leak-reject:0\n" +
	"replace-bare-i:1\n" +
	"replace-unicode-i:1\n" +
	"split-bare-i:1\n" +
	"split-unicode-i:1\n" +
	"supplementary-utf16-length:1\n" +
	"supplementary-literal-i:0\n" +
	"supplementary-literal-iu:1\n" +
	"supplementary-class-i:0\n" +
	"supplementary-class-iu:1\n" +
	"supplementary-backreference-i:0\n" +
	"supplementary-backreference-iu:1\n" +
	"lone-surrogate-i:1\n" +
	"lone-surrogate-iu:1\n"

func TestSleepJavaRegexCaseFoldingEntryPoints(t *testing.T) {
	got := runPureGoJavaRegexProbe(t, sleepJavaRegexCaseProbeSource)
	if !bytes.Equal(got, []byte(sleepJavaRegexCaseProbeOutput)) {
		t.Fatalf("Java regex case-folding probe mismatch\nwant:\n%sgot:\n%s", sleepJavaRegexCaseProbeOutput, got)
	}
}

// TestSleepJavaRegexCaseFoldingOfficialJARDifferential is opt-in because the
// official BSD Sleep JAR is supplied separately. Pinning the oracle hash keeps
// ordinary test runs pure Go and prevents a different JAR from silently
// defining the expected behavior.
func TestSleepJavaRegexCaseFoldingOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for case-folding differential verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	javaOutput, err := osexec.Command(java, "-jar", jar, "-e", sleepJavaRegexCaseProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep regex case-folding probe: %v\n%s", err, javaOutput)
	}
	if !bytes.Equal(javaOutput, []byte(sleepJavaRegexCaseProbeOutput)) {
		t.Fatalf("official Sleep regex case-folding oracle changed\nwant:\n%sgot:\n%s", sleepJavaRegexCaseProbeOutput, javaOutput)
	}
	goOutput := runPureGoJavaRegexProbe(t, sleepJavaRegexCaseProbeSource)
	if !bytes.Equal(goOutput, javaOutput) {
		t.Fatalf("official Sleep regex case-folding output mismatch\nwant:\n%sgot:\n%s", javaOutput, goOutput)
	}
}
