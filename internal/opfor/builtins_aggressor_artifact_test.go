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

type recordingAggressorArtifactProvider struct {
	mu       sync.Mutex
	requests []AggressorArtifactRequest
	generate func(context.Context, AggressorArtifactRequest) (Value, error)
}

func (provider *recordingAggressorArtifactProvider) GenerateAggressorArtifact(
	ctx context.Context,
	request AggressorArtifactRequest,
) (Value, error) {
	provider.mu.Lock()
	provider.requests = append(provider.requests, request)
	generate := provider.generate
	provider.mu.Unlock()
	if generate == nil {
		return Null(), nil
	}
	return generate(ctx, request)
}

func (provider *recordingAggressorArtifactProvider) snapshot() []AggressorArtifactRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorArtifactRequest(nil), provider.requests...)
}

func TestAggressorArtifactFunctionSetAndDocumentedSpecs(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorArtifactFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{"artifact_payload", "artifact_stageless"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor artifact names = %q, want %q", names, want)
	}
	wantSpecs := map[string]aggressorArtifactSpec{
		"artifact_payload":   {kind: AggressorArtifactPayload, minimum: 5, maximum: 9},
		"artifact_stageless": {kind: AggressorArtifactStageless, minimum: 5, maximum: 5},
	}
	if !reflect.DeepEqual(aggressorArtifactSpecs, wantSpecs) {
		t.Fatalf("Aggressor artifact specs = %#v, want %#v", aggressorArtifactSpecs, wantSpecs)
	}
	if string(AggressorArtifactPayload) != "artifact_payload" ||
		string(AggressorArtifactStageless) != "artifact_stageless" {
		t.Fatalf("unstable artifact Kinds = %q/%q", AggressorArtifactPayload, AggressorArtifactStageless)
	}
}

func TestAggressorArtifactPayloadArgumentsResultsAndPresence(t *testing.T) {
	t.Parallel()

	var hostCalls atomic.Int32
	provider := &recordingAggressorArtifactProvider{
		generate: func(_ context.Context, request AggressorArtifactRequest) (Value, error) {
			return request.Listener, nil
		},
	}
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed artifact request reached Host")
		})),
		WithAggressorArtifactProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for count := 5; count <= 9; count++ {
		listener := ArrayValue(NewArray(String(fmt.Sprintf("listener-%d", count))))
		arguments := []Value{
			listener,
			String("exe"),
			String("x64"),
			String("process"),
			String("Indirect"),
			Null(),
			String("dns_over_https"),
			BinaryString([]byte("stage { set userwx false; }")),
			HashValue(NewHash()),
		}
		arguments = arguments[:count]
		result, invokeErr := runtimeInstance.Invoke(context.Background(), "artifact_payload", arguments...)
		if invokeErr != nil || !result.IdentityEqual(listener) {
			t.Fatalf("arity %d result = (%s, %v), want identical listener result", count, result.Describe(), invokeErr)
		}

		requests := provider.snapshot()
		request := requests[len(requests)-1]
		if request.Kind != AggressorArtifactPayload || request.Name != "artifact_payload" ||
			request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 ||
			request.Script != 0 || request.Span != (Span{}) {
			t.Errorf("arity %d request route/provenance = %#v", count, request)
		}
		for index, got := range []Value{
			request.Listener,
			request.ArtifactType,
			request.Architecture,
			request.ExitMethod,
			request.SystemCallMethod,
		} {
			if !got.IdentityEqual(arguments[index]) {
				t.Errorf("arity %d required field %d = %s, want identical %s",
					count, index, got.Describe(), arguments[index].Describe())
			}
		}
		assertAggressorArtifactOptionalField(t, count, 6, request.HasHTTPLibrary, request.HTTPLibrary, arguments)
		assertAggressorArtifactOptionalField(t, count, 7, request.HasDNSCommMode, request.DNSCommMode, arguments)
		assertAggressorArtifactOptionalField(t, count, 8, request.HasMalleableProfileOverride, request.MalleableProfileOverride, arguments)
		assertAggressorArtifactOptionalField(t, count, 9, request.HasPayloadStoreInfo, request.PayloadStoreInfo, arguments)
		if !request.ProxyConfiguration.IsNull() || request.Callback != nil {
			t.Errorf("payload request stageless fields = %s/%T, want null/nil",
				request.ProxyConfiguration.Describe(), request.Callback)
		}
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("configured provider route reached Host %d time(s)", hostCalls.Load())
	}
}

func TestAggressorArtifactResolvesReferencesOnceWithProvenance(t *testing.T) {
	t.Parallel()

	listener := ArrayValue(NewArray(String("listener-original")))
	store := HashValue(NewHash())
	listenerCell := NewCell(listener)
	storeCell := NewCell(store)
	span := Span{Source: "artifact-values.cna", Start: Position{Line: 13, Column: 7}}
	var captured AggressorArtifactRequest
	provider := AggressorArtifactProviderFunc(func(_ context.Context, request AggressorArtifactRequest) (Value, error) {
		captured = request
		listenerCell.Set(String("listener-mutated"))
		storeCell.Set(String("store-mutated"))
		return request.PayloadStoreInfo, nil
	})
	runtimeInstance, err := New(WithAggressorArtifactProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  47,
		Name:    "artifact_payload",
		Span:    span,
		Arguments: []Argument{
			{Name: "$listener", Reference: listenerCell},
			{Value: String("exe")},
			{Value: String("x64")},
			{Value: String("process")},
			{Value: String("Indirect")},
			{Value: Null()},
			{Value: String("dns")},
			{Value: String("profile")},
			{Name: "$store", Reference: storeCell},
		},
	}
	result, err := runtimeInstance.aggressorArtifact(context.Background(), invocation)
	if err != nil || !result.IdentityEqual(store) {
		t.Fatalf("resolved request = (%s, %v), want original store identity", result.Describe(), err)
	}
	if captured.RuntimeID != runtimeInstance.ID() || captured.Script != 47 || captured.Span != span {
		t.Fatalf("request provenance = runtime %d script %d span %s", captured.RuntimeID, captured.Script, captured.Span)
	}
	if !captured.Listener.IdentityEqual(listener) || !captured.PayloadStoreInfo.IdentityEqual(store) {
		t.Fatalf("captured identities = %s/%s, want original listener/store",
			captured.Listener.Describe(), captured.PayloadStoreInfo.Describe())
	}
}

func TestAggressorArtifactStagelessRetainedCallbackAndLifecycle(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorArtifactProvider{
		generate: func(context.Context, AggressorArtifactRequest) (Value, error) {
			return String("provider return must be ignored"), nil
		},
	}
	runtimeInstance, err := New(WithAggressorArtifactProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("artifact-stageless-callback.cna", `
$calls = 0;
$ready = {
    $calls++;
    return $1;
};
sub issue_artifact {
    return artifact_stageless("local-listener", "raw", "x86", $null, $ready);
}
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Call(context.Background(), "issue_artifact")
	if err != nil || !result.IsNull() {
		t.Fatalf("artifact_stageless = (%s, %v), want null/nil despite provider return", result.Describe(), err)
	}
	requests := provider.snapshot()
	if len(requests) != 1 {
		t.Fatalf("provider requests = %d, want one", len(requests))
	}
	request := requests[0]
	if request.Kind != AggressorArtifactStageless || request.Name != "artifact_stageless" ||
		request.RuntimeID != runtimeInstance.ID() || request.Script != owner.ID() ||
		request.Span.Source != "artifact-stageless-callback.cna" || request.Span.Start.Line == 0 {
		t.Fatalf("stageless route/provenance = %#v", request)
	}
	if request.Listener.String() != "local-listener" || request.ArtifactType.String() != "raw" ||
		request.Architecture.String() != "x86" || !request.ProxyConfiguration.IsNull() || request.Callback == nil {
		t.Fatalf("stageless fields = %#v", request)
	}
	if !request.ExitMethod.IsNull() || !request.SystemCallMethod.IsNull() ||
		request.HasHTTPLibrary || request.HasDNSCommMode ||
		request.HasMalleableProfileOverride || request.HasPayloadStoreInfo {
		t.Fatalf("stageless request populated payload-only fields: %#v", request)
	}

	for index := 1; index <= 2; index++ {
		artifact := HashValue(NewHash())
		result, invokeErr := request.Callback.Invoke(context.Background(), artifact)
		if invokeErr != nil || !result.IdentityEqual(artifact) {
			t.Fatalf("callback %d = (%s, %v), want identical artifact", index, result.Describe(), invokeErr)
		}
	}
	if calls := owner.Get("$calls").Int32(); calls != 2 {
		t.Fatalf("callback calls = %d, want 2", calls)
	}
	if err := owner.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err = request.Callback.Invoke(context.Background(), String("after unload"))
	if !errors.Is(err, ErrScriptUnloaded) || !result.IsNull() {
		t.Fatalf("callback after unload = (%s, %v), want null/ErrScriptUnloaded", result.Describe(), err)
	}
}

func TestAggressorArtifactStagelessRejectsNonCallable(t *testing.T) {
	t.Parallel()

	for _, callback := range []string{"$null", `"not callable"`} {
		callback := callback
		t.Run(callback, func(t *testing.T) {
			var providerCalls atomic.Int32
			runtimeInstance, err := New(WithAggressorArtifactProvider(AggressorArtifactProviderFunc(func(
				context.Context,
				AggressorArtifactRequest,
			) (Value, error) {
				providerCalls.Add(1)
				return Null(), nil
			})))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			program, err := CompileString("artifact-invalid-callback.cna",
				fmt.Sprintf(`artifact_stageless("listener", "raw", "x64", $null, %s);`, callback))
			if err != nil {
				t.Fatal(err)
			}
			result, err := runtimeInstance.Execute(context.Background(), program)
			if !errors.Is(err, ErrInvalidCallable) || !result.IsNull() {
				t.Fatalf("invalid callback = (%s, %v), want null/ErrInvalidCallable", result.Describe(), err)
			}
			if providerCalls.Load() != 0 {
				t.Fatalf("invalid callback reached provider %d time(s)", providerCalls.Load())
			}
		})
	}
}

func TestAggressorArtifactArityStopsProviderAndHost(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorArtifactProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorArtifactProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for name, counts := range map[string][]int{
		"artifact_payload":   {4, 10},
		"artifact_stageless": {4, 6},
	} {
		for _, count := range counts {
			arguments := make([]Value, count)
			for index := range arguments {
				arguments[index] = String(fmt.Sprintf("argument-%d", index))
			}
			result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
			if invokeErr == nil || !result.IsNull() {
				t.Errorf("%s/%d = (%s, %v), want null arity error", name, count, result.Describe(), invokeErr)
			}
			want := "expected exactly 5 argument(s)"
			if name == "artifact_payload" {
				want = "expected 5 to 9 argument(s)"
			}
			if invokeErr != nil && !strings.Contains(invokeErr.Error(), want) {
				t.Errorf("%s/%d error = %v, want %q", name, count, invokeErr, want)
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

func TestAggressorArtifactUnsetProviderPreservesExactHostInvocation(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("Host artifact result")
	wantResult := BinaryString([]byte{0x00, 0xff, 0x41})
	listenerCell := NewCell(String("listener-before"))
	callbackCell := NewCell(String("Host-owned non-callable"))
	span := Span{Source: "artifact-host.cna", Start: Position{Line: 19, Column: 4}}
	original := Invocation{
		Script: 81,
		Name:   "artifact_stageless",
		Span:   span,
		Arguments: []Argument{
			{Name: "$listener", Reference: listenerCell},
			{Value: String("raw")},
			{Value: String("x86")},
			{Value: Null()},
			{Name: "&ready", Reference: callbackCell},
		},
	}
	var captured Invocation
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		captured = invocation
		invocation.Arguments[0].Set(String("listener-mutated-by-Host"))
		invocation.Arguments[4].Set(String("callback-mutated-by-Host"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original.Runtime = runtimeInstance

	result, err := runtimeInstance.aggressorArtifact(context.Background(), original)
	if !errors.Is(err, wantErr) || !result.IdentityEqual(wantResult) {
		t.Fatalf("Host fallback = (%s, %v), want identical result/%v", result.Describe(), err, wantErr)
	}
	if hostCalls.Load() != 1 || captured.Runtime != original.Runtime || captured.Script != original.Script ||
		captured.Name != original.Name || captured.Span != original.Span || len(captured.Arguments) != 5 {
		t.Fatalf("captured Host invocation/calls = %#v/%d", captured, hostCalls.Load())
	}
	if captured.Arguments[0].Reference != listenerCell || captured.Arguments[0].Name != "$listener" ||
		captured.Arguments[4].Reference != callbackCell || captured.Arguments[4].Name != "&ready" {
		t.Fatalf("Host did not receive original reference-bearing arguments: %#v", captured.Arguments)
	}
	if listenerCell.Get().String() != "listener-mutated-by-Host" ||
		callbackCell.Get().String() != "callback-mutated-by-Host" {
		t.Fatal("Host reference mutations were not preserved")
	}
}

func TestAggressorArtifactUnsetProviderRoutesBothNamesToHostOnce(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls.Add(1)
		return String("host:" + invocation.Name), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	for name := range aggressorArtifactSpecs {
		arguments := []Value{String("listener"), String("exe"), String("x64"), String("process"), String("raw callback")}
		result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
		if invokeErr != nil || result.String() != "host:"+name {
			t.Errorf("%s Host fallback = (%s, %v)", name, result.Describe(), invokeErr)
		}
	}
	if calls.Load() != int32(len(aggressorArtifactSpecs)) {
		t.Fatalf("Host calls = %d, want %d", calls.Load(), len(aggressorArtifactSpecs))
	}
}

func TestAggressorArtifactProviderErrorsAndCancellationAreAuthoritative(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("artifact generation rejected")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	var cancelDuring context.CancelFunc
	provider := AggressorArtifactProviderFunc(func(ctx context.Context, request AggressorArtifactRequest) (Value, error) {
		providerCalls.Add(1)
		switch request.Listener.String() {
		case "error":
			return String("discarded partial artifact"), wantErr
		case "cancel-during":
			cancelDuring()
			if !errors.Is(ctx.Err(), context.Canceled) {
				return Null(), fmt.Errorf("provider context error = %v", ctx.Err())
			}
			return String("late artifact"), nil
		default:
			return String("artifact"), nil
		}
	})
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host artifact"), nil
		})),
		WithAggressorArtifactProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	arguments := func(listener string) []Value {
		return []Value{String(listener), String("exe"), String("x64"), String("process"), String("Indirect")}
	}

	result, err := runtimeInstance.Invoke(context.Background(), "artifact_payload", arguments("error")...)
	if !errors.Is(err, wantErr) || !result.IsNull() {
		t.Fatalf("provider error = (%s, %v), want null/%v", result.Describe(), err, wantErr)
	}

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = runtimeInstance.Invoke(preCanceled, "artifact_payload", arguments("pre-canceled")...)
	if !errors.Is(err, context.Canceled) || !result.IsNull() || providerCalls.Load() != 1 {
		t.Fatalf("pre-canceled = (%s, %v), provider calls %d", result.Describe(), err, providerCalls.Load())
	}

	during, cancel := context.WithCancel(context.Background())
	cancelDuring = cancel
	result, err = runtimeInstance.Invoke(during, "artifact_payload", arguments("cancel-during")...)
	if !errors.Is(err, context.Canceled) || !result.IsNull() {
		t.Fatalf("cancel-during = (%s, %v), want null/context.Canceled", result.Describe(), err)
	}
	if providerCalls.Load() != 2 || hostCalls.Load() != 0 {
		t.Fatalf("provider/Host calls = %d/%d, want 2/0", providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorArtifactRuntimeCloseCancelsBlockingProvider(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	var once sync.Once
	runtimeInstance, err := New(WithAggressorArtifactProvider(AggressorArtifactProviderFunc(func(
		ctx context.Context,
		_ AggressorArtifactRequest,
	) (Value, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return Null(), ctx.Err()
	})))
	if err != nil {
		t.Fatal(err)
	}

	invokeDone := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.Invoke(
			context.Background(), "artifact_payload",
			String("listener"), String("exe"), String("x64"), String("process"), String("Indirect"),
		)
		invokeDone <- invokeErr
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("artifact provider did not start")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtimeInstance.Close(context.Background()) }()
	select {
	case invokeErr := <-invokeDone:
		if !errors.Is(invokeErr, context.Canceled) {
			t.Errorf("blocking provider error = %v, want context.Canceled", invokeErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocking artifact provider did not stop on Runtime.Close")
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

func TestAggressorArtifactWithFunctionOverridesBothNamesInBothOptionOrders(t *testing.T) {
	for name := range aggressorArtifactSpecs {
		name := name
		for _, overrideFirst := range []bool{false, true} {
			overrideFirst := overrideFirst
			t.Run(fmt.Sprintf("%s/override-first=%v", name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				var hostCalls atomic.Int32
				providerOption := WithAggressorArtifactProvider(AggressorArtifactProviderFunc(func(
					context.Context,
					AggressorArtifactRequest,
				) (Value, error) {
					providerCalls.Add(1)
					return Null(), nil
				}))
				overrideOption := WithFunction(name, func(_ context.Context, invocation Invocation) (Value, error) {
					return String("override:" + invocation.Name), nil
				})
				hostOption := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), nil
				}))
				options := []Option{hostOption, providerOption, overrideOption}
				if overrideFirst {
					options = []Option{hostOption, overrideOption, providerOption}
				}
				runtimeInstance, err := New(options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
				// Zero arguments is invalid for both stock wrappers. Success proves the
				// importer override won before native arity validation.
				result, err := runtimeInstance.Invoke(context.Background(), name)
				if err != nil || result.String() != "override:"+name {
					t.Fatalf("override = (%s, %v)", result.Describe(), err)
				}
				if providerCalls.Load() != 0 || hostCalls.Load() != 0 {
					t.Fatalf("override provider/Host calls = %d/%d", providerCalls.Load(), hostCalls.Load())
				}
			})
		}
	}
}

func TestPortableScriptLoaderInheritsAggressorArtifactProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-artifact.cna")
	if err := os.WriteFile(childPath, []byte(`artifact_payload("child", "exe", "x64", "process", "Indirect");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-artifact.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
artifact_payload("parent", "exe", "x64", "process", "Indirect");
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorArtifactProvider{
		generate: func(context.Context, AggressorArtifactRequest) (Value, error) {
			return BinaryString([]byte("artifact")), nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader artifact route reached Host")
		})),
		WithAggressorArtifactProvider(provider),
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
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want parent plus child", len(requests))
	}
	if requests[0].Listener.String() != "parent" || requests[1].Listener.String() != "child" ||
		requests[0].Kind != AggressorArtifactPayload || requests[1].Kind != AggressorArtifactPayload {
		t.Fatalf("parent/child artifact requests = %#v", requests)
	}
	if requests[0].RuntimeID != runtimeInstance.ID() || requests[0].RuntimeID == 0 ||
		requests[1].RuntimeID == 0 || requests[1].RuntimeID == requests[0].RuntimeID {
		t.Fatalf("parent/child RuntimeIDs = %d/%d", requests[0].RuntimeID, requests[1].RuntimeID)
	}
	if requests[0].Script != 1 || requests[1].Script != 1 ||
		requests[0].Span.Source != "parent-artifact.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child provenance = %#v", requests)
	}
}

func assertAggressorArtifactOptionalField(
	t *testing.T,
	count int,
	position int,
	has bool,
	got Value,
	arguments []Value,
) {
	t.Helper()
	wantHas := count >= position
	if has != wantHas {
		t.Errorf("arity %d argument %d presence = %v, want %v", count, position, has, wantHas)
		return
	}
	if !wantHas {
		if !got.IsNull() {
			t.Errorf("arity %d omitted argument %d = %s, want null", count, position, got.Describe())
		}
		return
	}
	if !got.IdentityEqual(arguments[position-1]) {
		t.Errorf("arity %d argument %d = %s, want identical %s",
			count, position, got.Describe(), arguments[position-1].Describe())
	}
}
