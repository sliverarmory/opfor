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

type recordingAggressorListenerProvider struct {
	mu       sync.Mutex
	requests []AggressorListenerRequest
	handle   func(context.Context, AggressorListenerRequest) (Value, error)
}

func (provider *recordingAggressorListenerProvider) HandleAggressorListener(
	ctx context.Context,
	request AggressorListenerRequest,
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

func (provider *recordingAggressorListenerProvider) snapshot() []AggressorListenerRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorListenerRequest(nil), provider.requests...)
}

func TestAggressorListenerFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorListenerFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{
		"listener_create", "listener_create_ext", "listener_delete", "listener_describe",
		"listener_info", "listener_pivot_create", "listener_restart", "listeners",
		"listeners_local", "listeners_stageless",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor listener names = %q, want %q", names, want)
	}
}

func TestAggressorListenerOperationsAritiesPresenceProvenanceAndEffects(t *testing.T) {
	t.Parallel()

	options := HashValue(NewOrderedHash())
	tests := []struct {
		name      string
		operation AggressorListenerOperation
		arguments []Value
		returns   bool
	}{
		{name: "listener_create", operation: AggressorListenerCreate, arguments: []Value{String("foreign"), String("windows/foreign/reverse_https"), String("host"), Int(443)}},
		{name: "listener_create", operation: AggressorListenerCreate, arguments: []Value{String("http"), String("windows/beacon_http/reverse_http"), String("host"), Int(80), String("one,two")}},
		{name: "listener_create_ext", operation: AggressorListenerCreateExtended, arguments: []Value{String("http"), String("windows/beacon_http/reverse_http"), options}},
		{name: "listener_delete", operation: AggressorListenerDelete, arguments: []Value{String("http")}},
		{name: "listener_describe", operation: AggressorListenerDescribe, arguments: []Value{String("http")}, returns: true},
		{name: "listener_describe", operation: AggressorListenerDescribe, arguments: []Value{String("http"), Null()}, returns: true},
		{name: "listener_info", operation: AggressorListenerInfo, arguments: []Value{String("http")}, returns: true},
		{name: "listener_info", operation: AggressorListenerInfo, arguments: []Value{String("http"), Null()}, returns: true},
		{name: "listener_pivot_create", operation: AggressorListenerPivotCreate, arguments: []Value{String("B-1"), String("pivot"), String("windows/beacon_reverse_tcp"), String("host"), Int(4444)}},
		{name: "listener_restart", operation: AggressorListenerRestart, arguments: []Value{String("http")}},
		{name: "listeners", operation: AggressorListenerList, returns: true},
		{name: "listeners_local", operation: AggressorListenerListLocal, returns: true},
		{name: "listeners_stageless", operation: AggressorListenerListStageless, returns: true},
	}

	for index, test := range tests {
		test := test
		t.Run(fmt.Sprintf("%02d-%s-%d", index, test.name, len(test.arguments)), func(t *testing.T) {
			provided := ArrayValue(NewArray(String("listener-result")))
			provider := &recordingAggressorListenerProvider{
				handle: func(context.Context, AggressorListenerRequest) (Value, error) {
					return provided, nil
				},
			}
			var hostCalls atomic.Int32
			runtimeInstance, err := New(
				WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), errors.New("listener provider route reached Host")
				})),
				WithAggressorListenerProvider(provider),
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

func TestAggressorListenerInvalidAritiesAndOptionsTypeStopBoundaries(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorListenerProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorListenerProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for name, spec := range aggressorListenerSpecs {
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
		result, invokeErr := runtimeInstance.Invoke(context.Background(), "listener_create_ext",
			String("listener"), String("windows/beacon_http/reverse_http"), invalid)
		if invokeErr == nil || !result.IsNull() {
			t.Errorf("listener_create_ext options %s = (%s, %v), want hash error", invalid.Describe(), result.Describe(), invokeErr)
		}
	}
	if len(provider.snapshot()) != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid calls reached provider/Host %d/%d", len(provider.snapshot()), hostCalls.Load())
	}
}

func TestAggressorListenerRawHostFallbackAndAuthoritativeErrors(t *testing.T) {
	t.Parallel()

	cell := NewCell(String("listener"))
	var captured Invocation
	hostRuntime, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		captured = invocation
		invocation.Arguments[0].Set(String("changed"))
		return String("host-result"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hostRuntime.Close(context.Background()) })
	original := Invocation{
		Runtime:   hostRuntime,
		Script:    7,
		Name:      "listener_info",
		Span:      Span{Source: "listener-host.cna", Start: Position{Line: 2, Column: 3}},
		Arguments: []Argument{{Name: "$listener", Reference: cell}},
	}
	result, err := hostRuntime.aggressorListener(context.Background(), original)
	if err != nil || result.String() != "host-result" || captured.Arguments[0].Reference != cell || cell.Get().String() != "changed" {
		t.Fatalf("raw Host fallback = (%s, %v), invocation %#v/cell %s", result.Describe(), err, captured, cell.Get().Describe())
	}

	wantErr := errors.New("listener mutation failed")
	var hostCalls atomic.Int32
	providerRuntime, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("host"), nil
		})),
		WithAggressorListenerProvider(AggressorListenerProviderFunc(func(context.Context, AggressorListenerRequest) (Value, error) {
			return String("partial"), wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = providerRuntime.Close(context.Background()) })
	result, err = providerRuntime.Invoke(context.Background(), "listener_delete", String("listener"))
	if !errors.Is(err, wantErr) || !result.IsNull() || hostCalls.Load() != 0 {
		t.Fatalf("provider error = (%s, %v), Host calls %d", result.Describe(), err, hostCalls.Load())
	}
}

func TestAggressorListenerOverrideCancellationAndNilPolicy(t *testing.T) {
	for name, spec := range aggressorListenerSpecs {
		name, spec := name, spec
		for _, overrideFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("override/%s/%v", name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				providerOption := WithAggressorListenerProvider(AggressorListenerProviderFunc(func(context.Context, AggressorListenerRequest) (Value, error) {
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

	var typedNil *recordingAggressorListenerProvider
	if _, err := New(WithAggressorListenerProvider(typedNil)); err == nil {
		t.Fatal("typed-nil listener provider accepted")
	}
	var nilFunction AggressorListenerProviderFunc
	if _, err := New(WithAggressorListenerProvider(nilFunction)); err == nil {
		t.Fatal("nil listener provider function accepted")
	}
	if _, err := nilFunction.HandleAggressorListener(context.Background(), AggressorListenerRequest{}); err == nil {
		t.Fatal("direct nil listener provider function succeeded")
	}

	var calls atomic.Int32
	var cancelDuring context.CancelFunc
	runtimeInstance, err := New(WithAggressorListenerProvider(AggressorListenerProviderFunc(func(context.Context, AggressorListenerRequest) (Value, error) {
		calls.Add(1)
		cancelDuring()
		return String("description"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtimeInstance.Invoke(preCanceled, "listener_info", String("pre")); !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("pre-canceled error/calls = %v/%d", err, calls.Load())
	}
	during, cancel := context.WithCancel(context.Background())
	cancelDuring = cancel
	result, err := runtimeInstance.Invoke(during, "listener_info", String("during"))
	if !errors.Is(err, context.Canceled) || !result.IsNull() || calls.Load() != 1 {
		t.Fatalf("cancel-during = (%s, %v), calls %d", result.Describe(), err, calls.Load())
	}
}
