package opfor

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
)

func TestStructuralContainerMutationsDoNotCommitAfterUnloadCancellation(t *testing.T) {
	tests := []struct {
		name     string
		function string
		source   string
		native   NativeFunc
		target   func(*testing.T, *Script) (lock func(), unlock func(), assertUnchanged func())
	}{
		{
			name:     "push",
			function: "push",
			source:   `@target = @(1, 2); sub mutate { push(@target, 3); }`,
			native:   builtinPush,
			target:   unchangedArrayTarget("@target", 1, 2),
		},
		{
			name:     "array insertion",
			function: "add",
			source:   `@target = @(1, 2); sub mutate { add(@target, 3, 1); }`,
			native:   builtinAdd,
			target:   unchangedArrayTarget("@target", 1, 2),
		},
		{
			name:     "array removal",
			function: "removeAt",
			source:   `@target = @(1, 2); sub mutate { removeAt(@target, 0); }`,
			native:   builtinRemoveAt,
			target:   unchangedArrayTarget("@target", 1, 2),
		},
		{
			name:     "array clear",
			function: "clear",
			source:   `@target = @(1, 2); sub mutate { clear(@target); }`,
			native:   builtinClear,
			target:   unchangedArrayTarget("@target", 1, 2),
		},
		{
			name:     "hash insertion",
			function: "add",
			source:   `%target = %("old" => 1); sub mutate { add(%target, "new" => 2); }`,
			native:   builtinAdd,
			target:   unchangedHashTarget("%target", map[string]Value{"old": Int(1)}),
		},
		{
			name:     "hash removal",
			function: "remove",
			source:   `%target = %("old" => 1, "keep" => 2); sub mutate { remove(%target, 1); }`,
			native:   builtinRemove,
			target: unchangedHashTarget("%target", map[string]Value{
				"old": Int(1), "keep": Int(2),
			}),
		},
		{
			name:     "hash clear",
			function: "clear",
			source:   `%target = %("old" => 1); sub mutate { clear(%target); }`,
			native:   builtinClear,
			target:   unchangedHashTarget("%target", map[string]Value{"old": Int(1)}),
		},
		{
			name:     "hash null cleanup",
			function: "size",
			source:   `%target = %("old" => $null); sub mutate { size(%target); }`,
			native:   builtinSize,
			target:   unchangedHashTarget("%target", map[string]Value{"old": Null()}),
		},
		{
			name:     "hash autovivification",
			function: "autoviv_probe",
			source:   `%target = %("old" => 1); sub mutate { autoviv_probe(%target); }`,
			native: func(ctx context.Context, invocation Invocation) (Value, error) {
				hash, ok := invocation.Arg(0).Hash()
				if !ok {
					return Null(), errors.New("autoviv_probe: expected hash")
				}
				_, err := hash.EnsureValueContext(ctx, String("new"))
				return Null(), err
			},
			target: unchangedHashTarget("%target", map[string]Value{"old": Int(1)}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entered := make(chan struct{})
			releaseNative := make(chan struct{})
			var enteredOnce sync.Once
			runtimeInstance, err := New(
				WithStdout(io.Discard),
				WithStderr(io.Discard),
				WithFunction(test.function, func(ctx context.Context, invocation Invocation) (Value, error) {
					enteredOnce.Do(func() { close(entered) })
					<-releaseNative
					return test.native(ctx, invocation)
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			program, err := CompileString("blocked-structural-"+test.name+".cna", test.source)
			if err != nil {
				t.Fatalf("CompileString: %v", err)
			}
			script, err := runtimeInstance.Load(context.Background(), program)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			lock, unlock, assertUnchanged := test.target(t, script)
			locked := false
			defer func() {
				if locked {
					unlock()
				}
			}()

			callDone := make(chan error, 1)
			go func() {
				_, callErr := script.Call(context.Background(), "mutate")
				callDone <- callErr
			}()
			awaitExecutionMutationSignal(t, entered, "container mutator entry")
			lock()
			locked = true
			close(releaseNative)

			unloadDone := make(chan error, 1)
			go func() { unloadDone <- script.Unload(context.Background()) }()
			awaitExecutionMutationSignal(t, script.executionCtx.Done(), "script cancellation")
			unlock()
			locked = false

			if err := awaitExecutionMutationError(t, callDone, "mutating Script.Call"); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrScriptUnloaded) {
				t.Fatalf("Script.Call error = %v, want cancellation", err)
			}
			if err := awaitExecutionMutationError(t, unloadDone, "script unload"); err != nil {
				t.Fatalf("Unload: %v", err)
			}
			assertUnchanged()
		})
	}
}

func TestSortCommitDoesNotRunAfterUnloadCancellation(t *testing.T) {
	enteredCompare := make(chan struct{})
	releaseCompare := make(chan struct{})
	var enteredOnce sync.Once
	runtimeInstance, err := New(
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithFunction("sort_probe", func(ctx context.Context, invocation Invocation) (Value, error) {
			array, err := invocationArray(invocation, 0)
			if err != nil {
				return Null(), err
			}
			return sortSequenceArray(ctx, invocation.Name, array, func(left, right Value) (int, error) {
				enteredOnce.Do(func() { close(enteredCompare) })
				<-releaseCompare
				return int(sleepInt32(left) - sleepInt32(right)), nil
			})
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("blocked-sort-commit.cna", `
@target = @(2, 1);
sub mutate { sort_probe(@target); }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := script.Get("@target").Array()
	if !ok {
		t.Fatal("@target is not an array")
	}
	storage, _ := array.arrayStorage()

	callDone := make(chan error, 1)
	go func() {
		_, callErr := script.Call(context.Background(), "mutate")
		callDone <- callErr
	}()
	awaitExecutionMutationSignal(t, enteredCompare, "sort comparison")
	storage.mu.Lock()
	locked := true
	defer func() {
		if locked {
			storage.mu.Unlock()
		}
	}()
	close(releaseCompare)

	unloadDone := make(chan error, 1)
	go func() { unloadDone <- script.Unload(context.Background()) }()
	awaitExecutionMutationSignal(t, script.executionCtx.Done(), "script cancellation")
	storage.mu.Unlock()
	locked = false

	if err := awaitExecutionMutationError(t, callDone, "sorting Script.Call"); !errors.Is(err, context.Canceled) && !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("Script.Call error = %v, want cancellation", err)
	}
	if err := awaitExecutionMutationError(t, unloadDone, "script unload"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	assertArrayValues(t, array, 2, 1)
}

func unchangedArrayTarget(name string, values ...int32) func(*testing.T, *Script) (func(), func(), func()) {
	return func(t *testing.T, script *Script) (func(), func(), func()) {
		t.Helper()
		array, ok := script.Get(name).Array()
		if !ok {
			t.Fatalf("%s is not an array", name)
		}
		storage, _ := array.arrayStorage()
		return storage.mu.Lock, storage.mu.Unlock, func() { assertArrayValues(t, array, values...) }
	}
}

func assertArrayValues(t *testing.T, array *Array, want ...int32) {
	t.Helper()
	values := array.Values()
	if len(values) != len(want) {
		t.Fatalf("array length = %d, want %d; values = %v", len(values), len(want), values)
	}
	for index, expected := range want {
		if got := values[index].Int32(); got != expected {
			t.Fatalf("array[%d] = %d, want %d", index, got, expected)
		}
	}
}

func unchangedHashTarget(name string, want map[string]Value) func(*testing.T, *Script) (func(), func(), func()) {
	return func(t *testing.T, script *Script) (func(), func(), func()) {
		t.Helper()
		hash, ok := script.Get(name).Hash()
		if !ok {
			t.Fatalf("%s is not a hash", name)
		}
		return hash.mu.Lock, hash.mu.Unlock, func() {
			hash.mu.RLock()
			defer hash.mu.RUnlock()
			if len(hash.items) != len(want) {
				t.Fatalf("hash size = %d, want %d", len(hash.items), len(want))
			}
			for key, expected := range want {
				cell, exists := hash.items[sleepCanonicalString(String(key))]
				if !exists {
					t.Fatalf("hash key %q is missing", key)
				}
				if got := cell.Get(); !got.IdentityEqual(expected) {
					t.Fatalf("hash[%q] = %s, want %s", key, got.Describe(), expected.Describe())
				}
			}
		}
	}
}
