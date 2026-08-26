package opfor

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestUtilityExtraFunctionSet(t *testing.T) {
	functions := (&Runtime{}).utilityExtraFunctions()
	got := make([]string, 0, len(functions))
	for name := range functions {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{
		"cast", "casti", "mid", "newInstance", "popl", "pushl", "putAll", "search", "setField", "systemProperties", "taint", "untaint",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("function names = %q, want %q", got, want)
	}
}

func TestSleepMidMatchesCanonicalPrograms(t *testing.T) {
	functions := (&Runtime{}).utilityExtraFunctions()
	tests := []struct {
		args []Value
		want string
	}{
		{args: []Value{String("this is a test"), Int(5), Int(2)}, want: "is"},
		{args: []Value{String("this is a test"), Int(5)}, want: "is a test"},
		{args: []Value{String("++this is a reversible string :)--"), Int(-11), Int(6)}, want: "string"},
		{args: []Value{String("abc"), Int(3)}, want: ""},
		{args: []Value{String("abc"), Int(3), Int(0)}, want: ""},
		{args: nil, want: ""},
	}
	for _, test := range tests {
		got := callUtilityExtra(t, functions, "mid", test.args...)
		if got.Kind() != KindString || got.String() != test.want {
			t.Fatalf("mid(%v) = (%s, %s), want string %q", test.args, got.Kind(), got.Describe(), test.want)
		}
	}

	_, err := functions["mid"](context.Background(), utilityInvocation("mid", String("abc"), Int(-9), Int(1)))
	if err == nil || !strings.Contains(err.Error(), "attempted an invalid index") {
		t.Fatalf("mid invalid index error = %v", err)
	}
}

func TestSearchMatchesSleepStartAndEmptyScalarSemantics(t *testing.T) {
	functions := (&Runtime{}).utilityExtraFunctions()
	array := ArrayValue(NewArray(String("a"), String("b"), String("c"), String("d"), String("e")))
	var calls [][]Value
	searcher := FunctionValue(utilityTestCallable(func(_ context.Context, values ...Value) (Value, error) {
		calls = append(calls, append([]Value(nil), values...))
		if values[0].String() == "c" {
			return String("found " + values[0].String() + " at " + values[1].String()), nil
		}
		return Null(), nil
	}))
	if got := callUtilityExtra(t, functions, "search", array, searcher); got.String() != "found c at 2" {
		t.Fatalf("search result = %s, want found c at 2", got.Describe())
	}
	if len(calls) != 3 || calls[2][0].String() != "c" || calls[2][1].Int32() != 2 {
		t.Fatalf("search calls = %#v", calls)
	}

	calls = nil
	if got := callUtilityExtra(t, functions, "search", array, searcher, Int(4)); !got.IsNull() {
		t.Fatalf("search from 4 = %s, want null", got.Describe())
	}
	if len(calls) != 1 || calls[0][0].String() != "e" || calls[0][1].Int32() != 4 {
		t.Fatalf("search from 4 calls = %#v", calls)
	}

	calls = nil
	if got := callUtilityExtra(t, functions, "search", array, searcher, Int(-99)); got.String() != "found c at 2" {
		t.Fatalf("search from far-negative index = %s, want found c at 2", got.Describe())
	}

	zero := FunctionValue(utilityTestCallable(func(context.Context, ...Value) (Value, error) { return Int(0), nil }))
	if got := callUtilityExtra(t, functions, "search", array, zero); got.Kind() != KindInt || got.Int32() != 0 {
		t.Fatalf("search returning integer zero = (%s, %s), want int zero", got.Kind(), got.Describe())
	}
}

func TestSearchRejectsInvalidInputsAndPropagatesCallbackErrors(t *testing.T) {
	functions := (&Runtime{}).utilityExtraFunctions()
	if _, err := functions["search"](context.Background(), utilityInvocation("search", String("not an array"), Null())); err == nil ||
		!strings.Contains(err.Error(), "expected array") {
		t.Fatalf("search invalid array error = %v", err)
	}
	if _, err := functions["search"](context.Background(), utilityInvocation("search", ArrayValue(NewArray()), String("not a function"))); err == nil ||
		!strings.Contains(err.Error(), "expected a function") {
		t.Fatalf("search invalid function error = %v", err)
	}

	wantErr := errors.New("search callback failed")
	callback := FunctionValue(utilityTestCallable(func(context.Context, ...Value) (Value, error) { return Null(), wantErr }))
	_, err := functions["search"](context.Background(), utilityInvocation("search", ArrayValue(NewArray(Int(1))), callback))
	if !errors.Is(err, wantErr) {
		t.Fatalf("search callback error = %v, want %v", err, wantErr)
	}
}

func TestPutAllPopulatesHashesFromPairedAndSeparateIterators(t *testing.T) {
	functions := (&Runtime{}).utilityExtraFunctions()
	hash := NewHash()
	target := HashValue(hash)
	result := callUtilityExtra(t, functions, "putAll", target,
		ArrayValue(NewArray(String("a"), String("apple"), String("b"), String("boy"), String("odd"))))
	if !result.IdentityEqual(target) {
		t.Fatal("putAll did not return its target hash")
	}
	assertUtilityHashValue(t, hash, "a", String("apple"))
	assertUtilityHashValue(t, hash, "b", String("boy"))
	if odd, exists := hash.Get("odd"); !exists || !odd.IsNull() {
		t.Fatalf("putAll odd trailing key = (%s, %v), want ($null, true)", odd.Describe(), exists)
	}

	separate := NewHash()
	callUtilityExtra(t, functions, "putAll", HashValue(separate),
		ArrayValue(NewArray(String("k1"), String("k2"), String("k3"))),
		ArrayValue(NewArray(String("v1"), String("v2"))))
	assertUtilityHashValue(t, separate, "k1", String("v1"))
	assertUtilityHashValue(t, separate, "k2", String("v2"))
	if missing, exists := separate.Get("k3"); !exists || !missing.IsNull() {
		t.Fatalf("putAll exhausted value iterator = (%s, %v), want ($null, true)", missing.Describe(), exists)
	}
}

func TestPutAllSupportsClosureIteratorsAndArrayValueCopies(t *testing.T) {
	functions := (&Runtime{}).utilityExtraFunctions()
	keys := FunctionValue(&utilityTestIterator{values: []Value{String("a"), String("b"), Null()}})
	values := FunctionValue(&utilityTestIterator{values: []Value{Int(1), Null()}})
	hash := NewHash()
	callUtilityExtra(t, functions, "putAll", HashValue(hash), keys, values)
	assertUtilityHashValue(t, hash, "a", Int(1))
	if value, exists := hash.Get("b"); !exists || !value.IsNull() {
		t.Fatalf("putAll exhausted closure = (%s, %v), want ($null, true)", value.Describe(), exists)
	}

	sourceArray := NewArray(String("one"), String("two"))
	targetArray := NewArray(String("zero"))
	target := ArrayValue(targetArray)
	if got := callUtilityExtra(t, functions, "putAll", target, ArrayValue(sourceArray)); !got.IdentityEqual(target) {
		t.Fatal("putAll did not return its target array")
	}
	if err := sourceArray.Set(0, String("changed")); err != nil {
		t.Fatalf("mutate source: %v", err)
	}
	if got := targetArray.Values(); len(got) != 3 || got[0].String() != "zero" || got[1].String() != "one" || got[2].String() != "two" {
		t.Fatalf("putAll array values = %#v", got)
	}
}

func TestPutAllIteratorValidationAndNonCollectionNoOp(t *testing.T) {
	functions := (&Runtime{}).utilityExtraFunctions()
	if _, err := functions["putAll"](context.Background(), utilityInvocation("putAll", HashValue(NewHash()), String("not an iterator"))); err == nil ||
		!strings.Contains(err.Error(), "expected iterator") {
		t.Fatalf("putAll invalid iterator error = %v", err)
	}
	target := String("unchanged")
	if got := callUtilityExtra(t, functions, "putAll", target, String("not an iterator")); got.Kind() != KindString || got.String() != "unchanged" {
		t.Fatalf("putAll scalar target = %s, want unchanged", got.Describe())
	}
	if got := callUtilityExtra(t, functions, "putAll"); !got.IsNull() {
		t.Fatalf("putAll() = %s, want null", got.Describe())
	}
}

type utilityTestCallable func(context.Context, ...Value) (Value, error)

func (function utilityTestCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return function(ctx, values...)
}

type utilityTestIterator struct {
	values []Value
	index  int
}

func (iterator *utilityTestIterator) Invoke(context.Context, ...Value) (Value, error) {
	if iterator.index >= len(iterator.values) {
		return Null(), nil
	}
	value := iterator.values[iterator.index]
	iterator.index++
	return value, nil
}

func utilityInvocation(name string, values ...Value) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{Name: name, Arguments: arguments}
}

func callUtilityExtra(t *testing.T, functions map[string]NativeFunc, name string, values ...Value) Value {
	t.Helper()
	value, err := functions[name](context.Background(), utilityInvocation(name, values...))
	if err != nil {
		t.Fatalf("%s(%v): %v", name, values, err)
	}
	return value
}

func assertUtilityHashValue(t *testing.T, hash *Hash, key string, want Value) {
	t.Helper()
	got, exists := hash.Get(key)
	if !exists || got.Kind() != want.Kind() || got.String() != want.String() {
		t.Fatalf("hash[%q] = (%s, %v), want %s", key, got.Describe(), exists, want.Describe())
	}
}
