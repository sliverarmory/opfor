package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var aggressorClientServiceTestSpecs = []struct {
	name         string
	operation    AggressorClientServiceOperation
	minimum      int
	maximum      int
	returnsValue bool
}{
	{"getAggressorClient", AggressorClientServiceGetAggressorClient, 0, 0, true},
	{"get_cs_version", AggressorClientServiceGetCSVersion, 0, 0, true},
	{"mynick", AggressorClientServiceMyNick, 0, 0, true},
	{"users", AggressorClientServiceUsers, 0, 0, true},
	{"action", AggressorClientServiceAction, 1, 1, false},
	{"elog", AggressorClientServiceEventLog, 1, 1, false},
	{"say", AggressorClientServiceSay, 1, 1, false},
	{"privmsg", AggressorClientServicePrivateMessage, 2, 2, false},
	{"custom_event", AggressorClientServiceCustomEvent, 2, 2, false},
	{"custom_event_private", AggressorClientServiceCustomEventPrivate, 3, 3, false},
	{"closeClient", AggressorClientServiceCloseClient, 0, 0, false},
	{"sync_download", AggressorClientServiceSyncDownload, 2, 3, false},
}

func TestAggressorClientServiceSpecsAndExactArities(t *testing.T) {
	if len(aggressorClientServiceSpecs) != len(aggressorClientServiceTestSpecs) {
		t.Fatalf("client-service specs = %d, want %d", len(aggressorClientServiceSpecs), len(aggressorClientServiceTestSpecs))
	}
	for _, test := range aggressorClientServiceTestSpecs {
		test := test
		t.Run(test.name, func(t *testing.T) {
			spec, ok := aggressorClientServiceSpecs[test.name]
			if !ok || spec.operation != test.operation || spec.minimum != test.minimum ||
				spec.maximum != test.maximum || spec.returnsValue != test.returnsValue {
				t.Fatalf("spec = %#v, %v; want %q/%d..%d/%v",
					spec, ok, test.operation, test.minimum, test.maximum, test.returnsValue)
			}

			var calls atomic.Int32
			runtimeInstance, err := New(WithAggressorClientServiceProvider(
				AggressorClientServiceProviderFunc(func(context.Context, AggressorClientServiceRequest) (Value, error) {
					calls.Add(1)
					return String("provider"), nil
				}),
			))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			wrongCounts := []int{test.maximum + 1}
			if test.minimum != 0 {
				wrongCounts = append(wrongCounts, test.minimum-1)
			}
			for _, wrong := range wrongCounts {
				arguments := make([]Value, wrong)
				for index := range arguments {
					arguments[index] = String(fmt.Sprintf("arg-%d", index))
				}
				result, err := runtimeInstance.Invoke(context.Background(), test.name, arguments...)
				if !result.IsNull() || err == nil {
					t.Fatalf("%d arguments result = (%s, %v), want null/error", wrong, result.Describe(), err)
				}
				if calls.Load() != 0 {
					t.Fatalf("wrong arity called provider %d time(s)", calls.Load())
				}
			}
		})
	}
}

func TestAggressorClientServiceProviderRoutesResolvedRequestsAndResults(t *testing.T) {
	t.Parallel()

	compound := NewArray(String("shared"))
	object := &aggressorClientServiceObject{name: "client-result"}
	providerResult := ObjectValue(object)
	var mu sync.Mutex
	var requests []AggressorClientServiceRequest
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed client service reached Host")
		})),
		WithAggressorClientServiceProvider(AggressorClientServiceProviderFunc(
			func(_ context.Context, request AggressorClientServiceRequest) (Value, error) {
				mu.Lock()
				requests = append(requests, request)
				mu.Unlock()
				return providerResult, nil
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, test := range aggressorClientServiceTestSpecs {
		arguments := make([]Value, test.minimum)
		for index := range arguments {
			arguments[index] = ArrayValue(compound)
		}
		result, err := runtimeInstance.Invoke(context.Background(), test.name, arguments...)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if test.returnsValue {
			got, ok := result.Object()
			if !ok || got != object {
				t.Errorf("%s result = %s, want exact provider object", test.name, result.Describe())
			}
		} else if !result.IsNull() {
			t.Errorf("%s result = %s, want $null", test.name, result.Describe())
		}
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("typed requests reached Host %d time(s)", hostCalls.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != len(aggressorClientServiceTestSpecs) {
		t.Fatalf("requests = %d, want %d", len(requests), len(aggressorClientServiceTestSpecs))
	}
	for index, request := range requests {
		want := aggressorClientServiceTestSpecs[index]
		if request.Operation != want.operation || request.Name != want.name || request.RuntimeID != runtimeInstance.ID() || request.Script != 0 || request.Span != (Span{}) {
			t.Errorf("request %d metadata = %#v", index, request)
		}
		if len(request.Arguments) != want.minimum {
			t.Errorf("request %d arguments = %d, want %d", index, len(request.Arguments), want.minimum)
		}
		for _, argument := range request.Arguments {
			array, ok := argument.Array()
			if !ok || array != compound {
				t.Errorf("request %d lost compound argument identity", index)
			}
		}
	}
}

func TestAggressorClientServiceGetClientPreservesObjectHostIdentity(t *testing.T) {
	t.Parallel()

	client := &aggressorClientServiceObject{name: "opaque-client"}
	var providerCalls atomic.Int32
	var objectCalls atomic.Int32
	runtimeInstance, err := New(
		WithAggressorClientServiceProvider(AggressorClientServiceProviderFunc(
			func(_ context.Context, request AggressorClientServiceRequest) (Value, error) {
				providerCalls.Add(1)
				if request.Operation != AggressorClientServiceGetAggressorClient {
					return Null(), fmt.Errorf("unexpected operation %q", request.Operation)
				}
				return ObjectValue(client), nil
			},
		)),
		WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			objectCalls.Add(1)
			target, ok := invocation.Target.Object()
			if !ok || target != client || invocation.Op != ObjectInvoke || invocation.Message != "getData" {
				return Null(), fmt.Errorf("opaque client object invocation = %#v", invocation)
			}
			return String("data-manager"), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("client-object.cna", `
sub probe_client {
    $client = getAggressorClient();
    return [$client getData];
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.InvokeBinding(context.Background(), BindingSub, "probe_client")
	if err != nil || result.String() != "data-manager" {
		t.Fatalf("probe_client = (%s, %v), want data-manager", result.Describe(), err)
	}
	if providerCalls.Load() != 1 || objectCalls.Load() != 1 {
		t.Fatalf("provider/object calls = %d/%d, want 1/1", providerCalls.Load(), objectCalls.Load())
	}
}

func TestAggressorClientServiceFallbackErrorsCancellationAndOverride(t *testing.T) {
	t.Parallel()

	t.Run("Host fallback exactly once", func(t *testing.T) {
		var calls atomic.Int32
		runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
			calls.Add(1)
			if invocation.Name != "mynick" {
				return Null(), fmt.Errorf("unexpected Host call %q", invocation.Name)
			}
			return String("host-nick"), nil
		})))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		result, err := runtimeInstance.Invoke(context.Background(), "mynick")
		if err != nil || result.String() != "host-nick" || calls.Load() != 1 {
			t.Fatalf("fallback = (%s, %v), calls %d", result.Describe(), err, calls.Load())
		}
	})

	t.Run("provider error is authoritative", func(t *testing.T) {
		boundaryErr := errors.New("client service failed")
		var hostCalls atomic.Int32
		runtimeInstance, err := New(
			WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
				hostCalls.Add(1)
				return Null(), nil
			})),
			WithAggressorClientServiceProvider(AggressorClientServiceProviderFunc(
				func(context.Context, AggressorClientServiceRequest) (Value, error) {
					return String("discard"), boundaryErr
				},
			)),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		result, err := runtimeInstance.Invoke(context.Background(), "say", String("hello"))
		if !result.IsNull() || !errors.Is(err, boundaryErr) || hostCalls.Load() != 0 {
			t.Fatalf("provider error = (%s, %v), Host calls %d", result.Describe(), err, hostCalls.Load())
		}
	})

	t.Run("pre-canceled context", func(t *testing.T) {
		var calls atomic.Int32
		var cancelDuring context.CancelFunc
		runtimeInstance, err := New(WithAggressorClientServiceProvider(
			AggressorClientServiceProviderFunc(func(context.Context, AggressorClientServiceRequest) (Value, error) {
				calls.Add(1)
				if cancelDuring != nil {
					cancelDuring()
				}
				return String("late"), nil
			}),
		))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := runtimeInstance.Invoke(ctx, "users")
		if !result.IsNull() || !errors.Is(err, context.Canceled) || calls.Load() != 0 {
			t.Fatalf("canceled query = (%s, %v), calls %d", result.Describe(), err, calls.Load())
		}

		during, cancel := context.WithCancel(context.Background())
		cancelDuring = cancel
		result, err = runtimeInstance.Invoke(during, "sync_download", String("remote"), String("local"))
		if !result.IsNull() || !errors.Is(err, context.Canceled) || calls.Load() != 1 {
			t.Fatalf("provider-canceled sync = (%s, %v), calls %d", result.Describe(), err, calls.Load())
		}
	})

	t.Run("WithFunction precedence", func(t *testing.T) {
		var providerCalls atomic.Int32
		runtimeInstance, err := New(
			WithAggressorClientServiceProvider(AggressorClientServiceProviderFunc(
				func(context.Context, AggressorClientServiceRequest) (Value, error) {
					providerCalls.Add(1)
					return Null(), nil
				},
			)),
			WithFunction("get_cs_version", func(context.Context, Invocation) (Value, error) {
				return String("override-version"), nil
			}),
			WithFunction("sync_download", func(context.Context, Invocation) (Value, error) {
				return String("override-sync"), nil
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		result, err := runtimeInstance.Invoke(context.Background(), "get_cs_version")
		if err != nil || result.String() != "override-version" || providerCalls.Load() != 0 {
			t.Fatalf("override = (%s, %v), provider calls %d", result.Describe(), err, providerCalls.Load())
		}
		// Deliberately violate sync_download's stock arity. Override selection
		// occurs before native-wrapper validation.
		result, err = runtimeInstance.Invoke(context.Background(), "sync_download", String("only-one"))
		if err != nil || result.String() != "override-sync" || providerCalls.Load() != 0 {
			t.Fatalf("sync override = (%s, %v), provider calls %d", result.Describe(), err, providerCalls.Load())
		}
	})
}

func TestAggressorSyncDownloadCallbackStatesMultiShotAndLifecycle(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []AggressorClientServiceRequest
	providerResult := ObjectValue(&struct{ ignored bool }{true})
	runtimeInstance, err := New(WithAggressorClientServiceProvider(
		AggressorClientServiceProviderFunc(func(_ context.Context, request AggressorClientServiceRequest) (Value, error) {
			mu.Lock()
			requests = append(requests, request)
			mu.Unlock()
			return providerResult, nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("sync-download-callbacks.cna", `
$calls = 0;
$sync_callback = {
	$calls++;
	return $1;
};
sub issue_omitted { return sync_download("remote-omitted", "local-omitted"); }
sub issue_null { return sync_download("remote-null", "local-null", $null); }
sub issue_callable { return sync_download("remote-callable", "local-callable", $sync_callback); }
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"issue_omitted", "issue_null", "issue_callable"} {
		result, callErr := owner.Call(context.Background(), name)
		if callErr != nil || !result.IsNull() {
			t.Fatalf("%s = (%s, %v), want null/nil", name, result.Describe(), callErr)
		}
	}

	mu.Lock()
	captured := append([]AggressorClientServiceRequest(nil), requests...)
	mu.Unlock()
	if len(captured) != 3 {
		t.Fatalf("sync provider requests = %d, want three", len(captured))
	}
	wantStates := []AggressorCallbackState{
		AggressorCallbackOmitted,
		AggressorCallbackNull,
		AggressorCallbackCallable,
	}
	for index, request := range captured {
		if request.Name != "sync_download" || request.Operation != AggressorClientServiceSyncDownload ||
			request.RuntimeID != runtimeInstance.ID() || request.Script != owner.ID() ||
			request.Span.Source != "sync-download-callbacks.cna" || len(request.Arguments) != 2 ||
			request.Arguments[0].String() != []string{"remote-omitted", "remote-null", "remote-callable"}[index] ||
			request.Arguments[1].String() != []string{"local-omitted", "local-null", "local-callable"}[index] ||
			request.CallbackState != wantStates[index] {
			t.Fatalf("sync request %d = %#v", index, request)
		}
		if (index < 2) != (request.Callback == nil) {
			t.Fatalf("sync request %d callback = %T", index, request.Callback)
		}
	}

	retained := captured[2].Callback
	for _, localPath := range []Value{String("/tmp/first"), String("/tmp/second")} {
		result, callbackErr := retained.Invoke(context.Background(), localPath)
		if callbackErr != nil || !result.IdentityEqual(localPath) {
			t.Fatalf("sync callback = (%s, %v), want identical %s", result.Describe(), callbackErr, localPath.Describe())
		}
	}
	if got := owner.Get("$calls").Int32(); got != 2 {
		t.Fatalf("sync callback calls = %d, want 2", got)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if result, callbackErr := retained.Invoke(canceled, String("ignored")); !result.IsNull() || !errors.Is(callbackErr, context.Canceled) {
		t.Fatalf("canceled sync callback = (%s, %v)", result.Describe(), callbackErr)
	}

	mu.Lock()
	beforeInvalid := len(requests)
	mu.Unlock()
	result, invokeErr := runtimeInstance.Invoke(
		context.Background(), "sync_download", String("remote"), String("local"), String("not callable"),
	)
	mu.Lock()
	afterInvalid := len(requests)
	mu.Unlock()
	if !result.IsNull() || !errors.Is(invokeErr, ErrInvalidCallable) || afterInvalid != beforeInvalid {
		t.Fatalf("invalid sync callback = (%s, %v), provider calls %d -> %d",
			result.Describe(), invokeErr, beforeInvalid, afterInvalid)
	}

	if err := owner.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result, callbackErr := retained.Invoke(context.Background(), String("ignored")); !result.IsNull() || !errors.Is(callbackErr, ErrScriptUnloaded) {
		t.Fatalf("unloaded sync callback = (%s, %v)", result.Describe(), callbackErr)
	}

	var closeRetained Callable
	closeRuntime, err := New(WithAggressorClientServiceProvider(
		AggressorClientServiceProviderFunc(func(_ context.Context, request AggressorClientServiceRequest) (Value, error) {
			closeRetained = request.Callback
			return Null(), nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	closeProgram, err := CompileString("sync-download-close.cna", `
sync_download("remote", "local", { return $1; });
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closeRuntime.Load(context.Background(), closeProgram); err != nil {
		t.Fatal(err)
	}
	if closeRetained == nil {
		t.Fatal("runtime-close sync request had no callback")
	}
	if err := closeRuntime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result, callbackErr := closeRetained.Invoke(context.Background(), String("ignored")); !result.IsNull() || !errors.Is(callbackErr, ErrScriptUnloaded) {
		t.Fatalf("runtime-closed sync callback = (%s, %v)", result.Describe(), callbackErr)
	}
}

func TestAggressorSyncDownloadHostFallbackPreservesRawInvocation(t *testing.T) {
	t.Parallel()

	remoteCell := NewCell(String("remote-before"))
	callbackCell := NewCell(String("not callable and Host-owned"))
	wantResult := ObjectValue(&struct{ host bool }{true})
	wantErr := errors.New("sync Host result")
	span := Span{Source: "sync-download-host.cna", Start: Position{Line: 8, Column: 2}}
	var captured Invocation
	var calls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls.Add(1)
		captured = invocation
		invocation.Arguments[0].Set(String("remote-mutated"))
		invocation.Arguments[2].Set(String("callback-mutated"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original := Invocation{
		Runtime: runtimeInstance,
		Script:  83,
		Name:    "sync_download",
		Span:    span,
		Arguments: []Argument{
			{Name: "$remote", Reference: remoteCell},
			{Value: String("local")},
			{Name: "$callback", Reference: callbackCell},
		},
	}

	result, invokeErr := runtimeInstance.aggressorClientService(context.Background(), original)
	if !result.IdentityEqual(wantResult) || !errors.Is(invokeErr, wantErr) || calls.Load() != 1 ||
		captured.Runtime != original.Runtime || captured.Script != original.Script || captured.Name != original.Name ||
		captured.Span != original.Span || &captured.Arguments[0] != &original.Arguments[0] ||
		captured.Arguments[0].Reference != remoteCell || captured.Arguments[2].Reference != callbackCell ||
		remoteCell.Get().String() != "remote-mutated" || callbackCell.Get().String() != "callback-mutated" {
		t.Fatalf("sync Host fallback = (%s, %v), calls %d, invocation %#v, remote/callback %s/%s",
			result.Describe(), invokeErr, calls.Load(), captured,
			remoteCell.Get().Describe(), callbackCell.Get().Describe())
	}
}

func TestAggressorSyncDownloadWithFunctionPrecedenceInEitherOptionOrder(t *testing.T) {
	for _, overrideFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("override-first=%v", overrideFirst), func(t *testing.T) {
			var providerCalls atomic.Int32
			providerOption := WithAggressorClientServiceProvider(AggressorClientServiceProviderFunc(
				func(context.Context, AggressorClientServiceRequest) (Value, error) {
					providerCalls.Add(1)
					return Null(), nil
				},
			))
			overrideOption := WithFunction("sync_download", func(context.Context, Invocation) (Value, error) {
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

			// Invalid stock arity proves the override ran before wrapper validation.
			result, invokeErr := runtimeInstance.Invoke(context.Background(), "sync_download", String("only-one"))
			if invokeErr != nil || result.String() != "override" || providerCalls.Load() != 0 {
				t.Fatalf("override = (%s, %v), provider calls %d", result.Describe(), invokeErr, providerCalls.Load())
			}
		})
	}
}

func TestAggressorSyncDownloadProviderSupportsConcurrentRequests(t *testing.T) {
	t.Parallel()

	const calls = 20
	entered := make(chan struct{}, calls)
	release := make(chan struct{})
	var mu sync.Mutex
	requests := make([]AggressorClientServiceRequest, 0, calls)
	runtimeInstance, err := New(WithAggressorClientServiceProvider(
		AggressorClientServiceProviderFunc(func(_ context.Context, request AggressorClientServiceRequest) (Value, error) {
			mu.Lock()
			requests = append(requests, request)
			mu.Unlock()
			entered <- struct{}{}
			<-release
			return String("discarded"), nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	errorsByCall := make(chan error, calls)
	var wait sync.WaitGroup
	for index := 0; index < calls; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, invokeErr := runtimeInstance.Invoke(
				context.Background(), "sync_download",
				String(fmt.Sprintf("remote-%d", index)), String(fmt.Sprintf("local-%d", index)),
			)
			if invokeErr == nil && !result.IsNull() {
				invokeErr = fmt.Errorf("sync result = %s, want null", result.Describe())
			}
			errorsByCall <- invokeErr
		}()
	}
	for index := 0; index < calls; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatalf("only %d/%d sync provider calls entered concurrently", index, calls)
		}
	}
	close(release)
	wait.Wait()
	close(errorsByCall)
	for invokeErr := range errorsByCall {
		if invokeErr != nil {
			t.Fatal(invokeErr)
		}
	}
	mu.Lock()
	captured := append([]AggressorClientServiceRequest(nil), requests...)
	mu.Unlock()
	if len(captured) != calls {
		t.Fatalf("concurrent sync requests = %d, want %d", len(captured), calls)
	}
	seen := make(map[string]bool, calls)
	for _, request := range captured {
		if request.Operation != AggressorClientServiceSyncDownload || request.CallbackState != AggressorCallbackOmitted ||
			request.Callback != nil || request.RuntimeID != runtimeInstance.ID() || len(request.Arguments) != 2 {
			t.Fatalf("concurrent sync request = %#v", request)
		}
		seen[request.Arguments[0].String()] = true
	}
	if len(seen) != calls {
		t.Fatalf("concurrent sync remote paths = %d unique, want %d", len(seen), calls)
	}
}

type typedNilAggressorClientServiceProvider struct{}

func (*typedNilAggressorClientServiceProvider) HandleAggressorClientService(
	context.Context,
	AggressorClientServiceRequest,
) (Value, error) {
	panic("typed-nil Aggressor client service provider was invoked")
}

func TestAggressorClientServiceProviderRejectsTypedNilAndNilAdapter(t *testing.T) {
	t.Parallel()

	var provider *typedNilAggressorClientServiceProvider
	if _, err := New(WithAggressorClientServiceProvider(provider)); err == nil || err.Error() != "opfor: Aggressor client service provider is nil" {
		t.Fatalf("typed-nil provider error = %v", err)
	}
	if _, err := New(WithAggressorClientServiceProvider(AggressorClientServiceProviderFunc(nil))); err == nil || err.Error() != "opfor: Aggressor client service provider is nil" {
		t.Fatalf("nil provider function option error = %v", err)
	}
	result, err := AggressorClientServiceProviderFunc(nil).HandleAggressorClientService(
		context.Background(), AggressorClientServiceRequest{},
	)
	if !result.IsNull() || err == nil || err.Error() != "opfor: Aggressor client service provider is nil" {
		t.Fatalf("nil provider function call = (%s, %v)", result.Describe(), err)
	}
}

func TestPortableScriptLoaderInheritsAggressorClientServiceProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-client-service.cna")
	if err := os.WriteFile(childPath, []byte(`
sync_download("remote-child", "local-child", { return "child:" . $1; });
`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-client-service.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
sync_download("remote-parent", "local-parent");
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []AggressorClientServiceRequest
	var childCallbackResult Value
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader client-service route reached Host")
		})),
		WithAggressorClientServiceProvider(AggressorClientServiceProviderFunc(
			func(ctx context.Context, request AggressorClientServiceRequest) (Value, error) {
				callbackResult := Null()
				if request.Callback != nil {
					var callbackErr error
					callbackResult, callbackErr = request.Callback.Invoke(ctx, String("local-child"))
					if callbackErr != nil {
						return Null(), fmt.Errorf("invoke child sync callback: %w", callbackErr)
					}
				}
				mu.Lock()
				requests = append(requests, request)
				if request.Callback != nil {
					childCallbackResult = callbackResult
				}
				mu.Unlock()
				return String("operator"), nil
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("inherited requests reached Host %d time(s)", hostCalls.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want parent plus child", len(requests))
	}
	if requests[0].Operation != AggressorClientServiceSyncDownload || requests[1].Operation != AggressorClientServiceSyncDownload ||
		requests[0].RuntimeID != runtimeInstance.ID() || requests[0].RuntimeID == 0 ||
		requests[1].RuntimeID == 0 || requests[1].RuntimeID == requests[0].RuntimeID ||
		requests[0].Script != 1 || requests[1].Script != 1 ||
		requests[0].Span.Source != "parent-client-service.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) ||
		requests[0].CallbackState != AggressorCallbackOmitted || requests[0].Callback != nil ||
		requests[1].CallbackState != AggressorCallbackCallable || requests[1].Callback == nil ||
		len(requests[0].Arguments) != 2 || requests[0].Arguments[0].String() != "remote-parent" ||
		len(requests[1].Arguments) != 2 || requests[1].Arguments[0].String() != "remote-child" {
		t.Fatalf("parent/child requests = %#v", requests)
	}
	if childCallbackResult.String() != "child:local-child" {
		t.Fatalf("child sync callback = %s", childCallbackResult.Describe())
	}
}

func TestAggressorClientServiceUnsupportedAndNilRuntimePolicy(t *testing.T) {
	t.Parallel()

	span := Span{Source: "unsupported-client-service.cna", Start: Position{Line: 4, Column: 3}}
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.aggressorClientService(context.Background(), Invocation{
		Name: "unknown_client_service", Span: span,
	})
	var unsupported *UnsupportedError
	if !result.IsNull() || !errors.As(err, &unsupported) ||
		unsupported.Operation != "Aggressor client service" ||
		unsupported.Name != "unknown_client_service" || unsupported.Span != span {
		t.Fatalf("unknown client service = (%s, %#v), want typed UnsupportedError", result.Describe(), err)
	}

	var nilRuntime *Runtime
	result, err = nilRuntime.aggressorClientService(context.Background(), Invocation{Name: "mynick"})
	if !result.IsNull() || err == nil || err.Error() != "opfor: runtime is nil" {
		t.Fatalf("nil Runtime mynick = (%s, %v), want null/runtime error", result.Describe(), err)
	}
}

type aggressorClientServiceObject struct{ name string }

func (object *aggressorClientServiceObject) String() string {
	if object == nil {
		return "<nil client service object>"
	}
	return object.name
}

func TestAggressorClientServiceRequestArgumentsAreDetachedAtTopLevel(t *testing.T) {
	t.Parallel()

	arguments := []Value{String("neo"), String("topic"), HashValue(NewHash())}
	var captured AggressorClientServiceRequest
	runtimeInstance, err := New(WithAggressorClientServiceProvider(
		AggressorClientServiceProviderFunc(func(_ context.Context, request AggressorClientServiceRequest) (Value, error) {
			captured = request
			return Null(), nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Invoke(context.Background(), "custom_event_private", arguments...); err != nil {
		t.Fatal(err)
	}
	arguments[0] = String("mutated")
	if got := captured.Arguments[0].String(); got != "neo" {
		t.Fatalf("captured top-level argument = %q, want neo", got)
	}
	if !reflect.DeepEqual(captured.Arguments[1:], []Value{String("topic"), captured.Arguments[2]}) {
		t.Fatalf("captured arguments = %#v", captured.Arguments)
	}
}
