package opfor

import (
	"bytes"
	"context"
	"sync"
	"testing"
)

func TestPortableCollectionsEmptySortedNavigableFactories(t *testing.T) {
	ctx := context.Background()
	sortedSetValue := invokePortableCollectionsUtilityForTest(t, ctx, "emptySortedSet")
	navigableSetValue := invokePortableCollectionsUtilityForTest(t, ctx, "emptyNavigableSet")
	sortedMapValue := invokePortableCollectionsUtilityForTest(t, ctx, "emptySortedMap")
	navigableMapValue := invokePortableCollectionsUtilityForTest(t, ctx, "emptyNavigableMap")
	if !sortedSetValue.IdentityEqual(navigableSetValue) || !sortedMapValue.IdentityEqual(navigableMapValue) {
		t.Fatal("sorted and navigable empty factories did not preserve their OpenJDK singleton aliases")
	}
	if !sortedSetValue.IdentityEqual(invokePortableCollectionsUtilityForTest(t, ctx, "emptySortedSet")) ||
		!sortedMapValue.IdentityEqual(invokePortableCollectionsUtilityForTest(t, ctx, "emptyNavigableMap")) {
		t.Fatal("empty sorted/navigable factories did not retain cached identity")
	}

	set := portableCollectionObjectForTest(t, sortedSetValue)
	if set.className() != portableCollectionsEmptyNavigableSetClass {
		t.Fatalf("empty sorted set class = %q", set.className())
	}
	for _, class := range []string{"java.util.Set", "java.util.SortedSet", "java.util.NavigableSet", "java.io.Serializable"} {
		value, handled, err := set.invoke(ObjectInvocation{Op: ObjectTypeCheck, Class: class})
		if err != nil || !handled || !value.Truth() {
			t.Fatalf("empty sorted set is %s = (%s, %v, %v)", class, value.Describe(), handled, err)
		}
	}
	if comparator := invokePortableCollectionForTest(t, set, "comparator"); !comparator.IsNull() {
		t.Fatalf("natural empty set comparator = %s, want null", comparator.Describe())
	}
	if err := invokePortableCollectionErrorForTest(set, "first"); err == nil || err.Error() != "java.util.NoSuchElementException" {
		t.Fatalf("empty sorted set first = %v", err)
	}
	if err := invokePortableCollectionErrorForTest(set, "pollFirst"); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
		t.Fatalf("empty navigable set pollFirst = %v", err)
	}
	for _, message := range []string{"clear", "remove"} {
		var values []Value
		if message == "remove" {
			values = []Value{String("missing")}
		}
		if err := invokePortableCollectionErrorForTest(set, message, values...); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
			t.Fatalf("empty navigable set %s = %v", message, err)
		}
	}

	sortedRange := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, set, "subSet", String("a"), String("z")))
	navigableRange := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, set, "subSet", String("a"), Int(1), String("z"), Int(0)))
	if sortedRange.className() != portableCollectionsUnmodifiableSortedSetClass || navigableRange.className() != portableCollectionsUnmodifiableNavigableSetClass {
		t.Fatalf("empty set range classes = %q/%q", sortedRange.className(), navigableRange.className())
	}
	requirePortableSerializableForTest(t, sortedRange)
	requirePortableSerializableForTest(t, navigableRange)
	if sortedRange == set || navigableRange == set || sortedRange == navigableRange {
		t.Fatal("empty set range operations must return fresh unmodifiable views")
	}
	if err := invokePortableCollectionErrorForTest(set, "subSet", String("z"), String("a")); err == nil || err.Error() != "java.lang.IllegalArgumentException: fromKey > toKey" {
		t.Fatalf("reverse natural set range = %v", err)
	}
	if err := invokePortableCollectionErrorForTest(set, "headSet", Null()); err == nil || err.Error() != "java.lang.NullPointerException" {
		t.Fatalf("null set endpoint = %v", err)
	}
	for _, message := range []string{"lower", "floor", "ceiling", "higher"} {
		if value := invokePortableCollectionForTest(t, set, message, Null()); !value.IsNull() {
			t.Fatalf("empty navigable set %s = %s, want null", message, value.Describe())
		}
	}

	descending := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, set, "descendingSet"))
	if descending.className() != portableCollectionsUnmodifiableNavigableSetClass || !descending.reverseOrder {
		t.Fatalf("descending empty set = class %q reverse %v", descending.className(), descending.reverseOrder)
	}
	requirePortableSerializableForTest(t, descending)
	comparatorValue := invokePortableCollectionForTest(t, descending, "comparator")
	comparator, ok := comparatorValue.data.(*portableJavaReverseComparator)
	if !ok || comparator == nil {
		t.Fatalf("descending comparator = %s", comparatorValue.Describe())
	}
	if comparison := invokePortableCollectionForTest(t, comparator, "compare", String("a"), String("z")); comparison.Int32() != 25 {
		t.Fatalf("reverse comparator a/z = %s, want 25", comparison.Describe())
	}
	if iterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, set, "descendingIterator")); invokePortableCollectionForTest(t, iterator, "hasNext").Int32() != 0 {
		t.Fatal("empty descending iterator has an element")
	}

	mapping := sortedMapValue.data.(*portableJavaMap)
	if mapping.className() != portableCollectionsEmptyNavigableMapClass {
		t.Fatalf("empty sorted map class = %q", mapping.className())
	}
	for _, class := range []string{"java.util.Map", "java.util.SortedMap", "java.util.NavigableMap", "java.io.Serializable"} {
		value, handled, err := mapping.invoke(ObjectInvocation{Op: ObjectTypeCheck, Class: class})
		if err != nil || !handled || !value.Truth() {
			t.Fatalf("empty sorted map is %s = (%s, %v, %v)", class, value.Describe(), handled, err)
		}
	}
	if comparator := invokePortableCollectionForTest(t, mapping, "comparator"); !comparator.IsNull() {
		t.Fatalf("natural empty map comparator = %s, want null", comparator.Describe())
	}
	if err := invokePortableCollectionErrorForTest(mapping, "firstKey"); err == nil || err.Error() != "java.util.NoSuchElementException" {
		t.Fatalf("empty sorted map firstKey = %v", err)
	}
	if err := invokePortableCollectionErrorForTest(mapping, "pollFirstEntry"); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
		t.Fatalf("empty navigable map pollFirstEntry = %v", err)
	}
	if err := invokePortableCollectionErrorForTest(mapping, "put", String("k"), String("v")); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
		t.Fatalf("empty navigable map put = %v", err)
	}
	sortedMapRange := invokePortableCollectionForTest(t, mapping, "subMap", String("a"), String("z")).data.(*portableJavaMap)
	navigableMapRange := invokePortableCollectionForTest(t, mapping, "subMap", String("a"), Int(1), String("z"), Int(0)).data.(*portableJavaMap)
	if sortedMapRange.className() != portableCollectionsUnmodifiableSortedMapClass || navigableMapRange.className() != portableCollectionsUnmodifiableNavigableMapClass {
		t.Fatalf("empty map range classes = %q/%q", sortedMapRange.className(), navigableMapRange.className())
	}
	requirePortableSerializableForTest(t, sortedMapRange)
	requirePortableSerializableForTest(t, navigableMapRange)
	descendingMap := invokePortableCollectionForTest(t, mapping, "descendingMap").data.(*portableJavaMap)
	requirePortableSerializableForTest(t, descendingMap)
	if err := invokePortableCollectionErrorForTest(mapping, "subMap", String("z"), String("a")); err == nil || err.Error() != "java.lang.IllegalArgumentException: fromKey > toKey" {
		t.Fatalf("reverse natural map range = %v", err)
	}
	for _, message := range []string{"lowerKey", "floorKey", "ceilingKey", "higherKey", "lowerEntry", "firstEntry", "lastEntry"} {
		var values []Value
		if message != "firstEntry" && message != "lastEntry" {
			values = []Value{Null()}
		}
		if value := invokePortableCollectionForTest(t, mapping, message, values...); !value.IsNull() {
			t.Fatalf("empty navigable map %s = %s, want null", message, value.Describe())
		}
	}

	navigableKeys := invokePortableCollectionForTest(t, mapping, "navigableKeySet")
	if !navigableKeys.IdentityEqual(navigableSetValue) || !navigableKeys.IdentityEqual(invokePortableCollectionForTest(t, mapping, "navigableKeySet")) {
		t.Fatal("empty navigable map key set did not retain the shared singleton identity")
	}
	views := []struct {
		message string
		class   string
	}{
		{"descendingKeySet", portableCollectionsUnmodifiableNavigableSetClass},
		{"keySet", "Collections$UnmodifiableSet"},
		{"entrySet", "Collections$UnmodifiableMap$UnmodifiableEntrySet"},
		{"values", "Collections$UnmodifiableCollection"},
	}
	for _, test := range views {
		value := invokePortableCollectionForTest(t, mapping, test.message)
		view := portableCollectionObjectForTest(t, value)
		if view.className() != test.class {
			t.Fatalf("empty navigable map %s class = %q, want %q", test.message, view.className(), test.class)
		}
		requirePortableSerializableForTest(t, view)
		if test.message != "descendingKeySet" && !value.IdentityEqual(invokePortableCollectionForTest(t, mapping, test.message)) {
			t.Fatalf("empty navigable map %s view was not cached", test.message)
		}
		if err := invokePortableCollectionErrorForTest(view, "clear"); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
			t.Fatalf("empty navigable map %s clear = %v", test.message, err)
		}
	}
}

func requirePortableSerializableForTest(t *testing.T, target portableCollectionTestInvoker) {
	t.Helper()
	value, handled, err := target.invoke(ObjectInvocation{Op: ObjectTypeCheck, Class: "java.io.Serializable"})
	if err != nil || !handled || !value.Truth() {
		t.Fatalf("%T isa java.io.Serializable = (%s, handled %v, %v)", target, value.Describe(), handled, err)
	}
}

func TestPortableCollectionsSortedNavigableIteratorMutationPrecedence(t *testing.T) {
	ctx := context.Background()
	set := portableCollectionObjectForTest(t, invokePortableCollectionsUtilityForTest(t, ctx, "emptyNavigableSet"))
	collections := []*portableJavaCollection{
		set,
		portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, set, "subSet", String("a"), String("z"))),
		portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, set, "subSet", String("a"), Int(1), String("z"), Int(0))),
		portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, set, "descendingSet")),
	}

	mapping := invokePortableCollectionsUtilityForTest(t, ctx, "emptyNavigableMap").data.(*portableJavaMap)
	maps := []*portableJavaMap{
		mapping,
		invokePortableCollectionForTest(t, mapping, "subMap", String("a"), String("z")).data.(*portableJavaMap),
		invokePortableCollectionForTest(t, mapping, "subMap", String("a"), Int(1), String("z"), Int(0)).data.(*portableJavaMap),
		invokePortableCollectionForTest(t, mapping, "descendingMap").data.(*portableJavaMap),
	}
	for _, current := range maps {
		for _, message := range []string{"keySet", "values", "entrySet"} {
			collections = append(collections, portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, current, message)))
		}
		if portableCollectionsNavigableMapClass(current.class) {
			collections = append(collections,
				portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, current, "navigableKeySet")),
				portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, current, "descendingKeySet")),
			)
		}
	}

	for _, collection := range collections {
		iterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, collection, "iterator"))
		if err := invokePortableCollectionErrorForTest(iterator, "remove"); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
			t.Fatalf("%s iterator.remove before traversal = %v", collection.className(), err)
		}
		if err := invokePortableCollectionErrorForTest(iterator, "next"); err == nil || err.Error() != "java.util.NoSuchElementException" {
			t.Fatalf("%s empty iterator.next = %v", collection.className(), err)
		}
		if err := invokePortableCollectionErrorForTest(iterator, "remove"); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
			t.Fatalf("%s iterator.remove after failed next = %v", collection.className(), err)
		}

		// Sorted sets and map views expose Iterator rather than ListIterator.
		// Exercise the shared ListIterator-shaped mutation dispatch directly so
		// future unmodifiable list-shaped views retain the same unconditional
		// wrapper precedence for remove, set, and add.
		listIterator := &portableJavaIterator{collection: collection, last: -1, listIterator: true}
		for _, mutation := range []struct {
			message string
			values  []Value
		}{
			{"remove", nil},
			{"set", []Value{String("replacement")}},
			{"add", []Value{String("addition")}},
		} {
			if err := invokePortableCollectionErrorForTest(listIterator, mutation.message, mutation.values...); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
				t.Fatalf("%s list iterator %s before traversal = %v", collection.className(), mutation.message, err)
			}
		}
	}
	nonemptyWrapper := &portableJavaCollection{
		class: portableCollectionsUnmodifiableNavigableSetClass, values: []Value{String("a")}, readOnly: true,
	}
	nonemptyIterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, nonemptyWrapper, "iterator"))
	if value := invokePortableCollectionForTest(t, nonemptyIterator, "next"); value.String() != "a" {
		t.Fatalf("nonempty unmodifiable iterator next = %s", value.Describe())
	}
	if err := invokePortableCollectionErrorForTest(nonemptyIterator, "remove"); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
		t.Fatalf("nonempty unmodifiable iterator.remove after traversal = %v", err)
	}

	descendingIterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, set, "descendingIterator"))
	if err := invokePortableCollectionErrorForTest(descendingIterator, "remove"); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
		t.Fatalf("descending iterator remove before traversal = %v", err)
	}

	// Preserve AbstractList/ListIterator state precedence for mutable lists.
	mutable := newPortableJavaCollection("ArrayList", []Value{String("x")})
	mutableIterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, mutable, "listIterator"))
	for _, mutation := range []struct {
		message string
		values  []Value
	}{
		{"remove", nil},
		{"set", []Value{String("replacement")}},
	} {
		if err := invokePortableCollectionErrorForTest(mutableIterator, mutation.message, mutation.values...); err == nil || err.Error() != "java.lang.IllegalStateException" {
			t.Fatalf("mutable list iterator %s before traversal = %v, want IllegalStateException", mutation.message, err)
		}
	}
	if err := invokePortableCollectionErrorForTest(mutableIterator, "add", String("prefix")); err != nil {
		t.Fatalf("mutable list iterator add before traversal = %v", err)
	}
}

func TestPortableCollectionsEmptySortedNavigableFactoriesRaceSafe(t *testing.T) {
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 200; iteration++ {
				_, _, _ = portableCollections(context.Background(), ObjectInvocation{
					Op: ObjectInvoke, Class: "java.util.Collections", Message: "emptyNavigableSet",
				})
				_, _, _ = portableCollectionsEmptyNavigableSet.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "descendingSet"})
				iteratorValue, _, _ := portableCollectionsEmptyNavigableSet.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "iterator"})
				iterator, _ := iteratorValue.Object()
				_, _, _ = iterator.(*portableJavaIterator).invoke(ObjectInvocation{Op: ObjectInvoke, Message: "remove"})
				_, _, _ = portableCollectionsEmptyNavigableMap.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "navigableKeySet"})
				_, _, _ = portableCollectionsEmptyNavigableMap.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "keySet"})
				_, _, _ = portableCollectionsReverseComparator.invoke(ObjectInvocation{
					Op: ObjectInvoke, Message: "compare", Arguments: []Argument{{Value: String("a")}, {Value: String("z")}},
				})
			}
		}()
	}
	wait.Wait()
}

const portableJavaCollectionsSortedFactoriesProbeSource = `$ss = [Collections emptySortedSet];
$ns = [Collections emptyNavigableSet];
$sm = [Collections emptySortedMap];
$nm = [Collections emptyNavigableMap];
if ($ss is $ns) { println("set-identity=1"); } else { println("set-identity=0"); }
if ($sm is $nm) { println("map-identity=1"); } else { println("map-identity=0"); }
$ssSerial = 0; if ($ss isa ^java.io.Serializable) { $ssSerial = 1; }
$nsSerial = 0; if ($ns isa ^java.io.Serializable) { $nsSerial = 1; }
$smSerial = 0; if ($sm isa ^java.io.Serializable) { $smSerial = 1; }
$nmSerial = 0; if ($nm isa ^java.io.Serializable) { $nmSerial = 1; }
println("serial=" . $ssSerial . "/" . $nsSerial . "/" . $smSerial . "/" . $nmSerial);
println([$ss getClass]);
println([$sm getClass]);
if ([$ss comparator] is $null) { println("set-comparator=null"); }
if ([$sm comparator] is $null) { println("map-comparator=null"); }
$sortedSet = [$ss subSet: "a", "z"];
$navigableSet = [$ns subSet: "a", 1, "z", 0];
println([$sortedSet getClass]);
println([$navigableSet getClass]);
println("set-navigation=" . [$ns lower: "x"] . "/" . [$ns floor: "x"] . "/" . [$ns ceiling: "x"] . "/" . [$ns higher: "x"]);
$descendingSet = [$ns descendingSet];
println([$descendingSet getClass]);
println([[$descendingSet comparator] getClass]);
println([[$descendingSet comparator] compare: "a", "z"]);
println([[$ns descendingIterator] hasNext]);
$sortedMap = [$sm subMap: "a", "z"];
$navigableMap = [$nm subMap: "a", 1, "z", 0];
println([$sortedMap getClass]);
println([$navigableMap getClass]);
println("map-navigation=" . [$nm lowerKey: "x"] . "/" . [$nm floorKey: "x"] . "/" . [$nm ceilingKey: "x"] . "/" . [$nm higherKey: "x"]);
$descendingMap = [$nm descendingMap];
println([$descendingMap getClass]);
println([[$descendingMap comparator] getClass]);
$sortedSetSerial = 0; if ($sortedSet isa ^java.io.Serializable) { $sortedSetSerial = 1; }
$navigableSetSerial = 0; if ($navigableSet isa ^java.io.Serializable) { $navigableSetSerial = 1; }
$descendingSetSerial = 0; if ($descendingSet isa ^java.io.Serializable) { $descendingSetSerial = 1; }
$sortedMapSerial = 0; if ($sortedMap isa ^java.io.Serializable) { $sortedMapSerial = 1; }
$navigableMapSerial = 0; if ($navigableMap isa ^java.io.Serializable) { $navigableMapSerial = 1; }
$descendingMapSerial = 0; if ($descendingMap isa ^java.io.Serializable) { $descendingMapSerial = 1; }
println("wrapper-serial=" . $sortedSetSerial . "/" . $navigableSetSerial . "/" . $descendingSetSerial . "/" . $sortedMapSerial . "/" . $navigableMapSerial . "/" . $descendingMapSerial);
$keys = [$nm navigableKeySet];
if ($keys is $ns) { println("keys-identity=1"); } else { println("keys-identity=0"); }
println([[$nm descendingKeySet] getClass]);
println([[$nm keySet] getClass]);
println([[$nm entrySet] getClass]);
println([[$nm values] getClass]);
$iterator = [$ss iterator]; [$iterator remove]; $iteratorError1 = checkError(); [$iterator next]; $iteratorNextError = checkError(); [$iterator remove]; $iteratorPostError = checkError();
$iterator = [$sortedSet iterator]; [$iterator remove]; $iteratorError2 = checkError();
$iterator = [$navigableSet iterator]; [$iterator remove]; $iteratorError3 = checkError();
$iterator = [$descendingSet iterator]; [$iterator remove]; $iteratorError4 = checkError();
$iterator = [$ns descendingIterator]; [$iterator remove]; $iteratorError5 = checkError();
println("set-iterator=" . $iteratorError1 . "/" . $iteratorError2 . "/" . $iteratorError3 . "/" . $iteratorError4 . "/" . $iteratorError5);
println("set-iterator-state=" . $iteratorError1 . "/" . $iteratorNextError . "/" . $iteratorPostError);
$iterator = [[$nm keySet] iterator]; [$iterator remove]; $iteratorError1 = checkError(); [$iterator next]; $iteratorNextError = checkError(); [$iterator remove]; $iteratorPostError = checkError();
$iterator = [[$nm values] iterator]; [$iterator remove]; $iteratorError2 = checkError();
$iterator = [[$nm entrySet] iterator]; [$iterator remove]; $iteratorError3 = checkError();
$iterator = [[$nm navigableKeySet] iterator]; [$iterator remove]; $iteratorError4 = checkError();
$iterator = [[$nm descendingKeySet] iterator]; [$iterator remove]; $iteratorError5 = checkError();
println("map-iterator=" . $iteratorError1 . "/" . $iteratorError2 . "/" . $iteratorError3 . "/" . $iteratorError4 . "/" . $iteratorError5);
println("map-iterator-state=" . $iteratorError1 . "/" . $iteratorNextError . "/" . $iteratorPostError);
$iterator = [[$sortedMap keySet] iterator]; [$iterator remove]; $iteratorError1 = checkError();
$iterator = [[$navigableMap keySet] iterator]; [$iterator remove]; $iteratorError2 = checkError();
$iterator = [[$descendingMap keySet] iterator]; [$iterator remove]; $iteratorError3 = checkError();
println("map-wrapper-iterator=" . $iteratorError1 . "/" . $iteratorError2 . "/" . $iteratorError3);
[$ns clear];
println(checkError());
[$nm put: "x", "y"];
println(checkError());
println("false=" . [$ns contains: "x"] . "/" . [$nm containsKey: "x"]);
`

const portableJavaCollectionsSortedFactoriesProbeOutput = `set-identity=1
map-identity=1
serial=1/1/1/1
class java.util.Collections$UnmodifiableNavigableSet$EmptyNavigableSet
class java.util.Collections$UnmodifiableNavigableMap$EmptyNavigableMap
set-comparator=null
map-comparator=null
class java.util.Collections$UnmodifiableSortedSet
class java.util.Collections$UnmodifiableNavigableSet
set-navigation=///
class java.util.Collections$UnmodifiableNavigableSet
class java.util.Collections$ReverseComparator
25
0
class java.util.Collections$UnmodifiableSortedMap
class java.util.Collections$UnmodifiableNavigableMap
map-navigation=///
class java.util.Collections$UnmodifiableNavigableMap
class java.util.Collections$ReverseComparator
wrapper-serial=1/1/1/1/1/1
keys-identity=1
class java.util.Collections$UnmodifiableNavigableSet
class java.util.Collections$UnmodifiableSet
class java.util.Collections$UnmodifiableMap$UnmodifiableEntrySet
class java.util.Collections$UnmodifiableCollection
set-iterator=java.lang.UnsupportedOperationException/java.lang.UnsupportedOperationException/java.lang.UnsupportedOperationException/java.lang.UnsupportedOperationException/java.lang.UnsupportedOperationException
set-iterator-state=java.lang.UnsupportedOperationException/java.util.NoSuchElementException/java.lang.UnsupportedOperationException
map-iterator=java.lang.UnsupportedOperationException/java.lang.UnsupportedOperationException/java.lang.UnsupportedOperationException/java.lang.UnsupportedOperationException/java.lang.UnsupportedOperationException
map-iterator-state=java.lang.UnsupportedOperationException/java.util.NoSuchElementException/java.lang.UnsupportedOperationException
map-wrapper-iterator=java.lang.UnsupportedOperationException/java.lang.UnsupportedOperationException/java.lang.UnsupportedOperationException
java.lang.UnsupportedOperationException
java.lang.UnsupportedOperationException
false=0/0
`

func TestPortableCollectionsSortedFactoriesRuntimeRouting(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-collections-sorted-factories.sl", portableJavaCollectionsSortedFactoriesProbeSource); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != portableJavaCollectionsSortedFactoriesProbeOutput {
		t.Fatalf("runtime sorted Collections factories output\nwant:\n%sgot:\n%s", portableJavaCollectionsSortedFactoriesProbeOutput, got)
	}
}

func TestPortableCollectionsSortedFactoriesObjectHostFirstRefusal(t *testing.T) {
	calls := 0
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if portableJavaClassName(invocation.Class) == "Collections" && invocation.Message == "emptyNavigableSet" {
			calls++
			return String("importer navigable set"), nil
		}
		return Null(), &UnsupportedError{Operation: "test object operation", Name: invocation.Message}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Eval(context.Background(), "java-collections-sorted-factory-host-first.sl", `return [Collections emptyNavigableSet];`)
	if err != nil {
		t.Fatal(err)
	}
	if result.String() != "importer navigable set" || calls != 1 {
		t.Fatalf("ObjectHost emptyNavigableSet = (%s, calls %d)", result.Describe(), calls)
	}
}

func TestPortableCollectionsSortedFactoriesOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	reference, err := officialSleepJavaCommand(java, "--add-opens=java.base/java.util=ALL-UNNAMED", "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaCollectionsSortedFactoriesProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep sorted Collections factory probe: %v\n%s", err, reference)
	}
	if string(reference) != portableJavaCollectionsSortedFactoriesProbeOutput {
		t.Fatalf("official Sleep sorted Collections factory output changed\nwant:\n%sgot:\n%s", portableJavaCollectionsSortedFactoriesProbeOutput, reference)
	}
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-collections-sorted-factories-differential.sl", portableJavaCollectionsSortedFactoriesProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official Sleep sorted Collections factory mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}
