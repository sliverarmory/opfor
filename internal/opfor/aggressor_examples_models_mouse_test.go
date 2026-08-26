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

func TestOfficialMouseExampleRetainsForkedMouseListener(t *testing.T) {
	t.Parallel()

	component := ObjectValue(&officialMouseObject{name: "label"})
	popupEvent := ObjectValue(&officialMouseObject{name: "popup-event", popupTrigger: true})
	ordinaryEvent := ObjectValue(&officialMouseObject{name: "ordinary-event"})
	objects := newOfficialMouseObjectHost(component)
	host := &officialMouseHost{}
	runtime, script, output := loadOfficialAdapterExample(t, "mouse.cna", host, objects)

	if _, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingCommand, Name: "mickey", RawInput: "mickey",
	}); err != nil {
		t.Fatalf("InvokeConsole(mickey): %v", err)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("addTab", "Mouse Test", component, "..."),
	)

	listener := objects.awaitListener(t)
	objectCalls := objects.takeCalls()
	if len(objectCalls) != 2 {
		t.Fatalf("initial object call count = %d, want 2", len(objectCalls))
	}
	assertOfficialMouseObjectCall(t, objectCalls[0], ObjectConstruct, Null(), "javax.swing.JLabel", "", String("Hello World"))
	assertOfficialMouseObjectCall(t, objectCalls[1], ObjectInvoke, component, "", "addMouseListener", officialCallbackValue{})

	if _, err := listener.Invoke(context.Background(), popupEvent); err != nil {
		t.Fatalf("popup mouse listener: %v", err)
	}
	objectCalls = objects.takeCalls()
	if len(objectCalls) != 1 {
		t.Fatalf("popup event object call count = %d, want 1", len(objectCalls))
	}
	assertOfficialMouseObjectCall(t, objectCalls[0], ObjectInvoke, popupEvent, "", "isPopupTrigger")
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("show_popup", popupEvent, "mickey_menu", component),
	)

	if _, err := listener.Invoke(context.Background(), ordinaryEvent); err != nil {
		t.Fatalf("ordinary mouse listener: %v", err)
	}
	objectCalls = objects.takeCalls()
	if len(objectCalls) != 1 {
		t.Fatalf("ordinary event object call count = %d, want 1", len(objectCalls))
	}
	assertOfficialMouseObjectCall(t, objectCalls[0], ObjectInvoke, ordinaryEvent, "", "isPopupTrigger")
	assertOfficialBehaviorCalls(t, host.takeCalls())

	// The importer asks OPFOR to compose the exact popup generation captured by
	// show_popup, without receiving Runtime access or arbitrary binding lookup.
	if err := host.takePopupComposer(t).Compose(context.Background()); err != nil {
		t.Fatalf("Compose(mickey_menu): %v", err)
	}
	if _, err := runtime.InvokeBinding(context.Background(), BindingItem, "Hello World"); err != nil {
		t.Fatalf("InvokeBinding(Hello World): %v", err)
	}
	objectCalls = objects.takeCalls()
	if len(objectCalls) != 1 {
		t.Fatalf("popup item object call count = %d, want 1", len(objectCalls))
	}
	assertOfficialMouseObjectCall(t, objectCalls[0], ObjectInvoke, component, "", "setText", String("Hey, you clicked me"))
	assertOfficialBehaviorCalls(t, host.takeCalls())
	if got := output.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if _, err := listener.Invoke(context.Background(), popupEvent); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("listener after parent unload error = %v, want ErrScriptUnloaded", err)
	}
}

func TestOfficialDataModelsExampleQueriesHostModelsAndProfile(t *testing.T) {
	t.Parallel()

	profile := ObjectValue(&officialDataModelProfile{})
	metadata := NewOrderedHash()
	metadata.Set("c2profile", profile)
	metadata.Set("server", String("team.example"))

	target := NewOrderedHash()
	target.Set("name", String("workstation-5"))
	target.Set("os", String("Windows 11"))
	targets := NewOrderedHash()
	targets.Set("10.0.0.5", HashValue(target))

	host := &officialDataModelHost{
		models: map[string]Value{
			"metadata": HashValue(metadata),
			"targets":  HashValue(targets),
		},
	}
	objects := &officialDataModelObjectHost{
		profile: profile,
		responses: map[string]string{
			".sleeptime":           "60000",
			".jitter":              "20",
			".stage.stomppe":       "true",
			".http-get.uri":        "/submit",
			".post-ex.spawnto_x64": `C:\Windows\System32\rundll32.exe`,
		},
	}
	_, _, output := loadOfficialAdapterExample(t, "data_models.cna", host, objects)

	wantOutput := "" +
		"-------------------------\n" +
		"\x034Data Models\n\n" +
		"metadata\n" +
		"targets\n" +
		"-------------------------\n" +
		"-------------------------\n" +
		"\x034List keys from a specific data model (example model: metadata)\n\n" +
		"c2profile\n" +
		"server\n" +
		"-------------------------\n" +
		"-------------------------\n" +
		"\x034Get Data from Data Model (example mode; targets)\n" +
		"%(10.0.0.5 => %(name => 'workstation-5', os => 'Windows 11'))\n" +
		"-------------------------\n" +
		"Sleep   : 60000\n" +
		"Jitter  : 20\n" +
		"StompPE : true\n" +
		"HTTP URI: /submit\n" +
		"SpawnTo: C:\\Windows\\System32\\rundll32.exe\n"
	if got := output.String(); got != wantOutput {
		t.Fatalf("stdout mismatch\n got: %q\nwant: %q", got, wantOutput)
	}

	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("data_keys"),
		expectedOfficialCall("data_query", "metadata"),
		expectedOfficialCall("data_query", "targets"),
		expectedOfficialCall("data_query", "metadata"),
	)
	objectCalls := objects.takeCalls()
	wantPaths := []string{
		".sleeptime",
		".jitter",
		".stage.stomppe",
		".http-get.uri",
		".post-ex.spawnto_x64",
	}
	if len(objectCalls) != len(wantPaths) {
		t.Fatalf("profile object call count = %d, want %d", len(objectCalls), len(wantPaths))
	}
	for index, path := range wantPaths {
		assertOfficialMouseObjectCall(t, objectCalls[index], ObjectInvoke, profile, "", "getString", String(path))
	}
}

func TestKnownWildcardClassHintLeavesUnknownClassesOnImporterPath(t *testing.T) {
	t.Parallel()

	var gotClass string
	runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Op != ObjectConstruct {
			return Null(), &UnsupportedError{Operation: "test object operation", Name: invocation.Message}
		}
		gotClass = invocation.Class
		return ObjectValue(&officialMouseObject{name: "unknown-importer-class"}), nil
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = runtime.Eval(context.Background(), "unknown-wildcard.sl", `
import java.awt.*;
import javax.swing.*;
return [new ImporterWidget];
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if gotClass != "java.awt.ImporterWidget" {
		t.Fatalf("unknown wildcard class = %q, want existing first-package importer path", gotClass)
	}
}

type officialCallbackValue struct{}

type officialMouseObject struct {
	name         string
	popupTrigger bool
}

func (object *officialMouseObject) SleepDescribe() string {
	return "<mouse:" + object.name + ">"
}

type officialMouseHost struct {
	mu    sync.Mutex
	calls []officialBehaviorCall
	popup AggressorPopupComposer
}

func (host *officialMouseHost) Call(_ context.Context, invocation Invocation) (Value, error) {
	if _, clientUI := aggressorClientUISpecs[invocation.Name]; clientUI {
		return Null(), fmt.Errorf("typed Aggressor client UI call %s unexpectedly reached Host", invocation.Name)
	}
	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{
		name: invocation.Name, values: append([]Value(nil), invocation.Values()...),
	})
	host.mu.Unlock()
	return Null(), nil
}

func (host *officialMouseHost) HandleAggressorClientUI(
	_ context.Context,
	request AggressorClientUIRequest,
) (Value, error) {
	if request.Operation == AggressorClientUIShowPopup && request.Popup == nil {
		return Null(), errors.New("typed show_popup request has no popup composer")
	}
	host.mu.Lock()
	if request.Operation == AggressorClientUIShowPopup {
		host.popup = request.Popup
	}
	host.calls = append(host.calls, officialBehaviorCall{
		name: request.Name, values: append([]Value(nil), request.Arguments...),
	})
	host.mu.Unlock()
	return Null(), nil
}

func (host *officialMouseHost) takePopupComposer(t *testing.T) AggressorPopupComposer {
	t.Helper()
	host.mu.Lock()
	defer host.mu.Unlock()
	composer := host.popup
	host.popup = nil
	if composer == nil {
		t.Fatal("official mouse Host has no retained popup composer")
	}
	return composer
}

func (host *officialMouseHost) takeCalls() []officialBehaviorCall {
	host.mu.Lock()
	defer host.mu.Unlock()
	calls := append([]officialBehaviorCall(nil), host.calls...)
	host.calls = nil
	return calls
}

type officialMouseObjectHost struct {
	mu        sync.Mutex
	component Value
	calls     []ObjectInvocation
	listeners chan Callable
}

func newOfficialMouseObjectHost(component Value) *officialMouseObjectHost {
	return &officialMouseObjectHost{component: component, listeners: make(chan Callable, 1)}
}

func (host *officialMouseObjectHost) Object(_ context.Context, invocation ObjectInvocation) (Value, error) {
	host.record(invocation)
	switch {
	case invocation.Op == ObjectConstruct && invocation.Class == "javax.swing.JLabel":
		return host.component, nil
	case invocation.Op == ObjectInvoke && invocation.Message == "addMouseListener":
		callback, err := invocation.Callback(0)
		if err != nil {
			return Null(), err
		}
		select {
		case host.listeners <- callback:
		default:
			return Null(), fmt.Errorf("mouse listener registered more than once")
		}
		return Null(), nil
	case invocation.Op == ObjectInvoke && invocation.Message == "isPopupTrigger":
		object, ok := invocation.Target.Object()
		if !ok {
			return Null(), fmt.Errorf("isPopupTrigger target = %s, want opaque mouse event", invocation.Target.Describe())
		}
		event, ok := object.(*officialMouseObject)
		if !ok {
			return Null(), fmt.Errorf("isPopupTrigger target type = %T, want *officialMouseObject", object)
		}
		return Bool(event.popupTrigger), nil
	case invocation.Op == ObjectInvoke && invocation.Message == "setText":
		return Null(), nil
	default:
		return Null(), &UnsupportedError{Operation: "test object operation", Name: invocation.Message}
	}
}

func (host *officialMouseObjectHost) record(invocation ObjectInvocation) {
	copyInvocation := invocation
	values := invocation.Values()
	copyInvocation.Arguments = make([]Argument, len(values))
	for index, value := range values {
		copyInvocation.Arguments[index] = Argument{Value: value}
	}
	host.mu.Lock()
	host.calls = append(host.calls, copyInvocation)
	host.mu.Unlock()
}

func (host *officialMouseObjectHost) awaitListener(t *testing.T) Callable {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case callback := <-host.listeners:
		return callback
	case <-timer.C:
		t.Fatal("timed out waiting for forked addMouseListener callback")
		return nil
	}
}

func (host *officialMouseObjectHost) takeCalls() []ObjectInvocation {
	host.mu.Lock()
	defer host.mu.Unlock()
	calls := append([]ObjectInvocation(nil), host.calls...)
	host.calls = nil
	return calls
}

type officialDataModelProfile struct{}

func (*officialDataModelProfile) SleepDescribe() string { return "<c2profile>" }

type officialDataModelHost struct {
	mu     sync.Mutex
	models map[string]Value
	calls  []officialBehaviorCall
}

func (host *officialDataModelHost) Call(_ context.Context, invocation Invocation) (Value, error) {
	if invocation.Name == "data_keys" || invocation.Name == "data_query" {
		return Null(), fmt.Errorf("typed data-model query %q bypassed configured provider", invocation.Name)
	}
	values := invocation.Values()
	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{
		name: invocation.Name, values: append([]Value(nil), values...),
	})
	host.mu.Unlock()
	return Null(), &UnsupportedError{Operation: "test host function", Name: invocation.Name}
}

func (host *officialDataModelHost) QueryAggressorDataModel(
	_ context.Context,
	query AggressorDataModelQuery,
) (Value, error) {
	values := []Value(nil)
	if query.Kind == AggressorDataModelQueryValue {
		values = append(values, query.Key)
	}
	host.mu.Lock()
	host.calls = append(host.calls, officialBehaviorCall{
		name: query.Name, values: append([]Value(nil), values...),
	})
	host.mu.Unlock()

	switch query.Kind {
	case AggressorDataModelQueryKeys:
		return ArrayValue(NewArray(String("metadata"), String("targets"))), nil
	case AggressorDataModelQueryValue:
		value, ok := host.models[query.Key.String()]
		if !ok {
			return Null(), fmt.Errorf("data_query has no fixture for %q", query.Key.String())
		}
		return value, nil
	default:
		return Null(), fmt.Errorf("unexpected data-model query kind %q", query.Kind)
	}
}

func (host *officialDataModelHost) takeCalls() []officialBehaviorCall {
	host.mu.Lock()
	defer host.mu.Unlock()
	calls := append([]officialBehaviorCall(nil), host.calls...)
	host.calls = nil
	return calls
}

type officialDataModelObjectHost struct {
	mu        sync.Mutex
	profile   Value
	responses map[string]string
	calls     []ObjectInvocation
}

func (host *officialDataModelObjectHost) Object(_ context.Context, invocation ObjectInvocation) (Value, error) {
	copyInvocation := invocation
	values := invocation.Values()
	copyInvocation.Arguments = make([]Argument, len(values))
	for index, value := range values {
		copyInvocation.Arguments[index] = Argument{Value: value}
	}
	host.mu.Lock()
	host.calls = append(host.calls, copyInvocation)
	host.mu.Unlock()
	if invocation.Op != ObjectInvoke || invocation.Message != "getString" || !invocation.Target.IdentityEqual(host.profile) {
		return Null(), &UnsupportedError{Operation: "test profile object", Name: invocation.Message}
	}
	if len(values) != 1 {
		return Null(), fmt.Errorf("c2profile getString received %d arguments, want 1", len(values))
	}
	response, ok := host.responses[values[0].String()]
	if !ok {
		return Null(), fmt.Errorf("c2profile fixture has no response for %q", values[0].String())
	}
	return String(response), nil
}

func (host *officialDataModelObjectHost) takeCalls() []ObjectInvocation {
	host.mu.Lock()
	defer host.mu.Unlock()
	calls := append([]ObjectInvocation(nil), host.calls...)
	host.calls = nil
	return calls
}

func loadOfficialAdapterExample(t *testing.T, name string, host Host, objects ObjectHost, extra ...Option) (*Runtime, *Script, *bytes.Buffer) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "upstream", "aggressor-script-examples", name))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	program, err := Compile(NewSource(name, data))
	if err != nil {
		t.Fatalf("Compile(%s): %v", name, err)
	}
	var output bytes.Buffer
	var diagnostics bytes.Buffer
	options := []Option{
		WithHost(host),
		WithObjectHost(objects),
		WithStdout(&output),
		WithStderr(&diagnostics),
		WithClock(ClockFunc(func() time.Time { return officialBehaviorInstant })),
		WithInstructionLimit(250_000),
	}
	if sink, ok := host.(AggressorBeaconTranscriptSink); ok {
		options = append(options, WithAggressorBeaconTranscriptSink(sink))
	}
	if provider, ok := host.(AggressorDataModelQueryProvider); ok {
		options = append(options, WithAggressorDataModelQueryProvider(provider))
	}
	if provider, ok := host.(AggressorDataStoreProvider); ok {
		options = append(options, WithAggressorDataStoreProvider(provider))
	}
	if provider, ok := host.(AggressorClientServiceProvider); ok {
		options = append(options, WithAggressorClientServiceProvider(provider))
	}
	if provider, ok := host.(AggressorTeamServerRPCProvider); ok {
		options = append(options, WithAggressorTeamServerRPCProvider(provider))
	}
	if provider, ok := host.(AggressorClientUIProvider); ok {
		options = append(options, WithAggressorClientUIProvider(provider))
	}
	options = append(options, extra...)
	runtime, err := New(options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load(%s): %v; diagnostics: %s", name, err, diagnostics.String())
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("Load(%s) diagnostics: %s", name, diagnostics.String())
	}
	t.Cleanup(func() {
		if err := script.Unload(context.Background()); err != nil {
			t.Errorf("Unload(%s): %v", name, err)
		}
		if got := diagnostics.String(); got != "" {
			t.Errorf("%s diagnostics = %q, want empty", name, got)
		}
	})
	return runtime, script, &output
}

func assertOfficialMouseObjectCall(
	t *testing.T,
	call ObjectInvocation,
	op ObjectOperation,
	target Value,
	class string,
	message string,
	want ...any,
) {
	t.Helper()
	if call.Op != op || call.Class != class || call.Message != message || !call.Target.IdentityEqual(target) {
		t.Fatalf("object call = {op:%d target:%s class:%q message:%q}, want {op:%d target:%s class:%q message:%q}",
			call.Op, call.Target.Describe(), call.Class, call.Message,
			op, target.Describe(), class, message,
		)
	}
	values := call.Values()
	if len(values) != len(want) {
		t.Fatalf("%s object argument count = %d, want %d; values = %s", message, len(values), len(want), ArrayValue(NewArray(values...)).Describe())
	}
	for index, expected := range want {
		switch expected := expected.(type) {
		case Value:
			if !values[index].IdentityEqual(expected) {
				t.Fatalf("%s object argument %d = %s, want identical %s", message, index, values[index].Describe(), expected.Describe())
			}
		case officialCallbackValue:
			if values[index].Kind() != KindFunction {
				t.Fatalf("%s object argument %d = %s, want function", message, index, values[index].Describe())
			}
		default:
			t.Fatalf("test has unsupported expected object argument %T", expected)
		}
	}
}
