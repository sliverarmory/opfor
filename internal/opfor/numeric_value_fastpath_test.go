package opfor

import (
	"bytes"
	"math"
	"testing"
)

func TestNumericValueInlinePayloadEdges(t *testing.T) {
	t.Parallel()

	for _, number := range []int32{math.MinInt32, -65537, -1, 0, 1, 65537, math.MaxInt32} {
		value := Int(number)
		if value.Kind() != KindInt || value.Int32() != number || value.Int64() != int64(number) || value.Float64() != float64(number) {
			t.Fatalf("Int(%d) coercions changed: %s", number, value.Describe())
		}
		if value.Truth() != (number != 0) || !value.IdentityEqual(Int(number)) || value.IdentityEqual(Long(int64(number))) {
			t.Fatalf("Int(%d) truth or identity changed", number)
		}
	}
	for _, number := range []int64{math.MinInt64, -4294967297, -1, 0, 1, 4294967297, math.MaxInt64} {
		value := Long(number)
		if value.Kind() != KindLong || value.Int32() != int32(number) || value.Int64() != number || value.Float64() != float64(number) {
			t.Fatalf("Long(%d) coercions changed: %s", number, value.Describe())
		}
		if value.Truth() != (number != 0) || !value.IdentityEqual(Long(number)) {
			t.Fatalf("Long(%d) truth or identity changed", number)
		}
	}
	for _, bits := range []uint64{
		0, 1 << 63, // Both signs of zero must remain distinct in storage.
		1, math.Float64bits(math.MaxFloat64),
		math.Float64bits(math.Inf(1)), math.Float64bits(math.Inf(-1)),
		0x7ff8000000001234, 0xfff8000000005678, // Preserve NaN payloads and signs.
	} {
		number := math.Float64frombits(bits)
		value := Double(number)
		if value.Kind() != KindDouble || math.Float64bits(value.Float64()) != bits {
			t.Fatalf("Double bits = %016x, want %016x", math.Float64bits(value.Float64()), bits)
		}
		if value.Truth() != (number != 0) || value.IdentityEqual(value) != !math.IsNaN(number) {
			t.Fatalf("Double(%016x) truth or identity changed", bits)
		}
	}
	if !Double(0).IdentityEqual(Double(math.Copysign(0, -1))) {
		t.Fatal("double identity must compare numeric value, including both signs of zero")
	}
}

func TestNumericValueArithmeticOverflowAndPromotion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		left, right, want Value
		operator          string
	}{
		{Int(math.MaxInt32), Int(1), Int(math.MinInt32), "+"},
		{Int(math.MinInt32), Int(1), Int(math.MaxInt32), "-"},
		{Long(math.MaxInt64), Int(1), Long(math.MinInt64), "+"},
		{Long(math.MinInt64), Long(1), Long(math.MaxInt64), "-"},
		{Int(-1), Long(4294967296), Long(4294967295), "+"},
		{Long(4294967296), Double(0.5), Double(4294967296.5), "+"},
		{Double(1), Double(0), Double(math.Inf(1)), "/"},
	} {
		got, err := numericBinary(test.left, test.operator, test.right)
		if err != nil || !got.IdentityEqual(test.want) {
			t.Fatalf("%s %s %s = (%s, %v), want %s", test.left.Describe(), test.operator, test.right.Describe(), got.Describe(), err, test.want.Describe())
		}
	}
}

func TestNumericValueTaintAndSerializationPreservePayload(t *testing.T) {
	t.Parallel()
	runtimeInstance := &Runtime{taintMode: true}
	for _, value := range []Value{
		Int(math.MinInt32), Int(math.MaxInt32), Long(math.MinInt64), Long(math.MaxInt64),
		Double(math.Copysign(0, -1)), Double(math.SmallestNonzeroFloat64),
		Double(math.Inf(1)), Double(math.Inf(-1)),
	} {
		tainted := runtimeInstance.Taint(value)
		if !tainted.IsTainted() || value.IsTainted() || !tainted.IdentityEqual(value) {
			t.Fatalf("taint changed numeric payload or original: %s", value.Describe())
		}
		encoded, err := encodeSleepScalarStream(tainted)
		if err != nil {
			t.Fatal(err)
		}
		decoded, consumed, err := decodeSleepScalarStream(bytes.NewReader(encoded))
		if err != nil || consumed != int64(len(encoded)) || !decoded.IdentityEqual(value) {
			t.Fatalf("numeric serialization = (%s, %d, %v), want %s", decoded.Describe(), consumed, err, value.Describe())
		}
		if value.Kind() == KindDouble && math.Float64bits(decoded.Float64()) != math.Float64bits(value.Float64()) {
			t.Fatalf("numeric serialization changed double bits for %s", value.Describe())
		}
		if untainted := runtimeInstance.Untaint(tainted); untainted.IsTainted() || !untainted.IdentityEqual(value) {
			t.Fatalf("untaint changed numeric payload: %s", value.Describe())
		}
	}
}

var numericValueAllocationSink Value

func TestNumericArithmeticDoesNotAllocateBoxes(t *testing.T) {
	for _, value := range []Value{Int(65537), Long(1 << 40), Double(1.25)} {
		var arithmeticErr error
		allocations := testing.AllocsPerRun(100, func() {
			numericValueAllocationSink, arithmeticErr = numericBinary(value, "+", value)
		})
		if arithmeticErr != nil {
			t.Fatal(arithmeticErr)
		}
		if allocations != 0 {
			t.Fatalf("%s arithmetic allocated %.0f boxes, want zero", value.Kind(), allocations)
		}
	}
}
