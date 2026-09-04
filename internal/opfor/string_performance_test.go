package opfor

import (
	"math"
	"testing"
)

func TestSleepStringCanonicalAndLengthFastPaths(t *testing.T) {
	cases := []struct {
		name      string
		value     Value
		canonical string
		length    int
	}{
		{"empty", String(""), "", 0},
		{"ascii", String("operator42@example.com"), "operator42@example.com", 22},
		{"nul", String("a\x00b"), "a\x00b", 3},
		{"unicode", String("aé水😀"), "aé水😀", 5},
		{"binary-utf8", BinaryString([]byte{0xc3, 0xa9}), "Ã©", 2},
		{"invalid-utf8", String("a\xff"), "aÿ", 2},
		{"surrogate", sleepStringValueFromUnits([]uint16{0xd800}, nil), "\xed\xa0\x80", 1},
		{"surrogate-pair", sleepStringValueFromUnits([]uint16{0xd83d, 0xde00}, nil), "😀", 2},
		{"mixed", sleepStringConcat(String("é"), BinaryString([]byte{0xe9})), "éé", 2},
		{"null", Null(), "", 0},
		{"int", Int(-1234), "-1234", 5},
		{"long", Long(4294967296), "4294967296", 10},
		{"double", Double(1.5), "1.5", 3},
		{"nan", Double(math.NaN()), "NaN", 3},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			for _, tainted := range []bool{false, true} {
				value := item.value
				value.tainted = tainted
				if got := sleepCanonicalString(value); got != item.canonical {
					t.Fatalf("canonical = %q, want %q", got, item.canonical)
				}
				if got := sleepStringLength(value); got != item.length {
					t.Fatalf("length = %d, want %d", got, item.length)
				}
				// Canonicalization and length must leave provenance untouched.
				if value.String() != item.value.String() || value.IsBinaryString() != item.value.IsBinaryString() {
					t.Fatal("string metadata changed")
				}
			}
		})
	}
}

func TestSleepStringPlainTextOperationsDoNotAllocate(t *testing.T) {
	value := String("operator42@例.example😀")
	var canonical string
	var length int
	if allocations := testing.AllocsPerRun(100, func() {
		canonical = sleepCanonicalString(value)
		length = sleepStringLength(value)
	}); allocations != 0 {
		t.Fatalf("plain text operations allocated %v times", allocations)
	}
	if canonical != value.String() || length != len(sleepStringUnits(value)) {
		t.Fatalf("canonical = %q, length = %d", canonical, length)
	}
}
