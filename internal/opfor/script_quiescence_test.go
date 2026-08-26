package opfor

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const quiescenceTestTimeout = 2 * time.Second

func awaitQuiescenceError(t *testing.T, channel <-chan error, operation string) error {
	t.Helper()
	select {
	case err := <-channel:
		return err
	case <-time.After(quiescenceTestTimeout):
		t.Fatalf("timed out waiting for %s", operation)
		return nil
	}
}

func awaitQuiescenceSignal(t *testing.T, channel <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(quiescenceTestTimeout):
		t.Fatalf("timed out waiting for %s", operation)
	}
}

func assertQuiescencePending(t *testing.T, channel <-chan error, operation string) {
	t.Helper()
	select {
	case err := <-channel:
		t.Fatalf("%s completed before execution became quiescent: %v", operation, err)
	default:
	}
}

func TestNestedScriptUnloadRecognizesAncestorAndDoesNotDeadlock(t *testing.T) {
	var first *Script
	var second *Script
	var observerCalls atomic.Int32
	observer := ScriptLifecycleFuncs{
		Unloaded: func(ctx context.Context, script *Script) error {
			observerCalls.Add(1)
			switch script {
			case first:
				return second.Unload(ctx)
			case second:
				return first.Unload(ctx)
			default:
				return nil
			}
		},
	}
	runtimeInstance, err := New(WithScriptLifecycleObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("nested-unload.cna", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	first, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	second, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	unloadResult := make(chan error, 1)
	go func() { unloadResult <- first.Unload(context.Background()) }()
	if err := awaitQuiescenceError(t, unloadResult, "nested unload cycle"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if err := second.Unload(context.Background()); err != nil {
		t.Fatalf("waiting for nested second Unload: %v", err)
	}
	if first.Active() || second.Active() {
		t.Fatalf("active state after nested unload = first %v second %v", first.Active(), second.Active())
	}
	if got := observerCalls.Load(); got != 2 {
		t.Fatalf("unload observer calls = %d, want 2", got)
	}
}

func TestNestedRuntimeCloseRecognizesAncestorAcrossScriptLoader(t *testing.T) {
	var parentRuntime *Runtime
	childObserverEntered := make(chan struct{})
	var childObserverOnce sync.Once
	observer := ScriptLifecycleFuncs{
		Unloaded: func(ctx context.Context, script *Script) error {
			if parentRuntime != nil && script.runtime != parentRuntime {
				childObserverOnce.Do(func() { close(childObserverEntered) })
				return parentRuntime.Close(ctx)
			}
			return nil
		},
	}
	var err error
	parentRuntime, err = New(WithScriptLifecycleObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("nested-runtime-close.cna", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "nested-child.sl", 'return 1;', $null];
[$child runScript];
return 1;
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parentRuntime.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}

	closeResult := make(chan error, 1)
	go func() { closeResult <- parentRuntime.Close(context.Background()) }()
	awaitQuiescenceSignal(t, childObserverEntered, "child ScriptUnloaded observer")
	if err := awaitQuiescenceError(t, closeResult, "nested Runtime.Close cycle"); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCompletedLifecycleContextsDoNotMasqueradeAsReentrant(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("stale-lifecycle-context.cna", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	unloadCtx, releaseUnload := withScriptUnloadContext(context.Background(), script)
	releaseUnload()
	if contextUnloadsScript(unloadCtx, script) || contextOwnsRuntimeUnload(unloadCtx, runtimeInstance) {
		t.Fatal("completed script-unload token remained active")
	}
	closeCtx, releaseClose := withRuntimeCloseContext(context.Background(), runtimeInstance)
	releaseClose()
	if contextClosesRuntime(closeCtx, runtimeInstance) {
		t.Fatal("completed runtime-close token remained active")
	}

	if err := script.Unload(unloadCtx); err != nil {
		t.Fatalf("Unload with completed lifecycle context: %v", err)
	}
	if err := runtimeInstance.Close(closeCtx); err != nil {
		t.Fatalf("Close with completed lifecycle context: %v", err)
	}
}

func TestSplitContextWaitErrorPreservesIndependentBranches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sentinel := errors.New("cleanup sentinel")

	remaining, waitExpired := splitContextWaitError(ctx, errors.Join(
		fmt.Errorf("wait failed: %w", context.Canceled),
		fmt.Errorf("observer failed: %w", sentinel),
	))
	if !waitExpired {
		t.Fatal("joined context error was not recognized as an expired wait")
	}
	if !errors.Is(remaining, sentinel) {
		t.Fatalf("remaining error = %v, want cleanup sentinel", remaining)
	}
	if errors.Is(remaining, context.Canceled) {
		t.Fatalf("remaining error = %v, still contains context.Canceled", remaining)
	}

	remaining, waitExpired = splitContextWaitError(ctx, fmt.Errorf("wait failed: %w", context.Canceled))
	if !waitExpired || remaining != nil {
		t.Fatalf("pure wait error split = (%v, %v), want (nil, true)", remaining, waitExpired)
	}
}

func TestRuntimeClosePreservesObserverErrorJoinedWithExpiredWait(t *testing.T) {
	for iteration := 0; iteration < 25; iteration++ {
		sentinel := fmt.Errorf("cleanup sentinel %d", iteration)
		observer := ScriptLifecycleFuncs{
			Unloaded: func(ctx context.Context, _ *Script) error {
				<-ctx.Done()
				return errors.Join(ctx.Err(), sentinel)
			},
		}
		runtimeInstance, err := New(WithScriptLifecycleObserver(observer))
		if err != nil {
			t.Fatal(err)
		}
		program, err := CompileString("mixed-cleanup-error.cna", `return 1;`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := runtimeInstance.Close(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("first Close error = %v, want context.Canceled", err)
		}
		terminalErr := runtimeInstance.Close(context.Background())
		if !errors.Is(terminalErr, sentinel) {
			t.Fatalf("terminal Close error = %v, want %v", terminalErr, sentinel)
		}
		if errors.Is(terminalErr, context.Canceled) {
			t.Fatalf("terminal Close error = %v, should not retain expired wait", terminalErr)
		}
	}
}

func TestRuntimeInvokeRejectsSynchronouslyCanceledScriptExecution(t *testing.T) {
	var runtimeInstance *Runtime
	var script *Script
	var recordCalls atomic.Int32
	var err error
	runtimeInstance, err = New(
		WithFunction("record_after_unload", func(context.Context, Invocation) (Value, error) {
			recordCalls.Add(1)
			return Int(1), nil
		}),
		WithFunction("unload_then_reenter_runtime", func(ctx context.Context, _ Invocation) (Value, error) {
			if err := script.Unload(ctx); err != nil {
				return Null(), err
			}
			return runtimeInstance.Invoke(ctx, "record_after_unload")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("unload-reenter-runtime.cna", `sub run { return unload_then_reenter_runtime(); }`)
	if err != nil {
		t.Fatal(err)
	}
	script, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := script.Call(context.Background(), "run"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Call error = %v, want context.Canceled", err)
	}
	if got := recordCalls.Load(); got != 0 {
		t.Fatalf("record calls = %d, want 0", got)
	}
}

func TestReentrantUnloadInitiatorReceivesCleanupErrorWithConcurrentExecution(t *testing.T) {
	cleanupErr := errors.New("initiating execution cleanup failure")
	blockedEntered := make(chan struct{})
	releaseBlocked := make(chan struct{})
	var script *Script
	runtimeInstance, err := New(
		WithScriptLifecycleObserver(ScriptLifecycleFuncs{
			Unloaded: func(context.Context, *Script) error { return cleanupErr },
		}),
		WithFunction("unload_current_script", func(ctx context.Context, _ Invocation) (Value, error) {
			return Null(), script.Unload(ctx)
		}),
		WithFunction("block_concurrent_execution", func(ctx context.Context, _ Invocation) (Value, error) {
			close(blockedEntered)
			<-ctx.Done()
			<-releaseBlocked
			return Null(), ctx.Err()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("cleanup-recipient.cna", `
sub initiate { unload_current_script(); }
sub concurrent { block_concurrent_execution(); }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	concurrentResult := make(chan error, 1)
	go func() {
		_, callErr := script.Call(context.Background(), "concurrent")
		concurrentResult <- callErr
	}()
	awaitQuiescenceSignal(t, blockedEntered, "concurrent execution")
	initiatorResult := make(chan error, 1)
	go func() {
		_, callErr := script.Call(context.Background(), "initiate")
		initiatorResult <- callErr
	}()
	awaitQuiescenceSignal(t, script.executionCtx.Done(), "reentrant unload cancellation")
	assertQuiescencePending(t, initiatorResult, "reentrant unload initiator")

	close(releaseBlocked)
	if err := awaitQuiescenceError(t, concurrentResult, "concurrent execution release"); !errors.Is(err, context.Canceled) {
		t.Fatalf("concurrent Call error = %v, want context.Canceled", err)
	} else if errors.Is(err, cleanupErr) {
		t.Fatalf("concurrent Call consumed initiator cleanup error: %v", err)
	}
	if err := awaitQuiescenceError(t, initiatorResult, "reentrant unload initiator"); !errors.Is(err, context.Canceled) || !errors.Is(err, cleanupErr) {
		t.Fatalf("initiator Call error = %v, want cancellation and cleanup failure", err)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("cleanup error was delivered more than once: %v", err)
	}
}

func TestReentrantUnloadRecipientCancellationLeavesCleanupForLaterWaiter(t *testing.T) {
	cleanupErr := errors.New("abandoned recipient cleanup failure")
	blockedEntered := make(chan struct{})
	releaseBlocked := make(chan struct{})
	var script *Script
	runtimeInstance, err := New(
		WithScriptLifecycleObserver(ScriptLifecycleFuncs{
			Unloaded: func(context.Context, *Script) error { return cleanupErr },
		}),
		WithFunction("unload_for_abandoned_recipient", func(ctx context.Context, _ Invocation) (Value, error) {
			return Null(), script.Unload(ctx)
		}),
		WithFunction("block_for_abandoned_recipient", func(ctx context.Context, _ Invocation) (Value, error) {
			close(blockedEntered)
			<-ctx.Done()
			<-releaseBlocked
			return Null(), ctx.Err()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("abandoned-cleanup-recipient.cna", `
sub initiate { unload_for_abandoned_recipient(); }
sub concurrent { block_for_abandoned_recipient(); }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	concurrentResult := make(chan error, 1)
	go func() {
		_, callErr := script.Call(context.Background(), "concurrent")
		concurrentResult <- callErr
	}()
	awaitQuiescenceSignal(t, blockedEntered, "concurrent execution for recipient cancellation")
	initiatorCtx, cancelInitiator := context.WithCancel(context.Background())
	initiatorResult := make(chan error, 1)
	go func() {
		_, callErr := script.Call(initiatorCtx, "initiate")
		initiatorResult <- callErr
	}()
	awaitQuiescenceSignal(t, script.executionCtx.Done(), "recipient unload cancellation")
	cancelInitiator()
	if err := awaitQuiescenceError(t, initiatorResult, "canceled unload recipient"); !errors.Is(err, context.Canceled) || errors.Is(err, cleanupErr) {
		t.Fatalf("initiator error = %v, want only cancellation", err)
	}

	laterResult := make(chan error, 1)
	go func() { laterResult <- script.Unload(context.Background()) }()
	assertQuiescencePending(t, laterResult, "later cleanup waiter")
	close(releaseBlocked)
	if err := awaitQuiescenceError(t, concurrentResult, "concurrent execution after recipient cancellation"); !errors.Is(err, context.Canceled) {
		t.Fatalf("concurrent Call error = %v, want context.Canceled", err)
	}
	if err := awaitQuiescenceError(t, laterResult, "later cleanup waiter"); !errors.Is(err, cleanupErr) {
		t.Fatalf("later Unload error = %v, want cleanup failure", err)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("cleanup error was delivered more than once: %v", err)
	}
}

func TestLateUnloadWaiterCannotStealReservedRecipientError(t *testing.T) {
	for iteration := 0; iteration < 25; iteration++ {
		cleanupErr := fmt.Errorf("reserved recipient cleanup failure %d", iteration)
		cleanupEntered := make(chan struct{})
		releaseCleanup := make(chan struct{})
		var script *Script
		runtimeInstance, err := New(
			WithScriptLifecycleObserver(ScriptLifecycleFuncs{
				Unloaded: func(context.Context, *Script) error {
					close(cleanupEntered)
					<-releaseCleanup
					return cleanupErr
				},
			}),
			WithFunction("unload_for_late_waiter", func(ctx context.Context, _ Invocation) (Value, error) {
				return Null(), script.Unload(ctx)
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		program, err := CompileString("late-cleanup-waiter.cna", `sub initiate { unload_for_late_waiter(); }`)
		if err != nil {
			t.Fatal(err)
		}
		script, err = runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		initiatorResult := make(chan error, 1)
		go func() {
			_, callErr := script.Call(context.Background(), "initiate")
			initiatorResult <- callErr
		}()
		awaitQuiescenceSignal(t, cleanupEntered, "reserved-recipient cleanup")
		lateResult := make(chan error, 1)
		go func() { lateResult <- script.Unload(context.Background()) }()
		assertQuiescencePending(t, lateResult, "late unload waiter")
		close(releaseCleanup)
		if err := awaitQuiescenceError(t, initiatorResult, "reserved unload recipient"); !errors.Is(err, cleanupErr) {
			t.Fatalf("initiator error = %v, want cleanup failure", err)
		}
		if err := awaitQuiescenceError(t, lateResult, "late unload waiter"); err != nil {
			t.Fatalf("late waiter stole/repeated cleanup failure: %v", err)
		}
	}
}

func TestMixedScriptAncestryDoesNotReserveTargetCleanupRecipient(t *testing.T) {
	cleanupErr := errors.New("mixed ancestry cleanup failure")
	var first *Script
	var second *Script
	runtimeInstance, err := New(
		WithScriptLifecycleObserver(ScriptLifecycleFuncs{
			Unloaded: func(_ context.Context, script *Script) error {
				if script == first {
					return cleanupErr
				}
				return nil
			},
		}),
		WithFunction("unload_outer_script", func(ctx context.Context, _ Invocation) (Value, error) {
			return Null(), first.Unload(ctx)
		}),
		WithFunction("call_second_script", func(ctx context.Context, _ Invocation) (Value, error) {
			return second.Call(ctx, "inner")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	firstProgram, err := CompileString("mixed-ancestry-first.cna", `sub outer { call_second_script(); }`)
	if err != nil {
		t.Fatal(err)
	}
	secondProgram, err := CompileString("mixed-ancestry-second.cna", `sub inner { unload_outer_script(); }`)
	if err != nil {
		t.Fatal(err)
	}
	first, err = runtimeInstance.Load(context.Background(), firstProgram)
	if err != nil {
		t.Fatal(err)
	}
	second, err = runtimeInstance.Load(context.Background(), secondProgram)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, callErr := first.Call(context.Background(), "outer")
		result <- callErr
	}()
	if err := awaitQuiescenceError(t, result, "mixed-script nested unload"); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("outer Call error = %v, want nil or cancellation", err)
	}
	if err := first.Unload(context.Background()); !errors.Is(err, cleanupErr) {
		t.Fatalf("later target Unload error = %v, want cleanup failure", err)
	}
	if err := second.Unload(context.Background()); err != nil {
		t.Fatalf("second Unload: %v", err)
	}
}

func TestNestedSameScriptUnloadDeliversCleanupToOutermostEntry(t *testing.T) {
	cleanupErr := errors.New("nested cleanup failure")
	var script *Script
	runtimeInstance, err := New(
		WithScriptLifecycleObserver(ScriptLifecycleFuncs{
			Unloaded: func(context.Context, *Script) error { return cleanupErr },
		}),
		WithFunction("unload_nested_script", func(ctx context.Context, _ Invocation) (Value, error) {
			return Null(), script.Unload(ctx)
		}),
		WithFunction("call_nested_entry", func(ctx context.Context, _ Invocation) (Value, error) {
			return script.Call(ctx, "inner")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("nested-cleanup-recipient.cna", `
sub inner { unload_nested_script(); }
sub outer { call_nested_entry(); }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, callErr := script.Call(context.Background(), "outer")
		result <- callErr
	}()
	if err := awaitQuiescenceError(t, result, "nested same-script unload"); !errors.Is(err, context.Canceled) || !errors.Is(err, cleanupErr) {
		t.Fatalf("outer Call error = %v, want cancellation and cleanup failure", err)
	}
}

func TestConcurrentCrossScriptUnloadCallsDoNotWaitOnEachOther(t *testing.T) {
	entered := make(chan ScriptID, 2)
	release := make(chan struct{})
	scripts := make(map[ScriptID]*Script)
	var scriptsMu sync.RWMutex
	runtimeInstance, err := New(WithFunction("unload_other_script", func(ctx context.Context, invocation Invocation) (Value, error) {
		entered <- invocation.Script
		<-release
		scriptsMu.RLock()
		var other *Script
		for id, candidate := range scripts {
			if id != invocation.Script {
				other = candidate
				break
			}
		}
		scriptsMu.RUnlock()
		return Null(), other.Unload(ctx)
	}))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("cross-script-unload.cna", `sub run { unload_other_script(); }`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		script, loadErr := runtimeInstance.Load(context.Background(), program)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		scriptsMu.Lock()
		scripts[script.ID()] = script
		scriptsMu.Unlock()
	}
	results := make(chan error, 2)
	for _, script := range scripts {
		go func(script *Script) {
			_, callErr := script.Call(context.Background(), "run")
			results <- callErr
		}(script)
	}
	awaitQuiescenceSignal(t, scriptIDSignal(entered), "first cross-script callback")
	awaitQuiescenceSignal(t, scriptIDSignal(entered), "second cross-script callback")
	close(release)
	for index := 0; index < 2; index++ {
		if err := awaitQuiescenceError(t, results, "cross-script unload callback"); err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Call error = %v, want nil or context.Canceled", err)
		}
	}
}

func scriptIDSignal(values <-chan ScriptID) <-chan struct{} {
	signal := make(chan struct{})
	go func() {
		<-values
		close(signal)
	}()
	return signal
}

func TestConcurrentUnloadObserversDoNotWaitOnEachOther(t *testing.T) {
	entered := make(chan *Script, 2)
	release := make(chan struct{})
	var first *Script
	var second *Script
	observer := ScriptLifecycleFuncs{
		Unloaded: func(ctx context.Context, script *Script) error {
			entered <- script
			<-release
			if script == first {
				return second.Unload(ctx)
			}
			return first.Unload(ctx)
		},
	}
	runtimeInstance, err := New(WithScriptLifecycleObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("cross-observer-unload.cna", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	first, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	second, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { results <- first.Unload(context.Background()) }()
	go func() { results <- second.Unload(context.Background()) }()
	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(quiescenceTestTimeout):
			t.Fatal("timed out waiting for concurrent unload observers")
		}
	}
	close(release)
	for index := 0; index < 2; index++ {
		if err := awaitQuiescenceError(t, results, "concurrent unload observer"); err != nil {
			t.Fatalf("Unload: %v", err)
		}
	}
}

func TestConcurrentCrossRuntimeCloseCallsDoNotWaitOnEachOther(t *testing.T) {
	entered := make(chan *Runtime, 2)
	release := make(chan struct{})
	var firstRuntime *Runtime
	var secondRuntime *Runtime
	newRuntime := func() *Runtime {
		instance, err := New(WithFunction("close_other_runtime", func(ctx context.Context, invocation Invocation) (Value, error) {
			entered <- invocation.Runtime
			<-release
			if invocation.Runtime == firstRuntime {
				return Null(), secondRuntime.Close(ctx)
			}
			return Null(), firstRuntime.Close(ctx)
		}))
		if err != nil {
			t.Fatal(err)
		}
		return instance
	}
	firstRuntime = newRuntime()
	secondRuntime = newRuntime()
	program, err := CompileString("cross-runtime-close.cna", `sub run { close_other_runtime(); }`)
	if err != nil {
		t.Fatal(err)
	}
	firstScript, err := firstRuntime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	secondScript, err := secondRuntime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() {
		_, callErr := firstScript.Call(context.Background(), "run")
		results <- callErr
	}()
	go func() {
		_, callErr := secondScript.Call(context.Background(), "run")
		results <- callErr
	}()
	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(quiescenceTestTimeout):
			t.Fatal("timed out waiting for cross-runtime Host callbacks")
		}
	}
	close(release)
	for index := 0; index < 2; index++ {
		if err := awaitQuiescenceError(t, results, "cross-runtime Close callback"); err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Call error = %v, want nil or context.Canceled", err)
		}
	}
	if err := firstRuntime.Close(context.Background()); err != nil {
		t.Fatalf("first terminal Close: %v", err)
	}
	if err := secondRuntime.Close(context.Background()); err != nil {
		t.Fatalf("second terminal Close: %v", err)
	}
}

func TestConcurrentCrossRuntimeCloseObserversDoNotWaitOnEachOther(t *testing.T) {
	entered := make(chan *Runtime, 2)
	release := make(chan struct{})
	var firstRuntime *Runtime
	var secondRuntime *Runtime
	newRuntime := func() *Runtime {
		var own *Runtime
		instance, err := New(WithScriptLifecycleObserver(ScriptLifecycleFuncs{
			Unloaded: func(ctx context.Context, _ *Script) error {
				entered <- own
				<-release
				if own == firstRuntime {
					return secondRuntime.Close(ctx)
				}
				return firstRuntime.Close(ctx)
			},
		}))
		if err != nil {
			t.Fatal(err)
		}
		own = instance
		return instance
	}
	firstRuntime = newRuntime()
	secondRuntime = newRuntime()
	program, err := CompileString("cross-runtime-close-observer.cna", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstRuntime.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if _, err := secondRuntime.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { results <- firstRuntime.Close(context.Background()) }()
	go func() { results <- secondRuntime.Close(context.Background()) }()
	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(quiescenceTestTimeout):
			t.Fatal("timed out waiting for cross-runtime Close observers")
		}
	}
	close(release)
	for index := 0; index < 2; index++ {
		if err := awaitQuiescenceError(t, results, "cross-runtime Close observer"); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
}

func TestMixedScriptRuntimeCloseDefersAllActiveScriptsToPrivateWorker(t *testing.T) {
	firstCleanup := errors.New("first mixed Close cleanup failure")
	secondCleanup := errors.New("second mixed Close cleanup failure")
	var runtimeInstance *Runtime
	var first *Script
	var second *Script
	var err error
	runtimeInstance, err = New(
		WithScriptLifecycleObserver(ScriptLifecycleFuncs{
			Unloaded: func(_ context.Context, script *Script) error {
				switch script {
				case first:
					return firstCleanup
				case second:
					return secondCleanup
				default:
					return nil
				}
			},
		}),
		WithFunction("close_mixed_runtime", func(ctx context.Context, _ Invocation) (Value, error) {
			return Null(), runtimeInstance.Close(ctx)
		}),
		WithFunction("call_mixed_second", func(ctx context.Context, _ Invocation) (Value, error) {
			return second.Call(ctx, "inner")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	firstProgram, err := CompileString("mixed-close-first.cna", `sub outer { call_mixed_second(); }`)
	if err != nil {
		t.Fatal(err)
	}
	secondProgram, err := CompileString("mixed-close-second.cna", `sub inner { close_mixed_runtime(); }`)
	if err != nil {
		t.Fatal(err)
	}
	first, err = runtimeInstance.Load(context.Background(), firstProgram)
	if err != nil {
		t.Fatal(err)
	}
	second, err = runtimeInstance.Load(context.Background(), secondProgram)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, callErr := first.Call(context.Background(), "outer")
		result <- callErr
	}()
	if err := awaitQuiescenceError(t, result, "mixed-script Runtime.Close"); !errors.Is(err, context.Canceled) {
		t.Fatalf("outer Call error = %v, want context.Canceled", err)
	} else if errors.Is(err, firstCleanup) || errors.Is(err, secondCleanup) {
		t.Fatalf("mixed-script execution consumed private-worker cleanup error: %v", err)
	}
	closeErr := runtimeInstance.Close(context.Background())
	if !errors.Is(closeErr, firstCleanup) || !errors.Is(closeErr, secondCleanup) {
		t.Fatalf("terminal Close error = %v, want both cleanup failures", closeErr)
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatalf("cleanup errors were delivered more than once: %v", err)
	}
}

func TestConcurrentCloseWaitsForScriptLoadedBeforeUnloaded(t *testing.T) {
	loaded := make(chan struct{})
	canceled := make(chan struct{})
	releaseLoaded := make(chan struct{})
	unloaded := make(chan struct{})
	observer := ScriptLifecycleFuncs{
		Loaded: func(ctx context.Context, _ *Script) error {
			close(loaded)
			<-ctx.Done()
			close(canceled)
			<-releaseLoaded
			return nil
		},
		Unloaded: func(context.Context, *Script) error {
			close(unloaded)
			return nil
		},
	}
	runtime, err := New(WithScriptLifecycleObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loaded-order.cna", `$top_level = 1;`)
	if err != nil {
		t.Fatal(err)
	}

	loadResult := make(chan error, 1)
	go func() {
		_, loadErr := runtime.Load(context.Background(), program)
		loadResult <- loadErr
	}()
	awaitQuiescenceSignal(t, loaded, "ScriptLoaded entry")

	closeResult := make(chan error, 1)
	go func() { closeResult <- runtime.Close(context.Background()) }()
	awaitQuiescenceSignal(t, canceled, "ScriptLoaded cancellation")
	assertQuiescencePending(t, closeResult, "Close")
	select {
	case <-unloaded:
		t.Fatal("ScriptUnloaded ran before ScriptLoaded returned")
	default:
	}

	close(releaseLoaded)
	if err := awaitQuiescenceError(t, loadResult, "Load"); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("Load error = %v, want ErrScriptUnloaded", err)
	}
	if err := awaitQuiescenceError(t, closeResult, "Close"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	awaitQuiescenceSignal(t, unloaded, "ScriptUnloaded")
}

func TestCloseCancelsBlockedTopLevelAndClosesAdmission(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	releaseHost := make(chan struct{})
	runtime, err := New(WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
		if invocation.Name != "block_top_level" {
			return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
		}
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-releaseHost
		return Int(7), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("blocked-top-level.cna", `$after = block_top_level();`)
	if err != nil {
		t.Fatal(err)
	}

	loadResult := make(chan error, 1)
	go func() {
		_, loadErr := runtime.Load(context.Background(), program)
		loadResult <- loadErr
	}()
	awaitQuiescenceSignal(t, entered, "blocked top-level Host call")
	scripts := runtime.Scripts()
	if len(scripts) != 1 {
		t.Fatalf("published scripts = %d, want 1", len(scripts))
	}
	blockedScript := scripts[0]

	closeResult := make(chan error, 1)
	go func() { closeResult <- runtime.Close(context.Background()) }()
	awaitQuiescenceSignal(t, canceled, "top-level execution cancellation")
	assertQuiescencePending(t, closeResult, "Close")

	other, err := CompileString("rejected-load.cna", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Load(context.Background(), other); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("Load while closing = %v, want ErrRuntimeClosed", err)
	}
	if _, err := runtime.Eval(context.Background(), "rejected-eval.cna", `return 1;`); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("Eval while closing = %v, want ErrRuntimeClosed", err)
	}

	close(releaseHost)
	if err := awaitQuiescenceError(t, loadResult, "blocked Load"); !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked Load error = %v, want context.Canceled", err)
	}
	if err := awaitQuiescenceError(t, closeResult, "Close"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !blockedScript.Get("$after").IsNull() {
		t.Fatalf("top-level mutated after cancellation: %s", blockedScript.Get("$after").Describe())
	}
	if scripts := runtime.Scripts(); len(scripts) != 0 {
		t.Fatalf("scripts after Close = %d, want 0", len(scripts))
	}
	if _, err := runtime.Load(context.Background(), other); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("post-Close Load = %v, want ErrRuntimeClosed", err)
	}
}

func TestUnloadWaitsForBlockedCallbackAndPreventsLaterMutation(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	releaseHost := make(chan struct{})
	runtime, err := New(WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
		if invocation.Name != "block_callback" {
			return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
		}
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-releaseHost
		return Int(9), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("blocked-callback.cna", `on ready { $after = block_callback(); }`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	dispatchResult := make(chan error, 1)
	go func() {
		_, dispatchErr := runtime.DispatchEvent(context.Background(), "ready")
		dispatchResult <- dispatchErr
	}()
	awaitQuiescenceSignal(t, entered, "blocked callback Host call")

	unloadResult := make(chan error, 1)
	go func() { unloadResult <- script.Unload(context.Background()) }()
	awaitQuiescenceSignal(t, canceled, "callback execution cancellation")
	assertQuiescencePending(t, unloadResult, "Unload")
	if bindings := runtime.Bindings(BindingEvent, "ready"); len(bindings) != 0 {
		t.Fatalf("bindings after unload request = %d, want 0", len(bindings))
	}

	close(releaseHost)
	if err := awaitQuiescenceError(t, dispatchResult, "DispatchEvent"); !errors.Is(err, context.Canceled) {
		t.Fatalf("DispatchEvent error = %v, want context.Canceled", err)
	}
	if err := awaitQuiescenceError(t, unloadResult, "Unload"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if !script.Get("$after").IsNull() {
		t.Fatalf("callback mutated after reentrant cancellation: %s", script.Get("$after").Describe())
	}
}

func TestReentrantHostUnloadPreservesCallerContextAndJoinsCleanup(t *testing.T) {
	type contextKey struct{}
	const contextValue = "importer-value"
	cleanupErr := errors.New("cleanup failure")
	var script *Script
	observation := make(chan error, 1)
	observer := ScriptLifecycleFuncs{
		Loaded: func(_ context.Context, loaded *Script) error {
			script = loaded
			return nil
		},
		Unloaded: func(ctx context.Context, unloaded *Script) error {
			var observed error
			if got := ctx.Value(contextKey{}); got != contextValue {
				observed = errors.Join(observed, fmt.Errorf("cleanup context value = %v", got))
			}
			if ctx.Err() != nil {
				observed = errors.Join(observed, fmt.Errorf("cleanup context was canceled by script: %w", ctx.Err()))
			}
			if err := unloaded.Unload(ctx); err != nil {
				observed = errors.Join(observed, fmt.Errorf("reentrant observer Unload: %w", err))
			}
			observation <- observed
			return cleanupErr
		},
	}
	hostResult := make(chan error, 1)
	runtime, err := New(
		WithScriptLifecycleObserver(observer),
		WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
			if invocation.Name != "unload_self" {
				return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
			}
			hostResult <- script.Unload(ctx)
			return Int(11), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("reentrant-unload.cna", `$after = unload_self();`)
	if err != nil {
		t.Fatal(err)
	}
	base := context.WithValue(context.Background(), contextKey{}, contextValue)
	ctx, cancel := context.WithTimeout(base, 10*time.Second)
	defer cancel()
	wantDeadline, _ := ctx.Deadline()

	_, loadErr := runtime.Load(ctx, program)
	if err := awaitQuiescenceError(t, hostResult, "reentrant Host Unload"); err != nil {
		t.Fatalf("reentrant Host Unload: %v", err)
	}
	if !errors.Is(loadErr, context.Canceled) || !errors.Is(loadErr, cleanupErr) {
		t.Fatalf("Load error = %v, want cancellation and cleanup failure", loadErr)
	}
	if err := awaitQuiescenceError(t, observation, "cleanup context observation"); err != nil {
		t.Fatal(err)
	}
	// Deadline is checked outside the callback too, so a future change that
	// accidentally strips it cannot hide behind a fast cleanup.
	if gotDeadline, ok := script.unloadContext.Deadline(); !ok || !gotDeadline.Equal(wantDeadline) {
		t.Fatalf("cleanup deadline = %v, %v; want %v, true", gotDeadline, ok, wantDeadline)
	}
	if !script.Get("$after").IsNull() {
		t.Fatalf("top-level assignment survived reentrant unload: %s", script.Get("$after").Describe())
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("cleanup error was delivered more than once: %v", err)
	}
}

func TestReentrantRuntimeCloseJoinsCleanupIntoEnclosingCallback(t *testing.T) {
	cleanupErr := errors.New("close cleanup failure")
	var runtimeInstance *Runtime
	observer := ScriptLifecycleFuncs{
		Unloaded: func(context.Context, *Script) error { return cleanupErr },
	}
	hostClose := make(chan error, 1)
	created, err := New(
		WithScriptLifecycleObserver(observer),
		WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
			if invocation.Name != "close_self" {
				return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
			}
			hostClose <- runtimeInstance.Close(ctx)
			return Int(13), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	runtimeInstance = created
	program, err := CompileString("reentrant-close.cna", `on ready { $after = close_self(); }`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	_, dispatchErr := runtimeInstance.DispatchEvent(context.Background(), "ready")
	if err := awaitQuiescenceError(t, hostClose, "reentrant Host Close"); err != nil {
		t.Fatalf("reentrant Host Close: %v", err)
	}
	if !errors.Is(dispatchErr, context.Canceled) || !errors.Is(dispatchErr, cleanupErr) {
		t.Fatalf("DispatchEvent error = %v, want cancellation and cleanup failure", dispatchErr)
	}
	if !script.Get("$after").IsNull() {
		t.Fatalf("callback assignment survived reentrant Close: %s", script.Get("$after").Describe())
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatalf("waiting Close repeated callback cleanup error: %v", err)
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatalf("cleanup error was delivered more than once: %v", err)
	}
}

func TestUnloadDeadlineReturnsWhileCleanupContinuesAndDeliversErrorOnce(t *testing.T) {
	cleanupErr := errors.New("deferred unload cleanup failure")
	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	runtimeInstance, err := New(WithScriptLifecycleObserver(ScriptLifecycleFuncs{
		Unloaded: func(context.Context, *Script) error {
			close(cleanupEntered)
			<-releaseCleanup
			return cleanupErr
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("deferred-unload.cna", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	unloadResult := make(chan error, 1)
	go func() { unloadResult <- script.Unload(ctx) }()
	awaitQuiescenceSignal(t, cleanupEntered, "blocking unload cleanup")
	cancel()
	if err := awaitQuiescenceError(t, unloadResult, "deadline-aware Unload"); !errors.Is(err, context.Canceled) {
		t.Fatalf("first Unload error = %v, want context.Canceled", err)
	}

	close(releaseCleanup)
	if err := script.Unload(context.Background()); !errors.Is(err, cleanupErr) {
		t.Fatalf("waiting Unload error = %v, want cleanup failure", err)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("cleanup error was delivered more than once: %v", err)
	}
}

func TestCloseDeadlineReturnsWhileCleanupContinuesAndDeliversErrorOnce(t *testing.T) {
	cleanupErr := errors.New("deferred close cleanup failure")
	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	runtimeInstance, err := New(WithScriptLifecycleObserver(ScriptLifecycleFuncs{
		Unloaded: func(context.Context, *Script) error {
			close(cleanupEntered)
			<-releaseCleanup
			return cleanupErr
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("deferred-close.cna", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	closeResult := make(chan error, 1)
	go func() { closeResult <- runtimeInstance.Close(ctx) }()
	awaitQuiescenceSignal(t, cleanupEntered, "blocking close cleanup")
	cancel()
	if err := awaitQuiescenceError(t, closeResult, "deadline-aware Close"); !errors.Is(err, context.Canceled) {
		t.Fatalf("first Close error = %v, want context.Canceled", err)
	}

	close(releaseCleanup)
	if err := runtimeInstance.Close(context.Background()); !errors.Is(err, cleanupErr) || errors.Is(err, context.Canceled) {
		t.Fatalf("waiting Close error = %v, want only cleanup failure", err)
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatalf("cleanup error was delivered more than once: %v", err)
	}
}

func TestCloseWaitsForDirectRuntimeInvokeAndRejectsNewInvocations(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	releaseHost := make(chan struct{})
	runtimeInstance, err := New(WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
		if invocation.Name != "block_direct_invoke" {
			return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
		}
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-releaseHost
		return Int(17), nil
	})))
	if err != nil {
		t.Fatal(err)
	}

	invokeResult := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.Invoke(context.Background(), "block_direct_invoke")
		invokeResult <- invokeErr
	}()
	awaitQuiescenceSignal(t, entered, "direct Runtime.Invoke Host call")

	closeResult := make(chan error, 1)
	go func() { closeResult <- runtimeInstance.Close(context.Background()) }()
	awaitQuiescenceSignal(t, canceled, "direct Runtime.Invoke cancellation")
	assertQuiescencePending(t, closeResult, "Close")
	if _, err := runtimeInstance.Invoke(context.Background(), "println", String("too late")); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("Invoke while closing = %v, want ErrRuntimeClosed", err)
	}

	close(releaseHost)
	if err := awaitQuiescenceError(t, invokeResult, "direct Runtime.Invoke"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Runtime.Invoke error = %v, want context.Canceled", err)
	}
	if err := awaitQuiescenceError(t, closeResult, "Close"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "println", String("too late")); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("post-Close Invoke = %v, want ErrRuntimeClosed", err)
	}
}

func TestReentrantDirectRuntimeInvokeCloseCancelsBeforeHostReturns(t *testing.T) {
	var runtimeInstance *Runtime
	hostClose := make(chan error, 1)
	created, err := New(WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
		if invocation.Name != "close_direct_invoke" {
			return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
		}
		hostClose <- runtimeInstance.Close(ctx)
		return Int(19), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	runtimeInstance = created

	_, invokeErr := runtimeInstance.Invoke(context.Background(), "close_direct_invoke")
	if err := awaitQuiescenceError(t, hostClose, "reentrant direct Runtime.Close"); err != nil {
		t.Fatalf("Host Close: %v", err)
	}
	if !errors.Is(invokeErr, context.Canceled) {
		t.Fatalf("Runtime.Invoke error = %v, want context.Canceled", invokeErr)
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatalf("waiting Close: %v", err)
	}
}

func TestNestedRuntimeInvokeDetachedAsyncPreservesImporterCancellation(t *testing.T) {
	inspectAsync := make(chan struct{})
	observations := make(chan error, 2)
	var runtimeInstance *Runtime
	created, err := New(
		WithFunction("start_detached_async", func(ctx context.Context, _ Invocation) (Value, error) {
			taskCtx, releaseTask := detachExecutionLeaseCancellationLease(ctx)
			go func() {
				defer releaseTask()
				<-inspectAsync
				observations <- taskCtx.Err()
				<-taskCtx.Done()
				observations <- taskCtx.Err()
			}()
			return Int(23), nil
		}),
		WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
			if invocation.Name != "reenter_async" {
				return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
			}
			return runtimeInstance.Invoke(ctx, "start_detached_async")
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	runtimeInstance = created
	program, err := CompileString("nested-runtime-async.cna", `return reenter_async();`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	script, err := runtimeInstance.Load(ctx, program)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if got := script.Result().Int32(); got != 23 {
		cancel()
		t.Fatalf("nested Runtime.Invoke result = %d, want 23", got)
	}

	// Both the nested Runtime.Invoke lease and the outer script lease have left.
	// A runtime-owned async task must not inherit either private cancellation.
	close(inspectAsync)
	if err := awaitQuiescenceError(t, observations, "detached async lease check"); err != nil {
		cancel()
		t.Fatalf("detached async context ended with its creating callback: %v", err)
	}

	// Importer cancellation still propagates through the detached task context.
	cancel()
	if err := awaitQuiescenceError(t, observations, "detached async importer cancellation"); !errors.Is(err, context.Canceled) {
		t.Fatalf("detached async cancellation = %v, want context.Canceled", err)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNestedRuntimeInvokeDetachedAsyncPreservesHostDerivedCancellation(t *testing.T) {
	type publishedDetachedTask struct {
		context context.Context
		release func()
	}
	taskPublished := make(chan publishedDetachedTask, 1)
	var runtimeInstance *Runtime
	created, err := New(
		WithFunction("start_host_derived_async", func(ctx context.Context, _ Invocation) (Value, error) {
			taskContext, releaseTask := detachExecutionLeaseCancellationLease(ctx)
			taskPublished <- publishedDetachedTask{context: taskContext, release: releaseTask}
			return Int(29), nil
		}),
		WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
			if invocation.Name != "reenter_host_derived_async" {
				return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
			}
			shortCtx, cancel := context.WithCancel(ctx)
			value, invokeErr := runtimeInstance.Invoke(shortCtx, "start_host_derived_async")
			cancel()
			return value, invokeErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	runtimeInstance = created
	program, err := CompileString("host-derived-runtime-async.cna", `return reenter_host_derived_async();`)
	if err != nil {
		t.Fatal(err)
	}
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	if _, err := runtimeInstance.Load(baseCtx, program); err != nil {
		t.Fatal(err)
	}

	var task publishedDetachedTask
	select {
	case task = <-taskPublished:
	case <-time.After(quiescenceTestTimeout):
		t.Fatal("timed out waiting for detached task context")
	}
	defer task.release()
	select {
	case <-task.context.Done():
		if !errors.Is(task.context.Err(), context.Canceled) {
			t.Fatalf("detached task error = %v, want context.Canceled", task.context.Err())
		}
	case <-time.After(quiescenceTestTimeout):
		t.Fatal("Host-derived cancellation did not reach detached task")
	}
}

func TestCloseDeadlineDoesNotPublishClosedBeforeScriptLoaderChildStops(t *testing.T) {
	childEntered := make(chan struct{})
	childCanceled := make(chan struct{})
	releaseChild := make(chan struct{})
	childRuntimes := make(chan *Runtime, 1)
	runtimeInstance, err := New(WithFunction("embedded_block", func(ctx context.Context, invocation Invocation) (Value, error) {
		childRuntimes <- invocation.Runtime
		close(childEntered)
		<-ctx.Done()
		close(childCanceled)
		<-releaseChild
		return Null(), ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loader-terminal-close.cna", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "blocking-child.sl", 'return embedded_block();', $null];
sub run_child { return [$child runScript]; }
`)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	parent.mu.RLock()
	var loader *portableScriptLoader
	for candidate := range parent.scriptLoaders {
		loader = candidate
		break
	}
	parent.mu.RUnlock()
	if loader == nil {
		t.Fatal("parent did not retain its ScriptLoader")
	}
	loader.mu.Lock()
	if len(loader.loaded) != 1 {
		loader.mu.Unlock()
		t.Fatalf("loaded children = %d, want 1", len(loader.loaded))
	}
	loader.mu.Unlock()

	callResult := make(chan error, 1)
	go func() {
		_, callErr := parent.Call(context.Background(), "run_child")
		callResult <- callErr
	}()
	awaitQuiescenceSignal(t, childEntered, "blocking ScriptLoader child")
	childRuntime := <-childRuntimes
	if childRuntime == nil {
		t.Fatal("ScriptLoader child runtime was not published")
	}

	ctx, cancel := context.WithCancel(context.Background())
	closeResult := make(chan error, 1)
	go func() { closeResult <- runtimeInstance.Close(ctx) }()
	awaitQuiescenceSignal(t, childCanceled, "ScriptLoader child cancellation")
	cancel()
	if err := awaitQuiescenceError(t, closeResult, "deadline-aware parent Close"); !errors.Is(err, context.Canceled) {
		t.Fatalf("first Close error = %v, want context.Canceled", err)
	}
	runtimeInstance.mu.RLock()
	closedEarly := runtimeInstance.closed
	runtimeInstance.mu.RUnlock()
	if closedEarly {
		t.Fatal("parent runtime published closed=true while ScriptLoader child was still running")
	}

	close(releaseChild)
	if err := awaitQuiescenceError(t, callResult, "ScriptLoader child call"); !errors.Is(err, context.Canceled) {
		t.Fatalf("child call error = %v, want context.Canceled", err)
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatalf("waiting Close: %v", err)
	}
	runtimeInstance.mu.RLock()
	parentClosed := runtimeInstance.closed
	runtimeInstance.mu.RUnlock()
	childRuntime.mu.RLock()
	childClosed := childRuntime.closed
	childRuntime.mu.RUnlock()
	if !parentClosed || !childClosed {
		t.Fatalf("terminal close state = parent %v child %v, want both true", parentClosed, childClosed)
	}
}

func TestScriptSetSerializesActiveCheckAndWriteWithUnload(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("set-atomic.cna", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	cell := script.globals.global("$atomic")
	cell.mu.Lock()

	setResult := make(chan error, 1)
	go func() { setResult <- script.Set("$atomic", String("written")) }()
	deadline := time.After(quiescenceTestTimeout)
	for {
		if !script.mu.TryLock() {
			break
		}
		script.mu.Unlock()
		runtime.Gosched()
		select {
		case <-deadline:
			cell.mu.Unlock()
			t.Fatal("Script.Set did not hold Script.mu while blocked on the Cell write")
		default:
		}
	}

	unloadResult := make(chan error, 1)
	unloadStarted := make(chan struct{})
	go func() {
		close(unloadStarted)
		unloadResult <- script.Unload(context.Background())
	}()
	awaitQuiescenceSignal(t, unloadStarted, "Unload start")
	for index := 0; index < 32; index++ {
		runtime.Gosched()
	}
	assertQuiescencePending(t, unloadResult, "Unload")

	cell.mu.Unlock()
	if err := awaitQuiescenceError(t, setResult, "Script.Set"); err != nil {
		t.Fatalf("Script.Set: %v", err)
	}
	if err := awaitQuiescenceError(t, unloadResult, "Unload"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if got := script.Get("$atomic").String(); got != "written" {
		t.Fatalf("atomic global = %q, want written", got)
	}
	if err := script.Set("$atomic", String("too-late")); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("post-Unload Set = %v, want ErrScriptUnloaded", err)
	}
}

type blockingBindingObserver struct {
	registered       chan struct{}
	registerCanceled chan struct{}
	releaseRegister  chan struct{}
	unregistered     chan struct{}
	onceRegistered   sync.Once
	onceCanceled     sync.Once
	onceUnregistered sync.Once
}

func (observer *blockingBindingObserver) Registered(ctx context.Context, _ Binding) error {
	observer.onceRegistered.Do(func() { close(observer.registered) })
	<-ctx.Done()
	observer.onceCanceled.Do(func() { close(observer.registerCanceled) })
	<-observer.releaseRegister
	return nil
}

func (observer *blockingBindingObserver) Unregistered(context.Context, Binding) error {
	observer.onceUnregistered.Do(func() { close(observer.unregistered) })
	return nil
}

func TestBindingPublicationCannotLeakPastUnloadSnapshot(t *testing.T) {
	observer := &blockingBindingObserver{
		registered:       make(chan struct{}),
		registerCanceled: make(chan struct{}),
		releaseRegister:  make(chan struct{}),
		unregistered:     make(chan struct{}),
	}
	runtimeInstance, err := New(WithBindingObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("binding-race.cna", `on ready { return 1; }`)
	if err != nil {
		t.Fatal(err)
	}
	loadResult := make(chan error, 1)
	go func() {
		_, loadErr := runtimeInstance.Load(context.Background(), program)
		loadResult <- loadErr
	}()
	awaitQuiescenceSignal(t, observer.registered, "binding Registered observer")
	scripts := runtimeInstance.Scripts()
	if len(scripts) != 1 {
		t.Fatalf("published scripts = %d, want 1", len(scripts))
	}

	unloadResult := make(chan error, 1)
	go func() { unloadResult <- scripts[0].Unload(context.Background()) }()
	awaitQuiescenceSignal(t, observer.registerCanceled, "binding registration cancellation")
	if bindings := runtimeInstance.Bindings(BindingEvent, "ready"); len(bindings) != 0 {
		t.Fatalf("runtime bindings after unload request = %d, want 0", len(bindings))
	}
	assertQuiescencePending(t, unloadResult, "Unload")
	select {
	case <-observer.unregistered:
		t.Fatal("Unregistered ran before Registered returned")
	default:
	}

	close(observer.releaseRegister)
	if err := awaitQuiescenceError(t, loadResult, "binding Load"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load error = %v, want context.Canceled", err)
	}
	if err := awaitQuiescenceError(t, unloadResult, "binding Unload"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	awaitQuiescenceSignal(t, observer.unregistered, "binding Unregistered observer")
	if bindings := runtimeInstance.Bindings(BindingEvent, "ready"); len(bindings) != 0 {
		t.Fatalf("runtime bindings after unload completion = %d, want 0", len(bindings))
	}
}

func TestUnloadRemovesOnlyMatchingScriptLocalBindingID(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	firstProgram, err := CompileString("first-binding.cna", `on shared { return "first"; }`)
	if err != nil {
		t.Fatal(err)
	}
	secondProgram, err := CompileString("second-binding.cna", `on shared { return "second"; }`)
	if err != nil {
		t.Fatal(err)
	}
	first, err := runtimeInstance.Load(context.Background(), firstProgram)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtimeInstance.Load(context.Background(), secondProgram)
	if err != nil {
		t.Fatal(err)
	}
	firstBindings := first.Bindings()
	secondBindings := second.Bindings()
	if len(firstBindings) != 1 || len(secondBindings) != 1 || firstBindings[0].ID != secondBindings[0].ID {
		t.Fatalf("script-local binding IDs = %v and %v, want one identical ID per script", firstBindings, secondBindings)
	}

	if err := first.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	bindings := runtimeInstance.Bindings(BindingEvent, "shared")
	if len(bindings) != 1 || bindings[0].Script != second.ID() {
		t.Fatalf("bindings after first unload = %#v, want only script %d", bindings, second.ID())
	}
	values, err := runtimeInstance.DispatchEvent(context.Background(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].String() != "second" {
		t.Fatalf("shared dispatch = %v, want [second]", values)
	}
	if err := second.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCloseStartedByUnloadObserverWaitsForObserverCompletion(t *testing.T) {
	observerEntered := make(chan struct{})
	releaseObserver := make(chan struct{})
	var runtimeInstance *Runtime
	observer := ScriptLifecycleFuncs{
		Unloaded: func(ctx context.Context, _ *Script) error {
			if err := runtimeInstance.Close(ctx); err != nil {
				return fmt.Errorf("reentrant Close: %w", err)
			}
			close(observerEntered)
			<-releaseObserver
			return nil
		},
	}
	var err error
	runtimeInstance, err = New(WithScriptLifecycleObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("observer-close.cna", `$loaded = 1;`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	unloadResult := make(chan error, 1)
	go func() { unloadResult <- script.Unload(context.Background()) }()
	awaitQuiescenceSignal(t, observerEntered, "ScriptUnloaded observer Close")

	closeResult := make(chan error, 1)
	go func() { closeResult <- runtimeInstance.Close(context.Background()) }()
	select {
	case err := <-closeResult:
		t.Fatalf("Close completed before its initiating unload observer returned: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	runtimeInstance.mu.RLock()
	closedEarly := runtimeInstance.closed
	runtimeInstance.mu.RUnlock()
	if closedEarly {
		t.Fatal("runtime published closed before its initiating unload observer returned")
	}

	close(releaseObserver)
	if err := awaitQuiescenceError(t, unloadResult, "observer-initiated Unload"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if err := awaitQuiescenceError(t, closeResult, "observer-initiated Close"); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestConcurrentRuntimeInvokeWithInheritedContextHoldsCloseLease(t *testing.T) {
	childEntered := make(chan struct{})
	releaseChild := make(chan struct{})
	childResult := make(chan error, 1)
	var runtimeInstance *Runtime
	runtimeInstance, err := New(WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
		switch invocation.Name {
		case "outer_runtime_invoke":
			go func() {
				_, invokeErr := runtimeInstance.Invoke(ctx, "blocked_runtime_invoke")
				childResult <- invokeErr
			}()
			awaitQuiescenceSignal(t, childEntered, "nested Runtime.Invoke admission")
			return Null(), nil
		case "blocked_runtime_invoke":
			close(childEntered)
			<-releaseChild
			return Null(), nil
		default:
			return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
		}
	})))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "outer_runtime_invoke"); err != nil {
		t.Fatalf("outer Runtime.Invoke: %v", err)
	}

	closeResult := make(chan error, 1)
	go func() { closeResult <- runtimeInstance.Close(context.Background()) }()
	time.Sleep(25 * time.Millisecond)
	assertQuiescencePending(t, closeResult, "Close with inherited concurrent Runtime.Invoke")
	close(releaseChild)
	if err := awaitQuiescenceError(t, childResult, "nested Runtime.Invoke"); !errors.Is(err, context.Canceled) {
		t.Fatalf("nested Runtime.Invoke error = %v, want context.Canceled", err)
	}
	if err := awaitQuiescenceError(t, closeResult, "Close with nested Runtime.Invoke"); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestConcurrentScriptCallWithInheritedContextHoldsUnloadLease(t *testing.T) {
	childEntered := make(chan struct{})
	releaseChild := make(chan struct{})
	childResult := make(chan error, 1)
	var script *Script
	runtimeInstance, err := New(WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
		switch invocation.Name {
		case "outer_script_call":
			go func() {
				_, callErr := script.Call(ctx, "blocked_child")
				childResult <- callErr
			}()
			awaitQuiescenceSignal(t, childEntered, "nested Script.Call admission")
			return Null(), nil
		case "blocked_script_call":
			close(childEntered)
			<-releaseChild
			return Null(), nil
		default:
			return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
		}
	})))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("inherited-call.cna", `
sub outer { outer_script_call(); }
sub blocked_child { blocked_script_call(); }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := script.Call(context.Background(), "outer"); err != nil {
		t.Fatalf("outer Script.Call: %v", err)
	}

	unloadResult := make(chan error, 1)
	go func() { unloadResult <- script.Unload(context.Background()) }()
	time.Sleep(25 * time.Millisecond)
	assertQuiescencePending(t, unloadResult, "Unload with inherited concurrent Script.Call")
	close(releaseChild)
	if err := awaitQuiescenceError(t, childResult, "nested Script.Call"); !errors.Is(err, context.Canceled) {
		t.Fatalf("nested Script.Call error = %v, want context.Canceled", err)
	}
	if err := awaitQuiescenceError(t, unloadResult, "Unload with nested Script.Call"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestStaleScriptExecutionContextCannotMasqueradeAsReentrantClose(t *testing.T) {
	var captured context.Context
	runtimeInstance, err := New(WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
		if invocation.Name != "capture_execution_context" {
			return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name}
		}
		captured = ctx
		return Null(), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("stale-context.cna", `capture_execution_context();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if captured == nil {
		t.Fatal("Host did not capture its execution context")
	}

	closeContext, cancel := context.WithTimeout(context.WithoutCancel(captured), quiescenceTestTimeout)
	defer cancel()
	if err := runtimeInstance.Close(closeContext); err != nil {
		t.Fatalf("Close with stale execution context: %v", err)
	}
}
