package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingAggressorPreferenceProvider struct {
	mu       sync.Mutex
	requests []AggressorPreferenceRequest
	handle   func(context.Context, AggressorPreferenceRequest) (Value, error)
}

func (provider *recordingAggressorPreferenceProvider) HandleAggressorPreference(
	ctx context.Context,
	request AggressorPreferenceRequest,
) (Value, error) {
	provider.mu.Lock()
	provider.requests = append(provider.requests, request)
	handle := provider.handle
	provider.mu.Unlock()
	if handle == nil {
		return Null(), nil
	}
	return handle(ctx, request)
}

func (provider *recordingAggressorPreferenceProvider) snapshot() []AggressorPreferenceRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorPreferenceRequest(nil), provider.requests...)
}

type aggressorPreferenceTestObject struct{ label string }

func (object *aggressorPreferenceTestObject) SleepDescribe() string {
	if object == nil {
		return "<nil-preference-object>"
	}
	return "<preference-object:" + object.label + ">"
}

func TestAggressorPreferenceFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPreferenceFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{"pref_get", "pref_get_list", "pref_set", "pref_set_list"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor preference names = %q, want %q", names, want)
	}
}

func TestAggressorPreferenceOperationsResolveOncePreserveIdentityAndProvenance(t *testing.T) {
	t.Parallel()

	name := ArrayValue(NewArray(String("provider-key")))
	defaultValue := HashValue(NewOrderedHash())
	scalarValue := ObjectValue(&aggressorPreferenceTestObject{label: "scalar"})
	listValue := ArrayValue(NewArray(String("a"), String("b")))
	returnedList := ArrayValue(NewArray(String("provider-owned")))
	returnedScalar := ObjectValue(&aggressorPreferenceTestObject{label: "returned"})
	tests := []struct {
		name      string
		operation AggressorPreferenceOperation
		second    Value
		result    Value
		wantValue bool
	}{
		{name: "pref_get", operation: AggressorPreferenceGet, second: defaultValue, result: returnedScalar, wantValue: true},
		{name: "pref_get_list", operation: AggressorPreferenceGetList, result: returnedList, wantValue: true},
		{name: "pref_set", operation: AggressorPreferenceSet, second: scalarValue, result: returnedScalar},
		{name: "pref_set_list", operation: AggressorPreferenceSetList, second: listValue, result: returnedList},
	}
	for index, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var hostCalls atomic.Int32
			var captured AggressorPreferenceRequest
			provider := AggressorPreferenceProviderFunc(func(_ context.Context, request AggressorPreferenceRequest) (Value, error) {
				captured = request
				return test.result, nil
			})
			runtimeInstance, err := New(
				WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), errors.New("preference provider route reached Host")
				})),
				WithAggressorPreferenceProvider(provider),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			nameCell := NewCell(name)
			arguments := []Argument{{Name: "$name", Reference: nameCell}}
			var secondCell *Cell
			if test.name != "pref_get_list" {
				secondCell = NewCell(test.second)
				arguments = append(arguments, Argument{Name: "$value", Reference: secondCell})
			}
			span := Span{Source: "preference-provenance.cna", Start: Position{Line: index + 3, Column: 5}}
			invocation := Invocation{
				Runtime:   runtimeInstance,
				Script:    41,
				Name:      test.name,
				Span:      span,
				Arguments: arguments,
			}
			result, invokeErr := runtimeInstance.aggressorPreference(context.Background(), invocation)
			if invokeErr != nil {
				t.Fatal(invokeErr)
			}
			if test.wantValue {
				if !result.IdentityEqual(test.result) {
					t.Fatalf("%s result = %s, want identical %s", test.name, result.Describe(), test.result.Describe())
				}
			} else if !result.IsNull() {
				t.Fatalf("%s result = %s, want $null setter result", test.name, result.Describe())
			}
			if hostCalls.Load() != 0 {
				t.Fatalf("%s provider route reached Host %d time(s)", test.name, hostCalls.Load())
			}
			if captured.Operation != test.operation || captured.RuntimeID != runtimeInstance.ID() || captured.RuntimeID == 0 || captured.Script != 41 || captured.Span != span {
				t.Fatalf("%s request provenance/operation = %#v", test.name, captured)
			}
			if !captured.PreferenceName.IdentityEqual(name) {
				t.Fatalf("%s preference name = %s, want identical %s", test.name, captured.PreferenceName.Describe(), name.Describe())
			}
			switch test.operation {
			case AggressorPreferenceGet:
				if !captured.DefaultValue.IdentityEqual(defaultValue) || captured.PreferenceValue.Kind() != KindNull {
					t.Fatalf("pref_get value fields = default %s/value %s", captured.DefaultValue.Describe(), captured.PreferenceValue.Describe())
				}
			case AggressorPreferenceSet, AggressorPreferenceSetList:
				if !captured.PreferenceValue.IdentityEqual(test.second) || captured.DefaultValue.Kind() != KindNull {
					t.Fatalf("%s value fields = default %s/value %s", test.name, captured.DefaultValue.Describe(), captured.PreferenceValue.Describe())
				}
			default:
				if captured.DefaultValue.Kind() != KindNull || captured.PreferenceValue.Kind() != KindNull {
					t.Fatalf("pref_get_list unused fields = default %s/value %s", captured.DefaultValue.Describe(), captured.PreferenceValue.Describe())
				}
			}

			// Provider-facing Values are a one-time snapshot. Later source Cell
			// writes do not change the request, while compound identity is retained.
			nameCell.Set(String("changed name"))
			if secondCell != nil {
				secondCell.Set(String("changed value"))
			}
			if !captured.PreferenceName.IdentityEqual(name) {
				t.Fatal("request preference name was not detached from its source Cell")
			}
		})
	}
}

func TestAggressorPreferenceProviderOwnsQueryKindAndMissingListPolicy(t *testing.T) {
	t.Parallel()

	// The public reference does not describe a pref_get_list missing-key value.
	// A typed provider may therefore use $null (or enforce a different policy)
	// without OPFOR inventing an empty array or retrying through Host.
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("unexpected Host result"), nil
		})),
		WithAggressorPreferenceProvider(AggressorPreferenceProviderFunc(func(context.Context, AggressorPreferenceRequest) (Value, error) {
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, invokeErr := runtimeInstance.Invoke(context.Background(), "pref_get_list", String("missing"))
	if invokeErr != nil || result.Kind() != KindNull || hostCalls.Load() != 0 {
		t.Fatalf("missing list provider policy = (%s, %v), Host calls %d", result.Describe(), invokeErr, hostCalls.Load())
	}
}

func TestAggressorPreferenceInvalidAritiesAndDocumentedListTypeStopAllBoundaries(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorPreferenceProvider(AggressorPreferenceProviderFunc(func(context.Context, AggressorPreferenceRequest) (Value, error) {
			providerCalls.Add(1)
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	invalid := map[string][][]Value{
		"pref_get":      {nil, {String("name")}, {String("name"), String("default"), String("extra")}},
		"pref_get_list": {nil, {String("name"), String("extra")}},
		"pref_set":      {nil, {String("name")}, {String("name"), String("value"), String("extra")}},
		"pref_set_list": {nil, {String("name")}, {String("name"), String("value"), String("extra")}, {String("name"), String("not an array")}},
	}
	for name, calls := range invalid {
		for _, arguments := range calls {
			result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
			if invokeErr == nil || !result.IsNull() {
				t.Errorf("%s/%d = (%s, %v), want null validation error", name, len(arguments), result.Describe(), invokeErr)
			}
			if name == "pref_set_list" && len(arguments) == 2 && !strings.Contains(invokeErr.Error(), "argument 2 must be an array") {
				t.Errorf("pref_set_list type error = %v", invokeErr)
			}
		}
	}
	if providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid calls reached provider/Host = %d/%d", providerCalls.Load(), hostCalls.Load())
	}

	// The same public type validation applies before the raw Host route.
	unsetRuntime, err := New(WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
		hostCalls.Add(1)
		return Null(), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unsetRuntime.Close(context.Background()) })
	result, invokeErr := unsetRuntime.Invoke(context.Background(), "pref_set_list", String("name"), String("not an array"))
	if invokeErr == nil || !result.IsNull() || hostCalls.Load() != 0 {
		t.Fatalf("unset-provider list type route = (%s, %v), Host calls %d", result.Describe(), invokeErr, hostCalls.Load())
	}
}

func TestAggressorPreferenceUnsetProviderPreservesRawHostInvocation(t *testing.T) {
	t.Parallel()

	wantResult := ObjectValue(&aggressorPreferenceTestObject{label: "host-result"})
	wantErr := errors.New("host preference result")
	nameCell := NewCell(String("original-name"))
	list := ArrayValue(NewArray(String("a")))
	listCell := NewCell(list)
	span := Span{Source: "preference-host.cna", Start: Position{Line: 9, Column: 4}}
	var captured Invocation
	var calls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls.Add(1)
		captured = invocation
		invocation.Arguments[0].Set(String("mutated name"))
		invocation.Arguments[1].Set(ArrayValue(NewArray(String("mutated list"))))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original := Invocation{
		Runtime: runtimeInstance,
		Script:  29,
		Name:    "pref_set_list",
		Span:    span,
		Arguments: []Argument{
			{Name: "$name", Reference: nameCell},
			{Name: "@values", Reference: listCell},
		},
	}

	result, invokeErr := runtimeInstance.aggressorPreference(context.Background(), original)
	if !errors.Is(invokeErr, wantErr) || !result.IdentityEqual(wantResult) {
		t.Fatalf("Host fallback = (%s, %v), want identical result/%v", result.Describe(), invokeErr, wantErr)
	}
	if calls.Load() != 1 || captured.Runtime != original.Runtime || captured.Script != 29 || captured.Name != "pref_set_list" || captured.Span != span {
		t.Fatalf("captured Host invocation/calls = %#v/%d", captured, calls.Load())
	}
	if len(captured.Arguments) != 2 || captured.Arguments[0].Reference != nameCell || captured.Arguments[1].Reference != listCell {
		t.Fatalf("Host did not receive exact reference-bearing arguments: %#v", captured.Arguments)
	}
	if nameCell.Get().String() != "mutated name" || listCell.Get().Describe() != "@('mutated list')" {
		t.Fatalf("Host mutations were not visible: %s/%s", nameCell.Get().Describe(), listCell.Get().Describe())
	}
}

func TestAggressorPreferenceUnsetProviderRoutesEveryOperationToHostOnce(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := make(map[string]int, len(aggressorPreferenceSpecs))
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		mu.Lock()
		calls[invocation.Name]++
		mu.Unlock()
		return String("host:" + invocation.Name), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	valid := map[string][]Value{
		"pref_get":      {String("name"), String("default")},
		"pref_get_list": {String("name")},
		"pref_set":      {String("name"), String("value")},
		"pref_set_list": {String("name"), ArrayValue(NewArray(String("value")))},
	}
	for name, arguments := range valid {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
		if invokeErr != nil || result.String() != "host:"+name {
			t.Errorf("%s Host route = (%s, %v)", name, result.Describe(), invokeErr)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != len(valid) {
		t.Fatalf("Host operation count = %d, want %d: %#v", len(calls), len(valid), calls)
	}
	for name := range valid {
		if calls[name] != 1 {
			t.Errorf("%s Host calls = %d, want one", name, calls[name])
		}
	}
}

func TestAggressorPreferenceProviderErrorsAreAuthoritative(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("provider failed after preference mutation")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host result"), nil
		})),
		WithAggressorPreferenceProvider(AggressorPreferenceProviderFunc(func(context.Context, AggressorPreferenceRequest) (Value, error) {
			providerCalls.Add(1)
			return String("discarded partial result"), wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, invokeErr := runtimeInstance.Invoke(context.Background(), "pref_set", String("name"), String("value"))
	if !errors.Is(invokeErr, wantErr) || !result.IsNull() || providerCalls.Load() != 1 || hostCalls.Load() != 0 {
		t.Fatalf("provider error = (%s, %v), provider/Host %d/%d", result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorPreferenceWithFunctionPrecedenceAndNilPolicy(t *testing.T) {
	for name := range aggressorPreferenceSpecs {
		name := name
		for _, overrideFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/override-first=%v", name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				var hostCalls atomic.Int32
				hostOption := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), nil
				}))
				providerOption := WithAggressorPreferenceProvider(AggressorPreferenceProviderFunc(func(context.Context, AggressorPreferenceRequest) (Value, error) {
					providerCalls.Add(1)
					return String("provider"), nil
				}))
				overrideOption := WithFunction(name, func(context.Context, Invocation) (Value, error) {
					return String("override"), nil
				})
				options := []Option{hostOption, providerOption, overrideOption}
				if overrideFirst {
					options = []Option{hostOption, overrideOption, providerOption}
				}
				runtimeInstance, err := New(options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
				// Invalid native-wrapper arity proves the override was selected first.
				result, invokeErr := runtimeInstance.Invoke(context.Background(), name)
				if invokeErr != nil || result.String() != "override" || providerCalls.Load() != 0 || hostCalls.Load() != 0 {
					t.Fatalf("override = (%s, %v), provider/Host %d/%d", result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
				}
			})
		}
	}

	var typedNil *recordingAggressorPreferenceProvider
	if _, err := New(WithAggressorPreferenceProvider(typedNil)); err == nil || err.Error() != "opfor: Aggressor preference provider is nil" {
		t.Fatalf("typed-nil provider error = %v", err)
	}
	var nilFunction AggressorPreferenceProviderFunc
	if _, err := New(WithAggressorPreferenceProvider(nilFunction)); err == nil || err.Error() != "opfor: Aggressor preference provider is nil" {
		t.Fatalf("nil provider function error = %v", err)
	}
	if _, err := nilFunction.HandleAggressorPreference(context.Background(), AggressorPreferenceRequest{}); err == nil {
		t.Fatal("direct nil preference provider function call succeeded")
	}
}

func TestAggressorPreferenceCancellationAndClose(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var cancelDuring context.CancelFunc
	provider := AggressorPreferenceProviderFunc(func(_ context.Context, request AggressorPreferenceRequest) (Value, error) {
		calls.Add(1)
		cancelDuring()
		return request.DefaultValue, nil
	})
	runtimeInstance, err := New(WithAggressorPreferenceProvider(provider))
	if err != nil {
		t.Fatal(err)
	}

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, invokeErr := runtimeInstance.Invoke(preCanceled, "pref_get", String("name"), String("default")); !errors.Is(invokeErr, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("pre-canceled error/provider calls = %v/%d", invokeErr, calls.Load())
	}
	during, cancel := context.WithCancel(context.Background())
	cancelDuring = cancel
	result, invokeErr := runtimeInstance.Invoke(during, "pref_get", String("name"), String("default"))
	if !errors.Is(invokeErr, context.Canceled) || !result.IsNull() || calls.Load() != 1 {
		t.Fatalf("cancel-during = (%s, %v), provider calls %d", result.Describe(), invokeErr, calls.Load())
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, invokeErr := runtimeInstance.Invoke(context.Background(), "pref_get_list", String("closed")); !errors.Is(invokeErr, ErrRuntimeClosed) || calls.Load() != 1 {
		t.Fatalf("closed Runtime error/provider calls = %v/%d", invokeErr, calls.Load())
	}
}

func TestAggressorPreferenceCloseCancelsBlockingProvider(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	var once sync.Once
	runtimeInstance, err := New(WithAggressorPreferenceProvider(AggressorPreferenceProviderFunc(func(ctx context.Context, _ AggressorPreferenceRequest) (Value, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return Null(), ctx.Err()
	})))
	if err != nil {
		t.Fatal(err)
	}

	invokeDone := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.Invoke(context.Background(), "pref_get_list", String("blocking"))
		invokeDone <- invokeErr
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("preference provider did not start")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtimeInstance.Close(context.Background()) }()
	select {
	case invokeErr := <-invokeDone:
		if !errors.Is(invokeErr, context.Canceled) {
			t.Errorf("blocking provider error = %v, want context.Canceled", invokeErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocking preference provider did not stop on Runtime.Close")
	}
	select {
	case closeErr := <-closeDone:
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Runtime.Close did not finish")
	}
}

func TestAggressorPreferenceProviderSupportsConcurrentCalls(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorPreferenceProvider{
		handle: func(_ context.Context, request AggressorPreferenceRequest) (Value, error) {
			return request.PreferenceName, nil
		},
	}
	runtimeInstance, err := New(WithAggressorPreferenceProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	const calls = 48
	var wait sync.WaitGroup
	errorsByCall := make(chan error, calls)
	for index := 0; index < calls; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			name := fmt.Sprintf("preference-%d", index)
			result, invokeErr := runtimeInstance.Invoke(context.Background(), "pref_get", String(name), String("default"))
			if invokeErr == nil && result.String() != name {
				invokeErr = fmt.Errorf("result = %s", result.Describe())
			}
			errorsByCall <- invokeErr
		}()
	}
	wait.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		if err != nil {
			t.Fatal(err)
		}
	}
	requests := provider.snapshot()
	if len(requests) != calls {
		t.Fatalf("concurrent provider calls = %d, want %d", len(requests), calls)
	}
	for index, request := range requests {
		if request.RuntimeID != runtimeInstance.ID() || request.Operation != AggressorPreferenceGet {
			t.Errorf("request %d route/provenance = %#v", index, request)
		}
	}
}

func TestPortableScriptLoaderInheritsAggressorPreferenceProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-preference.cna")
	if err := os.WriteFile(childPath, []byte(`pref_set_list("child", @("a", "b"));`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-preference.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
pref_get("parent", "default");
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorPreferenceProvider{
		handle: func(_ context.Context, request AggressorPreferenceRequest) (Value, error) {
			return request.DefaultValue, nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("preference provider was not inherited")
		})),
		WithAggressorPreferenceProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("inherited provider requests reached Host %d time(s)", hostCalls.Load())
	}
	requests := provider.snapshot()
	if len(requests) != 2 || requests[0].Operation != AggressorPreferenceGet || requests[0].PreferenceName.String() != "parent" ||
		requests[1].Operation != AggressorPreferenceSetList || requests[1].PreferenceName.String() != "child" {
		t.Fatalf("parent/child requests = %#v", requests)
	}
	childList, ok := requests[1].PreferenceValue.Array()
	if !ok || childList == nil || childList.Len() != 2 {
		t.Fatalf("child list value = %s", requests[1].PreferenceValue.Describe())
	}
	if requests[0].RuntimeID != runtimeInstance.ID() || requests[1].RuntimeID == requests[0].RuntimeID || requests[0].RuntimeID == 0 || requests[1].RuntimeID == 0 {
		t.Fatalf("parent/child RuntimeIDs = %d/%d", requests[0].RuntimeID, requests[1].RuntimeID)
	}
	if requests[0].Script != 1 || requests[1].Script != 1 || requests[0].Span.Source != "parent-preference.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child provenance = %#v", requests)
	}
}
