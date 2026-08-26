package opfor

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type portableScriptLoaderForkGenerationHost struct {
	mu       sync.Mutex
	captured map[string]Invocation
	ready    map[string]chan struct{}

	registrationEntered chan struct{}
	releaseRegistration chan struct{}
	callbackEntered     chan struct{}
	releaseCallback     chan struct{}

	registrationOnce sync.Once
	callbackOnce     sync.Once
}

func newPortableScriptLoaderForkGenerationHost() *portableScriptLoaderForkGenerationHost {
	return &portableScriptLoaderForkGenerationHost{
		captured:            make(map[string]Invocation),
		ready:               make(map[string]chan struct{}),
		registrationEntered: make(chan struct{}),
		releaseRegistration: make(chan struct{}),
		callbackEntered:     make(chan struct{}),
		releaseCallback:     make(chan struct{}),
	}
}

func (host *portableScriptLoaderForkGenerationHost) Call(
	ctx context.Context,
	invocation Invocation,
) (Value, error) {
	switch invocation.Name {
	case "capture_generation":
		label := invocation.Arg(0).String()
		invocation.Arguments = append([]Argument(nil), invocation.Arguments...)
		host.mu.Lock()
		host.captured[label] = invocation
		ready := host.ready[label]
		if ready == nil {
			ready = make(chan struct{})
			host.ready[label] = ready
		}
		if !channelClosed(ready) {
			close(ready)
		}
		host.mu.Unlock()
		return Null(), nil
	case "fork_registration_gate":
		host.registrationOnce.Do(func() { close(host.registrationEntered) })
		select {
		case <-host.releaseRegistration:
			return Null(), nil
		case <-ctx.Done():
			return Null(), ctx.Err()
		}
	case "fork_callback_gate":
		host.callbackOnce.Do(func() { close(host.callbackEntered) })
		select {
		case <-host.releaseCallback:
			return Int(1), nil
		case <-ctx.Done():
			return Null(), ctx.Err()
		}
	default:
		return Null(), &UnsupportedError{
			Operation: "host function",
			Name:      invocation.Name,
			Span:      invocation.Span,
		}
	}
}

func (host *portableScriptLoaderForkGenerationHost) waitInvocation(
	t *testing.T,
	label string,
) Invocation {
	t.Helper()
	host.mu.Lock()
	ready := host.ready[label]
	if ready == nil {
		ready = make(chan struct{})
		host.ready[label] = ready
	}
	host.mu.Unlock()
	awaitPortableScriptLoaderGenerationSignal(t, ready, label+" generation capture")
	host.mu.Lock()
	invocation := host.captured[label]
	host.mu.Unlock()
	invocation.Arguments = append([]Argument(nil), invocation.Arguments...)
	return invocation
}

type portableScriptLoaderForkGenerationBindings struct {
	mu           sync.Mutex
	registered   []Binding
	unregistered []Binding
	ready        chan struct{}
	once         sync.Once
}

func (observer *portableScriptLoaderForkGenerationBindings) Registered(
	_ context.Context,
	binding Binding,
) error {
	if binding.Name != "fork_generation_event" {
		return nil
	}
	observer.mu.Lock()
	observer.registered = append(observer.registered, cloneBinding(binding))
	observer.mu.Unlock()
	observer.once.Do(func() { close(observer.ready) })
	return nil
}

func (observer *portableScriptLoaderForkGenerationBindings) Unregistered(
	_ context.Context,
	binding Binding,
) error {
	if binding.Name != "fork_generation_event" {
		return nil
	}
	observer.mu.Lock()
	observer.unregistered = append(observer.unregistered, cloneBinding(binding))
	observer.mu.Unlock()
	return nil
}

func (observer *portableScriptLoaderForkGenerationBindings) snapshot() ([]Binding, []Binding) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	registered := make([]Binding, len(observer.registered))
	for index, binding := range observer.registered {
		registered[index] = cloneBinding(binding)
	}
	unregistered := make([]Binding, len(observer.unregistered))
	for index, binding := range observer.unregistered {
		unregistered[index] = cloneBinding(binding)
	}
	return registered, unregistered
}

func TestPortableScriptLoaderForkCapabilitiesKeepIndependentLifetime(t *testing.T) {
	host := newPortableScriptLoaderForkGenerationHost()
	bindings := &portableScriptLoaderForkGenerationBindings{ready: make(chan struct{})}
	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	nativeInvocations := make(chan Invocation, 1)
	var cleanupOnce sync.Once
	var releaseRegistrationOnce sync.Once
	var releaseCallbackOnce sync.Once
	var releaseCleanupOnce sync.Once

	bridge := LoadableBridgeFuncs{
		Loaded: func(_ context.Context, script *Script) error {
			return script.RegisterFunction("fork_bridge_value", func(_ context.Context, invocation Invocation) (Value, error) {
				nativeInvocations <- invocation
				return Int(42), nil
			})
		},
		Unloaded: func(context.Context, *Script) error {
			cleanupOnce.Do(func() { close(cleanupEntered) })
			<-releaseCleanup
			return nil
		},
	}
	provider := LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
		if request.ClassName != "generation.ForkBridge" {
			return nil, &UnsupportedError{Operation: "Loadable", Name: request.ClassName, Span: request.Span}
		}
		return bridge, nil
	})
	runtimeInstance, err := New(
		WithHost(host),
		WithBindingObserver(bindings),
		WithLoadableProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	// Register channel release after Runtime.Close so LIFO cleanup unblocks all
	// deliberately parked provider calls before terminal hierarchy teardown.
	t.Cleanup(func() {
		releaseRegistrationOnce.Do(func() { close(host.releaseRegistration) })
		releaseCallbackOnce.Do(func() { close(host.releaseCallback) })
		releaseCleanupOnce.Do(func() { close(releaseCleanup) })
	})

	childSource := `
use("generation.ForkBridge");
capture_generation("direct", lambda({ return 11; }));
$handle = fork({
    fork_registration_gate();
    on("fork_generation_event", lambda({ return 41; }));
    capture_generation("fork", lambda({ fork_callback_gate(); return fork_bridge_value(); }));
    return 73;
});
return $handle;
`
	if _, err := runtimeInstance.CompileString("fork-generation-child.cna", childSource); err != nil {
		t.Fatalf("compile fork-generation child: %v", err)
	}
	parentSource := fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "fork-generation-child.cna", base64_decode(%q), $null];
sub run_child { $last_fork = [$child runScript]; return 1; }
sub unload_child { [$loader unloadScript: $child]; return 1; }
sub wait_fork { return wait($last_fork, 1000); }
`, base64.StdEncoding.EncodeToString([]byte(childSource)))
	program, err := CompileString("fork-generation-parent.cna", parentSource)
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
	select {
	case <-host.registrationEntered:
	case <-time.After(portableScriptLoaderGenerationConcurrencyTimeout):
		result, waitErr := parent.Call(context.Background(), "wait_fork")
		t.Fatalf("fork never entered registration gate; wait result = (%s, %v)", result.Describe(), waitErr)
	}
	directInvocation := host.waitInvocation(t, "direct")
	directCallback, err := directInvocation.Callback(1)
	if err != nil {
		t.Fatalf("retain direct-generation callback: %v", err)
	}

	unloadDone := make(chan error, 1)
	go func() {
		_, unloadErr := parent.Call(context.Background(), "unload_child")
		unloadDone <- unloadErr
	}()
	awaitPortableScriptLoaderGenerationSignal(t, cleanupEntered, "direct-generation cleanup")
	assertPortableScriptLoaderGenerationPending(t, unloadDone, "logical unload")
	if _, callErr := directCallback.Invoke(context.Background()); !errors.Is(callErr, ErrScriptUnloaded) {
		t.Fatalf("direct callback during cleanup error = %v, want ErrScriptUnloaded", callErr)
	}

	// Official Sleep gives the fork a distinct loaded ScriptInstance. Let that
	// child register while the loader-owned instance is already retiring.
	releaseRegistrationOnce.Do(func() { close(host.releaseRegistration) })
	forkInvocation := host.waitInvocation(t, "fork")
	awaitPortableScriptLoaderGenerationSignal(t, bindings.ready, "fork binding registration")
	if forkInvocation.Script == 0 || forkInvocation.Script == directInvocation.Script {
		t.Fatalf("fork Script ID = %d, direct Script ID = %d", forkInvocation.Script, directInvocation.Script)
	}
	forkCallback, err := forkInvocation.Callback(1)
	if err != nil {
		t.Fatalf("retain fork-generation callback: %v", err)
	}

	callbackDone := make(chan struct {
		value Value
		err   error
	}, 1)
	go func() {
		value, callErr := forkCallback.Invoke(context.Background())
		callbackDone <- struct {
			value Value
			err   error
		}{value: value, err: callErr}
	}()
	awaitPortableScriptLoaderGenerationSignal(t, host.callbackEntered, "fork callback admission")

	// Releasing direct-generation cleanup must not wait for, cancel, or revoke
	// work admitted by the independently owned fork Script.
	releaseCleanupOnce.Do(func() { close(releaseCleanup) })
	if unloadErr := awaitPortableScriptLoaderGenerationError(t, unloadDone, "logical unload beside fork callback"); unloadErr != nil {
		t.Fatalf("logical unload: %v", unloadErr)
	}
	select {
	case result := <-callbackDone:
		t.Fatalf("fork callback completed before its own release: (%s, %v)", result.value.Describe(), result.err)
	case <-time.After(25 * time.Millisecond):
	}
	releaseCallbackOnce.Do(func() { close(host.releaseCallback) })
	select {
	case result := <-callbackDone:
		if result.err != nil || result.value.Int32() != 42 {
			t.Fatalf("fork callback after parent logical unload = (%s, %v), want 42/nil", result.value.Describe(), result.err)
		}
	case <-time.After(portableScriptLoaderGenerationConcurrencyTimeout):
		t.Fatal("timed out waiting for fork callback completion")
	}
	select {
	case invocation := <-nativeInvocations:
		if invocation.Script != forkInvocation.Script {
			t.Fatalf("cloned native Script ID = %d, want fork Script ID %d", invocation.Script, forkInvocation.Script)
		}
	case <-time.After(portableScriptLoaderGenerationConcurrencyTimeout):
		t.Fatal("timed out waiting for cloned fork-native invocation")
	}

	results, err := forkInvocation.Bindings().DispatchEvent(context.Background(), "fork_generation_event")
	if err != nil || len(results) != 1 || results[0].Int32() != 41 {
		t.Fatalf("fork event after parent logical unload = (%v, %v), want [41]/nil", results, err)
	}
	if result, err := parent.Call(context.Background(), "wait_fork"); err != nil || result.Int32() != 73 {
		t.Fatalf("fork result after parent logical unload = (%s, %v), want 73/nil", result.Describe(), err)
	}
	registered, unregistered := bindings.snapshot()
	if len(registered) != 1 || registered[0].Script != forkInvocation.Script || len(unregistered) != 0 {
		t.Fatalf("fork binding lifecycle before terminal unload = registered %#v, unregistered %#v", registered, unregistered)
	}

	if err := parent.Unload(context.Background()); err != nil {
		t.Fatalf("terminal parent unload: %v", err)
	}
	if _, callErr := forkCallback.Invoke(context.Background()); !errors.Is(callErr, ErrScriptUnloaded) {
		t.Fatalf("fork callback after terminal hierarchy unload error = %v, want ErrScriptUnloaded", callErr)
	}
	_, unregistered = bindings.snapshot()
	if len(unregistered) != 1 || unregistered[0].ID != registered[0].ID {
		t.Fatalf("fork binding terminal cleanup = %#v, want binding %d", unregistered, registered[0].ID)
	}
}

func TestPortableScriptLoaderForkDropsParentBindingAncestry(t *testing.T) {
	host := newPortableScriptLoaderForkGenerationHost()
	bindings := &portableScriptLoaderForkGenerationBindings{ready: make(chan struct{})}
	close(host.releaseRegistration)
	close(host.releaseCallback)
	runtimeInstance, err := New(WithHost(host), WithBindingObserver(bindings))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	childSource := `
capture_generation("direct", lambda({ return 11; }));
on("launch_fork_generation", lambda({
    return fork({
        on("fork_generation_event", lambda({ return 41; }));
        capture_generation("fork", lambda({ return 42; }));
        return 73;
    });
}));
return 1;
`
	parentSource := fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "fork-binding-generation-child.cna", base64_decode(%q), $null];
sub run_child { return [$child runScript]; }
sub unload_child { [$loader unloadScript: $child]; return 1; }
`, base64.StdEncoding.EncodeToString([]byte(childSource)))
	program, err := CompileString("fork-binding-generation-parent.cna", parentSource)
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
	directInvocation := host.waitInvocation(t, "direct")
	directCallback, err := directInvocation.Callback(1)
	if err != nil {
		t.Fatalf("retain direct callback: %v", err)
	}
	launchResults, err := directInvocation.Bindings().DispatchEvent(context.Background(), "launch_fork_generation")
	if err != nil || len(launchResults) != 1 {
		t.Fatalf("launch fork event = (%v, %v), want one handle/nil", launchResults, err)
	}
	forkInvocation := host.waitInvocation(t, "fork")
	awaitPortableScriptLoaderGenerationSignal(t, bindings.ready, "fork binding registration")
	forkCallback, err := forkInvocation.Callback(1)
	if err != nil {
		t.Fatalf("retain fork callback: %v", err)
	}
	registered, _ := bindings.snapshot()
	if len(registered) != 1 {
		t.Fatalf("fork registrations = %#v, want one", registered)
	}
	if registered[0].Parent != nil {
		t.Fatalf("fork binding retained parent invocation ancestry: %#v", registered[0].Parent)
	}

	if _, err := parent.Call(context.Background(), "unload_child"); err != nil {
		t.Fatalf("logical child unload: %v", err)
	}
	if _, err := directCallback.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("direct callback after logical unload error = %v, want ErrScriptUnloaded", err)
	}
	if value, err := forkCallback.Invoke(context.Background()); err != nil || value.Int32() != 42 {
		t.Fatalf("fork callback after logical unload = (%s, %v), want 42/nil", value.Describe(), err)
	}
	results, err := forkInvocation.Bindings().DispatchEvent(context.Background(), "fork_generation_event")
	if err != nil || len(results) != 1 || results[0].Int32() != 41 {
		t.Fatalf("fork event after ancestor cleanup = (%v, %v), want [41]/nil", results, err)
	}
	if result, err := directInvocation.Runtime.Invoke(context.Background(), "wait", launchResults[0], Int(1000)); err != nil || result.Int32() != 73 {
		t.Fatalf("fork result after ancestor cleanup = (%s, %v), want 73/nil", result.Describe(), err)
	}
	_, unregistered := bindings.snapshot()
	if len(unregistered) != 0 {
		t.Fatalf("parent logical unload removed independent fork binding: %#v", unregistered)
	}

	if err := parent.Unload(context.Background()); err != nil {
		t.Fatalf("terminal parent unload: %v", err)
	}
	if _, err := forkCallback.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("fork callback after terminal unload error = %v, want ErrScriptUnloaded", err)
	}
	_, unregistered = bindings.snapshot()
	if len(unregistered) != 1 || unregistered[0].ID != registered[0].ID {
		t.Fatalf("fork binding terminal cleanup = %#v, want binding %d", unregistered, registered[0].ID)
	}
}
