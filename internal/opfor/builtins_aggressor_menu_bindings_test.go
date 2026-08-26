package opfor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAggressorFunctionBindLayersWithDeclarationAndUnload(t *testing.T) {
	observer := &dynamicBindingObserver{}
	runtimeInstance, err := New(
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithBindingObserver(observer),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	first, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "key-first.cna", `
@seen = @();
bind Ctrl+Left {
	push(@seen, "declaration");
	return "declaration";
}
$bind_result = bind("Ctrl+Left", {
	push(@seen, "function-first");
	return "function-first";
});
sub clear_shortcut {
	return unbind("Ctrl+Left");
}
`))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Get("$bind_result").IsNull() {
		t.Fatalf("bind result = %s, want $null", first.Get("$bind_result").Describe())
	}
	firstLayers := runtimeInstance.Bindings(BindingKey, "Ctrl+Left")
	if len(firstLayers) != 2 || firstLayers[0].Keyword != "bind" || firstLayers[1].Keyword != "bind" ||
		firstLayers[0].ID >= firstLayers[1].ID || len(firstLayers[1].Selectors) != 1 ||
		!firstLayers[1].Selectors[0].Evaluated || firstLayers[1].Selectors[0].Value.String() != "Ctrl+Left" {
		t.Fatalf("first key layers = %#v", firstLayers)
	}
	value, err := runtimeInstance.InvokeBinding(context.Background(), BindingKey, "Ctrl+Left")
	if err != nil || value.String() != "function-first" {
		t.Fatalf("first layered key = (%s, %v), want function-first", value.Describe(), err)
	}

	second, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "key-second.cna", `
$bind_result = bind("Ctrl+Left", { return "function-second"; });
`))
	if err != nil {
		t.Fatal(err)
	}
	secondLayers := runtimeInstance.Bindings(BindingKey, "Ctrl+Left")
	if len(secondLayers) != 3 || secondLayers[2].Script != second.ID() {
		t.Fatalf("cross-script key layers = %#v", secondLayers)
	}
	retained := secondLayers[2].Callback
	value, err = runtimeInstance.InvokeBinding(context.Background(), BindingKey, "Ctrl+Left")
	if err != nil || value.String() != "function-second" {
		t.Fatalf("newest cross-script key = (%s, %v), want function-second", value.Describe(), err)
	}

	if err := second.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := retained.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("retained key callback after unload error = %v, want ErrScriptUnloaded", err)
	}
	value, err = runtimeInstance.InvokeBinding(context.Background(), BindingKey, "Ctrl+Left")
	if err != nil || value.String() != "function-first" {
		t.Fatalf("key after upper-layer unload = (%s, %v), want function-first", value.Describe(), err)
	}
	third, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "key-third.cna", `
bind("Ctrl+Left", { return "function-third"; });
`))
	if err != nil {
		t.Fatal(err)
	}
	if layers := runtimeInstance.Bindings(BindingKey, "Ctrl+Left"); len(layers) != 3 || layers[2].Script != third.ID() {
		t.Fatalf("key layers before cross-script unbind = %#v", layers)
	}

	clearResult, err := first.Call(context.Background(), "clear_shortcut")
	if err != nil || !clearResult.IsNull() {
		t.Fatalf("unbind = (%s, %v), want $null", clearResult.Describe(), err)
	}
	if layers := runtimeInstance.Bindings(BindingKey, "Ctrl+Left"); len(layers) != 0 {
		t.Fatalf("key layers after unbind = %#v, want none", layers)
	}
	if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingKey, "Ctrl+Left"); err == nil {
		t.Fatal("unbound shortcut remained invokable")
	}
	assertDynamicBindingStrings(t, first.Get("@seen"), []string{"function-first", "function-first"})

	registered, unregistered := observer.snapshot()
	if got := dynamicBindingIdentities(filterBindingsByKind(registered, BindingKey)); !reflect.DeepEqual(got, []string{
		"bind:Ctrl+Left", "bind:Ctrl+Left", "bind:Ctrl+Left", "bind:Ctrl+Left",
	}) {
		t.Fatalf("registered key notifications = %q", got)
	}
	if got := dynamicBindingIdentities(filterBindingsByKind(unregistered, BindingKey)); !reflect.DeepEqual(got, []string{
		"bind:Ctrl+Left", "bind:Ctrl+Left", "bind:Ctrl+Left", "bind:Ctrl+Left",
	}) {
		t.Fatalf("unregistered key notifications = %q", got)
	}
}

func TestAggressorFunctionBindAndUnbindValidateExactABI(t *testing.T) {
	runtimeInstance, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "bind-missing", source: `bind("Ctrl+X");`, want: "expected exactly 2"},
		{name: "bind-extra", source: `bind("Ctrl+X", { return; }, "extra");`, want: "expected exactly 2"},
		{name: "bind-empty", source: `bind("", { return; });`, want: "keyboard shortcut is empty"},
		{name: "bind-not-callable", source: `bind("Ctrl+X", "no");`, want: "argument 2 is not callable"},
		{name: "unbind-missing", source: `unbind();`, want: "expected exactly 1"},
		{name: "unbind-extra", source: `unbind("Ctrl+X", "extra");`, want: "expected exactly 1"},
		{name: "unbind-empty", source: `unbind("");`, want: "keyboard shortcut is empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, loadErr := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, test.name+".cna", test.source))
			if loadErr == nil || !containsErrorText(loadErr, test.want) {
				t.Fatalf("Load error = %v, want %q", loadErr, test.want)
			}
		})
	}
	if bindings := runtimeInstance.Bindings(BindingKey, ""); len(bindings) != 0 {
		t.Fatalf("invalid key calls published bindings: %#v", bindings)
	}
}

func TestAggressorFunctionItemAndMenuShareCompositionRegistry(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	registerFunctionMenuExtensions(t, runtimeInstance)
	script, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "function-menu-tree.cna", `
popup function_root {
	menu("Tools " . $1, {
		item("Action " . $1, { return $1; });
	});
	item("Direct " . $1, { return $1; });
}
`))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runtimeInstance.DispatchPopupHook(context.Background(), "function_root", String("root")); err != nil {
		t.Fatal(err)
	}
	menu := onlyBinding(t, runtimeInstance.Bindings(BindingMenu, "Tools root"))
	direct := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "Direct root"))
	assertBindingParent(t, menu.Parent, BindingPopup, "function_root", []string{"root"})
	assertBindingParent(t, direct.Parent, BindingPopup, "function_root", []string{"root"})
	directResult, err := runtimeInstance.InvokeBindingByID(context.Background(), direct.Script, direct.ID, String("direct-click"))
	if err != nil || directResult.String() != "direct-click" {
		t.Fatalf("function item callback = (%s, %v), want direct-click", directResult.Describe(), err)
	}

	if _, err := runtimeInstance.InvokeBindingByID(context.Background(), menu.Script, menu.ID, String("submenu")); err != nil {
		t.Fatal(err)
	}
	action := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "Action submenu"))
	assertBindingParent(t, action.Parent, BindingMenu, "Tools root", []string{"submenu"})
	assertBindingParent(t, action.Parent.Parent, BindingPopup, "function_root", []string{"root"})
	actionResult, err := runtimeInstance.InvokeBindingByID(context.Background(), action.Script, action.ID, String("action-click"))
	if err != nil || actionResult.String() != "action-click" {
		t.Fatalf("nested function item callback = (%s, %v), want action-click", actionResult.Describe(), err)
	}

	if _, err := runtimeInstance.DispatchPopupHook(context.Background(), "function_root", String("again")); err != nil {
		t.Fatal(err)
	}
	for _, retired := range []Binding{menu, direct, action} {
		if _, active := runtimeInstance.BindingByID(retired.Script, retired.ID); active {
			t.Errorf("recomposition retained %s %q binding %d", retired.Kind, retired.Name, retired.ID)
		}
	}
	newMenu := onlyBinding(t, runtimeInstance.Bindings(BindingMenu, "Tools again"))
	newDirect := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "Direct again"))
	if newMenu.ID == menu.ID || newDirect.ID == direct.ID {
		t.Fatal("function-form recomposition reused retired binding identity")
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if bindings := runtimeInstance.Bindings(BindingMenu, ""); len(bindings) != 0 {
		t.Fatalf("function menus after unload = %#v", bindings)
	}
	if bindings := runtimeInstance.Bindings(BindingItem, ""); len(bindings) != 0 {
		t.Fatalf("function items after unload = %#v", bindings)
	}
}

func TestAggressorFunctionItemAndMenuValidateABIAndComposition(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "item-missing", source: `popup invalid_item_missing { item(); }`, want: "expected exactly 2"},
		{name: "item-extra", source: `popup invalid_item_extra { item("x", { return; }, "extra"); }`, want: "expected exactly 2"},
		{name: "item-empty", source: `popup invalid_item_empty { item("", { return; }); }`, want: "menu description is empty"},
		{name: "item-not-callable", source: `popup invalid_item_callable { item("x", "no"); }`, want: "argument 2 is not callable"},
		{name: "menu-missing", source: `popup invalid_menu_missing { menu("x"); }`, want: "expected exactly 2"},
		{name: "menu-not-callable", source: `popup invalid_menu_callable { menu("x", "no"); }`, want: "argument 2 is not callable"},
		{name: "item-outside", source: `item("x", { return; });`, want: "requires an active popup or menu composition"},
		{name: "menu-outside", source: `menu("x", { return; });`, want: "requires an active popup or menu composition"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, err := New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			registerFunctionMenuExtensions(t, runtimeInstance)
			program := mustCompileMenuBindingTest(t, test.name+".cna", test.source)
			script, loadErr := runtimeInstance.Load(context.Background(), program)
			if strings.Contains(test.source, "popup ") {
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				bindings := runtimeInstance.Bindings(BindingPopup, "")
				if len(bindings) != 1 {
					t.Fatalf("popup bindings = %#v", bindings)
				}
				_, loadErr = runtimeInstance.InvokeBindingByID(context.Background(), bindings[0].Script, bindings[0].ID)
			} else if script != nil {
				t.Fatal("invalid top-level menu call unexpectedly loaded a script")
			}
			if loadErr == nil || !containsErrorText(loadErr, test.want) {
				t.Fatalf("execution error = %v, want %q", loadErr, test.want)
			}
		})
	}
}

func registerFunctionMenuExtensions(t *testing.T, runtimeInstance *Runtime) {
	t.Helper()
	if err := runtimeInstance.RegisterFunction("item", runtimeInstance.registerMenuItem); err != nil {
		t.Fatal(err)
	}
	if err := runtimeInstance.RegisterFunction("menu", runtimeInstance.registerSubmenu); err != nil {
		t.Fatal(err)
	}
}

func TestAggressorFunctionBindUnbindConcurrentLifecycle(t *testing.T) {
	observer := &dynamicBindingObserver{}
	runtimeInstance, err := New(WithBindingObserver(observer), WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "key-concurrent.cna", `
sub install_key {
	bind("Ctrl+Race", { return "race"; });
}
sub clear_key {
	unbind("Ctrl+Race");
}
`))
	if err != nil {
		t.Fatal(err)
	}

	const registrations = 48
	var wait sync.WaitGroup
	errorsSeen := make(chan error, registrations)
	for index := 0; index < registrations; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, callErr := script.Call(context.Background(), "install_key")
			errorsSeen <- callErr
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for callErr := range errorsSeen {
		if callErr != nil {
			t.Fatalf("concurrent bind: %v", callErr)
		}
	}
	if got := len(runtimeInstance.Bindings(BindingKey, "Ctrl+Race")); got != registrations {
		t.Fatalf("concurrent key layers = %d, want %d", got, registrations)
	}

	// Multiple simultaneous clears race through the same exact generation. The
	// registry admission check makes every layer retire and notify at most once.
	errorsSeen = make(chan error, 12)
	for index := 0; index < cap(errorsSeen); index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, callErr := script.Call(context.Background(), "clear_key")
			errorsSeen <- callErr
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for callErr := range errorsSeen {
		if callErr != nil {
			t.Fatalf("concurrent unbind: %v", callErr)
		}
	}
	if got := len(runtimeInstance.Bindings(BindingKey, "Ctrl+Race")); got != 0 {
		t.Fatalf("key layers after concurrent clear = %d, want 0", got)
	}
	registered, unregistered := observer.snapshot()
	registered = filterBindingsByKind(registered, BindingKey)
	unregistered = filterBindingsByKind(unregistered, BindingKey)
	if len(registered) != registrations || len(unregistered) != registrations {
		t.Fatalf("concurrent notifications registered/unregistered = %d/%d, want %d/%d",
			len(registered), len(unregistered), registrations, registrations)
	}
}

func TestAggressorInsertMenuComposesAllLayersUnderCurrentParent(t *testing.T) {
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("insert_menu reached Host")
		})),
		WithStdout(io.Discard),
		WithStderr(io.Discard),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "insert-menu.cna", `
@order = @();
$insert_result = "unset";
popup child_hook {
	push(@order, "first:" . $1 . ":" . $2);
	item "First" { return $1; }
}
popup child_hook {
	push(@order, "second:" . $1 . ":" . $2);
	item "Second" { return $1; }
}
popup root_hook {
	item "Before" { return; }
	$insert_result = insert_menu("child_hook", $1, "tail");
	insert_menu("missing_hook", $1);
	item "After" { return; }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })

	if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingPopup, "root_hook", String("component")); err != nil {
		t.Fatal(err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("insert_menu crossed Host %d time(s)", hostCalls.Load())
	}
	if !script.Get("$insert_result").IsNull() {
		t.Fatalf("insert_menu result = %s, want $null", script.Get("$insert_result").Describe())
	}
	assertDynamicBindingStrings(t, script.Get("@order"), []string{
		"first:component:tail", "second:component:tail",
	})
	items := runtimeInstance.Bindings(BindingItem, "")
	wantNames := []string{"Before", "First", "Second", "After"}
	gotNames := make([]string, len(items))
	for index, item := range items {
		gotNames[index] = item.Name
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("inserted item registration order = %q, want %q", gotNames, wantNames)
	}
	first := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "First"))
	second := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "Second"))
	before := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "Before"))
	after := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "After"))
	assertBindingParent(t, before.Parent, BindingPopup, "root_hook", []string{"component"})
	assertBindingParent(t, after.Parent, BindingPopup, "root_hook", []string{"component"})
	for _, child := range []Binding{first, second} {
		assertBindingParent(t, child.Parent, BindingPopup, "child_hook", []string{"component", "tail"})
		assertBindingParent(t, child.Parent.Parent, BindingPopup, "root_hook", []string{"component"})
	}

	firstIDs := []uint64{before.ID, first.ID, second.ID, after.ID}
	if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingPopup, "root_hook", String("again")); err != nil {
		t.Fatal(err)
	}
	items = runtimeInstance.Bindings(BindingItem, "")
	if len(items) != len(firstIDs) {
		t.Fatalf("items after recomposition = %d, want %d", len(items), len(firstIDs))
	}
	for index, item := range items {
		if item.ID == firstIDs[index] {
			t.Fatalf("recomposition retained item ID %d at index %d", item.ID, index)
		}
	}
}

func TestAggressorInsertMenuPinsGenerationAcrossConcurrentClear(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var calls []string
	runtimeInstance, err := New(
		WithFunction("block_inserted_popup", func(context.Context, Invocation) (Value, error) {
			once.Do(func() { close(started) })
			<-release
			return Null(), nil
		}),
		WithFunction("record_inserted_popup", func(_ context.Context, invocation Invocation) (Value, error) {
			mu.Lock()
			calls = append(calls, invocation.Arg(0).String())
			mu.Unlock()
			return Null(), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "insert-menu-pin.cna", `
popup pinned_child {
	block_inserted_popup();
	record_inserted_popup("first");
}
popup pinned_child {
	record_inserted_popup("second");
}
popup pinned_root {
	insert_menu("pinned_child");
}
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })

	done := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.InvokeBinding(context.Background(), BindingPopup, "pinned_root")
		done <- invokeErr
	}()
	<-started
	if err := runtimeInstance.clearAggressorPopupBindings(context.Background(), "pinned_child"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("pinned insert_menu composition: %v", err)
	}
	mu.Lock()
	got := append([]string(nil), calls...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("pinned popup calls = %q, want first/second", got)
	}
	if bindings := runtimeInstance.Bindings(BindingPopup, "pinned_child"); len(bindings) != 0 {
		t.Fatalf("cleared popup generation remains active: %#v", bindings)
	}
}

func TestAggressorInsertMenuCrossScriptDescendantsFollowRootComposition(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	firstOwner, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "insert-owner-first.cna", `
popup shared_child { item "Cross First" { return; } }
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstOwner.Unload(context.Background()) })
	secondOwner, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "insert-owner-second.cna", `
popup shared_child { item "Cross Second" { return; } }
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondOwner.Unload(context.Background()) })
	caller, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "insert-caller.cna", `
popup shared_root { insert_menu("shared_child"); }
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = caller.Unload(context.Background()) })

	if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingPopup, "shared_root"); err != nil {
		t.Fatal(err)
	}
	first := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "Cross First"))
	second := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "Cross Second"))
	if first.Script != firstOwner.ID() || second.Script != secondOwner.ID() {
		t.Fatalf("inserted item owners = %d/%d, want %d/%d", first.Script, second.Script, firstOwner.ID(), secondOwner.ID())
	}
	for _, child := range []Binding{first, second} {
		assertBindingParent(t, child.Parent, BindingPopup, "shared_child", []string{})
		assertBindingParent(t, child.Parent.Parent, BindingPopup, "shared_root", []string{})
	}

	if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingPopup, "shared_root"); err != nil {
		t.Fatal(err)
	}
	replacedFirst := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "Cross First"))
	replacedSecond := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "Cross Second"))
	if replacedFirst.ID == first.ID || replacedSecond.ID == second.ID {
		t.Fatalf("cross-script recomposition retained child IDs %d/%d", replacedFirst.ID, replacedSecond.ID)
	}
	if err := runtimeInstance.clearAggressorPopupBindings(context.Background(), "shared_root"); err != nil {
		t.Fatal(err)
	}
	if firstChildren, secondChildren := runtimeInstance.Bindings(BindingItem, "Cross First"), runtimeInstance.Bindings(BindingItem, "Cross Second"); len(firstChildren) != 0 || len(secondChildren) != 0 {
		t.Fatalf("cleared cross-script descendants = %d/%d, want 0/0", len(firstChildren), len(secondChildren))
	}
	if childRoots := runtimeInstance.Bindings(BindingPopup, "shared_child"); len(childRoots) != 2 {
		t.Fatalf("clearing parent removed reusable child popup roots: %#v", childRoots)
	}
}

func TestAggressorInsertMenuRequiresCompositionAndValidHook(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	for _, test := range []struct {
		name   string
		source string
		want   string
	}{
		{name: "missing", source: `insert_menu();`, want: "expected at least 1"},
		{name: "empty", source: `insert_menu("");`, want: "popup hook is empty"},
		{name: "outside", source: `insert_menu("child");`, want: "requires an active popup or menu composition"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, loadErr := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "insert-"+test.name+".cna", test.source))
			if loadErr == nil || !containsErrorText(loadErr, test.want) {
				t.Fatalf("Load error = %v, want %q", loadErr, test.want)
			}
		})
	}
}

func TestAggressorMenubarProviderRetainsOwnerGuardedPopupComposer(t *testing.T) {
	var request AggressorClientUIRequest
	var mu sync.Mutex
	var calls []string
	providerResult := ObjectValue(&struct{ menu bool }{true})
	runtimeInstance, err := New(
		WithFunction("record_menubar_popup", func(_ context.Context, invocation Invocation) (Value, error) {
			if len(invocation.Arguments) != 1 || invocation.Arg(0).Int32() != 0 {
				return Null(), fmt.Errorf("menubar popup positional count = %s", invocation.Arg(0).Describe())
			}
			mu.Lock()
			calls = append(calls, invocation.Arg(0).String())
			mu.Unlock()
			return Null(), nil
		}),
		WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(_ context.Context, candidate AggressorClientUIRequest) (Value, error) {
			request = candidate
			return providerResult, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "menubar-provider.cna", `
popup top_level_menu {
	record_menubar_popup(size(@_));
}
popup top_level_menu {
	record_menubar_popup(size(@_));
}
$menubar_result = menubar("My &Things", "top_level_menu");
`))
	if err != nil {
		t.Fatal(err)
	}
	if !script.Get("$menubar_result").IdentityEqual(providerResult) {
		t.Fatalf("menubar result = %s, want provider identity", script.Get("$menubar_result").Describe())
	}
	if request.Operation != AggressorClientUIMenubar || request.Name != "menubar" || request.Script != script.ID() ||
		request.RuntimeID != runtimeInstance.ID() || request.Popup == nil || len(request.Arguments) != 2 ||
		request.Arguments[0].String() != "My &Things" || request.Arguments[1].String() != "top_level_menu" {
		t.Fatalf("menubar request = %#v", request)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := request.Popup.Compose(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled menubar composer error = %v, want context.Canceled", err)
	}
	if err := request.Popup.Compose(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	mu.Unlock()
	if !reflect.DeepEqual(gotCalls, []string{"0", "0"}) {
		t.Fatalf("menubar popup calls = %q, want two zero-argument callbacks", gotCalls)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := request.Popup.Compose(context.Background()); !errors.Is(err, ErrAggressorPopupStale) || !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("menubar composer after owner unload = %v, want stale/script-unloaded", err)
	}
}

func TestAggressorMenubarHostFallbackPreservesRawInvocationAndPopup(t *testing.T) {
	var calls atomic.Int32
	var captured Invocation
	runtimeInstance, err := New(WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
		calls.Add(1)
		captured = invocation
		if invocation.Name != "menubar" {
			return Null(), fmt.Errorf("unexpected Host call %s", invocation.Name)
		}
		invocation.Arguments[0].Set(String("Host changed description"))
		_, invokeErr := invocation.Runtime.InvokeBinding(ctx, BindingPopup, invocation.Arg(1).String())
		return String("Host registered menu"), invokeErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "menubar-host.cna", `
$description = "Original";
$composed = 0;
popup host_menu {
	$composed++;
}
$menubar_result = menubar($description, "host_menu");
`))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || captured.Runtime != runtimeInstance || captured.Script != script.ID() ||
		captured.Arguments[0].Reference == nil || captured.Arguments[1].Resolve().String() != "host_menu" {
		t.Fatalf("menubar Host fallback = calls %d invocation %#v", calls.Load(), captured)
	}
	if got := script.Get("$description").String(); got != "Host changed description" {
		t.Fatalf("Host reference mutation = %q", got)
	}
	if got := script.Get("$composed").Int32(); got != 1 {
		t.Fatalf("Host-composed popup count = %d, want 1", got)
	}
	if got := script.Get("$menubar_result").String(); got != "Host registered menu" {
		t.Fatalf("Host menubar result = %q", got)
	}
}

func mustCompileMenuBindingTest(t *testing.T, name, source string) *Program {
	t.Helper()
	program, err := CompileString(name, source)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func containsErrorText(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}

func filterBindingsByKind(bindings []Binding, kind BindingKind) []Binding {
	result := make([]Binding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Kind == kind {
			result = append(result, binding)
		}
	}
	return result
}
