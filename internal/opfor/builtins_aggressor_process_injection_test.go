package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingAggressorProcessInjectionProvider struct {
	mu       sync.Mutex
	requests []AggressorProcessInjectionRequest
	handle   func(context.Context, AggressorProcessInjectionRequest) (Value, error)
}

func (provider *recordingAggressorProcessInjectionProvider) HandleAggressorProcessInjection(
	ctx context.Context,
	request AggressorProcessInjectionRequest,
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

func (provider *recordingAggressorProcessInjectionProvider) snapshot() []AggressorProcessInjectionRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorProcessInjectionRequest(nil), provider.requests...)
}

var aggressorProcessInjectionTestSpecs = []struct {
	name         string
	operation    AggressorProcessInjectionOperation
	arguments    int
	returnsValue bool
}{
	{name: "pi_explicit_get", operation: AggressorProcessInjectionExplicitGet, returnsValue: true},
	{name: "pi_explicit_info", operation: AggressorProcessInjectionExplicitInfo, returnsValue: true},
	{name: "pi_explicit_set", operation: AggressorProcessInjectionExplicitSet, arguments: 1},
	{name: "pi_spawn_get", operation: AggressorProcessInjectionSpawnGet, returnsValue: true},
	{name: "pi_spawn_info", operation: AggressorProcessInjectionSpawnInfo, returnsValue: true},
	{name: "pi_spawn_set", operation: AggressorProcessInjectionSpawnSet, arguments: 1},
	{name: "pi_user_explicit_clear", operation: AggressorProcessInjectionUserExplicitClear},
	{name: "pi_user_explicit_get", operation: AggressorProcessInjectionUserExplicitGet, returnsValue: true},
	{name: "pi_user_explicit_get_map", operation: AggressorProcessInjectionUserExplicitGetMap, returnsValue: true},
	{name: "pi_user_explicit_get_names", operation: AggressorProcessInjectionUserExplicitGetNames, returnsValue: true},
	{name: "pi_user_explicit_set", operation: AggressorProcessInjectionUserExplicitSet, arguments: 1},
	{name: "pi_user_spawn_clear", operation: AggressorProcessInjectionUserSpawnClear},
	{name: "pi_user_spawn_get", operation: AggressorProcessInjectionUserSpawnGet, returnsValue: true},
	{name: "pi_user_spawn_get_map", operation: AggressorProcessInjectionUserSpawnGetMap, returnsValue: true},
	{name: "pi_user_spawn_get_names", operation: AggressorProcessInjectionUserSpawnGetNames, returnsValue: true},
	{name: "pi_user_spawn_set", operation: AggressorProcessInjectionUserSpawnSet, arguments: 1},
}

func TestAggressorProcessInjectionFunctionSetAndDocumentedSpecs(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorProcessInjectionFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	slices.Sort(names)
	wantNames := make([]string, 0, len(aggressorProcessInjectionTestSpecs))
	for _, want := range aggressorProcessInjectionTestSpecs {
		wantNames = append(wantNames, want.name)
		spec, exists := aggressorProcessInjectionSpecs[want.name]
		if !exists {
			t.Errorf("missing spec for %s", want.name)
			continue
		}
		if spec.operation != want.operation || spec.arguments != want.arguments || spec.returnsValue != want.returnsValue {
			t.Errorf("%s spec = %#v, want %q/%d/returns=%t", want.name, spec, want.operation, want.arguments, want.returnsValue)
		}
		if string(spec.operation) != want.name {
			t.Errorf("%s operation spelling = %q", want.name, spec.operation)
		}
		if !slices.Contains(DefaultFunctionNames(), want.name) {
			t.Errorf("DefaultFunctionNames does not contain %q", want.name)
		}
	}
	slices.Sort(wantNames)
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("Aggressor process-injection names = %q, want %q", names, wantNames)
	}
}

func TestAggressorProcessInjectionProviderArgumentsResultsAndProvenance(t *testing.T) {
	t.Parallel()

	providerResult := ObjectValue(&struct{ providerOwned bool }{true})
	provider := &recordingAggressorProcessInjectionProvider{
		handle: func(context.Context, AggressorProcessInjectionRequest) (Value, error) {
			return providerResult, nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed process-injection request reached Host")
		})),
		WithAggressorProcessInjectionProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	wantSelections := make([]Value, len(aggressorProcessInjectionTestSpecs))
	for index, test := range aggressorProcessInjectionTestSpecs {
		arguments := []Value(nil)
		if test.arguments != 0 {
			selection := ArrayValue(NewArray(String(test.name)))
			wantSelections[index] = selection
			arguments = []Value{selection}
		}
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, arguments...)
		if invokeErr != nil {
			t.Errorf("%s: %v", test.name, invokeErr)
			continue
		}
		if test.returnsValue {
			if !result.IdentityEqual(providerResult) {
				t.Errorf("%s result = %s, want identical provider result", test.name, result.Describe())
			}
		} else if !result.IsNull() {
			t.Errorf("%s effect result = %s, want $null", test.name, result.Describe())
		}
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("configured provider reached Host %d time(s)", hostCalls.Load())
	}

	requests := provider.snapshot()
	if len(requests) != len(aggressorProcessInjectionTestSpecs) {
		t.Fatalf("process-injection requests = %d, want %d", len(requests), len(aggressorProcessInjectionTestSpecs))
	}
	for index, request := range requests {
		want := aggressorProcessInjectionTestSpecs[index]
		if request.Operation != want.operation || request.Name != want.name ||
			request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 ||
			request.Script != 0 || request.Span != (Span{}) {
			t.Errorf("request %d route/provenance = %#v", index, request)
		}
		if want.arguments == 0 {
			if !request.SelectionName.IsNull() {
				t.Errorf("%s selection = %s, want $null", want.name, request.SelectionName.Describe())
			}
		} else if !request.SelectionName.IdentityEqual(wantSelections[index]) {
			t.Errorf("%s selection lost Value identity", want.name)
		}
	}
}

func TestAggressorProcessInjectionResolvesSetterReferenceOnce(t *testing.T) {
	t.Parallel()

	selection := HashValue(NewOrderedHash())
	selectionCell := NewCell(selection)
	span := Span{Source: "process-injection-values.cna", Start: Position{Line: 9, Column: 3}}
	var captured AggressorProcessInjectionRequest
	runtimeInstance, err := New(WithAggressorProcessInjectionProvider(AggressorProcessInjectionProviderFunc(func(
		_ context.Context,
		request AggressorProcessInjectionRequest,
	) (Value, error) {
		captured = request
		selectionCell.Set(String("mutated after resolution"))
		return String("discarded setter result"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, invokeErr := runtimeInstance.aggressorProcessInjection(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  37,
		Name:    "pi_user_spawn_set",
		Span:    span,
		Arguments: []Argument{{
			Name:      "$selection",
			Reference: selectionCell,
		}},
	})
	if invokeErr != nil || !result.IsNull() {
		t.Fatalf("pi_user_spawn_set = (%s, %v), want null/success", result.Describe(), invokeErr)
	}
	if captured.Operation != AggressorProcessInjectionUserSpawnSet || captured.Name != "pi_user_spawn_set" ||
		captured.RuntimeID != runtimeInstance.ID() || captured.Script != 37 || captured.Span != span ||
		!captured.SelectionName.IdentityEqual(selection) {
		t.Fatalf("captured process-injection request = %#v", captured)
	}
}

func TestAggressorProcessInjectionHostFallbackPreservesInvocation(t *testing.T) {
	t.Parallel()

	selectionCell := NewCell(String("original"))
	wantResult := ObjectValue(&struct{ hostOwned bool }{true})
	wantErr := errors.New("Host process-injection result")
	var captured Invocation
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		captured = invocation
		invocation.Arguments[0].Set(String("mutated by Host"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  43,
		Name:    "pi_explicit_set",
		Span:    Span{Source: "process-injection-host.cna", Start: Position{Line: 4, Column: 2}},
		Arguments: []Argument{{
			Name:      "$selection",
			Reference: selectionCell,
		}},
	}
	result, invokeErr := runtimeInstance.aggressorProcessInjection(context.Background(), invocation)
	if !result.IdentityEqual(wantResult) || !errors.Is(invokeErr, wantErr) || hostCalls.Load() != 1 {
		t.Fatalf("Host fallback = (%s, %v), calls %d", result.Describe(), invokeErr, hostCalls.Load())
	}
	if captured.Runtime != invocation.Runtime || captured.Script != invocation.Script || captured.Name != invocation.Name ||
		captured.Span != invocation.Span || len(captured.Arguments) != 1 ||
		captured.Arguments[0].Reference != selectionCell || selectionCell.Get().String() != "mutated by Host" {
		t.Fatalf("Host fallback changed Invocation: %#v / %s", captured, selectionCell.Get().Describe())
	}
}

func TestAggressorProcessInjectionInvalidAritiesStopProviderAndHost(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorProcessInjectionProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorProcessInjectionProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	for _, test := range aggressorProcessInjectionTestSpecs {
		counts := []int{test.arguments + 1}
		if test.arguments != 0 {
			counts = append(counts, test.arguments-1)
		}
		for _, count := range counts {
			result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, make([]Value, count)...)
			if invokeErr == nil || !result.IsNull() || !strings.Contains(invokeErr.Error(), "expected exactly") {
				t.Errorf("%s/%d = (%s, %v), want null arity error", test.name, count, result.Describe(), invokeErr)
			}
		}
	}
	if got := len(provider.snapshot()); got != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid arities reached provider/Host = %d/%d", got, hostCalls.Load())
	}
}

func TestAggressorProcessInjectionProviderErrorCancellationAndOverrideAreAuthoritative(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("process-injection provider rejected request")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	providerOption := WithAggressorProcessInjectionProvider(AggressorProcessInjectionProviderFunc(func(
		context.Context,
		AggressorProcessInjectionRequest,
	) (Value, error) {
		providerCalls.Add(1)
		return String("partial"), wantErr
	}))
	for _, overrideFirst := range []bool{false, true} {
		overrideOption := WithFunction("pi_explicit_get", func(context.Context, Invocation) (Value, error) {
			return String("override"), nil
		})
		options := []Option{
			WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
				hostCalls.Add(1)
				return String("Host"), nil
			})),
			providerOption,
			overrideOption,
		}
		if overrideFirst {
			options[1], options[2] = options[2], options[1]
		}
		runtimeInstance, err := New(options...)
		if err != nil {
			t.Fatal(err)
		}
		result, invokeErr := runtimeInstance.Invoke(context.Background(), "pi_explicit_get")
		if closeErr := runtimeInstance.Close(context.Background()); closeErr != nil {
			t.Fatal(closeErr)
		}
		if invokeErr != nil || result.String() != "override" {
			t.Fatalf("override-first=%t = (%s, %v)", overrideFirst, result.Describe(), invokeErr)
		}
	}
	if providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("WithFunction override reached provider/Host %d/%d", providerCalls.Load(), hostCalls.Load())
	}

	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host"), nil
		})),
		providerOption,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, invokeErr := runtimeInstance.Invoke(context.Background(), "pi_spawn_get")
	if !result.IsNull() || !errors.Is(invokeErr, wantErr) || providerCalls.Load() != 1 || hostCalls.Load() != 0 {
		t.Fatalf("provider error = (%s, %v), provider/Host %d/%d", result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	result, invokeErr = runtimeInstance.Invoke(canceled, "pi_spawn_get")
	if !result.IsNull() || !errors.Is(invokeErr, context.Canceled) || providerCalls.Load() != 1 {
		t.Fatalf("pre-canceled request = (%s, %v), provider calls %d", result.Describe(), invokeErr, providerCalls.Load())
	}

	postCanceled, cancelPost := context.WithCancel(context.Background())
	postRuntime, err := New(WithAggressorProcessInjectionProvider(AggressorProcessInjectionProviderFunc(func(
		context.Context,
		AggressorProcessInjectionRequest,
	) (Value, error) {
		cancelPost()
		return String("completed after cancellation"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = postRuntime.Close(context.Background()) })
	result, invokeErr = postRuntime.Invoke(postCanceled, "pi_spawn_get")
	if !result.IsNull() || !errors.Is(invokeErr, context.Canceled) {
		t.Fatalf("provider-canceled request = (%s, %v)", result.Describe(), invokeErr)
	}
}

func TestAggressorProcessInjectionRejectsTypedNilAndNilAdapter(t *testing.T) {
	t.Parallel()

	var typedNil *recordingAggressorProcessInjectionProvider
	if _, err := New(WithAggressorProcessInjectionProvider(typedNil)); err == nil ||
		!strings.Contains(err.Error(), "Aggressor process-injection provider is nil") {
		t.Fatalf("typed-nil provider error = %v", err)
	}
	var nilFunction AggressorProcessInjectionProviderFunc
	if _, err := New(WithAggressorProcessInjectionProvider(nilFunction)); err == nil ||
		!strings.Contains(err.Error(), "Aggressor process-injection provider is nil") {
		t.Fatalf("nil provider function option error = %v", err)
	}
	if result, err := nilFunction.HandleAggressorProcessInjection(context.Background(), AggressorProcessInjectionRequest{}); err == nil || !result.IsNull() {
		t.Fatalf("nil provider adapter = (%s, %v), want null/error", result.Describe(), err)
	}
}

func TestAggressorProcessInjectionProviderCallsMayOverlap(t *testing.T) {
	t.Parallel()

	const calls = 64
	entered := make(chan struct{}, calls)
	release := make(chan struct{})
	provider := &recordingAggressorProcessInjectionProvider{
		handle: func(context.Context, AggressorProcessInjectionRequest) (Value, error) {
			entered <- struct{}{}
			<-release
			return Null(), nil
		},
	}
	runtimeInstance, err := New(WithAggressorProcessInjectionProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	// Register this cleanup after Runtime.Close so LIFO cleanup releases blocked
	// provider calls before Close waits for in-flight execution on a failure.
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	var wait sync.WaitGroup
	errorsByCall := make([]error, calls)
	for index := 0; index < calls; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, invokeErr := runtimeInstance.Invoke(context.Background(), "pi_user_explicit_set", String(fmt.Sprintf("selection-%d", index)))
			if invokeErr != nil {
				errorsByCall[index] = invokeErr
				return
			}
			if !result.IsNull() {
				errorsByCall[index] = fmt.Errorf("result = %s, want $null", result.Describe())
			}
		}()
	}
	for index := 0; index < calls; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d/%d process-injection provider calls entered concurrently", index, calls)
		}
	}
	close(release)
	wait.Wait()
	for index, invokeErr := range errorsByCall {
		if invokeErr != nil {
			t.Errorf("concurrent request %d: %v", index, invokeErr)
		}
	}
	requests := provider.snapshot()
	if len(requests) != calls {
		t.Fatalf("concurrent requests = %d, want %d", len(requests), calls)
	}
	seen := make(map[string]bool, calls)
	for _, request := range requests {
		if request.Operation != AggressorProcessInjectionUserExplicitSet || request.Name != "pi_user_explicit_set" ||
			request.RuntimeID != runtimeInstance.ID() {
			t.Fatalf("concurrent request metadata = %#v", request)
		}
		seen[request.SelectionName.String()] = true
	}
	if len(seen) != calls {
		t.Fatalf("concurrent selection names = %d unique, want %d", len(seen), calls)
	}
}

func TestPortableScriptLoaderInheritsAggressorProcessInjectionProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-process-injection.cna")
	if err := os.WriteFile(childPath, []byte(`pi_user_spawn_set("child");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-process-injection.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
pi_explicit_get();
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorProcessInjectionProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader process-injection route reached Host")
		})),
		WithAggressorProcessInjectionProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	requests := provider.snapshot()
	if hostCalls.Load() != 0 || len(requests) != 2 {
		t.Fatalf("provider/Host requests = %d/%d, want 2/0", len(requests), hostCalls.Load())
	}
	if requests[0].RuntimeID != runtimeInstance.ID() || requests[1].RuntimeID == 0 ||
		requests[1].RuntimeID == requests[0].RuntimeID || requests[0].Script != 1 || requests[1].Script != 1 ||
		requests[0].Span.Source != "parent-process-injection.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) ||
		requests[0].Operation != AggressorProcessInjectionExplicitGet ||
		requests[1].Operation != AggressorProcessInjectionUserSpawnSet || requests[1].SelectionName.String() != "child" {
		t.Fatalf("parent/child process-injection provenance = %#v", requests)
	}
}
