package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
)

type recordingAggressorDataStoreProvider struct {
	mu       sync.Mutex
	requests []AggressorDataStoreRequest
	handle   func(context.Context, AggressorDataStoreRequest) (Value, error)
}

type aggressorDataStoreTestObject struct{}

func (*aggressorDataStoreTestObject) SleepDescribe() string { return "<data-store-model>" }

func (provider *recordingAggressorDataStoreProvider) HandleAggressorDataStore(
	ctx context.Context,
	request AggressorDataStoreRequest,
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

func (provider *recordingAggressorDataStoreProvider) snapshot() []AggressorDataStoreRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorDataStoreRequest(nil), provider.requests...)
}

func TestAggressorDataStoreFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorDataStoreFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{
		"applications", "archives", "credential_add", "credentials", "downloads", "highlight",
		"host_delete", "host_info", "host_update", "hosts", "keystrokes",
		"redactobject", "resetData", "screenshots", "services", "targets", "tokenToEmail",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor data-store names = %q, want %q", names, want)
	}
}

func TestAggressorDataStoreOperationsAritiesProvenanceAndResults(t *testing.T) {
	t.Parallel()

	compound := ArrayValue(NewArray(String("host-a"), String("host-b")))
	model := ObjectValue(&aggressorDataStoreTestObject{})
	tests := []struct {
		name          string
		operation     AggressorDataStoreOperation
		arguments     []Value
		discardResult bool
	}{
		{name: "credential_add", operation: AggressorDataStoreCredentialAdd, arguments: []Value{String("alice"), String("secret")}},
		{name: "credential_add", operation: AggressorDataStoreCredentialAdd, arguments: []Value{String("alice"), String("secret"), Null(), String("source"), String("host"), String("password"), String("notes")}},
		{name: "credentials", operation: AggressorDataStoreCredentials},
		{name: "tokenToEmail", operation: AggressorDataStoreTokenToEmail, arguments: []Value{String("token")}},
		{name: "applications", operation: AggressorDataStoreApplications},
		{name: "archives", operation: AggressorDataStoreArchives},
		{name: "downloads", operation: AggressorDataStoreDownloads},
		{name: "highlight", operation: AggressorDataStoreHighlight, arguments: []Value{model, compound, String("blue")}},
		{name: "keystrokes", operation: AggressorDataStoreKeystrokes},
		{name: "screenshots", operation: AggressorDataStoreScreenshots},
		{name: "services", operation: AggressorDataStoreServices},
		{name: "targets", operation: AggressorDataStoreTargets},
		{name: "hosts", operation: AggressorDataStoreHosts},
		{name: "host_info", operation: AggressorDataStoreHostInfo, arguments: []Value{String("host-a")}},
		{name: "host_info", operation: AggressorDataStoreHostInfo, arguments: []Value{String("host-a"), Null()}},
		{name: "host_update", operation: AggressorDataStoreHostUpdate, arguments: []Value{String("host-a"), String("dns"), String("Windows"), String("11")}},
		{name: "host_update", operation: AggressorDataStoreHostUpdate, arguments: []Value{String("host-a"), String("dns"), String("Windows"), String("11"), Null()}},
		{name: "host_delete", operation: AggressorDataStoreHostDelete, arguments: []Value{compound}},
		{name: "redactobject", operation: AggressorDataStoreRedactObject, arguments: []Value{String("postex-object-id")}, discardResult: true},
		{name: "resetData", operation: AggressorDataStoreResetData},
	}
	for index, test := range tests {
		test := test
		t.Run(fmt.Sprintf("%02d-%s-%d", index, test.name, len(test.arguments)), func(t *testing.T) {
			provided := HashValue(NewOrderedHash())
			var hostCalls atomic.Int32
			provider := &recordingAggressorDataStoreProvider{
				handle: func(context.Context, AggressorDataStoreRequest) (Value, error) {
					return provided, nil
				},
			}
			runtimeInstance, err := New(
				WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), errors.New("data-store provider route reached Host")
				})),
				WithAggressorDataStoreProvider(provider),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
			wantResult := provided
			if test.discardResult {
				wantResult = Null()
			}
			if err != nil || !result.IdentityEqual(wantResult) {
				t.Fatalf("%s result = (%s, %v), want %s",
					test.name, result.Describe(), err, wantResult.Describe())
			}
			if hostCalls.Load() != 0 {
				t.Fatalf("%s provider route reached Host %d time(s)", test.name, hostCalls.Load())
			}
			requests := provider.snapshot()
			if len(requests) != 1 {
				t.Fatalf("%s provider calls = %d, want one", test.name, len(requests))
			}
			request := requests[0]
			if request.Name != test.name || request.Operation != test.operation || request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 || request.Script != 0 || request.Span != (Span{}) {
				t.Fatalf("%s request metadata = %#v", test.name, request)
			}
			if len(request.Arguments) != len(test.arguments) {
				t.Fatalf("%s request argument count = %d, want %d", test.name, len(request.Arguments), len(test.arguments))
			}
			for argumentIndex, argument := range test.arguments {
				if !request.Arguments[argumentIndex].IdentityEqual(argument) {
					t.Fatalf("%s request argument %d = %s, want identical %s",
						test.name, argumentIndex, request.Arguments[argumentIndex].Describe(), argument.Describe())
				}
			}
			if request.HasArgument(len(test.arguments)) || !request.Arg(len(test.arguments)).IsNull() || request.HasArgument(-1) {
				t.Fatalf("%s absent argument policy failed for %#v", test.name, request.Arguments)
			}
		})
	}
}

func TestAggressorDataStoreInvalidAritiesStopProviderAndHost(t *testing.T) {
	t.Parallel()

	var hostCalls atomic.Int32
	provider := &recordingAggressorDataStoreProvider{}
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorDataStoreProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	invalid := map[string][]int{
		"credential_add": {0, 1, 8},
		"credentials":    {1},
		"tokenToEmail":   {0, 2},
		"applications":   {1},
		"archives":       {1},
		"downloads":      {1},
		"highlight":      {0, 2, 4},
		"keystrokes":     {1},
		"screenshots":    {1},
		"services":       {1},
		"targets":        {1},
		"hosts":          {1},
		"host_info":      {0, 3},
		"host_update":    {0, 3, 6},
		"host_delete":    {0, 2},
		"redactobject":   {0, 2},
		"resetData":      {1},
	}
	for name, counts := range invalid {
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
	if got := len(provider.snapshot()); got != 0 {
		t.Fatalf("invalid arities reached provider %d time(s)", got)
	}
	if got := hostCalls.Load(); got != 0 {
		t.Fatalf("invalid arities reached Host %d time(s)", got)
	}
}

func TestAggressorDataStoreResolvesArgumentsOnceAndPreservesIdentity(t *testing.T) {
	t.Parallel()

	compound := ArrayValue(NewArray(String("host-a")))
	hostCell := NewCell(compound)
	noteCell := NewCell(Null())
	span := Span{Source: "data-store-provenance.cna", Start: Position{Line: 7, Column: 3}}
	var captured AggressorDataStoreRequest
	provider := AggressorDataStoreProviderFunc(func(_ context.Context, request AggressorDataStoreRequest) (Value, error) {
		captured = request
		hostCell.Set(String("changed"))
		noteCell.Set(String("late note"))
		return request.Arg(0), nil
	})
	runtimeInstance, err := New(WithAggressorDataStoreProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  41,
		Name:    "host_delete",
		Span:    span,
		Arguments: []Argument{
			{Name: "$hosts", Reference: hostCell},
		},
	}
	result, err := runtimeInstance.aggressorDataStore(context.Background(), invocation)
	if err != nil || !result.IdentityEqual(compound) || !captured.Arg(0).IdentityEqual(compound) {
		t.Fatalf("resolved request = result %s/captured %s/error %v, want original compound identity",
			result.Describe(), captured.Arg(0).Describe(), err)
	}
	if captured.RuntimeID != runtimeInstance.ID() || captured.Script != 41 || captured.Span != span {
		t.Fatalf("provider provenance = runtime %d script %d span %s", captured.RuntimeID, captured.Script, captured.Span)
	}
	if got := hostCell.Get().String(); got != "changed" {
		t.Fatalf("source reference mutation = %q, want changed", got)
	}

	// Omission remains distinct from an explicit null optional position.
	invocation.Name = "host_update"
	invocation.Arguments = []Argument{
		{Value: String("host")}, {Value: String("dns")}, {Value: String("os")}, {Value: String("version")},
		{Name: "$note", Reference: noteCell},
	}
	noteCell.Set(Null())
	if _, err := runtimeInstance.aggressorDataStore(context.Background(), invocation); err != nil {
		t.Fatal(err)
	}
	if len(captured.Arguments) != 5 || !captured.HasArgument(4) || !captured.Arg(4).IsNull() || captured.HasArgument(5) {
		t.Fatalf("explicit-null optional argument = %#v", captured.Arguments)
	}
}

func TestAggressorDataStoreUnsetProviderPreservesHostInvocationOnce(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("host data-store result")
	wantResult := HashValue(NewHash())
	modelCell := NewCell(String("downloads"))
	rowsCell := NewCell(ArrayValue(NewArray(String("row"))))
	accentCell := NewCell(String("blue"))
	span := Span{Source: "data-store-host.cna", Start: Position{Line: 9, Column: 4}}
	var captured Invocation
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		captured = invocation
		invocation.Arguments[2].Set(String("mutated by Host"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original := Invocation{
		Runtime: runtimeInstance,
		Script:  29,
		Name:    "highlight",
		Span:    span,
		Arguments: []Argument{
			{Name: "$model", Reference: modelCell},
			{Name: "@rows", Reference: rowsCell},
			{Name: "$accent", Reference: accentCell},
		},
	}

	result, err := runtimeInstance.aggressorDataStore(context.Background(), original)
	if !errors.Is(err, wantErr) || !result.IdentityEqual(wantResult) {
		t.Fatalf("Host fallback = (%s, %v), want identical result/%v", result.Describe(), err, wantErr)
	}
	if hostCalls.Load() != 1 || captured.Runtime != original.Runtime || captured.Script != original.Script || captured.Name != original.Name || captured.Span != original.Span || len(captured.Arguments) != 3 {
		t.Fatalf("captured Host invocation/calls = %#v/%d", captured, hostCalls.Load())
	}
	if captured.Arguments[0].Reference != modelCell || captured.Arguments[1].Reference != rowsCell || captured.Arguments[2].Reference != accentCell || accentCell.Get().String() != "mutated by Host" {
		t.Fatalf("Host did not receive the original reference-bearing arguments: %#v", captured.Arguments)
	}
}

func TestAggressorDataStoreUnsetProviderRoutesEveryOperationToHostOnce(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := make(map[string]int, len(aggressorDataStoreSpecs))
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

	for name, spec := range aggressorDataStoreSpecs {
		arguments := make([]Value, spec.minimum)
		for index := range arguments {
			arguments[index] = Int(int32(index))
		}
		result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
		if invokeErr != nil || result.String() != "host:"+name {
			t.Errorf("%s Host route = (%s, %v)", name, result.Describe(), invokeErr)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != len(aggressorDataStoreSpecs) {
		t.Fatalf("Host operation count = %d, want %d: %#v", len(calls), len(aggressorDataStoreSpecs), calls)
	}
	for name := range aggressorDataStoreSpecs {
		if calls[name] != 1 {
			t.Errorf("%s Host calls = %d, want one", name, calls[name])
		}
	}
}

func TestAggressorDataStoreProviderErrorsAreAuthoritative(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("provider failed after mutation")
	var hostCalls atomic.Int32
	var providerCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host result"), nil
		})),
		WithAggressorDataStoreProvider(AggressorDataStoreProviderFunc(func(context.Context, AggressorDataStoreRequest) (Value, error) {
			providerCalls.Add(1)
			return String("discarded provider partial result"), wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	for _, test := range []struct {
		name      string
		arguments []Value
	}{
		{name: "credential_add", arguments: []Value{String("alice"), String("secret")}},
		{name: "redactobject", arguments: []Value{String("postex-object-id")}},
	} {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		if !errors.Is(invokeErr, wantErr) || !result.IsNull() {
			t.Errorf("%s provider error = (%s, %v), want null/%v",
				test.name, result.Describe(), invokeErr, wantErr)
		}
	}
	if providerCalls.Load() != 2 || hostCalls.Load() != 0 {
		t.Fatalf("provider/Host calls = %d/%d, want two/zero", providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorDataStoreOverrideAndNilProviderPolicy(t *testing.T) {
	for name, spec := range aggressorDataStoreSpecs {
		name := name
		spec := spec
		for _, overrideFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/override-first=%v", name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				var hostCalls atomic.Int32
				hostOption := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), nil
				}))
				providerOption := WithAggressorDataStoreProvider(AggressorDataStoreProviderFunc(func(context.Context, AggressorDataStoreRequest) (Value, error) {
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

				// Use an invalid stock-wrapper arity so success proves selection
				// happened before the wrapper's validation.
				arguments := []Value(nil)
				if spec.minimum == 0 {
					arguments = []Value{String("invalid stock-wrapper arity")}
				}
				result, err := runtimeInstance.Invoke(context.Background(), name, arguments...)
				if err != nil || result.String() != "override" || providerCalls.Load() != 0 || hostCalls.Load() != 0 {
					t.Fatalf("override = (%s, %v), provider/Host %d/%d; want override/zero/zero",
						result.Describe(), err, providerCalls.Load(), hostCalls.Load())
				}
			})
		}
	}

	var typedNil *recordingAggressorDataStoreProvider
	if _, err := New(WithAggressorDataStoreProvider(typedNil)); err == nil {
		t.Fatal("typed-nil data-store provider was accepted")
	}
	var nilFunction AggressorDataStoreProviderFunc
	if _, err := New(WithAggressorDataStoreProvider(nilFunction)); err == nil {
		t.Fatal("nil data-store provider function was accepted")
	}
	if _, err := nilFunction.HandleAggressorDataStore(context.Background(), AggressorDataStoreRequest{}); err == nil {
		t.Fatal("direct nil data-store provider function call succeeded")
	}
}

func TestAggressorDataStoreCancellation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var cancelDuring context.CancelFunc
	provider := AggressorDataStoreProviderFunc(func(_ context.Context, request AggressorDataStoreRequest) (Value, error) {
		calls.Add(1)
		cancelDuring()
		return request.Arg(0), nil
	})
	runtimeInstance, err := New(WithAggressorDataStoreProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtimeInstance.Invoke(preCanceled, "tokenToEmail", String("pre-canceled")); !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("pre-canceled error/provider calls = %v/%d, want context.Canceled/zero", err, calls.Load())
	}
	during, cancel := context.WithCancel(context.Background())
	cancelDuring = cancel
	result, err := runtimeInstance.Invoke(during, "tokenToEmail", String("cancel-during"))
	if !errors.Is(err, context.Canceled) || !result.IsNull() || calls.Load() != 1 {
		t.Fatalf("cancel-during = (%s, %v), provider calls %d; want null/context.Canceled/one",
			result.Describe(), err, calls.Load())
	}
}

func TestAggressorDataStoreConcurrentRedactionRequestsAreDetachedAndResultsDiscarded(t *testing.T) {
	t.Parallel()

	const calls = 32
	var providerCalls atomic.Int32
	provider := AggressorDataStoreProviderFunc(func(_ context.Context, request AggressorDataStoreRequest) (Value, error) {
		providerCalls.Add(1)
		value := request.Arg(0)
		request.Arguments[0] = String("provider-local mutation")
		return value, nil
	})
	runtimeInstance, err := New(WithAggressorDataStoreProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	errorsChannel := make(chan error, calls)
	var wait sync.WaitGroup
	for index := 0; index < calls; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			want := fmt.Sprintf("postex-object-%d", index)
			result, invokeErr := runtimeInstance.Invoke(context.Background(), "redactobject", String(want))
			if invokeErr != nil || !result.IsNull() {
				errorsChannel <- fmt.Errorf("redactobject %d = (%s, %v), want null/nil", index, result.Describe(), invokeErr)
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
	if providerCalls.Load() != calls {
		t.Fatalf("concurrent redactobject provider calls = %d, want %d", providerCalls.Load(), calls)
	}
}

func TestPortableScriptLoaderInheritsAggressorDataStoreProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-data-store.cna")
	if err := os.WriteFile(childPath, []byte(`
highlight("downloads", @("row"), "blue");
redactobject("child-postex-object");
`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-data-store.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
tokenToEmail("parent");
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorDataStoreProvider{
		handle: func(_ context.Context, request AggressorDataStoreRequest) (Value, error) {
			return request.Arg(0), nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("data-store provider was not inherited")
		})),
		WithAggressorDataStoreProvider(provider),
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
	if len(requests) != 3 || requests[0].Operation != AggressorDataStoreTokenToEmail || requests[0].Arg(0).String() != "parent" ||
		requests[1].Operation != AggressorDataStoreHighlight || len(requests[1].Arguments) != 3 || requests[1].Arg(0).String() != "downloads" || requests[1].Arg(2).String() != "blue" {
		t.Fatalf("parent/child requests = %#v", requests)
	}
	if requests[2].Operation != AggressorDataStoreRedactObject || requests[2].Arg(0).String() != "child-postex-object" {
		t.Fatalf("child redactobject request = %#v", requests[2])
	}
	if requests[0].RuntimeID != runtimeInstance.ID() || requests[1].RuntimeID == requests[0].RuntimeID || requests[0].RuntimeID == 0 || requests[1].RuntimeID == 0 || requests[2].RuntimeID != requests[1].RuntimeID {
		t.Fatalf("parent/child RuntimeIDs = %d/%d/%d", requests[0].RuntimeID, requests[1].RuntimeID, requests[2].RuntimeID)
	}
	if requests[0].Script != 1 || requests[1].Script != 1 || requests[2].Script != 1 || requests[0].Span.Source != "parent-data-store.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) || requests[2].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child provenance = %#v", requests)
	}
}
