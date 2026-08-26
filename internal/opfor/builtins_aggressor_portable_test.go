package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAggressorPortableUtilityFunctionSetAndImporterOverride(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{"format_size", "gunzip", "gzip", "iprange", "powershell_command", "script_resource", "str_chunk", "str_decode", "str_encode", "str_xor", "transform"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("portable Aggressor utility names = %q, want %q", names, want)
	}

	override := String("importer iprange")
	runtime, err := New(
		WithStdout(io.Discard),
		WithFunction("iprange", func(context.Context, Invocation) (Value, error) { return override, nil }),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := runtime.Invoke(context.Background(), "iprange", String("ignored"))
	if err != nil {
		t.Fatalf("Invoke(iprange override): %v", err)
	}
	if !sleepStringValuesEqual(got, override) {
		t.Fatalf("iprange override = %s, want %s", got.Describe(), override.Describe())
	}
}

func TestAggressorScriptResourceUsesLoadedSourceAndAllowsOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceName := filepath.Join(root, "scripts", "main.cna")
	program, err := CompileString(sourceName, `return script_resource("assets/tool.bin");`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	value, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := filepath.Join(root, "scripts", "assets", "tool.bin")
	if value.String() != want {
		t.Fatalf("script_resource = %q, want %q", value.String(), want)
	}

	override := String("importer resource")
	overridden, err := New(WithFunction("script_resource", func(context.Context, Invocation) (Value, error) {
		return override, nil
	}))
	if err != nil {
		t.Fatalf("New override: %v", err)
	}
	t.Cleanup(func() { _ = overridden.Close(context.Background()) })
	got, err := overridden.Invoke(context.Background(), "script_resource", String("ignored"))
	if err != nil {
		t.Fatalf("Invoke(script_resource override): %v", err)
	}
	if !sleepStringValuesEqual(got, override) {
		t.Fatalf("script_resource override = %s, want %s", got.Describe(), override.Describe())
	}
}

func TestAggressorFormatSizeDocumentedExampleAndPortableEdges(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	// This is the exact compatibility guarantee documented by Fortra.
	if got := callAggressorPortableUtility(t, functions, "format_size", Int(1024)); got.String() != "1kb" {
		t.Fatalf("format_size(1024) = %s, want 1kb", got.Describe())
	}

	// Rounding, negative values, larger units, and missing arguments are
	// explicit deterministic OPFOR policy because the official page does not
	// specify those edges.
	tests := []struct {
		value []Value
		want  string
	}{
		{want: "0b"},
		{value: []Value{Null()}, want: "0b"},
		{value: []Value{String("1024")}, want: "1kb"},
		{value: []Value{Int(1023)}, want: "1023b"},
		{value: []Value{Int(1536)}, want: "1.5kb"},
		{value: []Value{Long(1024 * 1024)}, want: "1mb"},
		{value: []Value{Long(-1536)}, want: "-1.5kb"},
		{value: []Value{Double(1.236)}, want: "1.24b"},
	}
	for _, test := range tests {
		if got := callAggressorPortableUtility(t, functions, "format_size", test.value...); got.String() != test.want {
			t.Errorf("format_size(%v) = %s, want %q", test.value, got.Describe(), test.want)
		}
	}

	for _, nonFinite := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := functions["format_size"](context.Background(), aggressorPortableInvocation("format_size", Double(nonFinite)))
		assertPortableUtilityArgumentError(t, err, "format_size", 1)
	}
}

func TestAggressorIPRangeDocumentedFormsAndOrdering(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	sequential := func(prefix string, first, end int) []string {
		result := make([]string, 0, end-first)
		for value := first; value < end; value++ {
			result = append(result, fmt.Sprintf("%s%d", prefix, value))
		}
		return result
	}
	tests := []struct {
		name        string
		description string
		want        []string
	}{
		{
			name:        "single IPv4 address",
			description: "192.168.1.2",
			want:        []string{"192.168.1.2"},
		},
		{
			name:        "comma-separated IPv4 addresses",
			description: "192.168.1.1, 192.168.1.2",
			want:        []string{"192.168.1.1", "192.168.1.2"},
		},
		{
			name:        "IPv4 CIDR",
			description: "192.168.1.0/24",
			want:        sequential("192.168.1.", 0, 256),
		},
		{
			name:        "full IPv4 range excludes upper bound",
			description: "192.168.1.18-192.168.1.30",
			want:        sequential("192.168.1.", 18, 30),
		},
		{
			name:        "shortened final-octet range excludes upper bound",
			description: "192.168.1.18-30",
			want:        sequential("192.168.1.", 18, 30),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := callAggressorPortableUtility(t, functions, "iprange", String(test.description))
			if got := aggressorUtilityArrayStrings(t, value); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("iprange(%q) = %q, want %q", test.description, got, test.want)
			}
		})
	}

	combined := callAggressorPortableUtility(t, functions, "iprange",
		String(" 192.168.1.2 , 192.168.2.0/31, 192.168.3.1-3, 192.168.1.2 "))
	wantCombined := []string{
		"192.168.1.2",
		"192.168.2.0", "192.168.2.1",
		"192.168.3.1", "192.168.3.2",
		"192.168.1.2",
	}
	if got := aggressorUtilityArrayStrings(t, combined); !reflect.DeepEqual(got, wantCombined) {
		t.Fatalf("combined iprange order/duplicates = %q, want %q", got, wantCombined)
	}

	edges := []struct {
		description string
		want        []string
	}{
		{description: "192.168.1.99/30", want: []string{"192.168.1.96", "192.168.1.97", "192.168.1.98", "192.168.1.99"}},
		{description: "192.168.1.254-192.168.2.2", want: []string{"192.168.1.254", "192.168.1.255", "192.168.2.0", "192.168.2.1"}},
		{description: "255.255.255.255", want: []string{"255.255.255.255"}},
		{description: "255.255.255.255/32", want: []string{"255.255.255.255"}},
	}
	for _, test := range edges {
		value := callAggressorPortableUtility(t, functions, "iprange", String(test.description))
		if got := aggressorUtilityArrayStrings(t, value); !reflect.DeepEqual(got, test.want) {
			t.Errorf("iprange edge %q = %q, want %q", test.description, got, test.want)
		}
	}
}

func TestAggressorIPRangePortableInvalidAndExpansionPolicies(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	invalid := []string{
		"",
		" ",
		",",
		"192.168.1.1,",
		",192.168.1.1",
		"192.168.1.1,,192.168.1.2",
		"not-an-ip",
		"2001:db8::1",
		"192.168.1.0/33",
		"2001:db8::/64",
		"192.168.1.18-",
		"192.168.1.18-256",
		"192.168.1.18--30",
		"192.168.1.30-192.168.1.18",
		"192.168.1.18-192.168.1.18",
	}
	for _, description := range invalid {
		_, err := functions["iprange"](context.Background(), aggressorPortableInvocation("iprange", String(description)))
		assertPortableUtilityArgumentError(t, err, "iprange", 1)
	}

	maximum := callAggressorPortableUtility(t, functions, "iprange", String("10.20.0.0/16"))
	maximumValues := aggressorUtilityArrayStrings(t, maximum)
	if len(maximumValues) != aggressorIPRangeMaximumAddresses || maximumValues[0] != "10.20.0.0" || maximumValues[len(maximumValues)-1] != "10.20.255.255" {
		t.Fatalf("maximum iprange len/bounds = %d/%q/%q", len(maximumValues), maximumValues[0], maximumValues[len(maximumValues)-1])
	}
	for _, description := range []string{
		"10.20.0.0/15",
		"10.20.0.0/16, 192.168.1.1",
		"0.0.0.0-255.255.255.255",
	} {
		_, err := functions["iprange"](context.Background(), aggressorPortableInvocation("iprange", String(description)))
		assertPortableUtilityArgumentError(t, err, "iprange", 1)
		var argumentErr *PortableUtilityArgumentError
		if !errors.As(err, &argumentErr) || argumentErr.Reason != "expands to more than 65536 IPv4 addresses" {
			t.Errorf("iprange expansion error = %#v, want explicit 65536-address limit", argumentErr)
		}
	}
}

func TestAggressorGzipGunzipBinaryRoundTripAndFailures(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	input := BinaryString([]byte{0x00, 0x41, 0xc3, 0xa9, 0xff, 0x00})
	compressed := callAggressorPortableUtility(t, functions, "gzip", input)
	compressedBytes, ok := compressed.Bytes()
	if !ok || !compressed.IsBinaryString() || len(compressedBytes) < 2 || compressedBytes[0] != 0x1f || compressedBytes[1] != 0x8b {
		t.Fatalf("gzip result = %x/binary=%v, want GZIP binary string", compressedBytes, compressed.IsBinaryString())
	}
	compressedAgain := callAggressorPortableUtility(t, functions, "gzip", input)
	if again, _ := compressedAgain.Bytes(); !bytes.Equal(compressedBytes, again) {
		t.Fatal("gzip output is not deterministic for identical input")
	}

	decompressed := callAggressorPortableUtility(t, functions, "gunzip", compressed)
	decompressedBytes, ok := decompressed.Bytes()
	wantBytes, _ := input.Bytes()
	if !ok || !decompressed.IsBinaryString() || !bytes.Equal(decompressedBytes, wantBytes) {
		t.Fatalf("gunzip(gzip(input)) = %x/binary=%v, want %x/binary", decompressedBytes, decompressed.IsBinaryString(), wantBytes)
	}

	// Byte utilities consume the low eight bits of each UTF-16 unit. This is
	// OPFOR's portable byte boundary, not a licensed-runtime oracle claim.
	textRoundTrip := callAggressorPortableUtility(t, functions, "gunzip",
		callAggressorPortableUtility(t, functions, "gzip", String("é")))
	if got, _ := textRoundTrip.Bytes(); !bytes.Equal(got, []byte{0xe9}) || !textRoundTrip.IsBinaryString() {
		t.Fatalf("gzip text low-unit round trip = %x/binary=%v, want e9/binary", got, textRoundTrip.IsBinaryString())
	}

	for _, corrupt := range [][]byte{
		[]byte("not gzip"),
		compressedBytes[:len(compressedBytes)-2],
	} {
		if _, err := functions["gunzip"](context.Background(), aggressorPortableInvocation("gunzip", BinaryString(corrupt))); err == nil {
			t.Fatalf("gunzip unexpectedly accepted corrupt input %x", corrupt)
		}
	}
}

func TestAggressorStrChunkUsesUTF16UnitsAndPreservesProvenance(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	value := String("A😀B")
	chunked := callAggressorPortableUtility(t, functions, "str_chunk", value, Int(2))
	chunks := mustAggressorUtilityArray(t, chunked)
	if len(chunks) != 2 || !reflect.DeepEqual(sleepStringUnits(chunks[0]), []uint16{'A', 0xd83d}) ||
		!reflect.DeepEqual(sleepStringUnits(chunks[1]), []uint16{0xde00, 'B'}) {
		t.Fatalf("str_chunk(A😀B, 2) units = %04x/%04x", sleepStringUnits(chunks[0]), sleepStringUnits(chunks[1]))
	}
	if chunks[0].IsBinaryString() || chunks[1].IsBinaryString() {
		t.Fatal("text chunks unexpectedly gained binary provenance")
	}

	splitPair := mustAggressorUtilityArray(t, callAggressorPortableUtility(t, functions, "str_chunk", String("😀"), Int(1)))
	if len(splitPair) != 2 || !reflect.DeepEqual(sleepStringUnits(splitPair[0]), []uint16{0xd83d}) ||
		!reflect.DeepEqual(sleepStringUnits(splitPair[1]), []uint16{0xde00}) {
		t.Fatalf("str_chunk split surrogate = %v", splitPair)
	}

	binaryInput := BinaryString([]byte{0xc3, 0xa9, 0x00, 0xff, 'Z'})
	binaryChunks := mustAggressorUtilityArray(t, callAggressorPortableUtility(t, functions, "str_chunk", binaryInput, Int(2)))
	wantBinary := [][]byte{{0xc3, 0xa9}, {0x00, 0xff}, {'Z'}}
	if len(binaryChunks) != len(wantBinary) {
		t.Fatalf("binary chunk count = %d, want %d", len(binaryChunks), len(wantBinary))
	}
	for index, chunk := range binaryChunks {
		got, _ := chunk.Bytes()
		if !bytes.Equal(got, wantBinary[index]) || !chunk.IsBinaryString() {
			t.Errorf("binary chunk %d = %x/binary=%v, want %x/binary", index, got, chunk.IsBinaryString(), wantBinary[index])
		}
	}

	empty := mustAggressorUtilityArray(t, callAggressorPortableUtility(t, functions, "str_chunk", String(""), Int(3)))
	if len(empty) != 0 {
		t.Fatalf("str_chunk(empty) = %v, want empty array", empty)
	}
	for _, maximum := range []Value{Int(0), Int(-1)} {
		_, err := functions["str_chunk"](context.Background(), aggressorPortableInvocation("str_chunk", String("abc"), maximum))
		assertPortableUtilityArgumentError(t, err, "str_chunk", 2)
	}
}

func TestAggressorStrEncodeDecodeCharsetsReplacementAndBoundaries(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	tests := []struct {
		charset string
		want    []byte
	}{
		{charset: "UTF-8", want: []byte{0x41, 0xc3, 0xa9, 0xe2, 0x82, 0xac, 0xf0, 0x9f, 0x98, 0x80}},
		{charset: "uTf8", want: []byte{0x41, 0xc3, 0xa9, 0xe2, 0x82, 0xac, 0xf0, 0x9f, 0x98, 0x80}},
		{charset: "ASCII", want: []byte{'A', '?', '?', '?'}},
		{charset: "latin1", want: []byte{'A', 0xe9, '?', '?'}},
		{charset: "Cp1252", want: []byte{'A', 0xe9, 0x80, '?'}},
		{charset: "UTF-16BE", want: []byte{0x00, 0x41, 0x00, 0xe9, 0x20, 0xac, 0xd8, 0x3d, 0xde, 0x00}},
		{charset: "UTF16-LE", want: []byte{0x41, 0x00, 0xe9, 0x00, 0xac, 0x20, 0x3d, 0xd8, 0x00, 0xde}},
	}
	text := String("Aé€😀")
	for _, test := range tests {
		t.Run(test.charset, func(t *testing.T) {
			encoded := callAggressorPortableUtility(t, functions, "str_encode", text, String(test.charset))
			got, _ := encoded.Bytes()
			if !encoded.IsBinaryString() || !bytes.Equal(got, test.want) {
				t.Fatalf("str_encode bytes = %x/binary=%v, want %x/binary", got, encoded.IsBinaryString(), test.want)
			}
		})
	}

	for _, charset := range []string{"UTF-8", "UTF16-LE", "UnicodeBigUnmarked", "windows-1252"} {
		original := text
		if charset == "windows-1252" {
			original = String("Aé€")
		}
		encoded := callAggressorPortableUtility(t, functions, "str_encode", original, String(charset))
		decoded := callAggressorPortableUtility(t, functions, "str_decode", encoded, String(charset))
		if !sleepStringValuesEqual(decoded, original) || decoded.IsBinaryString() {
			t.Errorf("%s encode/decode = %s/binary=%v, want %s/text", charset, decoded.Describe(), decoded.IsBinaryString(), original.Describe())
		}
	}

	malformed := callAggressorPortableUtility(t, functions, "str_decode", BinaryString([]byte{0xe2, 0x82}), String("UTF-8"))
	if got := sleepStringUnits(malformed); !reflect.DeepEqual(got, []uint16{0xfffd}) {
		t.Fatalf("malformed UTF-8 decode units = %04x, want fffd", got)
	}
	unpaired := sleepStringValueFromUnits([]uint16{0xd800}, nil)
	if got, _ := callAggressorPortableUtility(t, functions, "str_encode", unpaired, String("UTF-8")).Bytes(); !bytes.Equal(got, []byte{'?'}) {
		t.Fatalf("unpaired UTF-8 replacement = %x, want 3f", got)
	}

	// Exercise the stateful encoder with a surrogate pair split exactly across
	// OPFOR's native processing boundary.
	boundaryUnits := make([]uint16, aggressorUtilityChunkSize+1)
	for index := 0; index < aggressorUtilityChunkSize-1; index++ {
		boundaryUnits[index] = 'A'
	}
	boundaryUnits[aggressorUtilityChunkSize-1] = 0xd83d
	boundaryUnits[aggressorUtilityChunkSize] = 0xde00
	boundaryText := sleepStringValueFromUnits(boundaryUnits, nil)
	boundaryEncoded := callAggressorPortableUtility(t, functions, "str_encode", boundaryText, String("UTF-8"))
	encodedBytes, _ := boundaryEncoded.Bytes()
	if !bytes.HasSuffix(encodedBytes, []byte{0xf0, 0x9f, 0x98, 0x80}) || len(encodedBytes) != aggressorUtilityChunkSize-1+4 {
		t.Fatalf("boundary UTF-8 encode len/suffix = %d/%x", len(encodedBytes), encodedBytes[len(encodedBytes)-4:])
	}

	// Exercise the stateful decoder with the first byte of a four-byte sequence
	// at the end of its input block.
	boundaryBytes := append(bytes.Repeat([]byte{'A'}, aggressorUtilityChunkSize-1), 0xf0, 0x9f, 0x98, 0x80)
	boundaryDecoded := callAggressorPortableUtility(t, functions, "str_decode", BinaryString(boundaryBytes), String("UTF8"))
	decodedUnits := sleepStringUnits(boundaryDecoded)
	if len(decodedUnits) != aggressorUtilityChunkSize+1 || decodedUnits[len(decodedUnits)-2] != 0xd83d || decodedUnits[len(decodedUnits)-1] != 0xde00 {
		t.Fatalf("boundary UTF-8 decode len/tail = %d/%04x", len(decodedUnits), decodedUnits[len(decodedUnits)-2:])
	}

	for _, name := range []string{"str_encode", "str_decode"} {
		_, err := functions[name](context.Background(), aggressorPortableInvocation(name, String("x"), String("not-a-charset")))
		assertPortableUtilityArgumentError(t, err, name, 2)
	}
}

func TestAggressorStrXORRepeatsKeyAndUsesLowUTF16Bytes(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	masked := callAggressorPortableUtility(t, functions, "str_xor", String("ABCDEF"), String("xy"))
	if got, _ := masked.Bytes(); !bytes.Equal(got, []byte("9;;==?")) || !masked.IsBinaryString() {
		t.Fatalf("str_xor repeated key = %x/binary=%v, want 393b3b3d3d3f/binary", got, masked.IsBinaryString())
	}
	plain := callAggressorPortableUtility(t, functions, "str_xor", masked, String("xy"))
	if got, _ := plain.Bytes(); !bytes.Equal(got, []byte("ABCDEF")) || !plain.IsBinaryString() {
		t.Fatalf("str_xor round trip = %x/binary=%v", got, plain.IsBinaryString())
	}

	// Each Java UTF-16 unit contributes its low byte. This behavior is OPFOR's
	// explicit portable boundary and is not asserted against a licensed oracle.
	unicodeInput := sleepStringValueFromUnits([]uint16{0x20ac, 0xd83d, 0xde00}, nil)
	unicodeMasked := callAggressorPortableUtility(t, functions, "str_xor", unicodeInput, String("Ā"))
	if got, _ := unicodeMasked.Bytes(); !bytes.Equal(got, []byte{0xac, 0x3d, 0x00}) || !unicodeMasked.IsBinaryString() {
		t.Fatalf("str_xor UTF-16 low bytes = %x/binary=%v, want ac3d00/binary", got, unicodeMasked.IsBinaryString())
	}

	_, err := functions["str_xor"](context.Background(), aggressorPortableInvocation("str_xor", String("data"), String("")))
	assertPortableUtilityArgumentError(t, err, "str_xor", 2)
}

func TestAggressorPortableUtilitiesHonorCanceledContexts(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		args []Value
	}{
		{name: "format_size", args: []Value{Int(1024)}},
		{name: "gzip", args: []Value{String("data")}},
		{name: "gunzip", args: []Value{BinaryString([]byte("data"))}},
		{name: "iprange", args: []Value{String("192.168.1.0/24")}},
		{name: "script_resource", args: []Value{String("asset.bin")}},
		{name: "str_chunk", args: []Value{String("data"), Int(1)}},
		{name: "str_encode", args: []Value{String("data"), String("UTF-8")}},
		{name: "str_decode", args: []Value{BinaryString([]byte("data")), String("UTF-8")}},
		{name: "str_xor", args: []Value{String("data"), String("key")}},
	}
	for _, test := range tests {
		if _, err := functions[test.name](ctx, aggressorPortableInvocation(test.name, test.args...)); !errors.Is(err, context.Canceled) {
			t.Errorf("%s canceled error = %v, want context.Canceled", test.name, err)
		}
	}
}

func TestAggressorPortableUtilitiesCancelDuringNativeLoops(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	large := BinaryString(bytes.Repeat([]byte{'A'}, aggressorUtilityChunkSize*3))
	compressed := callAggressorPortableUtility(t, functions, "gzip", large)
	tests := []struct {
		name   string
		args   []Value
		checks int32
	}{
		{name: "gzip", args: []Value{large}},
		{name: "gunzip", args: []Value{compressed}},
		{name: "iprange", args: []Value{String("10.20.0.0/16")}, checks: 4},
		{name: "str_chunk", args: []Value{large, Int(aggressorUtilityChunkSize)}},
		{name: "str_encode", args: []Value{large, String("UTF-8")}},
		{name: "str_decode", args: []Value{large, String("UTF-8")}},
		{name: "str_xor", args: []Value{large, String("key")}},
	}
	for _, test := range tests {
		checks := test.checks
		if checks == 0 {
			checks = 2
		}
		ctx := newCancelAfterChecksContext(checks)
		if _, err := functions[test.name](ctx, aggressorPortableInvocation(test.name, test.args...)); !errors.Is(err, context.Canceled) {
			t.Errorf("%s mid-loop cancellation error = %v, want context.Canceled", test.name, err)
		}
	}
}

func TestAggressorPortableUtilitiesExecuteFromScript(t *testing.T) {
	t.Parallel()

	program, err := CompileString("portable-aggressor-utilities.cna", `
$chunks = str_chunk("abcdef", 2);
$masked = str_xor("payload", "key");
return @(
    format_size(1024),
	join("|", iprange("192.168.1.18-21")),
    join("|", $chunks),
    str_decode(str_encode("Aé", "UTF16-LE"), "UTF16-LE"),
    str_xor($masked, "key"),
    gunzip(gzip("payload"))
);
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New(WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	values := mustAggressorUtilityArray(t, result)
	if len(values) != 6 || values[0].String() != "1kb" ||
		values[1].String() != "192.168.1.18|192.168.1.19|192.168.1.20" || values[2].String() != "ab|cd|ef" ||
		values[3].String() != "Aé" || values[4].String() != "payload" || values[5].String() != "payload" {
		t.Fatalf("script utility results = %s", result.Describe())
	}
	if values[3].IsBinaryString() || !values[4].IsBinaryString() || !values[5].IsBinaryString() {
		t.Fatalf("script utility provenance = text:%v xor:%v gunzip:%v", values[3].IsBinaryString(), values[4].IsBinaryString(), values[5].IsBinaryString())
	}
}

func aggressorPortableInvocation(name string, values ...Value) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{Name: name, Arguments: arguments}
}

func callAggressorPortableUtility(t *testing.T, functions map[string]NativeFunc, name string, values ...Value) Value {
	t.Helper()
	function := functions[name]
	if function == nil {
		t.Fatalf("portable Aggressor function %q is not registered", name)
	}
	value, err := function(context.Background(), aggressorPortableInvocation(name, values...))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return value
}

func mustAggressorUtilityArray(t *testing.T, value Value) []Value {
	t.Helper()
	array, ok := value.Array()
	if !ok {
		t.Fatalf("value = %s, want array", value.Describe())
	}
	return array.Values()
}

func aggressorUtilityArrayStrings(t *testing.T, value Value) []string {
	t.Helper()
	values := mustAggressorUtilityArray(t, value)
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func assertPortableUtilityArgumentError(t *testing.T, err error, function string, position int) {
	t.Helper()
	var argumentErr *PortableUtilityArgumentError
	if !errors.As(err, &argumentErr) {
		t.Fatalf("error = %T %v, want *PortableUtilityArgumentError", err, err)
	}
	if argumentErr.Function != function || argumentErr.Position != position || argumentErr.Reason == "" {
		t.Fatalf("argument error = %#v, want function %q position %d", argumentErr, function, position)
	}
}

type cancelAfterChecksContext struct {
	context.Context
	remaining atomic.Int32
	done      chan struct{}
	once      sync.Once
}

func newCancelAfterChecksContext(checks int32) *cancelAfterChecksContext {
	ctx := &cancelAfterChecksContext{Context: context.Background(), done: make(chan struct{})}
	ctx.remaining.Store(checks)
	return ctx
}

func (ctx *cancelAfterChecksContext) Done() <-chan struct{} { return ctx.done }

func (ctx *cancelAfterChecksContext) Err() error {
	if ctx.remaining.Add(-1) < 0 {
		ctx.once.Do(func() { close(ctx.done) })
	}
	select {
	case <-ctx.done:
		return context.Canceled
	default:
		return nil
	}
}
