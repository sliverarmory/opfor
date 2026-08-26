package opfor

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

const executionMutationTestTimeout = 2 * time.Second

func TestEvaluatorMutationDoesNotCommitAfterUnloadCancellation(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		rhs     Value
		prepare func(*testing.T, *Script) (*Cell, func(*testing.T))
	}{
		{
			name:    "simple scalar assignment",
			source:  `$target = 10; sub mutate { $target = rhs(); }`,
			rhs:     Int(30),
			prepare: scalarMutationTarget("$target", 10),
		},
		{
			name:    "compound scalar assignment",
			source:  `$target = 10; sub mutate { $target += rhs(); }`,
			rhs:     Int(7),
			prepare: scalarMutationTarget("$target", 10),
		},
		{
			name:    "postfix mutation",
			source:  `$target = 10; sub mutate { rhs(); $target++; }`,
			rhs:     Int(7),
			prepare: scalarMutationTarget("$target", 10),
		},
		{
			name:   "tuple assignment",
			source: `$target = 10; $other = 20; sub mutate { ($target, $other) = rhs(); }`,
			rhs:    ArrayValue(NewArray(Int(30), Int(40))),
			prepare: func(t *testing.T, script *Script) (*Cell, func(*testing.T)) {
				t.Helper()
				return script.globals.resolve("$target"), func(t *testing.T) {
					t.Helper()
					if got := script.Get("$target").Int32(); got != 10 {
						t.Fatalf("$target = %d, want pre-cancellation value 10", got)
					}
					if got := script.Get("$other").Int32(); got != 20 {
						t.Fatalf("$other = %d, want pre-cancellation value 20", got)
					}
				}
			},
		},
		{
			name:   "array element assignment",
			source: `@items = @(10); sub mutate { @items[0] = rhs(); }`,
			rhs:    Int(30),
			prepare: func(t *testing.T, script *Script) (*Cell, func(*testing.T)) {
				t.Helper()
				array, ok := script.Get("@items").Array()
				if !ok {
					t.Fatal("@items is not an array")
				}
				cell, ok := array.Cell(0)
				if !ok {
					t.Fatal("@items[0] is missing")
				}
				return cell, func(t *testing.T) {
					t.Helper()
					value, exists := array.Get(0)
					if !exists || value.Int32() != 10 {
						t.Fatalf("@items[0] = (%s, %v), want 10", value.Describe(), exists)
					}
				}
			},
		},
		{
			name:   "hash element assignment",
			source: `%items = %("key" => 10); sub mutate { %items["key"] = rhs(); }`,
			rhs:    Int(30),
			prepare: func(t *testing.T, script *Script) (*Cell, func(*testing.T)) {
				t.Helper()
				hash, ok := script.Get("%items").Hash()
				if !ok {
					t.Fatal("%items is not a hash")
				}
				cell, ok := hash.Cell("key")
				if !ok {
					t.Fatal(`%items["key"] is missing`)
				}
				return cell, func(t *testing.T) {
					t.Helper()
					value, exists := hash.Get("key")
					if !exists || value.Int32() != 10 {
						t.Fatalf(`%%items["key"] = (%s, %v), want 10`, value.Describe(), exists)
					}
				}
			},
		},
		{
			name:    "assignment while binding",
			source:  `$target = 10; sub mutate { while $target (rhs()) { } }`,
			rhs:     Int(30),
			prepare: scalarMutationTarget("$target", 10),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entered := make(chan struct{})
			releaseRHS := make(chan struct{})
			runtimeInstance, err := New(
				WithStdout(io.Discard),
				WithStderr(io.Discard),
				WithFunction("rhs", func(ctx context.Context, _ Invocation) (Value, error) {
					close(entered)
					select {
					case <-releaseRHS:
						return test.rhs, nil
					case <-ctx.Done():
						return Null(), ctx.Err()
					}
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			program, err := CompileString("canceled-mutation-"+test.name+".sl", test.source)
			if err != nil {
				t.Fatalf("CompileString: %v", err)
			}
			script, err := runtimeInstance.Load(context.Background(), program)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			target, assertUnchanged := test.prepare(t, script)
			target.mu.Lock()
			locked := true
			defer func() {
				if locked {
					target.mu.Unlock()
				}
			}()

			callDone := make(chan error, 1)
			go func() {
				_, callErr := script.Call(context.Background(), "mutate")
				callDone <- callErr
			}()
			awaitExecutionMutationSignal(t, entered, "rhs entry")
			close(releaseRHS)

			unloadDone := make(chan error, 1)
			go func() { unloadDone <- script.Unload(context.Background()) }()
			awaitExecutionMutationSignal(t, script.executionCtx.Done(), "script cancellation")
			target.mu.Unlock()
			locked = false

			if err := awaitExecutionMutationError(t, callDone, "mutating Script.Call"); !errors.Is(err, context.Canceled) && !errors.Is(err, ErrScriptUnloaded) {
				t.Fatalf("Script.Call error = %v, want cancellation", err)
			}
			if err := awaitExecutionMutationError(t, unloadDone, "script unload"); err != nil {
				t.Fatalf("Unload: %v", err)
			}
			assertUnchanged(t)
		})
	}
}

func TestEvaluatorMutationDoesNotCommitAfterCallerCancellation(t *testing.T) {
	entered := make(chan struct{})
	releaseRHS := make(chan struct{})
	runtimeInstance, err := New(
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithFunction("rhs", func(ctx context.Context, _ Invocation) (Value, error) {
			close(entered)
			select {
			case <-releaseRHS:
				return Int(30), nil
			case <-ctx.Done():
				return Null(), ctx.Err()
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("caller-canceled-mutation.sl", `$target = 10; sub mutate { $target = rhs(); }`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	target := script.globals.resolve("$target")
	target.mu.Lock()
	locked := true
	defer func() {
		if locked {
			target.mu.Unlock()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	callDone := make(chan error, 1)
	go func() {
		_, callErr := script.Call(ctx, "mutate")
		callDone <- callErr
	}()
	awaitExecutionMutationSignal(t, entered, "rhs entry")
	close(releaseRHS)
	cancel()
	target.mu.Unlock()
	locked = false
	if err := awaitExecutionMutationError(t, callDone, "caller-canceled Script.Call"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Script.Call error = %v, want context.Canceled", err)
	}
	if got := target.Get().Int32(); got != 10 {
		t.Fatalf("$target = %d, want pre-cancellation value 10", got)
	}
}

func scalarMutationTarget(name string, want int32) func(*testing.T, *Script) (*Cell, func(*testing.T)) {
	return func(t *testing.T, script *Script) (*Cell, func(*testing.T)) {
		t.Helper()
		return script.globals.resolve(name), func(t *testing.T) {
			t.Helper()
			if got := script.Get(name).Int32(); got != want {
				t.Fatalf("%s = %d, want pre-cancellation value %d", name, got, want)
			}
		}
	}
}

func awaitExecutionMutationSignal(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(executionMutationTestTimeout):
		t.Fatalf("timed out waiting for %s", operation)
	}
}

func awaitExecutionMutationError(t *testing.T, result <-chan error, operation string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(executionMutationTestTimeout):
		t.Fatalf("timed out waiting for %s", operation)
		return nil
	}
}
