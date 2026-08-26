package opfor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestOfficialPortForwardAliasesReceiveSessionAndParsedArguments(t *testing.T) {
	for _, kind := range []BindingKind{BindingAlias, BindingSSHAlias} {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			recorder := &consoleHostRecorder{}
			runtime := loadOfficialConsoleExample(t, "portfwd.cna", recorder, nil)
			recorder.reset()

			raw := `portfwd "db internal" 8080`
			_, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
				Kind:      kind,
				Name:      "portfwd",
				RawInput:  raw,
				SessionID: String("session-42"),
			})
			if err != nil {
				t.Fatalf("InvokeConsole: %v", err)
			}
			assertConsoleHostNames(t, recorder, "call")
			assertConsoleTask(t, recorder, "session-42", "Tasked session to forward 8080 to db internal:8080")

			call, ok := recorder.find("call")
			if !ok {
				t.Fatalf("host calls = %v, want call(beacons.portfwd, ...)", recorder.names())
			}
			arguments := call.Values()
			if len(arguments) != 5 {
				t.Fatalf("call arguments = %s, want five values", ArrayValue(NewArray(arguments...)).Describe())
			}
			if arguments[0].String() != "beacons.portfwd" || !arguments[1].IsNull() ||
				arguments[2].String() != "session-42" || arguments[3].String() != "db internal" ||
				arguments[4].Int32() != 8080 {
				t.Fatalf("call arguments = %s", ArrayValue(NewArray(arguments...)).Describe())
			}
		})
	}
}

func TestOfficialPortForwardStopRoutesTypedTeamServerRPC(t *testing.T) {
	t.Parallel()

	recorder := &consoleHostRecorder{}
	runtime := loadOfficialConsoleExample(t, "portfwd.cna", recorder, nil)
	recorder.reset()

	_, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind:      BindingAlias,
		Name:      "portfwd",
		RawInput:  "portfwd stop 9090",
		SessionID: String("session-stop"),
	})
	if err != nil {
		t.Fatalf("InvokeConsole: %v", err)
	}
	assertConsoleHostNames(t, recorder, "call")
	assertConsoleTask(t, recorder, "session-stop", "Tasked session to stop forward to 9090")

	call, ok := recorder.find("call")
	if !ok {
		t.Fatalf("host calls = %v, want call(beacons.pivot_stop_port, ...)", recorder.names())
	}
	arguments := call.Values()
	if len(arguments) != 3 || arguments[0].String() != "beacons.pivot_stop_port" ||
		!arguments[1].IsNull() || arguments[2].String() != "9090" {
		t.Fatalf("call arguments = %s, want command, null callback, and raw port",
			ArrayValue(NewArray(arguments...)).Describe())
	}
}

func TestOfficialCallAnyAliasReceivesUnparsedRawTail(t *testing.T) {
	t.Parallel()

	clientObject := &consoleTestObject{class: "aggressor.AggressorClient"}
	client := ObjectValue(clientObject)
	recorder := &consoleHostRecorder{client: client}
	objects := &consoleObjectRecorder{}
	runtime := loadOfficialConsoleExample(t, "callany.cna", recorder, objects)
	raw := `test shell "dir c:\Program Files"`
	_, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind:      BindingAlias,
		Name:      "test",
		RawInput:  raw,
		SessionID: String("beacon-7"),
	})
	if err != nil {
		t.Fatalf("InvokeConsole: %v", err)
	}

	actionEvent, ok := objects.construct("java.awt.event.ActionEvent")
	if !ok {
		t.Fatalf("constructed classes = %v, want java.awt.event.ActionEvent", objects.classes())
	}
	arguments := actionEvent.Values()
	if len(arguments) != 3 {
		t.Fatalf("ActionEvent arguments = %s, want three values", ArrayValue(NewArray(arguments...)).Describe())
	}
	if got, want := arguments[2].String(), `shell "dir c:\Program Files"`; got != want {
		t.Fatalf("ActionEvent command = %q, want raw alias tail %q", got, want)
	}
	textField, ok := arguments[0].Object()
	textFieldObject, textFieldOK := textField.(*consoleTestObject)
	if !ok || !textFieldOK || textFieldObject.class != "javax.swing.JTextField" {
		t.Fatalf("ActionEvent source = %s, want javax.swing.JTextField", arguments[0].Describe())
	}

	beaconConsole, ok := objects.construct("aggressor.windows.BeaconConsole")
	if !ok {
		t.Fatalf("constructed classes = %v, want aggressor.windows.BeaconConsole", objects.classes())
	}
	arguments = beaconConsole.Values()
	if len(arguments) != 2 {
		t.Fatalf("BeaconConsole arguments = %s, want session and client", ArrayValue(NewArray(arguments...)).Describe())
	}
	if arguments[0].String() != "beacon-7" || !arguments[1].IdentityEqual(client) {
		t.Fatalf("BeaconConsole arguments = %s, want beacon-7 and exact provider client object",
			ArrayValue(NewArray(arguments...)).Describe())
	}
	gotClientObject, ok := arguments[1].Object()
	if !ok || gotClientObject != clientObject {
		t.Fatalf("BeaconConsole client object = %T %p, want exact *consoleTestObject %p",
			gotClientObject, gotClientObject, clientObject)
	}

	objectCalls := objects.snapshot()
	if len(objectCalls) != 4 {
		t.Fatalf("object call count = %d, want 4", len(objectCalls))
	}
	action := objectCalls[3]
	if action.Op != ObjectInvoke || action.Message != "actionPerformed" || len(action.Arguments) != 1 {
		t.Fatalf("final object call = %#v, want actionPerformed(event)", action)
	}
	actionTarget, ok := action.Target.Object()
	actionTargetObject, actionTargetOK := actionTarget.(*consoleTestObject)
	if !ok || !actionTargetOK || actionTargetObject.class != "aggressor.windows.BeaconConsole" {
		t.Fatalf("actionPerformed target = %s, want BeaconConsole", action.Target.Describe())
	}
	actionEventObject, ok := action.Arg(0).Object()
	actionEventValue, actionEventOK := actionEventObject.(*consoleTestObject)
	if !ok || !actionEventOK || actionEventValue.class != "java.awt.event.ActionEvent" {
		t.Fatalf("actionPerformed argument = %s, want ActionEvent", action.Arg(0).Describe())
	}

	_, err = runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingAlias, Name: "test", RawInput: "test sleep 1", SessionID: String("beacon-7"),
	})
	if err != nil {
		t.Fatalf("second InvokeConsole: %v", err)
	}
	objectCalls = objects.snapshot()
	beaconConstructions := 0
	actionCalls := 0
	for _, invocation := range objectCalls {
		if invocation.Op == ObjectConstruct && invocation.Class == "aggressor.windows.BeaconConsole" {
			beaconConstructions++
		}
		if invocation.Op == ObjectInvoke && invocation.Message == "actionPerformed" {
			actionCalls++
		}
	}
	if beaconConstructions != 1 || actionCalls != 2 {
		t.Fatalf("two aliases produced %d BeaconConsole constructions and %d actionPerformed calls, want 1 and 2",
			beaconConstructions, actionCalls)
	}
	requests := recorder.clientServiceRequests()
	if len(requests) != 1 || requests[0].Operation != AggressorClientServiceGetAggressorClient ||
		requests[0].Name != "getAggressorClient" || len(requests[0].Arguments) != 0 {
		t.Fatalf("client service requests = %#v, want one getAggressorClient request", requests)
	}
}

func assertConsoleHostNames(t *testing.T, recorder *consoleHostRecorder, want ...string) {
	t.Helper()
	got := recorder.names()
	if len(got) != len(want) {
		t.Fatalf("host call names = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("host call names = %v, want %v", got, want)
		}
	}
}

func loadOfficialConsoleExample(t *testing.T, name string, host Host, objectHost ObjectHost) *Runtime {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "upstream", "aggressor-script-examples", name))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	program, err := Compile(NewSource(name, data))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var diagnostics bytes.Buffer
	options := []Option{WithHost(host), WithStdout(io.Discard), WithStderr(&diagnostics)}
	if provider, ok := host.(AggressorTeamServerRPCProvider); ok {
		options = append(options, WithAggressorTeamServerRPCProvider(provider))
	}
	if provider, ok := host.(AggressorClientServiceProvider); ok {
		options = append(options, WithAggressorClientServiceProvider(provider))
	}
	if sink, ok := host.(AggressorBeaconTranscriptSink); ok {
		options = append(options, WithAggressorBeaconTranscriptSink(sink))
	}
	if objectHost != nil {
		options = append(options, WithObjectHost(objectHost))
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
	return runtime
}

type consoleHostRecorder struct {
	mu            sync.Mutex
	calls         []Invocation
	client        Value
	clientService []AggressorClientServiceRequest
	transcripts   []AggressorBeaconTranscriptRecord
}

func (recorder *consoleHostRecorder) Call(_ context.Context, invocation Invocation) (Value, error) {
	if invocation.Name == "call" {
		return Null(), fmt.Errorf("typed Aggressor Team Server RPC call unexpectedly reached Host")
	}
	if _, clientService := aggressorClientServiceSpecs[invocation.Name]; clientService {
		return Null(), fmt.Errorf("typed Aggressor client service %q unexpectedly reached Host", invocation.Name)
	}
	recorder.record(invocation)
	return Null(), nil
}

func (recorder *consoleHostRecorder) HandleAggressorClientService(
	_ context.Context,
	request AggressorClientServiceRequest,
) (Value, error) {
	recorder.mu.Lock()
	recorder.clientService = append(recorder.clientService, request)
	client := recorder.client
	recorder.mu.Unlock()
	if request.Operation != AggressorClientServiceGetAggressorClient {
		return Null(), fmt.Errorf("official console fixture does not implement client service %q", request.Operation)
	}
	return client, nil
}

func (recorder *consoleHostRecorder) CallAggressorTeamServerRPC(
	_ context.Context,
	request AggressorTeamServerRPCRequest,
) error {
	if request.Callback.Valid() {
		return fmt.Errorf("official console fixture expected a null Team Server RPC callback for %s", request.Command.Describe())
	}
	values := make([]Value, 0, 2+len(request.Arguments))
	values = append(values, request.Command, Null())
	values = append(values, request.Arguments...)
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	recorder.record(Invocation{
		Script:    request.Script,
		Name:      request.Name,
		Arguments: arguments,
		Span:      request.Span,
	})
	return nil
}

func (recorder *consoleHostRecorder) PublishAggressorBeaconTranscript(
	_ context.Context,
	record AggressorBeaconTranscriptRecord,
) error {
	recorder.mu.Lock()
	recorder.transcripts = append(recorder.transcripts, record)
	recorder.mu.Unlock()
	return nil
}

func (recorder *consoleHostRecorder) record(invocation Invocation) {
	values := invocation.Values()
	copyInvocation := invocation
	copyInvocation.Arguments = make([]Argument, len(values))
	for index, value := range values {
		copyInvocation.Arguments[index] = Argument{Value: value}
	}
	recorder.mu.Lock()
	recorder.calls = append(recorder.calls, copyInvocation)
	recorder.mu.Unlock()
}

func (recorder *consoleHostRecorder) reset() {
	recorder.mu.Lock()
	recorder.calls = nil
	recorder.transcripts = nil
	recorder.mu.Unlock()
}

func (recorder *consoleHostRecorder) find(name string) (Invocation, bool) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, invocation := range recorder.calls {
		if invocation.Name == name {
			return invocation, true
		}
	}
	return Invocation{}, false
}

func (recorder *consoleHostRecorder) names() []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	result := make([]string, len(recorder.calls))
	for index, invocation := range recorder.calls {
		result[index] = invocation.Name
	}
	return result
}

func (recorder *consoleHostRecorder) clientServiceRequests() []AggressorClientServiceRequest {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]AggressorClientServiceRequest(nil), recorder.clientService...)
}

func (recorder *consoleHostRecorder) transcriptRecords() []AggressorBeaconTranscriptRecord {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]AggressorBeaconTranscriptRecord(nil), recorder.transcripts...)
}

func assertConsoleTask(t *testing.T, recorder *consoleHostRecorder, beaconID, message string) {
	t.Helper()
	records := recorder.transcriptRecords()
	if len(records) != 1 || records[0].Kind != AggressorBeaconTranscriptTask ||
		records[0].BeaconID.String() != beaconID || records[0].Text.String() != message || records[0].HasMITREIDs {
		t.Fatalf("transcript records = %#v, want one btask(%q, %q)", records, beaconID, message)
	}
}

type consoleObjectRecorder struct {
	mu          sync.Mutex
	invocations []ObjectInvocation
}

func (recorder *consoleObjectRecorder) Object(_ context.Context, invocation ObjectInvocation) (Value, error) {
	values := invocation.Values()
	copyInvocation := invocation
	copyInvocation.Arguments = make([]Argument, len(values))
	for index, value := range values {
		copyInvocation.Arguments[index] = Argument{Value: value}
	}
	recorder.mu.Lock()
	recorder.invocations = append(recorder.invocations, copyInvocation)
	recorder.mu.Unlock()
	if invocation.Op == ObjectConstruct {
		return ObjectValue(&consoleTestObject{class: invocation.Class}), nil
	}
	return Null(), nil
}

func (recorder *consoleObjectRecorder) construct(class string) (ObjectInvocation, bool) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, invocation := range recorder.invocations {
		if invocation.Op == ObjectConstruct && invocation.Class == class {
			return invocation, true
		}
	}
	return ObjectInvocation{}, false
}

func (recorder *consoleObjectRecorder) classes() []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	result := make([]string, 0, len(recorder.invocations))
	for _, invocation := range recorder.invocations {
		if invocation.Op == ObjectConstruct {
			result = append(result, invocation.Class)
		}
	}
	return result
}

func (recorder *consoleObjectRecorder) snapshot() []ObjectInvocation {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]ObjectInvocation(nil), recorder.invocations...)
}

type consoleTestObject struct {
	class string
}
