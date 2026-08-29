package opfor

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func newAggressorBOFPackTestRuntime(encoder BeaconStringEncoder) *Runtime {
	return &Runtime{aggressorState: aggressorState{
		aggressorIntegrationConfig: aggressorIntegrationConfig{beaconEncoder: encoder},
	}}
}

func TestAggressorBOFPackDefaultBigEndianMixedFormatsExactBytesAndNoOuterPrefix(t *testing.T) {
	t.Parallel()

	runtime := newAggressorBOFPackTestRuntime(utf8BeaconStringEncoder{})
	got := mustCallAggressorBOFPack(t, runtime, context.Background(),
		String("beacon-7"),
		String("biszZ"),
		BinaryString([]byte{0x00, 0x80, 0xff}),
		Int(0x01020304),
		Int(0x0506),
		String("Hi"),
		String("A😀"),
	)
	want := []byte{
		0x00, 0x00, 0x00, 0x03, 0x00, 0x80, 0xff,
		0x01, 0x02, 0x03, 0x04,
		0x05, 0x06,
		0x00, 0x00, 0x00, 0x03, 'H', 'i', 0x00,
		0x00, 0x00, 0x00, 0x08, 0x41, 0x00, 0x3d, 0xd8, 0x00, 0xde, 0x00, 0x00,
	}
	gotBytes, ok := got.Bytes()
	if !ok || !got.IsBinaryString() || !bytes.Equal(gotBytes, want) {
		t.Fatalf("bof_pack mixed result = %x/string=%v/binary=%v, want %x binary string",
			gotBytes, ok, got.IsBinaryString(), want)
	}

	// An integer-only buffer is exactly four bytes. A whole-buffer prefix would
	// make this eight bytes and is not part of the BOF ABI.
	integerOnly := mustCallAggressorBOFPack(t, runtime, context.Background(),
		String("beacon-7"), String("i"), Int(0x10203040))
	if integerBytes, _ := integerOnly.Bytes(); !bytes.Equal(integerBytes, []byte{0x10, 0x20, 0x30, 0x40}) {
		t.Fatalf("bof_pack integer-only result = %x, want no-prefix 10203040", integerBytes)
	}
}

func TestAggressorBOFPackLittleEndianMixedFormatsExactBytesAndNoOuterPrefix(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New(WithBOFPackByteOrder(BOFPackLittleEndian))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	got := mustCallAggressorBOFPack(t, runtimeInstance, context.Background(),
		String("beacon-7"),
		String("biszZ"),
		BinaryString([]byte{0x00, 0x80, 0xff}),
		Int(0x01020304),
		Int(0x0506),
		String("Hi"),
		String("A😀"),
	)
	want := []byte{
		0x03, 0x00, 0x00, 0x00, 0x00, 0x80, 0xff,
		0x04, 0x03, 0x02, 0x01,
		0x06, 0x05,
		0x03, 0x00, 0x00, 0x00, 'H', 'i', 0x00,
		0x08, 0x00, 0x00, 0x00, 0x41, 0x00, 0x3d, 0xd8, 0x00, 0xde, 0x00, 0x00,
	}
	gotBytes, ok := got.Bytes()
	if !ok || !got.IsBinaryString() || !bytes.Equal(gotBytes, want) {
		t.Fatalf("little-endian bof_pack mixed result = %x/string=%v/binary=%v, want %x binary string",
			gotBytes, ok, got.IsBinaryString(), want)
	}

	// The importer may add its own whole-buffer prefix. Even in little-endian
	// mode, OPFOR returns only the four-byte packed integer.
	integerOnly := mustCallAggressorBOFPack(t, runtimeInstance, context.Background(),
		String("beacon-7"), String("i"), Int(0x10203040))
	if integerBytes, _ := integerOnly.Bytes(); !bytes.Equal(integerBytes, []byte{0x40, 0x30, 0x20, 0x10}) {
		t.Fatalf("little-endian bof_pack integer-only result = %x, want no-prefix 40302010", integerBytes)
	}
}

func TestWithBOFPackByteOrderRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	_, err := New(WithBOFPackByteOrder(BOFPackByteOrder(0xff)))
	if err == nil || !strings.Contains(err.Error(), "invalid BOF pack byte order 255") {
		t.Fatalf("invalid BOF pack byte order error = %v, want byte-order validation error", err)
	}
}

func TestAggressorBOFPackSignedAndNarrowNumericBits(t *testing.T) {
	t.Parallel()

	runtime := newAggressorBOFPackTestRuntime(utf8BeaconStringEncoder{})
	got := mustCallAggressorBOFPack(t, runtime, context.Background(),
		String("beacon"), String("isis"),
		Int(-2), Int(-2), Long(1<<32+1), Long(1<<16+1),
	)
	want := []byte{
		0xff, 0xff, 0xff, 0xfe,
		0xff, 0xfe,
		0x00, 0x00, 0x00, 0x01,
		0x00, 0x01,
	}
	if gotBytes, _ := got.Bytes(); !bytes.Equal(gotBytes, want) {
		t.Fatalf("bof_pack signed/narrow result = %x, want %x", gotBytes, want)
	}
}

func TestAggressorBOFPackPreservesExactUTF16Units(t *testing.T) {
	t.Parallel()

	runtime := newAggressorBOFPackTestRuntime(utf8BeaconStringEncoder{})
	text := sleepStringValueFromUnits([]uint16{0x0041, 0xd800, 0x0042, 0xdc00}, nil)
	got := mustCallAggressorBOFPack(t, runtime, context.Background(),
		String("beacon"), String("bZ"), text, text)
	want := []byte{
		0x00, 0x00, 0x00, 0x04, 0x41, 0x00, 0x42, 0x00,
		0x00, 0x00, 0x00, 0x0a,
		0x41, 0x00, 0x00, 0xd8, 0x42, 0x00, 0x00, 0xdc, 0x00, 0x00,
	}
	if gotBytes, _ := got.Bytes(); !bytes.Equal(gotBytes, want) {
		t.Fatalf("bof_pack exact UTF-16 result = %x, want %x", gotBytes, want)
	}
}

func TestAggressorBOFPackZeroTerminatedFormatsStopAtFirstNUL(t *testing.T) {
	t.Parallel()

	runtime := newAggressorBOFPackTestRuntime(utf8BeaconStringEncoder{})
	value := sleepStringValueFromUnits([]uint16{'A', 0, 'B'}, nil)
	got := mustCallAggressorBOFPack(t, runtime, context.Background(),
		String("beacon"), String("bzZ"), value, value, value)
	want := []byte{
		// b is length-delimited binary and preserves the embedded zero.
		0x00, 0x00, 0x00, 0x03, 'A', 0x00, 'B',
		// z and Z follow the official C-string packer's strlen/wcslen boundary.
		0x00, 0x00, 0x00, 0x02, 'A', 0x00,
		0x00, 0x00, 0x00, 0x04, 'A', 0x00, 0x00, 0x00,
	}
	if gotBytes, _ := got.Bytes(); !bytes.Equal(gotBytes, want) {
		t.Fatalf("bof_pack embedded-NUL result = %x, want %x", gotBytes, want)
	}
}

func TestAggressorBOFPackCustomEncoderReceivesBeaconAndText(t *testing.T) {
	t.Parallel()

	type contextKey struct{}
	wantBeacon := String("beacon-custom")
	wantText := BinaryString([]byte{0x80, 'A'})
	var seenBeacon, seenText Value
	var sawContext bool
	runtime := newAggressorBOFPackTestRuntime(BeaconStringEncoderFunc(func(
		ctx context.Context,
		beaconID Value,
		text Value,
	) ([]byte, error) {
		seenBeacon = beaconID
		seenText = text
		sawContext = ctx.Value(contextKey{}) == "present"
		return []byte{0x81, 0x82, 0xfe}, nil
	}))

	ctx := context.WithValue(context.Background(), contextKey{}, "present")
	got := mustCallAggressorBOFPack(t, runtime, ctx, wantBeacon, String("z"), wantText)
	want := []byte{0x00, 0x00, 0x00, 0x04, 0x81, 0x82, 0xfe, 0x00}
	if gotBytes, _ := got.Bytes(); !bytes.Equal(gotBytes, want) {
		t.Fatalf("custom z encoding = %x, want %x", gotBytes, want)
	}
	if !sleepStringValuesEqual(seenBeacon, wantBeacon) ||
		!sleepStringValuesEqual(seenText, wantText) || !seenText.IsBinaryString() || !sawContext {
		t.Fatalf("encoder observed beacon=%s text=%s/binary=%v context=%v",
			seenBeacon.Describe(), seenText.Describe(), seenText.IsBinaryString(), sawContext)
	}
}

func TestAggressorBOFPackImporterEncoderErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			calls := 0
			runtimeInstance, err := New(WithBeaconStringEncoder(BeaconStringEncoderFunc(func(context.Context, Value, Value) ([]byte, error) {
				calls++
				return nil, boundaryErr
			})))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), "bof_pack", String("B"), String("z"), String("text"))
			if !errors.Is(err, boundaryErr) || !result.IsNull() {
				t.Fatalf("Invoke = (%s, %v), want null/wrapped %v", result.Describe(), err, boundaryErr)
			}
			_, err = runtimeInstance.Eval(context.Background(), "encoder-boundary-error.cna", `bof_pack("B", "z", "text");`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("script error = %v, want wrapped authoritative %v", err, boundaryErr)
			}
			if calls != 2 {
				t.Fatalf("encoder calls = %d, want two", calls)
			}
		})
	}
}

func TestAggressorBOFPackHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("before packing", func(t *testing.T) {
		called := false
		runtime := newAggressorBOFPackTestRuntime(BeaconStringEncoderFunc(func(
			context.Context,
			Value,
			Value,
		) ([]byte, error) {
			called = true
			return []byte("unreachable"), nil
		}))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := runtime.builtinAggressorBOFPack(ctx,
			bofPackTestInvocation(String("beacon"), String("z"), String("text")))
		if !errors.Is(err, context.Canceled) || called {
			t.Fatalf("pre-canceled bof_pack error/called = %v/%v, want context.Canceled/false", err, called)
		}
	})

	t.Run("during custom encoding", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		runtime := newAggressorBOFPackTestRuntime(BeaconStringEncoderFunc(func(
			context.Context,
			Value,
			Value,
		) ([]byte, error) {
			cancel()
			return []byte("encoded"), nil
		}))
		_, err := runtime.builtinAggressorBOFPack(ctx,
			bofPackTestInvocation(String("beacon"), String("z"), String("text")))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled encoder bof_pack error = %v, want context.Canceled", err)
		}
	})
}

func TestAggressorBOFPackRejectsInvalidFormatAndArity(t *testing.T) {
	t.Parallel()

	runtime := newAggressorBOFPackTestRuntime(utf8BeaconStringEncoder{})
	tests := []struct {
		name     string
		values   []Value
		position int
		contains string
	}{
		{name: "missing all", contains: "requires a beacon ID and format"},
		{name: "missing format", values: []Value{String("beacon")}, contains: "requires a beacon ID and format"},
		{name: "missing value", values: []Value{String("beacon"), String("bi"), String("bytes")}, contains: "exactly 2 value argument(s), received 1"},
		{name: "extra value", values: []Value{String("beacon"), String("b"), String("bytes"), Int(1)}, contains: "exactly 1 value argument(s), received 2"},
		{name: "unsupported ASCII", values: []Value{String("beacon"), String("x"), Int(1)}, position: 2, contains: "format character 1 ('x') is unsupported"},
		{name: "unsupported Unicode", values: []Value{String("beacon"), String("λ"), Int(1)}, position: 2, contains: "format character 1 ('λ') is unsupported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := runtime.builtinAggressorBOFPack(context.Background(), bofPackTestInvocation(test.values...))
			var argumentErr *PortableUtilityArgumentError
			if !errors.As(err, &argumentErr) {
				t.Fatalf("bof_pack error = %T %v, want *PortableUtilityArgumentError", err, err)
			}
			if argumentErr.Function != "bof_pack" || argumentErr.Position != test.position ||
				!strings.Contains(err.Error(), test.contains) {
				t.Fatalf("bof_pack error = %#v (%v), want function bof_pack, position %d, containing %q",
					argumentErr, err, test.position, test.contains)
			}
		})
	}

	empty := mustCallAggressorBOFPack(t, runtime, context.Background(), String("beacon"), String(""))
	if emptyBytes, ok := empty.Bytes(); !ok || len(emptyBytes) != 0 {
		t.Fatalf("empty bof_pack result = %x/string=%v, want empty string",
			emptyBytes, ok)
	}
}

func TestAggressorBOFPackFieldLengthsAreUint32Bounded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		format     uint16
		payload    uint64
		terminator uint64
	}{
		{name: "b payload", format: 'b', payload: uint64(math.MaxUint32) + 1},
		{name: "z terminator", format: 'z', payload: math.MaxUint32, terminator: 1},
		{name: "Z terminator", format: 'Z', payload: math.MaxUint32 - 1, terminator: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := bofPackFieldLength("bof_pack", 3, test.format, test.payload, test.terminator)
			var argumentErr *PortableUtilityArgumentError
			if !errors.As(err, &argumentErr) || argumentErr.Position != 3 ||
				!strings.Contains(err.Error(), "exceeds the uint32 maximum") {
				t.Fatalf("oversized field error = %T %v, want typed argument 3 uint32 error", err, err)
			}
		})
	}

	if length, err := bofPackFieldLength("bof_pack", 3, 'z', math.MaxUint32-1, 1); err != nil || length != math.MaxUint32 {
		t.Fatalf("maximum z field length = %d, %v; want %d, nil", length, err, uint64(math.MaxUint32))
	}
}

func bofPackTestInvocation(values ...Value) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{Name: "bof_pack", Arguments: arguments}
}

func mustCallAggressorBOFPack(
	t *testing.T,
	runtime *Runtime,
	ctx context.Context,
	values ...Value,
) Value {
	t.Helper()
	value, err := runtime.builtinAggressorBOFPack(ctx, bofPackTestInvocation(values...))
	if err != nil {
		t.Fatalf("bof_pack: %v", err)
	}
	return value
}
