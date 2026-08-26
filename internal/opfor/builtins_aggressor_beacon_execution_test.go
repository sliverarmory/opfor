package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type recordingAggressorBeaconExecutionProvider struct {
	mu       sync.Mutex
	requests []AggressorBeaconExecutionRequest
	handle   func(context.Context, AggressorBeaconExecutionRequest) (Value, error)
}

func (provider *recordingAggressorBeaconExecutionProvider) HandleAggressorBeaconExecution(
	ctx context.Context,
	request AggressorBeaconExecutionRequest,
) (Value, error) {
	provider.mu.Lock()
	provider.requests = append(provider.requests, request)
	provider.mu.Unlock()
	if provider.handle != nil {
		return provider.handle(ctx, request)
	}
	return String("private provider result"), nil
}

func (provider *recordingAggressorBeaconExecutionProvider) snapshot() []AggressorBeaconExecutionRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorBeaconExecutionRequest(nil), provider.requests...)
}

func TestAggressorBeaconExecutionProviderRoutesDocumentedShapes(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorBeaconExecutionProvider{
		handle: func(_ context.Context, request AggressorBeaconExecutionRequest) (Value, error) {
			if request.Kind == AggressorBeaconPostexKitCallbackID {
				return Int(73), nil
			}
			return String("not script visible"), nil
		},
	}
	runtimeInstance, err := New(WithAggressorBeaconExecutionProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Invoke(
		context.Background(), "beacon_execute_job",
		String("beacon-job"), String("cmd.exe"), String(" /c whoami"), Int(1),
	)
	if err != nil || !result.IsNull() {
		t.Fatalf("beacon_execute_job = (%s, %v), want null/nil", result.Describe(), err)
	}

	dll := BinaryString([]byte{0x00, 0xff, 0x41})
	result, err = runtimeInstance.Invoke(
		context.Background(), "beacon_execute_postex_job",
		String("beacon-postex"), Null(), dll, Null(), Null(), Long(9001),
	)
	if err != nil || !result.IsNull() {
		t.Fatalf("beacon_execute_postex_job = (%s, %v), want null/nil", result.Describe(), err)
	}

	packed := NewOrderedHash()
	packed.Set("bytes", BinaryString([]byte("packed")))
	result, err = runtimeInstance.Invoke(
		context.Background(), "beacon_inline_execute_pe",
		String("beacon-pe"), BinaryString([]byte("PE")), String("go"), HashValue(packed),
	)
	if err != nil || !result.IsNull() {
		t.Fatalf("beacon_inline_execute_pe = (%s, %v), want null/nil", result.Describe(), err)
	}

	result, err = runtimeInstance.Invoke(context.Background(), "get_postex_kit_callback_id")
	if err != nil || result.Int64() != 73 {
		t.Fatalf("get_postex_kit_callback_id = (%s, %v), want 73/nil", result.Describe(), err)
	}

	requests := provider.snapshot()
	if len(requests) != 4 {
		t.Fatalf("provider requests = %d, want 4", len(requests))
	}
	for index, request := range requests {
		if request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 || request.Script != 0 || request.Span != (Span{}) {
			t.Errorf("request %d provenance = runtime:%d script:%d span:%#v", index, request.RuntimeID, request.Script, request.Span)
		}
	}

	job := requests[0]
	if job.Kind != AggressorBeaconExecuteJob || job.Name != "beacon_execute_job" ||
		job.BeaconID.String() != "beacon-job" || job.Command.String() != "cmd.exe" ||
		job.CommandArguments.String() != " /c whoami" || job.Flags.Int32() != 1 ||
		job.CallbackState != AggressorCallbackOmitted || job.Callback != nil {
		t.Errorf("execute-job request = %#v", job)
	}

	postex := requests[1]
	postexBytes, postexBinary := postex.Content.Bytes()
	if postex.Kind != AggressorBeaconExecutePostexJob || postex.Name != "beacon_execute_postex_job" ||
		postex.BeaconID.String() != "beacon-postex" || !postex.PID.IsNull() ||
		!postexBinary || !postex.Content.IsBinaryString() || !bytes.Equal(postexBytes, []byte{0x00, 0xff, 0x41}) ||
		!postex.HasPackedArguments || !postex.PackedArguments.IsNull() ||
		postex.CallbackState != AggressorCallbackNull || postex.Callback != nil ||
		!postex.HasMessageID || postex.MessageID.Int64() != 9001 {
		t.Errorf("postex request = %#v content=%x/binary:%v", postex, postexBytes, postexBinary)
	}

	inlinePE := requests[2]
	gotPacked, packedOK := inlinePE.PackedArguments.Hash()
	if inlinePE.Kind != AggressorBeaconInlineExecutePE || inlinePE.Name != "beacon_inline_execute_pe" ||
		inlinePE.BeaconID.String() != "beacon-pe" || inlinePE.EntryPoint.String() != "go" ||
		!inlinePE.HasPackedArguments || !packedOK || gotPacked != packed ||
		inlinePE.CallbackState != AggressorCallbackOmitted || inlinePE.Callback != nil {
		t.Errorf("inline-PE request = %#v", inlinePE)
	}

	callbackID := requests[3]
	if callbackID.Kind != AggressorBeaconPostexKitCallbackID || callbackID.Name != "get_postex_kit_callback_id" ||
		!callbackID.BeaconID.IsNull() || callbackID.HasPackedArguments || callbackID.HasMessageID ||
		callbackID.CallbackState != AggressorCallbackOmitted || callbackID.Callback != nil {
		t.Errorf("callback-ID request = %#v", callbackID)
	}
}

func TestAggressorBeaconExecutionProviderHostsPowerShellScriptsWithExactValues(t *testing.T) {
	t.Parallel()

	importedResult := HashValue(NewOrderedHash())
	hostedResult := ArrayValue(NewArray(String("download"), String("cradle")))
	provider := &recordingAggressorBeaconExecutionProvider{
		handle: func(_ context.Context, request AggressorBeaconExecutionRequest) (Value, error) {
			switch request.Kind {
			case AggressorBeaconHostImportedScript:
				return importedResult, nil
			case AggressorBeaconHostScript:
				return hostedResult, nil
			default:
				return Null(), fmt.Errorf("unexpected execution kind %q", request.Kind)
			}
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed Beacon script host reached Host")
		})),
		WithAggressorBeaconExecutionProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	importedBeacon := ArrayValue(NewArray(String("B-imported")))
	result, err := runtimeInstance.Invoke(context.Background(), "beacon_host_imported_script", importedBeacon)
	if err != nil || !result.IdentityEqual(importedResult) {
		t.Fatalf("beacon_host_imported_script = (%s, %v), want identical provider result/nil", result.Describe(), err)
	}
	hostedBeacon := ObjectValue(&struct{ id string }{"B-hosted"})
	script := BinaryString([]byte{'$', 'x', '=', '1', ';', 0xff})
	result, err = runtimeInstance.Invoke(context.Background(), "beacon_host_script", hostedBeacon, script)
	if err != nil || !result.IdentityEqual(hostedResult) {
		t.Fatalf("beacon_host_script = (%s, %v), want identical provider result/nil", result.Describe(), err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("configured provider reached Host %d time(s)", hostCalls.Load())
	}

	requests := provider.snapshot()
	if len(requests) != 2 {
		t.Fatalf("script-host provider requests = %d, want 2", len(requests))
	}
	imported := requests[0]
	if imported.Kind != AggressorBeaconHostImportedScript || imported.Name != "beacon_host_imported_script" ||
		imported.RuntimeID != runtimeInstance.ID() || imported.Script != 0 || imported.Span != (Span{}) ||
		!imported.BeaconID.IdentityEqual(importedBeacon) || !imported.Content.IsNull() {
		t.Errorf("imported-script request = %#v", imported)
	}
	hosted := requests[1]
	if hosted.Kind != AggressorBeaconHostScript || hosted.Name != "beacon_host_script" ||
		hosted.RuntimeID != runtimeInstance.ID() || hosted.Script != 0 || hosted.Span != (Span{}) ||
		!hosted.BeaconID.IdentityEqual(hostedBeacon) || !hosted.Content.IdentityEqual(script) {
		t.Errorf("host-script request = %#v", hosted)
	}
}

func TestAggressorBeaconScriptHostFunctionOverridesWinInBothOptionOrders(t *testing.T) {
	for _, name := range []string{"beacon_host_imported_script", "beacon_host_script"} {
		for _, overrideFirst := range []bool{false, true} {
			name, overrideFirst := name, overrideFirst
			t.Run(fmt.Sprintf("%s/override-first=%v", name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				var hostCalls atomic.Int32
				provider := WithAggressorBeaconExecutionProvider(AggressorBeaconExecutionProviderFunc(func(
					context.Context,
					AggressorBeaconExecutionRequest,
				) (Value, error) {
					providerCalls.Add(1)
					return Null(), nil
				}))
				override := WithFunction(name, func(_ context.Context, invocation Invocation) (Value, error) {
					return String("override:" + invocation.Name), nil
				})
				host := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), nil
				}))
				options := []Option{host, provider, override}
				if overrideFirst {
					options = []Option{host, override, provider}
				}
				runtimeInstance, err := New(options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

				// No arguments is invalid for both native forms and proves the
				// importer override runs before ABI validation.
				result, err := runtimeInstance.Invoke(context.Background(), name)
				if err != nil || result.String() != "override:"+name || providerCalls.Load() != 0 || hostCalls.Load() != 0 {
					t.Fatalf("override = (%s, %v), provider/Host calls %d/%d",
						result.Describe(), err, providerCalls.Load(), hostCalls.Load())
				}
			})
		}
	}
}

func TestAggressorBeaconExecutionProviderAppliesHookAndGuardsCallbacks(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorBeaconExecutionProvider{}
	runtimeInstance, err := New(WithAggressorBeaconExecutionProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("typed-beacon-execution.cna", `
set BEACON_INLINE_EXECUTE {
    return "hooked:" . $1;
}
sub delivered {
    return $1 . ":" . $2 . ":" . $3["type"];
}
beacon_inline_execute("beacon-inline", "object", "go", "packed", &delivered);
beacon_execute_postex_job("beacon-postex", $null, "dll", "args", &delivered, 41);
beacon_inline_execute_pe("beacon-pe", "pe", "go", "pe-args", &delivered);
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	requests := provider.snapshot()
	if len(requests) != 3 {
		t.Fatalf("provider requests = %d, want 3", len(requests))
	}
	for index, request := range requests {
		if request.RuntimeID != runtimeInstance.ID() || request.Script != script.ID() || request.Span.Source != "typed-beacon-execution.cna" ||
			request.CallbackState != AggressorCallbackCallable || isNilInterface(request.Callback) {
			t.Errorf("request %d provenance/callback = %#v", index, request)
		}
	}
	hooked, ok := requests[0].Content.Bytes()
	if requests[0].Kind != AggressorBeaconInlineExecute || !ok || !requests[0].Content.IsBinaryString() || string(hooked) != "hooked:object" {
		t.Errorf("hooked inline request = %#v bytes:%q", requests[0], hooked)
	}
	if requests[1].Kind != AggressorBeaconExecutePostexJob || !requests[1].HasPackedArguments ||
		requests[1].PackedArguments.String() != "args" || !requests[1].HasMessageID || requests[1].MessageID.Int32() != 41 {
		t.Errorf("postex optional fields = %#v", requests[1])
	}
	if requests[2].Kind != AggressorBeaconInlineExecutePE || requests[2].Content.String() != "pe" {
		t.Errorf("inline-PE request = %#v", requests[2])
	}

	info := NewOrderedHash()
	info.Set("type", String("output"))
	for index, request := range requests {
		result, invokeErr := request.Callback.Invoke(
			context.Background(), String("B"), String("done"), HashValue(info),
		)
		if invokeErr != nil || result.String() != "B:done:output" {
			t.Errorf("callback %d = (%s, %v), want B:done:output/nil", index, result.Describe(), invokeErr)
		}
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	for index, request := range requests {
		if result, invokeErr := request.Callback.Invoke(context.Background()); !result.IsNull() || !errors.Is(invokeErr, ErrScriptUnloaded) {
			t.Errorf("callback %d after unload = (%s, %v), want null/ErrScriptUnloaded", index, result.Describe(), invokeErr)
		}
	}
}

func TestAggressorBeaconExecutionProviderErrorsAreAuthoritative(t *testing.T) {
	t.Parallel()

	providerErr := errors.New("execution rejected")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("unexpected Host result"), nil
		})),
		WithAggressorBeaconExecutionProvider(AggressorBeaconExecutionProviderFunc(func(
			context.Context,
			AggressorBeaconExecutionRequest,
		) (Value, error) {
			providerCalls.Add(1)
			return String("discarded partial result"), providerErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, invocation := range []struct {
		name string
		args []Value
	}{
		{name: "beacon_execute_job", args: []Value{String("B"), String("cmd"), String(" args"), Int(0)}},
		{name: "get_postex_kit_callback_id"},
	} {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), invocation.name, invocation.args...)
		if !result.IsNull() || !errors.Is(invokeErr, providerErr) {
			t.Errorf("%s = (%s, %v), want null/%v", invocation.name, result.Describe(), invokeErr, providerErr)
		}
	}
	if providerCalls.Load() != 2 || hostCalls.Load() != 0 {
		t.Fatalf("provider/Host calls = %d/%d, want 2/0", providerCalls.Load(), hostCalls.Load())
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	result, invokeErr := runtimeInstance.Invoke(canceled, "beacon_execute_job", String("B"), String("cmd"), String(""), Int(0))
	if !result.IsNull() || !errors.Is(invokeErr, context.Canceled) || providerCalls.Load() != 2 || hostCalls.Load() != 0 {
		t.Fatalf("canceled call = (%s, %v) provider/Host:%d/%d", result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorBeaconExecutionHostFallbackPreservesInvocation(t *testing.T) {
	t.Parallel()

	var received Invocation
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		received = invocation
		if len(invocation.Arguments) != 4 || invocation.Arguments[0].Reference == nil {
			return Null(), fmt.Errorf("Host received altered arguments: %#v", invocation.Arguments)
		}
		if !invocation.Arguments[0].Set(String("after")) {
			return Null(), errors.New("Host could not mutate pass-by-name Beacon ID")
		}
		return String("host-result"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Eval(context.Background(), "execution-host-fallback.cna", `
$bid = "before";
$host = beacon_execute_job($bid, "cmd", " args", 0);
return $bid . ":" . $host;
`)
	if err != nil || result.String() != "after:host-result" {
		t.Fatalf("Host fallback = (%s, %v), want after:host-result/nil", result.Describe(), err)
	}
	if received.Name != "beacon_execute_job" || received.Runtime != runtimeInstance || received.Script == 0 || received.Span.Source != "execution-host-fallback.cna" {
		t.Errorf("Host invocation provenance = %#v", received)
	}
}

func TestAggressorBeaconExecutionWithFunctionPrecedence(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	var overrideCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorBeaconExecutionProvider(AggressorBeaconExecutionProviderFunc(func(context.Context, AggressorBeaconExecutionRequest) (Value, error) {
			providerCalls.Add(1)
			return Null(), nil
		})),
		WithFunction("beacon_execute_job", func(_ context.Context, invocation Invocation) (Value, error) {
			overrideCalls.Add(1)
			return invocation.Arg(0), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	// One argument deliberately violates the native ABI and proves the importer
	// override runs before native validation.
	result, err := runtimeInstance.Invoke(context.Background(), "beacon_execute_job", String("override"))
	if err != nil || result.String() != "override" || overrideCalls.Load() != 1 || providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("override = (%s, %v) calls override/provider/Host:%d/%d/%d",
			result.Describe(), err, overrideCalls.Load(), providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorBeaconExecutionArgumentValidation(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int32
	runtimeInstance, err := New(WithAggressorBeaconExecutionProvider(AggressorBeaconExecutionProviderFunc(func(context.Context, AggressorBeaconExecutionRequest) (Value, error) {
		providerCalls.Add(1)
		return Null(), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	tests := []struct {
		name string
		args []Value
		want string
	}{
		{name: "beacon_execute_job", args: []Value{String("B")}, want: "expected 4 argument(s)"},
		{name: "beacon_execute_postex_job", args: []Value{String("B"), Null()}, want: "expected 3 to 6 argument(s)"},
		{name: "beacon_inline_execute_pe", args: []Value{String("B"), String("pe"), String("go"), String("args"), Null(), Null()}, want: "expected 4 or 5 arguments"},
		{name: "beacon_host_imported_script", args: nil, want: "expected 1 argument(s)"},
		{name: "beacon_host_imported_script", args: []Value{String("B"), String("extra")}, want: "expected 1 argument(s)"},
		{name: "beacon_host_script", args: []Value{String("B")}, want: "expected 2 argument(s)"},
		{name: "beacon_host_script", args: []Value{String("B"), String("script"), String("extra")}, want: "expected 2 argument(s)"},
		{name: "get_postex_kit_callback_id", args: []Value{Int(1)}, want: "expected 0 argument(s)"},
		{name: "beacon_execute_postex_job", args: []Value{String("B"), Null(), String("dll"), Null(), String("not callable")}, want: "argument 5 is not callable or $null"},
	}
	for _, test := range tests {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.args...)
		if !result.IsNull() || invokeErr == nil || !strings.Contains(invokeErr.Error(), test.want) {
			t.Errorf("%s invalid call = (%s, %v), want null error containing %q", test.name, result.Describe(), invokeErr, test.want)
		}
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("provider calls after invalid invocations = %d, want 0", providerCalls.Load())
	}
}

func TestPortableScriptLoaderInheritsAggressorBeaconExecutionProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-beacon-execution.cna")
	if err := os.WriteFile(childPath, []byte(`return beacon_host_imported_script("child-target");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-beacon-execution.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
$parent = beacon_host_script("parent-target", "parent-script");
$childResult = [$child runScript];
return $parent + $childResult;
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}

	provider := &recordingAggressorBeaconExecutionProvider{
		handle: func(_ context.Context, request AggressorBeaconExecutionRequest) (Value, error) {
			if request.Kind != AggressorBeaconHostScript && request.Kind != AggressorBeaconHostImportedScript {
				return Null(), fmt.Errorf("unexpected inherited request kind %q", request.Kind)
			}
			return Int(7), nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader Beacon execution route reached Host")
		})),
		WithAggressorBeaconExecutionProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil || result.Int32() != 14 {
		t.Fatalf("parent/child execution = (%s, %v), want 14/nil", result.Describe(), err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("parent/child provider requests reached Host %d time(s)", hostCalls.Load())
	}
	requests := provider.snapshot()
	if len(requests) != 2 {
		t.Fatalf("parent/child provider requests = %d, want 2", len(requests))
	}
	if requests[0].RuntimeID != runtimeInstance.ID() || requests[0].RuntimeID == 0 ||
		requests[1].RuntimeID == 0 || requests[1].RuntimeID == requests[0].RuntimeID ||
		requests[0].Span.Source != "parent-beacon-execution.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child provider provenance = %#v", requests)
	}
	if requests[0].Kind != AggressorBeaconHostScript || requests[0].Name != "beacon_host_script" ||
		requests[0].BeaconID.String() != "parent-target" || requests[0].Content.String() != "parent-script" ||
		requests[1].Kind != AggressorBeaconHostImportedScript || requests[1].Name != "beacon_host_imported_script" ||
		requests[1].BeaconID.String() != "child-target" || !requests[1].Content.IsNull() {
		t.Fatalf("parent/child script-host request shapes = %#v", requests)
	}
}

func TestAggressorBeaconExecutionProviderRejectsTypedNil(t *testing.T) {
	t.Parallel()

	var provider *recordingAggressorBeaconExecutionProvider
	if _, err := New(WithAggressorBeaconExecutionProvider(provider)); err == nil || err.Error() != "opfor: Aggressor Beacon execution provider is nil" {
		t.Fatalf("typed-nil provider error = %v", err)
	}
	if _, err := New(WithAggressorBeaconExecutionProvider(AggressorBeaconExecutionProviderFunc(nil))); err == nil || err.Error() != "opfor: Aggressor Beacon execution provider is nil" {
		t.Fatalf("nil provider function error = %v", err)
	}
	if _, err := AggressorBeaconExecutionProviderFunc(nil).HandleAggressorBeaconExecution(context.Background(), AggressorBeaconExecutionRequest{}); err == nil {
		t.Fatal("nil provider adapter returned no error")
	}
}
