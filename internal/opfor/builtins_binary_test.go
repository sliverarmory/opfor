package opfor

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash/adler32"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestBinaryFunctionSet(t *testing.T) {
	functions := newBinaryFunctionSet()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{"base64_decode", "base64_encode", "checksum", "digest", "pack", "sizeof", "unpack"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("binary function names = %q, want %q", names, want)
	}
}

func TestAggressorBase64BinaryStrings(t *testing.T) {
	functions := newBinaryFunctionSet()
	raw := String("\x00binary\xff")
	encoded := callBinaryBuiltin(t, functions, "base64_encode", raw)
	if got, want := encoded.String(), "AGJpbmFyef8="; got != want {
		t.Fatalf("base64_encode = %q, want %q", got, want)
	}
	decoded := callBinaryBuiltin(t, functions, "base64_decode", encoded)
	if got, want := decoded.String(), raw.String(); got != want {
		t.Fatalf("base64_decode bytes = %v, want %v", []byte(got), []byte(want))
	}
	if _, err := functions["base64_decode"](
		context.Background(),
		binaryInvocation("base64_decode", String("not base64!")),
	); err == nil {
		t.Fatal("base64_decode invalid input unexpectedly succeeded")
	}
}

func TestSleepPackUnpackCanonicalNumericFormats(t *testing.T) {
	functions := newBinaryFunctionSet()
	packed := callBinaryBuiltin(t, functions, "pack",
		String("CidZl"), String("A"), Int(42), Double(3.5), String("hehe this is a string"), Long(1234567890))

	want := make([]byte, 0, 42)
	want = append(want, 'A')
	var scratch [8]byte
	binary.BigEndian.PutUint32(scratch[:4], 42)
	want = append(want, scratch[:4]...)
	binary.BigEndian.PutUint64(scratch[:], 0x400c000000000000)
	want = append(want, scratch[:]...)
	want = append(want, []byte("hehe this is a string")...)
	want = append(want, 0)
	binary.BigEndian.PutUint64(scratch[:], 1234567890)
	want = append(want, scratch[:]...)
	if !bytes.Equal([]byte(packed.String()), want) {
		t.Fatalf("pack(CidZl) bytes = %v, want %v", []byte(packed.String()), want)
	}

	values := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("CidZl"), packed))
	if len(values) != 5 || values[0].String() != "A" || values[1].Int32() != 42 ||
		values[2].Float64() != 3.5 || values[3].String() != "hehe this is a string" || values[4].Int64() != 1234567890 {
		t.Fatalf("unpack(CidZl) = %v", describeBinaryValues(values))
	}

	little := callBinaryBuiltin(t, functions, "pack", String("i- d- l-"), Int(42), Double(3.5), Long(1234567890))
	littleValues := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("i- d- l-"), little))
	if len(littleValues) != 3 || littleValues[0].Int32() != 42 || littleValues[1].Float64() != 3.5 || littleValues[2].Int64() != 1234567890 {
		t.Fatalf("little-endian round trip = %v", describeBinaryValues(littleValues))
	}

	bytesValue := callBinaryBuiltin(t, functions, "pack", String("b3"), Int(32), Int(-3), Int(4))
	unsigned := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("B3"), bytesValue))
	signed := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("b3"), bytesValue))
	assertBinaryIntegers(t, unsigned, []int64{32, 253, 4})
	assertBinaryIntegers(t, signed, []int64{32, -3, 4})

	shorts := callBinaryBuiltin(t, functions, "pack", String("s3"), Int(-25), Int(-35), Int(16000))
	assertBinaryIntegers(t,
		binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("S3"), shorts)),
		[]int64{65511, 65501, 16000})
	assertBinaryIntegers(t,
		binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("s3"), shorts)),
		[]int64{-25, -35, 16000})
}

func TestSleepPackUnpackRemainingPrimitiveFormats(t *testing.T) {
	functions := newBinaryFunctionSet()
	packed := callBinaryBuiltin(t, functions, "pack",
		String("c+ f- I+ x C"), String("Q"), Double(1.25), Long(4294967295), String("Z"))
	if got, want := len(packed.String()), 12; got != want {
		t.Fatalf("mixed primitive pack length = %d, want %d", got, want)
	}
	values := binaryArrayValues(t,
		callBinaryBuiltin(t, functions, "unpack", String("c+ f- I+ x C"), packed))
	if len(values) != 4 || values[0].String() != "Q" || values[1].Float64() != 1.25 ||
		values[2].Kind() != KindLong || values[2].Int64() != 4294967295 || values[3].String() != "Z" {
		t.Fatalf("mixed primitive round trip = %v", describeBinaryValues(values))
	}

	native := callBinaryBuiltin(t, functions, "pack", String("i!"), Int(0x01020304))
	var wantNative [4]byte
	binary.NativeEndian.PutUint32(wantNative[:], 0x01020304)
	if !bytes.Equal([]byte(native.String()), wantNative[:]) {
		t.Fatalf("native-endian pack = %v, want %v", []byte(native.String()), wantNative)
	}
}

func TestSleepPackUnpackHexAndBinaryOctets(t *testing.T) {
	functions := newBinaryFunctionSet()
	hexText := String("000123456789ABCDEF")

	high := callBinaryBuiltin(t, functions, "pack", String("H*"), hexText)
	if got, want := []byte(high.String()), []byte{0x00, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}; !bytes.Equal(got, want) {
		t.Fatalf("pack(H*) = %v, want %v", got, want)
	}
	if got := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("H*"), high))[0].String(); got != "000123456789abcdef" {
		t.Fatalf("unpack(H*) = %q", got)
	}
	if got := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("h*"), high))[0].String(); got != "001032547698badcfe" {
		t.Fatalf("unpack(h*) = %q", got)
	}

	low := callBinaryBuiltin(t, functions, "pack", String("h*"), hexText)
	if got, want := []byte(low.String()), []byte{0x00, 0x10, 0x32, 0x54, 0x76, 0x98, 0xba, 0xdc, 0xfe}; !bytes.Equal(got, want) {
		t.Fatalf("pack(h*) = %v, want %v", got, want)
	}

	allOctets := ArrayValue(NewArray(Int(0), Int(1), Int(127), Int(128), Int(254), Int(255)))
	packed := callBinaryBuiltin(t, functions, "pack", String("B*"), allOctets)
	if got, want := []byte(packed.String()), []byte{255, 254, 128, 127, 1, 0}; !bytes.Equal(got, want) {
		t.Fatalf("binary-string bytes = %v, want %v", got, want)
	}
	assertBinaryIntegers(t,
		binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("B*"), packed)),
		[]int64{255, 254, 128, 127, 1, 0})

	if _, err := functions["pack"](context.Background(), binaryInvocation("pack", String("H*"), String("abc"))); err == nil {
		t.Fatal("pack accepted an odd-length hex string")
	}
	if _, err := functions["pack"](context.Background(), binaryInvocation("pack", String("H*"), String("zz"))); err == nil {
		t.Fatal("pack accepted a non-hex string")
	}
}

func TestSleepPackFixedAndTerminatedStrings(t *testing.T) {
	functions := newBinaryFunctionSet()
	fixed := callBinaryBuiltin(t, functions, "pack", String("Z10 I"), String("abcde"), Int(45))
	if len(fixed.String()) != 14 {
		t.Fatalf("pack(Z10 I) length = %d, want 14", len(fixed.String()))
	}
	values := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("Z10 I"), fixed))
	if len(values) != 2 || values[0].String() != "abcde" || values[1].Int64() != 45 {
		t.Fatalf("fixed string round trip = %v", describeBinaryValues(values))
	}

	terminated := callBinaryBuiltin(t, functions, "pack", String("zB"), String(string([]byte{'a', 0xff, 'b'})), Int(7))
	if got, want := []byte(terminated.String()), []byte{'a', 0xff, 'b', 0, 7}; !bytes.Equal(got, want) {
		t.Fatalf("pack(zB) = %v, want %v", got, want)
	}
	values = binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("zB"), terminated))
	if len(values) != 2 || !bytes.Equal([]byte(values[0].String()), []byte{'a', 0xff, 'b'}) || values[1].Int32() != 7 {
		t.Fatalf("terminated binary round trip = %v", describeBinaryValues(values))
	}

	utfFields := callBinaryBuiltin(t, functions, "pack", String("UU"), String("first"), String("second"))
	values = binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("UU"), utfFields))
	if len(values) != 2 || values[0].String() != "first" || values[1].String() != "second" {
		t.Fatalf("U fields = %v", describeBinaryValues(values))
	}
}

func TestSleepPackUnpackWideStringsUseUTF16CodeUnits(t *testing.T) {
	t.Parallel()
	functions := newBinaryFunctionSet()
	text := String("é😀")

	big := callBinaryBuiltin(t, functions, "pack", String("U* U5"), text, text)
	if got, want := []byte(big.String()), []byte{
		0x00, 0xe9, 0xd8, 0x3d, 0xde, 0x00, 0x00, 0x00,
		0x00, 0xe9, 0xd8, 0x3d, 0xde, 0x00, 0x00, 0x00, 0x00, 0x00,
	}; !bytes.Equal(got, want) {
		t.Fatalf("big-endian wide fields = %x, want %x", got, want)
	}
	little := callBinaryBuiltin(t, functions, "pack", String("U-* U-5"), text, text)
	if got, want := []byte(little.String()), []byte{
		0xe9, 0x00, 0x3d, 0xd8, 0x00, 0xde, 0x00, 0x00,
		0xe9, 0x00, 0x3d, 0xd8, 0x00, 0xde, 0x00, 0x00, 0x00, 0x00,
	}; !bytes.Equal(got, want) {
		t.Fatalf("little-endian wide fields = %x, want %x", got, want)
	}

	values := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("U* U-*"),
		BinaryString(append(append([]byte(nil), big.String()[:8]...), little.String()[:8]...))))
	if len(values) != 2 || values[0].String() != text.String() || values[1].String() != text.String() {
		t.Fatalf("wide Unicode values = %v", describeBinaryValues(values))
	}

	// The Value retains the exact unpaired UTF-16 unit. String exposes its
	// reversible WTF-8 host spelling, and a later wide write reproduces the unit.
	for _, test := range []struct {
		format string
		data   []byte
	}{
		{format: "U*", data: []byte{0xd8, 0x00, 0x00, 0x00}},
		{format: "U-*", data: []byte{0x00, 0xd8, 0x00, 0x00}},
	} {
		decoded := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String(test.format), BinaryString(test.data)))
		if len(decoded) != 1 || !bytes.Equal([]byte(decoded[0].String()), []byte{0xed, 0xa0, 0x80}) {
			t.Fatalf("unpaired %s value = %x, want WTF-8 eda080", test.format, []byte(decoded[0].String()))
		}
		repacked := callBinaryBuiltin(t, functions, "pack", String(test.format), decoded[0])
		if !bytes.Equal([]byte(repacked.String()), test.data) {
			t.Fatalf("unpaired %s repack = %x, want %x", test.format, []byte(repacked.String()), test.data)
		}
	}
}

func TestSleepSizeofMatchesDataPatternEstimate(t *testing.T) {
	functions := newBinaryFunctionSet()
	tests := []struct {
		format string
		want   int32
	}{
		{format: "CidZl", want: 21},
		{format: "Z10 I", want: 14},
		{format: "B4", want: 4},
		{format: "H*", want: 0},
		{format: "U3", want: 6},
		{format: "M R", want: 0},
	}
	for _, test := range tests {
		if got := callBinaryBuiltin(t, functions, "sizeof", String(test.format)); got.Int32() != test.want {
			t.Fatalf("sizeof(%q) = %s, want %d", test.format, got.Describe(), test.want)
		}
	}
}

func TestSleepDigestAndChecksumPreserveBinaryInput(t *testing.T) {
	functions := newBinaryFunctionSet()
	data := string([]byte{'a', 0, 0xff, 'z'})

	gotDigest := callBinaryBuiltin(t, functions, "digest", String(data))
	wantDigest := md5.Sum([]byte(data))
	if !bytes.Equal([]byte(gotDigest.String()), wantDigest[:]) {
		t.Fatalf("digest raw bytes = %x, want %x", []byte(gotDigest.String()), wantDigest)
	}
	sha256Digest := callBinaryBuiltin(t, functions, "digest", String(data), String("SHA-256"))
	if len(sha256Digest.String()) != 32 {
		t.Fatalf("SHA-256 digest length = %d, want 32", len(sha256Digest.String()))
	}

	if got := callBinaryBuiltin(t, functions, "checksum", String(data)); got.Kind() != KindLong || got.Int64() != int64(crc32.ChecksumIEEE([]byte(data))) {
		t.Fatalf("CRC32 = %s, want %dL", got.Describe(), crc32.ChecksumIEEE([]byte(data)))
	}
	if got := callBinaryBuiltin(t, functions, "checksum", String(data), String("Adler32")); got.Int64() != int64(adler32.Checksum([]byte(data))) {
		t.Fatalf("Adler32 = %s, want %dL", got.Describe(), adler32.Checksum([]byte(data)))
	}

	if got := callBinaryBuiltin(t, functions, "digest", String("abc"), String("MD2")); hex.EncodeToString([]byte(got.String())) != "da853b0d3f88d99b30283a69e6ded6bb" {
		t.Fatalf("MD2(abc) = %x", []byte(got.String()))
	}
	if _, err := functions["checksum"](context.Background(), binaryInvocation("checksum", String(data), String("bogus"))); err == nil {
		t.Fatal("checksum accepted an unknown algorithm")
	}
}

func TestSleepBinaryObjectSerializationRoundTrip(t *testing.T) {
	functions := newBinaryFunctionSet()
	packed := callBinaryBuiltin(t, functions, "pack", String("o"), String("value"))
	if !bytes.HasPrefix([]byte(packed.String()), []byte{0xac, 0xed, 0x00, 0x05}) {
		t.Fatalf("pack(o) = %x, want Java serialization stream", []byte(packed.String()))
	}
	values := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("o"), packed))
	if len(values) != 1 || values[0].Kind() != KindString || values[0].String() != "value" {
		t.Fatalf("unpack(o) = %v, want ['value']", describeBinaryValues(values))
	}
	if got := callBinaryBuiltin(t, functions, "pack", String("o")); got.String() != "" {
		t.Fatalf("pack(o) without a value = %q, want empty output", got.String())
	}
	if got := binaryArrayValues(t, callBinaryBuiltin(t, functions, "unpack", String("o"), String(""))); len(got) != 0 {
		t.Fatalf("unpack(o) at EOF = %v, want no values", describeBinaryValues(got))
	}
	if _, err := functions["pack"](context.Background(), binaryInvocation("pack", String("x*"), Int(1))); err == nil {
		t.Fatal("pack(x*) unexpectedly entered Sleep's non-consuming loop")
	}
}

func TestSleepMD2RFC1319Vectors(t *testing.T) {
	functions := newBinaryFunctionSet()
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: "8350e5a3e24c153df2275c9f80692773"},
		{input: "a", want: "32ec01ec4a6dac72c0ab96fb34c0b5d1"},
		{input: "abc", want: "da853b0d3f88d99b30283a69e6ded6bb"},
		{input: "message digest", want: "ab4f496bfb2a530b219ff33031fe06b0"},
		{input: "abcdefghijklmnopqrstuvwxyz", want: "4e8ddff3650292ab5a4108c3aa47940b"},
		{input: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", want: "da33def2a42df13975352846c30338cd"},
		{input: "12345678901234567890123456789012345678901234567890123456789012345678901234567890", want: "d5976f79d83d3a0dc9806c3c66f3efd8"},
	}
	for _, test := range tests {
		got := callBinaryBuiltin(t, functions, "digest", String(test.input), String("MD2"))
		if encoded := hex.EncodeToString([]byte(got.String())); encoded != test.want {
			t.Fatalf("MD2(%q) = %s, want %s", test.input, encoded, test.want)
		}
	}
}

func TestSleepDigestAndChecksumWrapReadStreams(t *testing.T) {
	functions := newBinaryFunctionSet()
	data := []byte("read-through\x00binary\xffpayload")
	handle := newIOHandle("digest-reader", bytes.NewReader(data), nil, false, false, false)
	handleValue := ObjectValue(handle)

	digestState := callBinaryBuiltin(t, functions, "digest", handleValue, String("SHA-256"))
	checksumState := callBinaryBuiltin(t, functions, "checksum", handleValue, String("Adler32"))
	if available, open, err := handle.availableBytes(); err != nil || !open || available != int64(len(data)) {
		t.Fatalf("available after wrapping = (%d, %v, %v), want (%d, true, nil)", available, open, err, len(data))
	}
	read, err := handle.readBytes(-1)
	if err != nil || !bytes.Equal(read, data) {
		t.Fatalf("wrapped read = %x, %v", read, err)
	}

	wantDigest := sha256.Sum256(data)
	gotDigest := callBinaryBuiltin(t, functions, "digest", digestState)
	if !bytes.Equal([]byte(gotDigest.String()), wantDigest[:]) {
		t.Fatalf("wrapped SHA-256 = %x, want %x", []byte(gotDigest.String()), wantDigest)
	}
	if got := callBinaryBuiltin(t, functions, "checksum", checksumState).Int64(); got != int64(adler32.Checksum(data)) {
		t.Fatalf("wrapped Adler32 = %d, want %d", got, adler32.Checksum(data))
	}

	// MessageDigest.digest() resets its state; Checksum.getValue() does not.
	emptyDigest := sha256.Sum256(nil)
	if got := callBinaryBuiltin(t, functions, "digest", digestState); !bytes.Equal([]byte(got.String()), emptyDigest[:]) {
		t.Fatalf("reset SHA-256 = %x, want %x", []byte(got.String()), emptyDigest)
	}
	if got := callBinaryBuiltin(t, functions, "checksum", checksumState).Int64(); got != int64(adler32.Checksum(data)) {
		t.Fatalf("retained Adler32 = %d, want %d", got, adler32.Checksum(data))
	}
}

func TestSleepReadDigestObservesReferenceBufferFill(t *testing.T) {
	functions := newBinaryFunctionSet()
	data := bytes.Repeat([]byte("0123456789"), 900)
	handle := newIOHandle("buffered-digest-reader", bytes.NewReader(data), nil, false, false, false)
	digestState := callBinaryBuiltin(t, functions, "digest", ObjectValue(handle), String("MD5"))
	if read, err := handle.readBytes(1); err != nil || len(read) != 1 {
		t.Fatalf("single-byte read = %x, %v", read, err)
	}

	// Sleep places an 8192-byte BufferedInputStream above DigestInputStream.
	// The digest therefore observes the buffer fill, not only the one byte
	// returned to the script.
	want := md5.Sum(data[:sleepIOReadBufferSize])
	if got := callBinaryBuiltin(t, functions, "digest", digestState); !bytes.Equal([]byte(got.String()), want[:]) {
		t.Fatalf("digest after buffer fill = %x, want %x", []byte(got.String()), want)
	}
}

func TestSleepDigestAndChecksumWrapWriteStreams(t *testing.T) {
	functions := newBinaryFunctionSet()
	data := []byte("write-through payload")
	var destination bytes.Buffer
	handle := newIOHandle("digest-writer", nil, &destination, false, false, false)
	handleValue := ObjectValue(handle)

	digestState := callBinaryBuiltin(t, functions, "digest", handleValue, String(">MD5"))
	checksumState := callBinaryBuiltin(t, functions, "checksum", handleValue, String(">CRC32"))
	if written, err := handle.Write(data); err != nil || written != len(data) {
		t.Fatalf("wrapped write = %d, %v", written, err)
	}
	if !bytes.Equal(destination.Bytes(), data) {
		t.Fatalf("destination = %x, want %x", destination.Bytes(), data)
	}

	wantDigest := md5.Sum(data)
	if got := callBinaryBuiltin(t, functions, "digest", digestState); !bytes.Equal([]byte(got.String()), wantDigest[:]) {
		t.Fatalf("wrapped MD5 = %x, want %x", []byte(got.String()), wantDigest)
	}
	if got := callBinaryBuiltin(t, functions, "checksum", checksumState).Int64(); got != int64(crc32.ChecksumIEEE(data)) {
		t.Fatalf("wrapped CRC32 = %d, want %d", got, crc32.ChecksumIEEE(data))
	}
}

func TestSleepDigestAndChecksumRejectInvalidStreamSetup(t *testing.T) {
	functions := newBinaryFunctionSet()
	readOnly := ObjectValue(newIOHandle("read-only", bytes.NewReader(nil), nil, false, false, false))
	writeOnly := ObjectValue(newIOHandle("write-only", nil, &bytes.Buffer{}, false, false, false))

	tests := []struct {
		name      string
		handle    Value
		algorithm string
	}{
		{name: "digest", handle: readOnly, algorithm: ">MD5"},
		{name: "digest", handle: writeOnly, algorithm: "MD5"},
		{name: "checksum", handle: readOnly, algorithm: ">CRC32"},
		{name: "checksum", handle: writeOnly, algorithm: "CRC32"},
		{name: "digest", handle: readOnly, algorithm: ""},
		{name: "checksum", handle: readOnly, algorithm: ">"},
	}
	for _, test := range tests {
		if _, err := functions[test.name](context.Background(), binaryInvocation(test.name, test.handle, String(test.algorithm))); err == nil {
			t.Fatalf("%s(%s) unexpectedly succeeded", test.name, test.algorithm)
		}
	}
}

func TestSleepStreamDigestScriptIntegration(t *testing.T) {
	program, err := CompileString("stream-digest.sl", `
$handle = allocate();
$md2 = digest($handle, ">MD2");
$crc = checksum($handle, ">CRC32");
writeb($handle, "abc");
println(unpack("H*", digest($md2))[0]);
println(checksum($crc));
closef($handle);
$sha = digest($handle, "SHA-256");
readb($handle, -1);
println(unpack("H*", digest($sha))[0]);
`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}
	const want = "da853b0d3f88d99b30283a69e6ded6bb\n891568578\nba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestSleepBinaryCanonicalFixtureSubset(t *testing.T) {
	for _, name := range []string{"binary", "binarysz"} {
		t.Run(name, func(t *testing.T) {
			programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(name+".sl", programBytes))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			var output bytes.Buffer
			runtime, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatal(err)
			}
			for functionName, function := range runtime.binaryFunctions() {
				if err := runtime.RegisterFunction(functionName, function); err != nil {
					t.Fatalf("RegisterFunction(%s): %v", functionName, err)
				}
			}
			if _, err = runtime.Execute(context.Background(), program); err != nil {
				t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
			}
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestSleepPackInvalidHexEmitsCanonicalWarning(t *testing.T) {
	program, err := CompileString("invalid-hex.sl", `pack("H*", "abc");`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	for name, function := range runtime.binaryFunctions() {
		if err := runtime.RegisterFunction(name, function); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := output.String(), "Warning: can not pack 'abc' as hex string, number of characters must be even at invalid-hex.sl:1\n"; got != want {
		t.Fatalf("warning = %q, want %q", got, want)
	}
}

func newBinaryFunctionSet() map[string]NativeFunc {
	return (*Runtime)(nil).binaryFunctions()
}

func binaryInvocation(name string, values ...Value) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{Name: name, Arguments: arguments}
}

func callBinaryBuiltin(t *testing.T, functions map[string]NativeFunc, name string, values ...Value) Value {
	t.Helper()
	value, err := functions[name](context.Background(), binaryInvocation(name, values...))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return value
}

func binaryArrayValues(t *testing.T, value Value) []Value {
	t.Helper()
	array, ok := value.Array()
	if !ok {
		t.Fatalf("value = %s, want array", value.Describe())
	}
	return array.Values()
}

func assertBinaryIntegers(t *testing.T, values []Value, want []int64) {
	t.Helper()
	got := make([]int64, len(values))
	for index, value := range values {
		got[index] = value.Int64()
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("integers = %v, want %v", got, want)
	}
}

func describeBinaryValues(values []Value) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.Describe()
	}
	return result
}
