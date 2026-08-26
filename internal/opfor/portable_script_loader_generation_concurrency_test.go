package opfor

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const portableScriptLoaderGenerationConcurrencyTimeout = 2 * time.Second

type portableScriptLoaderGenerationConcurrencyHost struct {
	mu       sync.Mutex
	captured []Invocation
	call     func(context.Context, Invocation) (Value, error)
}

func (host *portableScriptLoaderGenerationConcurrencyHost) Call(
	ctx context.Context,
	invocation Invocation,
) (Value, error) {
	if invocation.Name == "capture_generation" {
		invocation.Arguments = append([]Argument(nil), invocation.Arguments...)
		host.mu.Lock()
		host.captured = append(host.captured, invocation)
		host.mu.Unlock()
		return Null(), nil
	}
	if host.call != nil {
		return host.call(ctx, invocation)
	}
	return Null(), &UnsupportedError{
		Operation: "host function",
		Name:      invocation.Name,
		Span:      invocation.Span,
	}
}

func (host *portableScriptLoaderGenerationConcurrencyHost) invocation(t *testing.T) Invocation {
	t.Helper()
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.captured) != 1 {
		t.Fatalf("captured invocations = %d, want 1", len(host.captured))
	}
	invocation := host.captured[0]
	invocation.Arguments = append([]Argument(nil), invocation.Arguments...)
	return invocation
}

func loadPortableScriptLoaderGenerationConcurrencyHarness(
	t *testing.T,
	host *portableScriptLoaderGenerationConcurrencyHost,
	childSource string,
	options ...Option,
) (*Runtime, *Script, Invocation, Callable) {
	t.Helper()
	if _, err := CompileString("generation-concurrency-child.cna", childSource); err != nil {
		t.Fatalf("compile generation child: %v", err)
	}
	options = append([]Option{WithHost(host)}, options...)
	runtimeInstance, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	parentSource := fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "generation-concurrency-child.cna", base64_decode(%q), $null];
sub run_child { return [$child runScript]; }
sub unload_child { [$loader unloadScript: $child]; return 1; }
sub child_loaded { return [$child isLoaded]; }
`, base64.StdEncoding.EncodeToString([]byte(childSource)))
	program, err := CompileString("generation-concurrency-parent.cna", parentSource)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Call(context.Background(), "run_child"); err != nil {
		t.Fatalf("run child: %v", err)
	}
	invocation := host.invocation(t)
	callback, err := invocation.Callback(0)
	if err != nil {
		t.Fatalf("retain child callback: %v", err)
	}
	return runtimeInstance, parent, invocation, callback
}

func awaitPortableScriptLoaderGenerationSignal(
	t *testing.T,
	signal <-chan struct{},
	operation string,
) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(portableScriptLoaderGenerationConcurrencyTimeout):
		t.Fatalf("timed out waiting for %s", operation)
	}
}

func awaitPortableScriptLoaderGenerationError(
	t *testing.T,
	result <-chan error,
	operation string,
) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(portableScriptLoaderGenerationConcurrencyTimeout):
		t.Fatalf("timed out waiting for %s", operation)
		return nil
	}
}

func awaitPortableScriptLoaderGenerationRetirement(t *testing.T, invocation Invocation) {
	t.Helper()
	generation := invocation.generationToken()
	if generation == nil || generation.script == nil {
		t.Fatal("captured invocation has no script generation")
	}
	deadline := time.Now().Add(portableScriptLoaderGenerationConcurrencyTimeout)
	for {
		generation.script.mu.RLock()
		retiring := generation.retiring
		generation.script.mu.RUnlock()
		if retiring {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for script generation retirement")
		}
		time.Sleep(time.Millisecond)
	}
}

func assertPortableScriptLoaderGenerationPending(
	t *testing.T,
	result <-chan error,
	operation string,
) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("%s completed before generation cleanup drained: %v", operation, err)
	default:
	}
}

func TestPortableScriptLoaderGenerationUnloadWaitsForAdmittedRetainedCallback(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	var releaseOnce sync.Once
	host := &portableScriptLoaderGenerationConcurrencyHost{
		call: func(_ context.Context, invocation Invocation) (Value, error) {
			if invocation.Name != "block_generation" {
				return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name, Span: invocation.Span}
			}
			enteredOnce.Do(func() { close(entered) })
			<-release
			return Int(1), nil
		},
	}
	_, parent, invocation, callback := loadPortableScriptLoaderGenerationConcurrencyHarness(t, host, `
capture_generation(lambda({ block_generation(); return 7; }));
return 1;
`)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	callbackDone := make(chan error, 1)
	go func() {
		_, err := callback.Invoke(context.Background())
		callbackDone <- err
	}()
	awaitPortableScriptLoaderGenerationSignal(t, entered, "retained callback admission")

	unloadDone := make(chan error, 1)
	go func() {
		_, err := parent.Call(context.Background(), "unload_child")
		unloadDone <- err
	}()
	awaitPortableScriptLoaderGenerationRetirement(t, invocation)
	assertPortableScriptLoaderGenerationPending(t, unloadDone, "explicit ScriptLoader unload")

	releaseOnce.Do(func() { close(release) })
	if err := awaitPortableScriptLoaderGenerationError(t, callbackDone, "retained callback completion"); err != nil &&
		!errors.Is(err, ErrScriptUnloaded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("retained callback error = %v", err)
	}
	if err := awaitPortableScriptLoaderGenerationError(t, unloadDone, "explicit ScriptLoader unload"); err != nil {
		t.Fatalf("explicit ScriptLoader unload: %v", err)
	}
	if _, err := callback.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("retained callback after unload error = %v, want ErrScriptUnloaded", err)
	}
}

func TestPortableScriptLoaderConcurrentGenerationUnloadsJoinCleanup(t *testing.T) {
	unloadedEntered := make(chan struct{})
	releaseUnload := make(chan struct{})
	var releaseOnce sync.Once
	var unloadCalls atomic.Int32
	bridge := LoadableBridgeFuncs{
		Unloaded: func(context.Context, *Script) error {
			if unloadCalls.Add(1) == 1 {
				close(unloadedEntered)
			}
			<-releaseUnload
			return nil
		},
	}
	provider := LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
		if request.ClassName != "generation.ConcurrentBridge" {
			return nil, &UnsupportedError{Operation: "Loadable", Name: request.ClassName, Span: request.Span}
		}
		return bridge, nil
	})
	host := &portableScriptLoaderGenerationConcurrencyHost{}
	_, parent, _, callback := loadPortableScriptLoaderGenerationConcurrencyHarness(t, host, `
use("generation.ConcurrentBridge");
capture_generation(lambda({ return 1; }));
return 1;
`, WithLoadableProvider(provider))
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseUnload) }) })

	firstDone := make(chan error, 1)
	go func() {
		_, err := parent.Call(context.Background(), "unload_child")
		firstDone <- err
	}()
	awaitPortableScriptLoaderGenerationSignal(t, unloadedEntered, "first generation cleanup callback")
	assertPortableScriptLoaderGenerationPending(t, firstDone, "first ScriptLoader unload")

	joinCtx, cancelJoin := context.WithTimeout(context.Background(), 25*time.Millisecond)
	_, joinErr := parent.Call(joinCtx, "unload_child")
	cancelJoin()
	if !errors.Is(joinErr, context.DeadlineExceeded) {
		releaseOnce.Do(func() { close(releaseUnload) })
		t.Fatalf("concurrent ScriptLoader unload error = %v, want deadline exceeded while joining cleanup", joinErr)
	}
	if got := unloadCalls.Load(); got != 1 {
		releaseOnce.Do(func() { close(releaseUnload) })
		t.Fatalf("generation cleanup callbacks = %d, want 1", got)
	}
	assertPortableScriptLoaderGenerationPending(t, firstDone, "first ScriptLoader unload")

	releaseOnce.Do(func() { close(releaseUnload) })
	if err := awaitPortableScriptLoaderGenerationError(t, firstDone, "joined generation cleanup"); err != nil {
		t.Fatalf("first ScriptLoader unload: %v", err)
	}
	if got := unloadCalls.Load(); got != 1 {
		t.Fatalf("generation cleanup callbacks after release = %d, want 1", got)
	}
	if _, err := callback.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("retained callback after joined cleanup error = %v, want ErrScriptUnloaded", err)
	}
}

func TestPortableScriptLoaderReentrantGenerationUnloadDoesNotDeadlock(t *testing.T) {
	unloaded := make(chan struct{})
	var unloadOnce sync.Once
	bridge := LoadableBridgeFuncs{
		Unloaded: func(context.Context, *Script) error {
			unloadOnce.Do(func() { close(unloaded) })
			return nil
		},
	}
	provider := LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
		if request.ClassName != "generation.ReentrantBridge" {
			return nil, &UnsupportedError{Operation: "Loadable", Name: request.ClassName, Span: request.Span}
		}
		return bridge, nil
	})
	var parent *Script
	host := &portableScriptLoaderGenerationConcurrencyHost{}
	host.call = func(ctx context.Context, invocation Invocation) (Value, error) {
		if invocation.Name != "reentrant_unload" {
			return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name, Span: invocation.Span}
		}
		if parent == nil {
			return Null(), errors.New("test parent script is not initialized")
		}
		_, err := parent.Call(ctx, "unload_child")
		return Null(), err
	}
	_, parent, _, callback := loadPortableScriptLoaderGenerationConcurrencyHarness(t, host, `
use("generation.ReentrantBridge");
capture_generation(lambda({ reentrant_unload(); return 9; }));
return 1;
`, WithLoadableProvider(provider))

	callbackDone := make(chan error, 1)
	go func() {
		_, err := callback.Invoke(context.Background())
		callbackDone <- err
	}()
	if err := awaitPortableScriptLoaderGenerationError(t, callbackDone, "reentrant ScriptLoader unload"); err != nil &&
		!errors.Is(err, ErrScriptUnloaded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("reentrant retained callback error = %v", err)
	}
	awaitPortableScriptLoaderGenerationSignal(t, unloaded, "reentrant generation cleanup")
	if _, err := callback.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("reentrant callback after unload error = %v, want ErrScriptUnloaded", err)
	}
	loaded, err := parent.Call(context.Background(), "child_loaded")
	if err != nil {
		t.Fatalf("query child loaded state: %v", err)
	}
	if loaded.Truth() {
		t.Fatal("reentrant unload left ScriptInstance loaded")
	}
}
