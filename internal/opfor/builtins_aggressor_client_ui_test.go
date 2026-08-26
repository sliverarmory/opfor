package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingAggressorClientUIProvider struct {
	mu       sync.Mutex
	requests []AggressorClientUIRequest
	handle   func(context.Context, AggressorClientUIRequest) (Value, error)
}

func (provider *recordingAggressorClientUIProvider) HandleAggressorClientUI(
	ctx context.Context,
	request AggressorClientUIRequest,
) (Value, error) {
	provider.mu.Lock()
	provider.requests = append(provider.requests, request)
	function := provider.handle
	provider.mu.Unlock()
	if function == nil {
		return Null(), nil
	}
	return function(ctx, request)
}

func (provider *recordingAggressorClientUIProvider) snapshot() []AggressorClientUIRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorClientUIRequest(nil), provider.requests...)
}

func TestAggressorClientUIFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorClientUIFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{
		"addTab", "addVisualization", "add_to_clipboard", "bbrowser", "colorMenu", "file_browser",
		"insert_color_menu", "insert_component", "menubar", "nextTab",
		"openAboutDialog", "openApplicationManager", "openAutoRunDialog", "openBeaconBrowser",
		"openBeaconConsole", "openBrowserPivotSetup", "openCloneSiteDialog", "openConnectDialog",
		"openCovertVPNSetup", "openCredentialManager", "openDefaultShortcutsDialog", "openDownloadBrowser",
		"openElevateDialog", "openEventLog", "openFileBrowser", "openGoldenTicketDialog",
		"openHTMLApplicationDialog", "openHostFileDialog", "openInterfaceManager", "openJavaSignedAppletDialog",
		"openJavaSmartAppletDialog", "openJobBrowser", "openJobConsole", "openJumpDialog",
		"openKeystrokeBrowser", "openListenerManager", "openMakeTokenDialog", "openMalleableProfileDialog",
		"openNewCredentialDialog", "openOfficeMacroDialog", "openOneLinerDialog", "openOrActivate",
		"openPayloadGeneratorDialog", "openPayloadGeneratorStageDialog", "openPayloadHelper",
		"openPayloadStoreManager", "openPivotListenerSetup", "openPortScanner", "openPortScannerLocal",
		"openPowerShellWebDialog", "openPreferencesDialog", "openProcessBrowser", "openSOCKSBrowser",
		"openSOCKSSetup", "openScreenshotBrowser", "openScriptConsole", "openScriptManager",
		"openScriptedWebDialog", "openServiceBrowser", "openSiteManager", "openSpawnAsDialog",
		"openSpawnDialog", "openSpearPhishDialog", "openSystemInformationDialog", "openSystemProfilerDialog",
		"openTargetBrowser", "openUserDefinedBrowser", "openWebLog", "openWindowsExecutableDialog",
		"openWindowsExecutableStageAllDialog", "openWindowsExecutableStageDialog",
		"pgraph", "popup_clear", "previousTab", "process_browser", "removeTab", "sbrowser", "separator",
		"showVisualization", "show_error", "show_message", "show_popup", "tbrowser", "url_open",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor client UI function names = %q, want %q", names, want)
	}
}

func TestAggressorClientUIOperationsArgumentsAndUnchangedResults(t *testing.T) {
	t.Parallel()

	component := ObjectValue(&struct{ name string }{name: "component"})
	resultValue := ObjectValue(&struct{ name string }{name: "provider result"})
	ids := ArrayValue(NewArray(String("B-1"), String("B-2")))
	tests := []struct {
		name      string
		operation AggressorClientUIOperation
		arguments []Value
		effect    bool
	}{
		{name: "addTab", operation: AggressorClientUIAddTab, arguments: []Value{String("Tab"), component, String("tip")}},
		{name: "addVisualization", operation: AggressorClientUIAddVisualization, arguments: []Value{String("Graph"), component}},
		{name: "showVisualization", operation: AggressorClientUIShowVisualization, arguments: []Value{String("Graph")}},
		{name: "show_popup", operation: AggressorClientUIShowPopup, arguments: []Value{ObjectValue(&struct{ event bool }{true}), String("missing"), component}},
		{name: "menubar", operation: AggressorClientUIMenubar, arguments: []Value{String("&Tools"), String("missing")}},
		{name: "popup_clear", operation: AggressorClientUIPopupClear, arguments: []Value{String("filebrowser")}, effect: true},
		{name: "separator", operation: AggressorClientUISeparator},
		{name: "removeTab", operation: AggressorClientUIRemoveTab},
		{name: "nextTab", operation: AggressorClientUINextTab},
		{name: "previousTab", operation: AggressorClientUIPreviousTab},
		{name: "add_to_clipboard", operation: AggressorClientUIAddToClipboard, arguments: []Value{String("copy")}},
		{name: "url_open", operation: AggressorClientUIOpenURL, arguments: []Value{String("https://example.invalid/")}},
		{name: "show_error", operation: AggressorClientUIShowError, arguments: []Value{String("failure")}},
		{name: "show_message", operation: AggressorClientUIShowMessage, arguments: []Value{String("message")}},
		{name: "bbrowser", operation: AggressorClientUIGenerateBeaconBrowser},
		{name: "colorMenu", operation: AggressorClientUIColorMenu, arguments: []Value{String("beacon"), ids}},
		{name: "file_browser", operation: AggressorClientUIFileBrowser, effect: true},
		{name: "insert_color_menu", operation: AggressorClientUIInsertColorMenu, arguments: []Value{component}, effect: true},
		{name: "insert_component", operation: AggressorClientUIInsertComponent, arguments: []Value{component}, effect: true},
		{name: "pgraph", operation: AggressorClientUIGeneratePivotGraph},
		{name: "process_browser", operation: AggressorClientUIProcessBrowser, effect: true},
		{name: "sbrowser", operation: AggressorClientUIGenerateSessionBrowser},
		{name: "tbrowser", operation: AggressorClientUIGenerateTargetBrowser},
	}

	provider := &recordingAggressorClientUIProvider{handle: func(context.Context, AggressorClientUIRequest) (Value, error) {
		return resultValue, nil
	}}
	runtimeInstance, err := New(WithAggressorClientUIProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, test := range tests {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		if test.effect {
			if invokeErr != nil || !result.IsNull() {
				t.Errorf("%s result = (%s, %v), want null/nil", test.name, result.Describe(), invokeErr)
			}
		} else if invokeErr != nil || !result.IdentityEqual(resultValue) {
			t.Errorf("%s result = (%s, %v), want unchanged provider object", test.name, result.Describe(), invokeErr)
		}
	}

	requests := provider.snapshot()
	if len(requests) != len(tests) {
		t.Fatalf("provider requests = %d, want %d", len(requests), len(tests))
	}
	for index, request := range requests {
		test := tests[index]
		if request.Operation != test.operation || request.Name != test.name || request.RuntimeID != runtimeInstance.ID() {
			t.Errorf("request %d metadata = %#v, want %q/%q/runtime %d", index, request, test.operation, test.name, runtimeInstance.ID())
		}
		if len(request.Arguments) != len(test.arguments) {
			t.Errorf("request %s arguments = %d, want %d", request.Name, len(request.Arguments), len(test.arguments))
			continue
		}
		for argumentIndex := range test.arguments {
			if !request.Arguments[argumentIndex].IdentityEqual(test.arguments[argumentIndex]) {
				t.Errorf("request %s argument %d = %s, want identity %s", request.Name, argumentIndex+1, request.Arguments[argumentIndex].Describe(), test.arguments[argumentIndex].Describe())
			}
		}
		if request.Name == "show_popup" && request.Popup != nil {
			t.Error("show_popup without a registered popup received a composer")
		}
		if (request.Operation == AggressorClientUIInsertColorMenu || request.Operation == AggressorClientUIInsertComponent) && request.Composition != nil {
			t.Errorf("%s outside popup/menu received composition %#v", request.Name, request.Composition)
		}
	}
}

func TestAggressorClientUIRejectsInvalidAritiesBeforeBoundaries(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorClientUIProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorClientUIProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for name, spec := range aggressorClientUISpecs {
		counts := []int{spec.maximum + 1}
		if spec.minimum != 0 {
			counts = append(counts, spec.minimum-1)
		}
		for _, count := range counts {
			result, invokeErr := runtimeInstance.Invoke(context.Background(), name, make([]Value, count)...)
			if invokeErr == nil || !result.IsNull() || !strings.Contains(invokeErr.Error(), "expected") {
				t.Errorf("%s/%d = (%s, %v), want arity error", name, count, result.Describe(), invokeErr)
			}
		}
	}
	if len(provider.snapshot()) != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid calls reached provider/Host: %d/%d", len(provider.snapshot()), hostCalls.Load())
	}
}

func TestAggressorClientUIResolvesReferencesOnceAndPreservesIdentity(t *testing.T) {
	t.Parallel()

	title := String("before")
	component := ObjectValue(&struct{ component bool }{true})
	tooltip := ArrayValue(NewArray(String("tip")))
	cells := []*Cell{NewCell(title), NewCell(component), NewCell(tooltip)}
	arguments := []Argument{
		{Name: "$title", Reference: cells[0]},
		{Name: "$component", Reference: cells[1]},
		{Name: "$tooltip", Reference: cells[2]},
	}
	var captured AggressorClientUIRequest
	provider := AggressorClientUIProviderFunc(func(_ context.Context, request AggressorClientUIRequest) (Value, error) {
		captured = request
		for _, cell := range cells {
			cell.Set(String("changed"))
		}
		request.Arguments[0] = String("provider-local slice mutation")
		return request.Arguments[1], nil
	})
	runtimeInstance, err := New(WithAggressorClientUIProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	span := Span{Source: "client-ui-values.cna", Start: Position{Line: 9, Column: 3}}
	result, err := runtimeInstance.aggressorClientUI(context.Background(), Invocation{
		Runtime: runtimeInstance, Script: 17, Name: "addTab", Arguments: arguments, Span: span,
	})
	if err != nil || !result.IdentityEqual(component) {
		t.Fatalf("addTab = (%s, %v), want original component identity", result.Describe(), err)
	}
	if captured.Operation != AggressorClientUIAddTab || captured.Name != "addTab" || captured.RuntimeID != runtimeInstance.ID() || captured.Script != 17 || captured.Span != span {
		t.Fatalf("request provenance = %#v", captured)
	}
	// The provider changed only its detached top-level request slice. Captured
	// Values still prove compound/object identity and source Cells changed only
	// through the explicit mutations above.
	if !captured.Arguments[1].IdentityEqual(component) || !captured.Arguments[2].IdentityEqual(tooltip) {
		t.Fatalf("captured argument identity = %s", ArrayValue(NewArray(captured.Arguments...)).Describe())
	}
	for index, cell := range cells {
		if cell.Get().String() != "changed" {
			t.Errorf("source cell %d = %s, want provider mutation", index, cell.Get().Describe())
		}
	}
}

func TestAggressorClientUIHostFallbackPreservesInvocationExactlyOnce(t *testing.T) {
	t.Parallel()

	cell := NewCell(ObjectValue(&struct{ component bool }{true}))
	span := Span{Source: "client-ui-host.cna", Start: Position{Line: 4, Column: 8}}
	original := Invocation{
		Script: 23, Name: "addTab", Span: span,
		Arguments: []Argument{{Value: String("Tab")}, {Name: "$component", Reference: cell}, {Value: String("tip")}},
	}
	wantResult := ObjectValue(&struct{ result bool }{true})
	wantErr := errors.New("host UI result")
	var captured Invocation
	var calls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls.Add(1)
		captured = invocation
		invocation.Arguments[1].Set(String("mutated by Host"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original.Runtime = runtimeInstance

	result, invokeErr := runtimeInstance.aggressorClientUI(context.Background(), original)
	if !errors.Is(invokeErr, wantErr) || !result.IdentityEqual(wantResult) || calls.Load() != 1 {
		t.Fatalf("Host fallback = (%s, %v), calls %d", result.Describe(), invokeErr, calls.Load())
	}
	if captured.Runtime != original.Runtime || captured.Script != original.Script || captured.Name != original.Name || captured.Span != original.Span || &captured.Arguments[0] != &original.Arguments[0] || captured.Arguments[1].Reference != cell {
		t.Fatalf("Host invocation changed: %#v", captured)
	}
	if cell.Get().String() != "mutated by Host" {
		t.Fatalf("Host lost reference capability: %s", cell.Get().Describe())
	}
}

func TestAggressorPopupClearHostFallbackClearsOnlyAfterSuccess(t *testing.T) {
	wantErr := errors.New("Host rejected popup clear")
	for _, test := range []struct {
		name    string
		hostErr error
		cleared bool
	}{
		{name: "success", cleared: true},
		{name: "error", hostErr: wantErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			nameCell := NewCell(String("clear_target"))
			var captured Invocation
			var calls atomic.Int32
			runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
				calls.Add(1)
				captured = invocation
				invocation.Arguments[0].Set(String("redirected"))
				return String("Host result"), test.hostErr
			})))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			script, err := runtimeInstance.Load(context.Background(), mustCompileClientUITest(t, "popup-clear-host.cna", `
popup clear_target {
	item "clear child" {
		return;
	}
}
popup redirected {
	item "keep child" {
		return;
	}
}
`))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = script.Unload(context.Background()) })
			if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingPopup, "clear_target", String("component")); err != nil {
				t.Fatal(err)
			}
			invocation := Invocation{
				Runtime: runtimeInstance,
				Script:  script.ID(),
				Name:    "popup_clear",
				Arguments: []Argument{{
					Name: "$name", Reference: nameCell,
				}},
			}
			result, invokeErr := runtimeInstance.aggressorClientUI(context.Background(), invocation)
			if !result.IdentityEqual(String("Host result")) || !errors.Is(invokeErr, test.hostErr) || calls.Load() != 1 {
				t.Fatalf("popup_clear fallback = (%s, %v), calls %d", result.Describe(), invokeErr, calls.Load())
			}
			if captured.Arguments[0].Reference != nameCell || nameCell.Get().String() != "redirected" {
				t.Fatalf("Host reference invocation = %#v / %s", captured, nameCell.Get().Describe())
			}
			roots := runtimeInstance.Bindings(BindingPopup, "clear_target")
			children := runtimeInstance.Bindings(BindingItem, "clear child")
			if test.cleared {
				if len(roots) != 0 || len(children) != 0 {
					t.Fatalf("successful Host clear left root/child %d/%d", len(roots), len(children))
				}
			} else if len(roots) != 1 || len(children) != 1 {
				t.Fatalf("failed Host clear changed root/child %d/%d", len(roots), len(children))
			}
			if redirected := runtimeInstance.Bindings(BindingPopup, "redirected"); len(redirected) != 1 {
				t.Fatalf("Host mutation redirected local clear; redirected roots = %d", len(redirected))
			}
		})
	}
}

func TestAggressorClientUIProviderErrorsAreAuthoritative(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("UI provider rejected request")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host"), nil
		})),
		WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(context.Context, AggressorClientUIRequest) (Value, error) {
			providerCalls.Add(1)
			return String("partial"), wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, invokeErr := runtimeInstance.Invoke(context.Background(), "show_message", String("hello"))
	if !errors.Is(invokeErr, wantErr) || !result.IsNull() || providerCalls.Load() != 1 || hostCalls.Load() != 0 {
		t.Fatalf("provider error = (%s, %v), provider/Host %d/%d", result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorClientUIWithFunctionPrecedenceAndNilPolicy(t *testing.T) {
	for name := range aggressorClientUISpecs {
		for _, overrideFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/override-first=%v", name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				providerOption := WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(context.Context, AggressorClientUIRequest) (Value, error) {
					providerCalls.Add(1)
					return String("provider"), nil
				}))
				overrideOption := WithFunction(name, func(context.Context, Invocation) (Value, error) {
					return String("override"), nil
				})
				options := []Option{providerOption, overrideOption}
				if overrideFirst {
					options = []Option{overrideOption, providerOption}
				}
				runtimeInstance, err := New(options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
				result, err := runtimeInstance.Invoke(context.Background(), name, String("invalid stock arity"))
				if err != nil || result.String() != "override" || providerCalls.Load() != 0 {
					t.Fatalf("override = (%s, %v), provider calls %d", result.Describe(), err, providerCalls.Load())
				}
			})
		}
	}

	var typedNil *recordingAggressorClientUIProvider
	if _, err := New(WithAggressorClientUIProvider(typedNil)); err == nil {
		t.Fatal("typed-nil Aggressor client UI provider was accepted")
	}
	var nilFunction AggressorClientUIProviderFunc
	if _, err := New(WithAggressorClientUIProvider(nilFunction)); err == nil {
		t.Fatal("nil Aggressor client UI provider function was accepted")
	}
	if result, err := nilFunction.HandleAggressorClientUI(context.Background(), AggressorClientUIRequest{}); err == nil || !result.IsNull() {
		t.Fatalf("nil provider function = (%s, %v), want null/error", result.Describe(), err)
	}
}

func TestAggressorShowPopupComposerIsPinnedAndLifecycleBound(t *testing.T) {
	component := ObjectValue(&struct{ label string }{label: "exact component"})
	var request AggressorClientUIRequest
	var popupCalls []string
	var popupCallsMu sync.Mutex
	runtimeInstance, err := New(
		WithInitialGlobals(map[string]Value{"component": component}),
		WithFunction("record_popup", func(_ context.Context, invocation Invocation) (Value, error) {
			values := invocation.Values()
			if len(values) != 2 || !values[0].IdentityEqual(component) {
				return Null(), fmt.Errorf("record_popup arguments = %s", ArrayValue(NewArray(values...)).Describe())
			}
			popupCallsMu.Lock()
			popupCalls = append(popupCalls, values[1].String())
			popupCallsMu.Unlock()
			return Null(), nil
		}),
		WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(_ context.Context, candidate AggressorClientUIRequest) (Value, error) {
			request = candidate
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("popup-composer.cna", `
popup exact_popup {
	record_popup($1, "first");
}
popup exact_popup {
	record_popup($1, "second");
}
show_popup("event", "exact_popup", $component);
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if request.Popup == nil || request.Operation != AggressorClientUIShowPopup || request.Script != script.ID() || request.RuntimeID != runtimeInstance.ID() {
		t.Fatalf("show_popup request = %#v", request)
	}
	if !request.Arguments[2].IdentityEqual(component) {
		t.Fatalf("show_popup component = %s, want exact identity", request.Arguments[2].Describe())
	}
	if err := request.Popup.Compose(context.Background()); err != nil {
		t.Fatalf("Compose: %v", err)
	}
	popupCallsMu.Lock()
	gotPopupCalls := append([]string(nil), popupCalls...)
	popupCallsMu.Unlock()
	if !reflect.DeepEqual(gotPopupCalls, []string{"first", "second"}) {
		t.Fatalf("popup binding order = %q, want first/second", gotPopupCalls)
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	replacement, err := runtimeInstance.Load(context.Background(), mustCompileClientUITest(t, "popup-replacement.cna", `
popup exact_popup {
	return "replacement";
}
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replacement.Unload(context.Background()) })
	if err := request.Popup.Compose(context.Background()); !errors.Is(err, ErrAggressorPopupStale) || !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("composer after binding-owner unload = %v, want stale/script-unloaded", err)
	}
}

func TestAggressorPopupClearStalesCapturedGenerationWithoutRetargeting(t *testing.T) {
	t.Parallel()

	component := ObjectValue(&struct{ label string }{label: "captured"})
	var composer AggressorPopupComposer
	var replacementCalls atomic.Int32
	runtimeInstance, err := New(
		WithInitialGlobals(map[string]Value{"component": component}),
		WithFunction("replacement_popup", func(context.Context, Invocation) (Value, error) {
			replacementCalls.Add(1)
			return Null(), nil
		}),
		WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(_ context.Context, request AggressorClientUIRequest) (Value, error) {
			if request.Operation == AggressorClientUIShowPopup {
				composer = request.Popup
			}
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script, err := runtimeInstance.Load(context.Background(), mustCompileClientUITest(t, "popup-clear-generation.cna", `
popup generation {
	replacement_popup();
}
show_popup("event", "generation", $component);
popup_clear("generation");
popup generation {
	replacement_popup();
}
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	if composer == nil {
		t.Fatal("show_popup did not capture its original generation")
	}
	bindings := runtimeInstance.Bindings(BindingPopup, "generation")
	if len(bindings) != 1 {
		t.Fatalf("post-clear popup bindings = %d, want replacement only", len(bindings))
	}
	if err := composer.Compose(context.Background()); !errors.Is(err, ErrAggressorPopupStale) {
		t.Fatalf("pre-clear composer error = %v, want ErrAggressorPopupStale", err)
	}
	if replacementCalls.Load() != 0 {
		t.Fatalf("stale composer invoked replacement %d time(s)", replacementCalls.Load())
	}
}

func TestAggressorPopupClearRemovesComposedDescendants(t *testing.T) {
	t.Parallel()

	var composer AggressorPopupComposer
	providerResult := ObjectValue(&struct{ synchronous bool }{synchronous: true})
	runtimeInstance, err := New(WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(
		_ context.Context,
		request AggressorClientUIRequest,
	) (Value, error) {
		if request.Operation == AggressorClientUIShowPopup {
			composer = request.Popup
		}
		return providerResult, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script, err := runtimeInstance.Load(context.Background(), mustCompileClientUITest(t, "popup-clear-descendants.cna", `
popup clear_tree {
	item "child" {
		return;
	}
}
show_popup("event", "clear_tree", "component");
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	if composer == nil {
		t.Fatal("show_popup did not provide a composer")
	}
	if err := composer.Compose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if roots, children := runtimeInstance.Bindings(BindingPopup, "clear_tree"), runtimeInstance.Bindings(BindingItem, "child"); len(roots) != 1 || len(children) != 1 {
		t.Fatalf("composed root/child counts = %d/%d, want 1/1", len(roots), len(children))
	}
	result, err := runtimeInstance.Invoke(context.Background(), "popup_clear", String("clear_tree"))
	if err != nil || !result.IsNull() {
		t.Fatalf("popup_clear typed effect result = (%s, %v), want null/nil", result.Describe(), err)
	}
	if roots, children := runtimeInstance.Bindings(BindingPopup, "clear_tree"), runtimeInstance.Bindings(BindingItem, "child"); len(roots) != 0 || len(children) != 0 {
		t.Fatalf("post-clear root/child counts = %d/%d, want 0/0", len(roots), len(children))
	}
	if err := composer.Compose(context.Background()); !errors.Is(err, ErrAggressorPopupStale) {
		t.Fatalf("composer after descendant clear = %v, want ErrAggressorPopupStale", err)
	}
}

func TestAggressorPopupClearProviderErrorIsAuthoritativeAndPreservesBindings(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("client rejected popup clear")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("unexpected Host result"), nil
		})),
		WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(
			_ context.Context,
			request AggressorClientUIRequest,
		) (Value, error) {
			providerCalls.Add(1)
			if request.Operation != AggressorClientUIPopupClear || len(request.Arguments) != 1 ||
				request.Arguments[0].String() != "preserved" {
				return Null(), fmt.Errorf("unexpected popup_clear request: %#v", request)
			}
			return ObjectValue(&struct{ ignored bool }{ignored: true}), wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script, err := runtimeInstance.Load(context.Background(), mustCompileClientUITest(t, "popup-clear-provider-error.cna", `
popup preserved {
	item "still registered" {
		return;
	}
}
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })

	result, invokeErr := runtimeInstance.Invoke(context.Background(), "popup_clear", String("preserved"))
	if !result.IsNull() || !errors.Is(invokeErr, wantErr) || providerCalls.Load() != 1 || hostCalls.Load() != 0 {
		t.Fatalf("popup_clear provider failure = (%s, %v), provider/Host calls %d/%d",
			result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
	}
	if roots := runtimeInstance.Bindings(BindingPopup, "preserved"); len(roots) != 1 {
		t.Fatalf("failed provider clear changed popup bindings: %d", len(roots))
	}
}

func TestAggressorPopupComposerSeparatesCallerAndBindingOwnerLifetimes(t *testing.T) {
	t.Parallel()

	component := ObjectValue(&struct{ label string }{label: "cross-owner"})
	var composer AggressorPopupComposer
	var requestScript ScriptID
	var calls atomic.Int32
	runtimeInstance, err := New(
		WithInitialGlobals(map[string]Value{"component": component}),
		WithFunction("cross_owner_popup", func(_ context.Context, invocation Invocation) (Value, error) {
			if !invocation.Arg(0).IdentityEqual(component) {
				return Null(), fmt.Errorf("popup component = %s", invocation.Arg(0).Describe())
			}
			calls.Add(1)
			return Null(), nil
		}),
		WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(_ context.Context, request AggressorClientUIRequest) (Value, error) {
			if request.Operation == AggressorClientUIShowPopup {
				composer = request.Popup
				requestScript = request.Script
			}
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner, err := runtimeInstance.Load(context.Background(), mustCompileClientUITest(t, "popup-owner.cna", `
popup shared_popup {
	cross_owner_popup($1);
}
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Unload(context.Background()) })
	caller, err := runtimeInstance.Load(context.Background(), mustCompileClientUITest(t, "popup-caller.cna", `
show_popup("event", "shared_popup", $component);
`))
	if err != nil {
		t.Fatal(err)
	}
	if composer == nil || requestScript != caller.ID() || requestScript == owner.ID() {
		t.Fatalf("caller/owner provenance = request %d caller %d owner %d", requestScript, caller.ID(), owner.ID())
	}
	if err := composer.Compose(context.Background()); err != nil || calls.Load() != 1 {
		t.Fatalf("cross-owner Compose = %v, calls %d", err, calls.Load())
	}
	if err := caller.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := composer.Compose(context.Background()); !errors.Is(err, ErrAggressorPopupStale) || !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("composer after caller unload = %v, want stale/script-unloaded", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("stale cross-owner composer invoked binding; calls %d", calls.Load())
	}
}

func TestAggressorMenuInsertionHelpersCarryDetachedComposition(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorClientUIProvider{}
	runtimeInstance, err := New(WithAggressorClientUIProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script, err := runtimeInstance.Load(context.Background(), mustCompileClientUITest(t, "menu-helper-composition.cna", `
popup root {
	separator();
	insert_color_menu($1);
	insert_component($1);
}
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	component := ObjectValue(&struct{ menu bool }{true})
	if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingPopup, "root", component); err != nil {
		t.Fatal(err)
	}
	requests := provider.snapshot()
	if len(requests) != 3 {
		t.Fatalf("menu-helper requests = %d, want three", len(requests))
	}
	wantOperations := []AggressorClientUIOperation{
		AggressorClientUISeparator,
		AggressorClientUIInsertColorMenu,
		AggressorClientUIInsertComponent,
	}
	for index, request := range requests {
		composition := request.Composition
		if request.Operation != wantOperations[index] || composition == nil || composition.Kind != BindingPopup ||
			composition.Name != "root" || composition.Script != script.ID() || composition.BindingID == 0 ||
			len(composition.Arguments) != 1 || !composition.Arguments[0].IdentityEqual(component) {
			t.Fatalf("menu-helper request %d = %#v", index, request)
		}
		if index != 0 && (len(request.Arguments) != 1 || !request.Arguments[0].IdentityEqual(component)) {
			t.Fatalf("menu-helper request %d arguments = %#v", index, request.Arguments)
		}
	}
	requests[0].Composition.Arguments[0] = String("provider-local mutation")
	if !requests[1].Composition.Arguments[0].IdentityEqual(component) || !requests[2].Composition.Arguments[0].IdentityEqual(component) {
		t.Fatal("one provider-local composition mutation affected a sibling request")
	}
	if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingPopup, "root", component); err != nil {
		t.Fatal(err)
	}
	requests = provider.snapshot()
	if len(requests) != 6 || requests[3].Composition == nil || !requests[3].Composition.Arguments[0].IdentityEqual(component) {
		t.Fatalf("second detached composition = %#v", requests)
	}
}

func TestAggressorClientUIHelperHostFallbackPreservesRawInvocation(t *testing.T) {
	t.Parallel()

	componentCell := NewCell(ObjectValue(&struct{ component bool }{true}))
	span := Span{Source: "client-ui-helper-host.cna", Start: Position{Line: 11, Column: 6}}
	wantResult := ObjectValue(&struct{ result bool }{true})
	wantErr := errors.New("helper Host result")
	var captured Invocation
	var calls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls.Add(1)
		captured = invocation
		invocation.Arguments[0].Set(String("mutated by Host"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original := Invocation{
		Runtime: runtimeInstance,
		Script:  91,
		Name:    "insert_component",
		Span:    span,
		Arguments: []Argument{{
			Name: "$component", Reference: componentCell,
		}},
	}

	result, invokeErr := runtimeInstance.aggressorClientUI(context.Background(), original)
	if !result.IdentityEqual(wantResult) || !errors.Is(invokeErr, wantErr) || calls.Load() != 1 ||
		captured.Runtime != original.Runtime || captured.Script != original.Script || captured.Name != original.Name ||
		captured.Span != original.Span || captured.Arguments[0].Reference != componentCell ||
		componentCell.Get().String() != "mutated by Host" {
		t.Fatalf("helper Host fallback = (%s, %v), calls %d, invocation %#v, cell %s",
			result.Describe(), invokeErr, calls.Load(), captured, componentCell.Get().Describe())
	}
}

func TestAggressorClientUIHelpersSupportConcurrentOpaqueComponents(t *testing.T) {
	t.Parallel()

	const calls = 20
	entered := make(chan struct{}, calls)
	release := make(chan struct{})
	provider := &recordingAggressorClientUIProvider{handle: func(_ context.Context, request AggressorClientUIRequest) (Value, error) {
		if request.Operation != AggressorClientUIColorMenu || len(request.Arguments) != 2 {
			return Null(), fmt.Errorf("unexpected concurrent helper request %#v", request)
		}
		entered <- struct{}{}
		<-release
		return request.Arguments[1], nil
	}}
	runtimeInstance, err := New(WithAggressorClientUIProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	ids := make([]Value, calls)
	errorsByCall := make(chan error, calls)
	var wait sync.WaitGroup
	for index := 0; index < calls; index++ {
		index := index
		ids[index] = ArrayValue(NewArray(Int(int32(index))))
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, invokeErr := runtimeInstance.Invoke(
				context.Background(), "colorMenu", String(fmt.Sprintf("group-%d", index)), ids[index],
			)
			if invokeErr == nil && !result.IdentityEqual(ids[index]) {
				invokeErr = fmt.Errorf("result %s lost component identity %s", result.Describe(), ids[index].Describe())
			}
			errorsByCall <- invokeErr
		}()
	}
	for index := 0; index < calls; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatalf("only %d/%d helper provider calls entered concurrently", index, calls)
		}
	}
	close(release)
	wait.Wait()
	close(errorsByCall)
	for invokeErr := range errorsByCall {
		if invokeErr != nil {
			t.Fatal(invokeErr)
		}
	}
	requests := provider.snapshot()
	if len(requests) != calls {
		t.Fatalf("concurrent helper requests = %d, want %d", len(requests), calls)
	}
	seen := make(map[*Array]struct{}, calls)
	for _, request := range requests {
		array, ok := request.Arguments[1].Array()
		if !ok {
			t.Fatalf("concurrent helper IDs = %s, want array", request.Arguments[1].Describe())
		}
		seen[array] = struct{}{}
	}
	if len(seen) != calls {
		t.Fatalf("concurrent opaque identities = %d, want %d", len(seen), calls)
	}
}

func TestPortableScriptLoaderInheritsAggressorClientUIProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-client-ui.cna")
	if err := os.WriteFile(childPath, []byte(`bbrowser(); process_browser();`), 0o600); err != nil {
		t.Fatal(err)
	}
	program := mustCompileClientUITest(t, "parent-client-ui.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
show_message("parent");
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
[$child runScript];
`, filepath.ToSlash(childPath)))
	provider := &recordingAggressorClientUIProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader client UI route reached Host")
		})),
		WithAggressorClientUIProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	requests := provider.snapshot()
	if hostCalls.Load() != 0 || len(requests) != 3 {
		t.Fatalf("provider/Host requests = %d/%d, want 3/0", len(requests), hostCalls.Load())
	}
	if requests[0].Operation != AggressorClientUIShowMessage ||
		requests[1].Operation != AggressorClientUIGenerateBeaconBrowser ||
		requests[2].Operation != AggressorClientUIProcessBrowser ||
		requests[0].RuntimeID != runtimeInstance.ID() || requests[1].RuntimeID == 0 ||
		requests[1].RuntimeID == requests[0].RuntimeID || requests[2].RuntimeID != requests[1].RuntimeID ||
		requests[0].Script != 1 || requests[1].Script != 1 || requests[2].Script != 1 ||
		requests[0].Span.Source != "parent-client-ui.cna" ||
		requests[1].Span.Source != filepath.ToSlash(childPath) || requests[2].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child UI provenance = %#v", requests)
	}
}

func mustCompileClientUITest(t *testing.T, name, source string) *Program {
	t.Helper()
	program, err := CompileString(name, source)
	if err != nil {
		t.Fatal(err)
	}
	return program
}
