package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type aggressorOpenUISpecExpectation struct {
	name      string
	operation AggressorClientUIOperation
	minimum   int
	maximum   int
}

var aggressorOpenUISpecExpectations = []aggressorOpenUISpecExpectation{
	{name: "openAboutDialog", operation: AggressorClientUIOpenAboutDialog, minimum: 0, maximum: 0},
	{name: "openApplicationManager", operation: AggressorClientUIOpenApplicationManager, minimum: 0, maximum: 0},
	{name: "openAutoRunDialog", operation: AggressorClientUIOpenAutoRunDialog, minimum: 0, maximum: 0},
	{name: "openBeaconBrowser", operation: AggressorClientUIOpenBeaconBrowser, minimum: 0, maximum: 0},
	{name: "openBeaconConsole", operation: AggressorClientUIOpenBeaconConsole, minimum: 1, maximum: 1},
	{name: "openBrowserPivotSetup", operation: AggressorClientUIOpenBrowserPivotSetup, minimum: 1, maximum: 1},
	{name: "openCloneSiteDialog", operation: AggressorClientUIOpenCloneSiteDialog, minimum: 0, maximum: 0},
	{name: "openConnectDialog", operation: AggressorClientUIOpenConnectDialog, minimum: 0, maximum: 0},
	{name: "openCovertVPNSetup", operation: AggressorClientUIOpenCovertVPNSetup, minimum: 1, maximum: 1},
	{name: "openCredentialManager", operation: AggressorClientUIOpenCredentialManager, minimum: 0, maximum: 0},
	{name: "openDefaultShortcutsDialog", operation: AggressorClientUIOpenDefaultShortcutsDialog, minimum: 0, maximum: 0},
	{name: "openDownloadBrowser", operation: AggressorClientUIOpenDownloadBrowser, minimum: 0, maximum: 0},
	{name: "openElevateDialog", operation: AggressorClientUIOpenElevateDialog, minimum: 1, maximum: 1},
	{name: "openEventLog", operation: AggressorClientUIOpenEventLog, minimum: 0, maximum: 0},
	{name: "openFileBrowser", operation: AggressorClientUIOpenFileBrowser, minimum: 1, maximum: 1},
	{name: "openGoldenTicketDialog", operation: AggressorClientUIOpenGoldenTicketDialog, minimum: 1, maximum: 1},
	{name: "openHTMLApplicationDialog", operation: AggressorClientUIOpenHTMLApplicationDialog, minimum: 0, maximum: 0},
	{name: "openHostFileDialog", operation: AggressorClientUIOpenHostFileDialog, minimum: 0, maximum: 0},
	{name: "openInterfaceManager", operation: AggressorClientUIOpenInterfaceManager, minimum: 0, maximum: 0},
	{name: "openJavaSignedAppletDialog", operation: AggressorClientUIOpenJavaSignedAppletDialog, minimum: 0, maximum: 0},
	{name: "openJavaSmartAppletDialog", operation: AggressorClientUIOpenJavaSmartAppletDialog, minimum: 0, maximum: 0},
	{name: "openJobBrowser", operation: AggressorClientUIOpenJobBrowser, minimum: 0, maximum: 1},
	{name: "openJobConsole", operation: AggressorClientUIOpenJobConsole, minimum: 2, maximum: 2},
	{name: "openJumpDialog", operation: AggressorClientUIOpenJumpDialog, minimum: 2, maximum: 2},
	{name: "openKeystrokeBrowser", operation: AggressorClientUIOpenKeystrokeBrowser, minimum: 0, maximum: 0},
	{name: "openListenerManager", operation: AggressorClientUIOpenListenerManager, minimum: 0, maximum: 0},
	{name: "openMakeTokenDialog", operation: AggressorClientUIOpenMakeTokenDialog, minimum: 1, maximum: 1},
	{name: "openMalleableProfileDialog", operation: AggressorClientUIOpenMalleableProfileDialog, minimum: 0, maximum: 0},
	{name: "openNewCredentialDialog", operation: AggressorClientUIOpenNewCredentialDialog, minimum: 1, maximum: 1},
	{name: "openOfficeMacroDialog", operation: AggressorClientUIOpenOfficeMacroDialog, minimum: 0, maximum: 0},
	{name: "openOneLinerDialog", operation: AggressorClientUIOpenOneLinerDialog, minimum: 1, maximum: 1},
	{name: "openOrActivate", operation: AggressorClientUIOpenOrActivate, minimum: 1, maximum: 1},
	{name: "openPayloadGeneratorDialog", operation: AggressorClientUIOpenPayloadGeneratorDialog, minimum: 0, maximum: 0},
	{name: "openPayloadGeneratorStageDialog", operation: AggressorClientUIOpenPayloadGeneratorStageDialog, minimum: 0, maximum: 0},
	{name: "openPayloadHelper", operation: AggressorClientUIOpenPayloadHelper, minimum: 1, maximum: 1},
	{name: "openPayloadStoreManager", operation: AggressorClientUIOpenPayloadStoreManager, minimum: 0, maximum: 0},
	{name: "openPivotListenerSetup", operation: AggressorClientUIOpenPivotListenerSetup, minimum: 1, maximum: 1},
	{name: "openPortScanner", operation: AggressorClientUIOpenPortScanner, minimum: 1, maximum: 1},
	{name: "openPortScannerLocal", operation: AggressorClientUIOpenPortScannerLocal, minimum: 1, maximum: 1},
	{name: "openPowerShellWebDialog", operation: AggressorClientUIOpenPowerShellWebDialog, minimum: 0, maximum: 0},
	{name: "openPreferencesDialog", operation: AggressorClientUIOpenPreferencesDialog, minimum: 0, maximum: 0},
	{name: "openProcessBrowser", operation: AggressorClientUIOpenProcessBrowser, minimum: 1, maximum: 1},
	{name: "openSOCKSBrowser", operation: AggressorClientUIOpenSOCKSBrowser, minimum: 0, maximum: 0},
	{name: "openSOCKSSetup", operation: AggressorClientUIOpenSOCKSSetup, minimum: 1, maximum: 1},
	{name: "openScreenshotBrowser", operation: AggressorClientUIOpenScreenshotBrowser, minimum: 0, maximum: 0},
	{name: "openScriptConsole", operation: AggressorClientUIOpenScriptConsole, minimum: 0, maximum: 0},
	{name: "openScriptManager", operation: AggressorClientUIOpenScriptManager, minimum: 0, maximum: 0},
	{name: "openScriptedWebDialog", operation: AggressorClientUIOpenScriptedWebDialog, minimum: 0, maximum: 0},
	{name: "openServiceBrowser", operation: AggressorClientUIOpenServiceBrowser, minimum: 1, maximum: 1},
	{name: "openSiteManager", operation: AggressorClientUIOpenSiteManager, minimum: 0, maximum: 0},
	{name: "openSpawnAsDialog", operation: AggressorClientUIOpenSpawnAsDialog, minimum: 1, maximum: 1},
	{name: "openSpawnDialog", operation: AggressorClientUIOpenSpawnDialog, minimum: 1, maximum: 1},
	{name: "openSpearPhishDialog", operation: AggressorClientUIOpenSpearPhishDialog, minimum: 0, maximum: 0},
	{name: "openSystemInformationDialog", operation: AggressorClientUIOpenSystemInformationDialog, minimum: 0, maximum: 0},
	{name: "openSystemProfilerDialog", operation: AggressorClientUIOpenSystemProfilerDialog, minimum: 0, maximum: 0},
	{name: "openTargetBrowser", operation: AggressorClientUIOpenTargetBrowser, minimum: 0, maximum: 0},
	{name: "openUserDefinedBrowser", operation: AggressorClientUIOpenUserDefinedBrowser, minimum: 4, maximum: 6},
	{name: "openWebLog", operation: AggressorClientUIOpenWebLog, minimum: 0, maximum: 0},
	{name: "openWindowsExecutableDialog", operation: AggressorClientUIOpenWindowsExecutableDialog, minimum: 0, maximum: 0},
	{name: "openWindowsExecutableStageAllDialog", operation: AggressorClientUIOpenWindowsExecutableStageAllDialog, minimum: 0, maximum: 0},
	{name: "openWindowsExecutableStageDialog", operation: AggressorClientUIOpenWindowsExecutableStageDialog, minimum: 0, maximum: 0},
}

func TestAggressorOpenClientUISpecsMatchCurrentReference(t *testing.T) {
	t.Parallel()

	if len(aggressorOpenUISpecExpectations) != 61 {
		t.Fatalf("open* expectation count = %d, want 61", len(aggressorOpenUISpecExpectations))
	}
	gotNames := make([]string, 0, len(aggressorOpenUISpecExpectations))
	for _, want := range aggressorOpenUISpecExpectations {
		spec, exists := aggressorClientUISpecs[want.name]
		if !exists {
			t.Errorf("%s has no native client UI spec", want.name)
			continue
		}
		if spec.operation != want.operation || spec.minimum != want.minimum || spec.maximum != want.maximum {
			t.Errorf("%s spec = %#v, want operation %q arity %d..%d",
				want.name, spec, want.operation, want.minimum, want.maximum)
		}
		if string(spec.operation) != want.name {
			t.Errorf("%s operation spelling = %q", want.name, spec.operation)
		}
		gotNames = append(gotNames, want.name)
	}
	sort.Strings(gotNames)
	if len(gotNames) != 61 {
		t.Fatalf("native open* names = %d, want 61", len(gotNames))
	}
	for _, removed := range []string{"openBypassUACDialog", "openWindowsDropperDialog"} {
		if _, exists := aggressorClientUISpecs[removed]; exists {
			t.Errorf("removed function %s was exposed as a current typed client operation", removed)
		}
	}
}

func TestAggressorOpenClientUIProviderReceivesEveryResolvedCommandAndResult(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorClientUIProvider{}
	resultValue := ObjectValue(&struct{ providerResult bool }{true})
	provider.handle = func(context.Context, AggressorClientUIRequest) (Value, error) {
		return resultValue, nil
	}
	runtimeInstance, err := New(WithAggressorClientUIProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	type expectedRequest struct {
		spec      aggressorOpenUISpecExpectation
		arguments []Value
	}
	wants := make([]expectedRequest, 0, len(aggressorOpenUISpecExpectations)+2)
	for _, expectation := range aggressorOpenUISpecExpectations {
		if expectation.name == "openPayloadHelper" {
			continue
		}
		for count := expectation.minimum; count <= expectation.maximum; count++ {
			arguments := make([]Value, count)
			for index := range arguments {
				arguments[index] = ObjectValue(&struct {
					name  string
					index int
				}{name: expectation.name, index: index})
			}
			result, invokeErr := runtimeInstance.Invoke(context.Background(), expectation.name, arguments...)
			if invokeErr != nil || !result.IdentityEqual(resultValue) {
				t.Errorf("%s/%d = (%s, %v), want unchanged provider result",
					expectation.name, count, result.Describe(), invokeErr)
			}
			wants = append(wants, expectedRequest{spec: expectation, arguments: arguments})
		}
	}

	requests := provider.snapshot()
	if len(requests) != len(wants) {
		t.Fatalf("open* provider requests = %d, want %d", len(requests), len(wants))
	}
	for index, request := range requests {
		want := wants[index]
		if request.Name != want.spec.name || request.Operation != want.spec.operation ||
			request.RuntimeID != runtimeInstance.ID() || request.Script != 0 || request.Span != (Span{}) {
			t.Errorf("request %d metadata = %#v, want %s/%s/runtime %d",
				index, request, want.spec.name, want.spec.operation, runtimeInstance.ID())
		}
		if request.Callback != nil || request.Popup != nil || request.Composition != nil {
			t.Errorf("request %s received unrelated capabilities", request.Name)
		}
		if len(request.Arguments) != len(want.arguments) {
			t.Errorf("request %s arguments = %d, want %d", request.Name, len(request.Arguments), len(want.arguments))
			continue
		}
		for argumentIndex := range want.arguments {
			if !request.Arguments[argumentIndex].IdentityEqual(want.arguments[argumentIndex]) {
				t.Errorf("request %s argument %d lost Value identity", request.Name, argumentIndex+1)
			}
		}
	}
}

func TestAggressorOpenPayloadHelperRetainsCallbackABIAndLifecycle(t *testing.T) {
	var captured AggressorClientUIRequest
	wantProviderResult := ObjectValue(&struct{ chooser bool }{true})
	runtimeInstance, err := New(WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(
		_ context.Context,
		request AggressorClientUIRequest,
	) (Value, error) {
		captured = request
		return wantProviderResult, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	program := mustCompileClientUITest(t, "open-payload-helper.cna", `
$helper_result = openPayloadHelper({
	return "selected:" . $1;
});
`)
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Name != "openPayloadHelper" || captured.Operation != AggressorClientUIOpenPayloadHelper ||
		captured.RuntimeID != runtimeInstance.ID() || captured.Script != script.ID() ||
		captured.Span.Source != "open-payload-helper.cna" || captured.Callback == nil || len(captured.Arguments) != 0 {
		t.Fatalf("payload-helper request = %#v", captured)
	}
	if result := script.Get("$helper_result"); !result.IsNull() {
		t.Fatalf("payload-helper synchronous result = %s, want $null after provider success", result.Describe())
	}
	for _, listener := range []string{"first", "second"} {
		result, callbackErr := captured.Callback.Invoke(context.Background(), String(listener))
		if callbackErr != nil || result.String() != "selected:"+listener {
			t.Fatalf("payload-helper callback %q = (%s, %v)", listener, result.Describe(), callbackErr)
		}
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if result, callbackErr := captured.Callback.Invoke(canceled, String("ignored")); !result.IsNull() || !errors.Is(callbackErr, context.Canceled) {
		t.Fatalf("canceled payload-helper callback = (%s, %v)", result.Describe(), callbackErr)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result, callbackErr := captured.Callback.Invoke(context.Background(), String("ignored")); !result.IsNull() || !errors.Is(callbackErr, ErrScriptUnloaded) {
		t.Fatalf("unloaded payload-helper callback = (%s, %v)", result.Describe(), callbackErr)
	}

	var closeCaptured AggressorClientUIRequest
	closeRuntime, err := New(WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(
		_ context.Context,
		request AggressorClientUIRequest,
	) (Value, error) {
		closeCaptured = request
		return Null(), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	closeProgram := mustCompileClientUITest(t, "open-payload-helper-close.cna", `
openPayloadHelper({ return $1; });
`)
	if _, err := closeRuntime.Load(context.Background(), closeProgram); err != nil {
		t.Fatal(err)
	}
	if closeCaptured.Callback == nil {
		t.Fatal("runtime-close payload-helper request has no callback")
	}
	if err := closeRuntime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result, callbackErr := closeCaptured.Callback.Invoke(context.Background(), String("ignored")); !result.IsNull() || !errors.Is(callbackErr, ErrScriptUnloaded) {
		t.Fatalf("runtime-closed payload-helper callback = (%s, %v)", result.Describe(), callbackErr)
	}
}

func TestAggressorOpenPayloadHelperValidatesCallbackOnlyOnTypedRoute(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorClientUIProvider{}
	runtimeInstance, err := New(WithAggressorClientUIProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, invokeErr := runtimeInstance.Invoke(context.Background(), "openPayloadHelper", String("not callable"))
	if invokeErr == nil || !result.IsNull() || !errors.Is(invokeErr, ErrInvalidCallable) ||
		!strings.Contains(invokeErr.Error(), "argument 1") || len(provider.snapshot()) != 0 {
		t.Fatalf("typed invalid callback = (%s, %v), provider requests %d",
			result.Describe(), invokeErr, len(provider.snapshot()))
	}

	cell := NewCell(String("raw callback-shaped value"))
	wantResult := ObjectValue(&struct{ hostResult bool }{true})
	wantErr := errors.New("raw Host result")
	var captured Invocation
	hostRuntime, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		captured = invocation
		invocation.Arguments[0].Set(String("mutated by Host"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hostRuntime.Close(context.Background()) })
	invocation := Invocation{
		Runtime: hostRuntime, Script: 71, Name: "openPayloadHelper",
		Arguments: []Argument{{Name: "$callback", Reference: cell}},
		Span:      Span{Source: "raw-open-helper.cna", Start: Position{Line: 7, Column: 4}},
	}
	result, invokeErr = hostRuntime.aggressorClientUI(context.Background(), invocation)
	if !result.IdentityEqual(wantResult) || !errors.Is(invokeErr, wantErr) ||
		captured.Runtime != invocation.Runtime || captured.Script != invocation.Script ||
		captured.Span != invocation.Span || captured.Arguments[0].Reference != cell ||
		cell.Get().String() != "mutated by Host" {
		t.Fatalf("raw payload-helper Host route = (%s, %v), invocation %#v, cell %s",
			result.Describe(), invokeErr, captured, cell.Get().Describe())
	}
}

func TestAggressorOpenClientUIProviderErrorAndCancellationAreAuthoritative(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("open UI provider rejected request")
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
	result, invokeErr := runtimeInstance.Invoke(context.Background(), "openAboutDialog")
	if !result.IsNull() || !errors.Is(invokeErr, wantErr) || providerCalls.Load() != 1 || hostCalls.Load() != 0 {
		t.Fatalf("authoritative open UI error = (%s, %v), provider/Host %d/%d",
			result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
	}

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	result, invokeErr = runtimeInstance.Invoke(preCanceled, "openAboutDialog")
	if !result.IsNull() || !errors.Is(invokeErr, context.Canceled) || providerCalls.Load() != 1 || hostCalls.Load() != 0 {
		t.Fatalf("pre-canceled open UI = (%s, %v), provider/Host %d/%d",
			result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
	}

	postCanceled, cancelPost := context.WithCancel(context.Background())
	postRuntime, err := New(WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(
		context.Context,
		AggressorClientUIRequest,
	) (Value, error) {
		cancelPost()
		return String("completed after cancellation"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = postRuntime.Close(context.Background()) })
	result, invokeErr = postRuntime.Invoke(postCanceled, "openAboutDialog")
	if !result.IsNull() || !errors.Is(invokeErr, context.Canceled) {
		t.Fatalf("provider-canceled open UI = (%s, %v)", result.Describe(), invokeErr)
	}
}

func TestAggressorOpenClientUIConcurrentRequestsAreDetached(t *testing.T) {
	t.Parallel()

	const calls = 64
	provider := &recordingAggressorClientUIProvider{handle: func(_ context.Context, request AggressorClientUIRequest) (Value, error) {
		if len(request.Arguments) != 1 {
			return Null(), fmt.Errorf("arguments = %d, want one", len(request.Arguments))
		}
		return request.Arguments[0], nil
	}}
	runtimeInstance, err := New(WithAggressorClientUIProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	var wait sync.WaitGroup
	errorsByCall := make([]error, calls)
	for index := 0; index < calls; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			want := String(fmt.Sprintf("beacon-%d", index))
			result, invokeErr := runtimeInstance.Invoke(context.Background(), "openBeaconConsole", want)
			if invokeErr != nil {
				errorsByCall[index] = invokeErr
				return
			}
			if !result.IdentityEqual(want) {
				errorsByCall[index] = fmt.Errorf("result = %s, want %s", result.Describe(), want.Describe())
			}
		}()
	}
	wait.Wait()
	for index, invokeErr := range errorsByCall {
		if invokeErr != nil {
			t.Errorf("concurrent call %d: %v", index, invokeErr)
		}
	}
	requests := provider.snapshot()
	if len(requests) != calls {
		t.Fatalf("concurrent provider requests = %d, want %d", len(requests), calls)
	}
	seen := make(map[string]bool, calls)
	for _, request := range requests {
		if request.Name != "openBeaconConsole" || request.Operation != AggressorClientUIOpenBeaconConsole ||
			request.RuntimeID != runtimeInstance.ID() || len(request.Arguments) != 1 {
			t.Fatalf("concurrent request = %#v", request)
		}
		seen[request.Arguments[0].String()] = true
	}
	if len(seen) != calls {
		t.Fatalf("concurrent request values = %d unique, want %d", len(seen), calls)
	}
}

func TestPortableScriptLoaderInheritsAggressorOpenClientUIProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-open-client-ui.cna")
	if err := os.WriteFile(childPath, []byte(`openAboutDialog();`), 0o600); err != nil {
		t.Fatal(err)
	}
	program := mustCompileClientUITest(t, "parent-open-client-ui.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
[$child runScript];
`, filepath.ToSlash(childPath)))
	provider := &recordingAggressorClientUIProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader open UI route reached Host")
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
	if hostCalls.Load() != 0 || len(requests) != 1 {
		t.Fatalf("open UI child provider/Host requests = %d/%d, want 1/0", len(requests), hostCalls.Load())
	}
	request := requests[0]
	if request.Name != "openAboutDialog" || request.Operation != AggressorClientUIOpenAboutDialog ||
		request.RuntimeID == 0 || request.RuntimeID == runtimeInstance.ID() || request.Script != 1 ||
		request.Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("child open UI provenance = %#v", request)
	}
}

func TestAggressorOpenClientUIWithFunctionPrecedence(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host"), nil
		})),
		WithAggressorClientUIProvider(AggressorClientUIProviderFunc(func(context.Context, AggressorClientUIRequest) (Value, error) {
			providerCalls.Add(1)
			return String("provider"), nil
		})),
		WithFunction("openPayloadHelper", func(context.Context, Invocation) (Value, error) {
			return String("override"), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, invokeErr := runtimeInstance.Invoke(context.Background(), "openPayloadHelper", String("not callable"))
	if invokeErr != nil || result.String() != "override" || providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("open UI override = (%s, %v), provider/Host %d/%d",
			result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorOpenClientUIOperationNamesAreUnique(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, len(aggressorOpenUISpecExpectations))
	for _, expectation := range aggressorOpenUISpecExpectations {
		names = append(names, expectation.name)
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for index := 1; index < len(sorted); index++ {
		if sorted[index-1] == sorted[index] {
			t.Fatalf("duplicate open UI operation %q", sorted[index])
		}
	}
}
