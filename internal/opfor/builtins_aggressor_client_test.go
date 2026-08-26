package opfor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type aggressorClientTestCallable func(context.Context, ...Value) (Value, error)

func (function aggressorClientTestCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return function(ctx, values...)
}

type queuedAggressorEventDispatcher struct {
	mu       sync.Mutex
	ctx      context.Context
	callback Callable
	calls    int
	err      error
}

func (dispatcher *queuedAggressorEventDispatcher) DispatchAggressorEvent(ctx context.Context, callback Callable) error {
	dispatcher.mu.Lock()
	dispatcher.ctx = ctx
	dispatcher.callback = callback
	dispatcher.calls++
	err := dispatcher.err
	dispatcher.mu.Unlock()
	return err
}

func (dispatcher *queuedAggressorEventDispatcher) snapshot() (context.Context, Callable, int) {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	return dispatcher.ctx, dispatcher.callback, dispatcher.calls
}

func TestGetAggressorClientTypeDefaultsToHeadlessAndRequiresZeroArguments(t *testing.T) {
	t.Parallel()

	runtimeInstance := &Runtime{}
	value, err := runtimeInstance.getAggressorClientType(context.Background(), Invocation{
		Name: "getAggressorClientType",
	})
	if err != nil {
		t.Fatalf("getAggressorClientType: %v", err)
	}
	if value.Kind() != KindString || value.String() != "headless" {
		t.Fatalf("getAggressorClientType = %s, want headless string", value.Describe())
	}

	value, err = runtimeInstance.getAggressorClientType(context.Background(), Invocation{
		Name:      "getAggressorClientType",
		Arguments: []Argument{{Value: String("extra")}},
	})
	if err == nil || !strings.Contains(err.Error(), "expected exactly 0 argument(s), received 1") {
		t.Fatalf("getAggressorClientType extra argument error = %v, want exact arity error", err)
	}
	if !value.IsNull() {
		t.Fatalf("getAggressorClientType arity result = %s, want $null", value.Describe())
	}
}

func TestDispatchEventSynchronousDefaultRunsOnceAndReturnsNull(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := loadAggressorClientTestOwner(t, runtimeInstance)

	var calls atomic.Int32
	callback := aggressorClientTestCallable(func(context.Context, ...Value) (Value, error) {
		calls.Add(1)
		return String("discarded"), nil
	})
	value, err := runtimeInstance.dispatchEvent(context.Background(), aggressorClientInvocation(
		runtimeInstance, owner, "dispatch_event", FunctionValue(callback),
	))
	if err != nil {
		t.Fatalf("dispatch_event: %v", err)
	}
	if !value.IsNull() {
		t.Fatalf("dispatch_event result = %s, want $null", value.Describe())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("callback calls = %d, want 1", got)
	}
}

func TestDispatchEventSynchronousDefaultPreservesEvaluatorContext(t *testing.T) {
	t.Parallel()

	t.Run("exit unwinds the active fiber", func(t *testing.T) {
		runtimeInstance, err := New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		program, err := CompileString("synchronous-dispatch-exit.cna", `
$after = 0;
dispatch_event({ exit(); });
$after = 1;
`)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		if got := script.Get("$after").Int32(); got != 0 {
			t.Fatalf("post-dispatch value = %d, want 0 after synchronous exit", got)
		}
	})

	t.Run("nested callbacks share the instruction meter", func(t *testing.T) {
		const instructionLimit = 100
		runtimeInstance, err := New(WithInstructionLimit(instructionLimit))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		program, err := CompileString("synchronous-dispatch-limit.cna", `
$calls = 0;
$callback = {
    $calls++;
    if ($calls < 512) { dispatch_event($callback); }
};
dispatch_event($callback);
`)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err = runtimeInstance.Execute(ctx, program)
		if !errors.Is(err, ErrInstructionLimit) {
			t.Fatalf("nested synchronous dispatch error = %v, want ErrInstructionLimit", err)
		}
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Resource != "instruction" || limit.Limit != instructionLimit {
			t.Fatalf("nested synchronous dispatch LimitError = %+v", limit)
		}
	})
}

func TestDispatchEventImporterInlineDispatcherSharesInstructionMeter(t *testing.T) {
	t.Parallel()

	const instructionLimit = 100
	var dispatcherCalls atomic.Int32
	runtimeInstance, err := New(
		WithInstructionLimit(instructionLimit),
		WithAggressorEventDispatcher(AggressorEventDispatcherFunc(func(ctx context.Context, callback Callable) error {
			dispatcherCalls.Add(1)
			_, err := callback.Invoke(ctx)
			return err
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("importer-inline-dispatch-limit.cna", `
$calls = 0;
$callback = {
    $calls++;
    if ($calls < 512) { dispatch_event($callback); }
};
dispatch_event($callback);
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = runtimeInstance.Execute(ctx, program)
	if !errors.Is(err, ErrInstructionLimit) {
		t.Fatalf("nested importer-inline dispatch error = %v, want ErrInstructionLimit", err)
	}
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Resource != "instruction" || limit.Limit != instructionLimit {
		t.Fatalf("nested importer-inline dispatch LimitError = %+v", limit)
	}
	if calls := dispatcherCalls.Load(); calls < 2 {
		t.Fatalf("importer dispatcher calls = %d, want recursive inline dispatch", calls)
	}
}

func TestDispatchEventImporterConcurrentCallbackKeepsEntryMeterSnapshot(t *testing.T) {
	t.Parallel()

	const instructionLimit = 100
	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	dispatchReturned := make(chan struct{})
	callbackResult := make(chan error, 1)
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseCallback) })
	}
	defer release()

	runtimeInstance, err := New(
		WithInstructionLimit(instructionLimit),
		WithFunction("block_cross_return_callback", func(ctx context.Context, _ Invocation) (Value, error) {
			close(callbackStarted)
			select {
			case <-releaseCallback:
				return Null(), nil
			case <-ctx.Done():
				return Null(), ctx.Err()
			}
		}),
		WithFunction("mark_cross_return_dispatch_returned", func(context.Context, Invocation) (Value, error) {
			close(dispatchReturned)
			return Null(), nil
		}),
		WithAggressorEventDispatcher(AggressorEventDispatcherFunc(func(ctx context.Context, callback Callable) error {
			go func() {
				_, invokeErr := callback.Invoke(ctx)
				callbackResult <- invokeErr
			}()
			select {
			case <-callbackStarted:
				// Invoke has crossed the wrapper and captured its immutable
				// entry context before DispatchAggressorEvent returns.
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("importer-concurrent-dispatch-limit.cna", `
dispatch_event({
    block_cross_return_callback();
    $count = 0;
    while ($count < 512) { $count++; }
});
mark_cross_return_dispatch_returned();
`)
	if err != nil {
		t.Fatal(err)
	}

	type loadResult struct {
		script *Script
		err    error
	}
	loadDone := make(chan loadResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		script, loadErr := runtimeInstance.Load(ctx, program)
		loadDone <- loadResult{script: script, err: loadErr}
	}()

	select {
	case <-dispatchReturned:
		// The outer fiber can advance past dispatch_event while the admitted
		// callback remains blocked; importer dispatch is not made synchronous.
	case <-time.After(2 * time.Second):
		t.Fatal("outer fiber did not resume after concurrent dispatcher returned")
	}
	select {
	case invokeErr := <-callbackResult:
		t.Fatalf("callback returned before release with error %v", invokeErr)
	default:
	}
	var loaded *Script
	select {
	case result := <-loadDone:
		if result.err != nil {
			t.Fatalf("Load: %v", result.err)
		}
		if result.script == nil {
			t.Fatal("Load returned a nil script")
		}
		loaded = result.script
	case <-time.After(2 * time.Second):
		t.Fatal("Load did not return while the admitted callback remained blocked")
	}

	// Let the outer entry finish before the callback consumes the remainder of
	// their intentionally shared meter. Releasing earlier makes it inherently
	// nondeterministic which concurrent fiber observes the limit first; the
	// contract under test is that the callback retains that meter across the
	// dispatcher's return and the outer entry's completion.
	release()
	select {
	case invokeErr := <-callbackResult:
		if !errors.Is(invokeErr, ErrInstructionLimit) {
			t.Fatalf("cross-return callback error = %v, want ErrInstructionLimit", invokeErr)
		}
		var limit *LimitError
		if !errors.As(invokeErr, &limit) || limit.Resource != "instruction" || limit.Limit != instructionLimit {
			t.Fatalf("cross-return callback LimitError = %+v", limit)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cross-return callback did not finish after release")
	}
	if err := loaded.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
}

func TestDispatchEventPropagatesSynchronousCallbackAndDispatcherErrors(t *testing.T) {
	t.Parallel()

	t.Run("callback", func(t *testing.T) {
		wantErr := errors.New("callback failed")
		runtimeInstance, err := New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		owner := loadAggressorClientTestOwner(t, runtimeInstance)
		callback := aggressorClientTestCallable(func(context.Context, ...Value) (Value, error) {
			return Null(), wantErr
		})

		value, err := runtimeInstance.dispatchEvent(context.Background(), aggressorClientInvocation(
			runtimeInstance, owner, "dispatch_event", FunctionValue(callback),
		))
		if !errors.Is(err, wantErr) {
			t.Fatalf("dispatch_event error = %v, want callback error", err)
		}
		if !value.IsNull() {
			t.Fatalf("dispatch_event error result = %s, want $null", value.Describe())
		}
	})

	t.Run("dispatcher", func(t *testing.T) {
		wantErr := errors.New("dispatcher failed")
		dispatcher := &queuedAggressorEventDispatcher{err: wantErr}
		runtimeInstance, err := New(WithAggressorEventDispatcher(dispatcher))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		owner := loadAggressorClientTestOwner(t, runtimeInstance)
		callback := aggressorClientTestCallable(func(context.Context, ...Value) (Value, error) {
			t.Fatal("queued callback ran synchronously")
			return Null(), nil
		})

		value, err := runtimeInstance.dispatchEvent(context.Background(), aggressorClientInvocation(
			runtimeInstance, owner, "dispatch_event", FunctionValue(callback),
		))
		if !errors.Is(err, wantErr) {
			t.Fatalf("dispatch_event error = %v, want dispatcher error", err)
		}
		if !value.IsNull() {
			t.Fatalf("dispatch_event error result = %s, want $null", value.Describe())
		}
	})
}

func TestDispatchEventImporterErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			calls := 0
			runtimeInstance, err := New(WithAggressorEventDispatcher(AggressorEventDispatcherFunc(func(context.Context, Callable) error {
				calls++
				return boundaryErr
			})))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			_, err = runtimeInstance.Eval(context.Background(), "dispatcher-boundary-error.cna", `dispatch_event({ return 1; });`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("script error = %v, want authoritative %v", err, boundaryErr)
			}
			if calls != 1 {
				t.Fatalf("dispatcher calls = %d, want one", calls)
			}
		})
	}
}

func TestDispatchEventStockCallbackErrorsRemainNative(t *testing.T) {
	callback := aggressorClientTestCallable(func(context.Context, ...Value) (Value, error) {
		return String("discarded callback partial result"), ErrUnsafeArrayView
	})
	runtimeInstance, err := New(WithInitialGlobals(map[string]Value{"unsafe_callback": FunctionValue(callback)}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Eval(context.Background(), "dispatcher-native-error.cna", `return dispatch_event($unsafe_callback);`)
	if err != nil || !result.IsNull() {
		t.Fatalf("stock callback native translation = (%s, %v), want null/nil", result.Describe(), err)
	}
}

type aggressorClientContextKey struct{}

func TestDispatchEventQueuedCallbackOutlivesNativeReturnAndPreservesCallerContext(t *testing.T) {
	t.Parallel()

	dispatcher := &queuedAggressorEventDispatcher{}
	runtimeInstance, err := New(WithAggressorEventDispatcher(dispatcher))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	// Keep this test independent of the core-function wiring performed by the
	// runtime function table: the behavior under test is dispatchEvent itself.
	if err := runtimeInstance.RegisterFunction("dispatch_event", runtimeInstance.dispatchEvent); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("queued-dispatch-event.cna", `
$calls = 0;
$result = dispatch_event({ $calls++; return $calls; });
`)
	if err != nil {
		t.Fatal(err)
	}
	importerBase, cancelImporter := context.WithCancel(context.Background())
	importerContext := context.WithValue(importerBase, aggressorClientContextKey{}, "importer-value")
	script, err := runtimeInstance.Load(importerContext, program)
	if err != nil {
		cancelImporter()
		t.Fatal(err)
	}
	if !script.Get("$result").IsNull() {
		cancelImporter()
		t.Fatalf("dispatch_event script result = %s, want $null", script.Get("$result").Describe())
	}

	queuedContext, callback, dispatchCalls := dispatcher.snapshot()
	if dispatchCalls != 1 || callback == nil || queuedContext == nil {
		cancelImporter()
		t.Fatalf("queued dispatch = (calls=%d, context=%v, callback=%v), want one complete dispatch",
			dispatchCalls, queuedContext != nil, callback != nil)
	}
	if err := queuedContext.Err(); err != nil {
		cancelImporter()
		t.Fatalf("queued context after native return = %v, want live context", err)
	}
	if got := queuedContext.Value(aggressorClientContextKey{}); got != "importer-value" {
		cancelImporter()
		t.Fatalf("queued context value = %#v, want importer-value", got)
	}
	if currentFiber(queuedContext) != nil || currentBindingInvocation(queuedContext) != nil ||
		queuedContext.Value(executionMeterKey{}) != nil ||
		queuedContext.Value(includeChainContextKey{}) != nil ||
		queuedContext.Value(scriptExecutionContextKey{}) != nil ||
		queuedContext.Value(runtimeExecutionContextKey{}) != nil ||
		queuedContext.Value(scriptUnloadContextKey{}) != nil ||
		queuedContext.Value(runtimeCloseContextKey{}) != nil {
		cancelImporter()
		t.Fatal("queued context retained OPFOR-private evaluator or lifecycle state")
	}
	value, err := callback.Invoke(queuedContext)
	if err != nil {
		cancelImporter()
		t.Fatalf("queued callback after native return: %v", err)
	}
	if value.Int32() != 1 || script.Get("$calls").Int32() != 1 {
		cancelImporter()
		t.Fatalf("queued callback = %s, calls = %s; want 1 and 1", value.Describe(), script.Get("$calls").Describe())
	}

	cancelImporter()
	select {
	case <-queuedContext.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("queued context did not observe importer cancellation")
	}
	if !errors.Is(queuedContext.Err(), context.Canceled) {
		t.Fatalf("queued context error = %v, want context.Canceled", queuedContext.Err())
	}
	if _, err := callback.Invoke(queuedContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued callback with canceled importer context error = %v, want context.Canceled", err)
	}
	if script.Get("$calls").Int32() != 1 {
		t.Fatalf("canceled queued callback ran: calls = %s, want 1", script.Get("$calls").Describe())
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if _, err := callback.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("queued callback after unload error = %v, want ErrScriptUnloaded", err)
	}
	if script.Get("$calls").Int32() != 1 {
		t.Fatalf("post-unload queued callback ran: calls = %s, want 1", script.Get("$calls").Describe())
	}
}

func TestDispatchEventQueuedCallbackIsFreshTopLevelExecution(t *testing.T) {
	t.Parallel()

	dispatcher := &queuedAggressorEventDispatcher{}
	runtimeInstance, err := New(WithAggressorEventDispatcher(dispatcher))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("queued-dispatch-exit.cna", `dispatch_event({ exit(); });`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	queuedContext, callback, calls := dispatcher.snapshot()
	if calls != 1 || queuedContext == nil || callback == nil {
		t.Fatalf("queued dispatch = context:%v callback:%v calls:%d", queuedContext != nil, callback != nil, calls)
	}
	if _, err := callback.Invoke(queuedContext); err != nil {
		t.Fatalf("queued top-level exit callback: %v", err)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchEventImporterQueuedCallbackGetsFreshInstructionMeter(t *testing.T) {
	t.Parallel()

	outerMeter := make(chan *executionMeter, 1)
	callbackMeter := make(chan *executionMeter, 1)
	dispatcher := &queuedAggressorEventDispatcher{}
	runtimeInstance, err := New(
		WithInstructionLimit(100),
		WithAggressorEventDispatcher(dispatcher),
		WithFunction("record_outer_dispatch_meter", func(ctx context.Context, _ Invocation) (Value, error) {
			meter, _ := ctx.Value(executionMeterKey{}).(*executionMeter)
			outerMeter <- meter
			return Null(), nil
		}),
		WithFunction("record_queued_dispatch_meter", func(ctx context.Context, _ Invocation) (Value, error) {
			meter, _ := ctx.Value(executionMeterKey{}).(*executionMeter)
			callbackMeter <- meter
			return Null(), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("queued-dispatch-fresh-meter.cna", `
record_outer_dispatch_meter();
dispatch_event({ record_queued_dispatch_meter(); });
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	queuedContext, callback, calls := dispatcher.snapshot()
	if calls != 1 || queuedContext == nil || callback == nil {
		t.Fatalf("queued dispatch = context:%v callback:%v calls:%d", queuedContext != nil, callback != nil, calls)
	}
	if _, err := callback.Invoke(queuedContext); err != nil {
		t.Fatalf("queued callback: %v", err)
	}
	var origin *executionMeter
	select {
	case origin = <-outerMeter:
	default:
		t.Fatal("outer execution did not record an instruction meter")
	}
	var queued *executionMeter
	select {
	case queued = <-callbackMeter:
	default:
		t.Fatal("queued callback did not record an instruction meter")
	}
	if origin == nil || queued == nil {
		t.Fatalf("instruction meters = origin:%v queued:%v, want both non-nil", origin != nil, queued != nil)
	}
	if queued == origin {
		t.Fatal("queued callback reused the originating instruction meter")
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchEventRejectsTypedNilCallableWithoutPanic(t *testing.T) {
	t.Parallel()

	var callback *typedNilCallable
	runtimeInstance, err := New(WithFunction("typed_nil_callback", func(context.Context, Invocation) (Value, error) {
		return FunctionValue(callback), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("typed-nil-dispatch.cna", `dispatch_event(typed_nil_callback());`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); !errors.Is(err, ErrInvalidCallable) {
		t.Fatalf("typed-nil dispatch error = %v, want ErrInvalidCallable", err)
	}
}

func TestDispatchEventRejectsInvalidArityCallableAndMissingRuntimeState(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := loadAggressorClientTestOwner(t, runtimeInstance)

	for _, test := range []struct {
		name   string
		values []Value
		want   string
	}{
		{name: "missing", want: "expected exactly 1 argument(s), received 0"},
		{name: "extra", values: []Value{Null(), Null()}, want: "expected exactly 1 argument(s), received 2"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			value, err := runtimeInstance.dispatchEvent(context.Background(), aggressorClientInvocation(
				runtimeInstance, owner, "dispatch_event", test.values...,
			))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("dispatch_event error = %v, want %q", err, test.want)
			}
			if !value.IsNull() {
				t.Fatalf("dispatch_event arity result = %s, want $null", value.Describe())
			}
		})
	}

	value, err := runtimeInstance.dispatchEvent(context.Background(), aggressorClientInvocation(
		runtimeInstance, owner, "dispatch_event", String("not callable"),
	))
	if !errors.Is(err, ErrInvalidCallable) {
		t.Fatalf("dispatch_event non-callable error = %v, want ErrInvalidCallable", err)
	}
	if !value.IsNull() {
		t.Fatalf("dispatch_event non-callable result = %s, want $null", value.Describe())
	}

	callback := FunctionValue(aggressorClientTestCallable(func(context.Context, ...Value) (Value, error) {
		return Null(), nil
	}))
	value, err = runtimeInstance.dispatchEvent(context.Background(), Invocation{
		Name: "dispatch_event", Script: owner.ID(), Arguments: []Argument{{Value: callback}},
	})
	if err == nil || !strings.Contains(err.Error(), "invocation has no originating runtime") {
		t.Fatalf("dispatch_event missing invocation runtime error = %v, want safe runtime error", err)
	}
	if !value.IsNull() {
		t.Fatalf("dispatch_event missing runtime result = %s, want $null", value.Describe())
	}

	var nilRuntime *Runtime
	value, err = nilRuntime.dispatchEvent(context.Background(), Invocation{
		Name: "dispatch_event", Arguments: []Argument{{Value: callback}},
	})
	if err == nil || !strings.Contains(err.Error(), "runtime is nil") {
		t.Fatalf("dispatch_event nil receiver error = %v, want safe runtime error", err)
	}
	if !value.IsNull() {
		t.Fatalf("dispatch_event nil receiver result = %s, want $null", value.Describe())
	}

	runtimeInstance.eventDispatcher = nil
	value, err = runtimeInstance.dispatchEvent(context.Background(), aggressorClientInvocation(
		runtimeInstance, owner, "dispatch_event", callback,
	))
	if err == nil || !strings.Contains(err.Error(), "Aggressor event dispatcher is nil") {
		t.Fatalf("dispatch_event nil dispatcher error = %v, want safe dispatcher error", err)
	}
	if !value.IsNull() {
		t.Fatalf("dispatch_event nil dispatcher result = %s, want $null", value.Describe())
	}
}

func loadAggressorClientTestOwner(t *testing.T, runtimeInstance *Runtime) *Script {
	t.Helper()
	program, err := CompileString(t.Name()+".cna", `return $null;`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	return script
}

func aggressorClientInvocation(runtimeInstance *Runtime, script *Script, name string, values ...Value) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{
		Runtime:   runtimeInstance,
		Script:    script.ID(),
		Name:      name,
		Arguments: arguments,
	}
}
