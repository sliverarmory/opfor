package javaser

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialJavaValuesDecodeAndReencode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		className string
		field     Element
	}{
		{name: "raw-int.ser", className: "java.lang.Integer", field: Int(17)},
		{name: "raw-long.ser", className: "java.lang.Long", field: Long(1 << 32)},
		{name: "raw-double.ser", className: "java.lang.Double", field: Double(6.5)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := officialFixture(t, test.name)
			decoder := NewDecoder(bytes.NewReader(input))
			value, err := decoder.Decode()
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if decoder.BytesRead() != int64(len(input)) {
				t.Fatalf("BytesRead = %d, want %d", decoder.BytesRead(), len(input))
			}
			object, ok := value.(*Object)
			if !ok {
				t.Fatalf("root = %T, want *Object", value)
			}
			data, ok := object.DataFor(test.className)
			if !ok {
				t.Fatalf("missing class data for %s", test.className)
			}
			field, ok := data.Field("value")
			if !ok || field != test.field {
				t.Fatalf("value = %#v, %v, want %#v", field, ok, test.field)
			}
			assertReencodesExactly(t, input, value)
		})
	}

	t.Run("raw-string.ser", func(t *testing.T) {
		t.Parallel()
		input := officialFixture(t, "raw-string.ser")
		value, err := Decode(bytes.NewReader(input))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		stringValue, ok := value.(*String)
		if !ok || stringValue.Value != "raw string" {
			t.Fatalf("value = %#v, want raw string", value)
		}
		assertReencodesExactly(t, input, value)
	})

	t.Run("raw-boolean.ser", func(t *testing.T) {
		t.Parallel()
		input := officialFixture(t, "raw-boolean.ser")
		value, err := Decode(bytes.NewReader(input))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		object := value.(*Object)
		data, ok := object.DataFor("java.lang.Boolean")
		if !ok {
			t.Fatal("missing java.lang.Boolean class data")
		}
		field, ok := data.Field("value")
		if !ok || field != Boolean(true) {
			t.Fatalf("value = %#v, %v, want true", field, ok)
		}
		assertReencodesExactly(t, input, value)
	})

	t.Run("raw-class-string.ser", func(t *testing.T) {
		t.Parallel()
		input := officialFixture(t, "raw-class-string.ser")
		value, err := Decode(bytes.NewReader(input))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		class, ok := value.(*Class)
		if !ok || class.Descriptor.Name != "java.lang.String" {
			t.Fatalf("value = %#v, want java.lang.String class", value)
		}
		assertReencodesExactly(t, input, value)
	})
}

func TestOfficialSleepGraphsDecodeAndReencode(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"scalar-null.ser",
		"scalar-string.ser",
		"scalar-int.ser",
		"scalar-long.ser",
		"scalar-double.ser",
		"array-cycle.ser",
		"array-shared.ser",
		"hash.ser",
		"ordered-hash.ser",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := officialFixture(t, name)
			value, err := Decode(bytes.NewReader(input), WithClassDataResolver(sleepPhaseOneLayout))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			object, ok := value.(*Object)
			if !ok || object.Descriptor.Name != "sleep.runtime.Scalar" {
				t.Fatalf("root = %#v, want sleep.runtime.Scalar", value)
			}
			assertReencodesExactly(t, input, value)
		})
	}
}

func TestOfficialSleepClosureGraphsDecodeAndReencodeInertly(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"closure-unsuspended.ser",
		"closure-yielded.ser",
		"closure-local-stack.ser",
		"closure-foreach.ser",
		"closure-callcc.ser",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := officialFixture(t, name)
			value, err := Decode(bytes.NewReader(input), WithClassDataResolver(sleepGraphLayout))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			object, ok := value.(*Object)
			if !ok || object.Descriptor.Name != "sleep.runtime.Scalar" {
				t.Fatalf("root = %#v, want sleep.runtime.Scalar", value)
			}
			assertReencodesExactly(t, input, value)
		})
	}
}

func TestDecodeConsumesExactlyOneIndependentStream(t *testing.T) {
	t.Parallel()
	input := officialFixture(t, "concatenated-scalars.ser")
	reader := bytes.NewReader(input)
	decoder := NewDecoder(reader, WithClassDataResolver(sleepPhaseOneLayout))
	wantBytes := []int64{239, 211, 380}
	var total int64
	for index, want := range wantBytes {
		value, err := decoder.Decode()
		if err != nil {
			t.Fatalf("Decode root %d: %v", index, err)
		}
		if _, ok := value.(*Object); !ok {
			t.Fatalf("root %d = %T, want *Object", index, value)
		}
		if decoder.BytesRead() != want {
			t.Fatalf("root %d BytesRead = %d, want %d", index, decoder.BytesRead(), want)
		}
		total += decoder.BytesRead()
		if reader.Len() != len(input)-int(total) {
			t.Fatalf("root %d read ahead: %d bytes remain, want %d", index, reader.Len(), len(input)-int(total))
		}
	}
	if _, err := decoder.Decode(); !errors.Is(err, io.EOF) {
		t.Fatalf("Decode after final root error = %v, want io.EOF", err)
	}
	if decoder.BytesRead() != 0 {
		t.Fatalf("EOF BytesRead = %d, want 0", decoder.BytesRead())
	}
}

func TestDecodeFromOneByteReader(t *testing.T) {
	t.Parallel()
	input := officialFixture(t, "ordered-hash.ser")
	reader := &oneByteReader{reader: bytes.NewReader(input)}
	decoder := NewDecoder(reader, WithClassDataResolver(sleepPhaseOneLayout))
	value, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoder.BytesRead() != int64(len(input)) {
		t.Fatalf("BytesRead = %d, want %d", decoder.BytesRead(), len(input))
	}
	assertReencodesExactly(t, input, value)
}

func TestGraphIdentityCycleRoundTrip(t *testing.T) {
	t.Parallel()
	nextType := NewString("Lexample/Node;")
	desc := &ClassDesc{
		Name:             "example.Node",
		SerialVersionUID: 42,
		Flags:            SCSerializable,
		Fields: []FieldDesc{
			{TypeCode: TypeInt, Name: "number"},
			{TypeCode: TypeObject, Name: "next", ClassName: nextType},
		},
	}
	first := &Object{Descriptor: desc}
	second := &Object{Descriptor: desc}
	first.Data = []ClassData{{Descriptor: desc, Fields: []FieldValue{
		{Value: Int(1)},
		{Value: second},
	}}}
	second.Data = []ClassData{{Descriptor: desc, Fields: []FieldValue{
		{Value: Int(2)},
		{Value: first},
	}}}

	var encoded bytes.Buffer
	encoder := NewEncoder(&encoded)
	if err := encoder.Encode(first); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if encoder.BytesWritten() != int64(encoded.Len()) {
		t.Fatalf("BytesWritten = %d, want %d", encoder.BytesWritten(), encoded.Len())
	}
	decodedValue, err := Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decodedFirst := decodedValue.(*Object)
	firstData, _ := decodedFirst.DataFor("example.Node")
	decodedSecondValue, _ := firstData.Field("next")
	decodedSecond := decodedSecondValue.(*Object)
	secondData, _ := decodedSecond.DataFor("example.Node")
	backReference, _ := secondData.Field("next")
	if backReference != decodedFirst {
		t.Fatal("decoded back-reference did not preserve object identity")
	}
	assertReencodesExactly(t, encoded.Bytes(), decodedFirst)
}

func TestCustomAnnotationBlockDataAndSelfReference(t *testing.T) {
	t.Parallel()
	desc := &ClassDesc{
		Name:             "example.Custom",
		SerialVersionUID: 7,
		Flags:            SCSerializable | SCWriteMethod,
		Fields:           []FieldDesc{{TypeCode: TypeInt, Name: "omitted"}},
	}
	object := &Object{Descriptor: desc}
	block := bytes.Repeat([]byte{0xa5}, 300)
	object.Data = []ClassData{{
		Descriptor: desc,
		Annotation: []Content{&BlockData{Data: block}, object},
	}}
	var encoded bytes.Buffer
	if err := Encode(&encoded, object); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Contains(encoded.Bytes(), []byte{TCBlockDataLong, 0, 0, 1, 44}) {
		t.Fatal("300-byte annotation was not encoded as TC_BLOCKDATALONG")
	}
	decodedValue, err := Decode(bytes.NewReader(encoded.Bytes()), WithClassDataResolver(func(desc *ClassDesc) (ClassDataLayout, error) {
		if desc.Name == "example.Custom" {
			return ClassDataAnnotationOnly, nil
		}
		return ClassDataAuto, nil
	}))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decoded := decodedValue.(*Object)
	if len(decoded.Data) != 1 || len(decoded.Data[0].Fields) != 0 || len(decoded.Data[0].Annotation) != 2 {
		t.Fatalf("decoded class data = %#v", decoded.Data)
	}
	gotBlock := decoded.Data[0].Annotation[0].(*BlockData)
	if !bytes.Equal(gotBlock.Data, block) {
		t.Fatal("block data changed during round trip")
	}
	if decoded.Data[0].Annotation[1] != decoded {
		t.Fatal("custom-data self-reference did not preserve identity")
	}
}

func TestPrimitiveArrayAndClassToken(t *testing.T) {
	t.Parallel()
	desc := &ClassDesc{Name: "[I", SerialVersionUID: 5600894804908749477, Flags: SCSerializable}
	array := &Array{Descriptor: desc, Values: []Element{Int(-1), Int(0), Int(math.MaxInt32)}}
	var encoded bytes.Buffer
	if err := Encode(&encoded, array); err != nil {
		t.Fatalf("Encode array: %v", err)
	}
	decoded, err := Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("Decode array: %v", err)
	}
	got := decoded.(*Array)
	if len(got.Values) != 3 || got.Values[0] != Int(-1) || got.Values[2] != Int(math.MaxInt32) {
		t.Fatalf("decoded array = %#v", got.Values)
	}

	encoded.Reset()
	class := &Class{Descriptor: &ClassDesc{Name: "java.lang.String", SerialVersionUID: -6849794470754667710, Flags: SCSerializable}}
	if err := Encode(&encoded, class); err != nil {
		t.Fatalf("Encode class: %v", err)
	}
	decoded, err = Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("Decode class: %v", err)
	}
	if gotClass, ok := decoded.(*Class); !ok || gotClass.Descriptor.Name != "java.lang.String" {
		t.Fatalf("decoded class = %#v", decoded)
	}
}

func TestProxyClassDescriptorRoundTrip(t *testing.T) {
	t.Parallel()
	handlerType := NewString("Ljava/lang/reflect/InvocationHandler;")
	proxyBase := &ClassDesc{
		Name:             "java.lang.reflect.Proxy",
		SerialVersionUID: -2222568056686623797,
		Flags:            SCSerializable,
		Fields: []FieldDesc{{
			TypeCode:  TypeObject,
			Name:      "h",
			ClassName: handlerType,
		}},
	}
	proxy := &ClassDesc{
		IsProxy:         true,
		ProxyInterfaces: []string{"java.lang.Runnable", "java.io.Serializable"},
		Super:           proxyBase,
	}
	object := &Object{
		Descriptor: proxy,
		Data: []ClassData{{
			Descriptor: proxyBase,
			Fields:     []FieldValue{{Value: NullValue}},
		}},
	}
	var encoded bytes.Buffer
	if err := Encode(&encoded, object); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Contains(encoded.Bytes(), []byte{TCProxyClassDesc}) {
		t.Fatal("encoded stream does not contain TC_PROXYCLASSDESC")
	}
	decodedValue, err := Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decoded := decodedValue.(*Object)
	if !decoded.Descriptor.IsProxy || len(decoded.Descriptor.ProxyInterfaces) != 2 {
		t.Fatalf("decoded proxy descriptor = %#v", decoded.Descriptor)
	}
	if decoded.Descriptor.ProxyInterfaces[0] != "java.lang.Runnable" {
		t.Fatalf("first interface = %q", decoded.Descriptor.ProxyInterfaces[0])
	}
	assertReencodesExactly(t, encoded.Bytes(), decoded)
}

func TestModifiedUTF8AndLongString(t *testing.T) {
	t.Parallel()
	value := NewString("\x00A😀")
	var encoded bytes.Buffer
	if err := Encode(&encoded, value); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := []byte{0xac, 0xed, 0, 5, TCString, 0, 9, 0xc0, 0x80, 'A', 0xed, 0xa0, 0xbd, 0xed, 0xb8, 0x80}
	if !bytes.Equal(encoded.Bytes(), want) {
		t.Fatalf("encoded modified UTF-8 = %x, want %x", encoded.Bytes(), want)
	}
	decoded, err := Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.(*String).Value != value.Value {
		t.Fatalf("decoded = %q, want %q", decoded.(*String).Value, value.Value)
	}

	unpaired := &String{UTF16: []uint16{0xd800}}
	encoded.Reset()
	if err := Encode(&encoded, unpaired); err != nil {
		t.Fatalf("Encode unpaired surrogate: %v", err)
	}
	decoded, err = Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("Decode unpaired surrogate: %v", err)
	}
	gotUnits := decoded.(*String).UTF16
	if len(gotUnits) != 1 || gotUnits[0] != 0xd800 {
		t.Fatalf("decoded UTF-16 = %x, want d800", gotUnits)
	}
	assertReencodesExactly(t, encoded.Bytes(), decoded)

	long := NewString(strings.Repeat("x", math.MaxUint16+1))
	encoded.Reset()
	if err := Encode(&encoded, long); err != nil {
		t.Fatalf("Encode long string: %v", err)
	}
	if encoded.Bytes()[4] != TCLongString {
		t.Fatalf("tag = 0x%02x, want TC_LONGSTRING", encoded.Bytes()[4])
	}
	decoded, err = Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("Decode long string: %v", err)
	}
	if decoded.(*String).Value != long.Value {
		t.Fatal("long string changed during round trip")
	}
}

func TestResetAndInvalidReference(t *testing.T) {
	t.Parallel()
	stream := []byte{0xac, 0xed, 0, 5, TCReset, TCString, 0, 1, 'x'}
	value, err := Decode(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("Decode reset stream: %v", err)
	}
	if value.(*String).Value != "x" {
		t.Fatalf("decoded reset value = %#v", value)
	}

	invalid := []byte{0xac, 0xed, 0, 5, TCReference, 0, 0x7e, 0, 0}
	if _, err := Decode(bytes.NewReader(invalid)); err == nil || !strings.Contains(err.Error(), "invalid wire handle") {
		t.Fatalf("Decode invalid reference error = %v", err)
	}
}

func TestDecoderRejectsUnknownCustomLayoutAndLimits(t *testing.T) {
	t.Parallel()
	input := officialFixture(t, "scalar-string.ser")
	if _, err := Decode(bytes.NewReader(input)); err == nil || !strings.Contains(err.Error(), "no class-data layout") {
		t.Fatalf("Decode without custom layout error = %v", err)
	}

	limits := DefaultLimits()
	limits.MaxStringBytes = 4
	if _, err := Decode(bytes.NewReader(officialFixture(t, "raw-string.ser")), WithLimits(limits)); err == nil {
		t.Fatal("Decode unexpectedly accepted oversized string")
	} else {
		var limitError *LimitError
		if !errors.As(err, &limitError) || limitError.Resource != "string bytes" {
			t.Fatalf("error = %v, want string LimitError", err)
		}
	}

	limits = DefaultLimits()
	limits.MaxTotalBytes = 4
	if _, err := Decode(bytes.NewReader(officialFixture(t, "raw-string.ser")), WithLimits(limits)); err == nil {
		t.Fatal("Decode unexpectedly exceeded total-byte limit")
	}
}

func TestDecoderEOFAndMalformedInput(t *testing.T) {
	t.Parallel()
	if _, err := Decode(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Fatalf("empty Decode error = %v, want io.EOF", err)
	}
	if _, err := Decode(bytes.NewReader([]byte{0xac, 0xed})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("partial header error = %v, want io.ErrUnexpectedEOF", err)
	}
	badUTF := []byte{0xac, 0xed, 0, 5, TCString, 0, 1, 0}
	if _, err := Decode(bytes.NewReader(badUTF)); err == nil || !strings.Contains(err.Error(), "modified UTF-8") {
		t.Fatalf("bad modified UTF-8 error = %v", err)
	}
	unsupported := []byte{0xac, 0xed, 0, 5, TCException}
	if _, err := Decode(bytes.NewReader(unsupported)); err == nil || !strings.Contains(err.Error(), "TC_EXCEPTION") {
		t.Fatalf("TC_EXCEPTION error = %v", err)
	}
}

func sleepPhaseOneLayout(desc *ClassDesc) (ClassDataLayout, error) {
	if desc.Flags&SCWriteMethod == 0 {
		return ClassDataAuto, nil
	}
	switch desc.Name {
	case "sleep.runtime.Scalar", "sleep.engine.types.MyLinkedList":
		return ClassDataAnnotationOnly, nil
	default:
		return ClassDataDefaultFieldsAndAnnotation, nil
	}
}

func sleepGraphLayout(desc *ClassDesc) (ClassDataLayout, error) {
	if desc.Name == "sleep.bridges.SleepClosure" {
		return ClassDataAnnotationOnly, nil
	}
	return sleepPhaseOneLayout(desc)
}

func officialFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "serialization", "official-sleep-2.1", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return data
}

func assertReencodesExactly(t *testing.T, want []byte, value Value) {
	t.Helper()
	var encoded bytes.Buffer
	if err := Encode(&encoded, value); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(encoded.Bytes(), want) {
		first := firstDifference(encoded.Bytes(), want)
		t.Fatalf("re-encoded stream differs at byte %d: got %x, want %x", first, window(encoded.Bytes(), first), window(want, first))
	}
}

func firstDifference(left, right []byte) int {
	length := len(left)
	if len(right) < length {
		length = len(right)
	}
	for index := 0; index < length; index++ {
		if left[index] != right[index] {
			return index
		}
	}
	return length
}

func window(data []byte, offset int) []byte {
	end := offset + 16
	if end > len(data) {
		end = len(data)
	}
	if offset > len(data) {
		offset = len(data)
	}
	return data[offset:end]
}

type oneByteReader struct {
	reader *bytes.Reader
}

func (r *oneByteReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 1 {
		buffer = buffer[:1]
	}
	return r.reader.Read(buffer)
}
