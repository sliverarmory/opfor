package lexer

import (
	"math"
	"testing"
)

func TestClassifySleepNumericLiterals(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		raw    string
		hinted Kind
		want   Kind
		ok     bool
	}{
		{name: "octal integer", raw: "077", hinted: Integer, want: Integer, ok: true},
		{name: "invalid octal double fallback", raw: "09", hinted: Integer, want: Double, ok: true},
		{name: "decimal integer overflow double fallback", raw: "2147483648", hinted: Integer, want: Double, ok: true},
		{name: "negative decimal integer overflow double fallback", raw: "-2147483649", hinted: Integer, want: Double, ok: true},
		{name: "attached decimal minimum", raw: "-2147483648", hinted: Integer, want: Integer, ok: true},
		{name: "attached hexadecimal minimum", raw: "-0x80000000", hinted: Integer, want: Integer, ok: true},
		{name: "positive hexadecimal overflow", raw: "0x80000000", hinted: Integer, ok: false},
		{name: "long minimum", raw: "-9223372036854775808L", hinted: Long, want: Long, ok: true},
		{name: "long overflow", raw: "9223372036854775808L", hinted: Long, ok: false},
		{name: "negative long overflow", raw: "-9223372036854775809L", hinted: Long, ok: false},
		{name: "Arabic decimal digits", raw: "١٢", hinted: Integer, want: Integer, ok: true},
		{name: "fullwidth hexadecimal digits", raw: "0xＦｆ", hinted: Integer, want: Integer, ok: true},
		{name: "supplementary decimal digit", raw: "𝟙", hinted: Integer, ok: false},
		{name: "Unicode integer overflow does not become double", raw: "٢١٤٧٤٨٣٦٤٨", hinted: Integer, ok: false},
		{name: "Unicode decimal double", raw: "1.٢", hinted: Double, ok: false},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ClassifyNumericLiteral(test.raw, test.hinted)
			if got != test.want || ok != test.ok {
				t.Fatalf("ClassifyNumericLiteral(%q, %v) = (%v, %v), want (%v, %v)", test.raw, test.hinted, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestParseSleepJavaIntegerLiteralRejectsPastNegativeMinimum(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw  string
		bits int
	}{
		{raw: "-2147483649", bits: 32},
		{raw: "-0x80000001", bits: 32},
		{raw: "-9223372036854775809", bits: 64},
		{raw: "-0x8000000000000001", bits: 64},
	} {
		test := test
		t.Run(test.raw, func(t *testing.T) {
			t.Parallel()
			if got, err := ParseJavaIntegerLiteral(test.raw, test.bits); err == nil {
				t.Fatalf("ParseJavaIntegerLiteral(%q, %d) = (%d, nil), want range error", test.raw, test.bits, got)
			}
		})
	}
}

func TestParseSleepJavaIntegerLiteral(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw  string
		bits int
		want int64
	}{
		{raw: "00", bits: 32, want: 0},
		{raw: "077", bits: 32, want: 63},
		{raw: "-020000000000", bits: 32, want: math.MinInt32},
		{raw: "-2147483648", bits: 32, want: math.MinInt32},
		{raw: "-0x80000000", bits: 32, want: math.MinInt32},
		{raw: "-9223372036854775808", bits: 64, want: math.MinInt64},
		{raw: "-0x8000000000000000", bits: 64, want: math.MinInt64},
		{raw: "١٢", bits: 32, want: 12},
		{raw: "0x١٢", bits: 32, want: 18},
		{raw: "0xＡ９", bits: 32, want: 169},
		{raw: "٠٩", bits: 32, want: 9},
		{raw: "0٧٧", bits: 32, want: 63},
	} {
		test := test
		t.Run(test.raw, func(t *testing.T) {
			t.Parallel()
			got, err := ParseJavaIntegerLiteral(test.raw, test.bits)
			if err != nil || got != test.want {
				t.Fatalf("ParseJavaIntegerLiteral(%q, %d) = (%d, %v), want (%d, nil)", test.raw, test.bits, got, err, test.want)
			}
		})
	}
}

func TestParseSleepJavaDoubleRangeResults(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw          string
		wantInfinity int
		wantZero     bool
	}{
		{raw: "1e9999", wantInfinity: 1},
		{raw: "-1e9999", wantInfinity: -1},
		{raw: "1e-9999", wantZero: true},
		{raw: "0x1p999999", wantInfinity: 1},
		{raw: "0x1p-999999", wantZero: true},
	} {
		test := test
		t.Run(test.raw, func(t *testing.T) {
			t.Parallel()
			got, err := ParseJavaDoubleLiteral(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantInfinity != 0 && !math.IsInf(got, test.wantInfinity) {
				t.Fatalf("ParseJavaDoubleLiteral(%q) = %v, want infinity sign %d", test.raw, got, test.wantInfinity)
			}
			if test.wantZero && got != 0 {
				t.Fatalf("ParseJavaDoubleLiteral(%q) = %v, want zero", test.raw, got)
			}
		})
	}
}

func TestJavaDigitRejectsNonCharacterDigitForms(t *testing.T) {
	t.Parallel()

	for _, character := range []rune{'²', 'Ⅻ', 'Ⓐ', '𝟙'} {
		if got := JavaDigit(character, 16); got != -1 {
			t.Errorf("JavaDigit(%q, 16) = %d, want -1", character, got)
		}
	}
	if got := JavaDigit('Ｆ', 16); got != 15 {
		t.Fatalf("JavaDigit(fullwidth F, 16) = %d, want 15", got)
	}
}
