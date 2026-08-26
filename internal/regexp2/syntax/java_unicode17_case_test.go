package syntax

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestJavaUnicode17CaseTables(t *testing.T) {
	t.Parallel()
	if javaUnicodeDataVersion != "17.0.0" {
		t.Fatalf("Unicode version = %q, want 17.0.0", javaUnicodeDataVersion)
	}
	if javaUnicodeDataSHA256 != "2e1efc1dcb59c575eedf5ccae60f95229f706ee6d031835247d843c11d96470c" {
		t.Fatalf("UnicodeData digest = %q", javaUnicodeDataSHA256)
	}
	for name, mappings := range map[string][]javaUnicode17CaseMapping{
		"upper": javaUnicode17UpperMappings,
		"lower": javaUnicode17LowerMappings,
	} {
		for index, mapping := range mappings {
			if mapping.from == mapping.to {
				t.Fatalf("%s mapping %d maps a code point to itself", name, index)
			}
			if index > 0 && mappings[index-1].from >= mapping.from {
				t.Fatalf("%s mappings %d and %d are unsorted", name, index-1, index)
			}
		}
	}
	for _, test := range []struct {
		input rune
		want  rune
	}{
		{input: 'a', want: 'a'},
		{input: 'A', want: 'a'},
		{input: '\u017f', want: 's'},
		{input: '\u212a', want: 'k'},
		{input: '\u0131', want: 'i'},
		{input: '\u03c2', want: '\u03c3'},
		{input: '\U00010400', want: '\U00010428'},
	} {
		if got := FoldCase(test.input, CaseJavaUnicode); got != test.want {
			t.Errorf("FoldCase(%U) = %U, want %U", test.input, got, test.want)
		}
	}
}

func TestJavaUnicode17CaseTableFingerprint(t *testing.T) {
	t.Parallel()
	hash := sha256.New()
	for _, table := range []struct {
		name     string
		mappings []javaUnicode17CaseMapping
	}{
		{name: "upper", mappings: javaUnicode17UpperMappings},
		{name: "lower", mappings: javaUnicode17LowerMappings},
	} {
		for _, mapping := range table.mappings {
			fmt.Fprintf(hash, "%s/%x=%x\n", table.name, mapping.from, mapping.to)
		}
	}
	got := fmt.Sprintf("%x", hash.Sum(nil))
	const want = "11a6f3e4b7aecac1d3c6fa000ff043c5e23243399ea92a676e900fe733886071"
	if got != want {
		t.Fatalf("Unicode-17 case-table fingerprint = %s, want %s", got, want)
	}
}

func TestJavaUnicode17CaseTablesMatchGo17Oracle(t *testing.T) {
	if unicode.Version != "17.0.0" {
		t.Skipf("Go toolchain Unicode version %s is not the pinned 17.0.0 oracle", unicode.Version)
	}
	for current := rune(0); current <= utf8.MaxRune; current++ {
		if got, want := javaUnicode17ToUpper(current), unicode.ToUpper(current); got != want {
			t.Fatalf("upper(%U) = %U, want %U", current, got, want)
		}
		if got, want := javaUnicode17ToLower(current), unicode.ToLower(current); got != want {
			t.Fatalf("lower(%U) = %U, want %U", current, got, want)
		}
	}
}
