package opfor

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sort"
	"sync"
	"testing"
)

func TestStringNumberFunctionSet(t *testing.T) {
	functions := newStringNumberFunctionSet()
	got := make([]string, 0, len(functions))
	for name := range functions {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{
		"abs", "asc", "byteAt", "ceil", "charAt", "chr", "cos", "floor", "indexOf", "join",
		"lastIndexOf", "left", "lindexOf", "log", "rand", "replace", "replaceAt",
		"reverse", "right", "round", "sin", "split", "sqrt", "srand", "strlen", "tan", "tr",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("function names = %q, want %q", got, want)
	}
}

func TestStringFunctionsPreserveBinaryOctets(t *testing.T) {
	functions := newStringNumberFunctionSet()
	binary := BinaryString([]byte{'a', 0x00, 0xff, 'z'})

	if got := callStringNumberBuiltin(t, functions, "strlen", binary); got.Kind() != KindInt || got.Int32() != 4 {
		t.Fatalf("strlen(binary) = %s, want int 4", got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "charAt", binary, Int(2)); got.String() != string([]byte{0xff}) {
		t.Fatalf("charAt(binary, 2) bytes = %v, want [255]", []byte(got.String()))
	}
	if got := callStringNumberBuiltin(t, functions, "charAt", binary, Int(-1)); got.String() != "z" {
		t.Fatalf("charAt(binary, -1) = %s, want z", got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "byteAt", binary, Int(2)); got.Int32() != 255 {
		t.Fatalf("byteAt(binary, 2) = %s, want 255", got.Describe())
	}
	// Invalid UTF-8 passed through String retains the legacy byte fallback.
	if got := callStringNumberBuiltin(t, functions, "asc", String(string([]byte{0xff}))); got.Int32() != 255 {
		t.Fatalf("asc(0xff) = %s, want 255", got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "asc"); got.Int32() != 0 {
		t.Fatalf("asc() = %s, want 0", got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "chr", Int(255)); got.String() != "ÿ" {
		t.Fatalf("chr(255) = %q, want ÿ", got.String())
	}

	// The Java bridge casts to one UTF-16 code unit, including values above
	// Latin-1 rather than truncating them to an octet.
	if got := callStringNumberBuiltin(t, functions, "chr", Int(0x1234)); got.String() != "ሴ" {
		t.Fatalf("chr(0x1234) = %q, want ሴ", got.String())
	}

	if _, err := functions["asc"](context.Background(), stringNumberInvocation("asc", 0, String(""))); err == nil {
		t.Fatal("asc(empty) unexpectedly succeeded")
	}
	if _, err := functions["charAt"](context.Background(), stringNumberInvocation("charAt", 0, binary, Int(4))); err == nil {
		t.Fatal("charAt(binary, 4) unexpectedly succeeded")
	}
}

func TestLeftRightAndReplaceAtMatchSleepExamples(t *testing.T) {
	functions := newStringNumberFunctionSet()
	tests := []struct {
		name string
		args []Value
		want string
	}{
		{name: "right", args: []Value{String("this is a test"), Int(4)}, want: "test"},
		{name: "left", args: []Value{String("this is a test"), Int(4)}, want: "this"},
		{name: "right", args: []Value{String("this is a test"), Int(-5)}, want: "is a test"},
		{name: "left", args: []Value{String("this is a test"), Int(-5)}, want: "this is a"},
		{name: "left", args: []Value{String("abc"), Int(20)}, want: "abc"},
		{name: "replaceAt", args: []Value{String("this is a test"), String("uNF"), Int(-6), Int(1)}, want: "this is uNF test"},
		{name: "replaceAt", args: []Value{String("this is a test"), String(""), Int(4), Int(3)}, want: "this a test"},
		{name: "replaceAt", args: []Value{String("this is a test"), String("function "), Int(10), Int(0)}, want: "this is a function test"},
		{name: "replaceAt", args: []Value{String("abc"), String("x"), Int(2), Int(99)}, want: "abx"},
	}
	for _, test := range tests {
		t.Run(test.name+"/"+test.want, func(t *testing.T) {
			if got := callStringNumberBuiltin(t, functions, test.name, test.args...); got.String() != test.want {
				t.Fatalf("%s(%v) = %s, want %q", test.name, test.args, got.Describe(), test.want)
			}
		})
	}

	for _, invocation := range []Invocation{
		stringNumberInvocation("right", 0, String("abc"), Int(20)),
		stringNumberInvocation("replaceAt", 0, String("abc"), String("x"), Int(-20), Int(1)),
		stringNumberInvocation("replaceAt", 0, String("abc"), String("x"), Int(1), Int(-1)),
	} {
		if _, err := functions[invocation.Name](context.Background(), invocation); err == nil {
			t.Fatalf("%s unexpectedly accepted invalid indices", invocation.Name)
		}
	}
}

func TestIndexFunctionsMatchSleepBoundaries(t *testing.T) {
	functions := newStringNumberFunctionSet()
	value := String("this is a test")
	tests := []struct {
		args []Value
		want int32
	}{
		{args: []Value{value, String("is")}, want: 5},
		{args: []Value{String("this is a testz0r"), String("0")}, want: 15},
		{args: []Value{value, String("t")}, want: 13},
		{args: []Value{value, String("t"), Int(12)}, want: 10},
		{args: []Value{value, String("t"), Int(11)}, want: 10},
		{args: []Value{value, String("t"), Int(10)}, want: 10},
		{args: []Value{value, String("t"), Int(9)}, want: 0},
	}
	for index, test := range tests {
		got := callStringNumberBuiltin(t, functions, "lindexOf", test.args...)
		if got.Int32() != test.want {
			t.Fatalf("lindexOf case %d = %s, want %d", index, got.Describe(), test.want)
		}
	}
	if got := callStringNumberBuiltin(t, functions, "lastIndexOf", value, String("is")); got.Int32() != 5 {
		t.Fatalf("lastIndexOf alias = %s, want 5", got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "indexOf", String("abc"), String("a"), Int(-99)); got.Int32() != 0 {
		t.Fatalf("indexOf with far-negative start = %s, want 0", got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "indexOf", String("abc"), String(""), Int(99)); got.Int32() != 3 {
		t.Fatalf("indexOf empty string past end = %s, want 3", got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "indexOf", String("abc"), String("z")); !got.IsNull() {
		t.Fatalf("indexOf missing item = %s, want null", got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "lindexOf", String("abc"), String("a"), Int(-99)); !got.IsNull() {
		t.Fatalf("lindexOf with far-negative start = %s, want null", got.Describe())
	}
}

func TestSplitJoinReplaceAndRegexDeltas(t *testing.T) {
	functions := newStringNumberFunctionSet()
	split := callStringNumberBuiltin(t, functions, "split", String(" "),
		String("the rain in spain falls mainly on the plain"), Int(3))
	assertStringNumberArray(t, split, []string{"the", "rain", "in spain falls mainly on the plain"})
	assertStringNumberArray(t, callStringNumberBuiltin(t, functions, "split", String(","), String("a,b,,")),
		[]string{"a", "b"})
	assertStringNumberArray(t, callStringNumberBuiltin(t, functions, "split", String(","), String("a,b,,"), Int(-1)),
		[]string{"a", "b", "", ""})
	assertStringNumberArray(t, callStringNumberBuiltin(t, functions, "split", String(","), String("")), []string{""})

	joined := callStringNumberBuiltin(t, functions, "join", String("EQUALS"),
		callStringNumberBuiltin(t, functions, "split", String("="), String("key=value")))
	if joined.String() != "keyEQUALSvalue" {
		t.Fatalf("join(split()) = %s", joined.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "join", String(",")); got.String() != "" {
		t.Fatalf("join with omitted iterator = %s, want empty string", got.Describe())
	}

	replaceTests := []struct {
		args []Value
		want string
	}{
		{args: []Value{String("`butane is the man"), String(" is "), String(" was ")}, want: "`butane was the man"},
		{args: []Value{String("a12b"), String("([0-9]+)"), String("<$1>")}, want: "a<12>b"},
		// Java consumes only as many group-number digits as name an existing
		// group, so $1x is group 1 followed by a literal x.
		{args: []Value{String("a1b"), String("([0-9])"), String("$1x")}, want: "a1xb"},
		{args: []Value{String("aaa"), String("a"), String("x"), Int(2)}, want: "xxa"},
		{args: []Value{String("aaa"), String("a"), String("x"), Int(0)}, want: "aaa"},
	}
	for _, test := range replaceTests {
		if got := callStringNumberBuiltin(t, functions, "replace", test.args...); got.String() != test.want {
			t.Fatalf("replace(%v) = %s, want %q", test.args, got.Describe(), test.want)
		}
	}

	// RegexBridge uses java.util.regex. The portable engine retains Java-only
	// backtracking constructs, including zero-width lookbehind during split.
	lookbehind, err := functions["split"](context.Background(), stringNumberInvocation("split", 0,
		String("(?<=a)"), String("ab")))
	if err != nil {
		t.Fatalf("split lookbehind: %v", err)
	}
	assertStringNumberArray(t, lookbehind, []string{"a", "b"})
	_, err = functions["replace"](context.Background(), stringNumberInvocation("replace", 0,
		String("a"), String("a"), String("$9")))
	var warning *uncaughtScriptWarning
	if !errors.As(err, &warning) || err.Error() != "attempted an invalid index: No group 9" {
		t.Fatalf("replace bad group error = %v", err)
	}
	// Matcher.appendReplacement never parses the replacement when no match is
	// found, so an otherwise-invalid replacement remains harmless here.
	if got := callStringNumberBuiltin(t, functions, "replace", String("a"), String("z"), String("\\")); got.String() != "a" {
		t.Fatalf("replace no-match invalid replacement = %s", got.Describe())
	}
}

func TestRegexBridgeEmptyPatternUsesUTF16BoundariesAndProvenance(t *testing.T) {
	functions := newStringNumberFunctionSet()
	target := sleepStringValueFromUnits([]uint16{0xd83d, 0xde00}, []bool{true, false})

	partsValue := callStringNumberBuiltin(t, functions, "split", String(""), target, Int(-1))
	parts, ok := partsValue.Array()
	if !ok {
		t.Fatalf("split empty pattern = %s, want array", partsValue.Describe())
	}
	values := parts.Values()
	if len(values) != 3 {
		t.Fatalf("split empty pattern pieces = %d, want 3", len(values))
	}
	for index, want := range []struct {
		units []uint16
		raw   []bool
	}{
		{units: []uint16{0xd83d}, raw: []bool{true}},
		{units: []uint16{0xde00}, raw: []bool{false}},
		{units: []uint16{}, raw: nil},
	} {
		if got := sleepStringUnits(values[index]); !reflect.DeepEqual(got, want.units) {
			t.Errorf("split piece %d units = %x, want %x", index, got, want.units)
		}
		if got := sleepStringRawMask(values[index]); !reflect.DeepEqual(got, want.raw) {
			t.Errorf("split piece %d provenance = %v, want %v", index, got, want.raw)
		}
	}

	replacement := BinaryString([]byte{0xc3})
	for _, test := range []struct {
		name  string
		limit *int32
		units []uint16
		raw   []bool
	}{
		{name: "all", units: []uint16{0xc3, 0xd83d, 0xc3, 0xde00, 0xc3}, raw: []bool{true, true, true, false, true}},
		{name: "zero", limit: int32Pointer(0), units: []uint16{0xd83d, 0xde00}, raw: []bool{true, false}},
		{name: "one", limit: int32Pointer(1), units: []uint16{0xc3, 0xd83d, 0xde00}, raw: []bool{true, true, false}},
		{name: "two", limit: int32Pointer(2), units: []uint16{0xc3, 0xd83d, 0xc3, 0xde00}, raw: []bool{true, true, true, false}},
		{name: "three", limit: int32Pointer(3), units: []uint16{0xc3, 0xd83d, 0xc3, 0xde00, 0xc3}, raw: []bool{true, true, true, false, true}},
		{name: "large", limit: int32Pointer(99), units: []uint16{0xc3, 0xd83d, 0xc3, 0xde00, 0xc3}, raw: []bool{true, true, true, false, true}},
		{name: "other-negative", limit: int32Pointer(-2), units: []uint16{0xc3, 0xd83d, 0xc3, 0xde00, 0xc3}, raw: []bool{true, true, true, false, true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			arguments := []Value{target, String(""), replacement}
			if test.limit != nil {
				arguments = append(arguments, Int(*test.limit))
			}
			got := callStringNumberBuiltin(t, functions, "replace", arguments...)
			if units := sleepStringUnits(got); !reflect.DeepEqual(units, test.units) {
				t.Errorf("replace empty pattern units = %x, want %x", units, test.units)
			}
			if raw := sleepStringRawMask(got); !reflect.DeepEqual(raw, test.raw) {
				t.Errorf("replace empty pattern provenance = %v, want %v", raw, test.raw)
			}
		})
	}
}

func TestJoinAndReverseUseSleepIterators(t *testing.T) {
	functions := newStringNumberFunctionSet()
	original := ArrayValue(NewArray(String("a"), String("b"), String("c")))
	if got := callStringNumberBuiltin(t, functions, "join", String("-"), original); got.String() != "a-b-c" {
		t.Fatalf("join = %s, want a-b-c", got.Describe())
	}
	reversed := callStringNumberBuiltin(t, functions, "reverse", original)
	assertStringNumberArray(t, reversed, []string{"c", "b", "a"})
	assertStringNumberArray(t, original, []string{"a", "b", "c"})
	if reversed.IdentityEqual(original) {
		t.Fatal("reverse preserved outer array identity")
	}
	sequence := &stringNumberSequence{values: []Value{Int(1), Int(2), Int(3), Null()}}
	assertStringNumberArray(t, callStringNumberBuiltin(t, functions, "reverse", FunctionValue(sequence)),
		[]string{"3", "2", "1"})
	assertStringNumberArray(t, callStringNumberBuiltin(t, functions, "reverse"), []string{})

	// Sleep 2.1's reverse delegates to getIterator and therefore supports
	// arrays/closures, not scalar strings; it always returns a fresh array.
	if _, err := functions["reverse"](context.Background(), stringNumberInvocation("reverse", 0, String("abc"))); err == nil {
		t.Fatal("reverse(string) unexpectedly succeeded")
	}
}

func TestSleepMathFunctionsAndRoundKinds(t *testing.T) {
	functions := newStringNumberFunctionSet()
	if got := callStringNumberBuiltin(t, functions, "abs", Double(-3.25)); got.Kind() != KindDouble || got.Float64() != 3.25 {
		t.Fatalf("abs = (%s, %s), want double 3.25", got.Kind(), got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "round", Double(-1.5)); got.Kind() != KindLong || got.Int64() != -1 {
		t.Fatalf("round(-1.5) = (%s, %s), want long -1", got.Kind(), got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "round", Double(4.56734), Int(2)); got.Kind() != KindDouble || math.Abs(got.Float64()-4.57) > 1e-12 {
		t.Fatalf("round(4.56734, 2) = (%s, %s), want double 4.57", got.Kind(), got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "round", Double(math.NaN())); got.Int64() != 0 {
		t.Fatalf("round(NaN) = %s, want 0", got.Describe())
	}
	if got := callStringNumberBuiltin(t, functions, "round", Double(math.Inf(1))); got.Int64() != math.MaxInt64 {
		t.Fatalf("round(+Inf) = %s, want MaxInt64", got.Describe())
	}

	mathTests := []struct {
		name string
		args []Value
		want float64
	}{
		{name: "floor", args: []Value{Double(2.9)}, want: 2},
		{name: "ceil", args: []Value{Double(2.1)}, want: 3},
		{name: "sqrt", args: []Value{Double(81)}, want: 9},
		{name: "log", args: []Value{Double(math.E)}, want: 1},
		{name: "log", args: []Value{Double(8), Double(2)}, want: 3},
		{name: "sin", args: []Value{Double(math.Pi / 2)}, want: 1},
		{name: "cos", args: []Value{Double(0)}, want: 1},
		{name: "tan", args: []Value{Double(0)}, want: 0},
	}
	for _, test := range mathTests {
		got := callStringNumberBuiltin(t, functions, test.name, test.args...)
		if got.Kind() != KindDouble || math.Abs(got.Float64()-test.want) > 1e-12 {
			t.Fatalf("%s(%v) = (%s, %s), want double %g", test.name, test.args, got.Kind(), got.Describe(), test.want)
		}
	}
	if got := callStringNumberBuiltin(t, functions, "log"); !got.IsNull() {
		t.Fatalf("log() = %s, want null", got.Describe())
	}
}

func TestSrandReproducesJavaRandomSequencePerScript(t *testing.T) {
	functions := newStringNumberFunctionSet()
	script := ScriptID(42)
	callStringNumberInvocation(t, functions, stringNumberInvocation("srand", script, Long(12345)))

	items := make([]Value, 10)
	for index := range items {
		items[index] = Int(int32(index + 1))
	}
	array := ArrayValue(NewArray(items...))
	for index, want := range []int32{2, 1, 2, 9, 6, 5} {
		got := callStringNumberInvocation(t, functions, stringNumberInvocation("rand", script, array))
		if got.Int32() != want {
			t.Fatalf("rand(array) call %d = %s, want %d", index, got.Describe(), want)
		}
	}
	for index, want := range []float64{
		0.32647575623792624,
		0.2355237906476252,
		0.34911535662488336,
		0.4480776326931518,
	} {
		got := callStringNumberInvocation(t, functions, stringNumberInvocation("rand", script))
		if got.Kind() != KindDouble || got.Float64() != want {
			t.Fatalf("rand() call %d = %.17g, want %.17g", index, got.Float64(), want)
		}
	}

	// Random state belongs to the invoking script, matching ScriptInstance
	// metadata in BasicNumbers.java.
	other := ScriptID(99)
	callStringNumberInvocation(t, functions, stringNumberInvocation("srand", other, Long(12345)))
	if got := callStringNumberInvocation(t, functions, stringNumberInvocation("rand", other, array)); got.Int32() != 2 {
		t.Fatalf("first rand for independent script = %s, want 2", got.Describe())
	}

	callStringNumberInvocation(t, functions, stringNumberInvocation("srand", script, Long(0)))
	if got := callStringNumberInvocation(t, functions, stringNumberInvocation("rand", script, Int(100))); got.Int32() != 60 {
		t.Fatalf("new Random(0).nextInt(100) = %s, want 60", got.Describe())
	}
	if _, err := functions["rand"](context.Background(), stringNumberInvocation("rand", script, Int(0))); err == nil {
		t.Fatal("rand(0) unexpectedly succeeded")
	}
	if _, err := functions["rand"](context.Background(), stringNumberInvocation("rand", script, ArrayValue(NewArray()))); err == nil {
		t.Fatal("rand(empty array) unexpectedly succeeded")
	}
}

func TestRandConcurrentAccess(t *testing.T) {
	functions := newStringNumberFunctionSet()
	callStringNumberInvocation(t, functions, stringNumberInvocation("srand", 7, Long(1)))
	const workers = 8
	const calls = 100
	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for call := 0; call < calls; call++ {
				if _, err := functions["rand"](context.Background(), stringNumberInvocation("rand", 7, Int(1000))); err != nil {
					errors <- err
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent rand: %v", err)
	}
}

type stringNumberSequence struct {
	mu     sync.Mutex
	values []Value
	index  int
}

func (sequence *stringNumberSequence) Invoke(_ context.Context, _ ...Value) (Value, error) {
	sequence.mu.Lock()
	defer sequence.mu.Unlock()
	if sequence.index >= len(sequence.values) {
		return Null(), nil
	}
	value := sequence.values[sequence.index]
	sequence.index++
	return value, nil
}

func newStringNumberFunctionSet() map[string]NativeFunc {
	return (&Runtime{}).stringNumberFunctions()
}

func stringNumberInvocation(name string, script ScriptID, values ...Value) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{Name: name, Script: script, Arguments: arguments}
}

func callStringNumberBuiltin(t *testing.T, functions map[string]NativeFunc, name string, values ...Value) Value {
	t.Helper()
	return callStringNumberInvocation(t, functions, stringNumberInvocation(name, 0, values...))
}

func callStringNumberInvocation(t *testing.T, functions map[string]NativeFunc, invocation Invocation) Value {
	t.Helper()
	function := functions[invocation.Name]
	if function == nil {
		t.Fatalf("function %q is not registered", invocation.Name)
	}
	value, err := function(context.Background(), invocation)
	if err != nil {
		t.Fatalf("%s: %v", invocation.Name, err)
	}
	return value
}

func assertStringNumberArray(t *testing.T, value Value, want []string) {
	t.Helper()
	array, ok := value.Array()
	if !ok {
		t.Fatalf("value = %s, want array", value.Describe())
	}
	values := array.Values()
	got := make([]string, len(values))
	for index, item := range values {
		got[index] = item.String()
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("array values = %q, want %q", got, want)
	}
}
