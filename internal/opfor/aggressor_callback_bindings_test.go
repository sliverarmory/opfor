package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestAggressorTypedRequestsCarryRuntimeBindings(t *testing.T) {
	t.Parallel()

	var artifact AggressorArtifactRequest
	var action AggressorBeaconAction
	var execution AggressorBeaconExecutionRequest
	var payload AggressorPayloadRequest
	var listener AggressorListenerRequest
	var injection AggressorProcessInjectionRequest
	var service AggressorClientServiceRequest
	var ui AggressorClientUIRequest
	var site AggressorSiteRequest

	runtimeInstance, err := New(
		WithAggressorArtifactProvider(AggressorArtifactProviderFunc(func(_ context.Context, request AggressorArtifactRequest) (Value, error) {
			artifact = request
			return String("artifact"), nil
		})),
		WithAggressorBeaconActionProvider(AggressorBeaconActionProviderFunc(func(_ context.Context, request AggressorBeaconAction) error {
			action = request
			return nil
		})),
		WithAggressorBeaconExecutionProvider(AggressorBeaconExecutionProviderFunc(func(_ context.Context, request AggressorBeaconExecutionRequest) (Value, error) {
			execution = request
			return Int(7), nil
		})),
		WithAggressorPayloadProvider(AggressorPayloadProviderFunc(func(_ context.Context, request AggressorPayloadRequest) (Value, error) {
			payload = request
			return request.Arg(0), nil
		})),
		WithAggressorListenerProvider(AggressorListenerProviderFunc(func(_ context.Context, request AggressorListenerRequest) (Value, error) {
			listener = request
			return ArrayValue(NewArray()), nil
		})),
		WithAggressorProcessInjectionProvider(AggressorProcessInjectionProviderFunc(func(_ context.Context, request AggressorProcessInjectionRequest) (Value, error) {
			injection = request
			return String("injector"), nil
		})),
		WithAggressorClientServiceProvider(AggressorClientServiceProviderFunc(func(_ context.Context, request AggressorClientServiceRequest) (Value, error) {
			service = request
			return String("operator"), nil
		})),
		WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(_ context.Context, request AggressorClientUIRequest) (Value, error) {
			ui = request
			return Null(), nil
		})),
		WithAggressorSiteProvider(AggressorSiteProviderFunc(func(_ context.Context, request AggressorSiteRequest) (Value, error) {
			site = request
			return String("127.0.0.1"), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	calls := []struct {
		name      string
		arguments []Value
	}{
		{name: "artifact_payload", arguments: []Value{String("listener"), String("exe"), String("x64"), String("thread"), String("none")}},
		{name: "bexit", arguments: []Value{String("beacon")}},
		{name: "get_postex_kit_callback_id"},
		{name: "beacon_inline_execute", arguments: []Value{String("beacon"), String("bof"), String("go"), String("")}},
		{name: "artifact_sign", arguments: []Value{String("bytes")}},
		{name: "listeners"},
		{name: "pi_explicit_get"},
		{name: "mynick"},
		{name: "separator"},
		{name: "localip"},
	}
	for _, call := range calls {
		if _, invokeErr := runtimeInstance.Invoke(context.Background(), call.name, call.arguments...); invokeErr != nil {
			t.Fatalf("Invoke(%s): %v", call.name, invokeErr)
		}
	}

	bindings := []AggressorBindings{
		artifact.Bindings, action.Bindings, execution.Bindings,
		payload.Bindings, listener.Bindings, injection.Bindings,
		service.Bindings, ui.Bindings, site.Bindings,
	}
	runtimeIDs := []RuntimeID{
		artifact.RuntimeID, action.RuntimeID, execution.RuntimeID,
		payload.RuntimeID, listener.RuntimeID, injection.RuntimeID,
		service.RuntimeID, ui.RuntimeID, site.RuntimeID,
	}
	for index, current := range bindings {
		if !current.Valid() || current.RuntimeID() != runtimeInstance.ID() ||
			runtimeIDs[index] != runtimeInstance.ID() || !current.Same(bindings[0]) {
			t.Errorf("request %d bindings = valid:%v id:%d request-id:%d same:%v, want runtime %d",
				index, current.Valid(), current.RuntimeID(), runtimeIDs[index], current.Same(bindings[0]), runtimeInstance.ID())
		}
	}
}

func TestAggressorBindingsUseScriptLoaderChildRuntime(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-bindings.sl")
	if err := os.WriteFile(childPath, []byte(`
set PROVIDER_ROUTE {
    return "child:" . $1;
}
return artifact_sign("c");
`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-bindings.sl", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
set PROVIDER_ROUTE {
    return "parent:" . $1;
}
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
$parent = artifact_sign("p");
$child_result = [$child runScript];
return $parent . "|" . $child_result;
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}

	var mutex sync.Mutex
	var requests []AggressorPayloadRequest
	runtimeInstance, err := New(WithAggressorPayloadProvider(AggressorPayloadProviderFunc(func(
		ctx context.Context,
		request AggressorPayloadRequest,
	) (Value, error) {
		mutex.Lock()
		requests = append(requests, request)
		mutex.Unlock()
		return request.Bindings.InvokeHook(ctx, "PROVIDER_ROUTE", request.Arg(0))
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil || result.String() != "parent:p|child:c" {
		t.Fatalf("parent/child routed result = (%s, %v), want parent:p|child:c", result.Describe(), err)
	}
	mutex.Lock()
	captured := append([]AggressorPayloadRequest(nil), requests...)
	mutex.Unlock()
	if len(captured) != 2 {
		t.Fatalf("requests = %d, want parent and child", len(captured))
	}
	parentBindings, childBindings := captured[0].Bindings, captured[1].Bindings
	if !parentBindings.Valid() || !childBindings.Valid() ||
		parentBindings.RuntimeID() != runtimeInstance.ID() ||
		childBindings.RuntimeID() != captured[1].RuntimeID ||
		childBindings.RuntimeID() == parentBindings.RuntimeID() ||
		parentBindings.Same(childBindings) || !parentBindings.Same(captured[0].Bindings) {
		t.Fatalf("parent/child bindings = parent(valid:%v id:%d) child(valid:%v id:%d) same:%v",
			parentBindings.Valid(), parentBindings.RuntimeID(), childBindings.Valid(), childBindings.RuntimeID(), parentBindings.Same(childBindings))
	}
}

func TestAggressorBindingsDispatchLifecycleCancellationAndConcurrency(t *testing.T) {
	t.Parallel()

	var bindings AggressorBindings
	runtimeInstance, err := New(WithAggressorPayloadProvider(AggressorPayloadProviderFunc(func(
		_ context.Context,
		request AggressorPayloadRequest,
	) (Value, error) {
		bindings = request.Bindings
		return request.Arg(0), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	first, err := runtimeInstance.Load(context.Background(), mustCompileAggressorBindingsTest(t, "bindings-first.sl", `
on("ready", { return "event:" . $1; });
set ROUTE { return "first:" . $1; }
popup shared { return "popup-first:" . $1; }
`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtimeInstance.Load(context.Background(), mustCompileAggressorBindingsTest(t, "bindings-second.sl", `
set ROUTE { return "second:" . $1; }
popup shared { return "popup-second:" . $1; }
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "artifact_sign", String("capture")); err != nil {
		t.Fatal(err)
	}
	if !bindings.Valid() || bindings.RuntimeID() != runtimeInstance.ID() || !bindings.Same(bindings) {
		t.Fatalf("bindings identity = valid:%v id:%d self:%v", bindings.Valid(), bindings.RuntimeID(), bindings.Same(bindings))
	}

	events, err := bindings.DispatchEvent(context.Background(), "ready", String("value"))
	if err != nil || !reflect.DeepEqual(bindingResultStrings(events), []string{"event:value"}) {
		t.Fatalf("DispatchEvent = (%q, %v)", bindingResultStrings(events), err)
	}
	hook, err := bindings.InvokeHook(context.Background(), "ROUTE", String("value"))
	if err != nil || hook.String() != "second:value" {
		t.Fatalf("InvokeHook newest = (%s, %v)", hook.Describe(), err)
	}
	popup, err := bindings.InvokePopupHook(context.Background(), "shared", String("value"))
	if err != nil || popup.String() != "popup-second:value" {
		t.Fatalf("InvokePopupHook newest = (%s, %v)", popup.Describe(), err)
	}
	popups, err := bindings.DispatchPopupHook(context.Background(), "shared", String("value"))
	if err != nil || !reflect.DeepEqual(bindingResultStrings(popups), []string{"popup-first:value", "popup-second:value"}) {
		t.Fatalf("DispatchPopupHook = (%q, %v)", bindingResultStrings(popups), err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bindings.InvokeHook(cancelled, "ROUTE"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled InvokeHook error = %v, want context.Canceled", err)
	}

	const workers = 16
	var wait sync.WaitGroup
	errorsByWorker := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			want := fmt.Sprintf("second:%d", worker)
			value, invokeErr := bindings.InvokeHook(context.Background(), "ROUTE", Int(int32(worker)))
			if invokeErr != nil || value.String() != want {
				errorsByWorker <- fmt.Errorf("worker %d = (%s, %v), want %s", worker, value.Describe(), invokeErr, want)
			}
		}(worker)
	}
	wait.Wait()
	close(errorsByWorker)
	for workerErr := range errorsByWorker {
		t.Error(workerErr)
	}

	if err := second.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	hook, err = bindings.InvokeHook(context.Background(), "ROUTE", String("after"))
	if err != nil || hook.String() != "first:after" {
		t.Fatalf("InvokeHook after newest unload = (%s, %v)", hook.Describe(), err)
	}
	if err := first.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := bindings.InvokeHook(context.Background(), "ROUTE"); err == nil {
		t.Fatal("InvokeHook found an unloaded hook")
	} else {
		var unsupported *UnsupportedError
		if !errors.As(err, &unsupported) {
			t.Fatalf("InvokeHook after unload error = %v, want UnsupportedError", err)
		}
	}

	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !bindings.Valid() || bindings.RuntimeID() != runtimeInstance.ID() {
		t.Fatal("closing Runtime invalidated capability identity")
	}
	if _, err := bindings.DispatchEvent(context.Background(), "ready"); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("DispatchEvent after Close error = %v, want ErrRuntimeClosed", err)
	}
}

func TestAggressorBindingsZeroValue(t *testing.T) {
	t.Parallel()

	var bindings AggressorBindings
	if bindings.Valid() || bindings.RuntimeID() != 0 || bindings.Same(bindings) {
		t.Fatalf("zero bindings = valid:%v id:%d same:%v", bindings.Valid(), bindings.RuntimeID(), bindings.Same(bindings))
	}
	if runtimeInstance, err := New(); err != nil {
		t.Fatal(err)
	} else {
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		if bindings.Same(aggressorBindingsFor(runtimeInstance)) {
			t.Fatal("zero bindings compare equal to a valid capability")
		}
	}
	for name, call := range map[string]func() error{
		"DispatchEvent": func() error {
			_, err := bindings.DispatchEvent(context.Background(), "event")
			return err
		},
		"InvokeHook": func() error {
			_, err := bindings.InvokeHook(context.Background(), "hook")
			return err
		},
		"InvokePopupHook": func() error {
			_, err := bindings.InvokePopupHook(context.Background(), "popup")
			return err
		},
		"DispatchPopupHook": func() error {
			_, err := bindings.DispatchPopupHook(context.Background(), "popup")
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrAggressorBindingsUnavailable) {
			t.Errorf("zero %s error = %v, want ErrAggressorBindingsUnavailable", name, err)
		}
	}
}

func mustCompileAggressorBindingsTest(t *testing.T, name, source string) *Program {
	t.Helper()
	program, err := CompileString(name, source)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func bindingResultStrings(values []Value) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}
