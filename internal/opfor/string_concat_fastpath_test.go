package opfor

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSleepStringConcatPlainTextFastPathAndExplicitMetadataFallback(t *testing.T) {
	t.Parallel()

	plain := sleepStringConcat(String("alpha"), String("😀"), Int(42))
	if got, want := plain.String(), "alpha😀42"; got != want {
		t.Fatalf("plain concatenation = %q, want %q", got, want)
	}
	if plain.stringUnits != nil || plain.stringRaw != nil || plain.IsBinaryString() {
		t.Fatalf("plain concatenation retained explicit metadata: units=%x raw=%v", plain.stringUnits, plain.stringRaw)
	}
	if got, want := sleepStringUnits(plain), []uint16{'a', 'l', 'p', 'h', 'a', 0xd83d, 0xde00, '4', '2'}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plain concatenation units = %x, want %x", got, want)
	}

	binary := BinaryString([]byte{0xc3, 0xa9})
	unpaired := sleepStringValueFromUnits([]uint16{0xd800}, []bool{false})
	mixed := sleepStringConcat(String("x"), binary, unpaired, String("😀"))
	if got, want := sleepStringUnits(mixed), []uint16{'x', 0xc3, 0xa9, 0xd800, 0xd83d, 0xde00}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed concatenation units = %x, want %x", got, want)
	}
	if got, want := sleepStringRawMask(mixed), []bool{false, true, true, false, false, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed concatenation provenance = %v, want %v", got, want)
	}

	emptyBinary := sleepStringConcat(String("prefix"), BinaryString(nil))
	if emptyBinary.stringUnits == nil || emptyBinary.stringRaw == nil {
		t.Fatal("empty binary operand lost its explicit binary provenance representation")
	}
}

func TestSleepStringConcatFastPathPreservesEscapedDollarAndTaintTrace(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	taintedInput := String("left")
	taintedInput.tainted = true
	runtimeInstance, err := New(
		WithTaintMode(true),
		WithInitialGlobals(map[string]Value{"$input": taintedInput}),
		WithStdout(&output),
		WithStderr(&output),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	value, err := runtimeInstance.Eval(context.Background(), "concat-fastpath.sl", `
debug(debug() | 128);
$joined = $input . "-right";
println("cost: \$5");
return $joined;
`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), "left-right"; got != want || !value.IsTainted() {
		t.Fatalf("concatenated value = %q/tainted=%v, want %q/true", got, value.IsTainted(), want)
	}
	if got := output.String(); !strings.Contains(got, "Warning: tainted value: 'left-right' from: 'left' at concat-fastpath.sl:3\n") ||
		!strings.HasSuffix(got, "cost: $5\n") {
		t.Fatalf("taint trace/escaped dollar output mismatch:\n%s", got)
	}
}
