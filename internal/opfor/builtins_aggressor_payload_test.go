package opfor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type recordingAggressorPayloadProvider struct {
	mu       sync.Mutex
	requests []AggressorPayloadRequest
	handle   func(context.Context, AggressorPayloadRequest) (Value, error)
}

func (provider *recordingAggressorPayloadProvider) HandleAggressorPayload(
	ctx context.Context,
	request AggressorPayloadRequest,
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

func (provider *recordingAggressorPayloadProvider) snapshot() []AggressorPayloadRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorPayloadRequest(nil), provider.requests...)
}

func TestAggressorPayloadFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPayloadFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{
		"-hasbootstraphint", "all_payloads", "artifact", "artifact_general",
		"artifact_sign", "artifact_stager", "payload", "payload_bootstrap_hint",
		"payload_local", "powershell", "shellcode", "stager", "stager_bind_pipe", "stager_bind_tcp",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor payload names = %q, want %q", names, want)
	}
}

func TestAggressorPayloadOperationsAritiesPresenceProvenanceAndResults(t *testing.T) {
	t.Parallel()

	storeInfo := HashValue(NewOrderedHash())
	tests := []struct {
		name      string
		operation AggressorPayloadOperation
		arguments []Value
		predicate bool
	}{
		{name: "-hasbootstraphint", operation: AggressorPayloadHasBootstrapHint, arguments: []Value{BinaryString([]byte{1, 2})}, predicate: true},
		{name: "all_payloads", operation: AggressorPayloadGenerateAll, arguments: []Value{String("/tmp/out"), Int(1), String("None")}},
		{name: "all_payloads", operation: AggressorPayloadGenerateAll, arguments: []Value{String("/tmp/out"), Int(1), String("Indirect"), Null(), String("dns"), String("set host_stage { false; }")}},
		{name: "artifact", operation: AggressorPayloadArtifact, arguments: []Value{String("listener"), String("exe")}},
		{name: "artifact", operation: AggressorPayloadArtifact, arguments: []Value{String("listener"), String("exe"), Null(), String("x64")}},
		{name: "artifact_general", operation: AggressorPayloadArtifactGeneral, arguments: []Value{BinaryString([]byte{3}), String("exe"), String("x64")}},
		{name: "artifact_sign", operation: AggressorPayloadArtifactSign, arguments: []Value{BinaryString([]byte{4})}},
		{name: "artifact_stager", operation: AggressorPayloadArtifactStager, arguments: []Value{String("listener"), String("raw"), String("x86")}},
		{name: "artifact_stager", operation: AggressorPayloadArtifactStager, arguments: []Value{String("listener"), String("raw"), String("x86"), storeInfo}},
		{name: "payload", operation: AggressorPayloadExport, arguments: []Value{String("listener"), String("x64"), String("process"), String("Direct")}},
		{name: "payload", operation: AggressorPayloadExport, arguments: []Value{String("listener"), String("x64"), String("process"), String("Direct"), Null(), String("dns_over_https"), String("profile")}},
		{name: "payload_bootstrap_hint", operation: AggressorPayloadBootstrapHint, arguments: []Value{BinaryString([]byte{5}), String("GetProcAddress")}},
		{name: "payload_local", operation: AggressorPayloadExportLocal, arguments: []Value{String("B-1"), String("listener"), String("x86"), String("thread"), String("None")}},
		{name: "payload_local", operation: AggressorPayloadExportLocal, arguments: []Value{String("B-1"), String("listener"), String("x86"), String("thread"), String("None"), Null()}},
		{name: "powershell", operation: AggressorPayloadPowerShell, arguments: []Value{String("listener"), Int(0)}},
		{name: "powershell", operation: AggressorPayloadPowerShell, arguments: []Value{String("listener"), Int(1), String("x64")}},
		{name: "shellcode", operation: AggressorPayloadShellcode, arguments: []Value{String("listener"), Int(0), String("x86")}},
		{name: "stager", operation: AggressorPayloadStager, arguments: []Value{String("listener"), String("x64")}},
		{name: "stager_bind_pipe", operation: AggressorPayloadStagerBindPipe, arguments: []Value{String("listener")}},
		{name: "stager_bind_tcp", operation: AggressorPayloadStagerBindTCP, arguments: []Value{String("listener"), String("x64"), Int(4444)}},
	}

	for index, test := range tests {
		test := test
		t.Run(fmt.Sprintf("%02d-%s-%d", index, test.name, len(test.arguments)), func(t *testing.T) {
			provided := ArrayValue(NewArray(String("provider-result")))
			var hostCalls atomic.Int32
			provider := &recordingAggressorPayloadProvider{
				handle: func(context.Context, AggressorPayloadRequest) (Value, error) {
					return provided, nil
				},
			}
			runtimeInstance, err := New(
				WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), errors.New("payload provider route reached Host")
				})),
				WithAggressorPayloadProvider(provider),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
			if err != nil {
				t.Fatal(err)
			}
			if test.predicate {
				if result.Kind() != KindInt || !result.Truth() {
					t.Fatalf("predicate result = %s, want canonical true", result.Describe())
				}
			} else if !result.IdentityEqual(provided) {
				t.Fatalf("result = %s, want identical %s", result.Describe(), provided.Describe())
			}
			if hostCalls.Load() != 0 {
				t.Fatalf("provider route reached Host %d time(s)", hostCalls.Load())
			}
			requests := provider.snapshot()
			if len(requests) != 1 {
				t.Fatalf("provider calls = %d, want one", len(requests))
			}
			request := requests[0]
			if request.Name != test.name || request.Operation != test.operation || request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 || request.Script != 0 || request.Span != (Span{}) {
				t.Fatalf("request metadata = %#v", request)
			}
			if len(request.Arguments) != len(test.arguments) {
				t.Fatalf("argument count = %d, want %d", len(request.Arguments), len(test.arguments))
			}
			for argumentIndex, argument := range test.arguments {
				if !request.Arg(argumentIndex).IdentityEqual(argument) {
					t.Fatalf("argument %d = %s, want identical %s", argumentIndex, request.Arg(argumentIndex).Describe(), argument.Describe())
				}
			}
			if request.HasArgument(-1) || request.HasArgument(len(test.arguments)) || !request.Arg(len(test.arguments)).IsNull() {
				t.Fatalf("absent argument policy failed: %#v", request.Arguments)
			}
		})
	}
}

func TestAggressorPayloadInvalidAritiesAndMapTypeStopBoundaries(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorPayloadProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorPayloadProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for name, spec := range aggressorPayloadSpecs {
		for _, count := range []int{spec.minimum - 1, spec.maximum + 1} {
			if count < 0 {
				continue
			}
			arguments := make([]Value, count)
			for index := range arguments {
				arguments[index] = Int(int32(index))
			}
			result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
			if invokeErr == nil || !result.IsNull() {
				t.Errorf("%s/%d = (%s, %v), want null arity error", name, count, result.Describe(), invokeErr)
			}
		}
	}
	for _, invalid := range []Value{Null(), String("not-a-hash"), ArrayValue(NewArray())} {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), "artifact_stager",
			String("listener"), String("exe"), String("x64"), invalid)
		if invokeErr == nil || !result.IsNull() {
			t.Errorf("artifact_stager optional info %s = (%s, %v), want hash error", invalid.Describe(), result.Describe(), invokeErr)
		}
	}
	if len(provider.snapshot()) != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid calls reached provider/Host %d/%d", len(provider.snapshot()), hostCalls.Load())
	}
}

func TestAggressorAllPayloadsDocumentedEnumConstraintsAndRawHostFallback(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorPayloadProvider{handle: func(context.Context, AggressorPayloadRequest) (Value, error) {
		return String("/tmp/out"), nil
	}}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("validated all_payloads call reached Host")
		})),
		WithAggressorPayloadProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	valid := [][]Value{
		{String("/tmp/out"), Int(1), String("None")},
		{String("/tmp/out"), Int(1), String("Direct"), String("wininet")},
		{String("/tmp/out"), Int(1), String("Indirect"), String("winhttp")},
		{String("/tmp/out"), Int(1), String("None"), Null()},
		{String("/tmp/out"), Int(1), String("None"), String("")},
		{String("/tmp/out"), Int(1), String("None"), String("wininet"), String("dns")},
		{String("/tmp/out"), Int(1), String("None"), String("wininet"), String("dns_over_https")},
		{String("/tmp/out"), Int(1), String("None"), String("wininet"), Null()},
		{String("/tmp/out"), Int(1), String("None"), String("wininet"), String("")},
	}
	for index, arguments := range valid {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), "all_payloads", arguments...)
		if invokeErr != nil || result.String() != "/tmp/out" {
			t.Errorf("valid case %d = (%s, %v)", index, result.Describe(), invokeErr)
		}
	}

	invalid := []struct {
		name      string
		arguments []Value
		argument  int
	}{
		{name: "lowercase syscall method", arguments: []Value{String("/tmp/out"), Int(1), String("none")}, argument: 3},
		{name: "non-string syscall method", arguments: []Value{String("/tmp/out"), Int(1), Int(0)}, argument: 3},
		{name: "unknown HTTP library", arguments: []Value{String("/tmp/out"), Int(1), String("None"), String("curl")}, argument: 4},
		{name: "uppercase HTTP library", arguments: []Value{String("/tmp/out"), Int(1), String("None"), String("WININET")}, argument: 4},
		{name: "textual null HTTP library", arguments: []Value{String("/tmp/out"), Int(1), String("None"), String("$null")}, argument: 4},
		{name: "unknown DNS mode", arguments: []Value{String("/tmp/out"), Int(1), String("None"), Null(), String("dns-over-https")}, argument: 5},
	}
	validCalls := len(provider.snapshot())
	for _, test := range invalid {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), "all_payloads", test.arguments...)
		if invokeErr == nil || !result.IsNull() ||
			!strings.Contains(invokeErr.Error(), fmt.Sprintf("argument %d must be one of", test.argument)) {
			t.Errorf("%s = (%s, %v), want null constraint error", test.name, result.Describe(), invokeErr)
		}
	}
	if got := len(provider.snapshot()); got != validCalls {
		t.Fatalf("invalid calls reached provider: calls %d, want %d", got, validCalls)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("typed route reached Host %d time(s)", hostCalls.Load())
	}

	methodCell := NewCell(String("importer-owned-method"))
	var captured Invocation
	hostRuntime, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		captured = invocation
		invocation.Arguments[2].Set(String("host-mutated"))
		return String("host-result"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hostRuntime.Close(context.Background()) })
	result, err := hostRuntime.aggressorPayload(context.Background(), Invocation{
		Runtime: hostRuntime,
		Name:    "all_payloads",
		Arguments: []Argument{
			{Value: String("/tmp/out")},
			{Value: Int(1)},
			{Name: "$method", Reference: methodCell},
		},
	})
	if err != nil || result.String() != "host-result" || captured.Arguments[2].Reference != methodCell ||
		methodCell.Get().String() != "host-mutated" {
		t.Fatalf("raw Host fallback = (%s, %v), invocation %#v/cell %s", result.Describe(), err, captured, methodCell.Get().Describe())
	}
}

func TestAggressorPayloadResolvesOnceAndRawHostFallback(t *testing.T) {
	t.Parallel()

	original := ArrayValue(NewArray(String("payload")))
	cell := NewCell(original)
	span := Span{Source: "payload-provider.cna", Start: Position{Line: 4, Column: 8}}
	var captured AggressorPayloadRequest
	providerRuntime, err := New(WithAggressorPayloadProvider(AggressorPayloadProviderFunc(func(_ context.Context, request AggressorPayloadRequest) (Value, error) {
		captured = request
		cell.Set(String("changed"))
		return request.Arg(0), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = providerRuntime.Close(context.Background()) })
	invocation := Invocation{
		Runtime: providerRuntime,
		Script:  19,
		Name:    "artifact_sign",
		Span:    span,
		Arguments: []Argument{
			{Name: "$artifact", Reference: cell},
		},
	}
	result, err := providerRuntime.aggressorPayload(context.Background(), invocation)
	if err != nil || !result.IdentityEqual(original) || !captured.Arg(0).IdentityEqual(original) || captured.RuntimeID != providerRuntime.ID() || captured.Script != 19 || captured.Span != span {
		t.Fatalf("resolved provider call = (%s, %v), request %#v", result.Describe(), err, captured)
	}

	hostCell := NewCell(String("raw"))
	var hostInvocation Invocation
	hostRuntime, err := New(WithHost(HostFunc(func(_ context.Context, got Invocation) (Value, error) {
		hostInvocation = got
		got.Arguments[0].Set(String("mutated"))
		return String("host-result"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hostRuntime.Close(context.Background()) })
	invocation.Runtime = hostRuntime
	invocation.Script = 21
	invocation.Arguments = []Argument{{Name: "$artifact", Reference: hostCell}}
	result, err = hostRuntime.aggressorPayload(context.Background(), invocation)
	if err != nil || result.String() != "host-result" || hostInvocation.Arguments[0].Reference != hostCell || hostCell.Get().String() != "mutated" {
		t.Fatalf("raw Host fallback = (%s, %v), invocation %#v/cell %s", result.Describe(), err, hostInvocation, hostCell.Get().Describe())
	}
}

func TestAggressorPayloadProviderErrorCancellationOverrideAndNilPolicy(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("generation failed after side effect")
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("host"), nil
		})),
		WithAggressorPayloadProvider(AggressorPayloadProviderFunc(func(context.Context, AggressorPayloadRequest) (Value, error) {
			return String("partial"), wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Invoke(context.Background(), "artifact_sign", String("bytes"))
	if !errors.Is(err, wantErr) || !result.IsNull() || hostCalls.Load() != 0 {
		t.Fatalf("provider error = (%s, %v), Host calls %d", result.Describe(), err, hostCalls.Load())
	}

	for name, spec := range aggressorPayloadSpecs {
		name, spec := name, spec
		for _, overrideFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("override/%s/%v", name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				providerOption := WithAggressorPayloadProvider(AggressorPayloadProviderFunc(func(context.Context, AggressorPayloadRequest) (Value, error) {
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
				runtimeWithOverride, newErr := New(options...)
				if newErr != nil {
					t.Fatal(newErr)
				}
				t.Cleanup(func() { _ = runtimeWithOverride.Close(context.Background()) })
				arguments := []Value(nil)
				if spec.minimum == 0 {
					arguments = []Value{String("invalid wrapper arity")}
				}
				got, invokeErr := runtimeWithOverride.Invoke(context.Background(), name, arguments...)
				if invokeErr != nil || got.String() != "override" || providerCalls.Load() != 0 {
					t.Fatalf("override = (%s, %v), provider calls %d", got.Describe(), invokeErr, providerCalls.Load())
				}
			})
		}
	}

	var typedNil *recordingAggressorPayloadProvider
	if _, err := New(WithAggressorPayloadProvider(typedNil)); err == nil {
		t.Fatal("typed-nil payload provider accepted")
	}
	var nilFunction AggressorPayloadProviderFunc
	if _, err := New(WithAggressorPayloadProvider(nilFunction)); err == nil {
		t.Fatal("nil payload provider function accepted")
	}
	if _, err := nilFunction.HandleAggressorPayload(context.Background(), AggressorPayloadRequest{}); err == nil {
		t.Fatal("direct nil payload provider function succeeded")
	}
}

func TestAggressorPayloadCancellation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var cancelDuring context.CancelFunc
	provider := AggressorPayloadProviderFunc(func(_ context.Context, request AggressorPayloadRequest) (Value, error) {
		calls.Add(1)
		cancelDuring()
		return request.Arg(0), nil
	})
	runtimeInstance, err := New(WithAggressorPayloadProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtimeInstance.Invoke(preCanceled, "artifact_sign", String("pre")); !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("pre-canceled error/calls = %v/%d", err, calls.Load())
	}
	during, cancel := context.WithCancel(context.Background())
	cancelDuring = cancel
	result, err := runtimeInstance.Invoke(during, "artifact_sign", String("during"))
	if !errors.Is(err, context.Canceled) || !result.IsNull() || calls.Load() != 1 {
		t.Fatalf("cancel-during = (%s, %v), calls %d", result.Describe(), err, calls.Load())
	}
}
