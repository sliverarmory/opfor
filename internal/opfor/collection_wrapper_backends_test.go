package opfor

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
)

type portableCollectionTestInvoker interface {
	invoke(ObjectInvocation) (Value, bool, error)
}

func invokePortableCollectionForTest(t *testing.T, target portableCollectionTestInvoker, message string, values ...Value) Value {
	t.Helper()
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	result, handled, err := target.invoke(ObjectInvocation{Op: ObjectInvoke, Message: message, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", message, err)
	}
	if !handled {
		t.Fatalf("%s was not handled", message)
	}
	return result
}

func TestSleepUtilsCollectionWrapperKeepsLiveAndIndexedViewsDistinct(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "live-collection-wrapper.sl", `
$list = [new LinkedList];
[$list add: "a"];
$wrapped = [SleepUtils getArrayWrapper: $list];
$cached = $wrapped[0];
[$list set: 0, "updated"];
[$list add: "b"];
@walked = @();
foreach $item ($wrapped) { push(@walked, $item); }
$wrapped[0] = "ignored";
$sub = sublist($wrapped, 0, 2);
[$list set: 0, "later"];
[$list add: "c"];
return @(size($wrapped), $wrapped[0], "$list", @walked, $sub);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{
		"3", "a", "[later, b, c]", "@('updated', 'b')", "@('updated', 'b')",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("live collection wrapper values = %q, want %q", got, want)
	}
}

func TestSleepUtilsMapWrapperUsesLiveLookupsKeysAndSnapshotIteration(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "live-map-wrapper.sl", `
$map = [new HashMap];
[$map put: "a", "one"];
%wrapped = [SleepUtils getHashWrapper: $map];
@keys = keys(%wrapped);
@mapkeys = [SleepUtils getArrayWrapper: [$map keySet]];
$first = @keys[0];
$mapfirst = @mapkeys[0];
[$map put: "b", "two"];
%wrapped["a"] = "ignored";
%wrapped["missing"] = "added";
$before = %wrapped["a"] . "/" . [$map get: "a"] . "/" . [$map containsKey: "missing"];
[$map put: "a", "updated"];
@seen = @();
foreach $key => $item (%wrapped) {
   push(@seen, "$key=$item");
   if ($key eq "a") { [$map put: "c", "three"]; }
   remove();
}
[$map put: "nil", $null];
return @(
   size(%wrapped), size(@keys), @keys[0], size(@mapkeys), @mapkeys[0], $before,
   %wrapped["a"], [$map get: "a"], %wrapped["c"],
   size(@seen), size(values(%wrapped))
);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{
		"4", "4", "a", "4", "a", "one/one/0", "updated", "updated", "three", "2", "3",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("live map wrapper values = %q, want %q", got, want)
	}
}

func TestCollectionWrapperIteratorIsReadOnlyAndFailFast(t *testing.T) {
	collection := newPortableJavaCollection("LinkedList", []Value{String("a")})
	wrapper := newCollectionWrapperArray(collection)
	iterator := wrapper.backend.iterator(ArrayValue(wrapper))

	item, ok, err := iterator.next(context.Background())
	if err != nil || !ok || item.value.String() != "a" {
		t.Fatalf("first iterator item = (%s, %v, %v), want (a, true, nil)", item.value.Describe(), ok, err)
	}
	if _, _, err := collection.invoke(ObjectInvocation{
		Op: ObjectInvoke, Message: "add", Arguments: []Argument{{Value: String("b")}},
	}); err != nil {
		t.Fatalf("backing add: %v", err)
	}
	if _, _, err := iterator.next(context.Background()); !errors.Is(err, ErrArrayChangedDuringIteration) {
		t.Fatalf("next after backing mutation = %v, want ErrArrayChangedDuringIteration", err)
	}
	if err := iterator.remove(context.Background()); !errors.Is(err, errReadOnlyIterator) {
		t.Fatalf("iterator remove = %v, want read-only iterator", err)
	}
	if err := wrapper.appendValues(String("c")); !errors.Is(err, ErrReadOnlyArray) {
		t.Fatalf("wrapper append = %v, want ErrReadOnlyArray", err)
	}
}

func TestMapWrapperKeysAreLiveLazyReadOnlyAndFailFast(t *testing.T) {
	mapping := newPortableJavaMap("HashMap", nil)
	invokePortableCollectionForTest(t, mapping, "put", String("a"), String("one"))
	wrapper := newMapWrapperHash(mapping)
	keys, err := wrapper.backend.keysArray(nil)
	if err != nil {
		t.Fatalf("keysArray: %v", err)
	}

	first, ok := keys.Get(0)
	if !ok || first.String() != "a" {
		t.Fatalf("first cached key = (%s, %v), want (a, true)", first.Describe(), ok)
	}
	iterator := keys.backend.iterator(ArrayValue(keys))
	if item, ok, err := iterator.next(context.Background()); err != nil || !ok || item.value.String() != "a" {
		t.Fatalf("first key iterator item = (%s, %v, %v), want (a, true, nil)", item.value.Describe(), ok, err)
	}

	invokePortableCollectionForTest(t, mapping, "put", String("b"), String("two"))
	if keys.Len() != 2 {
		t.Fatalf("live key size = %d, want 2", keys.Len())
	}
	if cached, ok := keys.Get(0); !ok || cached.String() != "a" {
		t.Fatalf("cached key after map growth = (%s, %v), want (a, true)", cached.Describe(), ok)
	}
	if _, _, err := iterator.next(context.Background()); !errors.Is(err, ErrArrayChangedDuringIteration) {
		t.Fatalf("key iterator after map growth = %v, want ErrArrayChangedDuringIteration", err)
	}
	if err := keys.appendValues(String("c")); !errors.Is(err, ErrReadOnlyArray) {
		t.Fatalf("key wrapper append = %v, want ErrReadOnlyArray", err)
	}

	wrapper.Ensure("a").Set(String("ignored"))
	wrapper.Ensure("missing").Set(String("added"))
	if wrapper.Delete("a") {
		t.Fatal("Delete reported removal from a read-only map wrapper")
	}
	if got := invokePortableCollectionForTest(t, mapping, "get", String("a")); got.String() != "one" {
		t.Fatalf("backing value after wrapper assignment = %s, want one", got.Describe())
	}
	if got := invokePortableCollectionForTest(t, mapping, "containsKey", String("missing")); got.Truth() {
		t.Fatal("assignment through a detached missing-key cell inserted into the backing map")
	}
	if err := removeHashValues(wrapper, []Value{String("one")}); !errors.Is(err, ErrReadOnlyHash) {
		t.Fatalf("wrapper remove = %v, want ErrReadOnlyHash", err)
	}
}

func TestMapWrapperDataSnapshotRehashesEntrySetTraversal(t *testing.T) {
	mapping := newPortableJavaMap("HashMap", nil)
	// Aa and BB have the same Java String hash. A Java 7 HashMap source
	// traverses them as BB, Aa after these puts; MapWrapper.getData inserts that
	// traversal into a fresh HashMap, whose bucket chain is therefore Aa, BB.
	invokePortableCollectionForTest(t, mapping, "put", String("Aa"), String("one"))
	invokePortableCollectionForTest(t, mapping, "put", String("BB"), String("two"))

	snapshot := newMapWrapperHash(mapping).backend.dataSnapshot()
	if got, want := argvValueStrings(snapshot.KeyValues()), []string{"Aa", "BB"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("getData key order = %q, want %q", got, want)
	}

	invokePortableCollectionForTest(t, mapping, "put", String("Aa"), String("updated"))
	if got, _ := snapshot.Get("Aa"); got.String() != "one" {
		t.Fatalf("detached getData value after backing replacement = %s, want one", got.Describe())
	}
}

func TestMapWrapperReservedDataSnapshotIsAtomicWithMutation(t *testing.T) {
	mapping := newPortableJavaMap("HashMap", nil)
	invokePortableCollectionForTest(t, mapping, "put", String("a"), String("one"))
	backend := newMapWrapperHash(mapping).backend

	reservationEntered := make(chan int, 1)
	releaseReservation := make(chan struct{})
	type snapshotResult struct {
		snapshot *Hash
		err      error
	}
	result := make(chan snapshotResult, 1)
	go func() {
		snapshot, err := backend.dataSnapshotReserved(func(count int) error {
			reservationEntered <- count
			<-releaseReservation
			return nil
		})
		result <- snapshotResult{snapshot: snapshot, err: err}
	}()

	if count := <-reservationEntered; count != 1 {
		t.Fatalf("reserved entries = %d, want 1", count)
	}
	// The reservation callback runs under the map's read lock. This is the
	// boundary that makes its count authoritative for the following traversal
	// allocation and entry capture.
	if mapping.mu.TryLock() {
		mapping.mu.Unlock()
		t.Fatal("backing map write lock succeeded during snapshot reservation")
	}

	mutationDone := make(chan error, 1)
	go func() {
		_, handled, err := mapping.invoke(ObjectInvocation{
			Op:      ObjectInvoke,
			Message: "put",
			Arguments: []Argument{
				{Value: String("b")},
				{Value: String("two")},
			},
		})
		if !handled && err == nil {
			err = errors.New("HashMap.put was not handled")
		}
		mutationDone <- err
	}()
	close(releaseReservation)

	snapshot := <-result
	if snapshot.err != nil {
		t.Fatalf("reserved snapshot: %v", snapshot.err)
	}
	if snapshot.snapshot == nil || snapshot.snapshot.Len() != 1 {
		t.Fatalf("reserved snapshot length = %d, want 1", snapshot.snapshot.Len())
	}
	if _, exists := snapshot.snapshot.Get("b"); exists {
		t.Fatal("concurrent mutation entered the reserved snapshot")
	}
	if err := <-mutationDone; err != nil {
		t.Fatalf("concurrent mutation: %v", err)
	}
	mapping.mu.RLock()
	backingSize := len(mapping.values)
	mapping.mu.RUnlock()
	if got := backingSize; got != 2 {
		t.Fatalf("backing map length after mutation = %d, want 2", got)
	}
}

func TestWrapperSerializationUsesCurrentDetachedDataViews(t *testing.T) {
	collection := newPortableJavaCollection("LinkedList", []Value{String("a")})
	arrayWrapper := newCollectionWrapperArray(collection)
	if _, ok := arrayWrapper.Get(0); !ok {
		t.Fatal("initial indexed collection access failed")
	}
	invokePortableCollectionForTest(t, collection, "set", Int(0), String("updated"))
	invokePortableCollectionForTest(t, collection, "add", String("b"))
	arrayBytes, err := encodeSleepScalarStream(ArrayValue(arrayWrapper))
	if err != nil {
		t.Fatalf("encode collection wrapper: %v", err)
	}
	invokePortableCollectionForTest(t, collection, "set", Int(0), String("later"))
	decodedArrayValue, _, err := decodeSleepScalarStream(bytes.NewReader(arrayBytes))
	if err != nil {
		t.Fatalf("decode collection wrapper: %v", err)
	}
	decodedArray, _ := decodedArrayValue.Array()
	if got, want := argvValueStrings(decodedArray.Values()), []string{"updated", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("serialized collection snapshot = %q, want %q", got, want)
	}

	mapping := newPortableJavaMap("HashMap", nil)
	invokePortableCollectionForTest(t, mapping, "put", String("a"), String("one"))
	hashWrapper := newMapWrapperHash(mapping)
	invokePortableCollectionForTest(t, mapping, "put", String("a"), String("updated"))
	invokePortableCollectionForTest(t, mapping, "put", String("b"), String("two"))
	invokePortableCollectionForTest(t, mapping, "put", String("nil"), Null())
	hashBytes, err := encodeSleepScalarStream(HashValue(hashWrapper))
	if err != nil {
		t.Fatalf("encode map wrapper: %v", err)
	}
	invokePortableCollectionForTest(t, mapping, "put", String("a"), String("later"))
	invokePortableCollectionForTest(t, mapping, "put", String("c"), String("three"))
	decodedHashValue, _, err := decodeSleepScalarStream(bytes.NewReader(hashBytes))
	if err != nil {
		t.Fatalf("decode map wrapper: %v", err)
	}
	decodedHash, _ := decodedHashValue.Hash()
	if got, exists := decodedHash.Get("a"); !exists || got.String() != "updated" {
		t.Fatalf("serialized a = (%s, %v), want (updated, true)", got.Describe(), exists)
	}
	if got, exists := decodedHash.Get("b"); !exists || got.String() != "two" {
		t.Fatalf("serialized b = (%s, %v), want (two, true)", got.Describe(), exists)
	}
	if _, exists := decodedHash.Get("nil"); exists {
		t.Fatal("MapWrapper.getData serialization retained a null-valued entry")
	}
	if _, exists := decodedHash.Get("c"); exists {
		t.Fatal("decoded map snapshot observed a post-serialization backing insertion")
	}
}

func TestSequenceCursorRetainsCollectionWrapperFailFastIteration(t *testing.T) {
	collection := newPortableJavaCollection("LinkedList", []Value{String("a")})
	wrapper := newCollectionWrapperArray(collection)
	cursor, err := newSequenceCursor(ArrayValue(wrapper), "map")
	if err != nil {
		t.Fatalf("newSequenceCursor: %v", err)
	}
	if value, ok, err := cursor.next(context.Background()); err != nil || !ok || value.String() != "a" {
		t.Fatalf("first sequence value = (%s, %v, %v), want (a, true, nil)", value.Describe(), ok, err)
	}
	invokePortableCollectionForTest(t, collection, "add", String("b"))
	if _, _, err := cursor.next(context.Background()); !errors.Is(err, ErrArrayChangedDuringIteration) {
		t.Fatalf("sequence cursor after backing growth = %v, want ErrArrayChangedDuringIteration", err)
	}
}

func TestCollectionWrapperIteratorRemovalIsScriptWarning(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = runtime.Eval(context.Background(), "read-only-collection-iterator.sl", `
$list = [new LinkedList: @("a")];
$wrapped = [SleepUtils getArrayWrapper: $list];
foreach $item ($wrapped) { remove(); }
println("unreachable");
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got, want := output.String(), "Warning: iterator is read-only at read-only-collection-iterator.sl:4\nunreachable\n"; got != want {
		t.Fatalf("iterator removal output = %q, want %q", got, want)
	}
}
