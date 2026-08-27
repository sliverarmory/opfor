package opfor

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestPortableCollectionsShuffleMatchesJavaRandom(t *testing.T) {
	ctx := context.Background()
	want := []string{"b", "d", "e", "a", "c"}
	for _, class := range []string{"ArrayList", "LinkedList"} {
		list := newPortableJavaCollection(class, []Value{
			String("a"), String("b"), String("c"), String("d"), String("e"),
		})
		random := newPortableJavaRandom(123)
		invokePortableCollectionsUtilityForTest(t, ctx, "shuffle", ObjectValue(list), ObjectValue(random))
		if got := argvValueStrings(list.snapshot()); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s seeded shuffle = %q, want %q", class, got, want)
		}
		next, _, err := random.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "nextInt"})
		if err != nil || next.Int32() != 1087885590 {
			t.Fatalf("%s Random state after shuffle = (%s, %v), want 1087885590", class, next.Describe(), err)
		}
	}

	singleton := ObjectValue(newPortableJavaCollection("ArrayList", []Value{String("only")}))
	invokePortableCollectionsUtilityForTest(t, ctx, "shuffle", singleton, Null())
	two := ObjectValue(newPortableJavaCollection("ArrayList", []Value{String("a"), String("b")}))
	if handled, err := portableCollectionsUtilityErrorForTest(ctx, "shuffle", two, Null()); !handled || err == nil || err.Error() != "java.lang.NullPointerException" {
		t.Fatalf("shuffle(list, null) = (handled %v, %v), want NullPointerException", handled, err)
	}

	native := NewArray(String("a"), String("b"), String("c"), String("d"), String("e"))
	invokePortableCollectionsUtilityForTest(t, ctx, "shuffle", ArrayValue(native), ObjectValue(newPortableJavaRandom(123)))
	if got, unchanged := argvValueStrings(native.Values()), []string{"a", "b", "c", "d", "e"}; !reflect.DeepEqual(got, unchanged) {
		t.Fatalf("Sleep array after detached shuffle = %q, want %q", got, unchanged)
	}
}

func TestPortableCollectionsAddAllAndSubListSearch(t *testing.T) {
	ctx := context.Background()
	list := newPortableJavaCollection("ArrayList", nil)
	added := invokePortableCollectionsUtilityForTest(t, ctx, "addAll", ObjectValue(list), ArrayValue(NewArray(String("x"), String("y"), String("x"))))
	if added.Kind() != KindInt || added.Int32() != 1 {
		t.Fatalf("addAll result = %s, want Java boolean integer 1", added.Describe())
	}
	if got, want := argvValueStrings(list.snapshot()), []string{"x", "y", "x"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("addAll list = %q, want %q", got, want)
	}
	emptyAdd := invokePortableCollectionsUtilityForTest(t, ctx, "addAll", ObjectValue(list), ArrayValue(NewArray()))
	if emptyAdd.Kind() != KindInt || emptyAdd.Int32() != 0 {
		t.Fatalf("empty addAll result = %s, want Java boolean integer 0", emptyAdd.Describe())
	}

	objectArray := newPortableJavaArray(
		portableArrayType("java.lang.Object"), []int{2}, []Value{String("z"), Null()},
	)
	invokePortableCollectionsUtilityForTest(t, ctx, "addAll", ObjectValue(list), ObjectValue(objectArray))
	if got, want := argvValueStrings(list.snapshot()), []string{"x", "y", "x", "z", ""}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Object[] addAll = %q, want %q", got, want)
	}

	source := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b"), String("a"), String("b")})
	target := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b")})
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "indexOfSubList", ObjectValue(source), ObjectValue(target)); got.Int32() != 0 {
		t.Fatalf("indexOfSubList = %s, want 0", got.Describe())
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "lastIndexOfSubList", ObjectValue(source), ObjectValue(target)); got.Int32() != 2 {
		t.Fatalf("lastIndexOfSubList = %s, want 2", got.Describe())
	}
	empty := ObjectValue(newPortableJavaCollection("ArrayList", nil))
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "indexOfSubList", ObjectValue(source), empty); got.Int32() != 0 {
		t.Fatalf("indexOfSubList(empty) = %s, want 0", got.Describe())
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "lastIndexOfSubList", ObjectValue(source), empty); got.Int32() != 4 {
		t.Fatalf("lastIndexOfSubList(empty) = %s, want 4", got.Describe())
	}

	sequentialValues := make([]Value, portableCollectionsIndexOfThreshold)
	for index := range sequentialValues {
		sequentialValues[index] = Int(int32(index % 7))
	}
	sequential := newPortableJavaCollection("LinkedList", sequentialValues)
	sequentialTarget := newPortableJavaCollection("LinkedList", []Value{Int(5), Int(6), Int(0)})
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "indexOfSubList", ObjectValue(sequential), ObjectValue(sequentialTarget)); got.Int32() != 5 {
		t.Fatalf("sequential indexOfSubList = %s, want 5", got.Describe())
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "lastIndexOfSubList", ObjectValue(sequential), ObjectValue(sequentialTarget)); got.Int32() != 26 {
		t.Fatalf("sequential lastIndexOfSubList = %s, want 26", got.Describe())
	}
}

func TestPortableCollectionsEnumerationIsLiveAndListConsumesIt(t *testing.T) {
	ctx := context.Background()
	collection := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b"), String("a"), String("b")})
	enumerationValue := invokePortableCollectionsUtilityForTest(t, ctx, "enumeration", ObjectValue(collection))
	enumeration := portableIteratorForTest(t, enumerationValue)
	copyValue := invokePortableCollectionsUtilityForTest(t, ctx, "list", enumerationValue)
	copy := portableCollectionObjectForTest(t, copyValue)
	if got, want := argvValueStrings(copy.snapshot()), []string{"a", "b", "a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Collections.list = %q, want %q", got, want)
	}
	if got := invokePortableCollectionForTest(t, enumeration, "hasMoreElements"); got.Kind() != KindInt || got.Int32() != 0 {
		t.Fatalf("exhausted enumeration predicate = %s, want Java boolean integer 0", got.Describe())
	}

	staleValue := invokePortableCollectionsUtilityForTest(t, ctx, "enumeration", ObjectValue(collection))
	stale := portableIteratorForTest(t, staleValue)
	invokePortableCollectionForTest(t, collection, "add", String("c"))
	if !invokePortableCollectionForTest(t, stale, "hasMoreElements").Truth() {
		t.Fatal("live enumeration did not observe collection size change")
	}
	if err := invokePortableCollectionErrorForTest(stale, "nextElement"); err == nil || err.Error() != "java.util.ConcurrentModificationException" {
		t.Fatalf("live enumeration after structural change = %v, want ConcurrentModificationException", err)
	}

	ordinaryIterator := invokePortableCollectionForTest(t, collection, "iterator")
	if handled, err := portableCollectionsUtilityErrorForTest(ctx, "list", ordinaryIterator); !handled || err != nil {
		t.Fatalf("Collections.list(Iterator) = (handled %v, %v), want a no-match warning", handled, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if handled, err := portableCollectionsUtilityErrorForTest(canceled, "list", invokePortableCollectionsUtilityForTest(t, ctx, "enumeration", ObjectValue(collection))); !handled || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Collections.list = (handled %v, %v), want context.Canceled", handled, err)
	}
}

func TestPortableCollectionsImmutableFactories(t *testing.T) {
	ctx := context.Background()
	emptyListFirst := invokePortableCollectionsUtilityForTest(t, ctx, "emptyList")
	emptyListSecond := invokePortableCollectionsUtilityForTest(t, ctx, "emptyList")
	emptySet := invokePortableCollectionsUtilityForTest(t, ctx, "emptySet")
	emptyMapValue := invokePortableCollectionsUtilityForTest(t, ctx, "emptyMap")
	if !emptyListFirst.IdentityEqual(emptyListSecond) {
		t.Fatal("emptyList did not preserve cached singleton identity")
	}
	emptyMap := emptyMapValue.data.(*portableJavaMap)
	keySet := invokePortableCollectionForTest(t, emptyMap, "keySet")
	values := invokePortableCollectionForTest(t, emptyMap, "values")
	entrySet := invokePortableCollectionForTest(t, emptyMap, "entrySet")
	if !keySet.IdentityEqual(emptySet) || !values.IdentityEqual(emptySet) || !entrySet.IdentityEqual(emptySet) {
		t.Fatal("EmptyMap views do not share Collections.EMPTY_SET identity")
	}
	if hash := invokePortableCollectionForTest(t, emptyListFirst.data.(*portableJavaCollection), "hashCode"); hash.Int32() != 1 {
		t.Fatalf("emptyList hash = %s, want 1", hash.Describe())
	}
	if hash := invokePortableCollectionForTest(t, emptySet.data.(*portableJavaCollection), "hashCode"); hash.Int32() != 0 {
		t.Fatalf("emptySet hash = %s, want 0", hash.Describe())
	}
	if hash := invokePortableCollectionForTest(t, emptyMap, "hashCode"); hash.Int32() != 0 {
		t.Fatalf("emptyMap hash = %s, want 0", hash.Describe())
	}
	invokePortableCollectionForTest(t, emptyListFirst.data.(*portableJavaCollection), "clear")
	invokePortableCollectionForTest(t, emptyMap, "clear")

	copiesValue := invokePortableCollectionsUtilityForTest(t, ctx, "nCopies", Int(3), String("q"))
	copies := copiesValue.data.(*portableJavaCollection)
	if copies.copiesCount != 3 || len(copies.values) != 0 {
		t.Fatalf("nCopies storage = count %d, values %d, want compact 3/0", copies.copiesCount, len(copies.values))
	}
	if got := invokePortableCollectionForTest(t, copies, "toString"); got.String() != "[q, q, q]" {
		t.Fatalf("nCopies string = %s", got.Describe())
	}
	if got := invokePortableCollectionForTest(t, copies, "hashCode"); got.Int32() != 142000 {
		t.Fatalf("nCopies hash = %s, want 142000", got.Describe())
	}
	sub := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, copies, "subList", Int(1), Int(3)))
	gotSub := invokePortableCollectionForTest(t, sub, "toString").String()
	if sub.copiesCount != 2 || gotSub != "[q, q]" {
		t.Fatalf("nCopies subList = count %d, %q", sub.copiesCount, gotSub)
	}
	if handled, err := portableCollectionsUtilityErrorForTest(ctx, "nCopies", Int(-1), String("q")); !handled || err == nil || err.Error() != "java.lang.IllegalArgumentException: List length = -1" {
		t.Fatalf("nCopies(-1) = (handled %v, %v)", handled, err)
	}
	hugeValue := invokePortableCollectionsUtilityForTest(t, ctx, "nCopies", Int(2147483647), String("q"))
	huge := hugeValue.data.(*portableJavaCollection)
	if got := invokePortableCollectionForTest(t, huge, "size"); got.Int32() != 2147483647 {
		t.Fatalf("huge nCopies size = %s", got.Describe())
	}
	if got := invokePortableCollectionForTest(t, huge, "hashCode"); got.Int32() != -415642000 {
		t.Fatalf("huge nCopies hash = %s, want -415642000", got.Describe())
	}
	if len(huge.values) != 0 {
		t.Fatalf("huge nCopies materialized %d values", len(huge.values))
	}

	singletonListValue := invokePortableCollectionsUtilityForTest(t, ctx, "singletonList", String("v"))
	singletonList := singletonListValue.data.(*portableJavaCollection)
	if got := invokePortableCollectionForTest(t, singletonList, "hashCode"); got.Int32() != 149 {
		t.Fatalf("singletonList hash = %s, want 149", got.Describe())
	}
	invokePortableCollectionsUtilityForTest(t, ctx, "sort", singletonListValue)
	if err := invokePortableCollectionErrorForTest(singletonList, "set", Int(0), String("x")); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
		t.Fatalf("singletonList.set = %v, want UnsupportedOperationException", err)
	}

	singletonSetValue := invokePortableCollectionsUtilityForTest(t, ctx, "singleton", String("v"))
	singletonSet := singletonSetValue.data.(*portableJavaCollection)
	if got := invokePortableCollectionForTest(t, singletonSet, "hashCode"); got.Int32() != 118 {
		t.Fatalf("singleton set hash = %s, want 118", got.Describe())
	}
	if removed := invokePortableCollectionForTest(t, singletonSet, "remove", String("missing")); removed.Truth() {
		t.Fatal("singleton set removed an absent value")
	}

	singletonMapValue := invokePortableCollectionsUtilityForTest(t, ctx, "singletonMap", String("k"), String("v"))
	singletonMap := singletonMapValue.data.(*portableJavaMap)
	if got := invokePortableCollectionForTest(t, singletonMap, "hashCode"); got.Int32() != 29 {
		t.Fatalf("singletonMap hash = %s, want 29", got.Describe())
	}
	firstKeySet := invokePortableCollectionForTest(t, singletonMap, "keySet")
	secondKeySet := invokePortableCollectionForTest(t, singletonMap, "keySet")
	if !firstKeySet.IdentityEqual(secondKeySet) {
		t.Fatal("singletonMap keySet did not preserve cached identity")
	}
	mapEntry := portableMapEntryForTest(t, portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, singletonMap, "entrySet")), "k")
	if err := invokePortableCollectionErrorForTest(mapEntry, "setValue", String("x")); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
		t.Fatalf("singletonMap entry setValue = %v, want UnsupportedOperationException", err)
	}
}

func TestPortableCollectionsFactoryJavaFalseScalars(t *testing.T) {
	ctx := context.Background()
	emptyMap := invokePortableCollectionsUtilityForTest(t, ctx, "emptyMap")
	singletonList := invokePortableCollectionsUtilityForTest(t, ctx, "singletonList", String("v")).data.(*portableJavaCollection)
	singletonSet := invokePortableCollectionsUtilityForTest(t, ctx, "singleton", String("v")).data.(*portableJavaCollection)
	singletonMap := invokePortableCollectionsUtilityForTest(t, ctx, "singletonMap", String("k"), String("v")).data.(*portableJavaMap)

	values := []Value{
		invokePortableCollectionForTest(t, singletonList, "contains", String("missing")),
		invokePortableCollectionForTest(t, singletonSet, "remove", String("missing")),
		invokePortableCollectionForTest(t, singletonMap, "containsKey", String("missing")),
		invokePortableCollectionForTest(t, singletonMap, "equals", emptyMap),
	}
	for index, value := range values {
		if value.Kind() != KindInt || value.Int32() != 0 || value.String() != "0" {
			t.Fatalf("Java false result %d = %s/%q, want integer scalar 0", index, value.Describe(), value.String())
		}
	}
}

func TestPortableCollectionsCopiesListMaterializationLimitAndEquality(t *testing.T) {
	const outOfMemory = "java.lang.OutOfMemoryError: Required length exceeds implementation limit"
	huge := &portableJavaCollection{
		class: "Collections$CopiesList", readOnly: true, copies: true,
		copiesCount: portableCollectionsMaximumMaterializedElements + 1, copiesValue: String("q"),
	}
	for _, message := range []string{"toArray", "toString"} {
		_, handled, err := huge.invoke(ObjectInvocation{Op: ObjectInvoke, Message: message})
		if !handled || err == nil || err.Error() != outOfMemory {
			t.Fatalf("huge nCopies.%s = (handled %v, %v), want typed OOME", message, handled, err)
		}
		if exception := newPortableJavaException(err); exception == nil || !exception.isA("java.lang.OutOfMemoryError") {
			t.Fatalf("huge nCopies.%s exception = %#v, want OutOfMemoryError", message, exception)
		}
	}
	if got := huge.String(); got != outOfMemory {
		t.Fatalf("huge nCopies implicit string = %q, want bounded OOME diagnostic", got)
	}

	same := &portableJavaCollection{
		class: "Collections$CopiesList", readOnly: true, copies: true,
		copiesCount: huge.copiesCount, copiesValue: String("q"),
	}
	different := &portableJavaCollection{
		class: "Collections$CopiesList", readOnly: true, copies: true,
		copiesCount: huge.copiesCount, copiesValue: String("x"),
	}
	if equal, err := huge.equalValueChecked(ObjectValue(same)); err != nil || !equal {
		t.Fatalf("equal compact huge nCopies = (%v, %v), want (true, nil)", equal, err)
	}
	if equal, err := huge.equalValueChecked(ObjectValue(different)); err != nil || equal {
		t.Fatalf("different compact huge nCopies = (%v, %v), want (false, nil)", equal, err)
	}

	root := newPortableJavaCollection("ArrayList", nil)
	nonCompact := &portableJavaCollection{class: "AbstractList$SubList"}
	nonCompact.listView = &portableJavaListView{
		root: root, owner: nonCompact, size: huge.copiesCount, expectedMod: root.mod,
	}
	if _, err := huge.equalValueChecked(ObjectValue(nonCompact)); err == nil || err.Error() != outOfMemory {
		t.Fatalf("huge compact/non-compact equality = %v, want typed OOME", err)
	}
}

func TestPortableCollectionsFactoriesAndDefaultShuffleAreRaceSafe(t *testing.T) {
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for iteration := 0; iteration < 200; iteration++ {
				list := newPortableJavaCollection("ArrayList", []Value{Int(0), Int(1), Int(2), Int(3), Int(4)})
				_, _, _ = portableCollections(context.Background(), ObjectInvocation{
					Op: ObjectInvoke, Class: "java.util.Collections", Message: "shuffle",
					Arguments: []Argument{{Value: ObjectValue(list)}},
				})
				copies := &portableJavaCollection{
					class: "Collections$CopiesList", readOnly: true, copies: true,
					copiesCount: worker + iteration, copiesValue: String("q"),
				}
				_, _ = copies.javaHashCode()
				_ = portableCollectionsEmptyList.String()
				_ = portableCollectionsEmptyMap.String()
			}
		}(worker)
	}
	wait.Wait()
}

const portableJavaCollectionsFactoriesProbeSource = `$list = [new ArrayList: @("a", "b", "c", "d", "e")];
$random = [new Random: 123L];
[Collections shuffle: $list, $random];
println($list);
println([$random nextInt]);
$added = [new ArrayList];
println([Collections addAll: $added, @("x", "y", "x")]);
println($added);
$source = [new ArrayList: @("a", "b", "a", "b")];
$target = [new ArrayList: @("a", "b")];
println([Collections indexOfSubList: $source, $target]);
println([Collections lastIndexOfSubList: $source, $target]);
$emptyTarget = [new ArrayList];
println([Collections indexOfSubList: $source, $emptyTarget]);
println([Collections lastIndexOfSubList: $source, $emptyTarget]);
$enumeration = [Collections enumeration: $source];
println([Collections list: $enumeration]);
println([$enumeration hasMoreElements]);
$emptyList = [Collections emptyList];
$emptySet = [Collections emptySet];
$emptyMap = [Collections emptyMap];
println([$emptyList getClass]);
println([$emptySet getClass]);
println([$emptyMap getClass]);
println($emptyList . ":" . $emptySet . ":" . $emptyMap);
$copies = [Collections nCopies: 3, "q"];
println($copies . ":" . [$copies hashCode] . ":" . [$copies indexOf: "q"] . ":" . [$copies lastIndexOf: "q"]);
$singletonList = [Collections singletonList: "v"];
println($singletonList . ":" . [$singletonList hashCode]);
$singleton = [Collections singleton: "v"];
println($singleton . ":" . [$singleton hashCode]);
$singletonMap = [Collections singletonMap: "k", "v"];
println($singletonMap . ":" . [$singletonMap hashCode]);
println("c" . [$singletonList contains: "missing"] . ";r" . [$singleton remove: "missing"] . ";k" . [$singletonMap containsKey: "missing"] . ";e" . [$singletonMap equals: $emptyMap] . ";");
`

const portableJavaCollectionsFactoriesProbeOutput = `[b, d, e, a, c]
1087885590
1
[x, y, x]
0
2
0
4
[a, b, a, b]
0
class java.util.Collections$EmptyList
class java.util.Collections$EmptySet
class java.util.Collections$EmptyMap
[]:[]:{}
[q, q, q]:142000:0:2
[v]:149
[v]:118
{k=v}:29
c0;r0;k0;e0;
`

func TestPortableCollectionsFactoriesRuntimeRouting(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-collections-factories.sl", portableJavaCollectionsFactoriesProbeSource); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != portableJavaCollectionsFactoriesProbeOutput {
		t.Fatalf("runtime Collections factories output\nwant:\n%sgot:\n%s", portableJavaCollectionsFactoriesProbeOutput, got)
	}
}

func TestPortableCollectionsFactoriesObjectHostFirstRefusal(t *testing.T) {
	calls := 0
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if portableJavaClassName(invocation.Class) == "Collections" && invocation.Message == "nCopies" {
			calls++
			return String("importer copies"), nil
		}
		return Null(), &UnsupportedError{Operation: "test object operation", Name: invocation.Message}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Eval(context.Background(), "java-collections-factory-host-first.sl", `return [Collections nCopies: 2, "x"];`)
	if err != nil {
		t.Fatal(err)
	}
	if result.String() != "importer copies" || calls != 1 {
		t.Fatalf("ObjectHost nCopies = (%s, calls %d), want importer copies/1", result.Describe(), calls)
	}
}

func TestPortableCollectionsFactoriesOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	reference, err := officialSleepJavaCommand(java, "--add-opens=java.base/java.util=ALL-UNNAMED", "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaCollectionsFactoriesProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep Collections factory probe: %v\n%s", err, reference)
	}
	if string(reference) != portableJavaCollectionsFactoriesProbeOutput {
		t.Fatalf("official Sleep Collections factory output changed\nwant:\n%sgot:\n%s", portableJavaCollectionsFactoriesProbeOutput, reference)
	}
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-collections-factories-differential.sl", portableJavaCollectionsFactoriesProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official Sleep Collections factory mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}
