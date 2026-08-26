package opfor

import (
	"math"
	"testing"
)

func TestSleepTruth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value Value
		want  bool
	}{
		{name: "null", value: Null(), want: false},
		{name: "empty string", value: String(""), want: false},
		{name: "integer zero", value: Int(0), want: false},
		{name: "string zero", value: String("0"), want: false},
		{name: "double zero", value: Double(0), want: false},
		{name: "nonzero", value: Int(42), want: true},
		{name: "text", value: String("false"), want: true},
		{name: "empty array", value: ArrayValue(NewArray()), want: true},
		{name: "empty hash", value: HashValue(NewHash()), want: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.value.Truth(); got != test.want {
				t.Fatalf("Truth() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestDoubleStringUsesJavaNotation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value float64
		want  string
	}{
		{value: 0, want: "0.0"},
		{value: math.Copysign(0, -1), want: "-0.0"},
		{value: 0.001, want: "0.001"},
		{value: 0.0001, want: "1.0E-4"},
		{value: 9_999_999, want: "9999999.0"},
		{value: 10_000_000, want: "1.0E7"},
		{value: 1.5494e-320, want: "1.5494E-320"},
		{value: math.Inf(1), want: "Infinity"},
		{value: math.Inf(-1), want: "-Infinity"},
		{value: math.NaN(), want: "NaN"},
	}
	for _, test := range tests {
		if got := Double(test.value).String(); got != test.want {
			t.Errorf("Double(%v).String() = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestDescribeCycles(t *testing.T) {
	t.Parallel()

	array := NewArray(String("a"), String("b"))
	array.Append(ArrayValue(array))
	if got, want := ArrayValue(array).Describe(), "@('a', 'b', @0)"; got != want {
		t.Fatalf("array description = %q, want %q", got, want)
	}

	hash := NewHash()
	hash.Set("self", HashValue(hash))
	if got, want := HashValue(hash).Describe(), "%(self => %0)"; got != want {
		t.Fatalf("hash description = %q, want %q", got, want)
	}
}

func TestArrayNegativeIndexAndGrowth(t *testing.T) {
	t.Parallel()

	array := NewArray(Int(1), Int(2))
	last, ok := array.Get(-1)
	if !ok || last.Int32() != 2 {
		t.Fatalf("Get(-1) = (%v, %v), want (2, true)", last, ok)
	}

	cell, err := array.Ensure(4)
	if err != nil {
		t.Fatalf("Ensure(4): %v", err)
	}
	cell.Set(Int(5))
	if got, want := array.Len(), 5; got != want {
		t.Fatalf("Len() = %d, want %d", got, want)
	}
	if middle, ok := array.Get(3); !ok || !middle.IsNull() {
		t.Fatalf("grown slot = (%v, %v), want ($null, true)", middle, ok)
	}
}

func TestHashAutovivificationAndIdentity(t *testing.T) {
	t.Parallel()

	hash := NewHash()
	if _, ok := hash.Get("missing"); ok {
		t.Fatal("Get inserted a missing key")
	}
	cell := hash.Ensure("missing")
	if got, want := hash.Len(), 1; got != want {
		t.Fatalf("Len() = %d, want %d", got, want)
	}
	cell.Set(String("present"))

	left := HashValue(hash)
	right := HashValue(hash)
	other := HashValue(NewHash())
	if !left.IdentityEqual(right) {
		t.Fatal("same hash reference was not identical")
	}
	if left.IdentityEqual(other) {
		t.Fatal("distinct hash references were identical")
	}
}
