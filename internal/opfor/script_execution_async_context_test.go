package opfor

import (
	"context"
	"errors"
	goruntime "runtime"
	"sync"
	"testing"
	"time"
)

type asynchronousExecutionImporterContextKey struct{}

func TestDetachAsynchronousExecutionContextMasksPrivateState(t *testing.T) {
	deadlineContext, cancelDeadline := context.WithDeadline(
		context.Background(),
		time.Now().Add(time.Hour),
	)
	defer cancelDeadline()
	importerContext, cancelImporter := context.WithCancelCause(deadlineContext)
	importerValue := &struct{ name string }{name: "importer value"}
	meter := &executionMeter{limit: 73}

	ctx := context.WithValue(importerContext, asynchronousExecutionImporterContextKey{}, importerValue)
	ctx = context.WithValue(ctx, executionMeterKey{}, meter)
	privateValues := []struct {
		name  string
		key   any
		value any
	}{
		{name: "fiber", key: currentFiberContextKey{}, value: &fiber{}},
		{name: "include", key: includeChainContextKey{}, value: []includeChainEntry{{}}},
		{name: "binding", key: bindingInvocationContextKey{}, value: &BindingInvocation{}},
		{name: "loadable", key: loadableResolutionContextKey{}, value: &loadableResolutionToken{}},
		{name: "native", key: nativeDispatchStateContextKey{}, value: &nativeDispatchState{}},
		{name: "run", key: portableScriptInstanceRunContextKey{}, value: &portableScriptInstanceRunToken{}},
		{name: "script execution", key: scriptExecutionContextKey{}, value: &scriptExecutionToken{}},
		{name: "runtime execution", key: runtimeExecutionContextKey{}, value: &runtimeExecutionToken{}},
		{name: "script unload", key: scriptUnloadContextKey{}, value: &scriptUnloadToken{}},
		{name: "runtime close", key: runtimeCloseContextKey{}, value: &runtimeCloseToken{}},
		{name: "UI ancestry", key: aggressorUICallbackAncestryContextKey{}, value: &aggressorUICallbackAncestry{}},
		{name: "generation cleanup", key: scriptGenerationCleanupContextKey{}, value: &scriptGenerationCleanupToken{}},
	}
	for _, private := range privateValues {
		ctx = context.WithValue(ctx, private.key, private.value)
		if got := ctx.Value(private.key); got == nil {
			t.Fatalf("source context %s value is nil", private.name)
		}
	}

	detached, releaseDetached := detachAsynchronousExecutionContextLease(ctx)
	defer releaseDetached()
	if got := detached.Value(asynchronousExecutionImporterContextKey{}); got != importerValue {
		t.Fatalf("importer value = %#v, want exact retained value %#v", got, importerValue)
	}
	if got := detached.Value(executionMeterKey{}); got != meter {
		t.Fatalf("execution meter = %p, want exact retained meter %p", got, meter)
	}
	for _, private := range privateValues {
		if got := detached.Value(private.key); got != nil {
			t.Errorf("detached context retained private %s value %#v", private.name, got)
		}
	}
	wantDeadline, wantDeadlineOK := importerContext.Deadline()
	gotDeadline, gotDeadlineOK := detached.Deadline()
	if gotDeadlineOK != wantDeadlineOK || !gotDeadline.Equal(wantDeadline) {
		t.Fatalf("detached deadline = (%v, %v), want (%v, %v)", gotDeadline, gotDeadlineOK, wantDeadline, wantDeadlineOK)
	}
	if err := detached.Err(); err != nil {
		t.Fatalf("detached context before importer cancellation = %v", err)
	}

	importerCanceled := errors.New("importer canceled")
	cancelImporter(importerCanceled)
	select {
	case <-detached.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("detached context did not observe importer cancellation")
	}
	if !errors.Is(detached.Err(), context.Canceled) {
		t.Fatalf("detached error = %v, want context.Canceled", detached.Err())
	}
	if !errors.Is(context.Cause(detached), importerCanceled) {
		t.Fatalf("detached cancellation cause = %v, want %v", context.Cause(detached), importerCanceled)
	}
}

type trackedAfterFuncContext struct {
	context.Context
	done <-chan struct{}

	mu     sync.Mutex
	active int
}

func (ctx *trackedAfterFuncContext) AfterFunc(function func()) func() bool {
	ctx.mu.Lock()
	ctx.active++
	ctx.mu.Unlock()
	var stopOnce sync.Once
	return func() bool {
		stopped := false
		stopOnce.Do(func() {
			ctx.mu.Lock()
			ctx.active--
			ctx.mu.Unlock()
			stopped = true
		})
		return stopped
	}
}

func (ctx *trackedAfterFuncContext) Done() <-chan struct{} { return ctx.done }

func (ctx *trackedAfterFuncContext) activeAfterFuncs() int {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.active
}

func TestGenerationExecutionReleaseCleansCapturedCallerBridge(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtimeInstance.Close(context.Background()); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})

	program, err := CompileString("caller-bridge.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	importerContext := &trackedAfterFuncContext{
		Context: context.Background(),
		done:    make(chan struct{}),
	}
	outerContext, releaseOuter, err := script.acquireExecution(importerContext)
	if err != nil {
		t.Fatal(err)
	}
	releasedOuter := false
	t.Cleanup(func() {
		if !releasedOuter {
			if err := releaseOuter(); err != nil {
				t.Errorf("release outer execution: %v", err)
			}
		}
	})
	baseline := importerContext.activeAfterFuncs()

	generation := script.currentScriptGeneration()
	if generation == nil {
		t.Fatal("script generation is nil")
	}
	for iteration := 0; iteration < 128; iteration++ {
		innerContext, releaseInner, err := script.acquireGenerationExecution(outerContext, generation)
		if err != nil {
			t.Fatalf("acquire generation execution %d: %v", iteration, err)
		}
		token, _ := innerContext.Value(scriptExecutionContextKey{}).(*scriptExecutionToken)
		active := importerContext.activeAfterFuncs()
		if token == nil || token.caller == nil {
			_ = releaseInner()
			t.Fatalf("generation execution %d has no captured caller", iteration)
		}
		if active <= baseline {
			_ = releaseInner()
			t.Fatalf("generation execution %d active AfterFuncs = %d, want more than baseline %d", iteration, active, baseline)
		}
		capturedCaller := token.caller
		if err := releaseInner(); err != nil {
			t.Fatalf("release generation execution %d: %v", iteration, err)
		}
		if !errors.Is(capturedCaller.Err(), context.Canceled) {
			t.Fatalf("captured caller %d error = %v, want context.Canceled", iteration, capturedCaller.Err())
		}
		if err := outerContext.Err(); err != nil {
			t.Fatalf("outer execution canceled after inner release %d: %v", iteration, err)
		}
		if active := importerContext.activeAfterFuncs(); active != baseline {
			t.Fatalf("generation execution %d left %d active AfterFuncs, want baseline %d", iteration, active, baseline)
		}
	}
	if err := releaseOuter(); err != nil {
		t.Fatalf("release outer execution: %v", err)
	}
	releasedOuter = true
	if active := importerContext.activeAfterFuncs(); active != 0 {
		t.Fatalf("outer execution left %d active AfterFuncs, want 0", active)
	}
}

func TestDetachedGenerationExecutionRetainsCapturedCallerBridge(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtimeInstance.Close(context.Background()); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})

	program, err := CompileString("retained-caller-bridge.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	importerContext := &trackedAfterFuncContext{
		Context: context.Background(),
		done:    make(chan struct{}),
	}
	outerContext, releaseOuter, err := script.acquireExecution(importerContext)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := releaseOuter(); err != nil {
			t.Errorf("release outer execution: %v", err)
		}
	})
	baseline := importerContext.activeAfterFuncs()

	generation := script.currentScriptGeneration()
	innerContext, releaseInner, err := script.acquireGenerationExecution(outerContext, generation)
	if err != nil {
		t.Fatal(err)
	}
	token, _ := innerContext.Value(scriptExecutionContextKey{}).(*scriptExecutionToken)
	if token == nil || token.caller == nil {
		_ = releaseInner()
		t.Fatal("generation execution has no captured caller")
	}
	capturedCaller := token.caller
	firstDetached, releaseFirstDetached := detachExecutionLeaseCancellationLease(innerContext)
	wrappedDetached, cancelWrappedDetached := context.WithCancel(firstDetached)
	wrappedDetached = context.WithValue(
		callbackContextSnapshot{Context: wrappedDetached},
		asynchronousExecutionImporterContextKey{},
		"wrapped",
	)
	detached, releaseDetached := detachExecutionLeaseCancellationLease(wrappedDetached)
	if err := releaseInner(); err != nil {
		t.Fatal(err)
	}
	releaseFirstDetached()
	if err := capturedCaller.Err(); err != nil {
		t.Fatalf("retained caller ended with its synchronous execution: %v", err)
	}
	if err := detached.Err(); err != nil {
		t.Fatalf("detached context ended with its synchronous execution: %v", err)
	}
	if active := importerContext.activeAfterFuncs(); active <= baseline {
		t.Fatalf("retained caller active AfterFuncs = %d, want more than baseline %d", active, baseline)
	}

	releaseDetached()
	releaseDetached()
	cancelWrappedDetached()
	if !errors.Is(detached.Err(), context.Canceled) {
		t.Fatalf("detached error after final release = %v, want context.Canceled", detached.Err())
	}
	if active := importerContext.activeAfterFuncs(); active != baseline {
		t.Fatalf("detached release left %d active AfterFuncs, want baseline %d", active, baseline)
	}
}

func TestAsynchronousTaskCompletionPreservesNestedDetachedOwner(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtimeInstance.Close(context.Background()); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	program, err := CompileString("task-owner-bridge.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	importerContext, cancelImporter := context.WithCancel(context.Background())
	defer cancelImporter()
	outerContext, releaseOuter, err := script.acquireExecution(importerContext)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = releaseOuter() })
	taskContext, releaseTaskContext, cancelTaskContext := newAsynchronousExecutionTaskContext(outerContext)
	t.Cleanup(func() {
		cancelTaskContext(context.Canceled)
		releaseTaskContext()
	})

	callbackContext, releaseCallback, err := script.acquireExecution(taskContext)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = releaseCallback() })
	nestedContext, releaseNested, err := script.acquireGenerationExecution(
		callbackContext,
		script.currentScriptGeneration(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = releaseNested() })
	detached, releaseDetached := detachExecutionLeaseCancellationLease(nestedContext)
	t.Cleanup(releaseDetached)

	if err := releaseNested(); err != nil {
		t.Fatal(err)
	}
	if err := releaseCallback(); err != nil {
		t.Fatal(err)
	}
	// This is the natural fork/read/socket completion path: release the task's
	// owner without explicitly canceling descendants which retained it.
	releaseTaskContext()
	if err := releaseOuter(); err != nil {
		t.Fatal(err)
	}
	if err := detached.Err(); err != nil {
		t.Fatalf("nested detached owner ended with its parent task: %v", err)
	}

	cancelImporter()
	select {
	case <-detached.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("nested detached owner did not observe importer cancellation")
	}
	if !errors.Is(detached.Err(), context.Canceled) {
		t.Fatalf("nested detached error = %v, want context.Canceled", detached.Err())
	}
}

func TestCallbackSchedulingDoneRetentionSurvivesGarbageCollection(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtimeInstance.Close(context.Background()); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	program, err := CompileString("dispatch-done-bridge.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	importerContext, cancelImporter := context.WithCancel(context.Background())
	defer cancelImporter()
	outerContext, releaseOuter, err := script.acquireExecution(importerContext)
	if err != nil {
		t.Fatal(err)
	}

	done := func() <-chan struct{} {
		schedulingContext, _ := captureCallbackSchedulingContext(outerContext)
		return schedulingContext.Done()
	}()
	if err := releaseOuter(); err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 3; iteration++ {
		goruntime.GC()
		goruntime.Gosched()
	}
	select {
	case <-done:
		t.Fatal("retaining only scheduling Done was canceled by garbage collection")
	default:
	}

	cancelImporter()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retained scheduling Done did not observe importer cancellation")
	}
}

func TestRuntimeSpecificDetachRetainsFinalCancellationSource(t *testing.T) {
	runtimeA := &Runtime{}
	runtimeB := &Runtime{}
	sourceA, cancelA := context.WithCancelCause(context.Background())
	callerA, releaseCallerA, _ := newExecutionCallerCapture(
		sourceA,
		[]context.Context{sourceA},
		func() {},
	)
	sourceB, cancelB := context.WithCancelCause(context.Background())
	callerB, releaseCallerB, _ := newExecutionCallerCapture(
		sourceB,
		[]context.Context{sourceB},
		func() {},
	)
	t.Cleanup(func() {
		cancelA(context.Canceled)
		cancelB(context.Canceled)
		releaseCallerA()
		releaseCallerB()
	})

	tokenA := &runtimeExecutionToken{runtime: runtimeA, caller: callerA}
	tokenA.active.Store(true)
	tokenB := &runtimeExecutionToken{runtime: runtimeB, caller: callerB, parent: tokenA}
	tokenB.active.Store(true)
	ctx := context.WithValue(context.Background(), runtimeExecutionContextKey{}, tokenB)
	generalContext, releaseGeneral := detachExecutionLeaseCancellationLease(ctx)
	specificContext, releaseSpecific, detached := detachRuntimeCancellationLease(generalContext, runtimeA)
	if !detached {
		t.Fatal("runtime-specific cancellation source was not selected")
	}
	t.Cleanup(releaseSpecific)

	// Model token-owner release after the runtime-specific handoff retained A.
	tokenB.active.Store(false)
	tokenA.active.Store(false)
	releaseGeneral()
	releaseCallerB()
	releaseCallerA()
	if err := specificContext.Err(); err != nil {
		t.Fatalf("runtime-specific context ended with the general B source: %v", err)
	}

	wantCause := errors.New("runtime A importer canceled")
	cancelA(wantCause)
	select {
	case <-specificContext.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("runtime-specific context did not observe runtime A importer cancellation")
	}
	if !errors.Is(specificContext.Err(), context.Canceled) {
		t.Fatalf("runtime-specific error = %v, want context.Canceled", specificContext.Err())
	}
}
