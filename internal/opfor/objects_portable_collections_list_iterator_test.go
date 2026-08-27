package opfor

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func portableIteratorForTest(t *testing.T, value Value) *portableJavaIterator {
	t.Helper()
	object, ok := value.Object()
	if !ok {
		t.Fatalf("value = %s, want iterator object", value.Describe())
	}
	iterator, ok := object.(*portableJavaIterator)
	if !ok || iterator == nil {
		t.Fatalf("object = %T, want *portableJavaIterator", object)
	}
	return iterator
}

func TestPortableJavaListIteratorBidirectionalMutationState(t *testing.T) {
	list := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b"), String("c")})
	iterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, list, "listIterator", Int(1)))

	if matched, _, err := iterator.invoke(ObjectInvocation{Op: ObjectTypeCheck, Class: "java.util.ListIterator"}); err != nil || !matched.Truth() {
		t.Fatalf("ListIterator type check = (%s, %v), want true", matched.Describe(), err)
	}
	if got := invokePortableCollectionForTest(t, iterator, "nextIndex").Int32(); got != 1 {
		t.Fatalf("initial nextIndex = %d, want 1", got)
	}
	if got := invokePortableCollectionForTest(t, iterator, "previousIndex").Int32(); got != 0 {
		t.Fatalf("initial previousIndex = %d, want 0", got)
	}
	if !invokePortableCollectionForTest(t, iterator, "hasNext").Truth() || !invokePortableCollectionForTest(t, iterator, "hasPrevious").Truth() {
		t.Fatal("list iterator at index 1 did not have values in both directions")
	}

	if value := invokePortableCollectionForTest(t, iterator, "previous"); value.String() != "a" {
		t.Fatalf("previous = %s, want a", value.Describe())
	}
	invokePortableCollectionForTest(t, iterator, "set", String("A"))
	invokePortableCollectionForTest(t, iterator, "add", String("X"))
	if err := invokePortableCollectionErrorForTest(iterator, "set", String("invalid")); err == nil || err.Error() != "java.lang.IllegalStateException" {
		t.Fatalf("set immediately after add error = %v, want IllegalStateException", err)
	}
	if err := invokePortableCollectionErrorForTest(iterator, "remove"); err == nil || err.Error() != "java.lang.IllegalStateException" {
		t.Fatalf("remove immediately after add error = %v, want IllegalStateException", err)
	}
	if value := invokePortableCollectionForTest(t, iterator, "previous"); value.String() != "X" {
		t.Fatalf("previous after add = %s, want X", value.Describe())
	}
	invokePortableCollectionForTest(t, iterator, "remove")
	if err := invokePortableCollectionErrorForTest(iterator, "remove"); err == nil || err.Error() != "java.lang.IllegalStateException" {
		t.Fatalf("repeated remove error = %v, want IllegalStateException", err)
	}
	if value := invokePortableCollectionForTest(t, iterator, "next"); value.String() != "A" {
		t.Fatalf("next after removing previous value = %s, want A", value.Describe())
	}
	invokePortableCollectionForTest(t, iterator, "remove")
	invokePortableCollectionForTest(t, iterator, "add", String("P"))
	if value := invokePortableCollectionForTest(t, iterator, "next"); value.String() != "b" {
		t.Fatalf("next after add = %s, want b", value.Describe())
	}
	invokePortableCollectionForTest(t, iterator, "set", String("B"))
	if value := invokePortableCollectionForTest(t, iterator, "previous"); value.String() != "B" {
		t.Fatalf("previous after set = %s, want B", value.Describe())
	}
	invokePortableCollectionForTest(t, iterator, "set", String("BB"))

	if got, want := argvValueStrings(list.snapshot()), []string{"P", "BB", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("list after iterator state machine = %q, want %q", got, want)
	}
	if got := invokePortableCollectionForTest(t, iterator, "nextIndex").Int32(); got != 1 {
		t.Fatalf("final nextIndex = %d, want 1", got)
	}
	if got := invokePortableCollectionForTest(t, iterator, "previousIndex").Int32(); got != 0 {
		t.Fatalf("final previousIndex = %d, want 0", got)
	}
}

func TestPortableJavaListIteratorBoundsAndFailFastRules(t *testing.T) {
	list := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b")})
	for _, index := range []int32{-1, 3} {
		err := invokePortableCollectionErrorForTest(list, "listIterator", Int(index))
		want := fmt.Sprintf("java.lang.IndexOutOfBoundsException: Index: %d", index)
		if err == nil || err.Error() != want {
			t.Fatalf("listIterator(%d) error = %v, want %q", index, err, want)
		}
	}
	linked := newPortableJavaCollection("LinkedList", []Value{String("a"), String("b")})
	if err := invokePortableCollectionErrorForTest(linked, "listIterator", Int(3)); err == nil || err.Error() != "java.lang.IndexOutOfBoundsException: Index: 3, Size: 2" {
		t.Fatalf("LinkedList.listIterator(3) error = %v, want Index/Size form", err)
	}
	view := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, list, "subList", Int(0), Int(2)))
	if err := invokePortableCollectionErrorForTest(view, "listIterator", Int(3)); err == nil || err.Error() != "java.lang.IndexOutOfBoundsException: Index: 3, Size: 2" {
		t.Fatalf("SubList.listIterator(3) error = %v, want Index/Size form", err)
	}
	end := portableIteratorForTest(t, invokePortableCollectionForTest(t, list, "listIterator", Int(2)))
	if invokePortableCollectionForTest(t, end, "hasNext").Truth() {
		t.Fatal("listIterator(size).hasNext = true")
	}
	if !invokePortableCollectionForTest(t, end, "hasPrevious").Truth() {
		t.Fatal("listIterator(size).hasPrevious = false")
	}
	if value := invokePortableCollectionForTest(t, end, "previous"); value.String() != "b" {
		t.Fatalf("previous from end = %s, want b", value.Describe())
	}
	start := portableIteratorForTest(t, invokePortableCollectionForTest(t, list, "listIterator"))
	if err := invokePortableCollectionErrorForTest(start, "previous"); err == nil || err.Error() != "java.util.NoSuchElementException" {
		t.Fatalf("previous at start error = %v, want NoSuchElementException", err)
	}

	stale := portableIteratorForTest(t, invokePortableCollectionForTest(t, list, "listIterator"))
	if value := invokePortableCollectionForTest(t, stale, "next"); value.String() != "a" {
		t.Fatalf("stale iterator first value = %s, want a", value.Describe())
	}
	invokePortableCollectionForTest(t, list, "add", String("c"))
	// OpenJDK cursor predicates and index queries deliberately omit the
	// modCount check. Dereference and mutation methods remain fail-fast.
	if !invokePortableCollectionForTest(t, stale, "hasNext").Truth() {
		t.Fatal("stale iterator hasNext unexpectedly changed cursor state")
	}
	if got := invokePortableCollectionForTest(t, stale, "nextIndex").Int32(); got != 1 {
		t.Fatalf("stale iterator nextIndex = %d, want 1", got)
	}
	for _, operation := range []struct {
		message string
		args    []Value
	}{
		{message: "next"},
		{message: "previous"},
		{message: "set", args: []Value{String("x")}},
		{message: "add", args: []Value{String("x")}},
		{message: "remove"},
	} {
		err := invokePortableCollectionErrorForTest(stale, operation.message, operation.args...)
		if err == nil || err.Error() != "java.util.ConcurrentModificationException" {
			t.Fatalf("stale iterator %s error = %v, want ConcurrentModificationException", operation.message, err)
		}
	}

	shrunk := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b")})
	stalePastEnd := portableIteratorForTest(t, invokePortableCollectionForTest(t, shrunk, "listIterator", Int(2)))
	invokePortableCollectionForTest(t, shrunk, "remove", Int(1))
	invokePortableCollectionForTest(t, shrunk, "remove", Int(0))
	if !invokePortableCollectionForTest(t, stalePastEnd, "hasNext").Truth() {
		t.Fatal("stale cursor past a shrunken ArrayList used cursor < size instead of OpenJDK cursor != size")
	}
	if err := invokePortableCollectionErrorForTest(stalePastEnd, "next"); err == nil || err.Error() != "java.util.ConcurrentModificationException" {
		t.Fatalf("stale cursor past shrunken list next error = %v, want ConcurrentModificationException", err)
	}
}

func TestPortableJavaListIteratorBackedNestedSubList(t *testing.T) {
	root := newPortableJavaCollection("ArrayList", []Value{Int(0), Int(1), Int(2), Int(3), Int(4)})
	parent := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, root, "subList", Int(1), Int(4)))
	nested := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, parent, "subList", Int(1), Int(3)))
	sibling := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, parent, "subList", Int(0), Int(1)))
	iterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, nested, "listIterator", Int(1)))

	if value := invokePortableCollectionForTest(t, iterator, "previous"); value.Int32() != 2 {
		t.Fatalf("nested previous = %s, want 2", value.Describe())
	}
	invokePortableCollectionForTest(t, iterator, "set", Int(20))
	invokePortableCollectionForTest(t, iterator, "add", Int(21))
	if value := invokePortableCollectionForTest(t, iterator, "next"); value.Int32() != 20 {
		t.Fatalf("nested next after add = %s, want 20", value.Describe())
	}
	invokePortableCollectionForTest(t, iterator, "remove")

	if got, want := argvValueStrings(root.snapshot()), []string{"0", "1", "21", "3", "4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root after nested iterator mutations = %q, want %q", got, want)
	}
	if got, want := argvValueStrings(parent.snapshot()), []string{"1", "21", "3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parent after nested iterator mutations = %q, want %q", got, want)
	}
	if got, want := argvValueStrings(nested.snapshot()), []string{"21", "3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nested after iterator mutations = %q, want %q", got, want)
	}
	if err := invokePortableCollectionErrorForTest(sibling, "size"); err == nil || err.Error() != "java.util.ConcurrentModificationException" {
		t.Fatalf("sibling view after nested structural change = %v, want ConcurrentModificationException", err)
	}

	// ArrayList.SubList.iterator() returns the same bidirectional wrapper as
	// listIterator(), despite the declared Iterator result type.
	plain := portableIteratorForTest(t, invokePortableCollectionForTest(t, nested, "iterator"))
	if matched, _, err := plain.invoke(ObjectInvocation{Op: ObjectTypeCheck, Class: "java.util.ListIterator"}); err != nil || !matched.Truth() {
		t.Fatalf("subList iterator ListIterator type check = (%s, %v), want true", matched.Describe(), err)
	}
	if value := invokePortableCollectionForTest(t, plain, "next"); value.Int32() != 21 {
		t.Fatalf("subList iterator next = %s, want 21", value.Describe())
	}
	if value := invokePortableCollectionForTest(t, plain, "previous"); value.Int32() != 21 {
		t.Fatalf("subList iterator previous = %s, want 21", value.Describe())
	}
}

func TestPortableJavaListIteratorRuntimeRouting(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-list-iterator.sl", portableJavaListIteratorProbeSource)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !value.IsNull() {
		t.Fatalf("probe result = %s, want empty scalar", value.Describe())
	}
	want := "1:0:1:1\na\n[X, A, b, c]:1:0\nX\nA\n[AA, b, c]\nAA\n[Y, AA, b, c]:[Y, AA, b]:1\n"
	if output.String() != want {
		t.Fatalf("runtime ListIterator output\nwant:\n%sgot:\n%s", want, output.String())
	}
}

func TestPortableJavaListIteratorConcurrentMutationIsRaceSafe(t *testing.T) {
	list := newPortableJavaCollection("ArrayList", []Value{Int(0), Int(1), Int(2), Int(3)})
	iterator := portableIteratorForTest(t, invokePortableCollectionForTest(t, list, "listIterator"))

	var wait sync.WaitGroup
	wait.Add(4)
	for worker := 0; worker < 2; worker++ {
		go func(worker int) {
			defer wait.Done()
			for index := 0; index < 500; index++ {
				value := Int(int32(worker*1000 + index + 10))
				_, _, _ = list.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "add", Arguments: []Argument{{Value: value}}})
				_, _, _ = list.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "remove", Arguments: []Argument{{Value: value}}})
			}
		}(worker)
	}
	for worker := 0; worker < 2; worker++ {
		go func() {
			defer wait.Done()
			for index := 0; index < 1000; index++ {
				_, _, _ = iterator.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "hasNext"})
				_, _, _ = iterator.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "hasPrevious"})
				_, _, _ = iterator.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "nextIndex"})
				_, _, _ = iterator.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "next"})
			}
		}()
	}
	wait.Wait()
}

func TestPortableJavaMapEntryDetachesAcrossRemoveAndReinsert(t *testing.T) {
	mapping := newPortableJavaMap("LinkedHashMap", nil)
	invokePortableCollectionForTest(t, mapping, "put", String("key"), String("old"))
	entrySet := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, mapping, "entrySet"))
	oldEntry := portableMapEntryForTest(t, entrySet, "key")
	if repeated := portableMapEntryForTest(t, entrySet, "key"); repeated != oldEntry {
		t.Fatal("unchanged map returned a different live Map.Entry node")
	}

	invokePortableCollectionForTest(t, mapping, "put", String("key"), String("updated"))
	if value := invokePortableCollectionForTest(t, oldEntry, "getValue"); value.String() != "updated" {
		t.Fatalf("live old entry after Map.put = %s, want updated", value.Describe())
	}
	invokePortableCollectionForTest(t, mapping, "remove", String("key"))
	invokePortableCollectionForTest(t, mapping, "put", String("key"), String("replacement"))

	if value := invokePortableCollectionForTest(t, oldEntry, "getValue"); value.String() != "updated" {
		t.Fatalf("detached old entry after reinsert = %s, want updated", value.Describe())
	}
	if previous := invokePortableCollectionForTest(t, oldEntry, "setValue", String("detached")); previous.String() != "updated" {
		t.Fatalf("detached old entry setValue previous = %s, want updated", previous.Describe())
	}
	if value := invokePortableCollectionForTest(t, mapping, "get", String("key")); value.String() != "replacement" {
		t.Fatalf("detached old entry changed reinserted mapping to %s", value.Describe())
	}

	newEntry := portableMapEntryForTest(t, entrySet, "key")
	if newEntry == oldEntry {
		t.Fatal("remove/reinsert reused the detached Map.Entry node")
	}
	if previous := invokePortableCollectionForTest(t, newEntry, "setValue", String("fresh")); previous.String() != "replacement" {
		t.Fatalf("new live entry setValue previous = %s, want replacement", previous.Describe())
	}
	if value := invokePortableCollectionForTest(t, mapping, "get", String("key")); value.String() != "fresh" {
		t.Fatalf("new live entry did not update mapping: %s", value.Describe())
	}

	invokePortableCollectionForTest(t, mapping, "clear")
	invokePortableCollectionForTest(t, mapping, "put", String("key"), String("after-clear"))
	if previous := invokePortableCollectionForTest(t, newEntry, "setValue", String("also-detached")); previous.String() != "fresh" {
		t.Fatalf("entry detached by clear had previous = %s, want fresh", previous.Describe())
	}
	if value := invokePortableCollectionForTest(t, mapping, "get", String("key")); value.String() != "after-clear" {
		t.Fatalf("entry detached by clear changed new mapping to %s", value.Describe())
	}
}

func TestPortableJavaMapEntryDetachmentIsRaceSafe(t *testing.T) {
	mapping := newPortableJavaMap("LinkedHashMap", nil)
	invokePortableCollectionForTest(t, mapping, "put", String("key"), Int(0))
	entrySet := portableCollectionObjectForTest(t, invokePortableCollectionForTest(t, mapping, "entrySet"))
	entry := portableMapEntryForTest(t, entrySet, "key")

	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		for index := 0; index < 1000; index++ {
			_, _, _ = mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "put", Arguments: []Argument{{Value: String("key")}, {Value: Int(int32(index))}}})
			_, _, _ = mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "remove", Arguments: []Argument{{Value: String("key")}}})
			_, _, _ = mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "put", Arguments: []Argument{{Value: String("key")}, {Value: Int(int32(-index))}}})
		}
	}()
	for worker := 0; worker < 2; worker++ {
		go func(worker int) {
			defer wait.Done()
			for index := 0; index < 1000; index++ {
				_, _, _ = entry.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "getValue"})
				_, _, _ = entry.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "setValue", Arguments: []Argument{{Value: Int(int32(worker*1000 + index))}}})
				_ = entry.String()
			}
		}(worker)
	}
	wait.Wait()
}

const portableJavaListIteratorProbeSource = `$list = [new ArrayList: @("a", "b", "c")];
$iterator = [$list listIterator: 1];
println([$iterator nextIndex] . ":" . [$iterator previousIndex] . ":" . [$iterator hasNext] . ":" . [$iterator hasPrevious]);
println([$iterator previous]);
[$iterator set: "A"];
[$iterator add: "X"];
println($list . ":" . [$iterator nextIndex] . ":" . [$iterator previousIndex]);
println([$iterator previous]);
[$iterator remove];
println("$list:" . [$iterator next]);
[$iterator set: "AA"];
println("$list");
$sub = [$list subList: 0, 2];
$subiterator = [$sub listIterator: 1];
println([$subiterator previous]);
[$subiterator add: "Y"];
println($list . ":" . $sub . ":" . [$subiterator nextIndex]);
`

func TestPortableJavaListIteratorOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	// Sleep 2.1 predates the Java module system and reflects through package-
	// private java.util iterator implementations. Open the package so a modern
	// JDK exercises the historical public-method behavior instead of emitting
	// module-access warnings.
	reference, err := officialSleepJavaCommand(java, "--add-opens=java.base/java.util=ALL-UNNAMED", "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaListIteratorProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep ListIterator probe: %v\n%s", err, reference)
	}
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-list-iterator-differential.sl", portableJavaListIteratorProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official Sleep ListIterator mismatch\nwant:\n%sgot:\n%s", reference, output.Bytes())
	}
}

const portableJavaMapEntryDetachmentProbeSource = `$map = [new LinkedHashMap];
[$map put: "key", "old"];
$entry = [[[$map entrySet] iterator] next];
[$map put: "key", "updated"];
println([$entry getValue]);
[$map remove: "key"];
[$map put: "key", "replacement"];
println([$entry getValue]);
println([$entry setValue: "detached"]);
println([$map get: "key"]);
$fresh = [[[$map entrySet] iterator] next];
println([$fresh setValue: "fresh"]);
println([$map get: "key"]);
`

func TestPortableJavaMapEntryDetachmentOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	reference, err := officialSleepJavaCommand(java, "--add-opens=java.base/java.util=ALL-UNNAMED", "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaMapEntryDetachmentProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep Map.Entry detachment probe: %v\n%s", err, reference)
	}
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-map-entry-detachment.sl", portableJavaMapEntryDetachmentProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official Sleep Map.Entry detachment mismatch\nwant:\n%sgot:\n%s", reference, output.Bytes())
	}
}
