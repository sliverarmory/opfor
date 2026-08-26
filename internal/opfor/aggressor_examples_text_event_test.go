package opfor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestOfficialSearchExampleQueriesBeaconLogAndPreservesFormattingBytes(t *testing.T) {
	t.Parallel()

	const (
		beaconID = "beacon-search"
		when     = int64(1_704_164_645_000)
		stamp    = "2024-01-02 03:04:05"
	)
	rows := NewArray(
		ArrayValue(NewArray(String("beacon_output"), String(beaconID), String("prefix needle suffix"), Long(when))),
		ArrayValue(NewArray(String("beacon_input"), String(beaconID), String("prefix needle suffix"), Long(when+1))),
		ArrayValue(NewArray(String("beacon_output"), String("other-beacon"), String("prefix needle suffix"), Long(when+2))),
		ArrayValue(NewArray(String("beacon_output"), String(beaconID), String("Output at yesterday matches needle: prefix needle suffix"), Long(when+3))),
		ArrayValue(NewArray(String("beacon_output"), String(beaconID), String("unrelated output"), Long(when+4))),
	)
	host := &officialTextEventHost{
		beaconLog: ArrayValue(rows),
	}
	runtime := loadOfficialTextEventExample(t, "search.cna", host)

	if _, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingAlias, Name: "search", RawInput: "search", SessionID: String(beaconID),
	}); err != nil {
		t.Fatalf("InvokeConsole(search without regex): %v", err)
	}
	assertOfficialTextEventCalls(t, host.takeCalls(),
		textEventCall("berror", beaconID, "search [regex]"),
	)

	if _, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingAlias, Name: "search", RawInput: "search needle", SessionID: String(beaconID),
	}); err != nil {
		t.Fatalf("InvokeConsole(search needle): %v", err)
	}
	assertOfficialTextEventCalls(t, host.takeCalls(),
		textEventCall("btask", beaconID, "Search log with\x03E needle\x0f"),
		textEventCall("data_query", "beaconlog"),
		textEventCall("blog", beaconID, "Output at\x03E "+stamp+" \x0fmatches\x03B needle\x03E:\x0f\n\nprefix needle suffix"),
	)
}

func TestOfficialGetenvExampleTracksEnvironmentPerBeacon(t *testing.T) {
	t.Parallel()

	host := &officialTextEventHost{}
	runtime := loadOfficialTextEventExample(t, "getenv.cna", host)
	ctx := context.Background()

	if _, err := runtime.DispatchEvent(ctx, "beacon_initial", String("beacon-env")); err != nil {
		t.Fatalf("DispatchEvent(beacon_initial): %v", err)
	}
	assertOfficialTextEventCalls(t, host.takeCalls(), textEventCall("bshell", "beacon-env", "set"))

	// Output without APPDATA takes the ignore branch and must not mark the
	// Beacon initialized; a later representative `set` response still parses.
	if _, err := runtime.DispatchEvent(ctx, "beacon_output", String("beacon-env"), String("USERNAME=ignored\r\nTEMP=C:\\Temp\r\n")); err != nil {
		t.Fatalf("DispatchEvent(non-environment beacon_output): %v", err)
	}
	assertOfficialTextEventCalls(t, host.takeCalls())

	setOutput := "ALLUSERSPROFILE=C:\\ProgramData\r\n" +
		"APPDATA=C:\\Users\\alice\\AppData\\Roaming\r\n" +
		"NOT_AN_ENVIRONMENT_LINE\r\n" +
		"USERNAME=alice\r\n" +
		"COMPLEX=one=two\r\n" +
		"EMPTY=\r\n"
	if _, err := runtime.DispatchEvent(ctx, "beacon_output", String("beacon-env"), String(setOutput)); err != nil {
		t.Fatalf("DispatchEvent(environment beacon_output): %v", err)
	}
	assertOfficialTextEventCalls(t, host.takeCalls())

	invokeEnv := func(variable string) {
		t.Helper()
		if _, err := runtime.InvokeConsole(ctx, ConsoleInvocation{
			Kind:      BindingAlias,
			Name:      "env",
			RawInput:  "env " + variable,
			SessionID: String("beacon-env"),
		}); err != nil {
			t.Fatalf("InvokeConsole(env %s): %v", variable, err)
		}
	}
	invokeEnv("APPDATA")
	invokeEnv("USERNAME")
	invokeEnv("COMPLEX")
	invokeEnv("EMPTY")
	assertOfficialTextEventCalls(t, host.takeCalls(),
		textEventCall("blog", "beacon-env", "APPDATA is: 'C:\\Users\\alice\\AppData\\Roaming'"),
		textEventCall("blog", "beacon-env", "USERNAME is: 'alice'"),
		textEventCall("blog", "beacon-env", "COMPLEX is: 'one=two'"),
		textEventCall("blog", "beacon-env", "EMPTY is: ''"),
	)

	// Once a Beacon has environment state, later output returns before parsing.
	if _, err := runtime.DispatchEvent(ctx, "beacon_output", String("beacon-env"), String("APPDATA=C:\\Changed\r\nUSERNAME=bob\r\n")); err != nil {
		t.Fatalf("DispatchEvent(repeated beacon_output): %v", err)
	}
	assertOfficialTextEventCalls(t, host.takeCalls())
	invokeEnv("USERNAME")
	assertOfficialTextEventCalls(t, host.takeCalls(),
		textEventCall("blog", "beacon-env", "USERNAME is: 'alice'"),
	)
}

func TestOfficialTokenToEmailWebHitHookFormatsResponseAndHandlerBranches(t *testing.T) {
	t.Parallel()

	const (
		when  = int64(1_704_164_645_000)
		stamp = "2024-01-02 03:04:05"
	)
	host := &officialTextEventHost{
		tokens: map[string]string{"token-7": "alice@example.test"},
	}
	runtime := loadOfficialTextEventExample(t, "tokenToEmail.cna", host)

	params := NewOrderedHash()
	params.Set("id", String("token-7"))
	params.Set("campaign", String("summer"))
	value, err := runtime.InvokeBinding(
		context.Background(), BindingHook, "WEB_HIT",
		String("POST"), String("/collect?x=1"), String("198.51.100.24"), String("Mozilla/5.0"),
		String("404"), Int(321), String(""), HashValue(params), Long(when),
	)
	if err != nil {
		t.Fatalf("InvokeBinding(WEB_HIT response): %v", err)
	}
	wantResponse := stamp + " visit from\x03E:\x0f 198.51.100.24 (alice@example.test)\n" +
		"\tRequest\x03E:\x0f POST /collect?x=1\n" +
		"\tResponse\x03E:\x034 404\n" +
		"\tMozilla/5.0\n" +
		"\t= Form Data=\n" +
		"\tid         = token-7\n" +
		"\tcampaign   = summer\n\n"
	if got := value.String(); got != wantResponse {
		t.Fatalf("WEB_HIT response bytes = % x, want % x\nresponse = %q", []byte(got), []byte(wantResponse), got)
	}
	assertOfficialTextEventCalls(t, host.takeCalls(),
		textEventCall("tokenToEmail", "token-7"),
	)

	emptyParams := NewOrderedHash()
	value, err = runtime.InvokeBinding(
		context.Background(), BindingHook, "WEB_HIT",
		String("GET"), String("/stage"), String("203.0.113.8"), String("curl/8.7"),
		String("200"), Int(0), String("PowerShell handler"), HashValue(emptyParams), Long(when+1000),
	)
	if err != nil {
		t.Fatalf("InvokeBinding(WEB_HIT handler): %v", err)
	}
	wantHandler := "2024-01-02 03:04:06 visit from\x03E:\x0f 203.0.113.8\n" +
		"\tRequest\x03E:\x0f GET /stage\n" +
		"\tPowerShell handler\n" +
		"\tcurl/8.7\n\n"
	if got := value.String(); got != wantHandler {
		t.Fatalf("WEB_HIT handler bytes = % x, want % x\nhandler = %q", []byte(got), []byte(wantHandler), got)
	}
	assertOfficialTextEventCalls(t, host.takeCalls())
}

func TestOfficialTokenToEmailProfilerHitHookFormatsApplications(t *testing.T) {
	t.Parallel()

	host := &officialTextEventHost{tokens: map[string]string{"profile-token": "bob@example.test"}}
	runtime := loadOfficialTextEventExample(t, "tokenToEmail.cna", host)
	applications := NewOrderedHash()
	applications.Set("Microsoft Edge", String("126.0"))
	applications.Set("Java", String("17.0.8"))

	value, err := runtime.InvokeBinding(
		context.Background(), BindingHook, "PROFILER_HIT",
		String("198.51.100.9"), String("Windows 11"), String("unused"),
		HashValue(applications), String("profile-token"),
	)
	if err != nil {
		t.Fatalf("InvokeBinding(PROFILER_HIT): %v", err)
	}
	want := "\x039[+]\x0f 198.51.100.9/Windows 11 [bob@example.test] Applications\n" +
		"\tMicrosoft Edge            126.0\n" +
		"\tJava                      17.0.8\n\n"
	if got := value.String(); got != want {
		t.Fatalf("PROFILER_HIT bytes = % x, want % x\noutput = %q", []byte(got), []byte(want), got)
	}
	assertOfficialTextEventCalls(t, host.takeCalls(), textEventCall("tokenToEmail", "profile-token"))
}

type officialTextEventHost struct {
	mu        sync.Mutex
	calls     []officialTextEventCall
	beaconLog Value
	tokens    map[string]string
}

type officialTextEventCall struct {
	name   string
	values []Value
}

func (host *officialTextEventHost) Call(_ context.Context, invocation Invocation) (Value, error) {
	if invocation.Name == "data_keys" || invocation.Name == "data_query" {
		return Null(), fmt.Errorf("typed data-model query %q bypassed configured provider", invocation.Name)
	}
	if invocation.Name == "dstamp" || invocation.Name == "tstamp" {
		return Null(), fmt.Errorf("portable timestamp function %q reached Host", invocation.Name)
	}
	if _, dataStore := aggressorDataStoreSpecs[invocation.Name]; dataStore {
		return Null(), fmt.Errorf("typed data-store operation %q bypassed configured provider", invocation.Name)
	}
	values := invocation.Values()
	host.mu.Lock()
	host.calls = append(host.calls, officialTextEventCall{
		name:   invocation.Name,
		values: append([]Value(nil), values...),
	})
	host.mu.Unlock()

	switch invocation.Name {
	case "berror", "blog", "bshell", "btask":
		return Null(), nil
	default:
		return Null(), fmt.Errorf("official text/event fixture rejected unexpected host function %q", invocation.Name)
	}
}

func (host *officialTextEventHost) HandleAggressorDataStore(
	_ context.Context,
	request AggressorDataStoreRequest,
) (Value, error) {
	values := append([]Value(nil), request.Arguments...)
	host.mu.Lock()
	host.calls = append(host.calls, officialTextEventCall{name: request.Name, values: values})
	host.mu.Unlock()
	if request.Operation != AggressorDataStoreTokenToEmail || len(values) != 1 {
		return Null(), fmt.Errorf("data-store request = %q/%d, want tokenToEmail/1", request.Operation, len(values))
	}
	email, ok := host.tokens[values[0].String()]
	if !ok {
		return Null(), fmt.Errorf("tokenToEmail has no fixture for %s", values[0].Describe())
	}
	return String(email), nil
}

func (host *officialTextEventHost) QueryAggressorDataModel(
	_ context.Context,
	query AggressorDataModelQuery,
) (Value, error) {
	values := []Value(nil)
	if query.Kind == AggressorDataModelQueryValue {
		values = append(values, query.Key)
	}
	host.mu.Lock()
	host.calls = append(host.calls, officialTextEventCall{
		name: query.Name, values: append([]Value(nil), values...),
	})
	host.mu.Unlock()
	if query.Kind != AggressorDataModelQueryValue || !query.Key.IdentityEqual(String("beaconlog")) {
		return Null(), fmt.Errorf("data-model query = %q/%s, want data_query/beaconlog", query.Kind, query.Key.Describe())
	}
	return host.beaconLog, nil
}

func (host *officialTextEventHost) PublishAggressorBeaconTranscript(
	_ context.Context,
	record AggressorBeaconTranscriptRecord,
) error {
	name, values := aggressorBeaconTranscriptCall(record)
	host.mu.Lock()
	host.calls = append(host.calls, officialTextEventCall{name: name, values: values})
	host.mu.Unlock()
	return nil
}

func (host *officialTextEventHost) takeCalls() []officialTextEventCall {
	host.mu.Lock()
	defer host.mu.Unlock()
	calls := append([]officialTextEventCall(nil), host.calls...)
	host.calls = nil
	return calls
}

func loadOfficialTextEventExample(t *testing.T, name string, host *officialTextEventHost) *Runtime {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "upstream", "aggressor-script-examples", name))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	program, err := Compile(NewSource(name, data))
	if err != nil {
		t.Fatalf("Compile(%s): %v", name, err)
	}
	var diagnostics bytes.Buffer
	options := []Option{
		WithHost(host),
		WithAggressorBeaconTranscriptSink(host),
		WithAggressorDataModelQueryProvider(host),
		WithAggressorDataStoreProvider(host),
		WithClock(ClockFunc(func() time.Time { return time.Unix(0, 0).UTC() })),
		WithStdout(io.Discard),
		WithStderr(&diagnostics),
		WithInstructionLimit(250_000),
	}
	for _, external := range []string{
		"__EXEC__", "exec", "openf", "ls", "lof", "mkdir",
		"deleteFile", "move", "rename", "copyFile",
	} {
		external := external
		options = append(options, WithFunction(external, func(context.Context, Invocation) (Value, error) {
			return Null(), fmt.Errorf("official text/event fixture blocked external function %s", external)
		}))
	}
	runtime, err := New(options...)
	if err != nil {
		t.Fatalf("New(%s): %v", name, err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load(%s): %v; diagnostics: %s", name, err, diagnostics.String())
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("Load(%s) diagnostics = %q, want empty", name, diagnostics.String())
	}
	t.Cleanup(func() {
		if err := script.Unload(context.Background()); err != nil {
			t.Errorf("Unload(%s): %v", name, err)
		}
		if diagnostics.Len() != 0 {
			t.Errorf("%s diagnostics = %q, want empty", name, diagnostics.String())
		}
	})
	return runtime
}

func textEventCall(name string, values ...any) officialTextEventCall {
	result := officialTextEventCall{name: name, values: make([]Value, len(values))}
	for index, value := range values {
		switch value := value.(type) {
		case string:
			result.values[index] = String(value)
		case int64:
			result.values[index] = Long(value)
		default:
			panic(fmt.Sprintf("unsupported official text/event call fixture %T", value))
		}
	}
	return result
}

func assertOfficialTextEventCalls(t *testing.T, got []officialTextEventCall, want ...officialTextEventCall) {
	t.Helper()
	if !reflect.DeepEqual(officialTextEventCallSnapshot(got), officialTextEventCallSnapshot(want)) {
		t.Fatalf("host calls = %#v, want %#v", officialTextEventCallSnapshot(got), officialTextEventCallSnapshot(want))
	}
}

type officialTextEventCallView struct {
	Name   string
	Values []string
}

func officialTextEventCallSnapshot(calls []officialTextEventCall) []officialTextEventCallView {
	result := make([]officialTextEventCallView, len(calls))
	for index, call := range calls {
		values := make([]string, len(call.values))
		for valueIndex, value := range call.values {
			values[valueIndex] = value.Describe()
		}
		result[index] = officialTextEventCallView{Name: call.name, Values: values}
	}
	return result
}
