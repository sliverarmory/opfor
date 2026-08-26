package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSequenceFunctionSet(t *testing.T) {
	functions := newSequenceFunctionSet()
	got := make([]string, 0, len(functions))
	for name := range functions {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{
		"addAll", "clear", "concat", "contains", "containsAll", "filter", "grep", "isEmpty",
		"map", "mapValues", "range", "reduce", "removeAll", "retainAll", "reverse", "sort",
		"sorta", "sortd", "sortn", "subarray", "sublist", "sum", "zip",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("function names = %q, want %q", got, want)
	}
}

func TestSequenceImporterIteratorErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			calls := 0
			iterator := IteratorFunc(func(context.Context) (Value, bool, error) {
				calls++
				return String("discarded iterator partial result"), true, boundaryErr
			})
			runtimeInstance, err := New(WithInitialGlobals(map[string]Value{"boundary_iterator": ObjectValue(iterator)}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), "sum", ObjectValue(iterator))
			if !errors.Is(err, boundaryErr) || !result.IsNull() {
				t.Fatalf("Invoke = (%s, %v), want null/%v", result.Describe(), err, boundaryErr)
			}
			_, err = runtimeInstance.Eval(context.Background(), "iterator-boundary-error.sl", `sum($boundary_iterator);`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("script error = %v, want authoritative %v", err, boundaryErr)
			}
			if calls != 2 {
				t.Fatalf("iterator calls = %d, want two", calls)
			}
		})
	}
}

func TestSequenceConcatReturnsIndependentArray(t *testing.T) {
	functions := newSequenceFunctionSet()
	first := ArrayValue(NewArray(String("a"), String("b"), String("c")))
	second := ArrayValue(NewArray(Int(1), Int(2)))
	callable := FunctionValue(&stringNumberSequence{values: []Value{String("not invoked")}})
	combined := callSequenceBuiltin(t, functions, "concat", first, second, String("scalar"), callable, first)
	assertValueStrings(t, combined, []string{"a", "b", "c", "1", "2", "scalar", callable.String(), "a", "b", "c"})

	array, _ := combined.Array()
	if err := array.Set(2, String("changed")); err != nil {
		t.Fatal(err)
	}
	assertValueStrings(t, combined, []string{"a", "b", "changed", "1", "2", "scalar", callable.String(), "a", "b", "c"})
	assertValueStrings(t, first, []string{"a", "b", "c"})
	assertValueStrings(t, callSequenceBuiltin(t, functions, "concat"), []string{})
}

func TestSequenceSublistSharesElementCells(t *testing.T) {
	functions := newSequenceFunctionSet()
	source := ArrayValue(NewArray(String("a"), String("b"), String("c"), String("d")))
	view := callSequenceBuiltin(t, functions, "sublist", source, Int(-3), Int(99))
	assertValueStrings(t, view, []string{"b", "c", "d"})
	if view.IdentityEqual(source) {
		t.Fatal("sublist returned its source rather than a distinct view")
	}

	viewArray, _ := view.Array()
	if err := viewArray.Set(0, String("B")); err != nil {
		t.Fatalf("set through sublist: %v", err)
	}
	sourceArray, _ := source.Array()
	if got, _ := sourceArray.Get(1); got.String() != "B" {
		t.Fatalf("source[1] = %s, want B after sublist write", got.Describe())
	}
	if err := sourceArray.Set(2, String("C")); err != nil {
		t.Fatalf("set through source: %v", err)
	}
	if got, _ := viewArray.Get(1); got.String() != "C" {
		t.Fatalf("sublist[1] = %s, want C after source write", got.Describe())
	}

	alias := callSequenceBuiltin(t, functions, "subarray", source, Int(1), Int(-1))
	assertValueStrings(t, alias, []string{"B", "C"})
	if _, err := functions["sublist"](context.Background(), invocationOf("sublist", source, Int(3), Int(2))); err == nil ||
		!strings.Contains(err.Error(), "illegal subarray") {
		t.Fatalf("bad sublist error = %v", err)
	}
}

func TestSequenceIllegalSublistBecomesScriptWarning(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Eval(context.Background(), "bad_sublist.sl", `
@items = @("a", "b");
println("before");
sublist(@items, 1, 0);
println("after");
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	want := "before\nWarning: illegal subarray(@('a', 'b'), 1 -> 1, 0 -> 0) at bad_sublist.sl:4\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestSequenceMapConsumesPortableJavaIterator(t *testing.T) {
	collection := newPortableJavaCollection("LinkedList", []Value{Int(1), Int(3), Int(9), Int(12)})
	iterator, handled, err := collection.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "iterator"})
	if err != nil || !handled {
		t.Fatalf("iterator: handled=%v err=%v", handled, err)
	}
	mapped := callSequenceBuiltin(t, newSequenceFunctionSet(), "map",
		FunctionValue(testCallable(func(_ context.Context, values ...Value) (Value, error) {
			return Int(values[0].Int32() * 3), nil
		})), iterator)
	assertValueStrings(t, mapped, []string{"3", "9", "27", "36"})
}

func TestSequenceMapRejectsNativeBridgeFunctionReference(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "mapasc.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "mapasc.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("mapasc.sl", programBytes))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// java.lang.Object.toString appends a process-specific identity hash to
	// BasicStrings$func_asc. Compare the complete canonical warning after
	// removing only that JVM-only suffix from the pinned golden.
	javaIdentity := regexp.MustCompile(`@[[:xdigit:]]+`)
	want = javaIdentity.ReplaceAll(want, nil)
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch after JVM identity normalization\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func TestSequenceHigherOrderFunctionsRejectNativeCallables(t *testing.T) {
	functions := newSequenceFunctionSet()
	native := FunctionValue(nativeSequenceCallable(func(_ context.Context, _ ...Value) (Value, error) {
		t.Fatal("native callback was invoked")
		return Null(), nil
	}))
	array := ArrayValue(NewArray(Int(1), Int(2)))
	hash := HashValue(NewHash())
	tests := []struct {
		name      string
		arguments []Value
	}{
		{name: "map", arguments: []Value{native, array}},
		{name: "filter", arguments: []Value{native, array}},
		{name: "grep", arguments: []Value{native, array}},
		{name: "reduce", arguments: []Value{native, array}},
		{name: "sort", arguments: []Value{native, array}},
		{name: "mapValues", arguments: []Value{native, hash}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := functions[test.name](context.Background(), invocationOf(test.name, test.arguments...))
			if err == nil || !strings.Contains(err.Error(), "expected &closure--received:") {
				t.Fatalf("error = %v, want SleepClosure diagnostic", err)
			}
		})
	}

	if _, err := newSequenceCursor(native, "sum"); err == nil ||
		!strings.Contains(err.Error(), "expected iterator (@array or &closure)") {
		t.Fatalf("native iterator error = %v", err)
	}
}

func TestSequenceJavaIteratorPinnedGolden(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "jiter3.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "jiter3.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("jiter3.sl", programBytes))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func TestSequenceSublistStructuralMutationsReachRoot(t *testing.T) {
	root := NewArray(String("a"), String("b"), String("c"), String("d"), String("e"))
	view, err := root.sublist(1, 4)
	if err != nil {
		t.Fatalf("sublist: %v", err)
	}
	if err := view.appendValues(String("x")); err != nil {
		t.Fatalf("append through view: %v", err)
	}
	assertValueStrings(t, ArrayValue(view), []string{"b", "c", "d", "x"})
	assertValueStrings(t, ArrayValue(root), []string{"a", "b", "c", "d", "x", "e"})

	if err := removeArrayAt(view, 0); err != nil {
		t.Fatalf("remove through view: %v", err)
	}
	assertValueStrings(t, ArrayValue(view), []string{"c", "d", "x"})
	assertValueStrings(t, ArrayValue(root), []string{"a", "c", "d", "x", "e"})

	nested, err := view.sublist(1, 3)
	if err != nil {
		t.Fatalf("nested sublist: %v", err)
	}
	if err := removeArrayAt(nested, 0); err != nil {
		t.Fatalf("remove through nested view: %v", err)
	}
	assertValueStrings(t, ArrayValue(nested), []string{"x"})
	assertValueStrings(t, ArrayValue(root), []string{"a", "c", "x", "e"})
	if !errors.Is(view.viewError(), ErrUnsafeArrayView) {
		t.Fatalf("parent view error = %v, want ErrUnsafeArrayView", view.viewError())
	}
	if _, err := view.snapshotCells(); !errors.Is(err, ErrUnsafeArrayView) {
		t.Fatalf("invalid parent view snapshot error = %v", err)
	}
}

func TestSequenceSublistPinnedGoldens(t *testing.T) {
	for _, name := range []string{"listops_simple", "listops2", "splicetest", "splsublist", "arrmods", "mlistfun"} {
		name := name
		t.Run(name, func(t *testing.T) {
			programPath := filepath.Join("testdata", "upstream", "sleep-2.1", "programs", name+".sl")
			programBytes, err := os.ReadFile(programPath)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(name+".sl", programBytes))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			var output bytes.Buffer
			runtime, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := runtime.Execute(ctx, program); err != nil {
				t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
			}
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestSequenceUnsafeViewNativeErrorBecomesWarning(t *testing.T) {
	root := NewArray(String("a"), String("b"), String("c"), String("d"))
	view, err := root.sublist(1, 3)
	if err != nil {
		t.Fatalf("sublist: %v", err)
	}
	if err := root.appendValues(String("e")); err != nil {
		t.Fatalf("mutate root: %v", err)
	}

	var warnings bytes.Buffer
	runtime, err := New(WithStderr(&warnings))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.invoke(context.Background(), Invocation{
		Name: "size",
		Span: Span{
			Source: "unsafe.sl",
			Start:  Position{Line: 12, Column: 1},
			End:    Position{Line: 12, Column: 12},
		},
		Arguments: []Argument{{Value: ArrayValue(view)}},
	})
	if err != nil {
		t.Fatalf("size(invalid view): %v", err)
	}
	if !result.IsNull() {
		t.Fatalf("size(invalid view) = %s, want null", result.Describe())
	}
	want := "Warning: unsafe data modification: parent @array changed after &sublist creation at unsafe.sl:12\n"
	if warnings.String() != want {
		t.Fatalf("warning = %q, want %q", warnings.String(), want)
	}
}

func TestSequenceUnsafeViewIteratorBecomesWarning(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Eval(context.Background(), "unsafe_foreach.sl", `@root = @("a", "b", "c");
@view = sublist(@root, 0, 2);
push(@root, "d");
foreach $item (@view) { println($item); }
println("done");
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	want := "Warning: unsafe data modification: parent @array changed after &sublist creation at unsafe_foreach.sl:4\ndone\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestSequenceSetOperationsUseSleepIdentity(t *testing.T) {
	functions := newSequenceFunctionSet()
	target := ArrayValue(NewArray(Int(1), Int(2)))
	additions := ArrayValue(NewArray(Long(1), Int(3), Long(3)))
	if got := callSequenceBuiltin(t, functions, "addAll", target, additions); !got.IdentityEqual(target) {
		t.Fatal("addAll did not return its target array")
	}
	// addAll's identity set is built once from the original target. Both 3s
	// therefore survive, matching Sleep 2.1's implementation.
	assertValueStrings(t, target, []string{"1", "2", "3", "3"})

	if got := callSequenceBuiltin(t, functions, "removeAll", target,
		ArrayValue(NewArray(String("1"), Long(3)))); !got.IdentityEqual(target) {
		t.Fatal("removeAll did not return its target array")
	}
	assertValueStrings(t, target, []string{"2"})

	keep := ArrayValue(NewArray(Int(1), Int(2), Int(3)))
	keepArray, _ := keep.Array()
	originalCell, _ := keepArray.Cell(1)
	if got := callSequenceBuiltin(t, functions, "retainAll", keep,
		ArrayValue(NewArray(Long(2)))); !got.IdentityEqual(keep) {
		t.Fatal("retainAll did not return its target array")
	}
	assertValueStrings(t, keep, []string{"2"})
	retainedCell, _ := keepArray.Cell(0)
	if retainedCell != originalCell {
		t.Fatal("retainAll replaced rather than retained the matching scalar cell")
	}

	unchanged := ArrayValue(NewArray(String("x")))
	callSequenceBuiltin(t, functions, "addAll", unchanged, String("not an array"))
	assertValueStrings(t, unchanged, []string{"x"})
	callSequenceBuiltin(t, functions, "retainAll", unchanged, String("not an array"))
	assertValueStrings(t, unchanged, []string{})

	if _, err := functions["addAll"](context.Background(), invocationOf("addAll", String("wrong"))); err == nil ||
		!strings.Contains(err.Error(), "expected array") {
		t.Fatalf("addAll wrong-target error = %v", err)
	}
}

func TestSequenceMembershipFunctions(t *testing.T) {
	functions := newSequenceFunctionSet()
	array := ArrayValue(NewArray(Int(1), String("two"), Long(1), String("two")))
	if got := callSequenceBuiltin(t, functions, "contains", array, String("1")); !got.Truth() {
		t.Fatalf("contains numeric identity = %s, want true", got.Describe())
	}
	if got := callSequenceBuiltin(t, functions, "contains", array, String("missing")); got.Truth() {
		t.Fatalf("contains missing = %s, want false", got.Describe())
	}
	if got := callSequenceBuiltin(t, functions, "containsAll", array,
		ArrayValue(NewArray(String("1"), String("two")))); !got.Truth() {
		t.Fatalf("containsAll = %s, want true", got.Describe())
	}
	if got := callSequenceBuiltin(t, functions, "containsAll", array,
		FunctionValue(&testSequenceIterator{values: []Value{Int(1), String("missing"), Null()}})); got.Truth() {
		t.Fatalf("containsAll iterator = %s, want false", got.Describe())
	}
}

func TestSequenceClearAndIsEmpty(t *testing.T) {
	functions := newSequenceFunctionSet()
	array := ArrayValue(NewArray(Int(1)))
	callSequenceBuiltin(t, functions, "clear", array)
	if arrayValue, _ := array.Array(); arrayValue.Len() != 0 {
		t.Fatalf("cleared array = %s", array.Describe())
	}
	if !callSequenceBuiltin(t, functions, "isEmpty", array).Truth() {
		t.Fatal("isEmpty(empty array) = false")
	}

	hash := NewHash()
	hash.Set("active", String("value"))
	hashValue := HashValue(hash)
	hashReference := NewCell(hashValue)
	callSequenceInvocation(t, functions, Invocation{
		Name:      "clear",
		Arguments: []Argument{{Reference: hashReference}},
	})
	if hash.Len() != 1 || !callSequenceBuiltin(t, functions, "isEmpty", hashReference.Get()).Truth() {
		t.Fatalf("clear(hash reference) = old %s, replacement %s", hashValue.Describe(), hashReference.Get().Describe())
	}
	replacementHash, _ := hashReference.Get().Hash()
	replacementHash.Set("null", Null())
	if !callSequenceBuiltin(t, functions, "isEmpty", hashReference.Get()).Truth() {
		t.Fatal("isEmpty(null-only hash) = false")
	}

	reference := NewCell(String("value"))
	callSequenceInvocation(t, functions, Invocation{
		Name:      "clear",
		Arguments: []Argument{{Reference: reference}},
	})
	if !reference.Get().IsNull() {
		t.Fatalf("clear(pass-by-name scalar) = %s", reference.Get().Describe())
	}
	if !callSequenceBuiltin(t, functions, "isEmpty", Null()).Truth() ||
		!callSequenceBuiltin(t, functions, "isEmpty", String("")).Truth() ||
		callSequenceBuiltin(t, functions, "isEmpty", Int(0)).Truth() {
		t.Fatal("isEmpty scalar semantics do not distinguish empty storage from numeric zero")
	}
}

func TestSequenceMapFilterAndGrep(t *testing.T) {
	functions := newSequenceFunctionSet()
	input := ArrayValue(NewArray(Int(1), Int(2), Int(3)))

	mapper := FunctionValue(testCallable(func(_ context.Context, values ...Value) (Value, error) {
		if values[0].Int32() == 2 {
			return Null(), nil
		}
		return Int(values[0].Int32() * 10), nil
	}))
	mapped := callSequenceBuiltin(t, functions, "map", mapper, input)
	mappedArray, _ := mapped.Array()
	mappedValues := mappedArray.Values()
	if len(mappedValues) != 3 || mappedValues[0].Int32() != 10 || !mappedValues[1].IsNull() || mappedValues[2].Int32() != 30 {
		t.Fatalf("map = %s, want @(10, null, 30)", mapped.Describe())
	}

	filter := FunctionValue(testCallable(func(_ context.Context, values ...Value) (Value, error) {
		if values[0].Int32()%2 == 0 {
			return String(fmt.Sprintf("mapped-%d", values[0].Int32())), nil
		}
		return Null(), nil
	}))
	assertValueStrings(t, callSequenceBuiltin(t, functions, "filter", filter, input), []string{"mapped-2"})

	predicate := FunctionValue(testCallable(func(_ context.Context, values ...Value) (Value, error) {
		return Bool(values[0].Int32()%2 != 0), nil
	}))
	assertValueStrings(t, callSequenceBuiltin(t, functions, "grep", predicate, input), []string{"1", "3"})

	iterator := FunctionValue(&testSequenceIterator{values: []Value{String("a"), String("b"), Null()}})
	identity := FunctionValue(testCallable(func(_ context.Context, values ...Value) (Value, error) { return values[0], nil }))
	assertValueStrings(t, callSequenceBuiltin(t, functions, "map", identity, iterator), []string{"a", "b"})

	if _, err := functions["map"](context.Background(), invocationOf("map", String("not callable"), input)); err == nil ||
		!strings.Contains(err.Error(), "expected &closure--received:") {
		t.Fatalf("map callback error = %v", err)
	}
}

func TestSequenceReduceMatchesSleepArgumentOrder(t *testing.T) {
	functions := newSequenceFunctionSet()
	add := FunctionValue(testCallable(func(_ context.Context, values ...Value) (Value, error) {
		return Int(values[0].Int32() + values[1].Int32()), nil
	}))
	if got := callSequenceBuiltin(t, functions, "reduce", add,
		ArrayValue(NewArray(Int(1), Int(2), Int(3), Int(4), Int(5)))); got.Int32() != 15 {
		t.Fatalf("reduce sum = %s, want 15", got.Describe())
	}

	concatenate := FunctionValue(testCallable(func(_ context.Context, values ...Value) (Value, error) {
		return String(values[0].String() + values[1].String()), nil
	}))
	if got := callSequenceBuiltin(t, functions, "reduce", concatenate,
		ArrayValue(NewArray(String("a"), String("b"), String("c")))); got.String() != "bac" {
		t.Fatalf("reduce argument order = %q, want bac", got.String())
	}
	if got := callSequenceBuiltin(t, functions, "reduce", add, ArrayValue(NewArray())); got.Int32() != 0 {
		t.Fatalf("reduce empty = %s, want numeric callback result 0", got.Describe())
	}
}

func TestSequenceSumMatchesPinnedGoldens(t *testing.T) {
	functions := newSequenceFunctionSet()
	powers := ArrayValue(NewArray(Int(1), Int(2), Int(4), Int(8), Int(16), Int(32), Int(64), Int(128)))
	if got := callSequenceBuiltin(t, functions, "sum", powers); got.Kind() != KindDouble || got.Float64() != 255 {
		t.Fatalf("sum powers = %s/%s, want double 255", got.Kind(), got.Describe())
	}

	constant := &constantSequenceCallable{value: Int(2)}
	if got := callSequenceBuiltin(t, functions, "sum", powers, FunctionValue(constant)); got.Float64() != 510 {
		t.Fatalf("sum powers*2 = %s, want 510", got.Describe())
	}
	if constant.calls != 8 {
		t.Fatalf("constant auxiliary iterator calls = %d, want 8 (lazy bounded consumption)", constant.calls)
	}

	if got := callSequenceBuiltin(t, functions, "sum",
		ArrayValue(NewArray(Int(1), Int(2), Int(3))),
		ArrayValue(NewArray(Int(2), Int(3), Int(4))),
		ArrayValue(NewArray(Int(5), Int(5), Int(5)))); got.Float64() != 100 {
		t.Fatalf("sum aligned products = %s, want 100", got.Describe())
	}
	if got := callSequenceBuiltin(t, functions, "sum",
		ArrayValue(NewArray(Int(1), Int(2), Int(3))),
		ArrayValue(NewArray(Int(5)))); got.Float64() != 5 {
		t.Fatalf("sum short auxiliary = %s, want 5", got.Describe())
	}
	if got := callSequenceInvocation(t, functions, Invocation{Name: "sum"}); got.Kind() != KindDouble || got.Float64() != 0 {
		t.Fatalf("sum() = %s/%s, want double 0", got.Kind(), got.Describe())
	}
}

func TestSequenceSortsMutateAndPreserveCells(t *testing.T) {
	functions := newSequenceFunctionSet()
	for _, name := range []string{"sorta", "sortn", "sortd"} {
		if got := callSequenceInvocation(t, functions, Invocation{Name: name}); got.Kind() != KindArray {
			t.Fatalf("%s() = %s, want empty array", name, got.Describe())
		}
	}
	lexical := NewArray(String("b"), String("a"), String("a"))
	firstA, _ := lexical.Cell(1)
	secondA, _ := lexical.Cell(2)
	lexicalValue := ArrayValue(lexical)
	if got := callSequenceBuiltin(t, functions, "sorta", lexicalValue); !got.IdentityEqual(lexicalValue) {
		t.Fatal("sorta did not return its input array")
	}
	assertValueStrings(t, lexicalValue, []string{"a", "a", "b"})
	gotFirstA, _ := lexical.Cell(0)
	gotSecondA, _ := lexical.Cell(1)
	if gotFirstA != firstA || gotSecondA != secondA {
		t.Fatal("sorta was not stable or replaced scalar cells")
	}

	wrapped := ArrayValue(NewReadOnlyArray(String("b"), String("a")))
	worked := callSequenceBuiltin(t, functions, "sorta", wrapped)
	if worked.IdentityEqual(wrapped) {
		t.Fatal("sorta returned the read-only wrapper instead of a workable copy")
	}
	assertValueStrings(t, worked, []string{"a", "b"})
	assertValueStrings(t, wrapped, []string{"b", "a"})

	numeric := ArrayValue(NewArray(String("10"), Int(2), Long(-1)))
	callSequenceBuiltin(t, functions, "sortn", numeric)
	assertValueStrings(t, numeric, []string{"-1", "2", "10"})
	doubles := ArrayValue(NewArray(Double(1.5), Double(-2), Double(1.25)))
	callSequenceBuiltin(t, functions, "sortd", doubles)
	assertValueStrings(t, doubles, []string{"-2.0", "1.25", "1.5"})

	descending := FunctionValue(testCallable(func(_ context.Context, values ...Value) (Value, error) {
		return Int(values[1].Int32() - values[0].Int32()), nil
	}))
	custom := ArrayValue(NewArray(Int(2), Int(1), Int(3)))
	if got := callSequenceBuiltin(t, functions, "sort", descending, custom); !got.IdentityEqual(custom) {
		t.Fatal("sort did not return its input array")
	}
	assertValueStrings(t, custom, []string{"3", "2", "1"})

	broken := ArrayValue(NewArray(Int(2), Int(1)))
	wantErr := errors.New("comparator failed")
	callback := FunctionValue(testCallable(func(context.Context, ...Value) (Value, error) { return Null(), wantErr }))
	if _, err := functions["sort"](context.Background(), invocationOf("sort", callback, broken)); !errors.Is(err, wantErr) {
		t.Fatalf("sort comparator error = %v", err)
	}
	assertValueStrings(t, broken, []string{"2", "1"})
}

func TestSequenceReverseRangeZipAndMapValues(t *testing.T) {
	functions := newSequenceFunctionSet()
	source := ArrayValue(NewArray(String("a"), String("b"), String("c")))
	reversed := callSequenceBuiltin(t, functions, "reverse", source)
	assertValueStrings(t, reversed, []string{"c", "b", "a"})
	assertValueStrings(t, source, []string{"a", "b", "c"})
	if reversed.IdentityEqual(source) {
		t.Fatal("reverse mutated/returned its input instead of a new array")
	}

	assertValueStrings(t, callSequenceBuiltin(t, functions, "range", String("2,4-6,103")), []string{"2", "4", "5", "103"})
	assertValueStrings(t, callSequenceBuiltin(t, functions, "range", String("3-1")), []string{"3", "2"})
	if _, err := functions["range"](context.Background(), invocationOf("range", String("1,nope"))); err == nil ||
		!strings.Contains(err.Error(), "invalid range") {
		t.Fatalf("range malformed error = %v", err)
	}

	zipped := callSequenceBuiltin(t, functions, "zip",
		ArrayValue(NewArray(Int(1), Int(2), Int(3))),
		ArrayValue(NewArray(String("a"), String("b"))))
	zippedArray, _ := zipped.Array()
	rows := zippedArray.Values()
	if len(rows) != 2 {
		t.Fatalf("zip row count = %d, want 2", len(rows))
	}
	assertValueStrings(t, rows[0], []string{"1", "a"})
	assertValueStrings(t, rows[1], []string{"2", "b"})

	hash := NewHash()
	hash.Set("b", Int(2))
	hash.Set("a", Int(1))
	hashValue := HashValue(hash)
	mapper := FunctionValue(testCallable(func(_ context.Context, values ...Value) (Value, error) {
		return String(values[1].String() + ":" + values[0].String()), nil
	}))
	mapped := callSequenceBuiltin(t, functions, "mapValues", mapper, hashValue)
	if mapped.IdentityEqual(hashValue) {
		t.Fatal("mapValues returned/mutated its source hash")
	}
	mappedHash, _ := mapped.Hash()
	if got, _ := mappedHash.Get("b"); got.String() != "b:2" {
		t.Fatalf("mapValues[b] = %s, want b:2", got.Describe())
	}
	if got, _ := mappedHash.Get("a"); got.String() != "a:1" {
		t.Fatalf("mapValues[a] = %s, want a:1", got.Describe())
	}
	if got, _ := hash.Get("b"); got.Int32() != 2 {
		t.Fatalf("mapValues mutated source: b = %s", got.Describe())
	}
}

func TestSequenceFunctionsConcurrentReadsAndMutation(t *testing.T) {
	functions := newSequenceFunctionSet()
	value := ArrayValue(NewArray(Int(1), Int(2), Int(3)))
	additions := ArrayValue(NewArray(Int(4), Int(5)))
	const workers = 8

	var wait sync.WaitGroup
	errorsFound := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for iteration := 0; iteration < 50; iteration++ {
				name := "contains"
				invocation := invocationOf(name, value, Int(int32(worker)))
				if worker%2 != 0 {
					name = "addAll"
					invocation = invocationOf(name, value, additions)
				}
				if _, err := functions[name](context.Background(), invocation); err != nil {
					errorsFound <- err
					return
				}
			}
		}(worker)
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent sequence operation: %v", err)
	}
}

func TestSequenceSublistConcurrentStructuralMutation(t *testing.T) {
	root := NewArray(String("left"), String("right"))
	view, err := root.sublist(1, 1)
	if err != nil {
		t.Fatalf("sublist: %v", err)
	}
	const workers = 8
	const perWorker = 50
	var wait sync.WaitGroup
	errorsFound := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for item := 0; item < perWorker; item++ {
				if err := view.appendValues(Int(int32(worker*perWorker + item))); err != nil {
					errorsFound <- err
					return
				}
			}
		}(worker)
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent view append: %v", err)
	}
	if got, want := view.Len(), workers*perWorker; got != want {
		t.Fatalf("view length = %d, want %d", got, want)
	}
	if got, want := root.Len(), workers*perWorker+2; got != want {
		t.Fatalf("root length = %d, want %d", got, want)
	}
	values := root.Values()
	if values[0].String() != "left" || values[len(values)-1].String() != "right" {
		t.Fatalf("view insert boundaries moved: first=%s last=%s", values[0].Describe(), values[len(values)-1].Describe())
	}
}

type testCallable func(context.Context, ...Value) (Value, error)

func (callable testCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return callable(ctx, values...)
}

func (testCallable) isSleepSequenceClosure() {}

type nativeSequenceCallable func(context.Context, ...Value) (Value, error)

func (callable nativeSequenceCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return callable(ctx, values...)
}

type testSequenceIterator struct {
	mu     sync.Mutex
	values []Value
	index  int
}

func (iterator *testSequenceIterator) Invoke(_ context.Context, _ ...Value) (Value, error) {
	iterator.mu.Lock()
	defer iterator.mu.Unlock()
	if iterator.index >= len(iterator.values) {
		return Null(), nil
	}
	value := iterator.values[iterator.index]
	iterator.index++
	return value, nil
}

func (*testSequenceIterator) isSleepSequenceClosure() {}

// utilityTestIterator stands in for a script generator in the utility bridge
// tests; it therefore carries the same internal marker as testSequenceIterator.
func (*utilityTestIterator) isSleepSequenceClosure() {}

func (utilityTestCallable) isSleepSequenceClosure() {}

type constantSequenceCallable struct {
	mu    sync.Mutex
	value Value
	calls int
}

func (callable *constantSequenceCallable) Invoke(_ context.Context, _ ...Value) (Value, error) {
	callable.mu.Lock()
	defer callable.mu.Unlock()
	callable.calls++
	return callable.value, nil
}

func (*constantSequenceCallable) isSleepSequenceClosure() {}

func newSequenceFunctionSet() map[string]NativeFunc {
	return (&Runtime{}).sequenceFunctions()
}

func callSequenceBuiltin(t *testing.T, functions map[string]NativeFunc, name string, values ...Value) Value {
	t.Helper()
	return callSequenceInvocation(t, functions, invocationOf(name, values...))
}

func callSequenceInvocation(t *testing.T, functions map[string]NativeFunc, invocation Invocation) Value {
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
