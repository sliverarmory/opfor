package opfor

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func portableCollectionObjectForTest(t *testing.T, value Value) *portableJavaCollection {
	t.Helper()
	object, ok := value.Object()
	if !ok {
		t.Fatalf("value = %s, want portable collection object", value.Describe())
	}
	collection, ok := object.(*portableJavaCollection)
	if !ok || collection == nil {
		t.Fatalf("object = %T, want *portableJavaCollection", object)
	}
	return collection
}

func portableMapEntryForTest(t *testing.T, collection *portableJavaCollection, key string) *portableJavaMapEntry {
	t.Helper()
	values, err := collection.snapshotChecked()
	if err != nil {
		t.Fatalf("entry-set snapshot: %v", err)
	}
	for _, value := range values {
		entry, ok := portableJavaMapEntryValue(value)
		if ok && sleepCanonicalString(entry.keyValue) == key {
			return entry
		}
	}
	t.Fatalf("entry %q was not present", key)
	return nil
}

func invokePortableCollectionErrorForTest(target portableCollectionTestInvoker, message string, values ...Value) error {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	_, handled, err := target.invoke(ObjectInvocation{Op: ObjectInvoke, Message: message, Arguments: arguments})
	if !handled {
		return errors.New("portable collection invocation was not handled")
	}
	return err
}

func TestPortableJavaCollectionBulkContracts(t *testing.T) {
	list := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b")})
	source := newPortableJavaCollection("LinkedHashSet", []Value{String("b"), String("c")})
	if changed := invokePortableCollectionForTest(t, list, "addAll", ObjectValue(source)); !changed.Truth() {
		t.Fatal("list addAll reported no change")
	}
	if got, want := argvValueStrings(list.snapshot()), []string{"a", "b", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("list after addAll = %q, want %q", got, want)
	}
	needles := newPortableJavaCollection("ArrayList", []Value{String("a"), String("c")})
	if contains := invokePortableCollectionForTest(t, list, "containsAll", ObjectValue(needles)); !contains.Truth() {
		t.Fatal("containsAll(a, c) = false, want true")
	}

	remove := newPortableJavaCollection("HashSet", []Value{String("b")})
	if changed := invokePortableCollectionForTest(t, list, "removeAll", ObjectValue(remove)); !changed.Truth() {
		t.Fatal("removeAll(b) reported no change")
	}
	if changed := invokePortableCollectionForTest(t, list, "removeAll", ObjectValue(remove)); changed.Truth() {
		t.Fatal("second removeAll(b) reported a change")
	}
	retain := newPortableJavaCollection("ArrayList", []Value{String("c"), String("missing")})
	if changed := invokePortableCollectionForTest(t, list, "retainAll", ObjectValue(retain)); !changed.Truth() {
		t.Fatal("retainAll(c, missing) reported no change")
	}
	if got, want := argvValueStrings(list.snapshot()), []string{"c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("list after bulk filtering = %q, want %q", got, want)
	}
	if changed := invokePortableCollectionForTest(t, list, "retainAll", ObjectValue(retain)); changed.Truth() {
		t.Fatal("second retainAll(c, missing) reported a change")
	}

	indexed := newPortableJavaCollection("ArrayList", []Value{String("a"), String("d")})
	middle := newPortableJavaCollection("LinkedList", []Value{String("b"), String("c")})
	if changed := invokePortableCollectionForTest(t, indexed, "addAll", Int(1), ObjectValue(middle)); !changed.Truth() {
		t.Fatal("indexed addAll reported no change")
	}
	if got, want := argvValueStrings(indexed.snapshot()), []string{"a", "b", "c", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("indexed addAll = %q, want %q", got, want)
	}

	set := newPortableJavaCollection("LinkedHashSet", []Value{String("a")})
	duplicates := NewArray(String("a"), String("a"), String("b"))
	if changed := invokePortableCollectionForTest(t, set, "addAll", ArrayValue(duplicates)); !changed.Truth() {
		t.Fatal("set addAll reported no change")
	}
	if got, want := argvValueStrings(set.snapshot()), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("set after addAll = %q, want %q", got, want)
	}
	if changed := invokePortableCollectionForTest(t, set, "addAll", ArrayValue(duplicates)); changed.Truth() {
		t.Fatal("duplicate-only set addAll reported a change")
	}
}

func TestPortableJavaListSubListBackedViewAndFailFast(t *testing.T) {
	root := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b"), String("c"), String("d")})
	sub := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, root, "subList", Int(1), Int(3)))
	if got, want := argvValueStrings(sub.snapshot()), []string{"b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("initial subList = %q, want %q", got, want)
	}
	if list, _, err := sub.invoke(ObjectInvocation{Op: ObjectTypeCheck, Class: "java.util.List"}); err != nil || !list.Truth() {
		t.Fatalf("subList isa List = (%s, %v), want true", list.Describe(), err)
	}
	if previous := invokePortableCollectionForTest(t, sub, "set", Int(0), String("B")); previous.String() != "b" {
		t.Fatalf("subList.set previous = %s, want b", previous.Describe())
	}
	invokePortableCollectionForTest(t, sub, "add", String("X"))
	nested := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, sub, "subList", Int(1), Int(3)))
	if removed := invokePortableCollectionForTest(t, nested, "remove", Int(0)); removed.String() != "c" {
		t.Fatalf("nested subList.remove = %s, want c", removed.Describe())
	}
	if got, want := argvValueStrings(root.snapshot()), []string{"a", "B", "X", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root after subList changes = %q, want %q", got, want)
	}
	if got, want := argvValueStrings(sub.snapshot()), []string{"B", "X"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parent subList after nested removal = %q, want %q", got, want)
	}
	if got, want := argvValueStrings(nested.snapshot()), []string{"X"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nested subList after removal = %q, want %q", got, want)
	}

	iteratorValue := invokePortableCollectionForTest(t, sub, "iterator")
	iteratorObject, _ := iteratorValue.Object()
	iterator := iteratorObject.(*portableJavaIterator)
	if first := invokePortableCollectionForTest(t, iterator, "next"); first.String() != "B" {
		t.Fatalf("subList iterator first = %s, want B", first.Describe())
	}
	invokePortableCollectionForTest(t, iterator, "remove")
	if got, want := argvValueStrings(root.snapshot()), []string{"a", "X", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root after subList iterator remove = %q, want %q", got, want)
	}
	if changed := invokePortableCollectionForTest(t, sub, "addAll", ArrayValue(NewArray(String("Y"), String("Z")))); !changed.Truth() {
		t.Fatal("subList.addAll reported no change")
	}
	if changed := invokePortableCollectionForTest(t, sub, "removeAll", ArrayValue(NewArray(String("Y")))); !changed.Truth() {
		t.Fatal("subList.removeAll reported no change")
	}
	if got, want := argvValueStrings(root.snapshot()), []string{"a", "X", "Z", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root after subList bulk changes = %q, want %q", got, want)
	}
	if got, want := argvValueStrings(sub.snapshot()), []string{"X", "Z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("subList after bulk changes = %q, want %q", got, want)
	}

	invokePortableCollectionForTest(t, root, "add", String("tail"))
	if err := invokePortableCollectionErrorForTest(sub, "size"); err == nil || err.Error() != "java.util.ConcurrentModificationException" {
		t.Fatalf("stale subList.size error = %v, want ConcurrentModificationException", err)
	}
	if err := invokePortableCollectionErrorForTest(sub, "equals", ObjectValue(newPortableJavaCollection("ArrayList", nil))); err == nil || err.Error() != "java.util.ConcurrentModificationException" {
		t.Fatalf("stale subList.equals error = %v, want ConcurrentModificationException", err)
	}

	if err := invokePortableCollectionErrorForTest(root, "subList", Int(0), Int(20)); err == nil || err.Error() != "java.lang.IndexOutOfBoundsException: toIndex = 20" {
		t.Fatalf("subList upper-bound error = %v", err)
	}
}

func TestPortableJavaListAndSetEqualityHashContracts(t *testing.T) {
	arrayList := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b")})
	linkedList := newPortableJavaCollection("LinkedList", []Value{String("a"), String("b")})
	if equal := invokePortableCollectionForTest(t, arrayList, "equals", ObjectValue(linkedList)); !equal.Truth() {
		t.Fatal("equal lists with different implementations compared unequal")
	}
	leftHash := invokePortableCollectionForTest(t, arrayList, "hashCode")
	rightHash := invokePortableCollectionForTest(t, linkedList, "hashCode")
	if leftHash.Int32() != 4066 || rightHash.Int32() != leftHash.Int32() {
		t.Fatalf("list hashes = (%d, %d), want (4066, 4066)", leftHash.Int32(), rightHash.Int32())
	}
	reversed := newPortableJavaCollection("ArrayList", []Value{String("b"), String("a")})
	if equal := invokePortableCollectionForTest(t, arrayList, "equals", ObjectValue(reversed)); equal.Truth() {
		t.Fatal("lists with different order compared equal")
	}

	leftSet := newPortableJavaCollection("HashSet", []Value{String("a"), String("b")})
	rightSet := newPortableJavaCollection("LinkedHashSet", []Value{String("b"), String("a")})
	if equal := invokePortableCollectionForTest(t, leftSet, "equals", ObjectValue(rightSet)); !equal.Truth() {
		t.Fatal("sets with equal members compared unequal")
	}
	if hash := invokePortableCollectionForTest(t, leftSet, "hashCode"); hash.Int32() != 195 {
		t.Fatalf("set hash = %d, want 195", hash.Int32())
	}
	if equal := invokePortableCollectionForTest(t, leftSet, "equals", ObjectValue(arrayList)); equal.Truth() {
		t.Fatal("set compared equal to list")
	}

	integerList := newPortableJavaCollection("ArrayList", []Value{Int(1)})
	longList := newPortableJavaCollection("ArrayList", []Value{Long(1)})
	if equal := invokePortableCollectionForTest(t, integerList, "equals", ObjectValue(longList)); equal.Truth() {
		t.Fatal("Integer and Long elements compared equal")
	}
	positiveZero := newPortableJavaCollection("ArrayList", []Value{Double(0)})
	negativeZero := newPortableJavaCollection("ArrayList", []Value{Double(math.Copysign(0, -1))})
	if equal := invokePortableCollectionForTest(t, positiveZero, "equals", ObjectValue(negativeZero)); equal.Truth() {
		t.Fatal("Double positive and negative zero compared equal")
	}
	leftNaN := newPortableJavaCollection("ArrayList", []Value{Double(math.NaN())})
	rightNaN := newPortableJavaCollection("LinkedList", []Value{Double(math.Float64frombits(0x7ff0000000000001))})
	if equal := invokePortableCollectionForTest(t, leftNaN, "equals", ObjectValue(rightNaN)); !equal.Truth() {
		t.Fatal("canonical Java Double NaN values compared unequal")
	}
	if left, right := invokePortableCollectionForTest(t, leftNaN, "hashCode").Int32(), invokePortableCollectionForTest(t, rightNaN, "hashCode").Int32(); left != right {
		t.Fatalf("equal NaN list hashes = (%d, %d), want equal", left, right)
	}
}

func TestPortableJavaMapBulkEqualityAndBackedViews(t *testing.T) {
	left := newPortableJavaMap("LinkedHashMap", nil)
	invokePortableCollectionForTest(t, left, "put", String("a"), Int(1))
	invokePortableCollectionForTest(t, left, "put", String("b"), Int(2))
	right := newPortableJavaMap("HashMap", nil)
	invokePortableCollectionForTest(t, right, "put", String("b"), Int(2))
	invokePortableCollectionForTest(t, right, "put", String("a"), Int(1))

	if equal := invokePortableCollectionForTest(t, left, "equals", ObjectValue(right)); !equal.Truth() {
		t.Fatal("maps with equal mappings compared unequal")
	}
	leftHash := invokePortableCollectionForTest(t, left, "hashCode").Int32()
	rightHash := invokePortableCollectionForTest(t, right, "hashCode").Int32()
	if leftHash != 192 || rightHash != leftHash {
		t.Fatalf("map hashes = (%d, %d), want (192, 192)", leftHash, rightHash)
	}
	if contained := invokePortableCollectionForTest(t, left, "containsValue", Int(1)); !contained.Truth() {
		t.Fatal("containsValue(Integer 1) = false")
	}
	if contained := invokePortableCollectionForTest(t, left, "containsValue", Long(1)); contained.Truth() {
		t.Fatal("containsValue(Long 1) = true for Integer value")
	}

	copyMap := newPortableJavaMap("TreeMap", nil)
	invokePortableCollectionForTest(t, copyMap, "putAll", ObjectValue(left))
	if equal := invokePortableCollectionForTest(t, copyMap, "equals", ObjectValue(left)); !equal.Truth() {
		t.Fatal("putAll copy compared unequal to source")
	}
	extra := NewHash()
	extra.SetValue(String("c"), Int(3))
	invokePortableCollectionForTest(t, copyMap, "putAll", HashValue(extra))
	if got := invokePortableCollectionForTest(t, copyMap, "get", String("c")); got.Int32() != 3 {
		t.Fatalf("putAll Sleep hash value = %s, want 3", got.Describe())
	}

	entrySet := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, left, "entrySet"))
	keySet := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, left, "keySet"))
	values := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, left, "values"))
	if repeated := invokePortableCollectionForTest(t, left, "values"); !repeated.IdentityEqual(ObjectValue(values)) {
		t.Fatal("repeated Map.values() did not return the cached backed view")
	}
	if repeated := invokePortableCollectionForTest(t, left, "keySet"); !repeated.IdentityEqual(ObjectValue(keySet)) {
		t.Fatal("repeated Map.keySet() did not return the cached backed view")
	}
	if repeated := invokePortableCollectionForTest(t, left, "entrySet"); !repeated.IdentityEqual(ObjectValue(entrySet)) {
		t.Fatal("repeated Map.entrySet() did not return the cached backed view")
	}
	invokePortableCollectionForTest(t, left, "put", String("c"), Int(3))
	invokePortableCollectionForTest(t, right, "put", String("c"), Int(3))
	for name, view := range map[string]*portableJavaCollection{"entrySet": entrySet, "keySet": keySet, "values": values} {
		if size := invokePortableCollectionForTest(t, view, "size").Int32(); size != 3 {
			t.Fatalf("live %s size = %d, want 3", name, size)
		}
	}
	rightEntrySet := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, right, "entrySet"))
	rightKeySet := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, right, "keySet"))
	if equal := invokePortableCollectionForTest(t, entrySet, "equals", ObjectValue(rightEntrySet)); !equal.Truth() {
		t.Fatal("equal entry-set views compared unequal")
	}
	if leftViewHash, rightViewHash := invokePortableCollectionForTest(t, entrySet, "hashCode").Int32(), invokePortableCollectionForTest(t, rightEntrySet, "hashCode").Int32(); leftViewHash != rightViewHash {
		t.Fatalf("equal entry-set hashes = (%d, %d), want equal", leftViewHash, rightViewHash)
	}
	if equal := invokePortableCollectionForTest(t, keySet, "equals", ObjectValue(rightKeySet)); !equal.Truth() {
		t.Fatal("equal key-set views compared unequal")
	}
	if err := invokePortableCollectionErrorForTest(keySet, "add", String("d")); err == nil || err.Error() != "java.lang.UnsupportedOperationException" {
		t.Fatalf("keySet.add error = %v, want UnsupportedOperationException", err)
	}

	leftEntryA := portableMapEntryForTest(t, entrySet, "a")
	rightEntryA := portableMapEntryForTest(t, rightEntrySet, "a")
	if equal := invokePortableCollectionForTest(t, leftEntryA, "equals", ObjectValue(rightEntryA)); !equal.Truth() {
		t.Fatal("equal Map.Entry values compared unequal")
	}
	if leftEntryHash, rightEntryHash := invokePortableCollectionForTest(t, leftEntryA, "hashCode").Int32(), invokePortableCollectionForTest(t, rightEntryA, "hashCode").Int32(); leftEntryHash != rightEntryHash {
		t.Fatalf("equal entry hashes = (%d, %d), want equal", leftEntryHash, rightEntryHash)
	}
	if previous := invokePortableCollectionForTest(t, leftEntryA, "setValue", Int(9)); previous.Int32() != 1 {
		t.Fatalf("Map.Entry.setValue previous = %s, want 1", previous.Describe())
	}
	if got := invokePortableCollectionForTest(t, left, "get", String("a")); got.Int32() != 9 {
		t.Fatalf("map value after entry setValue = %s, want 9", got.Describe())
	}
	invokePortableCollectionForTest(t, leftEntryA, "setValue", Int(1))
	if removed := invokePortableCollectionForTest(t, entrySet, "remove", ObjectValue(rightEntryA)); !removed.Truth() {
		t.Fatal("entrySet.remove(equal entry) = false")
	}
	if present := invokePortableCollectionForTest(t, left, "containsKey", String("a")); present.Truth() {
		t.Fatal("entrySet.remove left its map key present")
	}

	keyIteratorValue := invokePortableCollectionForTest(t, keySet, "iterator")
	keyIteratorObject, _ := keyIteratorValue.Object()
	keyIterator := keyIteratorObject.(*portableJavaIterator)
	removedKey := invokePortableCollectionForTest(t, keyIterator, "next")
	invokePortableCollectionForTest(t, keyIterator, "remove")
	if present := invokePortableCollectionForTest(t, left, "containsKey", removedKey); present.Truth() {
		t.Fatalf("key-set iterator remove left key %s present", removedKey.Describe())
	}

	duplicateValues := newPortableJavaMap("LinkedHashMap", nil)
	invokePortableCollectionForTest(t, duplicateValues, "put", String("x"), String("same"))
	invokePortableCollectionForTest(t, duplicateValues, "put", String("y"), String("same"))
	duplicateView := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, duplicateValues, "values"))
	if removed := invokePortableCollectionForTest(t, duplicateView, "remove", String("same")); !removed.Truth() {
		t.Fatal("values.remove(existing) = false")
	}
	if present := invokePortableCollectionForTest(t, duplicateValues, "containsKey", String("x")); present.Truth() {
		t.Fatal("values.remove did not remove the first matching mapping")
	}
	if present := invokePortableCollectionForTest(t, duplicateValues, "containsKey", String("y")); !present.Truth() {
		t.Fatal("values.remove removed more than one matching mapping")
	}
	bulkViewMap := newPortableJavaMap("LinkedHashMap", nil)
	invokePortableCollectionForTest(t, bulkViewMap, "put", String("a"), Int(1))
	invokePortableCollectionForTest(t, bulkViewMap, "put", String("b"), Int(2))
	invokePortableCollectionForTest(t, bulkViewMap, "put", String("c"), Int(3))
	bulkKeys := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, bulkViewMap, "keySet"))
	retainedKeys := ArrayValue(NewArray(String("b"), String("c")))
	if contains := invokePortableCollectionForTest(t, bulkKeys, "containsAll", retainedKeys); !contains.Truth() {
		t.Fatal("keySet.containsAll(b, c) = false")
	}
	if changed := invokePortableCollectionForTest(t, bulkKeys, "retainAll", retainedKeys); !changed.Truth() {
		t.Fatal("keySet.retainAll(b, c) reported no change")
	}
	bulkValues := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, bulkViewMap, "values"))
	if changed := invokePortableCollectionForTest(t, bulkValues, "removeAll", ArrayValue(NewArray(Int(3)))); !changed.Truth() {
		t.Fatal("values.removeAll(3) reported no change")
	}
	if size := invokePortableCollectionForTest(t, bulkViewMap, "size").Int32(); size != 1 {
		t.Fatalf("map size after bulk view filtering = %d, want 1", size)
	}
	if present := invokePortableCollectionForTest(t, bulkViewMap, "containsKey", String("b")); !present.Truth() {
		t.Fatal("bulk view filtering removed retained key b")
	}

	failFastEntries := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, right, "entrySet"))
	failFastIteratorValue := invokePortableCollectionForTest(t, failFastEntries, "iterator")
	failFastIteratorObject, _ := failFastIteratorValue.Object()
	failFastIterator := failFastIteratorObject.(*portableJavaIterator)
	invokePortableCollectionForTest(t, right, "put", String("new"), Int(4))
	if err := invokePortableCollectionErrorForTest(failFastIterator, "next"); err == nil || err.Error() != "java.util.ConcurrentModificationException" {
		t.Fatalf("entry-set iterator after map growth = %v, want ConcurrentModificationException", err)
	}

	clearKeys := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, duplicateValues, "keySet"))
	invokePortableCollectionForTest(t, clearKeys, "clear")
	if size := invokePortableCollectionForTest(t, duplicateValues, "size").Int32(); size != 0 {
		t.Fatalf("map size after keySet.clear = %d, want 0", size)
	}
}

func TestPortableJavaCollectionBulkRuntimeRouting(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "java-collection-bulk.sl", `
$list = [new ArrayList: @("a", "d")];
$changed = [$list addAll: 1, [new LinkedHashSet: @("b", "c")]];
$sub = [$list subList: 1, 3];
[$sub remove: 0];
$map = [new LinkedHashMap];
[$map put: "key", "old"];
$entry = [[[$map entrySet] iterator] next];
$old = [$entry setValue: "updated"];
return @("$list", "$sub", $changed, [$list containsAll: @("a", "c")],
	[$map containsValue: "updated"], $old, [$entry getKey], [$entry getValue], [$entry getClass],
	$entry isa ^java.util.Map$Entry);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{
		"[a, c, d]", "[c]", "1", "1", "1", "old", "key", "updated", "class java.util.Map$Entry", "1",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime bulk collection values = %q, want %q", got, want)
	}
}

func TestPortableJavaCollectionConcurrentBulkViews(t *testing.T) {
	list := newPortableJavaCollection("ArrayList", []Value{Int(0)})
	mapping := newPortableJavaMap("LinkedHashMap", nil)
	invokePortableCollectionForTest(t, mapping, "put", String("seed"), Int(0))
	keySet := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, mapping, "keySet"))

	var wait sync.WaitGroup
	wait.Add(4)
	for worker := 0; worker < 2; worker++ {
		worker := worker
		go func() {
			defer wait.Done()
			for index := 0; index < 200; index++ {
				value := Int(int32(worker*200 + index + 1))
				_, _, _ = list.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "add", Arguments: []Argument{{Value: value}}})
				if index%3 == 0 {
					_, _, _ = list.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "remove", Arguments: []Argument{{Value: Int(0)}}})
				}
				key := String(value.String())
				_, _, _ = mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "put", Arguments: []Argument{{Value: key}, {Value: value}}})
				if index%3 == 0 {
					_, _, _ = mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "remove", Arguments: []Argument{{Value: key}}})
				}
			}
		}()
	}
	for worker := 0; worker < 2; worker++ {
		go func() {
			defer wait.Done()
			for index := 0; index < 300; index++ {
				_, _ = list.snapshotChecked()
				_, _ = list.javaHashCode()
				_ = list.equalValue(ObjectValue(list))
				_ = mapping.snapshotEntries()
				_ = mapping.javaHashCode()
				_ = mapping.String()
				_, _ = keySet.snapshotChecked()
			}
		}()
	}
	wait.Wait()

	viewRoot := newPortableJavaCollection("ArrayList", []Value{Int(0), Int(1), Int(2)})
	view := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, viewRoot, "subList", Int(0), Int(3)))
	wait.Add(2)
	go func() {
		defer wait.Done()
		for index := 0; index < 300; index++ {
			_, _, _ = view.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "add", Arguments: []Argument{{Value: Int(int32(index + 3))}}})
			_, _, _ = view.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "remove", Arguments: []Argument{{Value: Int(0)}}})
		}
	}()
	go func() {
		defer wait.Done()
		for index := 0; index < 300; index++ {
			value, _, err := view.invoke(ObjectInvocation{
				Op: ObjectInvoke, Message: "subList", Arguments: []Argument{{Value: Int(0)}, {Value: Int(0)}},
			})
			if err != nil {
				continue
			}
			if object, ok := value.Object(); ok {
				if nested, ok := object.(*portableJavaCollection); ok {
					_, _ = nested.snapshotChecked()
				}
			}
		}
	}()
	wait.Wait()
}

func TestPortableJavaMapViewsAreCachedAcrossConcurrentRetrieval(t *testing.T) {
	mapping := newPortableJavaMap("LinkedHashMap", nil)
	invokePortableCollectionForTest(t, mapping, "put", String("seed"), Int(1))

	methods := []string{"keySet", "values", "entrySet"}
	const workers = 32
	results := make([][]Value, len(methods))
	errorsByMethod := make([][]error, len(methods))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for methodIndex, method := range methods {
		results[methodIndex] = make([]Value, workers)
		errorsByMethod[methodIndex] = make([]error, workers)
		for worker := 0; worker < workers; worker++ {
			methodIndex, method, worker := methodIndex, method, worker
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				value, handled, err := mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: method})
				if !handled && err == nil {
					err = errors.New("portable map view invocation was not handled")
				}
				results[methodIndex][worker] = value
				errorsByMethod[methodIndex][worker] = err
			}()
		}
	}
	close(start)
	wait.Wait()

	for methodIndex, method := range methods {
		first := results[methodIndex][0]
		if _, ok := first.Object(); !ok {
			t.Fatalf("first %s result = %s, want object", method, first.Describe())
		}
		for worker := 0; worker < workers; worker++ {
			if err := errorsByMethod[methodIndex][worker]; err != nil {
				t.Fatalf("concurrent %s worker %d: %v", method, worker, err)
			}
			if !first.IdentityEqual(results[methodIndex][worker]) {
				t.Fatalf("concurrent %s worker %d returned a different view instance", method, worker)
			}
		}
	}
	if results[0][0].IdentityEqual(results[1][0]) || results[0][0].IdentityEqual(results[2][0]) || results[1][0].IdentityEqual(results[2][0]) {
		t.Fatal("different map view methods shared one cached collection instance")
	}

	invokePortableCollectionForTest(t, mapping, "put", String("second"), Int(2))
	for methodIndex, method := range methods {
		repeated := invokePortableCollectionForTest(t, mapping, method)
		if !repeated.IdentityEqual(results[methodIndex][0]) {
			t.Fatalf("%s returned a different instance after map mutation", method)
		}
		view := portableCollectionObjectForTest(t, repeated)
		if size := invokePortableCollectionForTest(t, view, "size").Int32(); size != 2 {
			t.Fatalf("cached %s size after map mutation = %d, want 2", method, size)
		}
	}
}
