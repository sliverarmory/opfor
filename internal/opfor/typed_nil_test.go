package opfor

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

type typedNilHost struct{}

func (*typedNilHost) Call(context.Context, Invocation) (Value, error) {
	panic("typed-nil host was invoked")
}

type typedNilObjectHost struct{}

func (*typedNilObjectHost) Object(context.Context, ObjectInvocation) (Value, error) {
	panic("typed-nil object host was invoked")
}

type typedNilBindingObserver struct{}

func (*typedNilBindingObserver) Registered(context.Context, Binding) error {
	panic("typed-nil binding observer was invoked")
}

func (*typedNilBindingObserver) Unregistered(context.Context, Binding) error {
	panic("typed-nil binding observer was invoked")
}

type typedNilLifecycleObserver struct{}

func (*typedNilLifecycleObserver) ScriptLoaded(context.Context, *Script) error {
	panic("typed-nil lifecycle observer was invoked")
}

func (*typedNilLifecycleObserver) ScriptUnloaded(context.Context, *Script) error {
	panic("typed-nil lifecycle observer was invoked")
}

type typedNilSourceResolver struct{}

func (*typedNilSourceResolver) ResolveSource(context.Context, SourceRequest) (Source, error) {
	panic("typed-nil source resolver was invoked")
}

type typedNilClock struct{}

func (*typedNilClock) Now() time.Time {
	panic("typed-nil clock was invoked")
}

type typedNilBeaconStringEncoder struct{}

func (*typedNilBeaconStringEncoder) EncodeBeaconString(context.Context, Value, Value) ([]byte, error) {
	panic("typed-nil Beacon string encoder was invoked")
}

type typedNilAggressorEventDispatcher struct{}

func (*typedNilAggressorEventDispatcher) DispatchAggressorEvent(context.Context, Callable) error {
	panic("typed-nil Aggressor event dispatcher was invoked")
}

type typedNilAggressorSessionQueryProvider struct{}

func (*typedNilAggressorSessionQueryProvider) QueryAggressorSession(
	context.Context,
	AggressorSessionQuery,
) (Value, error) {
	panic("typed-nil Aggressor session query provider was invoked")
}

type typedNilAggressorArtifactProvider struct{}

func (*typedNilAggressorArtifactProvider) GenerateAggressorArtifact(
	context.Context,
	AggressorArtifactRequest,
) (Value, error) {
	panic("typed-nil Aggressor artifact provider was invoked")
}

type typedNilAggressorSiteProvider struct{}

func (*typedNilAggressorSiteProvider) HandleAggressorSite(
	context.Context,
	AggressorSiteRequest,
) (Value, error) {
	panic("typed-nil Aggressor site provider was invoked")
}

type typedNilAggressorTeamServerRPCProvider struct{}

func (*typedNilAggressorTeamServerRPCProvider) CallAggressorTeamServerRPC(
	context.Context,
	AggressorTeamServerRPCRequest,
) error {
	panic("typed-nil Aggressor Team Server RPC provider was invoked")
}

type typedNilCallable struct{}

func (*typedNilCallable) Invoke(context.Context, ...Value) (Value, error) {
	panic("typed-nil callable was invoked")
}

func TestOptionsRejectTypedNilInterfaces(t *testing.T) {
	t.Parallel()

	var stream *bytes.Buffer
	var host *typedNilHost
	var objectHost *typedNilObjectHost
	var bindingObserver *typedNilBindingObserver
	var lifecycleObserver *typedNilLifecycleObserver
	var resolver *typedNilSourceResolver
	var clock *typedNilClock
	var beaconEncoder *typedNilBeaconStringEncoder
	var eventDispatcher *typedNilAggressorEventDispatcher
	var sessionQueryProvider *typedNilAggressorSessionQueryProvider
	var artifactProvider *typedNilAggressorArtifactProvider
	var siteProvider *typedNilAggressorSiteProvider
	var teamServerRPCProvider *typedNilAggressorTeamServerRPCProvider

	tests := []struct {
		name   string
		option Option
		want   string
	}{
		{name: "stdin", option: WithStdin(stream), want: "stdin reader is nil"},
		{name: "stdout", option: WithStdout(stream), want: "stdout writer is nil"},
		{name: "stderr", option: WithStderr(stream), want: "stderr writer is nil"},
		{name: "host pointer", option: WithHost(host), want: "host is nil"},
		{name: "host function", option: WithHost(HostFunc(nil)), want: "host is nil"},
		{name: "object host pointer", option: WithObjectHost(objectHost), want: "object host is nil"},
		{name: "object host function", option: WithObjectHost(ObjectHostFunc(nil)), want: "object host is nil"},
		{name: "binding observer", option: WithBindingObserver(bindingObserver), want: "binding observer is nil"},
		{name: "lifecycle observer", option: WithScriptLifecycleObserver(lifecycleObserver), want: "script lifecycle observer is nil"},
		{name: "source resolver", option: WithSourceResolver(resolver), want: "source resolver is nil"},
		{name: "clock pointer", option: WithClock(clock), want: "clock is nil"},
		{name: "clock function", option: WithClock(ClockFunc(nil)), want: "clock is nil"},
		{name: "Beacon encoder pointer", option: WithBeaconStringEncoder(beaconEncoder), want: "Beacon string encoder is nil"},
		{name: "Beacon encoder function", option: WithBeaconStringEncoder(BeaconStringEncoderFunc(nil)), want: "Beacon string encoder is nil"},
		{name: "Aggressor dispatcher pointer", option: WithAggressorEventDispatcher(eventDispatcher), want: "Aggressor event dispatcher is nil"},
		{name: "Aggressor dispatcher function", option: WithAggressorEventDispatcher(AggressorEventDispatcherFunc(nil)), want: "Aggressor event dispatcher is nil"},
		{name: "Aggressor session query provider pointer", option: WithAggressorSessionQueryProvider(sessionQueryProvider), want: "Aggressor session query provider is nil"},
		{name: "Aggressor session query provider function", option: WithAggressorSessionQueryProvider(AggressorSessionQueryProviderFunc(nil)), want: "Aggressor session query provider is nil"},
		{name: "Aggressor artifact provider pointer", option: WithAggressorArtifactProvider(artifactProvider), want: "Aggressor artifact provider is nil"},
		{name: "Aggressor artifact provider function", option: WithAggressorArtifactProvider(AggressorArtifactProviderFunc(nil)), want: "Aggressor artifact provider is nil"},
		{name: "Aggressor site provider pointer", option: WithAggressorSiteProvider(siteProvider), want: "Aggressor site provider is nil"},
		{name: "Aggressor site provider function", option: WithAggressorSiteProvider(AggressorSiteProviderFunc(nil)), want: "Aggressor site provider is nil"},
		{name: "Aggressor Team Server RPC provider pointer", option: WithAggressorTeamServerRPCProvider(teamServerRPCProvider), want: "Aggressor Team Server RPC provider is nil"},
		{name: "Aggressor Team Server RPC provider function", option: WithAggressorTeamServerRPCProvider(AggressorTeamServerRPCProviderFunc(nil)), want: "Aggressor Team Server RPC provider is nil"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(test.option); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNilFunctionAdaptersReturnErrors(t *testing.T) {
	t.Parallel()

	if _, err := HostFunc(nil).Call(context.Background(), Invocation{}); err == nil {
		t.Fatal("nil HostFunc.Call returned no error")
	}
	if _, err := ObjectHostFunc(nil).Object(context.Background(), ObjectInvocation{}); err == nil {
		t.Fatal("nil ObjectHostFunc.Object returned no error")
	}
	if _, err := PredicateEvaluatorFunc(nil).Evaluate(context.Background()); err == nil {
		t.Fatal("nil PredicateEvaluatorFunc.Evaluate returned no error")
	}
	if _, err := BeaconStringEncoderFunc(nil).EncodeBeaconString(context.Background(), Null(), Null()); err == nil {
		t.Fatal("nil BeaconStringEncoderFunc.EncodeBeaconString returned no error")
	}
	if err := AggressorEventDispatcherFunc(nil).DispatchAggressorEvent(context.Background(), nil); err == nil {
		t.Fatal("nil AggressorEventDispatcherFunc.DispatchAggressorEvent returned no error")
	}
	if _, err := AggressorSessionQueryProviderFunc(nil).QueryAggressorSession(context.Background(), AggressorSessionQuery{}); err == nil {
		t.Fatal("nil AggressorSessionQueryProviderFunc.QueryAggressorSession returned no error")
	}
	if _, err := AggressorArtifactProviderFunc(nil).GenerateAggressorArtifact(context.Background(), AggressorArtifactRequest{}); err == nil {
		t.Fatal("nil AggressorArtifactProviderFunc.GenerateAggressorArtifact returned no error")
	}
	if _, err := AggressorSiteProviderFunc(nil).HandleAggressorSite(context.Background(), AggressorSiteRequest{}); err == nil {
		t.Fatal("nil AggressorSiteProviderFunc.HandleAggressorSite returned no error")
	}
	if err := AggressorTeamServerRPCProviderFunc(nil).CallAggressorTeamServerRPC(context.Background(), AggressorTeamServerRPCRequest{}); err == nil {
		t.Fatal("nil AggressorTeamServerRPCProviderFunc.CallAggressorTeamServerRPC returned no error")
	}
	if _, present, err := IteratorFunc(nil).Next(context.Background()); err == nil || present {
		t.Fatalf("nil IteratorFunc.Next = (present=%v, err=%v), want false and error", present, err)
	}
}

func TestFunctionValueRejectsTypedNilCallable(t *testing.T) {
	t.Parallel()

	var callable *typedNilCallable
	if value := FunctionValue(callable); !value.IsNull() {
		t.Fatalf("FunctionValue(typed nil) = %s, want $null", value.Describe())
	}
	malformed := Value{kind: KindFunction, data: Callable(callable)}
	if value, ok := malformed.Function(); ok || value != nil {
		t.Fatalf("malformed typed-nil Function = %v/%v, want nil/false", value, ok)
	}
}

func TestEmptyScriptLifecycleFuncsRemainValid(t *testing.T) {
	t.Parallel()

	runtime, err := New(WithScriptLifecycleObserver(ScriptLifecycleFuncs{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
