package regexp2

import (
	"errors"
	"reflect"
	"testing"
)

func TestJavaGraphemeBoundaryRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text []rune
		want []int
	}{
		{name: "ascii", text: []rune("ab"), want: []int{0, 1, 2}},
		{name: "crlf", text: []rune("\r\n"), want: []int{0, 2}},
		{name: "extend", text: []rune{'a', 0x0308, 'b'}, want: []int{0, 2, 3}},
		{name: "prepend", text: []rune{0x0600, 'a'}, want: []int{0, 2}},
		{name: "spacing_mark", text: []rune{0x0915, 0x093e}, want: []int{0, 2}},
		{name: "hangul", text: []rune{0x1100, 0x1161, 0x11a8}, want: []int{0, 3}},
		{name: "regional_indicators", text: []rune{0x1f1fa, 0x1f1f8, 0x1f1e8}, want: []int{0, 2, 3}},
		{name: "emoji_zwj", text: []rune{0x1f469, 0x200d, 0x1f4bb}, want: []int{0, 3}},
		// Pinned OpenJDK initializes GB11 only from nextBoundary's first
		// code point. After GB9b consumes Prepend, it therefore breaks before
		// the second pictograph; formal UAX #29 would keep this constructed
		// cross-rule sequence together.
		{name: "openjdk_prepend_then_emoji_zwj", text: []rune{0x0600, 0x1f469, 0x200d, 0x1f4bb}, want: []int{0, 3, 4}},
		{name: "indic_conjunct", text: []rune{0x0915, 0x094d, 0x0937}, want: []int{0, 3}},
		{name: "unpaired_surrogates", text: []rune{0xd800, 0xdfff}, want: []int{0, 1, 2}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := []int{0}
			for offset := 0; offset < len(test.text); {
				next, err := nextJavaGraphemeBoundary(test.text, offset, len(test.text), nil)
				if err != nil {
					t.Fatal(err)
				}
				if next <= offset {
					t.Fatalf("next boundary %d did not advance from %d", next, offset)
				}
				got = append(got, next)
				offset = next
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("boundaries = %v, want %v", got, test.want)
			}
		})
	}
}

func TestJavaGraphemeStartsAtRequestedOffset(t *testing.T) {
	t.Parallel()
	// OpenJDK's XGrapheme passes its actual input offset to nextBoundary. The
	// RI pair therefore starts at index one even though index one is not a
	// boundary in the surrounding three-indicator string.
	text := []rune{0x1f1fa, 0x1f1f8, 0x1f1e8}
	got, err := nextJavaGraphemeBoundary(text, 1, len(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("next boundary from offset 1 = %d, want 3", got)
	}
}

func TestJavaGraphemePinnedMetadata(t *testing.T) {
	t.Parallel()
	if javaGraphemeUnicodeVersion != "17.0.0" ||
		javaGraphemeBreakPropertySHA256 != "d6b51d1d2ae5c33b451b7ed994b48f1f4dc62b2272a5831e7fd418514a6bae89" ||
		javaGraphemeDerivedCorePropertiesSHA256 != "24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08" {
		t.Fatal("grapheme metadata does not identify the pinned Unicode 17 inputs")
	}
}

func TestJavaGraphemeLongClusterChecksTimeout(t *testing.T) {
	t.Parallel()
	want := errors.New("test timeout")
	text := make([]rune, 4096)
	text[0] = 'a'
	for index := 1; index < len(text); index++ {
		text[index] = 0x0301
	}
	checks := 0
	_, err := nextJavaGraphemeBoundary(text, 0, len(text), func() error {
		checks++
		return want
	})
	if !errors.Is(err, want) || checks == 0 {
		t.Fatalf("nextJavaGraphemeBoundary error = %v, checks = %d", err, checks)
	}
}

func TestJavaGraphemeRightToLeftConsumesWholePreviousCluster(t *testing.T) {
	t.Parallel()
	expression, err := Compile(`\X`, RE2|RightToLeft)
	if err != nil {
		t.Fatal(err)
	}
	match, err := expression.FindRunesMatch([]rune{'a', 0x0301, 'b'})
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || match.Index != 2 || match.Length != 1 {
		t.Fatalf("first right-to-left match = %#v, want index 2 length 1", match)
	}
	match, err = expression.FindNextMatch(match)
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || match.Index != 0 || match.Length != 2 {
		t.Fatalf("second right-to-left match = %#v, want index 0 length 2", match)
	}
}
