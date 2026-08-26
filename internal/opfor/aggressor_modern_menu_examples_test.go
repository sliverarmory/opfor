package opfor

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// These OPFOR-authored snippets isolate the menubar, popup composition, and
// dynamic submenu contracts. External executable .cna evidence is intentionally
// limited to the approved aggressor_script_examples corpus.
const (
	authoredConditionalMenuSource = "authored-conditional-menubar.sl"
	authoredPayloadMenuSource     = "authored-payload-menubar.sl"
	authoredDynamicMenuSource     = "authored-dynamic-submenu.sl"
)

const authoredConditionalMenuSnippet = `
if (getAggressorClientType() eq "ui") {
   popup preferences_menu
   {
      item "Preferences" {
         show_config_dialog();
      }
   }

   menubar("Preferences", "preferences_menu");
}
`

const authoredPayloadMenuSnippet = `
popup payloads_server {
    item("Stager Payload Generator", {open_stager_dialog();});
    item("Stageless Payload Generator", {open_stageless_dialog();});
    item("Download payload", {open_download_dialog();});
}


menubar("Server-Side &Payloads", "payloads_server");
`

const authoredDynamicMenuSnippet = `
popup ssh {
    item("SSH Action", { return $1; });
}
popup targets {
    $beacon = %(id => "ssh-1");
    $user = "alice";
    menu("$user", lambda({
        insert_menu("ssh", @($bid));
    }, $bid => $beacon['id']));
}
`

func TestAuthoredMenubarSnippetsLoadThroughTypedProvider(t *testing.T) {
	provider := &recordingAggressorClientUIProvider{}
	runtimeInstance, err := New(
		WithFunction("getAggressorClientType", func(context.Context, Invocation) (Value, error) {
			return String("ui"), nil
		}),
		WithAggressorClientUIProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	registerFunctionMenuExtensions(t, runtimeInstance)

	tests := []struct {
		name        string
		source      string
		excerpt     string
		description string
		hook        string
	}{
		{
			name: authoredConditionalMenuSource, source: authoredConditionalMenuSource,
			excerpt:     authoredConditionalMenuSnippet,
			description: "Preferences", hook: "preferences_menu",
		},
		{
			name: authoredPayloadMenuSource, source: authoredPayloadMenuSource,
			excerpt:     authoredPayloadMenuSnippet,
			description: "Server-Side &Payloads", hook: "payloads_server",
		},
	}
	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			program, compileErr := CompileString(test.source, test.excerpt)
			if compileErr != nil {
				t.Fatal(compileErr)
			}
			script, loadErr := runtimeInstance.Load(context.Background(), program)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			t.Cleanup(func() { _ = script.Unload(context.Background()) })
		})
	}

	requests := provider.snapshot()
	if len(requests) != len(tests) {
		t.Fatalf("modern menubar requests = %d, want %d", len(requests), len(tests))
	}
	for index, request := range requests {
		test := tests[index]
		if request.Operation != AggressorClientUIMenubar || request.Name != "menubar" || request.Popup == nil ||
			len(request.Arguments) != 2 || request.Arguments[0].String() != test.description ||
			request.Arguments[1].String() != test.hook || request.Span.Source != test.source {
			t.Errorf("modern menubar request %d = %#v", index, request)
		}
	}
}

func TestAuthoredFunctionItemsComposeActivateAndUnload(t *testing.T) {
	provider := &recordingAggressorClientUIProvider{}
	var activated []string
	options := []Option{WithAggressorClientUIProvider(provider)}
	for _, name := range []string{
		"open_stager_dialog",
		"open_stageless_dialog",
		"open_download_dialog",
	} {
		name := name
		options = append(options, WithFunction(name, func(context.Context, Invocation) (Value, error) {
			activated = append(activated, name)
			return Null(), nil
		}))
	}
	runtimeInstance, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	registerFunctionMenuExtensions(t, runtimeInstance)
	program, err := CompileString(authoredPayloadMenuSource, authoredPayloadMenuSnippet)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	requests := provider.snapshot()
	if len(requests) != 1 || requests[0].Popup == nil {
		t.Fatalf("authored menubar request = %#v", requests)
	}
	composer := requests[0].Popup
	if err := composer.Compose(context.Background()); err != nil {
		t.Fatal(err)
	}
	items := runtimeInstance.Bindings(BindingItem, "")
	wantLabels := []string{"Stager Payload Generator", "Stageless Payload Generator", "Download payload"}
	labels := make([]string, len(items))
	for index, item := range items {
		labels[index] = item.Name
		assertBindingParent(t, item.Parent, BindingPopup, "payloads_server", []string{})
		if _, err := runtimeInstance.InvokeBindingByID(context.Background(), item.Script, item.ID); err != nil {
			t.Fatalf("activate %q: %v", item.Name, err)
		}
	}
	if !reflect.DeepEqual(labels, wantLabels) {
		t.Fatalf("authored function items = %q, want %q", labels, wantLabels)
	}
	wantActivated := []string{
		"open_stager_dialog",
		"open_stageless_dialog",
		"open_download_dialog",
	}
	if !reflect.DeepEqual(activated, wantActivated) {
		t.Fatalf("authored activations = %q, want %q", activated, wantActivated)
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if _, active := runtimeInstance.BindingByID(item.Script, item.ID); active {
			t.Errorf("item %q remained active after owner unload", item.Name)
		}
	}
	if err := composer.Compose(context.Background()); !errors.Is(err, ErrAggressorPopupStale) || !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("composer after unload = %v, want stale/script-unloaded", err)
	}
}

func TestAuthoredFunctionMenuComposesNestedPopup(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	registerFunctionMenuExtensions(t, runtimeInstance)
	program, err := CompileString(authoredDynamicMenuSource, authoredDynamicMenuSnippet)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })

	if _, err := runtimeInstance.DispatchPopupHook(context.Background(), "targets"); err != nil {
		t.Fatal(err)
	}
	menu := onlyBinding(t, runtimeInstance.Bindings(BindingMenu, "alice"))
	assertBindingParent(t, menu.Parent, BindingPopup, "targets", []string{})
	if _, err := runtimeInstance.InvokeBindingByID(context.Background(), menu.Script, menu.ID); err != nil {
		t.Fatal(err)
	}
	action := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "SSH Action"))
	if action.Parent == nil || action.Parent.Kind != BindingPopup || action.Parent.Name != "ssh" || len(action.Parent.Arguments) != 1 {
		t.Fatalf("inserted action popup parent = %#v", action.Parent)
	}
	beaconIDs, ok := action.Parent.Arguments[0].Array()
	beaconID, present := Null(), false
	if ok {
		beaconID, present = beaconIDs.Get(0)
	}
	if !ok || !present || beaconIDs.Len() != 1 || beaconID.String() != "ssh-1" {
		t.Fatalf("inserted action arguments = %#v", action.Parent.Arguments)
	}
	if action.Parent.Parent == nil || action.Parent.Parent.BindingID != menu.ID || action.Parent.Parent.Name != "alice" {
		t.Fatalf("inserted action submenu parent = %#v, want menu %d", action.Parent.Parent, menu.ID)
	}
	value, err := runtimeInstance.InvokeBindingByID(context.Background(), action.Script, action.ID, String("activated"))
	if err != nil || value.String() != "activated" {
		t.Fatalf("inserted action callback = (%s, %v), want activated", value.Describe(), err)
	}
}
