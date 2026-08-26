package opfor

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestCollectionFunctionSet(t *testing.T) {
	functions := newCollectionFunctionSet()
	got := make([]string, 0, len(functions))
	for name := range functions {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{
		"%", "@", "add", "array", "copy", "double", "flatten", "hash", "int", "keys", "lc",
		"long", "ohash", "ohasha", "pop", "push", "remove", "scalar", "setMissPolicy",
		"setRemovalPolicy", "shift", "size", "strrep", "substr", "trim", "typeOf", "uc",
		"unshift", "values",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("function names = %q, want %q", got, want)
	}
}

func TestCollectionFunctionsRuntimeIntegration(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := runtime.RegisterFunction("unshift", builtinUnshift); err != nil {
		t.Fatalf("opt in unshift extension: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "collections.sl", `
@items = array("b", "c");
unshift(@items, "a");
push(@items, "d");
add(@items, "z", -1);
%mapping = hash(a => "apple", "b=bat");
return array(size(@items), shift(@items), pop(@items), %mapping["a"], size(%mapping));
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	// Sleep evaluates function arguments from last to first. The final size
	// argument therefore runs before pop and shift, while the first size runs
	// after both mutations.
	assertValueStrings(t, result, []string{"3", "a", "z", "apple", "2"})
}

func TestArrayAndHashConstructors(t *testing.T) {
	functions := newCollectionFunctionSet()

	array := callCollectionBuiltin(t, functions, "array", Int(1), String("two"))
	assertValueStrings(t, array, []string{"1", "two"})
	alias := callCollectionBuiltin(t, functions, "@", String("a"), String("b"))
	assertValueStrings(t, alias, []string{"a", "b"})

	hash := callCollectionInvocation(t, functions, Invocation{
		Name: "hash",
		Arguments: []Argument{
			{Name: "fruit", Value: String("apple")},
			{Value: String("answer=42")},
			{Name: "nested", Value: array},
		},
	})
	hashValue, ok := hash.Hash()
	if !ok {
		t.Fatalf("hash() returned %s", hash.Describe())
	}
	if got, _ := hashValue.Get("fruit"); got.String() != "apple" {
		t.Fatalf("fruit = %s, want apple", got.Describe())
	}
	if got, _ := hashValue.Get("answer"); got.String() != "42" {
		t.Fatalf("answer = %s, want 42", got.Describe())
	}
	if got, _ := hashValue.Get("nested"); !got.IdentityEqual(array) {
		t.Fatal("hash constructor did not preserve nested array identity")
	}

	_, err := functions["hash"](context.Background(), Invocation{
		Name:      "hash",
		Arguments: []Argument{{Value: String("malformed")}},
	})
	if err == nil || !strings.Contains(err.Error(), "malformed key value pair") {
		t.Fatalf("hash malformed pair error = %v", err)
	}
}

func TestArrayMutationFunctions(t *testing.T) {
	functions := newCollectionFunctionSet()
	value := ArrayValue(NewArray(String("a"), String("b")))

	if got := callCollectionBuiltin(t, functions, "push", value, String("c"), String("d")); got.String() != "d" {
		t.Fatalf("push return = %s, want d", got.Describe())
	}
	if got := callCollectionBuiltin(t, functions, "pop", value); got.String() != "d" {
		t.Fatalf("pop return = %s, want d", got.Describe())
	}
	if got := callCollectionBuiltin(t, functions, "shift", value); got.String() != "a" {
		t.Fatalf("shift return = %s, want a", got.Describe())
	}
	if got := callCollectionBuiltin(t, functions, "unshift", value, String("x"), String("y")); got.String() != "y" {
		t.Fatalf("unshift return = %s, want y", got.Describe())
	}
	assertValueStrings(t, value, []string{"x", "y", "b", "c"})

	if got := callCollectionBuiltin(t, functions, "add", value, String("tail"), Int(-1)); !got.IdentityEqual(value) {
		t.Fatal("add did not return its array")
	}
	callCollectionBuiltin(t, functions, "add", value, String("front"), Int(0))
	assertValueStrings(t, value, []string{"front", "x", "y", "b", "c", "tail"})

	if _, err := functions["push"](context.Background(), invocationOf("push", String("not an array"))); err == nil ||
		!strings.Contains(err.Error(), "expected array") {
		t.Fatalf("push wrong-container error = %v", err)
	}
	if _, err := functions["add"](context.Background(), invocationOf("add", value, String("bad"), Int(-99))); err == nil ||
		!strings.Contains(err.Error(), "out of range") {
		t.Fatalf("add bad-index error = %v", err)
	}
	if _, err := functions["pop"](context.Background(), invocationOf("pop", ArrayValue(NewArray()))); err == nil ||
		!strings.Contains(err.Error(), "array is empty") {
		t.Fatalf("empty pop error = %v", err)
	}
}

func TestHashSizeKeysAndValues(t *testing.T) {
	functions := newCollectionFunctionSet()
	hash := NewHash()
	hash.Set("a", String("apple"))
	hash.Set("dead", Null())
	hash.Set("b", String("bat"))
	value := HashValue(hash)

	if got := callCollectionBuiltin(t, functions, "size", value).Int32(); got != 2 {
		t.Fatalf("size(hash) = %d, want 2", got)
	}
	if _, exists := hash.Get("dead"); exists {
		t.Fatal("size did not purge Sleep-null hash entry")
	}
	// Ordinary hashes follow the Java 7 HashMap bucket order used by Sleep
	// 2.1. Use ohash when insertion order is required.
	assertValueStrings(t, callCollectionBuiltin(t, functions, "keys", value), []string{"b", "a"})
	assertValueStrings(t, callCollectionBuiltin(t, functions, "values", value), []string{"bat", "apple"})

	selected := callCollectionBuiltin(t, functions, "values", value,
		ArrayValue(NewArray(String("b"), String("missing"), String("a"))))
	selectedArray, _ := selected.Array()
	selectedValues := selectedArray.Values()
	if got := []string{selectedValues[0].String(), selectedValues[2].String()}; !reflect.DeepEqual(got, []string{"bat", "apple"}) {
		t.Fatalf("selected values = %q", got)
	}
	if !selectedValues[1].IsNull() {
		t.Fatalf("missing selected value = %s, want null", selectedValues[1].Describe())
	}
	if missing, exists := hash.Get("missing"); !exists || !missing.IsNull() {
		t.Fatal("values(hash, keys) did not autovivify a missing key")
	}
	if got := callCollectionBuiltin(t, functions, "keys", String("wrong")); !got.IsNull() {
		t.Fatalf("keys(non-hash) = %s, want null", got.Describe())
	}
}

func TestRemoveUsesSleepIdentity(t *testing.T) {
	functions := newCollectionFunctionSet()
	array := ArrayValue(NewArray(Int(1), Long(1), Double(1), String("1"), Int(2)))
	if got := callCollectionBuiltin(t, functions, "remove", array, Int(1)); !got.IdentityEqual(array) {
		t.Fatal("remove did not return its array")
	}
	remaining, _ := array.Array()
	values := remaining.Values()
	if len(values) != 2 || values[0].Kind() != KindDouble || values[1].Int32() != 2 {
		t.Fatalf("remaining array = %s, want @(1.0, 2)", array.Describe())
	}

	hash := NewHash()
	hash.Set("int", Int(1))
	hash.Set("long", Long(1))
	hash.Set("double", Double(1))
	hash.Set("string", String("1"))
	hashValue := HashValue(hash)
	callCollectionBuiltin(t, functions, "remove", hashValue, Int(1))
	assertValueStrings(t, callCollectionBuiltin(t, functions, "keys", hashValue), []string{"double"})

	if _, err := functions["remove"](context.Background(), invocationOf("remove")); err == nil ||
		!strings.Contains(err.Error(), "no active foreach loop") {
		t.Fatalf("remove() error = %v", err)
	}
}

func TestCopyIsShallowAndFlattenIsRecursive(t *testing.T) {
	functions := newCollectionFunctionSet()
	nested := ArrayValue(NewArray(String("nested")))
	original := ArrayValue(NewArray(Int(1), nested))
	copied := callCollectionBuiltin(t, functions, "copy", original)
	if copied.IdentityEqual(original) {
		t.Fatal("copy(array) preserved outer reference identity")
	}
	copiedArray, _ := copied.Array()
	if got, _ := copiedArray.Get(1); !got.IdentityEqual(nested) {
		t.Fatal("copy(array) did not preserve nested reference identity")
	}

	hash := NewHash()
	hash.Set("nested", nested)
	hashCopy := callCollectionBuiltin(t, functions, "copy", HashValue(hash))
	if hashCopy.IdentityEqual(HashValue(hash)) {
		t.Fatal("copy(hash) preserved outer reference identity")
	}
	hashCopyValue, _ := hashCopy.Hash()
	if got, _ := hashCopyValue.Get("nested"); !got.IdentityEqual(nested) {
		t.Fatal("copy(hash) did not preserve nested reference identity")
	}

	deep := ArrayValue(NewArray(Int(1), ArrayValue(NewArray(Int(2), ArrayValue(NewArray(Int(3))))), hashCopy))
	flat := callCollectionBuiltin(t, functions, "flatten", deep)
	flatArray, _ := flat.Array()
	flatValues := flatArray.Values()
	if len(flatValues) != 4 || flatValues[0].Int32() != 1 || flatValues[1].Int32() != 2 ||
		flatValues[2].Int32() != 3 || !flatValues[3].IdentityEqual(hashCopy) {
		t.Fatalf("flatten = %s", flat.Describe())
	}

	cycle := NewArray()
	cycle.Append(ArrayValue(cycle))
	if _, err := functions["flatten"](context.Background(), invocationOf("flatten", ArrayValue(cycle))); err == nil ||
		!strings.Contains(err.Error(), "cyclic array") {
		t.Fatalf("flatten cycle error = %v", err)
	}
}

func TestCopyAndFlattenFunctionIterators(t *testing.T) {
	functions := newCollectionFunctionSet()
	sequence := &builtinSequence{values: []Value{Int(1), Int(2), Null()}}
	copied := callCollectionBuiltin(t, functions, "copy", FunctionValue(sequence))
	assertValueStrings(t, copied, []string{"1", "2"})

	sequence = &builtinSequence{values: []Value{
		ArrayValue(NewArray(Int(1), ArrayValue(NewArray(Int(2))))),
		Int(3),
		Null(),
	}}
	flat := callCollectionBuiltin(t, functions, "flatten", FunctionValue(sequence))
	assertValueStrings(t, flat, []string{"1", "2", "3"})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := functions["copy"](cancelled, invocationOf("copy", FunctionValue(&builtinSequence{})))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copy cancelled iterator error = %v", err)
	}
}

func TestTypeOfUsesSleepClassNames(t *testing.T) {
	functions := newCollectionFunctionSet()
	tests := []struct {
		name  string
		value Value
		want  string
	}{
		{name: "null", value: Null(), want: "class sleep.engine.types.NullValue"},
		{name: "int", value: Int(1), want: "class sleep.engine.types.IntValue"},
		{name: "long", value: Long(1), want: "class sleep.engine.types.LongValue"},
		{name: "double", value: Double(1), want: "class sleep.engine.types.DoubleValue"},
		{name: "string", value: String("x"), want: "class sleep.engine.types.StringValue"},
		{name: "array", value: ArrayValue(NewArray()), want: "class sleep.engine.types.ListContainer"},
		{name: "hash", value: HashValue(NewHash()), want: "class sleep.engine.types.HashContainer"},
		{name: "function", value: FunctionValue(&builtinSequence{}), want: "class sleep.engine.types.ObjectValue"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := callCollectionBuiltin(t, functions, "typeOf", test.value)
			if got.Kind() != KindObject || got.String() != test.want {
				t.Fatalf("typeOf(%s) = (%s, %q), want object %q", test.value.Describe(), got.Kind(), got.String(), test.want)
			}
		})
	}

	ordered := callCollectionInvocation(t, functions, Invocation{Name: "ohash"})
	if got := callCollectionBuiltin(t, functions, "typeOf", ordered).String(); got != "class sleep.engine.types.OrderedHashContainer" {
		t.Fatalf("typeOf(ohash()) = %q", got)
	}
	orderedCopy := callCollectionBuiltin(t, functions, "copy", ordered)
	if got := callCollectionBuiltin(t, functions, "typeOf", orderedCopy).String(); got != "class sleep.engine.types.HashContainer" {
		t.Fatalf("typeOf(copy(ohash())) = %q", got)
	}
}

func TestNumericAndScalarConversions(t *testing.T) {
	functions := newCollectionFunctionSet()
	if got := callCollectionBuiltin(t, functions, "int", String("0x10")).Int32(); got != 0 {
		t.Fatalf("int(string hex) = %d, want 0", got)
	}
	if got := callCollectionBuiltin(t, functions, "int", ObjectValue("0x10")).Int32(); got != 16 {
		t.Fatalf("int(object hex) = %d, want 16", got)
	}
	if got := callCollectionBuiltin(t, functions, "long", String("12.9")).Int64(); got != 0 {
		t.Fatalf("long(decimal string) = %d, want 0", got)
	}
	if got := callCollectionBuiltin(t, functions, "double", String(" 12.5 ")).Float64(); got != 12.5 {
		t.Fatalf("double = %v, want 12.5", got)
	}
	if got := callCollectionBuiltin(t, functions, "double", String("Infinity")).Float64(); !math.IsInf(got, 1) {
		t.Fatalf("double(Infinity) = %v", got)
	}

	converted := callCollectionBuiltin(t, functions, "scalar", ObjectValue(false))
	if converted.Kind() != KindInt || converted.Int32() != 0 {
		t.Fatalf("scalar(false) = %s/%s, want int 0", converted.Kind(), converted.Describe())
	}
	binary := callCollectionBuiltin(t, functions, "scalar", ObjectValue([]byte{0, 0xff, 'A'}))
	if got, want := binary.String(), string([]byte{0, 0xff, 'A'}); got != want {
		t.Fatalf("scalar(byte array) = %q, want %q", got, want)
	}
	convertedArray := callCollectionBuiltin(t, functions, "scalar", ObjectValue([2]any{int32(7), "x"}))
	assertValueStrings(t, convertedArray, []string{"7", "x"})
}

func TestStringFunctions(t *testing.T) {
	functions := newCollectionFunctionSet()
	if got := callCollectionBuiltin(t, functions, "strrep", String("abcabc"), String("a"), String("x"), String("bc"), String("!")).String(); got != "x!x!" {
		t.Fatalf("strrep = %q, want x!x!", got)
	}
	if got := callCollectionBuiltin(t, functions, "strrep", String("banana"), String("a")).String(); got != "bnn" {
		t.Fatalf("strrep odd pair = %q, want bnn", got)
	}
	if got := callCollectionBuiltin(t, functions, "strrep", String("same"), String(""), String("x")).String(); got != "same" {
		t.Fatalf("strrep empty pattern = %q, want same", got)
	}
	if got := callCollectionBuiltin(t, functions, "substr", String("abcde"), Int(-3), Int(-1)).String(); got != "cd" {
		t.Fatalf("substr negative = %q, want cd", got)
	}
	if got := callCollectionBuiltin(t, functions, "substr", String("abcde"), Int(2), Int(99)).String(); got != "cde" {
		t.Fatalf("substr clamped = %q, want cde", got)
	}
	if _, err := functions["substr"](context.Background(), invocationOf("substr", String("abc"), Int(-9))); err == nil ||
		!strings.Contains(err.Error(), "illegal substring") {
		t.Fatalf("substr bad-index error = %v", err)
	}
	if got := callCollectionBuiltin(t, functions, "uc", String("Mixed 123")).String(); got != "MIXED 123" {
		t.Fatalf("uc = %q", got)
	}
	if got := callCollectionBuiltin(t, functions, "lc", String("Mixed 123")).String(); got != "mixed 123" {
		t.Fatalf("lc = %q", got)
	}
	if got := callCollectionBuiltin(t, functions, "trim", String("\x00\t value \r\n")).String(); got != "value" {
		t.Fatalf("trim = %q, want value", got)
	}
	if got := callCollectionBuiltin(t, functions, "trim", String("\u00a0value\u00a0")).String(); got != "\u00a0value\u00a0" {
		t.Fatalf("trim removed non-Java whitespace: %q", got)
	}
}

func TestSubstrScriptFailureWarnsAndAbortsOnlyActiveBlock(t *testing.T) {
	var output strings.Builder
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Eval(context.Background(), "substr-warning.sl", `
sub probe {
    substr("test", 8, 20);
    println("not reached");
}
probe();
println("caller continued");
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	wantError := "&substr: illegal substring('test', 8 -> 8, 20 -> 4) indices"
	wantOutput := "Warning: " + wantError + " at substr-warning.sl:3\ncaller continued\n"
	if got := output.String(); got != wantOutput {
		t.Fatalf("output = %q, want %q", got, wantOutput)
	}
}

func TestCollectionFunctionsConcurrentMutation(t *testing.T) {
	functions := newCollectionFunctionSet()
	array := ArrayValue(NewArray())
	const workers = 8
	const perWorker = 200

	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for index := 0; index < perWorker; index++ {
				_, err := functions["push"](context.Background(), invocationOf("push", array, Int(int32(worker*perWorker+index))))
				if err != nil {
					errors <- err
					return
				}
			}
		}(worker)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent push: %v", err)
	}
	if got := callCollectionBuiltin(t, functions, "size", array).Int32(); got != workers*perWorker {
		t.Fatalf("size after concurrent push = %d, want %d", got, workers*perWorker)
	}
}

type builtinSequence struct {
	mu     sync.Mutex
	values []Value
	index  int
}

func (sequence *builtinSequence) Invoke(_ context.Context, _ ...Value) (Value, error) {
	sequence.mu.Lock()
	defer sequence.mu.Unlock()
	if sequence.index >= len(sequence.values) {
		return Null(), nil
	}
	value := sequence.values[sequence.index]
	sequence.index++
	return value, nil
}

func newCollectionFunctionSet() map[string]NativeFunc {
	return (&Runtime{}).collectionFunctions()
}

func invocationOf(name string, values ...Value) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{Name: name, Arguments: arguments}
}

func callCollectionBuiltin(t *testing.T, functions map[string]NativeFunc, name string, values ...Value) Value {
	t.Helper()
	return callCollectionInvocation(t, functions, invocationOf(name, values...))
}

func callCollectionInvocation(t *testing.T, functions map[string]NativeFunc, invocation Invocation) Value {
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

func assertValueStrings(t *testing.T, value Value, want []string) {
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
