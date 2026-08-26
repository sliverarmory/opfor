package opfor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
)

type recordingAggressorPayloadStoreProvider struct {
	mu       sync.Mutex
	requests []AggressorPayloadStoreRequest
	handle   func(context.Context, AggressorPayloadStoreRequest) (Value, error)
}

func (provider *recordingAggressorPayloadStoreProvider) HandleAggressorPayloadStore(
	ctx context.Context,
	request AggressorPayloadStoreRequest,
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

func (provider *recordingAggressorPayloadStoreProvider) snapshot() []AggressorPayloadStoreRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorPayloadStoreRequest(nil), provider.requests...)
}

func TestAggressorPayloadStoreFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPayloadStoreFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{
		"payloadstore_add", "payloadstore_fetch", "payloadstore_list",
		"payloadstore_metadata", "payloadstore_remove",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor Payload Store names = %q, want %q", names, want)
	}
}

func TestAggressorPayloadStoreOperationsPresenceProvenanceResultsAndEffects(t *testing.T) {
	t.Parallel()

	info := HashValue(NewOrderedHash())
	tests := []struct {
		name      string
		operation AggressorPayloadStoreOperation
		arguments []Value
		returns   bool
	}{
		{name: "payloadstore_add", operation: AggressorPayloadStoreAdd, arguments: []Value{String("name"), String("Beacon"), String("raw"), String("x64"), BinaryString([]byte{1})}, returns: true},
		{name: "payloadstore_add", operation: AggressorPayloadStoreAdd, arguments: []Value{String("name"), String("Beacon"), String("raw"), String("x64"), BinaryString([]byte{1}), info}, returns: true},
		{name: "payloadstore_fetch", operation: AggressorPayloadStoreFetch, arguments: []Value{Long(41)}, returns: true},
		{name: "payloadstore_list", operation: AggressorPayloadStoreList, returns: true},
		{name: "payloadstore_metadata", operation: AggressorPayloadStoreMetadata, arguments: []Value{String("name")}, returns: true},
		{name: "payloadstore_remove", operation: AggressorPayloadStoreRemove, arguments: []Value{String("name")}},
	}

	for index, test := range tests {
		test := test
		t.Run(fmt.Sprintf("%02d-%s-%d", index, test.name, len(test.arguments)), func(t *testing.T) {
			provided := HashValue(NewOrderedHash())
			provider := &recordingAggressorPayloadStoreProvider{
				handle: func(context.Context, AggressorPayloadStoreRequest) (Value, error) {
					return provided, nil
				},
			}
			var hostCalls atomic.Int32
			runtimeInstance, err := New(
				WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), errors.New("Payload Store provider route reached Host")
				})),
				WithAggressorPayloadStoreProvider(provider),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
			if err != nil {
				t.Fatal(err)
			}
			if test.returns {
				if !result.IdentityEqual(provided) {
					t.Fatalf("query result = %s, want identical %s", result.Describe(), provided.Describe())
				}
			} else if !result.IsNull() {
				t.Fatalf("effect result = %s, want null", result.Describe())
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
				t.Fatalf("arguments = %#v, want %d", request.Arguments, len(test.arguments))
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

func TestAggressorPayloadStoreInvalidAritiesAndInfoTypeStopBoundaries(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorPayloadStoreProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorPayloadStoreProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for name, spec := range aggressorPayloadStoreSpecs {
		counts := []int{spec.maximum + 1}
		if spec.minimum > 0 {
			counts = append(counts, spec.minimum-1)
		} else {
			counts = []int{1}
		}
		for _, count := range counts {
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
		result, invokeErr := runtimeInstance.Invoke(context.Background(), "payloadstore_add",
			String("name"), String("type"), String("raw"), String("x64"), BinaryString([]byte{1}), invalid)
		if invokeErr == nil || !result.IsNull() {
			t.Errorf("payloadstore_add info %s = (%s, %v), want hash error", invalid.Describe(), result.Describe(), invokeErr)
		}
	}
	if len(provider.snapshot()) != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid calls reached provider/Host %d/%d", len(provider.snapshot()), hostCalls.Load())
	}
}

func TestAggressorPayloadStoreRawHostFallbackAndAuthoritativeErrors(t *testing.T) {
	t.Parallel()

	cell := NewCell(String("entry"))
	var captured Invocation
	hostRuntime, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		captured = invocation
		invocation.Arguments[0].Set(String("changed"))
		return String("bytes"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hostRuntime.Close(context.Background()) })
	original := Invocation{
		Runtime:   hostRuntime,
		Script:    9,
		Name:      "payloadstore_fetch",
		Span:      Span{Source: "store-host.cna", Start: Position{Line: 3, Column: 2}},
		Arguments: []Argument{{Name: "$entry", Reference: cell}},
	}
	result, err := hostRuntime.aggressorPayloadStore(context.Background(), original)
	if err != nil || result.String() != "bytes" || captured.Arguments[0].Reference != cell || cell.Get().String() != "changed" {
		t.Fatalf("raw Host fallback = (%s, %v), invocation %#v/cell %s", result.Describe(), err, captured, cell.Get().Describe())
	}

	wantErr := errors.New("store mutation failed")
	var hostCalls atomic.Int32
	providerRuntime, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("host"), nil
		})),
		WithAggressorPayloadStoreProvider(AggressorPayloadStoreProviderFunc(func(context.Context, AggressorPayloadStoreRequest) (Value, error) {
			return String("partial"), wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = providerRuntime.Close(context.Background()) })
	result, err = providerRuntime.Invoke(context.Background(), "payloadstore_remove", String("entry"))
	if !errors.Is(err, wantErr) || !result.IsNull() || hostCalls.Load() != 0 {
		t.Fatalf("provider error = (%s, %v), Host calls %d", result.Describe(), err, hostCalls.Load())
	}
}

func TestAggressorPayloadStoreOverrideCancellationAndNilPolicy(t *testing.T) {
	for name, spec := range aggressorPayloadStoreSpecs {
		name, spec := name, spec
		for _, overrideFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("override/%s/%v", name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				providerOption := WithAggressorPayloadStoreProvider(AggressorPayloadStoreProviderFunc(func(context.Context, AggressorPayloadStoreRequest) (Value, error) {
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
				arguments := []Value(nil)
				if spec.minimum == 0 {
					arguments = []Value{String("invalid wrapper arity")}
				}
				result, err := runtimeInstance.Invoke(context.Background(), name, arguments...)
				if err != nil || result.String() != "override" || providerCalls.Load() != 0 {
					t.Fatalf("override = (%s, %v), provider calls %d", result.Describe(), err, providerCalls.Load())
				}
			})
		}
	}

	var typedNil *recordingAggressorPayloadStoreProvider
	if _, err := New(WithAggressorPayloadStoreProvider(typedNil)); err == nil {
		t.Fatal("typed-nil Payload Store provider accepted")
	}
	var nilFunction AggressorPayloadStoreProviderFunc
	if _, err := New(WithAggressorPayloadStoreProvider(nilFunction)); err == nil {
		t.Fatal("nil Payload Store provider function accepted")
	}
	if _, err := nilFunction.HandleAggressorPayloadStore(context.Background(), AggressorPayloadStoreRequest{}); err == nil {
		t.Fatal("direct nil Payload Store provider function succeeded")
	}

	var calls atomic.Int32
	var cancelDuring context.CancelFunc
	runtimeInstance, err := New(WithAggressorPayloadStoreProvider(AggressorPayloadStoreProviderFunc(func(context.Context, AggressorPayloadStoreRequest) (Value, error) {
		calls.Add(1)
		cancelDuring()
		return String("bytes"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtimeInstance.Invoke(preCanceled, "payloadstore_fetch", String("pre")); !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("pre-canceled error/calls = %v/%d", err, calls.Load())
	}
	during, cancel := context.WithCancel(context.Background())
	cancelDuring = cancel
	result, err := runtimeInstance.Invoke(during, "payloadstore_fetch", String("during"))
	if !errors.Is(err, context.Canceled) || !result.IsNull() || calls.Load() != 1 {
		t.Fatalf("cancel-during = (%s, %v), calls %d", result.Describe(), err, calls.Load())
	}
}
