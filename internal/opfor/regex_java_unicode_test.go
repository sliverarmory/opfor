package opfor

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestSleepJavaRegexNamedCharacters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "unicode_data_name", pattern: `\N{LATIN SMALL LETTER A}`, input: "a", want: true},
		{name: "case_insensitive_name", pattern: `\N{white smiling face}`, input: "☺", want: true},
		{name: "locale_root_full_upper_name", pattern: `\N{LEß-THAN SIGN}`, input: "<", want: true},
		{name: "java_trim", pattern: "\\N{  LATIN SMALL LETTER A\t}", input: "a", want: true},
		{name: "control_old_name", pattern: `\N{LINE FEED (LF)}`, input: "\n", want: true},
		{name: "openjdk_bel_special_case", pattern: `\N{BEL}`, input: "\a", want: true},
		{name: "bell_is_emoji_name", pattern: `\N{BELL}`, input: "🔔", want: true},
		{name: "supplementary_name", pattern: `\N{GRINNING FACE}`, input: "😀", want: true},
		{name: "inside_class", pattern: `[\N{LATIN SMALL LETTER A}]`, input: "a", want: true},
		{name: "unicode_case_mode_applies", pattern: `(?iu)\N{LATIN SMALL LETTER A}`, input: "A", want: true},
		{name: "hangul_block_hex_fallback", pattern: `\N{HANGUL SYLLABLES AC00}`, input: "가", want: true},
		{name: "private_use_block_hex_fallback", pattern: `\N{PRIVATE USE AREA E000}`, input: "\ue000", want: true},
		{name: "wrong_character", pattern: `\N{LATIN SMALL LETTER A}`, input: "b", want: false},
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

func TestSleepJavaRegexNamedCharacterErrors(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{
		`\N`,
		`\N{`,
		`\N{NOT A CHARACTER}`,
		`\N{HANGUL SYLLABLE GA}`,
		`\N{GREEK 0378}`,
		`\N{PRIVATE USE AREA E001`,
	} {
		if _, err := compileSleepRegex(pattern, false); err == nil {
			t.Errorf("compileSleepRegex(%q) unexpectedly succeeded", pattern)
		}
	}
}

func TestSleepJavaRegexUnicodeBlocks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "ancient_symbols", pattern: `\p{InAncientSymbols}`, input: string(rune(0x10190)), want: true},
		{name: "ancient_symbols_lower_edge", pattern: `\p{blk=Ancient_Symbols}`, input: string(rune(0x1018f)), want: false},
		{name: "unicode_17_extension_j", pattern: `\p{block=CJK Unified Ideographs Extension J}`, input: string(rune(0x323b0)), want: true},
		{name: "legacy_greek_alias", pattern: `\p{InGreek}`, input: "α", want: true},
		{name: "canonical_greek_alias", pattern: `\p{InGreek and Coptic}`, input: "α", want: true},
		{name: "legacy_combining_alias", pattern: `\p{InCombining Marks for Symbols}`, input: string(rune(0x20d0)), want: true},
		{name: "canonical_combining_alias", pattern: `\p{InCombining Diacritical Marks for Symbols}`, input: string(rune(0x20d0)), want: true},
		{name: "negated_block", pattern: `\P{InAncientSymbols}`, input: "A", want: true},
		{name: "historical_empty_surrogates_area", pattern: `\p{blk=SURROGATES_AREA}`, input: "A", want: false},
	}
	assertJavaRegexMatches(t, tests)

	if _, err := compileSleepRegex(`\p{InDefinitelyNotABlock}`, false); err == nil {
		t.Fatal("unknown Unicode block unexpectedly compiled")
	}
	for _, pattern := range []string{`\p{InAncient-Symbols}`, `\p{inAncientSymbols}`} {
		if _, err := compileSleepRegex(pattern, false); err == nil {
			t.Errorf("non-Java block spelling %q unexpectedly compiled", pattern)
		}
	}
}

func TestSleepJavaRegexEmojiBinaryProperties(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "emoji", pattern: `\p{IsEmoji}`, input: "😀", want: true},
		{name: "emoji_ascii_digit", pattern: `\p{IsEmoji}`, input: "1", want: true},
		{name: "emoji_presentation", pattern: `\p{IsEmoji_Presentation}`, input: "😀", want: true},
		{name: "emoji_presentation_negative", pattern: `\p{IsEmoji_Presentation}`, input: "1", want: false},
		{name: "emoji_modifier", pattern: `\p{IsEmoji_Modifier}`, input: "🏻", want: true},
		{name: "emoji_modifier_base", pattern: `\p{IsEmoji_Modifier_Base}`, input: "👍", want: true},
		{name: "emoji_component", pattern: `\p{IsEmoji_Component}`, input: "\u200d", want: true},
		{name: "extended_pictographic", pattern: `\p{IsExtended_Pictographic}`, input: "©", want: true},
		{name: "negated_emoji", pattern: `\P{IsEmoji}`, input: "A", want: true},
		{name: "case_mode_does_not_widen", pattern: `(?iu)\p{IsEmoji}`, input: "A", want: false},
	}
	assertJavaRegexMatches(t, tests)
	for _, pattern := range []string{`\p{IsEmojiPresentation}`, `\p{IsEmoji-Modifier-Base}`} {
		if _, err := compileSleepRegex(pattern, false); err == nil {
			t.Errorf("non-Java emoji-property spelling %q unexpectedly compiled", pattern)
		}
	}
}

func TestSleepJavaRegexJavaCharacterProperties(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "mirrored_ascii", pattern: `\p{javaMirrored}`, input: "(", want: true},
		{name: "mirrored_supplementary", pattern: `\p{javaMirrored}`, input: string(rune(0x1d6db)), want: true},
		{name: "mirrored_negative", pattern: `\p{javaMirrored}`, input: "A", want: false},
		{name: "mirrored_complement", pattern: `\P{javaMirrored}`, input: "A", want: true},
		{name: "alphabetic", pattern: `\p{javaAlphabetic}`, input: "Ⅰ", want: true},
		{name: "ideographic", pattern: `\p{javaIdeographic}`, input: "中", want: true},
		{name: "digit", pattern: `\p{javaDigit}`, input: "١", want: true},
		{name: "defined_private_use", pattern: `\p{javaDefined}`, input: "\ue000", want: true},
		{name: "defined_unassigned", pattern: `\p{javaDefined}`, input: string(rune(0x0378)), want: false},
		{name: "letter", pattern: `\p{javaLetter}`, input: "é", want: true},
		{name: "letter_or_digit", pattern: `\p{javaLetterOrDigit}`, input: "١", want: true},
		{name: "java_identifier_start_currency", pattern: `\p{javaJavaIdentifierStart}`, input: "$", want: true},
		{name: "java_identifier_start_digit", pattern: `\p{javaJavaIdentifierStart}`, input: "1", want: false},
		{name: "java_identifier_part_mark", pattern: `\p{javaJavaIdentifierPart}`, input: "\u0301", want: true},
		{name: "unicode_identifier_start_other", pattern: `\p{javaUnicodeIdentifierStart}`, input: "℘", want: true},
		{name: "unicode_identifier_start_currency", pattern: `\p{javaUnicodeIdentifierStart}`, input: "$", want: false},
		{name: "unicode_identifier_part_other", pattern: `\p{javaUnicodeIdentifierPart}`, input: "·", want: true},
		{name: "identifier_ignorable", pattern: `\p{javaIdentifierIgnorable}`, input: "\u200b", want: true},
		{name: "space_char_nbsp", pattern: `\p{javaSpaceChar}`, input: "\u00a0", want: true},
		{name: "java_whitespace_nbsp", pattern: `\p{javaWhitespace}`, input: "\u00a0", want: false},
		{name: "iso_control", pattern: `\p{javaISOControl}`, input: "\u0085", want: true},
	}
	assertJavaRegexMatches(t, tests)
}

func TestSleepJavaRegexGeneratedUnicodeTableShape(t *testing.T) {
	t.Parallel()
	if len(javaRegexUnicodeBlocks) < 340 {
		t.Fatalf("Unicode block count = %d, want the complete Unicode 17 registry", len(javaRegexUnicodeBlocks))
	}
	for index, block := range javaRegexUnicodeBlocks {
		if block.lo > block.hi || block.javaName == "" {
			t.Fatalf("Unicode block %d is invalid: %+v", index, block)
		}
		if index > 0 && javaRegexUnicodeBlocks[index-1].hi >= block.lo {
			t.Fatalf("Unicode blocks %d and %d overlap or are unsorted", index-1, index)
		}
	}
	for alias, index := range javaRegexUnicodeBlockAliases {
		if alias == "" || index < -1 || index >= len(javaRegexUnicodeBlocks) {
			t.Fatalf("Unicode block alias %q has invalid index %d", alias, index)
		}
		if _, err := javaRegexGeneratedBlockProperty(alias); err != nil {
			t.Fatalf("generated block alias %q: %v", alias, err)
		}
	}
	if len(javaRegexMirroredRanges) < 100 {
		t.Fatalf("mirrored range count = %d, want generated Unicode 17 table", len(javaRegexMirroredRanges))
	}
	assertJavaRegexRangesSorted(t, "mirrored", javaRegexMirroredRanges)
	assertJavaRegexRangesSorted(t, "assigned", javaRegexAssignedRanges)
	for _, property := range []string{
		"EMOJI", "EMOJI_PRESENTATION", "EMOJI_MODIFIER", "EMOJI_MODIFIER_BASE", "EMOJI_COMPONENT", "EXTENDED_PICTOGRAPHIC",
	} {
		if len(javaRegexEmojiRanges[property]) == 0 {
			t.Errorf("generated property %q is empty", property)
		}
		assertJavaRegexRangesSorted(t, property, javaRegexEmojiRanges[property])
	}
	byName, _, err := loadJavaRegexCharacterNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(byName) != 40534 {
		t.Fatalf("OpenJDK direct character-name count = %d, want 40534", len(byName))
	}
}

func assertJavaRegexRangesSorted(t *testing.T, name string, ranges []javaRegexRuneRange) {
	t.Helper()
	for index, current := range ranges {
		if current.lo > current.hi {
			t.Fatalf("%s range %d is invalid: %+v", name, index, current)
		}
		if index > 0 && ranges[index-1].hi >= current.lo {
			t.Fatalf("%s ranges %d and %d overlap or are unsorted", name, index-1, index)
		}
	}
}

func assertJavaRegexMatches(t *testing.T, tests []struct {
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

func TestSleepJavaRegexNamedCharacterTranslation(t *testing.T) {
	t.Parallel()
	translated, err := translateSleepRegex(`\N{LATIN SMALL LETTER A}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(translated, `\x{61}`) {
		t.Fatalf("translated named character = %q, want U+0061 escape", translated)
	}
}

func TestJavaRegexUnicode17RootUpper(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"opfor":             "OPFOR",
		"straße":            "STRASSE",
		"ﬃ":                 "FFI",
		"ı":                 "I",
		"Greek expansion ᾀ": "GREEK EXPANSION ἈΙ",
	}
	for input, want := range tests {
		if got := javaRegexRootUpper(input); got != want {
			t.Errorf("javaRegexRootUpper(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestJavaRegexUnicode17ExactLookupGrammar(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Latin", "latin", "Latn", "latn", "Old_Italic", "old_italic"} {
		if _, err := javaRegexGeneratedScriptProperty(name); err != nil {
			t.Errorf("script %q: %v", name, err)
		}
	}
	for _, name := range []string{"Old Italic", "Old-Italic", "OldItalic"} {
		if _, err := javaRegexGeneratedScriptProperty(name); err == nil {
			t.Errorf("non-Java script spelling %q unexpectedly accepted", name)
		}
	}

	for _, name := range []string{"Lu", "Ll", "LC", "LD", "Cn"} {
		if _, err := javaRegexGeneratedCategoryProperty(name, false); err != nil {
			t.Errorf("category %q: %v", name, err)
		}
	}
	for _, name := range []string{"lu", "Uppercase_Letter", "Lowercase Letter"} {
		if _, err := javaRegexGeneratedCategoryProperty(name, false); err == nil {
			t.Errorf("non-Java category spelling %q unexpectedly accepted", name)
		}
	}

	withUnderscore, ok := javaRegexGeneratedBinaryProperty("white_space")
	if !ok {
		t.Fatal("WHITE_SPACE property missing")
	}
	withoutUnderscore, ok := javaRegexGeneratedBinaryProperty("whitespace")
	if !ok || withUnderscore != withoutUnderscore {
		t.Fatal("WHITESPACE aliases do not resolve to the same fixed table")
	}
	if _, ok := javaRegexGeneratedBinaryProperty("white space"); ok {
		t.Fatal("binary property loose-space spelling unexpectedly accepted")
	}
	for _, internal := range []string{"Other_Alphabetic", "CASED", "JAVA_IDENTIFIER_START"} {
		if _, ok := javaRegexGeneratedBinaryProperty(internal); ok {
			t.Errorf("internal generated property %q unexpectedly exposed as a Java binary property", internal)
		}
	}
}

func TestSleepJavaRegexUnicode17NewScriptsAndCategories(t *testing.T) {
	t.Parallel()
	assertJavaRegexMatches(t, []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "sidetic_enum", pattern: `\p{sc=Sidetic}`, input: string(rune(0x10940)), want: true},
		{name: "sidetic_iso_alias", pattern: `\p{script=Sidt}`, input: string(rune(0x10940)), want: true},
		{name: "beria_erfe_upper", pattern: `\p{Lu}`, input: string(rune(0x16ea0)), want: true},
		{name: "beria_erfe_lower_not_upper", pattern: `\p{Lu}`, input: string(rune(0x16ebb)), want: false},
		{name: "beria_erfe_script_iso", pattern: `\p{sc=Berf}`, input: string(rune(0x16ebb)), want: true},
		{name: "locale_root_full_upper_property", pattern: `\p{IsAßigned}`, input: "A", want: true},
	})
}

func TestJavaRegexUnicode17PinnedMetadata(t *testing.T) {
	t.Parallel()
	if javaRegexUnicodeVersion != "17.0.0" {
		t.Fatalf("Unicode version = %q, want 17.0.0", javaRegexUnicodeVersion)
	}
	wantHashes := map[string]string{
		"UnicodeData.txt":          "2e1efc1dcb59c575eedf5ccae60f95229f706ee6d031835247d843c11d96470c",
		"Blocks.txt":               "c0edefaf1a19771e830a82735472716af6bf3c3975f6c2a23ffbe2580fbbcb15",
		"Scripts.txt":              "9f5e50d3abaee7d6ce09480f325c706f485ae3240912527e651954d2d6b035bf",
		"PropList.txt":             "130dcddcaadaf071008bdfce1e7743e04fdfbc910886f017d9f9ac931d8c64dd",
		"PropertyValueAliases.txt": "64e9a5f76f7a1e8b5a47d6a1f9a26522a251208f5276bdfa1559dac7cf2e827a",
		"SpecialCasing.txt":        "efc25faf19de21b92c1194c111c932e03d2a5eaf18194e33f1156e96de4c9588",
		"emoji-data.txt":           "2cb2bb9455cda83e8481541ecf5b6dfda66a3bb89efa3fa7c5297eccf607b72b",
	}
	gotHashes := map[string]string{
		"UnicodeData.txt":          javaRegexUnicodeDataSHA256,
		"Blocks.txt":               javaRegexBlocksSHA256,
		"Scripts.txt":              javaRegexScriptsSHA256,
		"PropList.txt":             javaRegexPropListSHA256,
		"PropertyValueAliases.txt": javaRegexPropertyValueAliasesSHA256,
		"SpecialCasing.txt":        javaRegexSpecialCasingSHA256,
		"emoji-data.txt":           javaRegexEmojiDataSHA256,
	}
	for name, want := range wantHashes {
		if got := gotHashes[name]; got != want {
			t.Errorf("%s digest = %q, want %q", name, got, want)
		}
	}
	if len(javaRegexUnicodeBlocks) != 346 || len(javaRegexUnicodeBlockAliases) != 804 {
		t.Fatalf("block registry shape = %d blocks/%d aliases, want 346/804", len(javaRegexUnicodeBlocks), len(javaRegexUnicodeBlockAliases))
	}
	if len(javaRegexScriptRanges) != 175 || len(javaRegexScriptAliases) != 342 {
		t.Fatalf("script registry shape = %d scripts/%d aliases, want 175/342", len(javaRegexScriptRanges), len(javaRegexScriptAliases))
	}
}

func TestJavaRegexUnicode17SemanticFingerprint(t *testing.T) {
	t.Parallel()
	hash := sha256.New()
	writeRanges := func(namespace, name string, ranges []javaRegexRuneRange) {
		fmt.Fprintf(hash, "%s/%s:", namespace, name)
		for _, current := range ranges {
			fmt.Fprintf(hash, "%x-%x,", current.lo, current.hi)
		}
		hash.Write([]byte{'\n'})
	}
	writeRangeMap := func(namespace string, values map[string][]javaRegexRuneRange) {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			writeRanges(namespace, key, values[key])
		}
	}
	writeRanges("core", "assigned", javaRegexAssignedRanges)
	writeRanges("core", "mirrored", javaRegexMirroredRanges)
	writeRangeMap("category", javaRegexCategoryRanges)
	writeRangeMap("script", javaRegexScriptRanges)
	writeRangeMap("property", javaRegexPropertyRanges)
	for index, block := range javaRegexUnicodeBlocks {
		fmt.Fprintf(hash, "block-range/%d=%x-%x/%s\n", index, block.lo, block.hi, block.javaName)
	}
	aliasKeys := make([]string, 0, len(javaRegexScriptAliases)+len(javaRegexUnicodeBlockAliases))
	for key := range javaRegexScriptAliases {
		aliasKeys = append(aliasKeys, "script/"+key)
	}
	for key := range javaRegexUnicodeBlockAliases {
		aliasKeys = append(aliasKeys, "block/"+key)
	}
	sort.Strings(aliasKeys)
	for _, key := range aliasKeys {
		if strings.HasPrefix(key, "script/") {
			name := strings.TrimPrefix(key, "script/")
			fmt.Fprintf(hash, "%s=%s\n", key, javaRegexScriptAliases[name])
		} else {
			name := strings.TrimPrefix(key, "block/")
			fmt.Fprintf(hash, "%s=%d\n", key, javaRegexUnicodeBlockAliases[name])
		}
	}
	for _, mapping := range javaRegexRootUpperMappings {
		fmt.Fprintf(hash, "upper/%x=%x\n", mapping.from, []byte(mapping.to))
	}
	characterNames, _, err := loadJavaRegexCharacterNames()
	if err != nil {
		t.Fatal(err)
	}
	nameKeys := make([]string, 0, len(characterNames))
	for name := range characterNames {
		nameKeys = append(nameKeys, name)
	}
	sort.Strings(nameKeys)
	for _, name := range nameKeys {
		fmt.Fprintf(hash, "name/%s=%x\n", name, characterNames[name])
	}
	got := fmt.Sprintf("%x", hash.Sum(nil))
	const want = "04defb513db91b1ee2b9e0ea9626b934bfa0cb2f576d809a16a5d157202a8f5e"
	if got != want {
		t.Fatalf("Unicode-17 semantic fingerprint = %s, want %s", got, want)
	}
}
