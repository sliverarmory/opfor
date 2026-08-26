package opfor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAggressorPEFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{
		"pe_mask",
		"pe_mask_string",
		"pe_set_compile_time_with_long",
		"pe_set_long",
		"pe_set_short",
		"pe_set_string",
		"pe_set_stringz",
		"pe_stomp",
		"pe_update_checksum",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor PE function names = %q, want %q", names, want)
	}
}

func TestAggressorPECompileTimeWithLongUsesCOFFSeconds(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()
	for _, test := range []struct {
		name         string
		magic        uint16
		milliseconds int64
		wantSeconds  uint32
	}{
		{
			name:         "PE32 first official example",
			magic:        aggressorPE32Magic,
			milliseconds: 1_893_521_594_000,
			wantSeconds:  1_893_521_594,
		},
		{
			name:         "PE32+ second official example",
			magic:        aggressorPE32PlusMagic,
			milliseconds: 1_700_000_001_000,
			wantSeconds:  1_700_000_001,
		},
		{
			name:         "negative milliseconds truncate toward zero and narrow",
			magic:        aggressorPE32Magic,
			milliseconds: -1_001,
			wantSeconds:  uint32(0xffffffff),
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			fixture := aggressorPETestLayoutForMagic(test.magic)
			inputBytes := makeAggressorPETestImage(test.magic, 0)
			original := bytes.Clone(inputBytes)
			input := BinaryString(inputBytes)
			result := callAggressorPE(
				t,
				functions,
				"pe_set_compile_time_with_long",
				input,
				Long(test.milliseconds),
			)

			parsed, err := parseAggressorPELayout(
				aggressorPEInvocation("pe_set_compile_time_with_long"),
				original,
			)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.timeDateStampOffset != fixture.timestampOffset || parsed.checksumOffset != fixture.checksumOffset {
				t.Fatalf("parsed offsets = timestamp %#x/checksum %#x, want literal fixture offsets %#x/%#x",
					parsed.timeDateStampOffset, parsed.checksumOffset, fixture.timestampOffset, fixture.checksumOffset)
			}
			want := bytes.Clone(original)
			binary.LittleEndian.PutUint32(
				want[fixture.timestampOffset:fixture.timestampOffset+4],
				test.wantSeconds,
			)
			assertAggressorPEBytes(t, result, want)
			if got := binary.LittleEndian.Uint32(want[fixture.checksumOffset : fixture.checksumOffset+4]); got != 0xaabbccdd {
				t.Fatalf("compile-time helper changed checksum to %#x", got)
			}
			gotInput, _ := input.Bytes()
			if !bytes.Equal(gotInput, original) {
				t.Fatal("compile-time helper mutated its caller-owned input")
			}
		})
	}
}

func TestAggressorPEUpdateChecksumReferenceVectors(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()
	for _, test := range []struct {
		name         string
		magic        uint16
		wantChecksum uint32
	}{
		{name: "even-length PE32", magic: aggressorPE32Magic, wantChecksum: 0x40ec},
		{name: "odd-length PE32+ with overlay", magic: aggressorPE32PlusMagic, wantChecksum: 0xc7ff},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			fixture := aggressorPETestLayoutForMagic(test.magic)
			inputBytes := makeAggressorPETestImage(test.magic, 0)
			original := bytes.Clone(inputBytes)
			parsed, err := parseAggressorPELayout(aggressorPEInvocation("pe_update_checksum"), original)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.timeDateStampOffset != fixture.timestampOffset || parsed.checksumOffset != fixture.checksumOffset {
				t.Fatalf("parsed offsets = timestamp %#x/checksum %#x, want literal fixture offsets %#x/%#x",
					parsed.timeDateStampOffset, parsed.checksumOffset, fixture.timestampOffset, fixture.checksumOffset)
			}
			if len(original) != fixture.defaultLength {
				t.Fatalf("reference fixture length = %d, want %d", len(original), fixture.defaultLength)
			}

			calculated, err := aggressorPEImageChecksum(context.Background(), original, fixture.checksumOffset)
			if err != nil || calculated != test.wantChecksum {
				t.Fatalf("checksum = %#x, %v; want %#x", calculated, err, test.wantChecksum)
			}
			withDifferentPriorChecksum := bytes.Clone(original)
			binary.LittleEndian.PutUint32(
				withDifferentPriorChecksum[fixture.checksumOffset:fixture.checksumOffset+4],
				0x01020304,
			)
			independentOfOldField, err := aggressorPEImageChecksum(
				context.Background(),
				withDifferentPriorChecksum,
				fixture.checksumOffset,
			)
			if err != nil || independentOfOldField != test.wantChecksum {
				t.Fatalf("checksum with different prior field = %#x, %v; want %#x",
					independentOfOldField, err, test.wantChecksum)
			}
			input := BinaryString(original)
			result := callAggressorPE(t, functions, "pe_update_checksum", input)
			want := bytes.Clone(original)
			binary.LittleEndian.PutUint32(want[fixture.checksumOffset:fixture.checksumOffset+4], test.wantChecksum)
			assertAggressorPEBytes(t, result, want)

			// The checksum field is logically zero while calculating, so updating
			// an already-updated image is byte-for-byte idempotent.
			again := callAggressorPE(t, functions, "pe_update_checksum", result)
			assertAggressorPEBytes(t, again, want)
			gotInput, _ := input.Bytes()
			if !bytes.Equal(gotInput, original) || !bytes.Equal(inputBytes, original) {
				t.Fatal("checksum helper mutated caller-owned input")
			}
		})
	}
}

func TestAggressorPEAwareHelpersRejectMalformedImages(t *testing.T) {
	t.Parallel()

	fixture := aggressorPETestLayoutForMagic(aggressorPE32Magic)
	valid := makeAggressorPETestImage(aggressorPE32Magic, 0)
	validPlus := makeAggressorPETestImage(aggressorPE32PlusMagic, 0)
	const optionalSizeOffset = 0x54
	mutate := func(change func([]byte)) []byte {
		content := bytes.Clone(valid)
		change(content)
		return content
	}
	mutatePlus := func(change func([]byte)) []byte {
		content := bytes.Clone(validPlus)
		change(content)
		return content
	}
	overlappingDOSHeader := mutate(func(content []byte) {
		copy(content[0x20:0x38], content[0x40:0x58])
		binary.LittleEndian.PutUint16(content[0x34:], 0x60)
		binary.LittleEndian.PutUint16(content[0x38:], 0x10b)
		binary.LittleEndian.PutUint32(content[0x3c:], 0x20)
	})
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "truncated DOS header", content: valid[:0x3f]},
		{name: "bad MZ signature", content: mutate(func(content []byte) { content[0] = 'N' })},
		{name: "PE header overlaps DOS header", content: overlappingDOSHeader},
		{name: "PE offset beyond content", content: mutate(func(content []byte) {
			binary.LittleEndian.PutUint32(content[0x3c:], uint32(len(content)+1))
		})},
		{name: "maximum PE offset", content: mutate(func(content []byte) {
			binary.LittleEndian.PutUint32(content[0x3c:], ^uint32(0))
		})},
		{name: "truncated PE signature", content: valid[:0x42]},
		{name: "truncated COFF header", content: valid[:0x57]},
		{name: "bad PE signature", content: mutate(func(content []byte) { content[fixture.peHeaderOffset] = 'Q' })},
		{name: "missing optional header", content: mutate(func(content []byte) {
			binary.LittleEndian.PutUint16(content[optionalSizeOffset:], 0)
		})},
		{name: "truncated declared optional header", content: valid[:fixture.optionalHeaderOffset+0x60-1]},
		{name: "unsupported optional magic", content: mutate(func(content []byte) {
			binary.LittleEndian.PutUint16(content[fixture.optionalHeaderOffset:], 0x107)
		})},
		{name: "undersized PE32 optional header", content: mutate(func(content []byte) {
			binary.LittleEndian.PutUint16(content[optionalSizeOffset:], 0x60-1)
		})},
		{name: "undersized PE32+ optional header", content: mutatePlus(func(content []byte) {
			binary.LittleEndian.PutUint16(content[0x94:], 0x70-1)
		})},
	}

	functions := (&Runtime{}).aggressorPEFunctions()
	for _, test := range tests {
		for _, function := range []string{"pe_set_compile_time_with_long", "pe_update_checksum"} {
			t.Run(test.name+"/"+function, func(t *testing.T) {
				input := BinaryString(test.content)
				before, _ := input.Bytes()
				arguments := []Value{input}
				if function == "pe_set_compile_time_with_long" {
					arguments = append(arguments, Long(0))
				}
				result, err := functions[function](context.Background(), aggressorPEInvocation(function, arguments...))
				if err == nil || !result.IsNull() {
					t.Fatalf("%s malformed result = (%s, %v), want null/error", function, result.Describe(), err)
				}
				var argumentErr *PortableUtilityArgumentError
				if !errors.As(err, &argumentErr) || argumentErr.Position != 1 || argumentErr.Function != function {
					t.Fatalf("%s malformed error = %T %#v, want argument 1 PortableUtilityArgumentError", function, err, err)
				}
				after, _ := input.Bytes()
				if !bytes.Equal(after, before) {
					t.Fatalf("%s malformed-input error mutated input from %x to %x", function, before, after)
				}
			})
		}
	}
}

func TestAggressorPEAwareHelpersHonorContextCancellation(t *testing.T) {
	t.Parallel()

	content := makeAggressorPETestImage(aggressorPE32Magic, aggressorUtilityChunkSize*2+1)
	functions := (&Runtime{}).aggressorPEFunctions()
	for _, test := range []struct {
		name      string
		cancelAt  int32
		arguments func(Value) []Value
	}{
		{
			name:      "pe_set_compile_time_with_long",
			cancelAt:  6,
			arguments: func(input Value) []Value { return []Value{input, Long(1_700_000_001_000)} },
		},
		{
			name:      "pe_update_checksum",
			cancelAt:  7,
			arguments: func(input Value) []Value { return []Value{input} },
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			original := bytes.Clone(content)
			input := BinaryString(content)
			ctx := newAggressorPECheckCancelContext(test.cancelAt)
			result, err := functions[test.name](ctx, aggressorPEInvocation(test.name, test.arguments(input)...))
			if !errors.Is(err, context.Canceled) || !result.IsNull() {
				t.Fatalf("cancellation = (%s, %v), want null/context.Canceled", result.Describe(), err)
			}
			got, _ := input.Bytes()
			if !bytes.Equal(got, original) {
				t.Fatal("canceled PE-aware helper mutated its input")
			}
		})
	}
}

func TestAggressorPEMutatorsAreRegisteredAndImporterOverrideWins(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	masked, err := runtimeInstance.Invoke(
		context.Background(),
		"pe_mask",
		BinaryString([]byte{0x10, 0x20}),
		Int(0),
		Int(2),
		Int(0xff),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertAggressorPEBytes(t, masked, []byte{0xef, 0xdf})

	overridden, err := New(WithFunction("pe_mask", func(context.Context, Invocation) (Value, error) {
		return String("importer override"), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = overridden.Close(context.Background()) })
	value, err := overridden.Invoke(context.Background(), "pe_mask")
	if err != nil || value.String() != "importer override" {
		t.Fatalf("overridden pe_mask = (%s, %v), want importer override", value.Describe(), err)
	}
}

func TestAggressorPEMutatorsDocumentedByteSemantics(t *testing.T) {
	t.Parallel()

	// These cases cover the official successful-operation contracts. XOR,
	// endian, narrowing, and Unicode low-byte details are the provisional
	// compatibility interpretations documented in builtins_aggressor_pe.go.
	tests := []struct {
		name      string
		function  string
		input     []byte
		arguments []Value
		want      []byte
	}{
		{
			name:      "mask exact range",
			function:  "pe_mask",
			input:     []byte{0x10, 0x20, 0x30, 0x40},
			arguments: []Value{Int(1), Int(2), Int(0xff)},
			want:      []byte{0x10, 0xdf, 0xcf, 0x40},
		},
		{
			name:      "mask string includes first NUL",
			function:  "pe_mask_string",
			input:     []byte{'x', 'A', 'B', 0, 'C'},
			arguments: []Value{Int(1), Int(0x20)},
			want:      []byte{'x', 'a', 'b', 0x20, 'C'},
		},
		{
			name:      "set DWORD little endian",
			function:  "pe_set_long",
			input:     []byte{0, 0, 0, 0, 0, 0},
			arguments: []Value{Int(1), Long(0x11223344)},
			want:      []byte{0, 0x44, 0x33, 0x22, 0x11, 0},
		},
		{
			name:      "set WORD little endian",
			function:  "pe_set_short",
			input:     []byte{0, 0, 0, 0},
			arguments: []Value{Int(1), Int(0x3344)},
			want:      []byte{0, 0x44, 0x33, 0},
		},
		{
			name:      "set string without terminator",
			function:  "pe_set_string",
			input:     []byte{0xff, 0xff, 0xff, 0xff, 0xff},
			arguments: []Value{Int(1), String("\u20acA")},
			want:      []byte{0xff, 0xac, 0x41, 0xff, 0xff},
		},
		{
			name:      "set string with terminator",
			function:  "pe_set_stringz",
			input:     []byte{0xff, 0xff, 0xff, 0xff, 0xff},
			arguments: []Value{Int(1), String("\u20acA")},
			want:      []byte{0xff, 0xac, 0x41, 0, 0xff},
		},
		{
			name:      "stomp through first terminator",
			function:  "pe_stomp",
			input:     []byte{'X', 'A', 'B', 0, 'C'},
			arguments: []Value{Int(1)},
			want:      []byte{'X', 0, 0, 0, 'C'},
		},
	}

	functions := (&Runtime{}).aggressorPEFunctions()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalBytes := bytes.Clone(test.input)
			input := BinaryString(test.input)
			arguments := append([]Value{input}, test.arguments...)
			got := callAggressorPE(t, functions, test.function, arguments...)
			assertAggressorPEBytes(t, got, test.want)

			if !bytes.Equal(test.input, originalBytes) {
				t.Fatalf("caller bytes mutated to %x, want original %x", test.input, originalBytes)
			}
			inputBytes, ok := input.Bytes()
			if !ok || !bytes.Equal(inputBytes, originalBytes) {
				t.Fatalf("input Value mutated to %x/string=%v, want %x", inputBytes, ok, originalBytes)
			}
		})
	}
}

func TestAggressorPEMaskCoercionInvolutionAndBoundaries(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()
	input := BinaryString([]byte{0x10, 0x20, 0x30, 0x40})

	// Decimal-string and double inputs follow Sleep intValue coercion; a
	// negative key narrows to its low byte.
	masked := callAggressorPE(t, functions, "pe_mask", input, String("1"), Double(2.9), Int(-1))
	assertAggressorPEBytes(t, masked, []byte{0x10, 0xdf, 0xcf, 0x40})
	restored := callAggressorPE(t, functions, "pe_mask", masked, Int(1), Int(2), Int(-1))
	assertAggressorPEBytes(t, restored, []byte{0x10, 0x20, 0x30, 0x40})

	// A zero-width range at the exact end is valid. Numeric NUL/bogus values
	// coerce to zero and a key of 256 truncates to zero.
	zeroWidth := callAggressorPE(t, functions, "pe_mask", input, String("4"), Null(), String("256"))
	assertAggressorPEBytes(t, zeroWidth, []byte{0x10, 0x20, 0x30, 0x40})
	bogusOffsetAndNullKey := callAggressorPE(
		t,
		functions,
		"pe_mask",
		input,
		String("not-a-number"),
		String("1"),
		Null(),
	)
	assertAggressorPEBytes(t, bogusOffsetAndNullKey, []byte{0x10, 0x20, 0x30, 0x40})

	// A 64-bit value is narrowed through Sleep's signed int32 boundary before
	// its low byte is used.
	longKey := callAggressorPE(t, functions, "pe_mask", input, Int(0), Int(1), Long(0x1_000000ff))
	assertAggressorPEBytes(t, longKey, []byte{0xef, 0x20, 0x30, 0x40})

	// Binary provenance is carried per UTF-16 unit. An empty BinaryString
	// result is still the requested empty string, but has no unit on which an
	// IsBinaryString marker could be retained.
	empty := callAggressorPE(
		t,
		functions,
		"pe_mask",
		BinaryString(nil),
		Int(0),
		Int(0),
		Int(1),
	)
	assertAggressorPEBytes(t, empty, nil)
	if empty.IsBinaryString() {
		t.Fatal("empty pe_mask result unexpectedly has a per-unit binary marker")
	}
}

func TestAggressorPELowBytesMatchesSleepSemantics(t *testing.T) {
	t.Parallel()

	boundaryText := strings.Repeat("A", aggressorUtilityChunkSize-1) + "😀"
	tests := []struct {
		name  string
		value Value
	}{
		{name: "ordinary text", value: String("Aé\u20ac😀\x00")},
		{name: "binary octets", value: BinaryString([]byte{0, 0x7f, 0x80, 0xff})},
		{name: "invalid UTF-8 String octets", value: String(string([]byte{0xff, 0xc3, 'A'}))},
		{name: "numeric coercion", value: Long(-12345)},
		{name: "null coercion", value: Null()},
		{name: "surrogate pair crosses chunk", value: String(boundaryText)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := aggressorPELowBytes(context.Background(), test.value)
			if err != nil {
				t.Fatalf("aggressorPELowBytes: %v", err)
			}
			want := sleepStringLowBytes(test.value)
			if !bytes.Equal(got, want) {
				t.Fatalf("low bytes = %x, want Sleep low bytes %x", got, want)
			}
		})
	}
}

func TestAggressorPENumericSetterCoercionAndTruncation(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()
	tests := []struct {
		name     string
		function string
		input    []byte
		offset   Value
		value    Value
		want     []byte
	}{
		{
			name:     "long truncates high bits",
			function: "pe_set_long",
			input:    []byte{0, 0, 0, 0},
			offset:   String("0"),
			value:    Long(0x1_11223344),
			want:     []byte{0x44, 0x33, 0x22, 0x11},
		},
		{
			name:     "long negative one",
			function: "pe_set_long",
			input:    []byte{0, 0, 0, 0},
			offset:   Null(),
			value:    Int(-1),
			want:     []byte{0xff, 0xff, 0xff, 0xff},
		},
		{
			name:     "long invalid string coerces to zero",
			function: "pe_set_long",
			input:    []byte{0xff, 0xff, 0xff, 0xff},
			offset:   Int(0),
			value:    String("invalid"),
			want:     []byte{0, 0, 0, 0},
		},
		{
			name:     "short truncates high bits",
			function: "pe_set_short",
			input:    []byte{0, 0},
			offset:   Double(0.9),
			value:    Long(0x1_3344),
			want:     []byte{0x44, 0x33},
		},
		{
			name:     "short negative one",
			function: "pe_set_short",
			input:    []byte{0, 0},
			offset:   Int(0),
			value:    Int(-1),
			want:     []byte{0xff, 0xff},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := callAggressorPE(
				t,
				functions,
				test.function,
				BinaryString(test.input),
				test.offset,
				test.value,
			)
			assertAggressorPEBytes(t, got, test.want)
		})
	}
}

func TestAggressorPEStringSetterProvisionalNULEdges(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()

	// Embedded NULs are treated as ordinary bytes and stringz appends one more
	// terminator. This is deliberately tested as provisional OPFOR policy.
	raw := BinaryString([]byte{0x80, 0, 0xff})
	withoutTerminator := callAggressorPE(
		t,
		functions,
		"pe_set_string",
		BinaryString(bytes.Repeat([]byte{0xee}, 5)),
		Int(1),
		raw,
	)
	assertAggressorPEBytes(t, withoutTerminator, []byte{0xee, 0x80, 0, 0xff, 0xee})

	withTerminator := callAggressorPE(
		t,
		functions,
		"pe_set_stringz",
		BinaryString(bytes.Repeat([]byte{0xee}, 5)),
		Int(1),
		raw,
	)
	assertAggressorPEBytes(t, withTerminator, []byte{0xee, 0x80, 0, 0xff, 0})

	// A supplementary Unicode character becomes the low bytes of both Java
	// UTF-16 surrogate code units.
	surrogateBytes := callAggressorPE(
		t,
		functions,
		"pe_set_string",
		BinaryString([]byte{0xff, 0xff}),
		Int(0),
		String("😀"),
	)
	assertAggressorPEBytes(t, surrogateBytes, []byte{0x3d, 0})

	// Empty pe_set_string is a zero-width exact-end write. Empty stringz still
	// requires and writes one byte for its terminator.
	exactEnd := callAggressorPE(
		t,
		functions,
		"pe_set_string",
		BinaryString([]byte{'A'}),
		Int(1),
		String(""),
	)
	assertAggressorPEBytes(t, exactEnd, []byte{'A'})
	emptyStringZ := callAggressorPE(
		t,
		functions,
		"pe_set_stringz",
		BinaryString([]byte{'A', 0xff}),
		Int(1),
		String(""),
	)
	assertAggressorPEBytes(t, emptyStringZ, []byte{'A', 0})
}

func TestAggressorPEStringScanBoundariesAndMissingTerminator(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()
	source := BinaryString([]byte{'P', 0, 'Q', 0})

	// An empty string masks its one original terminator byte. The documented
	// unmasking pattern uses pe_mask with that known one-byte range.
	masked := callAggressorPE(t, functions, "pe_mask_string", source, Int(1), Int(0x7f))
	assertAggressorPEBytes(t, masked, []byte{'P', 0x7f, 'Q', 0})
	unmasked := callAggressorPE(t, functions, "pe_mask", masked, Int(1), Int(1), Int(0x7f))
	assertAggressorPEBytes(t, unmasked, []byte{'P', 0, 'Q', 0})

	// Stomping from an existing terminator is an unchanged binary result.
	stomped := callAggressorPE(t, functions, "pe_stomp", source, Int(1))
	assertAggressorPEBytes(t, stomped, []byte{'P', 0, 'Q', 0})

	for _, name := range []string{"pe_mask_string", "pe_stomp"} {
		t.Run(name+" missing terminator", func(t *testing.T) {
			input := BinaryString([]byte{'A', 'B'})
			arguments := []Value{input, Int(0)}
			if name == "pe_mask_string" {
				arguments = append(arguments, Int(1))
			}
			value, err := functions[name](context.Background(), aggressorPEInvocation(name, arguments...))
			if err == nil || !value.IsNull() {
				t.Fatalf("%s missing terminator = %s, %v; want null error", name, value.Describe(), err)
			}
			assertAggressorPEArgumentError(t, err, name, 1)
			inputBytes, _ := input.Bytes()
			if !bytes.Equal(inputBytes, []byte{'A', 'B'}) {
				t.Fatalf("%s mutated unterminated input to %x", name, inputBytes)
			}
		})

		t.Run(name+" exact-end start", func(t *testing.T) {
			arguments := []Value{BinaryString([]byte{'A', 0}), Int(2)}
			if name == "pe_mask_string" {
				arguments = append(arguments, Int(1))
			}
			value, err := functions[name](context.Background(), aggressorPEInvocation(name, arguments...))
			if err == nil || !value.IsNull() {
				t.Fatalf("%s exact-end start = %s, %v; want null error", name, value.Describe(), err)
			}
			assertAggressorPEArgumentError(t, err, name, 2)
		})
	}
}

func TestAggressorPECheckedNoExtensionBounds(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()
	tests := []struct {
		name      string
		function  string
		arguments []Value
		position  int
	}{
		{
			name:      "mask negative start",
			function:  "pe_mask",
			arguments: []Value{BinaryString([]byte{0}), Int(-1), Int(0), Int(1)},
			position:  2,
		},
		{
			name:      "mask negative length",
			function:  "pe_mask",
			arguments: []Value{BinaryString([]byte{0}), Int(0), Int(-1), Int(1)},
			position:  3,
		},
		{
			name:      "mask start beyond end even when empty",
			function:  "pe_mask",
			arguments: []Value{BinaryString([]byte{0}), Int(2), Int(0), Int(1)},
			position:  2,
		},
		{
			name:      "mask range crosses end",
			function:  "pe_mask",
			arguments: []Value{BinaryString([]byte{0, 0, 0}), Int(2), Int(2), Int(1)},
			position:  3,
		},
		{
			name:      "long fixed write crosses end",
			function:  "pe_set_long",
			arguments: []Value{BinaryString([]byte{0, 0, 0, 0}), Int(1), Int(1)},
			position:  2,
		},
		{
			name:      "short fixed write crosses end",
			function:  "pe_set_short",
			arguments: []Value{BinaryString([]byte{0, 0}), Int(1), Int(1)},
			position:  2,
		},
		{
			name:      "string value crosses end",
			function:  "pe_set_string",
			arguments: []Value{BinaryString([]byte{0, 0, 0}), Int(2), String("AB")},
			position:  3,
		},
		{
			name:      "stringz terminator crosses end",
			function:  "pe_set_stringz",
			arguments: []Value{BinaryString([]byte{0, 0, 0}), Int(2), String("A")},
			position:  3,
		},
		{
			name:      "stringz empty value at exact end",
			function:  "pe_set_stringz",
			arguments: []Value{BinaryString([]byte{0}), Int(1), String("")},
			position:  3,
		},
		{
			name:      "stomp negative start",
			function:  "pe_stomp",
			arguments: []Value{BinaryString([]byte{0}), Int(-1)},
			position:  2,
		},
		{
			name:      "mask huge offset cannot overflow",
			function:  "pe_mask",
			arguments: []Value{BinaryString([]byte{0}), Int(1<<31 - 1), Int(1), Int(1)},
			position:  2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := functions[test.function](
				context.Background(),
				aggressorPEInvocation(test.function, test.arguments...),
			)
			if err == nil || !value.IsNull() {
				t.Fatalf("%s = %s, %v; want null argument error", test.function, value.Describe(), err)
			}
			assertAggressorPEArgumentError(t, err, test.function, test.position)
		})
	}
}

func TestAggressorPEStrictDocumentedArity(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()
	tests := []struct {
		name      string
		arguments []Value
	}{
		{"pe_mask", []Value{BinaryString([]byte{0}), Int(0), Int(0), Int(0)}},
		{"pe_mask_string", []Value{BinaryString([]byte{0}), Int(0), Int(0)}},
		{"pe_set_compile_time_with_long", []Value{BinaryString([]byte{0}), Long(0)}},
		{"pe_set_long", []Value{BinaryString([]byte{0, 0, 0, 0}), Int(0), Int(0)}},
		{"pe_set_short", []Value{BinaryString([]byte{0, 0}), Int(0), Int(0)}},
		{"pe_set_string", []Value{BinaryString([]byte{0}), Int(0), String("")}},
		{"pe_set_stringz", []Value{BinaryString([]byte{0}), Int(0), String("")}},
		{"pe_stomp", []Value{BinaryString([]byte{0}), Int(0)}},
		{"pe_update_checksum", []Value{BinaryString([]byte{0})}},
	}

	for _, test := range tests {
		for _, arguments := range [][]Value{
			test.arguments[:len(test.arguments)-1],
			append(append([]Value(nil), test.arguments...), Null()),
		} {
			value, err := functions[test.name](
				context.Background(),
				aggressorPEInvocation(test.name, arguments...),
			)
			if err == nil || !value.IsNull() {
				t.Errorf("%s with %d arguments = %s, %v; want null exact-arity error",
					test.name, len(arguments), value.Describe(), err)
			}
		}
	}
}

func TestAggressorPELongOperationsHonorContextCancellation(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()
	terminated := append(bytes.Repeat([]byte{'A'}, aggressorUtilityChunkSize*2), 0)
	replacement := BinaryString(bytes.Repeat([]byte{'R'}, aggressorUtilityChunkSize+1))
	tests := []struct {
		name      string
		content   []byte
		cancelAt  int32
		arguments func(Value) []Value
	}{
		{
			name:     "pe_mask",
			content:  terminated,
			cancelAt: 7,
			arguments: func(input Value) []Value {
				return []Value{input, Int(0), Int(int32(len(terminated))), Int(1)}
			},
		},
		{
			name:      "pe_mask_string",
			content:   terminated,
			cancelAt:  10,
			arguments: func(input Value) []Value { return []Value{input, Int(0), Int(1)} },
		},
		{
			name:      "pe_stomp",
			content:   terminated,
			cancelAt:  10,
			arguments: func(input Value) []Value { return []Value{input, Int(0)} },
		},
		{
			name:      "pe_set_string",
			content:   bytes.Repeat([]byte{'D'}, aggressorUtilityChunkSize+1),
			cancelAt:  9,
			arguments: func(input Value) []Value { return []Value{input, Int(0), replacement} },
		},
		{
			name:      "pe_set_stringz",
			content:   bytes.Repeat([]byte{'D'}, aggressorUtilityChunkSize+2),
			cancelAt:  9,
			arguments: func(input Value) []Value { return []Value{input, Int(0), replacement} },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := bytes.Clone(test.content)
			input := BinaryString(original)
			ctx := newAggressorPECheckCancelContext(test.cancelAt)
			value, err := functions[test.name](ctx, aggressorPEInvocation(test.name, test.arguments(input)...))
			if !errors.Is(err, context.Canceled) || !value.IsNull() {
				t.Fatalf("%s cancellation = %s, %v; want null context.Canceled", test.name, value.Describe(), err)
			}
			got, _ := input.Bytes()
			if !bytes.Equal(got, original) {
				t.Fatalf("%s cancellation mutated input from %x to %x", test.name, original, got)
			}
		})
	}
}

func TestAggressorPEConversionAndFixedSetterCancellation(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEFunctions()

	t.Run("pre-canceled conversion", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got, err := aggressorPELowBytes(ctx, BinaryString(bytes.Repeat([]byte{'A'}, aggressorUtilityChunkSize+1)))
		if !errors.Is(err, context.Canceled) || got != nil {
			t.Fatalf("pre-canceled conversion = %x, %v; want nil/context.Canceled", got, err)
		}
	})

	t.Run("source conversion chunk", func(t *testing.T) {
		contentBytes := bytes.Repeat([]byte{'A'}, aggressorUtilityChunkSize+1)
		content := BinaryString(contentBytes)
		// Check 1 is argument preflight, check 2 is conversion preflight,
		// and check 3 begins the second conversion chunk.
		ctx := newAggressorPECheckCancelContext(3)
		value, err := functions["pe_mask"](ctx, aggressorPEInvocation(
			"pe_mask", content, Int(0), Int(0), Int(1),
		))
		if !errors.Is(err, context.Canceled) || !value.IsNull() {
			t.Fatalf("source conversion cancellation = %s, %v; want null/context.Canceled", value.Describe(), err)
		}
		got, _ := content.Bytes()
		if !bytes.Equal(got, contentBytes) {
			t.Fatalf("source conversion cancellation mutated input to %x", got)
		}
	})

	t.Run("replacement conversion chunk", func(t *testing.T) {
		replacement := String(strings.Repeat("R", aggressorUtilityChunkSize+1))
		// Checks 1-3 complete argument/source conversion, check 4 begins
		// replacement conversion, and check 5 begins its second chunk.
		ctx := newAggressorPECheckCancelContext(5)
		value, err := functions["pe_set_string"](ctx, aggressorPEInvocation(
			"pe_set_string", BinaryString([]byte{0xee}), Int(0), replacement,
		))
		if !errors.Is(err, context.Canceled) || !value.IsNull() {
			t.Fatalf("replacement conversion cancellation = %s, %v; want null/context.Canceled", value.Describe(), err)
		}
	})

	for _, test := range []struct {
		name    string
		content []byte
	}{
		{name: "pe_set_long", content: []byte{0, 0, 0, 0}},
		{name: "pe_set_short", content: []byte{0, 0}},
	} {
		t.Run(test.name+" after conversion", func(t *testing.T) {
			input := BinaryString(test.content)
			// Checks 1-3 complete argument/source conversion. The fixed-width
			// setter's own pre-write check observes cancellation at check 4.
			ctx := newAggressorPECheckCancelContext(4)
			value, err := functions[test.name](ctx, aggressorPEInvocation(
				test.name, input, Int(0), Int(-1),
			))
			if !errors.Is(err, context.Canceled) || !value.IsNull() {
				t.Fatalf("%s cancellation = %s, %v; want null/context.Canceled", test.name, value.Describe(), err)
			}
			got, _ := input.Bytes()
			if !bytes.Equal(got, test.content) {
				t.Fatalf("%s cancellation mutated input to %x", test.name, got)
			}
		})
	}
}

func FuzzAggressorPEMutatorsNoPanic(f *testing.F) {
	f.Add([]byte{'A', 0, 'B'}, int32(0), int32(1), int32(0xff))
	f.Add([]byte{}, int32(-1), int32(-1), int32(-1))
	f.Add([]byte{0x80, 0xff}, int32(1<<31-1), int32(1<<31-1), int32(0))

	functions := (&Runtime{}).aggressorPEFunctions()
	f.Fuzz(func(t *testing.T, data []byte, start, length, value int32) {
		original := bytes.Clone(data)
		stringValue := BinaryString([]byte{byte(value), 0})
		tests := []struct {
			name      string
			arguments []Value
		}{
			{"pe_mask", []Value{BinaryString(data), Int(start), Int(length), Int(value)}},
			{"pe_mask_string", []Value{BinaryString(data), Int(start), Int(value)}},
			{"pe_set_long", []Value{BinaryString(data), Int(start), Int(value)}},
			{"pe_set_short", []Value{BinaryString(data), Int(start), Int(value)}},
			{"pe_set_string", []Value{BinaryString(data), Int(start), stringValue}},
			{"pe_set_stringz", []Value{BinaryString(data), Int(start), stringValue}},
			{"pe_stomp", []Value{BinaryString(data), Int(start)}},
		}

		for _, test := range tests {
			got, err := functions[test.name](
				context.Background(),
				aggressorPEInvocation(test.name, test.arguments...),
			)
			if err != nil {
				var argumentErr *PortableUtilityArgumentError
				if !errors.As(err, &argumentErr) || !got.IsNull() {
					t.Fatalf("%s error = %T %v/result=%s, want portable argument error/null",
						test.name, err, err, got.Describe())
				}
				continue
			}
			gotBytes, ok := got.Bytes()
			if !ok || len(gotBytes) != len(data) {
				t.Fatalf("%s result = %x/string=%v, want %d-byte string", test.name, gotBytes, ok, len(data))
			}
			raw := sleepStringRawMask(got)
			if len(raw) != len(data) {
				t.Fatalf("%s raw provenance length = %d, want %d", test.name, len(raw), len(data))
			}
			for index, isRaw := range raw {
				if !isRaw {
					t.Fatalf("%s result unit %d lost binary provenance", test.name, index)
				}
			}
		}
		if !bytes.Equal(data, original) {
			t.Fatalf("mutators changed caller data from %x to %x", original, data)
		}
	})
}

func FuzzAggressorPEAwareHelpersNoPanic(f *testing.F) {
	f.Add(makeAggressorPETestImage(aggressorPE32Magic, 0))
	f.Add(makeAggressorPETestImage(aggressorPE32PlusMagic, 0))
	f.Add([]byte{})
	f.Add([]byte{'M', 'Z'})

	functions := (&Runtime{}).aggressorPEFunctions()
	f.Fuzz(func(t *testing.T, data []byte) {
		callerBytes := bytes.Clone(data)
		for _, test := range []struct {
			name      string
			arguments func(Value) []Value
		}{
			{
				name: "pe_set_compile_time_with_long",
				arguments: func(input Value) []Value {
					return []Value{input, Long(1_700_000_001_000)}
				},
			},
			{
				name:      "pe_update_checksum",
				arguments: func(input Value) []Value { return []Value{input} },
			},
		} {
			input := BinaryString(data)
			before, _ := input.Bytes()
			got, err := functions[test.name](
				context.Background(),
				aggressorPEInvocation(test.name, test.arguments(input)...),
			)
			if err != nil {
				var argumentErr *PortableUtilityArgumentError
				if !errors.As(err, &argumentErr) || !got.IsNull() {
					t.Fatalf("%s error = %T %v/result=%s, want portable argument error/null",
						test.name, err, err, got.Describe())
				}
			} else {
				gotBytes, ok := got.Bytes()
				if !ok || len(gotBytes) != len(data) {
					t.Fatalf("%s result = %x/string=%v, want %d-byte string",
						test.name, gotBytes, ok, len(data))
				}
				raw := sleepStringRawMask(got)
				for index, isRaw := range raw {
					if !isRaw {
						t.Fatalf("%s result unit %d lost binary provenance", test.name, index)
					}
				}
			}
			after, _ := input.Bytes()
			if !bytes.Equal(after, before) {
				t.Fatalf("%s changed input from %x to %x", test.name, before, after)
			}
		}
		if !bytes.Equal(data, callerBytes) {
			t.Fatalf("PE-aware helpers changed caller data from %x to %x", callerBytes, data)
		}
	})
}

// aggressorPETestLayout deliberately records literal offsets instead of using
// production parser constants. These compact fixtures make a shared-offset bug
// in the implementation and its tests observable.
type aggressorPETestLayout struct {
	peHeaderOffset       int
	coffHeaderOffset     int
	optionalHeaderOffset int
	timestampOffset      int
	checksumOffset       int
	optionalSize         int
	defaultLength        int
	machine              uint16
}

func aggressorPETestLayoutForMagic(optionalMagic uint16) aggressorPETestLayout {
	switch optionalMagic {
	case 0x10b:
		return aggressorPETestLayout{
			peHeaderOffset:       0x40,
			coffHeaderOffset:     0x44,
			optionalHeaderOffset: 0x58,
			timestampOffset:      0x48,
			checksumOffset:       0x98,
			optionalSize:         0x60,
			defaultLength:        0xb8,
			machine:              0x14c,
		}
	case 0x20b:
		return aggressorPETestLayout{
			peHeaderOffset:       0x80,
			coffHeaderOffset:     0x84,
			optionalHeaderOffset: 0x98,
			timestampOffset:      0x88,
			checksumOffset:       0xd8,
			optionalSize:         0x70,
			defaultLength:        0x109,
			machine:              0x8664,
		}
	default:
		panic("unsupported PE test fixture magic")
	}
}

func makeAggressorPETestImage(optionalMagic uint16, totalLength int) []byte {
	layout := aggressorPETestLayoutForMagic(optionalMagic)
	if totalLength < layout.defaultLength {
		totalLength = layout.defaultLength
	}
	content := make([]byte, totalLength)
	content[0], content[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(content[0x3c:], uint32(layout.peHeaderOffset))
	copy(content[layout.peHeaderOffset:], []byte{'P', 'E', 0, 0})
	binary.LittleEndian.PutUint16(content[layout.coffHeaderOffset:], layout.machine)
	binary.LittleEndian.PutUint32(content[layout.timestampOffset:], 0xdeadbeef)
	binary.LittleEndian.PutUint16(content[layout.coffHeaderOffset+16:], uint16(layout.optionalSize))
	binary.LittleEndian.PutUint16(content[layout.coffHeaderOffset+18:], 0x0002)
	binary.LittleEndian.PutUint16(content[layout.optionalHeaderOffset:], optionalMagic)
	binary.LittleEndian.PutUint32(content[layout.checksumOffset:], 0xaabbccdd)
	if optionalMagic == 0x20b || totalLength > layout.defaultLength {
		content[len(content)-1] = 0x5a
	}
	return content
}

func aggressorPEInvocation(name string, values ...Value) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{Name: name, Arguments: arguments}
}

func callAggressorPE(
	t *testing.T,
	functions map[string]NativeFunc,
	name string,
	values ...Value,
) Value {
	t.Helper()
	function := functions[name]
	if function == nil {
		t.Fatalf("Aggressor PE function %q is unavailable", name)
	}
	value, err := function(context.Background(), aggressorPEInvocation(name, values...))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return value
}

func assertAggressorPEBytes(t *testing.T, value Value, want []byte) {
	t.Helper()
	got, ok := value.Bytes()
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("result = %x/string=%v, want %x", got, ok, want)
	}
	raw := sleepStringRawMask(value)
	if len(raw) != len(want) {
		t.Fatalf("raw provenance length = %d, want %d", len(raw), len(want))
	}
	for index, isRaw := range raw {
		if !isRaw {
			t.Fatalf("result unit %d does not retain binary provenance", index)
		}
	}
}

func assertAggressorPEArgumentError(
	t *testing.T,
	err error,
	function string,
	position int,
) {
	t.Helper()
	var argumentErr *PortableUtilityArgumentError
	if !errors.As(err, &argumentErr) {
		t.Fatalf("error = %T %v, want *PortableUtilityArgumentError", err, err)
	}
	if argumentErr.Function != function || argumentErr.Position != position || argumentErr.Reason == "" {
		t.Fatalf("argument error = %#v, want function %q position %d with reason",
			argumentErr, function, position)
	}
}

type aggressorPECheckCancelContext struct {
	context.Context
	cancelAt int32
	checks   atomic.Int32
	done     chan struct{}
	once     sync.Once
}

func newAggressorPECheckCancelContext(cancelAt int32) *aggressorPECheckCancelContext {
	return &aggressorPECheckCancelContext{
		Context:  context.Background(),
		cancelAt: cancelAt,
		done:     make(chan struct{}),
	}
}

func (ctx *aggressorPECheckCancelContext) Done() <-chan struct{} { return ctx.done }

func (ctx *aggressorPECheckCancelContext) Err() error {
	if ctx.checks.Add(1) >= ctx.cancelAt {
		ctx.once.Do(func() { close(ctx.done) })
		return context.Canceled
	}
	return nil
}
