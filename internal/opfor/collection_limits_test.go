package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func newCollectionLimitedRuntime(t *testing.T, limit uint64, options ...Option) *Runtime {
	t.Helper()
	options = append([]Option{WithLimits(Limits{MaxCollectionEntriesPerRuntime: limit})}, options...)
	runtimeInstance, err := New(options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	return runtimeInstance
}

func assertCollectionLimitError(t *testing.T, err error, limit uint64) {
	t.Helper()
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("error = %v, want ErrResourceLimit", err)
	}
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || limitErr.Resource != resourceCollectionEntries || limitErr.Limit != limit {
		t.Fatalf("LimitError = %+v, want %q/%d", limitErr, resourceCollectionEntries, limit)
	}
}

func TestResourceLimitCannotBeCaughtBySleepTryCatch(t *testing.T) {
	runtimeInstance := newCollectionLimitedRuntime(t, 1)
	value, err := runtimeInstance.Eval(context.Background(), "uncatchable-limit.sl", `
debug(34);
try {
    @items = @(1, 2);
}
catch $exception {
    return "caught";
}
return "continued";
`)
	assertCollectionLimitError(t, err, 1)
	if !value.IsNull() {
		t.Fatalf("result = %s, want null after fatal resource limit", value.Describe())
	}
}

func TestCollectionLimitRejectsScriptLiteralAndRangeMaterialization(t *testing.T) {
	t.Run("array literal reserves atomically", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		_, err := runtimeInstance.Eval(context.Background(), "array-limit.sl", `return @(1, 2, 3);`)
		assertCollectionLimitError(t, err, 2)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 0 {
			t.Fatalf("failed array literal retained %d entries, want 0", got)
		}
	})

	t.Run("hash literal counts canonical unique keys", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 1)
		value, err := runtimeInstance.Eval(context.Background(), "hash-limit.sl", `return %("a" => 1, "a" => 2);`)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		hash, ok := value.Hash()
		if !ok || hash.Len() != 1 {
			t.Fatalf("result = %s, want one-entry hash", value.Describe())
		}
		stored, exists := hash.Get("a")
		if !exists || stored.Int32() != 2 {
			t.Fatalf("hash a = (%s, %t), want 2/true", stored.Describe(), exists)
		}
	})

	t.Run("range cannot bypass the budget", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		_, err := runtimeInstance.Eval(context.Background(), "range-limit.sl", `return range("1-4");`)
		assertCollectionLimitError(t, err, 2)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 2 {
			t.Fatalf("incremental range reservation = %d, want 2", got)
		}
	})
}

func TestCollectionLimitTreatsImporterValuesAsTrustedAndChargesScriptGrowth(t *testing.T) {
	trusted := NewArray(Int(1), Int(2))
	runtimeInstance := newCollectionLimitedRuntime(t, 1, WithInitialGlobals(map[string]Value{
		"@trusted": ArrayValue(trusted),
	}))

	_, err := runtimeInstance.Eval(context.Background(), "trusted-growth.sl", `
push(@trusted, 3);
push(@trusted, 4);
`)
	assertCollectionLimitError(t, err, 1)
	if got := trusted.Len(); got != 3 {
		t.Fatalf("trusted array length after rejected growth = %d, want 3", got)
	}
	if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 1 {
		t.Fatalf("charged entries = %d, want 1", got)
	}
}

func TestCollectionLimitRejectsBulkMaterializersBeforeEntryAppend(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "regex split", source: `return split(",", "a,b,c");`},
		{name: "binary unpack", source: `return unpack("b*", "abc");`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance := newCollectionLimitedRuntime(t, 2)
			_, err := runtimeInstance.Eval(context.Background(), test.name+".sl", test.source)
			assertCollectionLimitError(t, err, 2)
			if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 2 {
				t.Fatalf("incremental materialization usage = %d, want 2", got)
			}
		})
	}

	t.Run("Java String stream", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 3)
		value := newPortableJavaStringStream(String("abcd"), portableJavaStringCharsStream)
		object, _ := value.Object()
		stream := object.(*portableJavaStringStream)
		result, handled, err := stream.invoke(context.Background(), ObjectInvocation{
			Runtime: runtimeInstance,
			Target:  value,
			Op:      ObjectInvoke,
			Message: "toArray",
		})
		if !handled || !result.IsNull() {
			t.Fatalf("chars.toArray = (%s, %t), want null/handled", result.Describe(), handled)
		}
		assertCollectionLimitError(t, err, 3)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 3 {
			t.Fatalf("String stream usage = %d, want 3", got)
		}
	})

	t.Run("backtick output lines", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		command := "printf 'first\\nsecond\\nthird\\n'"
		if runtimePackageGOOS() == "windows" {
			command = "echo first&echo second&echo third"
		}
		value, err := runtimeInstance.Invoke(context.Background(), "__EXEC__", String(command))
		assertCollectionLimitError(t, err, 2)
		if !value.IsNull() {
			t.Fatalf("backtick result = %s, want null after rejected line-array reservation", value.Describe())
		}
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 0 {
			t.Fatalf("rejected backtick line-array usage = %d, want 0", got)
		}
	})
}

func TestCollectionLimitChargesOnlyRetainedSplitEntries(t *testing.T) {
	t.Run("Sleep split", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 1)
		value, err := runtimeInstance.Eval(context.Background(), "split-retained.sl", `return split(",", "a,,");`)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		array, ok := value.Array()
		if !ok || array.Len() != 1 || array.Values()[0].String() != "a" {
			t.Fatalf("split result = %s, want @(a)", value.Describe())
		}
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 1 {
			t.Fatalf("split usage = %d, want 1", got)
		}
	})

	t.Run("Java String.split", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 1)
		value, err := runtimeInstance.Eval(context.Background(), "java-split-retained.sl", `return ["a,," split: ","];`)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		array, ok := value.Array()
		if !ok || array.Len() != 1 || array.Values()[0].String() != "a" {
			t.Fatalf("String.split result = %s, want @(a)", value.Describe())
		}
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 1 {
			t.Fatalf("String.split usage = %d, want 1", got)
		}
	})
}

func TestCollectionLimitDoesNotChargeScalarStringStreamTerminals(t *testing.T) {
	runtimeInstance := newCollectionLimitedRuntime(t, 1)

	countValue := newPortableJavaStringStream(String("abcd"), portableJavaStringCharsStream)
	countStreamObject, _ := countValue.Object()
	countStream := countStreamObject.(*portableJavaStringStream)
	count, handled, err := countStream.invoke(context.Background(), ObjectInvocation{
		Runtime: runtimeInstance,
		Target:  countValue,
		Op:      ObjectInvoke,
		Message: "count",
	})
	if err != nil || !handled || count.Int64() != 4 {
		t.Fatalf("chars.count = (%s, %t, %v), want 4/true/nil", count.Describe(), handled, err)
	}

	sumValue := newPortableJavaStringStream(String("AB"), portableJavaStringCharsStream)
	sumStreamObject, _ := sumValue.Object()
	sumStream := sumStreamObject.(*portableJavaStringStream)
	sum, handled, err := sumStream.invoke(context.Background(), ObjectInvocation{
		Runtime: runtimeInstance,
		Target:  sumValue,
		Op:      ObjectInvoke,
		Message: "sum",
	})
	if err != nil || !handled || sum.Int32() != int32('A'+'B') {
		t.Fatalf("chars.sum = (%s, %t, %v), want 131/true/nil", sum.Describe(), handled, err)
	}
	if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 0 {
		t.Fatalf("scalar stream terminal usage = %d, want 0", got)
	}
}

func TestCollectionLimitReservesReverseAndCopyBeforeDestinationMaterialization(t *testing.T) {
	t.Run("reverse function iterator", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		_, err := builtinSleepReverse(context.Background(), Invocation{
			Runtime: runtimeInstance,
			Name:    "reverse",
			Arguments: []Argument{{Value: FunctionValue(&builtinSequence{
				values: []Value{Int(1), Int(2), Int(3)},
			})}},
		})
		assertCollectionLimitError(t, err, 2)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 2 {
			t.Fatalf("reverse iterator usage = %d, want 2", got)
		}
	})

	t.Run("copy live wrapper", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		wrapper := newCollectionWrapperArray(newPortableJavaCollection("ArrayList", []Value{
			Int(1), Int(2), Int(3),
		}))
		state := &collectionBuiltinState{}
		_, err := state.copy(context.Background(), Invocation{
			Runtime:   runtimeInstance,
			Name:      "copy",
			Arguments: []Argument{{Value: ArrayValue(wrapper)}},
		})
		assertCollectionLimitError(t, err, 2)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 2 {
			t.Fatalf("wrapper copy usage = %d, want 2", got)
		}
	})

	t.Run("sublist live wrapper", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		wrapper := newCollectionWrapperArray(newPortableJavaCollection("ArrayList", []Value{
			Int(1), Int(2), Int(3),
		}))
		backend := wrapper.backend.(*collectionWrapperArrayBackend)
		view, err := backend.sublistForRuntime(runtimeInstance, 0, 3)
		if view != nil {
			t.Fatalf("wrapper sublist = %s, want nil", ArrayValue(view).Describe())
		}
		assertCollectionLimitError(t, err, 2)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 2 {
			t.Fatalf("wrapper sublist usage = %d, want 2", got)
		}
	})

	t.Run("copy ordinary array reserves atomically", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		state := &collectionBuiltinState{}
		_, err := state.copy(context.Background(), Invocation{
			Runtime: runtimeInstance,
			Name:    "copy",
			Arguments: []Argument{{Value: ArrayValue(NewArray(
				Int(1), Int(2), Int(3),
			))}},
		})
		assertCollectionLimitError(t, err, 2)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 0 {
			t.Fatalf("ordinary copy failed reservation usage = %d, want 0", got)
		}
	})

	t.Run("values key iterator", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		hash := NewHash()
		for index, key := range []string{"a", "b", "c"} {
			hash.Set(key, Int(int32(index)))
		}
		_, err := builtinValues(context.Background(), Invocation{
			Runtime: runtimeInstance,
			Name:    "values",
			Arguments: []Argument{
				{Value: HashValue(hash)},
				{Value: FunctionValue(&builtinSequence{values: []Value{String("a"), String("b"), String("c")}})},
			},
		})
		assertCollectionLimitError(t, err, 2)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 2 {
			t.Fatalf("values iterator usage = %d, want 2", got)
		}
	})

	t.Run("flatten iterator", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		_, err := builtinFlatten(context.Background(), Invocation{
			Runtime: runtimeInstance,
			Name:    "flatten",
			Arguments: []Argument{{Value: FunctionValue(&builtinSequence{values: []Value{
				ArrayValue(NewArray(Int(1), Int(2))), Int(3),
			}})}},
		})
		assertCollectionLimitError(t, err, 2)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 2 {
			t.Fatalf("flatten iterator usage = %d, want 2", got)
		}
	})
}

func TestCollectionLimitMatchesPreflightsSelectedCaptures(t *testing.T) {
	runtimeInstance := newCollectionLimitedRuntime(t, 2, WithInitialGlobals(map[string]Value{
		"$text": String(strings.Repeat("a", 4096)),
	}))
	_, err := runtimeInstance.Eval(context.Background(), "matches-preflight.sl", `return matches($text, '(a)');`)
	assertCollectionLimitError(t, err, 2)
	if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 0 {
		t.Fatalf("failed matches preflight usage = %d, want 0", got)
	}
}

func TestCollectionLimitFilesystemListingsReserveBeforeResultMaterialization(t *testing.T) {
	directory := t.TempDir()
	for index := 0; index < 5; index++ {
		name := filepath.Join(directory, fmt.Sprintf("entry-%d", index))
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	t.Run("BasicIO listFiles batch", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		state := &ioBuiltinState{}
		_, err := state.listFiles(context.Background(), Invocation{
			Runtime:   runtimeInstance,
			Name:      "listFiles",
			Arguments: []Argument{{Value: String(directory)}},
		})
		assertCollectionLimitError(t, err, 2)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 0 {
			t.Fatalf("failed listing batch usage = %d, want 0", got)
		}
	})

	t.Run("portable File list", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 2)
		value, handled, err := newPortableJavaFile(String(directory)).portableListContextForRuntime(
			context.Background(), runtimeInstance, false, portableJavaFileFilterNone, nil,
		)
		if !handled || !value.IsNull() {
			t.Fatalf("File.list = (%s, %t), want null/handled", value.Describe(), handled)
		}
		assertCollectionLimitError(t, err, 2)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 2 {
			t.Fatalf("portable listing usage = %d, want 2", got)
		}
	})
}

func TestCollectionLimitPortableBulkGrowthIsAtomic(t *testing.T) {
	runtimeInstance := newCollectionLimitedRuntime(t, 2)
	list := newPortableJavaCollection("ArrayList", nil)
	source := ArrayValue(NewArray(String("a"), String("b"), String("c")))

	_, handled, err := list.invoke(ObjectInvocation{
		Runtime: runtimeInstance,
		Op:      ObjectInvoke,
		Message: "addAll",
		Arguments: []Argument{
			{Value: source},
		},
	})
	if !handled {
		t.Fatal("ArrayList.addAll was not handled")
	}
	assertCollectionLimitError(t, err, 2)
	if got := len(list.snapshot()); got != 0 {
		t.Fatalf("list length after rejected addAll = %d, want 0", got)
	}
	if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 0 {
		t.Fatalf("failed bulk reservation retained %d entries, want 0", got)
	}
}

func TestCollectionLimitPortableMapChargesOnlyNewKeys(t *testing.T) {
	runtimeInstance := newCollectionLimitedRuntime(t, 1)
	mapping := newPortableJavaMap("HashMap", nil)
	put := func(key string, value Value) error {
		_, handled, err := mapping.invoke(ObjectInvocation{
			Runtime: runtimeInstance,
			Op:      ObjectInvoke,
			Message: "put",
			Arguments: []Argument{
				{Value: String(key)},
				{Value: value},
			},
		})
		if !handled {
			t.Fatalf("HashMap.put(%q) was not handled", key)
		}
		return err
	}

	if err := put("a", Int(1)); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := put("a", Int(2)); err != nil {
		t.Fatalf("replacement put: %v", err)
	}
	assertCollectionLimitError(t, put("b", Int(3)), 1)
	mapping.mu.RLock()
	size := len(mapping.values)
	value, exists := mapping.values["a"]
	mapping.mu.RUnlock()
	if size != 1 {
		t.Fatalf("map size after rejected new key = %d, want 1", size)
	}
	if !exists || value.Int32() != 2 {
		t.Fatalf("map a = (%s, %t), want 2/true", value.Describe(), exists)
	}
}

func TestCollectionLimitOrdinaryHashConversionReservesStableSnapshot(t *testing.T) {
	runtimeInstance := newCollectionLimitedRuntime(t, 1)
	hash := NewHash()
	hash.Set("a", Int(1))

	reserved := -1
	entries, ok, err := portableMapEntriesReserved(HashValue(hash), func(count int) error {
		reserved = count
		// A concurrent or reentrant importer mutation after the stable capture
		// must not enlarge the destination built from the admitted entries.
		hash.Set("b", Int(2))
		return reserveCollectionEntries(runtimeInstance, count)
	})
	if err != nil || !ok {
		t.Fatalf("stable hash snapshot = (%t, %v)", ok, err)
	}
	if reserved != 1 || len(entries) != 1 {
		t.Fatalf("reserved/snapshotted entries = %d/%d, want 1/1", reserved, len(entries))
	}
	converted := newPortableJavaMapFromEntries("HashMap", entries)
	converted.mu.RLock()
	convertedSize := len(converted.values)
	_, convertedHasB := converted.values["b"]
	converted.mu.RUnlock()
	if convertedSize != 1 || convertedHasB {
		t.Fatalf("converted map size/has-b = %d/%t, want 1/false", convertedSize, convertedHasB)
	}
	if hash.Len() != 2 {
		t.Fatalf("source hash length after reservation hook = %d, want 2", hash.Len())
	}
	if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 1 {
		t.Fatalf("stable conversion usage = %d, want 1", got)
	}
}

func TestCollectionLimitChargesLiveWrapperLogicalEntries(t *testing.T) {
	t.Run("SleepUtils array wrapper", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 1)
		collection := newPortableJavaCollection("ArrayList", []Value{String("a"), String("b")})
		_, handled, err := portableSleepUtils(ObjectInvocation{
			Runtime: runtimeInstance,
			Op:      ObjectInvoke,
			Message: "getArrayWrapper",
			Arguments: []Argument{
				{Value: ObjectValue(collection)},
			},
		})
		if !handled {
			t.Fatal("SleepUtils.getArrayWrapper was not handled")
		}
		assertCollectionLimitError(t, err, 1)
	})

	t.Run("MapWrapper keys", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 1)
		mapping := newPortableJavaMapFromEntries("HashMap", []portableJavaMapSnapshotEntry{
			{key: "a", keyValue: String("a"), value: Int(1)},
			{key: "b", keyValue: String("b"), value: Int(2)},
		})
		wrapped := newMapWrapperHash(mapping)
		_, err := builtinKeys(context.Background(), Invocation{
			Runtime: runtimeInstance,
			Name:    "keys",
			Arguments: []Argument{
				{Value: HashValue(wrapped)},
			},
		})
		assertCollectionLimitError(t, err, 1)
	})

	t.Run("MapWrapper values snapshot", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 1)
		mapping := newPortableJavaMapFromEntries("HashMap", []portableJavaMapSnapshotEntry{
			{key: "a", keyValue: String("a"), value: Int(1)},
			{key: "b", keyValue: String("b"), value: Int(2)},
		})
		wrapped := newMapWrapperHash(mapping)
		_, err := builtinValues(context.Background(), Invocation{
			Runtime: runtimeInstance,
			Name:    "values",
			Arguments: []Argument{
				{Value: HashValue(wrapped)},
			},
		})
		assertCollectionLimitError(t, err, 1)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 0 {
			t.Fatalf("failed wrapper snapshot usage = %d, want 0", got)
		}
	})

	t.Run("MapWrapper foreach snapshot", func(t *testing.T) {
		mapping := newPortableJavaMapFromEntries("HashMap", []portableJavaMapSnapshotEntry{
			{key: "a", keyValue: String("a"), value: Int(1)},
			{key: "b", keyValue: String("b"), value: Int(2)},
		})
		wrapped := newMapWrapperHash(mapping)
		runtimeInstance := newCollectionLimitedRuntime(t, 1, WithInitialGlobals(map[string]Value{
			"%wrapped": HashValue(wrapped),
		}))
		_, err := runtimeInstance.Eval(context.Background(), "wrapper-foreach-limit.sl", `
foreach $key => $value (%wrapped) { }
`)
		assertCollectionLimitError(t, err, 1)
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 0 {
			t.Fatalf("failed wrapper foreach usage = %d, want 0", got)
		}
	})
}

func TestCollectionLimitChargesLiveWrapperIndexedCacheGrowth(t *testing.T) {
	t.Run("CollectionWrapper contextual access", func(t *testing.T) {
		account := newRuntimeResourceAccount(Limits{MaxCollectionEntriesPerRuntime: 1})
		collection := newPortableJavaCollection("ArrayList", []Value{String("a")})
		wrapped, err := newAccountedCollectionWrapperArray(account, collection)
		if err != nil {
			t.Fatalf("newAccountedCollectionWrapperArray: %v", err)
		}
		runtimeInstance, err := New(
			withRuntimeResourceAccount(account),
			WithInitialGlobals(map[string]Value{"@wrapped": ArrayValue(wrapped)}),
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

		invokePortableCollectionForTest(t, collection, "add", String("b"))
		_, err = runtimeInstance.Eval(context.Background(), "wrapper-index-limit.sl", `return @wrapped[0];`)
		assertCollectionLimitError(t, err, 1)
		backend := wrapped.backend.(*collectionWrapperArrayBackend)
		backend.indexedMu.Lock()
		indexedSet := backend.indexedSet
		backend.indexedMu.Unlock()
		if indexedSet {
			t.Fatal("failed indexed-cache admission published a snapshot")
		}
		if got := account.used(resourceCollectionEntries); got != 1 {
			t.Fatalf("failed indexed-cache usage = %d, want initial credit 1", got)
		}

		invokePortableCollectionForTest(t, collection, "remove", Int(1))
		value, err := runtimeInstance.Eval(context.Background(), "wrapper-index-retry.sl", `return @wrapped[0];`)
		if err != nil {
			t.Fatalf("retry after source shrink: %v", err)
		}
		if value.String() != "a" {
			t.Fatalf("retry value = %s, want a", value.Describe())
		}
	})

	t.Run("MapWrapper keys contextual access", func(t *testing.T) {
		account := newRuntimeResourceAccount(Limits{MaxCollectionEntriesPerRuntime: 1})
		mapping := newPortableJavaMapFromEntries("HashMap", []portableJavaMapSnapshotEntry{
			{key: "a", keyValue: String("a"), value: Int(1)},
		})
		wrapped := newAccountedMapWrapperHash(account, mapping)
		runtimeInstance, err := New(
			withRuntimeResourceAccount(account),
			WithInitialGlobals(map[string]Value{"%wrapped": HashValue(wrapped)}),
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

		value, err := runtimeInstance.Eval(context.Background(), "map-wrapper-keys.sl", `
@saved = keys(%wrapped);
return size(@saved);
`)
		if err != nil || value.Int32() != 1 {
			t.Fatalf("initial keys = (%s, %v), want (1, nil)", value.Describe(), err)
		}
		invokePortableCollectionForTest(t, mapping, "put", String("b"), Int(2))
		_, err = runtimeInstance.Eval(context.Background(), "map-wrapper-key-index-limit.sl", `return @saved[0];`)
		assertCollectionLimitError(t, err, 1)
		if got := account.used(resourceCollectionEntries); got != 1 {
			t.Fatalf("failed key-cache usage = %d, want initial credit 1", got)
		}
		invokePortableCollectionForTest(t, mapping, "remove", String("b"))
		value, err = runtimeInstance.Eval(context.Background(), "map-wrapper-key-index-retry.sl", `return @saved[0];`)
		if err != nil {
			t.Fatalf("key retry after source shrink: %v", err)
		}
		if value.String() != "a" {
			t.Fatalf("key retry value = %s, want a", value.Describe())
		}
	})
}

type instrumentedReservedWrapperSource struct {
	mu       sync.RWMutex
	values   []Value
	revision uint64

	eventMu sync.Mutex
	events  []string
}

func newInstrumentedReservedWrapperSource(values ...Value) *instrumentedReservedWrapperSource {
	return &instrumentedReservedWrapperSource{values: append([]Value(nil), values...)}
}

func (source *instrumentedReservedWrapperSource) record(event string) {
	source.eventMu.Lock()
	source.events = append(source.events, event)
	source.eventMu.Unlock()
}

func (source *instrumentedReservedWrapperSource) recordedEvents() []string {
	source.eventMu.Lock()
	defer source.eventMu.Unlock()
	return append([]string(nil), source.events...)
}

func (source *instrumentedReservedWrapperSource) wrapperSize() int {
	source.mu.RLock()
	defer source.mu.RUnlock()
	return len(source.values)
}

func (source *instrumentedReservedWrapperSource) wrapperSnapshot() []Value {
	source.mu.RLock()
	values := append([]Value(nil), source.values...)
	source.record("unreserved materialize")
	source.mu.RUnlock()
	return values
}

func (source *instrumentedReservedWrapperSource) wrapperSnapshotReserved(reserve func(int) error) ([]Value, error) {
	source.mu.RLock()
	defer source.mu.RUnlock()
	source.record("reserve")
	if reserve != nil {
		if err := reserve(len(source.values)); err != nil {
			return nil, err
		}
	}
	values := make([]Value, len(source.values))
	copy(values, source.values)
	source.record("materialize")
	return values, nil
}

func (source *instrumentedReservedWrapperSource) wrapperRevision() uint64 {
	source.mu.RLock()
	defer source.mu.RUnlock()
	return source.revision
}

func (source *instrumentedReservedWrapperSource) wrapperIteratorNext(index int, expected uint64) (Value, bool, error) {
	source.mu.RLock()
	defer source.mu.RUnlock()
	if expected != source.revision {
		return Null(), false, ErrArrayChangedDuringIteration
	}
	if index < 0 || index >= len(source.values) {
		return Null(), false, nil
	}
	return source.values[index], true, nil
}

func (source *instrumentedReservedWrapperSource) append(value Value) {
	source.mu.Lock()
	source.values = append(source.values, value)
	source.revision++
	source.mu.Unlock()
}

func (source *instrumentedReservedWrapperSource) truncate(length int) {
	source.mu.Lock()
	clear(source.values[length:])
	source.values = source.values[:length]
	source.revision++
	source.mu.Unlock()
}

func TestCollectionWrapperIndexedCacheReservesBeforeMaterializing(t *testing.T) {
	account := newRuntimeResourceAccount(Limits{MaxCollectionEntriesPerRuntime: 1})
	source := newInstrumentedReservedWrapperSource(String("a"))
	collection := newPortableJavaCollection("InstrumentedView", nil)
	collection.wrapperSource = source
	if err := account.reserve(resourceCollectionEntries, 1); err != nil {
		t.Fatalf("initial wrapper credit: %v", err)
	}
	wrapper := newAdmittedCollectionWrapperArray(account, 1, collection)
	source.append(String("b"))

	cell, ok, err := wrapper.backend.cellContext(0)
	if cell != nil || ok {
		t.Fatalf("rejected indexed cell = (%v, %t), want (nil, false)", cell, ok)
	}
	assertCollectionLimitError(t, err, 1)
	if got, want := source.recordedEvents(), []string{"reserve"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("failed snapshot events = %q, want %q", got, want)
	}
	backend := wrapper.backend.(*collectionWrapperArrayBackend)
	backend.indexedMu.Lock()
	indexedSet := backend.indexedSet
	backend.indexedMu.Unlock()
	if indexedSet {
		t.Fatal("failed preallocation reservation published the indexed cache")
	}

	// A failed attempt remains retryable. Once the live source fits its original
	// credit, the next reservation callback precedes the sole materialization.
	source.truncate(1)
	cell, ok, err = wrapper.backend.cellContext(0)
	if err != nil || !ok || cell.Get().String() != "a" {
		t.Fatalf("retry indexed cell = (%v, %t, %v), want (a, true, nil)", cell, ok, err)
	}
	if got, want := source.recordedEvents(), []string{"reserve", "reserve", "materialize"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retry snapshot events = %q, want %q", got, want)
	}
}

func TestCollectionWrapperIndexedCacheSharedSourceManyWrappersRace(t *testing.T) {
	const (
		wrapperCount = 32
		readersPer   = 8
		snapshotLen  = 3
	)
	account := newRuntimeResourceAccount(Limits{
		MaxCollectionEntriesPerRuntime: wrapperCount * snapshotLen,
	})
	collection := newPortableJavaCollection("ArrayList", []Value{String("a")})
	wrappers := make([]*Array, wrapperCount)
	for index := range wrappers {
		wrapper, err := newAccountedCollectionWrapperArray(account, collection)
		if err != nil {
			t.Fatalf("wrapper %d: %v", index, err)
		}
		wrappers[index] = wrapper
	}
	invokePortableCollectionForTest(t, collection, "add", String("b"))
	invokePortableCollectionForTest(t, collection, "add", String("c"))

	start := make(chan struct{})
	errorsSeen := make(chan error, wrapperCount*readersPer)
	var readers sync.WaitGroup
	for _, wrapper := range wrappers {
		for reader := 0; reader < readersPer; reader++ {
			readers.Add(1)
			go func(wrapper *Array) {
				defer readers.Done()
				<-start
				value, ok, err := wrapper.backend.cellContext(2)
				if err != nil {
					errorsSeen <- err
					return
				}
				if !ok || value.Get().String() != "c" {
					errorsSeen <- fmt.Errorf("indexed value = (%v, %t), want (c, true)", value, ok)
				}
			}(wrapper)
		}
	}
	close(start)
	readers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Error(err)
	}
	if got, want := account.used(resourceCollectionEntries), uint64(wrapperCount*snapshotLen); got != want {
		t.Fatalf("shared-source indexed-cache usage = %d, want %d", got, want)
	}
}

func TestCollectionLimitChargesEveryNestedPortableJavaArrayLevel(t *testing.T) {
	runtimeInstance := newCollectionLimitedRuntime(t, 5)
	value, handled, err := portableJavaReflectArray(ObjectInvocation{
		Runtime: runtimeInstance,
		Op:      ObjectInvoke,
		Message: "newInstance",
		Arguments: []Argument{
			{Value: ObjectValue(classReference("int"))},
			{Value: ArrayValue(NewArray(Int(2), Int(2)))},
		},
	})
	if !handled || !value.IsNull() {
		t.Fatalf("Array.newInstance = (%s, %t), want null/handled", value.Describe(), handled)
	}
	assertCollectionLimitError(t, err, 5)
	// int[2][2] creates two two-entry inner arrays before the two-entry outer
	// array. The final atomic reservation fails without exceeding the limit.
	if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 4 {
		t.Fatalf("nested array reservations = %d, want 4", got)
	}

	exactRuntime := newCollectionLimitedRuntime(t, 6)
	value, handled, err = portableJavaReflectArray(ObjectInvocation{
		Runtime: exactRuntime,
		Op:      ObjectInvoke,
		Message: "newInstance",
		Arguments: []Argument{
			{Value: ObjectValue(classReference("int"))},
			{Value: ArrayValue(NewArray(Int(2), Int(2)))},
		},
	})
	if err != nil || !handled {
		t.Fatalf("Array.newInstance exact limit = (%s, %t, %v), want value/true/nil", value.Describe(), handled, err)
	}
	array, ok := value.Array()
	if !ok || array.Len() != 2 {
		t.Fatalf("Array.newInstance exact result = %s, want two-entry outer array", value.Describe())
	}
	if got := exactRuntime.resources.used(resourceCollectionEntries); got != 6 {
		t.Fatalf("successful nested array usage = %d, want 6", got)
	}
}

func TestCollectionLimitScriptLoaderBridgeInstallationIsAtomic(t *testing.T) {
	t.Run("installation", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 1)
		loader := &portableScriptLoader{runtime: runtimeInstance}
		table := newPortableJavaMap("Hashtable", nil)
		shared := portableSharedEnvironment(table)

		err := shared.installGlobalBridges(loader, runtimeInstance)
		assertCollectionLimitError(t, err, 1)
		table.mu.RLock()
		size := len(table.values)
		table.mu.RUnlock()
		if size != 0 {
			t.Fatalf("bridge table size after rejected installation = %d, want 0", size)
		}
		if got := runtimeInstance.resources.used(resourceCollectionEntries); got != 0 {
			t.Fatalf("failed bridge reservation retained %d entries, want 0", got)
		}
	})

	t.Run("registration propagation", func(t *testing.T) {
		runtimeInstance := newCollectionLimitedRuntime(t, 1)
		program, err := runtimeInstance.CompileString("collection-child.sl", `return 1;`)
		if err != nil {
			t.Fatalf("CompileString: %v", err)
		}
		loader := &portableScriptLoader{
			runtime:       runtimeInstance,
			loadedScripts: newPortableJavaCollection("LinkedList", nil),
			scriptsByKey:  newPortableJavaMap("HashMap", nil),
			instances:     make(map[*portableScriptInstance]struct{}),
		}
		value, err := loader.registerScriptAtRuntime(
			ObjectInvocation{Runtime: runtimeInstance}, "collection-child.sl", program, "", true, nil,
		)
		if !value.IsNull() {
			t.Fatalf("registerScript = %s, want null", value.Describe())
		}
		assertCollectionLimitError(t, err, 1)
		if got := len(loader.loadedScripts.snapshot()); got != 0 {
			t.Fatalf("loaded registry size = %d, want 0", got)
		}
		loader.scriptsByKey.mu.RLock()
		mapSize := len(loader.scriptsByKey.values)
		loader.scriptsByKey.mu.RUnlock()
		if mapSize != 0 {
			t.Fatalf("key registry size = %d, want 0", mapSize)
		}
	})
}

func TestCollectionLimitScriptFunctionPublicationChargesOnlyNewKeys(t *testing.T) {
	runtimeInstance := newCollectionLimitedRuntime(t, 1)
	table := newPortableJavaMap("Hashtable", nil)
	shared := portableSharedEnvironment(table)
	first := &portableSharedRuntimeCallable{name: "first"}

	if err := shared.publish(runtimeInstance, "first", first); err != nil {
		t.Fatalf("first publication: %v", err)
	}
	if err := shared.publish(runtimeInstance, "first", &portableSharedRuntimeCallable{name: "replacement"}); err != nil {
		t.Fatalf("replacement publication: %v", err)
	}
	assertCollectionLimitError(t, shared.publish(runtimeInstance, "second", &portableSharedRuntimeCallable{name: "second"}), 1)
	table.mu.RLock()
	_, firstPresent := table.values["&first"]
	_, secondPresent := table.values["&second"]
	table.mu.RUnlock()
	if !firstPresent || secondPresent {
		t.Fatalf("published keys = first:%t second:%t, want true/false", firstPresent, secondPresent)
	}
}

func TestCollectionLimitSharedAccountDoesNotOvershootAcrossConcurrentGrowth(t *testing.T) {
	const limit = 32
	runtimeInstance := newCollectionLimitedRuntime(t, limit)
	left := newPortableJavaCollection("ArrayList", nil)
	right := newPortableJavaCollection("LinkedList", nil)
	collections := []*portableJavaCollection{left, right}

	const workers = 128
	var successes atomic.Uint64
	unexpected := make(chan error, workers)
	var wait sync.WaitGroup
	for index := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			collection := collections[index%len(collections)]
			_, handled, err := collection.invoke(ObjectInvocation{
				Runtime: runtimeInstance,
				Op:      ObjectInvoke,
				Message: "add",
				Arguments: []Argument{
					{Value: Int(int32(index))},
				},
			})
			if !handled {
				unexpected <- errors.New("portable add was not handled")
				return
			}
			if err == nil {
				successes.Add(1)
				return
			}
			if !errors.Is(err, ErrResourceLimit) {
				unexpected <- err
			}
		}()
	}
	wait.Wait()
	close(unexpected)
	for err := range unexpected {
		t.Errorf("concurrent growth: %v", err)
	}
	if got := successes.Load(); got != limit {
		t.Fatalf("successful growth operations = %d, want %d", got, limit)
	}
	if got := len(left.snapshot()) + len(right.snapshot()); got != limit {
		t.Fatalf("combined collection size = %d, want %d", got, limit)
	}
	if got := runtimeInstance.resources.used(resourceCollectionEntries); got != limit {
		t.Fatalf("shared collection usage = %d, want %d", got, limit)
	}
}
