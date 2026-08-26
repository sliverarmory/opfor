package opfor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCallableFuncAdapter(t *testing.T) {
	t.Parallel()

	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "present")
	array := NewArray(String("before"))
	callable := CallableFunc(func(received context.Context, values ...Value) (Value, error) {
		if received.Value(contextKey{}) != "present" {
			return Null(), errors.New("context value was not preserved")
		}
		if len(values) != 1 {
			return Null(), errors.New("arguments were not preserved")
		}
		receivedArray, ok := values[0].Array()
		if !ok || receivedArray != array {
			return Null(), errors.New("compound value identity was not preserved")
		}
		receivedArray.Append(String("after"))
		return Int(42), nil
	})

	value := FunctionValue(callable)
	wrapped, ok := value.Function()
	if !ok {
		t.Fatal("FunctionValue(CallableFunc) did not contain a callable")
	}
	result, err := wrapped.Invoke(ctx, ArrayValue(array))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := result.Int32(); got != 42 {
		t.Fatalf("Invoke result = %d, want 42", got)
	}
	if got := array.Len(); got != 2 {
		t.Fatalf("shared array length = %d, want 2", got)
	}
}

func TestCallableFuncPreservesCancellationAndErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wantErr := errors.New("callable failure")
	callable := CallableFunc(func(received context.Context, _ ...Value) (Value, error) {
		if !errors.Is(received.Err(), context.Canceled) {
			return Null(), errors.New("cancellation was not preserved")
		}
		return String("partial"), wantErr
	})
	result, err := callable.Invoke(ctx)
	if result.String() != "partial" || !errors.Is(err, wantErr) {
		t.Fatalf("Invoke = (%s, %v), want partial/%v", result.Describe(), err, wantErr)
	}

	if result, err := CallableFunc(nil).Invoke(context.Background()); !result.IsNull() || !errors.Is(err, ErrInvalidCallable) {
		t.Fatalf("nil Invoke = (%s, %v), want $null/ErrInvalidCallable", result.Describe(), err)
	}
	if value := FunctionValue(CallableFunc(nil)); !value.IsNull() {
		t.Fatalf("FunctionValue(nil CallableFunc) = %s, want $null", value.Describe())
	}
}

func TestCallableFuncSupportsConcurrentInvocation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	callable := CallableFunc(func(context.Context, ...Value) (Value, error) {
		return Long(calls.Add(1)), nil
	})

	const goroutines = 32
	errorsByCall := make(chan error, goroutines)
	var workers sync.WaitGroup
	workers.Add(goroutines)
	for range goroutines {
		go func() {
			defer workers.Done()
			if _, err := callable.Invoke(context.Background()); err != nil {
				errorsByCall <- err
			}
		}()
	}
	workers.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		t.Errorf("Invoke: %v", err)
	}
	if got := calls.Load(); got != goroutines {
		t.Fatalf("calls = %d, want %d", got, goroutines)
	}
}
