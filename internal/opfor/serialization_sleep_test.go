package opfor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sliverarmory/opfor/internal/javaser"
)

func TestSerializationImporterErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		for _, test := range []struct {
			name      string
			function  string
			arguments []Value
			option    Option
		}{
			{name: "readObject", function: "readObject", option: WithStdin(rawImporterErrorReader{err: boundaryErr})},
			{name: "readAsObject", function: "readAsObject", option: WithStdin(rawImporterErrorReader{err: boundaryErr})},
			{name: "writeObject", function: "writeObject", arguments: []Value{String("value")}, option: WithStdout(rawImporterErrorWriter{err: boundaryErr})},
			{name: "writeAsObject", function: "writeAsObject", arguments: []Value{String("value")}, option: WithStdout(rawImporterErrorWriter{err: boundaryErr})},
		} {
			test := test
			t.Run(test.name+"/"+boundaryErr.Error(), func(t *testing.T) {
				runtimeInstance, err := New(test.option)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

				result, err := runtimeInstance.Invoke(context.Background(), test.function, test.arguments...)
				if !errors.Is(err, boundaryErr) || !result.IsNull() {
					t.Fatalf("Invoke = (%s, %v), want null/%v", result.Describe(), err, boundaryErr)
				}
			})
		}
	}
}

func TestOfficialSleepScalarSerializationVectors(t *testing.T) {
	tests := []struct {
		name string
		want Value
	}{
		{name: "scalar-null", want: Null()},
		{name: "scalar-string", want: String("hello, Sleep")},
		{name: "scalar-int", want: Int(42)},
		{name: "scalar-long", want: Long(4294967296)},
		{name: "scalar-double", want: Double(3.25)},
		{name: "scalar-binary-string", want: BinaryString([]byte{0, 127, 128, 255})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := readOfficialSleepSerializationVector(t, test.name)
			got, consumed, err := decodeSleepScalarStream(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("decode official vector: %v", err)
			}
			if consumed != int64(len(data)) {
				t.Fatalf("consumed = %d, want %d", consumed, len(data))
			}
			if !got.IdentityEqual(test.want) {
				t.Fatalf("decoded = %s, want %s", got.Describe(), test.want.Describe())
			}

			encoded, err := encodeSleepScalarStream(test.want)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if !bytes.Equal(encoded, data) {
				t.Fatalf("encoded official stream mismatch\nwant: %x\n got: %x", data, encoded)
			}
		})
	}
}

func TestOfficialSleepArraySerializationVectorsPreserveIdentity(t *testing.T) {
	sharedData := readOfficialSleepSerializationVector(t, "array-shared")
	shared, _, err := decodeSleepScalarStream(bytes.NewReader(sharedData))
	if err != nil {
		t.Fatalf("decode shared array: %v", err)
	}
	outer, ok := shared.Array()
	if !ok || outer.Len() != 3 {
		t.Fatalf("shared root = %s, want three-element array", shared.Describe())
	}
	first, _ := outer.Get(0)
	second, _ := outer.Get(1)
	firstArray, firstOK := first.Array()
	secondArray, secondOK := second.Array()
	if !firstOK || !secondOK || firstArray != secondArray {
		t.Fatalf("shared child identity was not preserved: %s", shared.Describe())
	}
	if got := firstArray.Values(); len(got) != 2 || got[0].String() != "x" || got[1].Int32() != 7 {
		t.Fatalf("shared child = %v", got)
	}
	encodedShared, err := encodeSleepScalarStream(shared)
	if err != nil {
		t.Fatalf("encode shared array: %v", err)
	}
	if !bytes.Equal(encodedShared, sharedData) {
		t.Fatalf("encoded shared array does not match official stream")
	}

	cycleData := readOfficialSleepSerializationVector(t, "array-cycle")
	cycle, _, err := decodeSleepScalarStream(bytes.NewReader(cycleData))
	if err != nil {
		t.Fatalf("decode cycle: %v", err)
	}
	cycleArray, ok := cycle.Array()
	if !ok || cycleArray.Len() != 2 {
		t.Fatalf("cycle root = %s, want two-element array", cycle.Describe())
	}
	backReference, _ := cycleArray.Get(1)
	backArray, ok := backReference.Array()
	if !ok || backArray != cycleArray {
		t.Fatalf("array cycle was not preserved: %s", cycle.Describe())
	}
	encodedCycle, err := encodeSleepScalarStream(cycle)
	if err != nil {
		t.Fatalf("encode array cycle: %v", err)
	}
	if !bytes.Equal(encodedCycle, cycleData) {
		t.Fatalf("encoded array cycle does not match official stream")
	}
}

func TestOfficialSleepHashSerializationVectors(t *testing.T) {
	ordinaryData := readOfficialSleepSerializationVector(t, "hash")
	ordinary, _, err := decodeSleepScalarStream(bytes.NewReader(ordinaryData))
	if err != nil {
		t.Fatalf("decode hash: %v", err)
	}
	hash, ok := ordinary.Hash()
	if !ok {
		t.Fatalf("ordinary root = %s, want hash", ordinary.Describe())
	}
	if got, want := hash.Keys(), []string{"b", "c", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ordinary keys = %q, want %q", got, want)
	}
	if value, _ := hash.Get("a"); value.String() != "apple" {
		t.Fatalf("hash[a] = %s", value.Describe())
	}
	encodedOrdinary, err := encodeSleepScalarStream(ordinary)
	if err != nil {
		t.Fatalf("encode hash: %v", err)
	}
	roundTripOrdinary, _, err := decodeSleepScalarStream(bytes.NewReader(encodedOrdinary))
	if err != nil || roundTripOrdinary.Describe() != ordinary.Describe() {
		t.Fatalf("hash round trip = %s, %v; want %s", roundTripOrdinary.Describe(), err, ordinary.Describe())
	}

	orderedData := readOfficialSleepSerializationVector(t, "ordered-hash")
	ordered, _, err := decodeSleepScalarStream(bytes.NewReader(orderedData))
	if err != nil {
		t.Fatalf("decode ordered hash: %v", err)
	}
	orderedHash, ok := ordered.Hash()
	if !ok || !orderedHash.ordered {
		t.Fatalf("ordered root = %s, want ordered hash", ordered.Describe())
	}
	if got, want := orderedHash.Keys(), []string{"third", "first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered keys = %q, want %q", got, want)
	}
	if orderedHash.missPolicy != nil || orderedHash.removalPolicy != nil {
		t.Fatal("transient ordered-hash policies were restored")
	}
	encodedOrdered, err := encodeSleepScalarStream(ordered)
	if err != nil {
		t.Fatalf("encode ordered hash: %v", err)
	}
	if !bytes.Equal(encodedOrdered, orderedData) {
		t.Fatalf("encoded ordered hash does not match official stream\nwant: %x\n got: %x", orderedData, encodedOrdered)
	}
}

func TestOfficialSleepRawSerializationVectors(t *testing.T) {
	tests := []struct {
		name  string
		class string
		want  Value
	}{
		{name: "raw-string", class: "java.lang.String", want: String("raw string")},
		{name: "raw-int", class: "java.lang.Integer", want: Int(17)},
		{name: "raw-long", class: "java.lang.Long", want: Long(4294967296)},
		{name: "raw-double", class: "java.lang.Double", want: Double(6.5)},
		{name: "raw-binary-string", class: "java.lang.String", want: BinaryString([]byte{0, 127, 128, 255})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := readOfficialSleepSerializationVector(t, test.name)
			got, consumed, err := decodeSleepRawStream(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("decode official raw vector: %v", err)
			}
			if consumed != int64(len(data)) {
				t.Fatalf("consumed = %d, want %d", consumed, len(data))
			}
			object, ok := got.Object()
			serialized, serializedOK := object.(*serializedJavaObject)
			if !ok || !serializedOK || serialized.class != test.class || !serialized.value.IdentityEqual(test.want) {
				t.Fatalf("decoded raw value = %#v, want %s %s", object, test.class, test.want.Describe())
			}
			encoded, err := encodeSleepRawStream(test.want)
			if err != nil {
				t.Fatalf("encode raw: %v", err)
			}
			if !bytes.Equal(encoded, data) {
				t.Fatalf("encoded official raw stream mismatch\nwant: %x\n got: %x", data, encoded)
			}
		})
	}
}

func TestSleepJavaStringDecodingPreservesHigherUnicode(t *testing.T) {
	encoded := &javaser.String{UTF16: []uint16{0x0100, 0xd83d, 0xde00}}
	if got, want := sleepStringFromJava(encoded), "Ā😀"; got != want {
		t.Fatalf("decoded = %q, want %q", got, want)
	}
}

func TestSleepJavaStringLatin1AmbiguityPrefersBinaryOctet(t *testing.T) {
	original := String("é")
	stream, err := encodeSleepScalarStream(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := decodeSleepScalarStream(bytes.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []byte(decoded.String()), []byte{0xe9}; !bytes.Equal(got, want) {
		t.Fatalf("decoded Latin-1 bytes = %x, want binary-carrier octet %x", got, want)
	}
	if !decoded.IdentityEqual(original) {
		t.Fatal("ambiguous Latin-1 code unit changed Java UTF-16 string identity")
	}
	if !decoded.IsBinaryString() {
		t.Fatal("ambiguous Latin-1 code unit did not retain reversible byte provenance")
	}
	reencoded, err := encodeSleepScalarStream(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, stream) {
		t.Fatal("binary-preferred Latin-1 value did not preserve the Java wire stream")
	}
}

func TestOfficialSleepRawBooleanAndClassVectors(t *testing.T) {
	tests := []struct {
		name  string
		class string
		text  string
	}{
		{name: "raw-boolean", class: "java.lang.Boolean", text: "true"},
		{name: "raw-class-string", class: "java.lang.Class", text: "class java.lang.String"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := readOfficialSleepSerializationVector(t, test.name)
			got, consumed, err := decodeSleepRawStream(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("decode official raw vector: %v", err)
			}
			if consumed != int64(len(data)) {
				t.Fatalf("consumed = %d, want %d", consumed, len(data))
			}
			object, ok := got.Object()
			serialized, serializedOK := object.(*serializedJavaObject)
			if !ok || !serializedOK || serialized.class != test.class || serialized.String() != test.text {
				t.Fatalf("decoded raw value = %#v, want %s %q", object, test.class, test.text)
			}
			encoded, err := encodeSleepRawStream(got)
			if err != nil {
				t.Fatalf("re-encode raw: %v", err)
			}
			if !bytes.Equal(encoded, data) {
				t.Fatalf("re-encoded official raw stream mismatch\nwant: %x\n got: %x", data, encoded)
			}
		})
	}
}

func TestSerializedJavaObjectsRetainPortableMethods(t *testing.T) {
	stringObject, _, err := decodeSleepRawStream(bytes.NewReader(readOfficialSleepSerializationVector(t, "raw-string")))
	if err != nil {
		t.Fatal(err)
	}
	substring, handled, err := portableObject(context.Background(), ObjectInvocation{
		Op:        ObjectInvoke,
		Target:    stringObject,
		Message:   "substring",
		Arguments: []Argument{{Value: Int(4)}},
	})
	if err != nil || !handled || substring.String() != "string" {
		t.Fatalf("substring = %s, handled=%v, err=%v", substring.Describe(), handled, err)
	}

	scalarObject, _, err := decodeSleepRawStream(bytes.NewReader(readOfficialSleepSerializationVector(t, "scalar-int")))
	if err != nil {
		t.Fatal(err)
	}
	integer, handled, err := portableObject(context.Background(), ObjectInvocation{
		Op:      ObjectInvoke,
		Target:  scalarObject,
		Message: "intValue",
	})
	if err != nil || !handled || integer.Int32() != 42 {
		t.Fatalf("Scalar.intValue = %s, handled=%v, err=%v", integer.Describe(), handled, err)
	}
}

func TestSleepSerializationConsumesOneIndependentStream(t *testing.T) {
	data := readOfficialSleepSerializationVector(t, "concatenated-scalars")
	reader := bytes.NewReader(data)
	first, consumed, err := decodeSleepScalarStream(reader)
	if err != nil {
		t.Fatal(err)
	}
	if first.String() != "first" || consumed <= 4 {
		t.Fatalf("first = %s, consumed = %d", first.Describe(), consumed)
	}
	remaining := make([]byte, 4)
	if _, err := io.ReadFull(reader, remaining); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(remaining, []byte{0xac, 0xed, 0x00, 0x05}) {
		t.Fatalf("next stream header = %x", remaining)
	}

	values := binaryArrayValues(t, callBinaryBuiltin(t, newBinaryFunctionSet(), "unpack", String("o*"), BinaryString(data)))
	if len(values) != 3 || values[0].String() != "first" || values[1].Int32() != 2 {
		t.Fatalf("unpack(o*) = %v", values)
	}
	array, ok := values[2].Array()
	if !ok || array.Len() != 1 || array.Values()[0].String() != "third" {
		t.Fatalf("unpack(o*) final value = %s", values[2].Describe())
	}
}

func TestSleepObjectIOReadsConcatenatedStreamsAndClosesAtEOF(t *testing.T) {
	data := readOfficialSleepSerializationVector(t, "concatenated-scalars")
	handle := newIOHandle("concatenated", bytes.NewReader(data), nil, false, false, false)
	state := &ioBuiltinState{}
	invocation := Invocation{
		Name:      "readObject",
		Arguments: []Argument{{Value: ObjectValue(handle)}},
	}
	first, err := state.readObject(context.Background(), invocation)
	if err != nil || first.String() != "first" {
		t.Fatalf("first read = %s, %v", first.Describe(), err)
	}
	second, err := state.readObject(context.Background(), invocation)
	if err != nil || second.Int32() != 2 {
		t.Fatalf("second read = %s, %v", second.Describe(), err)
	}
	third, err := state.readObject(context.Background(), invocation)
	if err != nil {
		t.Fatalf("third read: %v", err)
	}
	thirdArray, ok := third.Array()
	if !ok || thirdArray.Len() != 1 || thirdArray.Values()[0].String() != "third" {
		t.Fatalf("third read = %s", third.Describe())
	}
	eof, err := state.readObject(context.Background(), invocation)
	if err != nil || !eof.IsNull() {
		t.Fatalf("EOF read = %s, %v", eof.Describe(), err)
	}
	handle.mu.Lock()
	readerClosed := handle.reader == nil
	handle.mu.Unlock()
	if !readerClosed {
		t.Fatal("readObject did not close the handle at EOF")
	}
}

func TestSleepObjectIORejectsUnsupportedClosureAndClosesHandle(t *testing.T) {
	first := readOfficialSleepSerializationVector(t, "scalar-string")
	closure := readOfficialSleepSerializationVector(t, "closure-unsuspended")
	data := append(append([]byte(nil), first...), closure...)
	handle := newIOHandle("closure", bytes.NewReader(data), nil, false, false, false)
	state := &ioBuiltinState{}
	invocation := Invocation{
		Name:      "readObject",
		Arguments: []Argument{{Value: ObjectValue(handle)}},
	}
	value, err := state.readObject(context.Background(), invocation)
	if err != nil || value.String() != "hello, Sleep" {
		t.Fatalf("first read = %s, %v", value.Describe(), err)
	}
	if _, err := state.readObject(context.Background(), invocation); err == nil {
		t.Fatal("readObject accepted an unsupported SleepClosure graph")
	}
	handle.mu.Lock()
	readerClosed := handle.reader == nil
	handle.mu.Unlock()
	if !readerClosed {
		t.Fatal("readObject did not close the handle after the unsupported root")
	}
}

func TestSleepSerializationCanonicalPhaseOneFixtures(t *testing.T) {
	for _, name := range []string{"pureinterop", "sertest", "serohash", "taint11"} {
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
			if _, err := runtime.Execute(context.Background(), program); err != nil {
				t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
			}
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestOfficialSleepJavaConsumesOPFORPhaseOneStreams(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	ordinary := NewHash()
	ordinary.Set("a", String("apple"))
	ordinary.Set("b", Int(2))
	ordinary.Set("c", Double(3.5))
	ordered := NewOrderedHash()
	ordered.Set("third", Int(3))
	ordered.Set("first", Int(1))
	ordered.Set("second", Int(2))
	scalarRoots := []Value{
		String("hello"),
		Int(42),
		ArrayValue(NewArray(String("x"), Int(7))),
		HashValue(ordinary),
		HashValue(ordered),
	}
	rawRoots := []Value{
		String("raw"),
		Int(17),
		Long(4294967296),
		Double(6.5),
		ObjectValue(classReference("java.lang.String")),
	}

	temporary := t.TempDir()
	scalarPath := filepath.Join(temporary, "opfor-scalars.ser")
	rawPath := filepath.Join(temporary, "opfor-raw.ser")
	writeSerializedRoots := func(path string, roots []Value, raw bool) {
		t.Helper()
		var stream bytes.Buffer
		for _, root := range roots {
			var encoded []byte
			var encodeErr error
			if raw {
				encoded, encodeErr = encodeSleepRawStream(root)
			} else {
				encoded, encodeErr = encodeSleepScalarStream(root)
			}
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			stream.Write(encoded)
		}
		if err := os.WriteFile(path, stream.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSerializedRoots(scalarPath, scalarRoots, false)
	writeSerializedRoots(rawPath, rawRoots, true)

	consumer := filepath.Join("testdata", "serialization", "consume_phase1.sl")
	command := officialSleepJavaCommand(java, "-jar", jar, consumer, scalarPath, rawPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep consumer: %v\n%s", err, output)
	}
	const want = "hello\n42\n@('x', 7)\napple,2,3.5\n%(third => 3, first => 1, second => 2)\n" +
		"class java.lang.String:raw\nclass java.lang.Integer:17\nclass java.lang.Long:4294967296\n" +
		"class java.lang.Double:6.5\nclass java.lang.Class:class java.lang.String\n"
	if string(output) != want {
		t.Fatalf("official Sleep consumer output mismatch\nwant:\n%s\ngot:\n%s", want, output)
	}
}

func readOfficialSleepSerializationVector(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "serialization", "official-sleep-2.1", name+".ser"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
