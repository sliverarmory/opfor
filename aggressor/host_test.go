package aggressor

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestHostAdaptsInvocationAndPassByNameArguments(t *testing.T) {
	t.Parallel()

	host := NewHost()
	reference := opfor.NewCell(opfor.String("before"))
	span := opfor.Span{
		Source: "adapter.cna",
		Start:  opfor.Position{Offset: 10, Line: 2, Column: 3},
		End:    opfor.Position{Offset: 20, Line: 2, Column: 13},
	}
	if err := host.Register("&blog", func(_ context.Context, request Request) (Value, error) {
		if request.Name != "blog" || request.ScriptID != 42 {
			t.Fatalf("request identity = %q/%d", request.Name, request.ScriptID)
		}
		if request.Location.Source != "adapter.cna" || request.Location.Start.Line != 2 || request.Location.End.Column != 13 {
			t.Fatalf("request location = %#v", request.Location)
		}
		if got := request.Values(); !reflect.DeepEqual(valueDescriptions(got), []string{"7", "'before'"}) {
			t.Fatalf("request values = %v", valueDescriptions(got))
		}
		first, ok := request.Arg(0)
		if !ok || first.Name != "bid" || first.IsReference() {
			t.Fatalf("first argument = %#v, %v", first, ok)
		}
		if err := first.Set(opfor.Int(9)); !errors.Is(err, ErrNotReference) {
			t.Fatalf("ordinary argument Set error = %v", err)
		}
		second, ok := request.Arg(1)
		if !ok || !second.IsReference() {
			t.Fatalf("second argument = %#v, %v", second, ok)
		}
		if err := second.Set(opfor.String("after")); err != nil {
			t.Fatalf("reference Set: %v", err)
		}
		return opfor.String("handled"), nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	value, err := host.Call(context.Background(), opfor.Invocation{
		Script: 42,
		Name:   "blog",
		Span:   span,
		Arguments: []opfor.Argument{
			{Name: "bid", Value: opfor.Int(7)},
			{Reference: reference},
		},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if value.String() != "handled" || reference.Get().String() != "after" {
		t.Fatalf("result/reference = %q/%q", value.String(), reference.Get().String())
	}
	if names := host.Names(); !reflect.DeepEqual(names, []string{"blog"}) {
		t.Fatalf("Names = %q", names)
	}
}

func TestHostWorksWithRuntimeAndReportsUnsupportedCalls(t *testing.T) {
	t.Parallel()

	host := NewHost()
	if err := host.Register("echo_host", func(_ context.Context, request Request) (Value, error) {
		argument, ok := request.Arg(0)
		if !ok {
			return opfor.Null(), errors.New("missing argument")
		}
		return argument.Value(), nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	runtime, err := opfor.New(opfor.WithHost(host))
	if err != nil {
		t.Fatalf("opfor.New: %v", err)
	}
	value, err := runtime.Invoke(context.Background(), "echo_host", opfor.String("payload"))
	if err != nil || value.String() != "payload" {
		t.Fatalf("Invoke = %s, %v", value.Describe(), err)
	}

	if !host.Unregister("&echo_host") || host.Unregister("echo_host") {
		t.Fatal("Unregister presence result is incorrect")
	}
	span := opfor.Span{Source: "missing.cna", Start: opfor.Position{Line: 4, Column: 2}, End: opfor.Position{Line: 4, Column: 9}}
	_, err = host.Call(context.Background(), opfor.Invocation{Name: "missing", Span: span})
	var unsupported *opfor.UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Name != "missing" || unsupported.Span != span {
		t.Fatalf("unsupported error = %#v (%v)", unsupported, err)
	}
}

func TestHostCallbackCanMutateBareVariableArgument(t *testing.T) {
	t.Parallel()

	host := NewHost()
	if err := host.Register("host_mutate", func(_ context.Context, request Request) (Value, error) {
		argument, ok := request.Arg(0)
		if !ok || !argument.IsReference() {
			return opfor.Null(), errors.New("bare variable was not passed as a scalar reference")
		}
		if err := argument.Set(opfor.Int(8)); err != nil {
			return opfor.Null(), err
		}
		return opfor.Null(), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime, err := opfor.New(opfor.WithHost(host))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(context.Background(), "mutate.cna", `$x = 1; host_mutate($x); return $x;`)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.Int32(); got != 8 {
		t.Fatalf("mutated value = %d, want 8", got)
	}
}

func TestHostFallbackValidationAndCancellation(t *testing.T) {
	t.Parallel()

	var zero Host
	zero.SetFallback(func(_ context.Context, request Request) (Value, error) {
		return opfor.String("fallback:" + request.Name), nil
	})
	value, err := zero.Call(context.Background(), opfor.Invocation{Name: "dynamic_name"})
	if err != nil || value.String() != "fallback:dynamic_name" {
		t.Fatalf("fallback Call = %s, %v", value.Describe(), err)
	}

	for _, test := range []struct {
		name     string
		callback Callback
	}{
		{name: "", callback: func(context.Context, Request) (Value, error) { return opfor.Null(), nil }},
		{name: "bad name", callback: func(context.Context, Request) (Value, error) { return opfor.Null(), nil }},
		{name: "valid", callback: nil},
	} {
		if err := zero.Register(test.name, test.callback); err == nil {
			t.Errorf("Register(%q, %v) unexpectedly succeeded", test.name, test.callback)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := zero.Call(ctx, opfor.Invocation{Name: "dynamic_name"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Call error = %v", err)
	}
	zero.SetFallback(nil)
	if _, err := zero.Call(context.Background(), opfor.Invocation{Name: "dynamic_name"}); err == nil {
		t.Fatal("cleared fallback unexpectedly handled call")
	}
}

func TestHostRegistryIsConcurrent(t *testing.T) {
	t.Parallel()

	host := NewHost()
	callback := func(_ context.Context, request Request) (Value, error) {
		return opfor.String(request.Name), nil
	}
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		worker := worker
		group.Add(1)
		go func() {
			defer group.Done()
			name := "callback_" + string(rune('a'+worker))
			for iteration := 0; iteration < 100; iteration++ {
				if err := host.Register(name, callback); err != nil {
					t.Errorf("Register: %v", err)
					return
				}
				if _, err := host.Call(context.Background(), opfor.Invocation{Name: name}); err != nil {
					t.Errorf("Call: %v", err)
					return
				}
			}
		}()
	}
	group.Wait()
	if names := host.Names(); len(names) != 8 || !sort.StringsAreSorted(names) {
		t.Fatalf("Names = %q", names)
	}
}

func TestRuntimeCapabilityDisambiguatesRuntimesAndDispatchesBindings(t *testing.T) {
	t.Parallel()

	host := NewHost()
	var capabilities []Runtime
	if err := host.Register("capture_runtime", func(_ context.Context, request Request) (Value, error) {
		capabilities = append(capabilities, request.Runtime)
		return opfor.Null(), nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	first, err := opfor.New(opfor.WithHost(host))
	if err != nil {
		t.Fatalf("first opfor.New: %v", err)
	}
	second, err := opfor.New(opfor.WithHost(host))
	if err != nil {
		t.Fatalf("second opfor.New: %v", err)
	}
	program, err := opfor.CompileString("bridge.cna", `
on bridge_event { return $1; }
set BRIDGE_HOOK { return $1; }
popup bridge_popup { return $1; }
popup bridge_layers { return "first:" . $1; }
popup bridge_layers { return "second:" . $1; }
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	if _, err := first.Load(context.Background(), program); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := first.Invoke(context.Background(), "capture_runtime"); err != nil {
		t.Fatalf("first Invoke: %v", err)
	}
	if _, err := second.Invoke(context.Background(), "capture_runtime"); err != nil {
		t.Fatalf("second Invoke: %v", err)
	}
	if len(capabilities) != 2 || !capabilities[0].Valid() || !capabilities[1].Valid() || capabilities[0].Same(capabilities[1]) {
		t.Fatalf("runtime capabilities = %#v", capabilities)
	}

	results, err := capabilities[0].DispatchEvent(context.Background(), "bridge_event", opfor.String("event"))
	if err != nil || len(results) != 1 || results[0].String() != "event" {
		t.Fatalf("DispatchEvent = %v, %v", valueDescriptions(results), err)
	}
	hook, err := capabilities[0].InvokeHook(context.Background(), "BRIDGE_HOOK", opfor.String("hook"))
	if err != nil || hook.String() != "hook" {
		t.Fatalf("InvokeHook = %s, %v", hook.Describe(), err)
	}
	popup, err := capabilities[0].InvokePopupHook(context.Background(), "bridge_popup", opfor.String("popup"))
	if err != nil || popup.String() != "popup" {
		t.Fatalf("InvokePopupHook = %s, %v", popup.Describe(), err)
	}
	popupLayers, err := capabilities[0].DispatchPopupHook(context.Background(), "bridge_layers", opfor.String("popup"))
	if err != nil || !reflect.DeepEqual(valueDescriptions(popupLayers), []string{"'first:popup'", "'second:popup'"}) {
		t.Fatalf("DispatchPopupHook = %v, %v", valueDescriptions(popupLayers), err)
	}
	newestPopup, err := capabilities[0].InvokePopupHook(context.Background(), "bridge_layers", opfor.String("popup"))
	if err != nil || newestPopup.String() != "second:popup" {
		t.Fatalf("newest InvokePopupHook = %s, %v", newestPopup.Describe(), err)
	}
	missingPopupLayers, err := capabilities[0].DispatchPopupHook(context.Background(), "missing_popup")
	if err != nil || len(missingPopupLayers) != 0 {
		t.Fatalf("missing DispatchPopupHook = %v, %v, want empty success", valueDescriptions(missingPopupLayers), err)
	}

	var missing Runtime
	if missing.Same(Runtime{}) {
		t.Fatal("two invalid Runtime capabilities unexpectedly identify a runtime")
	}
	if _, err := missing.DispatchEvent(context.Background(), "bridge_event"); !errors.Is(err, ErrNoRuntime) {
		t.Fatalf("zero Runtime DispatchEvent error = %v", err)
	}
	if _, err := missing.InvokeHook(context.Background(), "BRIDGE_HOOK"); !errors.Is(err, ErrNoRuntime) {
		t.Fatalf("zero Runtime InvokeHook error = %v", err)
	}
	if _, err := missing.InvokePopupHook(context.Background(), "bridge_popup"); !errors.Is(err, ErrNoRuntime) {
		t.Fatalf("zero Runtime InvokePopupHook error = %v", err)
	}
	if _, err := missing.DispatchPopupHook(context.Background(), "bridge_popup"); !errors.Is(err, ErrNoRuntime) {
		t.Fatalf("zero Runtime DispatchPopupHook error = %v", err)
	}
}

func TestRuntimeDispatchPopupHookComposesAcrossScriptsAndTracksUnload(t *testing.T) {
	t.Parallel()

	host := NewHost()
	var capability Runtime
	if err := host.Register("capture_popup_runtime", func(_ context.Context, request Request) (Value, error) {
		capability = request.Runtime
		return opfor.Null(), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtimeInstance, err := opfor.New(opfor.WithHost(host))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	firstProgram, err := opfor.CompileString("popup-first.cna", `
popup shared_popup { return "first:" . $1; }
capture_popup_runtime();
`)
	if err != nil {
		t.Fatal(err)
	}
	first, err := runtimeInstance.Load(context.Background(), firstProgram)
	if err != nil {
		t.Fatal(err)
	}
	secondProgram, err := opfor.CompileString("popup-second.cna", `
popup shared_popup { return "second:" . $1; }
`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtimeInstance.Load(context.Background(), secondProgram)
	if err != nil {
		t.Fatal(err)
	}
	if !capability.Valid() {
		t.Fatal("captured runtime capability is invalid")
	}

	results, err := capability.DispatchPopupHook(context.Background(), "shared_popup", opfor.String("selection"))
	if err != nil || !reflect.DeepEqual(valueDescriptions(results), []string{"'first:selection'", "'second:selection'"}) {
		t.Fatalf("two-script DispatchPopupHook = %v, %v", valueDescriptions(results), err)
	}
	if err := second.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	results, err = capability.DispatchPopupHook(context.Background(), "shared_popup", opfor.String("selection"))
	if err != nil || !reflect.DeepEqual(valueDescriptions(results), []string{"'first:selection'"}) {
		t.Fatalf("DispatchPopupHook after upper-layer unload = %v, %v", valueDescriptions(results), err)
	}
	if err := first.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !capability.Valid() || !capability.Same(capability) {
		t.Fatal("unloading the originating script invalidated runtime provenance")
	}
	results, err = capability.DispatchPopupHook(context.Background(), "shared_popup", opfor.String("selection"))
	if !errors.Is(err, opfor.ErrScriptUnloaded) || len(results) != 0 {
		t.Fatalf("DispatchPopupHook after owner unload = %v, %v, want ErrScriptUnloaded", valueDescriptions(results), err)
	}
}

func valueDescriptions(values []Value) []string {
	descriptions := make([]string, len(values))
	for index, value := range values {
		descriptions[index] = value.Describe()
	}
	return descriptions
}
