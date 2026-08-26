package opfor_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestInvocationCallbackCanBeRetainedAfterHostReturns(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var retained opfor.Callable
	runtime, err := opfor.New(
		opfor.WithFunction("callback_probe", func(_ context.Context, invocation opfor.Invocation) (opfor.Value, error) {
			calls.Add(1)
			return invocation.Arg(0), nil
		}),
		opfor.WithHost(opfor.HostFunc(func(_ context.Context, invocation opfor.Invocation) (opfor.Value, error) {
			if invocation.Runtime == nil {
				return opfor.Null(), errors.New("host invocation omitted its runtime")
			}
			var callbackErr error
			retained, callbackErr = invocation.Callback(0)
			return opfor.Null(), callbackErr
		})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := opfor.CompileString("retained-callback.cna", `
retain_callback({ return callback_probe($1); });
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	if retained == nil {
		t.Fatal("host did not retain the callback")
	}

	value, err := retained.Invoke(context.Background(), opfor.String("after-host-return"))
	if err != nil {
		t.Fatalf("retained Invoke: %v", err)
	}
	if got := value.String(); got != "after-host-return" {
		t.Fatalf("retained callback result = %q, want %q", got, "after-host-return")
	}

	const workers = 32
	type outcome struct {
		want  string
		value opfor.Value
		err   error
	}
	outcomes := make(chan outcome, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		want := fmt.Sprintf("worker-%d", worker)
		group.Add(1)
		go func() {
			defer group.Done()
			result, invokeErr := retained.Invoke(context.Background(), opfor.String(want))
			outcomes <- outcome{want: want, value: result, err: invokeErr}
		}()
	}
	group.Wait()
	close(outcomes)
	for result := range outcomes {
		if result.err != nil {
			t.Errorf("concurrent Invoke(%q): %v", result.want, result.err)
			continue
		}
		if got := result.value.String(); got != result.want {
			t.Errorf("concurrent Invoke(%q) = %q", result.want, got)
		}
	}
	if got, want := calls.Load(), int32(workers+1); got != want {
		t.Fatalf("callback probe calls = %d, want %d", got, want)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := retained.Invoke(canceled, opfor.String("must-not-run")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled callback error = %v, want context.Canceled", err)
	}
	if got, want := calls.Load(), int32(workers+1); got != want {
		t.Fatalf("canceled callback ran: calls = %d, want %d", got, want)
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if _, err := retained.Invoke(context.Background(), opfor.String("must-not-run")); !errors.Is(err, opfor.ErrScriptUnloaded) {
		t.Fatalf("post-unload callback error = %v, want ErrScriptUnloaded", err)
	}
	if got, want := calls.Load(), int32(workers+1); got != want {
		t.Fatalf("post-unload callback ran: calls = %d, want %d", got, want)
	}
}

func TestInvocationCallbackRejectsRuntimeClose(t *testing.T) {
	t.Parallel()

	var retained opfor.Callable
	runtime, err := opfor.New(opfor.WithHost(opfor.HostFunc(func(_ context.Context, invocation opfor.Invocation) (opfor.Value, error) {
		var callbackErr error
		retained, callbackErr = invocation.Callback(0)
		return opfor.Null(), callbackErr
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := opfor.CompileString("closed-callback.cna", `retain_callback({ return $1; });`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	if _, err := runtime.Load(context.Background(), program); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := retained.Invoke(context.Background(), opfor.String("must-not-run")); !errors.Is(err, opfor.ErrScriptUnloaded) {
		t.Fatalf("post-close callback error = %v, want ErrScriptUnloaded", err)
	}
}

func TestInvocationCallbackCancellationPreservesSuspendedContinuation(t *testing.T) {
	t.Parallel()

	var retained opfor.Callable
	runtime, err := opfor.New(opfor.WithHost(opfor.HostFunc(func(_ context.Context, invocation opfor.Invocation) (opfor.Value, error) {
		var callbackErr error
		retained, callbackErr = invocation.Callback(0)
		return opfor.Null(), callbackErr
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := opfor.CompileString("callback-continuation.cna", `
retain_callback({
    yield "paused";
    return "resumed:" . $1;
});
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	if first, err := retained.Invoke(context.Background(), opfor.String("first")); err != nil || first.String() != "paused" {
		t.Fatalf("first callback invocation = %s, %v; want paused", first.Describe(), err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := retained.Invoke(canceled, opfor.String("canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled callback error = %v, want context.Canceled", err)
	}
	resumed, err := retained.Invoke(context.Background(), opfor.String("later"))
	if err != nil {
		t.Fatalf("resumed callback: %v", err)
	}
	if got := resumed.String(); got != "resumed:later" {
		t.Fatalf("resumed callback = %q, want %q", got, "resumed:later")
	}
}

func TestInvocationCallbackGuardsRetainedRuntimeFunctionReference(t *testing.T) {
	t.Parallel()

	var deferredCalls atomic.Int32
	var retained opfor.Callable
	runtime, err := opfor.New(
		opfor.WithFunction("deferred_host", func(_ context.Context, invocation opfor.Invocation) (opfor.Value, error) {
			deferredCalls.Add(1)
			return invocation.Arg(0), nil
		}),
		opfor.WithHost(opfor.HostFunc(func(_ context.Context, invocation opfor.Invocation) (opfor.Value, error) {
			if invocation.Name == "retain_callback" {
				var callbackErr error
				retained, callbackErr = invocation.Callback(0)
				return opfor.Null(), callbackErr
			}
			return opfor.Null(), &opfor.UnsupportedError{Operation: "host function", Name: invocation.Name}
		})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := opfor.CompileString("runtime-function-callback.cna", `retain_callback(&deferred_host);`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	value, err := retained.Invoke(context.Background(), opfor.String("before-unload"))
	if err != nil {
		t.Fatalf("retained runtime function: %v", err)
	}
	if got := value.String(); got != "before-unload" {
		t.Fatalf("retained runtime function = %q, want %q", got, "before-unload")
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if _, err := retained.Invoke(context.Background(), opfor.String("after-unload")); !errors.Is(err, opfor.ErrScriptUnloaded) {
		t.Fatalf("post-unload runtime function error = %v, want ErrScriptUnloaded", err)
	}
	if got := deferredCalls.Load(); got != 1 {
		t.Fatalf("deferred host calls = %d, want 1", got)
	}
}

func TestObjectInvocationCallbackCanBeRetainedAfterObjectCall(t *testing.T) {
	t.Parallel()

	var retained opfor.Callable
	runtime, err := opfor.New(
		opfor.WithFunction("listener_target", func(context.Context, opfor.Invocation) (opfor.Value, error) {
			return opfor.ObjectValue(&callbackListenerTarget{}), nil
		}),
		opfor.WithObjectHost(opfor.ObjectHostFunc(func(_ context.Context, invocation opfor.ObjectInvocation) (opfor.Value, error) {
			if invocation.Message != "addListener" {
				return opfor.Null(), &opfor.UnsupportedError{Operation: "object method", Name: invocation.Message}
			}
			var callbackErr error
			retained, callbackErr = invocation.Callback(0)
			return opfor.Null(), callbackErr
		})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := opfor.CompileString("object-listener.cna", `
$target = listener_target();
[$target addListener: { return "listener:" . $1; }];
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	value, err := retained.Invoke(context.Background(), opfor.String("later"))
	if err != nil {
		t.Fatalf("listener callback: %v", err)
	}
	if got := value.String(); got != "listener:later" {
		t.Fatalf("listener callback = %q, want %q", got, "listener:later")
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if _, err := retained.Invoke(context.Background()); !errors.Is(err, opfor.ErrScriptUnloaded) {
		t.Fatalf("post-unload listener error = %v, want ErrScriptUnloaded", err)
	}
}

func TestInvocationCallbackValidatesFunctionArgument(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`retain_callback("not callable");`,
		`retain_callback();`,
	} {
		runtime, err := opfor.New(opfor.WithHost(opfor.HostFunc(func(_ context.Context, invocation opfor.Invocation) (opfor.Value, error) {
			_, callbackErr := invocation.Callback(0)
			return opfor.Null(), callbackErr
		})))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		program, err := opfor.CompileString("invalid-callback.cna", source)
		if err != nil {
			t.Fatalf("CompileString(%q): %v", source, err)
		}
		if _, err := runtime.Load(context.Background(), program); !errors.Is(err, opfor.ErrInvalidCallable) {
			t.Errorf("Load(%q) error = %v, want ErrInvalidCallable", source, err)
		}
	}
}

type callbackListenerTarget struct{}
