package opfor

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAggressorUISynchronousUnloadKeepsImporterCancellation(t *testing.T) {
	var owner *Script
	unloadRequested := make(chan error, 1)
	cleanupEntered := make(chan struct{})
	cleanupInitialErr := make(chan error, 1)
	cleanupResult := make(chan error, 1)
	callback := FunctionValue(aggressorUITestCallable(func(ctx context.Context, _ ...Value) (Value, error) {
		err := owner.Unload(ctx)
		unloadRequested <- err
		return Null(), err
	}))
	provider := AggressorPromptProviderFunc(func(ctx context.Context, _ AggressorPromptPresentation, responder AggressorPromptResponder) error {
		_, err := responder.Accept(ctx)
		return err
	})
	runtimeInstance, err := New(
		WithInitialGlobals(map[string]Value{"ui_callback": callback}),
		WithAggressorPromptProvider(provider),
		WithScriptLifecycleObserver(ScriptLifecycleFuncs{Unloaded: func(ctx context.Context, _ *Script) error {
			close(cleanupEntered)
			cleanupInitialErr <- ctx.Err()
			<-ctx.Done()
			err := ctx.Err()
			cleanupResult <- err
			return err
		}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("ui-callback-caller-cancellation.cna", `
sub run_ui {
    prompt_confirm("Unload?", "Caller cancellation", $ui_callback);
}
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	callerContext, cancelCaller := context.WithCancel(context.Background())
	defer cancelCaller()
	callResult := make(chan error, 1)
	go func() {
		_, callErr := owner.Call(callerContext, "run_ui")
		callResult <- callErr
	}()
	if err := awaitQuiescenceError(t, unloadRequested, "synchronous callback unload request"); err != nil {
		t.Fatalf("reentrant Unload: %v", err)
	}
	awaitQuiescenceSignal(t, cleanupEntered, "ScriptUnloaded observer")
	if err := awaitQuiescenceError(t, cleanupInitialErr, "ScriptUnloaded initial context"); err != nil {
		t.Fatalf("ScriptUnloaded context was internally canceled: %v", err)
	}
	assertQuiescencePending(t, cleanupResult, "cleanup before importer cancellation")
	assertQuiescencePending(t, callResult, "outer call before importer cancellation")

	cancelCaller()
	if err := awaitQuiescenceError(t, cleanupResult, "cleanup importer cancellation"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ScriptUnloaded context error = %v, want context.Canceled", err)
	}
	if err := awaitQuiescenceError(t, callResult, "outer call importer cancellation"); !errors.Is(err, context.Canceled) {
		t.Fatalf("outer call error = %v, want context.Canceled", err)
	}
}

func TestAggressorUISynchronousCleanupUsesBackgroundResponderContext(t *testing.T) {
	var owner *Script
	unloadRequested := make(chan error, 1)
	cleanupEntered := make(chan struct{})
	checkCleanup := make(chan struct{})
	cleanupContextErr := make(chan error, 1)
	releaseCleanup := make(chan struct{})
	callback := FunctionValue(aggressorUITestCallable(func(ctx context.Context, _ ...Value) (Value, error) {
		err := owner.Unload(ctx)
		unloadRequested <- err
		return Null(), err
	}))
	provider := AggressorPromptProviderFunc(func(_ context.Context, _ AggressorPromptPresentation, responder AggressorPromptResponder) error {
		_, err := responder.Accept(context.Background())
		return err
	})
	runtimeInstance, err := New(
		WithInitialGlobals(map[string]Value{"ui_callback": callback}),
		WithAggressorPromptProvider(provider),
		WithScriptLifecycleObserver(ScriptLifecycleFuncs{Unloaded: func(ctx context.Context, _ *Script) error {
			close(cleanupEntered)
			<-checkCleanup
			cleanupContextErr <- ctx.Err()
			<-releaseCleanup
			return nil
		}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("ui-background-response-cancellation.cna", `
sub run_ui {
    prompt_confirm("Unload?", "Background response", $ui_callback);
}
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	callerContext, cancelCaller := context.WithCancel(context.Background())
	defer cancelCaller()
	callResult := make(chan error, 1)
	go func() {
		_, callErr := owner.Call(callerContext, "run_ui")
		callResult <- callErr
	}()
	if err := awaitQuiescenceError(t, unloadRequested, "background responder unload request"); err != nil {
		t.Fatalf("reentrant Unload: %v", err)
	}
	awaitQuiescenceSignal(t, cleanupEntered, "background responder cleanup")
	cancelCaller()
	close(checkCleanup)
	if err := awaitQuiescenceError(t, cleanupContextErr, "background responder cleanup context"); err != nil {
		t.Fatalf("cleanup inherited presentation cancellation = %v, want nil", err)
	}
	close(releaseCleanup)
	if err := awaitQuiescenceError(t, callResult, "background responder outer call"); !errors.Is(err, context.Canceled) {
		t.Fatalf("outer call error = %v, want context.Canceled", err)
	}
}

func TestAggressorUISynchronousCleanupUsesDerivedResponderCancellation(t *testing.T) {
	var owner *Script
	unloadRequested := make(chan error, 1)
	cleanupEntered := make(chan struct{})
	cleanupInitialErr := make(chan error, 1)
	cleanupResult := make(chan error, 1)
	responseCancel := make(chan context.CancelFunc, 1)
	callback := FunctionValue(aggressorUITestCallable(func(ctx context.Context, _ ...Value) (Value, error) {
		err := owner.Unload(ctx)
		unloadRequested <- err
		return Null(), err
	}))
	provider := AggressorPromptProviderFunc(func(_ context.Context, _ AggressorPromptPresentation, responder AggressorPromptResponder) error {
		responseContext, cancel := context.WithCancel(context.Background())
		responseCancel <- cancel
		_, err := responder.Accept(responseContext)
		return err
	})
	runtimeInstance, err := New(
		WithInitialGlobals(map[string]Value{"ui_callback": callback}),
		WithAggressorPromptProvider(provider),
		WithScriptLifecycleObserver(ScriptLifecycleFuncs{Unloaded: func(ctx context.Context, _ *Script) error {
			close(cleanupEntered)
			cleanupInitialErr <- ctx.Err()
			<-ctx.Done()
			err := ctx.Err()
			cleanupResult <- err
			return err
		}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("ui-derived-response-cancellation.cna", `
sub run_ui {
    prompt_confirm("Unload?", "Derived response", $ui_callback);
}
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	callResult := make(chan error, 1)
	go func() {
		_, callErr := owner.Call(context.Background(), "run_ui")
		callResult <- callErr
	}()
	if err := awaitQuiescenceError(t, unloadRequested, "derived responder unload request"); err != nil {
		t.Fatalf("reentrant Unload: %v", err)
	}
	awaitQuiescenceSignal(t, cleanupEntered, "derived responder cleanup")
	if err := awaitQuiescenceError(t, cleanupInitialErr, "derived responder initial cleanup context"); err != nil {
		t.Fatalf("cleanup context was internally canceled: %v", err)
	}
	cancelResponse := <-responseCancel
	cancelResponse()
	if err := awaitQuiescenceError(t, cleanupResult, "derived responder cleanup cancellation"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup context error = %v, want context.Canceled", err)
	}
	if err := awaitQuiescenceError(t, callResult, "derived responder outer call"); !errors.Is(err, context.Canceled) {
		t.Fatalf("outer call error = %v, want context.Canceled", err)
	}
}

func TestAggressorUIProviderErrorDrainsEnrolledPromptCallback(t *testing.T) {
	providerErr := errors.New("provider failed after answering")
	callbackEntered := make(chan struct{})
	releaseCallback := make(chan struct{})
	acceptResult := make(chan error, 1)
	invocationResult := make(chan error, 1)
	var callbackCalls atomic.Int32

	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
		callbackCalls.Add(1)
		close(callbackEntered)
		<-releaseCallback
		return String("accepted"), nil
	}))
	provider := &aggressorUITestPromptProvider{present: func(
		ctx context.Context,
		_ AggressorPromptPresentation,
		responder AggressorPromptResponder,
	) error {
		go func() {
			_, err := responder.Accept(ctx, String("answer"))
			acceptResult <- err
		}()
		<-callbackEntered
		return providerErr
	}}
	runtimeInstance, err := New(WithAggressorPromptProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)

	go func() {
		_, invokeErr := aggressorUITestInvoke(
			context.Background(), runtimeInstance, owner, "prompt_text", Span{},
			String("question"), String("default"), callback,
		)
		invocationResult <- invokeErr
	}()
	awaitQuiescenceSignal(t, callbackEntered, "enrolled prompt callback")
	assertQuiescencePending(t, invocationResult, "provider boundary draining its callback")
	close(releaseCallback)
	if err := awaitQuiescenceError(t, acceptResult, "enrolled prompt Accept"); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := awaitQuiescenceError(t, invocationResult, "provider error after callback"); !errors.Is(err, providerErr) {
		t.Fatalf("prompt_text error = %v, want provider error", err)
	}
	if callbackCalls.Load() != 1 {
		t.Fatalf("callback calls = %d, want one", callbackCalls.Load())
	}
	call := provider.snapshot()[0]
	assertAggressorUITestDoneClosed(t, call.responder.Done())
	if result, err := call.responder.Accept(context.Background(), String("late")); !errors.Is(err, ErrAggressorUIClosed) || !result.IsNull() || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("late Accept = (%s, %v), want null closed error", result.Describe(), err)
	}
}

func TestAggressorUITerminalPromptStateWinsRevocationRace(t *testing.T) {
	provider := &aggressorUITestPromptProvider{}
	runtimeInstance, err := New(WithAggressorPromptProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
		return String("accepted"), nil
	}))
	if _, err := aggressorUITestInvoke(
		context.Background(), runtimeInstance, owner, "prompt_text", Span{},
		String("question"), String("default"), callback,
	); err != nil {
		t.Fatal(err)
	}
	call := provider.snapshot()[0]
	prompt, ok := call.responder.(*aggressorPrompt)
	if !ok {
		t.Fatalf("responder type = %T, want *aggressorPrompt", call.responder)
	}

	// Hold Script.mu so Accept has terminalized the prompt but cannot yet remove
	// it from the script's resource registry. This deterministically recreates
	// unload's snapshot/revoke window without relying on scheduler timing.
	owner.mu.Lock()
	ownerLocked := true
	defer func() {
		if ownerLocked {
			owner.mu.Unlock()
		}
	}()
	acceptResult := make(chan error, 1)
	go func() {
		_, acceptErr := call.responder.Accept(context.Background(), String("answer"))
		acceptResult <- acceptErr
	}()
	awaitQuiescenceSignal(t, call.responder.Done(), "terminal prompt state")
	prompt.revokeAggressorUI()
	prompt.mu.Lock()
	state := prompt.state
	prompt.mu.Unlock()
	owner.mu.Unlock()
	ownerLocked = false

	if state != aggressorUICompleted {
		t.Fatalf("state after losing revoke = %d, want completed", state)
	}
	if err := awaitQuiescenceError(t, acceptResult, "terminal prompt Accept"); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if result, err := call.responder.Accept(context.Background(), String("late")); !errors.Is(err, ErrAggressorUIClosed) || !result.IsNull() || errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("late Accept = (%s, %v), want non-revoked closed error", result.Describe(), err)
	}
}

func TestAggressorUITerminalDialogStateWinsRevocationRace(t *testing.T) {
	provider := &aggressorUITestDialogProvider{}
	runtimeInstance, err := New(WithAggressorDialogProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
		return Null(), nil
	}))
	dialogValue, err := aggressorUITestInvoke(
		context.Background(), runtimeInstance, owner, "dialog", Span{},
		String("dialog"), HashValue(NewHash()), callback,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog_show", Span{}, dialogValue); err != nil {
		t.Fatal(err)
	}
	call := provider.snapshot()[0]
	dialog, ok := call.responder.(*aggressorDialog)
	if !ok {
		t.Fatalf("responder type = %T, want *aggressorDialog", call.responder)
	}

	owner.mu.Lock()
	ownerLocked := true
	defer func() {
		if ownerLocked {
			owner.mu.Unlock()
		}
	}()
	dismissResult := make(chan error, 1)
	go func() { dismissResult <- call.responder.Dismiss() }()
	awaitQuiescenceSignal(t, call.responder.Done(), "terminal dialog state")
	dialog.revokeAggressorUI()
	dialog.mu.Lock()
	state := dialog.state
	dialog.mu.Unlock()
	owner.mu.Unlock()
	ownerLocked = false

	if state != aggressorUIDismissed {
		t.Fatalf("state after losing revoke = %d, want dismissed", state)
	}
	if err := awaitQuiescenceError(t, dismissResult, "terminal dialog Dismiss"); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if err := call.responder.Dismiss(); !errors.Is(err, ErrAggressorUIClosed) || errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("late Dismiss error = %v, want non-revoked closed error", err)
	}
}
