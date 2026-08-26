package opfor

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type scriptLoaderGenerationHost struct {
	mu          sync.Mutex
	invocations []Invocation
	objects     []ObjectInvocation
}

func (host *scriptLoaderGenerationHost) Call(_ context.Context, invocation Invocation) (Value, error) {
	invocation.Arguments = append([]Argument(nil), invocation.Arguments...)
	host.mu.Lock()
	host.invocations = append(host.invocations, invocation)
	host.mu.Unlock()
	return Null(), nil
}

func (host *scriptLoaderGenerationHost) Object(_ context.Context, invocation ObjectInvocation) (Value, error) {
	if invocation.Op != ObjectConstruct || invocation.Class != "generation.Capture" {
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	}
	invocation.Arguments = append([]Argument(nil), invocation.Arguments...)
	host.mu.Lock()
	host.objects = append(host.objects, invocation)
	host.mu.Unlock()
	return Null(), nil
}

func (host *scriptLoaderGenerationHost) snapshot() ([]Invocation, []ObjectInvocation) {
	host.mu.Lock()
	defer host.mu.Unlock()
	return append([]Invocation(nil), host.invocations...), append([]ObjectInvocation(nil), host.objects...)
}

type scriptLoaderGenerationBindings struct {
	mu           sync.Mutex
	registered   []Binding
	unregistered []Binding
}

func (observer *scriptLoaderGenerationBindings) Registered(_ context.Context, binding Binding) error {
	if binding.Span.Source != "generation-child.cna" {
		return nil
	}
	observer.mu.Lock()
	observer.registered = append(observer.registered, cloneBinding(binding))
	observer.mu.Unlock()
	return nil
}

func (observer *scriptLoaderGenerationBindings) Unregistered(_ context.Context, binding Binding) error {
	if binding.Span.Source != "generation-child.cna" {
		return nil
	}
	observer.mu.Lock()
	observer.unregistered = append(observer.unregistered, cloneBinding(binding))
	observer.mu.Unlock()
	return nil
}

func (observer *scriptLoaderGenerationBindings) snapshot() ([]Binding, []Binding) {
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

func TestPortableScriptLoaderUnloadRotatesImporterCapabilities(t *testing.T) {
	host := &scriptLoaderGenerationHost{}
	bindings := &scriptLoaderGenerationBindings{}
	lifecycle := &recordingScriptLifecycle{}

	var bridgeMu sync.Mutex
	bridgeLoads := 0
	bridgeUnloads := 0
	var childScript *Script
	bridge := LoadableBridgeFuncs{
		Loaded: func(_ context.Context, script *Script) error {
			bridgeMu.Lock()
			bridgeLoads++
			generation := bridgeLoads
			childScript = script
			bridgeMu.Unlock()
			return script.RegisterFunction("bridge_generation", func(context.Context, Invocation) (Value, error) {
				return Int(int32(generation)), nil
			})
		},
		Unloaded: func(_ context.Context, script *Script) error {
			bridgeMu.Lock()
			bridgeUnloads++
			bridgeMu.Unlock()
			if !script.Active() {
				return errors.New("logical ScriptLoader cleanup received an inactive Script")
			}
			return nil
		},
	}
	provider := LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
		if request.ClassName != "generation.Bridge" {
			return nil, &UnsupportedError{Operation: "Loadable", Name: request.ClassName, Span: request.Span}
		}
		return bridge, nil
	})

	runtimeInstance, err := New(
		WithHost(host),
		WithObjectHost(host),
		WithBindingObserver(bindings),
		WithScriptLifecycleObserver(lifecycle),
		WithLoadableProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}

	childSource := `
import generation.*;
$generation++;
use("generation.Bridge");
$raw = lambda({ $raw_counter++; return $raw_counter; });
capture_generation($raw, function('&bridge_generation'));
[new Capture: $raw];
on("generation_event", lambda({ return $generation; }));
return @($generation, bridge_generation());
`
	if _, err := runtimeInstance.CompileString("generation-child.cna", childSource); err != nil {
		t.Fatalf("compile generation child: %v", err)
	}
	parentSource := fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "generation-child.cna", base64_decode(%q), $null];
sub run_child { return [$child runScript]; }
sub unload_child { [$loader unloadScript: $child]; return [$child isLoaded]; }
`, base64.StdEncoding.EncodeToString([]byte(childSource)))
	program, err := CompileString("generation-parent.cna", parentSource)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Unload(context.Background()) })

	first, err := parent.Call(context.Background(), "run_child")
	if err != nil {
		t.Fatal(err)
	}
	if got := argvValueStrings(mustArrayValues(t, first)); fmt.Sprint(got) != "[1 1]" {
		t.Fatalf("first child run = %v, want [1 1]", got)
	}
	invocations, objects := host.snapshot()
	if len(invocations) != 1 || len(objects) != 1 {
		t.Fatalf("captured host/object invocations = %d/%d, want 1/1", len(invocations), len(objects))
	}
	oldInvocation := invocations[0]
	oldObjectInvocation := objects[0]
	oldCallback, err := oldInvocation.Callback(0)
	if err != nil {
		t.Fatal(err)
	}
	oldObjectCallback, err := oldObjectInvocation.Callback(0)
	if err != nil {
		t.Fatal(err)
	}
	oldRaw, ok := oldInvocation.Arg(0).Function()
	if !ok {
		t.Fatal("first host argument is not a raw Sleep closure")
	}
	oldNative, ok := oldInvocation.Arg(1).Function()
	if !ok {
		t.Fatal("second host argument is not the bridge native function")
	}
	oldOpaqueBindings := oldInvocation.Bindings()
	registered, _ := bindings.snapshot()
	if len(registered) != 1 || registered[0].Name != "generation_event" {
		t.Fatalf("generation-one registrations = %#v, want generation_event", registered)
	}
	oldBinding := registered[0]
	resolvedChild, resolveErr := oldInvocation.Runtime.ScriptByID(oldInvocation.Script)
	if resolveErr != nil || childScript == nil || resolvedChild != childScript {
		t.Fatal("child Script pointer/provenance was not stable at first run")
	}

	loaded, err := parent.Call(context.Background(), "unload_child")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Truth() {
		t.Fatal("explicit unload reactivated ScriptInstance.loaded")
	}
	bridgeMu.Lock()
	loadsAfterFirstUnload, unloadsAfterFirstUnload := bridgeLoads, bridgeUnloads
	bridgeMu.Unlock()
	if loadsAfterFirstUnload != 1 || unloadsAfterFirstUnload != 1 {
		t.Fatalf("bridge load/unload counts = %d/%d, want 1/1 before unload returns", loadsAfterFirstUnload, unloadsAfterFirstUnload)
	}
	_, unregistered := bindings.snapshot()
	if len(unregistered) != 1 || unregistered[0].ID != oldBinding.ID {
		t.Fatalf("generation-one unregistrations = %#v, want binding %d", unregistered, oldBinding.ID)
	}
	resolvedChild, resolveErr = oldInvocation.Runtime.ScriptByID(oldInvocation.Script)
	if resolveErr != nil || resolvedChild != childScript || !childScript.Active() {
		t.Fatal("explicit unload replaced or terminally unloaded the child Script")
	}
	for name, callback := range map[string]Callable{
		"Invocation.Callback":       oldCallback,
		"ObjectInvocation.Callback": oldObjectCallback,
		"Binding.Callback":          oldBinding.Callback,
		"bridge native":             oldNative,
	} {
		if _, callErr := callback.Invoke(context.Background()); !errors.Is(callErr, ErrScriptUnloaded) {
			t.Errorf("old %s error = %v, want ErrScriptUnloaded", name, callErr)
		}
	}
	if _, lateErr := oldInvocation.Callback(0); !errors.Is(lateErr, ErrScriptUnloaded) {
		t.Errorf("late Invocation.Callback error = %v, want ErrScriptUnloaded", lateErr)
	}
	if _, lateErr := oldObjectInvocation.Callback(0); !errors.Is(lateErr, ErrScriptUnloaded) {
		t.Errorf("late ObjectInvocation.Callback error = %v, want ErrScriptUnloaded", lateErr)
	}
	if _, dispatchErr := oldOpaqueBindings.DispatchEvent(context.Background(), "generation_event"); !errors.Is(dispatchErr, ErrScriptUnloaded) {
		t.Errorf("old AggressorBindings error = %v, want ErrScriptUnloaded", dispatchErr)
	}
	if rawResult, rawErr := oldRaw.Invoke(context.Background()); rawErr != nil || rawResult.Int32() != 1 {
		t.Fatalf("raw retained Sleep closure after unload = (%s, %v), want 1/nil", rawResult.Describe(), rawErr)
	}

	second, err := parent.Call(context.Background(), "run_child")
	if err != nil {
		t.Fatal(err)
	}
	if got := argvValueStrings(mustArrayValues(t, second)); fmt.Sprint(got) != "[2 2]" {
		t.Fatalf("second child run = %v, want [2 2]", got)
	}
	invocations, _ = host.snapshot()
	if len(invocations) != 2 {
		t.Fatalf("captured host invocations after rerun = %d, want 2", len(invocations))
	}
	newInvocation := invocations[1]
	newCallback, err := newInvocation.Callback(0)
	if err != nil {
		t.Fatal(err)
	}
	if value, callErr := newCallback.Invoke(context.Background()); callErr != nil || value.Int32() != 2 {
		t.Fatalf("generation-two callback = (%s, %v), want 2/nil", value.Describe(), callErr)
	}
	if _, callErr := oldCallback.Invoke(context.Background()); !errors.Is(callErr, ErrScriptUnloaded) {
		t.Errorf("generation-one callback revived after rerun: %v", callErr)
	}
	newOpaqueBindings := newInvocation.Bindings()
	results, dispatchErr := newOpaqueBindings.DispatchEvent(context.Background(), "generation_event")
	if dispatchErr != nil || len(results) != 1 || results[0].Int32() != 2 {
		t.Fatalf("generation-two event dispatch = (%v, %v), want [2]/nil", results, dispatchErr)
	}
	if _, dispatchErr = oldOpaqueBindings.DispatchEvent(context.Background(), "generation_event"); !errors.Is(dispatchErr, ErrScriptUnloaded) {
		t.Errorf("generation-one AggressorBindings revived after rerun: %v", dispatchErr)
	}

	if _, err := parent.Call(context.Background(), "unload_child"); err != nil {
		t.Fatal(err)
	}
	bridgeMu.Lock()
	finalLoads, finalUnloads := bridgeLoads, bridgeUnloads
	bridgeMu.Unlock()
	if finalLoads != 2 || finalUnloads != 2 {
		t.Fatalf("bridge load/unload counts after second cycle = %d/%d, want 2/2", finalLoads, finalUnloads)
	}
	if _, callErr := newCallback.Invoke(context.Background()); !errors.Is(callErr, ErrScriptUnloaded) {
		t.Errorf("generation-two callback after second unload = %v, want ErrScriptUnloaded", callErr)
	}

	events := lifecycle.snapshot()
	childLoads, childUnloads := 0, 0
	for _, event := range events {
		if event.name != "generation-child.cna" {
			continue
		}
		if event.phase == "loaded" {
			childLoads++
		} else if event.phase == "unloaded" {
			childUnloads++
		}
	}
	if childLoads != 1 || childUnloads != 0 {
		t.Fatalf("child lifecycle before terminal parent unload = %d load/%d unload, want 1/0", childLoads, childUnloads)
	}
}
