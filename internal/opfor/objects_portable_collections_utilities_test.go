package opfor

import (
	"bytes"
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func invokePortableCollectionsUtilityForTest(t *testing.T, ctx context.Context, message string, values ...Value) Value {
	t.Helper()
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	result, handled, err := portableCollections(ctx, ObjectInvocation{
		Op: ObjectInvoke, Class: "java.util.Collections", Message: message, Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("Collections.%s: %v", message, err)
	}
	if !handled {
		t.Fatalf("Collections.%s was not handled", message)
	}
	return result
}

func portableCollectionsUtilityErrorForTest(ctx context.Context, message string, values ...Value) (bool, error) {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	_, handled, err := portableCollections(ctx, ObjectInvocation{
		Op: ObjectInvoke, Class: "java.util.Collections", Message: message, Arguments: arguments,
	})
	return handled, err
}

func TestPortableCollectionsNaturalOrderUtilities(t *testing.T) {
	ctx := context.Background()
	list := newPortableJavaCollection("ArrayList", []Value{Int(3), Int(1), Int(2), Int(1)})
	listValue := ObjectValue(list)

	if got := invokePortableCollectionsUtilityForTest(t, ctx, "frequency", listValue, Int(1)); got.Int32() != 2 {
		t.Fatalf("frequency = %s, want 2", got.Describe())
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "min", listValue); got.Int32() != 1 {
		t.Fatalf("min = %s, want 1", got.Describe())
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "max", listValue); got.Int32() != 3 {
		t.Fatalf("max = %s, want 3", got.Describe())
	}
	invokePortableCollectionsUtilityForTest(t, ctx, "sort", listValue)
	if got, want := argvValueStrings(list.snapshot()), []string{"1", "1", "2", "3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sort = %q, want %q", got, want)
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "binarySearch", listValue, Int(2)); got.Int32() != 2 {
		t.Fatalf("binarySearch(2) = %s, want 2", got.Describe())
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "binarySearch", listValue, Int(0)); got.Int32() != -1 {
		t.Fatalf("binarySearch(0) = %s, want -1", got.Describe())
	}
	// OpenJDK's indexed binary search returns the first midpoint it finds; it
	// does not normalize duplicate matches to their first or last position.
	duplicates := newPortableJavaCollection("ArrayList", []Value{Int(1), Int(1), Int(1), Int(1)})
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "binarySearch", ObjectValue(duplicates), Int(1)); got.Int32() != 1 {
		t.Fatalf("binarySearch duplicate midpoint = %s, want 1", got.Describe())
	}

	invokePortableCollectionsUtilityForTest(t, ctx, "reverse", listValue)
	if got, want := argvValueStrings(list.snapshot()), []string{"3", "2", "1", "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reverse = %q, want %q", got, want)
	}
	invokePortableCollectionsUtilityForTest(t, ctx, "rotate", listValue, Int(-1))
	if got, want := argvValueStrings(list.snapshot()), []string{"2", "1", "1", "3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rotate(-1) = %q, want %q", got, want)
	}
	invokePortableCollectionsUtilityForTest(t, ctx, "swap", listValue, Int(0), Int(3))
	if got, want := argvValueStrings(list.snapshot()), []string{"3", "1", "1", "2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("swap = %q, want %q", got, want)
	}
	invokePortableCollectionsUtilityForTest(t, ctx, "fill", listValue, Int(9))
	if got, want := argvValueStrings(list.snapshot()), []string{"9", "9", "9", "9"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fill = %q, want %q", got, want)
	}
}

func TestPortableCollectionsCopyReplaceDisjointAndArrayDetachment(t *testing.T) {
	ctx := context.Background()
	source := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b")})
	destination := newPortableJavaCollection("ArrayList", []Value{String("x"), String("y"), String("z")})
	invokePortableCollectionsUtilityForTest(t, ctx, "copy", ObjectValue(destination), ObjectValue(source))
	if got, want := argvValueStrings(destination.snapshot()), []string{"a", "b", "z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("copy = %q, want %q", got, want)
	}
	if changed := invokePortableCollectionsUtilityForTest(t, ctx, "replaceAll", ObjectValue(destination), String("a"), String("A")); !changed.Truth() {
		t.Fatal("replaceAll reported no replacement")
	}
	if changed := invokePortableCollectionsUtilityForTest(t, ctx, "replaceAll", ObjectValue(destination), String("missing"), String("M")); changed.Kind() != KindInt || changed.Int32() != 0 {
		t.Fatalf("replaceAll absent result = %s, want Java boolean integer 0", changed.Describe())
	}
	disjoint := newPortableJavaCollection("HashSet", []Value{String("q")})
	overlap := newPortableJavaCollection("HashSet", []Value{String("b")})
	if value := invokePortableCollectionsUtilityForTest(t, ctx, "disjoint", ObjectValue(destination), ObjectValue(disjoint)); !value.Truth() {
		t.Fatal("disjoint collection pair returned false")
	}
	if value := invokePortableCollectionsUtilityForTest(t, ctx, "disjoint", ObjectValue(destination), ObjectValue(overlap)); value.Kind() != KindInt || value.Int32() != 0 {
		t.Fatalf("overlapping disjoint result = %s, want Java boolean integer 0", value.Describe())
	}

	native := NewArray(Int(3), Int(2), Int(1))
	invokePortableCollectionsUtilityForTest(t, ctx, "sort", ArrayValue(native))
	if got, want := argvValueStrings(native.Values()), []string{"3", "2", "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Sleep array after detached Collections.sort = %q, want %q", got, want)
	}
}

func TestPortableCollectionsOrderingErrorsAndFloatOrder(t *testing.T) {
	ctx := context.Background()
	empty := ObjectValue(newPortableJavaCollection("ArrayList", nil))
	for _, message := range []string{"min", "max"} {
		handled, err := portableCollectionsUtilityErrorForTest(ctx, message, empty)
		if !handled || err == nil || err.Error() != "java.util.NoSuchElementException" {
			t.Fatalf("Collections.%s(empty) = (handled %v, %v), want NoSuchElementException", message, handled, err)
		}
	}
	heterogeneous := ObjectValue(newPortableJavaCollection("ArrayList", []Value{Int(1), String("a")}))
	if handled, err := portableCollectionsUtilityErrorForTest(ctx, "sort", heterogeneous); !handled || err == nil || err.Error() != "java.lang.ClassCastException" {
		t.Fatalf("heterogeneous sort = (handled %v, %v), want ClassCastException", handled, err)
	}
	small := ObjectValue(newPortableJavaCollection("ArrayList", []Value{String("x")}))
	large := ObjectValue(newPortableJavaCollection("ArrayList", []Value{String("a"), String("b")}))
	if handled, err := portableCollectionsUtilityErrorForTest(ctx, "copy", small, large); !handled || err == nil || err.Error() != "java.lang.IndexOutOfBoundsException: Source does not fit in dest" {
		t.Fatalf("short destination copy = (handled %v, %v), want Source does not fit in dest", handled, err)
	}

	floats := newPortableJavaCollection("ArrayList", []Value{
		Double(math.NaN()), Double(math.Inf(1)), Double(0), Double(math.Copysign(0, -1)), Double(math.Inf(-1)),
	})
	invokePortableCollectionsUtilityForTest(t, ctx, "sort", ObjectValue(floats))
	ordered := floats.snapshot()
	if !math.IsInf(ordered[0].Float64(), -1) || !math.Signbit(ordered[1].Float64()) || math.Signbit(ordered[2].Float64()) || !math.IsInf(ordered[3].Float64(), 1) || !math.IsNaN(ordered[4].Float64()) {
		t.Fatalf("Double natural order = %v, want -Inf, -0.0, +0.0, +Inf, NaN", ordered)
	}

	comparator := ObjectValue(struct{ Name string }{Name: "importer comparator"})
	if handled, err := portableCollectionsUtilityErrorForTest(ctx, "sort", ObjectValue(floats), comparator); handled || err != nil {
		t.Fatalf("non-null comparator fallback = (handled %v, %v), want importer-owned", handled, err)
	}
}

func TestPortableCollectionsSortRevisionMatchesListImplementation(t *testing.T) {
	ctx := context.Background()
	arrayList := newPortableJavaCollection("ArrayList", []Value{Int(2), Int(1)})
	arrayIterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, arrayList, "iterator"))
	staleAfterRootSort := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, arrayList, "subList", Int(0), Int(1)))
	invokePortableCollectionsUtilityForTest(t, ctx, "sort", ObjectValue(arrayList))
	if err := invokePortableCollectionErrorForTest(arrayIterator, "next"); err == nil || err.Error() != "java.util.ConcurrentModificationException" {
		t.Fatalf("ArrayList iterator after sort = %v, want ConcurrentModificationException", err)
	}
	if err := invokePortableCollectionErrorForTest(staleAfterRootSort, "size"); err == nil || err.Error() != "java.util.ConcurrentModificationException" {
		t.Fatalf("ArrayList subList after root sort = %v, want ConcurrentModificationException", err)
	}

	linkedList := newPortableJavaCollection("LinkedList", []Value{Int(2), Int(1)})
	linkedIterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, linkedList, "iterator"))
	invokePortableCollectionsUtilityForTest(t, ctx, "sort", ObjectValue(linkedList))
	if value := invokePortableCollectionForTest(t, linkedIterator, "next"); value.Int32() != 1 {
		t.Fatalf("LinkedList iterator after sort returned %s, want sorted first value 1", value.Describe())
	}

	root := newPortableJavaCollection("ArrayList", []Value{Int(3), Int(2), Int(1)})
	view := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, root, "subList", Int(0), Int(2)))
	viewIterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, view, "iterator"))
	invokePortableCollectionsUtilityForTest(t, ctx, "sort", ObjectValue(view))
	if value := invokePortableCollectionForTest(t, viewIterator, "next"); value.Int32() != 2 {
		t.Fatalf("ArrayList subList iterator after sort returned %s, want 2", value.Describe())
	}
}

func TestPortableCollectionsSequentialThresholdPaths(t *testing.T) {
	ctx := context.Background()
	searchValues := make([]Value, portableCollectionsBinarySearchThreshold)
	for index := range searchValues {
		searchValues[index] = Int(int32(index * 2))
	}
	search := newPortableJavaCollection("LinkedList", searchValues)
	if portableCollectionsRandomAccess(search) {
		t.Fatal("LinkedList unexpectedly implements RandomAccess")
	}
	searchView := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, search, "subList", Int(0), Int(10)))
	if portableCollectionsRandomAccess(searchView) {
		t.Fatal("LinkedList subList unexpectedly implements RandomAccess")
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "binarySearch", ObjectValue(search), Int(4998)); got.Int32() != 2499 {
		t.Fatalf("sequential binarySearch = %s, want 2499", got.Describe())
	}

	arrayRoot := newPortableJavaCollection("ArrayList", []Value{Int(2), Int(1)})
	arrayView := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, arrayRoot, "subList", Int(0), Int(2)))
	if !portableCollectionsRandomAccess(arrayView) {
		t.Fatal("ArrayList subList lost RandomAccess")
	}

	reverseValues := make([]Value, portableCollectionsReverseThreshold)
	for index := range reverseValues {
		reverseValues[index] = Int(int32(index))
	}
	reversed := newPortableJavaCollection("LinkedList", reverseValues)
	invokePortableCollectionsUtilityForTest(t, ctx, "reverse", ObjectValue(reversed))
	if got := reversed.snapshot(); got[0].Int32() != portableCollectionsReverseThreshold-1 || got[len(got)-1].Int32() != 0 {
		t.Fatalf("sequential reverse endpoints = (%s, %s)", got[0].Describe(), got[len(got)-1].Describe())
	}

	filled := newPortableJavaCollection("LinkedList", make([]Value, portableCollectionsFillThreshold))
	invokePortableCollectionsUtilityForTest(t, ctx, "fill", ObjectValue(filled), String("x"))
	for index, value := range filled.snapshot() {
		if value.String() != "x" {
			t.Fatalf("sequential fill[%d] = %s, want x", index, value.Describe())
		}
	}

	sourceValues := make([]Value, portableCollectionsCopyThreshold)
	for index := range sourceValues {
		sourceValues[index] = Int(int32(index))
	}
	source := newPortableJavaCollection("LinkedList", sourceValues)
	destination := newPortableJavaCollection("LinkedList", make([]Value, len(sourceValues)))
	invokePortableCollectionsUtilityForTest(t, ctx, "copy", ObjectValue(destination), ObjectValue(source))
	if got, want := argvValueStrings(destination.snapshot()), argvValueStrings(sourceValues); !reflect.DeepEqual(got, want) {
		t.Fatalf("sequential copy = %q, want %q", got, want)
	}

	rotateValues := make([]Value, portableCollectionsRotateThreshold)
	for index := range rotateValues {
		rotateValues[index] = Int(int32(index))
	}
	rotated := newPortableJavaCollection("LinkedList", rotateValues)
	invokePortableCollectionsUtilityForTest(t, ctx, "rotate", ObjectValue(rotated), Int(7))
	for index, value := range rotated.snapshot() {
		want := (index - 7 + len(rotateValues)) % len(rotateValues)
		if value.Int32() != int32(want) {
			t.Fatalf("sequential rotate[%d] = %s, want %d", index, value.Describe(), want)
		}
	}

	replaceValues := make([]Value, portableCollectionsReplaceAllThreshold)
	for index := range replaceValues {
		replaceValues[index] = String("old")
	}
	replaced := newPortableJavaCollection("LinkedList", replaceValues)
	if value := invokePortableCollectionsUtilityForTest(t, ctx, "replaceAll", ObjectValue(replaced), String("old"), String("new")); value.Int32() != 1 {
		t.Fatalf("sequential replaceAll = %s, want 1", value.Describe())
	}
	for index, value := range replaced.snapshot() {
		if value.String() != "new" {
			t.Fatalf("sequential replaceAll[%d] = %s, want new", index, value.Describe())
		}
	}
}

func TestPortableCollectionsBackedViewFailFastCancellationAndMetering(t *testing.T) {
	root := newPortableJavaCollection("ArrayList", []Value{Int(3), Int(2), Int(1)})
	view := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, root, "subList", Int(0), Int(2)))
	invokePortableCollectionForTest(t, root, "add", Int(0))
	if handled, err := portableCollectionsUtilityErrorForTest(context.Background(), "reverse", ObjectValue(view)); !handled || err == nil || err.Error() != "java.util.ConcurrentModificationException" {
		t.Fatalf("reverse stale subList = (handled %v, %v), want ConcurrentModificationException", handled, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	list := ObjectValue(newPortableJavaCollection("ArrayList", []Value{Int(1), Int(2)}))
	if handled, err := portableCollectionsUtilityErrorForTest(canceled, "reverse", list); !handled || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reverse = (handled %v, %v), want context.Canceled", handled, err)
	}
	metered := context.WithValue(context.Background(), executionMeterKey{}, &executionMeter{limit: 1})
	list = ObjectValue(newPortableJavaCollection("ArrayList", []Value{Int(1), Int(2), Int(3), Int(4)}))
	handled, err := portableCollectionsUtilityErrorForTest(metered, "reverse", list)
	var limit *LimitError
	if !handled || !errors.As(err, &limit) || limit.Resource != "instruction" || limit.Limit != 1 {
		t.Fatalf("metered reverse = (handled %v, %v), want instruction limit 1", handled, err)
	}
}

func TestPortableJavaMapNoCallbackDefaults(t *testing.T) {
	mapping := newPortableJavaMap("LinkedHashMap", nil)
	if got := invokePortableCollectionForTest(t, mapping, "getOrDefault", String("missing"), String("fallback")); got.String() != "fallback" {
		t.Fatalf("getOrDefault missing = %s, want fallback", got.Describe())
	}
	invokePortableCollectionForTest(t, mapping, "put", String("null"), Null())
	entrySet := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, mapping, "entrySet"))
	nullEntry := portableMapEntryForTest(t, entrySet, "null")
	mapping.mu.RLock()
	revision := mapping.mod
	mapping.mu.RUnlock()
	if got := invokePortableCollectionForTest(t, mapping, "getOrDefault", String("null"), String("fallback")); !got.IsNull() {
		t.Fatalf("getOrDefault present null = %s, want null", got.Describe())
	}
	if got := invokePortableCollectionForTest(t, mapping, "putIfAbsent", String("null"), String("value")); !got.IsNull() {
		t.Fatalf("putIfAbsent present null previous = %s, want null", got.Describe())
	}
	if got := invokePortableCollectionForTest(t, nullEntry, "getValue"); got.String() != "value" {
		t.Fatalf("live entry after putIfAbsent = %s, want value", got.Describe())
	}
	mapping.mu.RLock()
	if mapping.mod != revision {
		t.Fatalf("putIfAbsent null replacement revision = %d, want %d", mapping.mod, revision)
	}
	mapping.mu.RUnlock()
	if got := invokePortableCollectionForTest(t, mapping, "putIfAbsent", String("null"), String("other")); got.String() != "value" {
		t.Fatalf("putIfAbsent existing previous = %s, want value", got.Describe())
	}
	if removed := invokePortableCollectionForTest(t, mapping, "remove", String("null"), String("wrong")); removed.Kind() != KindInt || removed.Int32() != 0 {
		t.Fatalf("conditional remove wrong value result = %s, want Java boolean integer 0", removed.Describe())
	}
	if replaced := invokePortableCollectionForTest(t, mapping, "replace", String("null"), String("wrong"), String("new")); replaced.Kind() != KindInt || replaced.Int32() != 0 {
		t.Fatalf("conditional replace wrong value result = %s, want Java boolean integer 0", replaced.Describe())
	}
	if replaced := invokePortableCollectionForTest(t, mapping, "replace", String("null"), String("value"), String("new")); !replaced.Truth() {
		t.Fatal("conditional replace rejected the current value")
	}
	if got := invokePortableCollectionForTest(t, nullEntry, "getValue"); got.String() != "new" {
		t.Fatalf("live entry after conditional replace = %s, want new", got.Describe())
	}
	if previous := invokePortableCollectionForTest(t, mapping, "replace", String("null"), String("final")); previous.String() != "new" {
		t.Fatalf("two-argument replace previous = %s, want new", previous.Describe())
	}
	if removed := invokePortableCollectionForTest(t, mapping, "remove", String("null"), String("final")); !removed.Truth() {
		t.Fatal("conditional remove rejected the current value")
	}
	invokePortableCollectionForTest(t, mapping, "put", String("null"), String("replacement"))
	if got := invokePortableCollectionForTest(t, nullEntry, "getValue"); got.String() != "final" {
		t.Fatalf("removed entry reattached after same-key insert: %s", got.Describe())
	}
}

func TestPortableCollectionsAndMapDefaultsAreRaceSafe(t *testing.T) {
	list := newPortableJavaCollection("ArrayList", []Value{Int(3), Int(2), Int(1), Int(0)})
	mapping := newPortableJavaMap("LinkedHashMap", nil)
	invokePortableCollectionForTest(t, mapping, "put", String("key"), Int(0))

	var wait sync.WaitGroup
	wait.Add(4)
	for worker := 0; worker < 2; worker++ {
		go func(worker int) {
			defer wait.Done()
			for index := 0; index < 300; index++ {
				value := Int(int32(worker*1000 + index))
				_, _, _ = list.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "add", Arguments: []Argument{{Value: value}}})
				_, _, _ = list.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "remove", Arguments: []Argument{{Value: value}}})
				_, _, _ = mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "putIfAbsent", Arguments: []Argument{{Value: String("key")}, {Value: value}}})
				_, _, _ = mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "replace", Arguments: []Argument{{Value: String("key")}, {Value: value}}})
			}
		}(worker)
	}
	for worker := 0; worker < 2; worker++ {
		go func() {
			defer wait.Done()
			for index := 0; index < 300; index++ {
				_, _, _ = portableCollections(context.Background(), ObjectInvocation{Op: ObjectInvoke, Class: "java.util.Collections", Message: "reverse", Arguments: []Argument{{Value: ObjectValue(list)}}})
				_, _, _ = portableCollections(context.Background(), ObjectInvocation{Op: ObjectInvoke, Class: "java.util.Collections", Message: "sort", Arguments: []Argument{{Value: ObjectValue(list)}}})
				_, _, _ = mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "getOrDefault", Arguments: []Argument{{Value: String("key")}, {Value: Null()}}})
				_, _, _ = mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "remove", Arguments: []Argument{{Value: String("missing")}, {Value: Null()}}})
			}
		}()
	}
	wait.Wait()
}

const portableJavaCollectionsUtilitiesProbeSource = `$list = [new ArrayList: @(3, 1, 2, 1)];
println([Collections frequency: $list, 1]);
println([Collections min: $list]);
println([Collections max: $list]);
[Collections sort: $list];
println($list);
println([Collections binarySearch: $list, 2]);
[Collections reverse: $list];
println($list);
[Collections rotate: $list, -1];
println($list);
[Collections swap: $list, 0, 3];
println($list);
[Collections fill: $list, 9];
println($list);
$source = [new ArrayList: @("a", "b")];
$dest = [new ArrayList: @("x", "y", "z")];
[Collections copy: $dest, $source];
println($dest);
println([Collections replaceAll: $dest, "a", "A"]);
println($dest);
$other = [new HashSet: @("q")];
println([Collections disjoint: $dest, $other]);
@native = @(3, 2, 1);
[Collections sort: @native];
println(@native);
$map = [new LinkedHashMap];
println([$map getOrDefault: "missing", "fallback"]);
println([$map putIfAbsent: "a", "one"]);
println([$map putIfAbsent: "a", "two"]);
println([$map remove: "a", "wrong"]);
println([$map replace: "a", "one", "ONE"]);
println([$map replace: "a", "final"]);
println([$map get: "a"]);
$search = [new LinkedList];
for ($i = 0; $i < 5000; $i++) { [$search add: $i * 2]; }
println([Collections binarySearch: $search, 4998]);
$reverse = [new LinkedList];
for ($i = 0; $i < 18; $i++) { [$reverse add: $i]; }
[Collections reverse: $reverse];
println([$reverse get: 0] . ":" . [$reverse get: 17]);
$fill = [new LinkedList];
for ($i = 0; $i < 25; $i++) { [$fill add: $i]; }
[Collections fill: $fill, "x"];
println([$fill get: 0] . ":" . [$fill get: 24]);
$copySource = [new LinkedList];
$copyDestination = [new LinkedList];
for ($i = 0; $i < 10; $i++) { [$copySource add: $i]; [$copyDestination add: -1]; }
[Collections copy: $copyDestination, $copySource];
println([$copyDestination get: 0] . ":" . [$copyDestination get: 9]);
$rotate = [new LinkedList];
for ($i = 0; $i < 100; $i++) { [$rotate add: $i]; }
[Collections rotate: $rotate, 7];
println([$rotate get: 0] . ":" . [$rotate get: 99]);
$replace = [new LinkedList];
for ($i = 0; $i < 11; $i++) { [$replace add: "old"]; }
$didReplace = [Collections replaceAll: $replace, "old", "new"];
println($didReplace . ":" . [$replace get: 0]);
`

const portableJavaCollectionsUtilitiesProbeOutput = `2
1
3
[1, 1, 2, 3]
2
[3, 2, 1, 1]
[2, 1, 1, 3]
[3, 1, 1, 2]
[9, 9, 9, 9]
[a, b, z]
1
[A, b, z]
1
@(3, 2, 1)
fallback

one
0
1
ONE
final
2499
17:0
x:x
0:9
93:92
1:new
`

func TestPortableCollectionsUtilitiesRuntimeRouting(t *testing.T) {
	var output bytes.Buffer
	removeArgumentCount := -1
	runtimeInstance, err := New(
		WithStdout(&output),
		WithStderr(&output),
		WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			if object, ok := invocation.Target.Object(); ok {
				if _, ok := object.(*portableJavaMap); ok && invocation.Message == "remove" {
					removeArgumentCount = len(invocation.Arguments)
				}
			}
			return Null(), &UnsupportedError{Operation: "test object operation", Name: invocation.Message}
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-collections-utilities.sl", portableJavaCollectionsUtilitiesProbeSource); err != nil {
		t.Fatal(err)
	}
	if removeArgumentCount != 2 {
		t.Fatalf("conditional Map.remove argument count = %d, want 2", removeArgumentCount)
	}
	if got := output.String(); got != portableJavaCollectionsUtilitiesProbeOutput {
		t.Fatalf("runtime Collections utilities output\nwant:\n%sgot:\n%s", portableJavaCollectionsUtilitiesProbeOutput, got)
	}
}

func TestPortableCollectionsUtilitiesObjectHostFirstRefusal(t *testing.T) {
	called := 0
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if portableJavaClassName(invocation.Class) == "Collections" && invocation.Message == "reverse" {
			called++
			return String("importer reverse"), nil
		}
		return Null(), &UnsupportedError{Operation: "test object operation", Name: invocation.Message}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Eval(context.Background(), "java-collections-host-first.sl", `
$list = [new ArrayList: @(1, 2, 3)];
return @([Collections reverse: $list], "$list");
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{"importer reverse", "[1, 2, 3]"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ObjectHost-first result = %q, want %q", got, want)
	}
	if called != 1 {
		t.Fatalf("importer Collections.reverse calls = %d, want 1", called)
	}
}

func TestPortableCollectionsUtilitiesOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	reference, err := officialSleepJavaCommand(java, "--add-opens=java.base/java.util=ALL-UNNAMED", "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaCollectionsUtilitiesProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep Collections utilities probe: %v\n%s", err, reference)
	}
	if string(reference) != portableJavaCollectionsUtilitiesProbeOutput {
		t.Fatalf("official Sleep Collections utilities output changed\nwant:\n%sgot:\n%s", portableJavaCollectionsUtilitiesProbeOutput, reference)
	}
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-collections-utilities-differential.sl", portableJavaCollectionsUtilitiesProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official Sleep Collections utilities mismatch\nwant:\n%sgot:\n%s", reference, output.Bytes())
	}
}
