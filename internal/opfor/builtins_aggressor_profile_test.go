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
)

type recordingAggressorProfileProvider struct {
	mu       sync.Mutex
	requests []AggressorProfileRequest
	handle   func(context.Context, AggressorProfileRequest) (Value, error)
}

func (provider *recordingAggressorProfileProvider) HandleAggressorProfile(
	ctx context.Context,
	request AggressorProfileRequest,
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

func (provider *recordingAggressorProfileProvider) snapshot() []AggressorProfileRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorProfileRequest(nil), provider.requests...)
}

func TestAggressorProfileFunctionSetAndDocumentedSpecs(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorProfileFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	slices.Sort(names)
	wantNames := []string{"killdate", "setup_strings", "setup_transformations"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("Aggressor profile names = %q, want %q", names, wantNames)
	}
	wantSpecs := map[string]aggressorProfileSpec{
		"killdate":              {operation: AggressorProfileKillDate, arguments: 0},
		"setup_strings":         {operation: AggressorProfileSetupStrings, arguments: 1},
		"setup_transformations": {operation: AggressorProfileSetupTransformations, arguments: 2},
	}
	if !reflect.DeepEqual(aggressorProfileSpecs, wantSpecs) {
		t.Fatalf("Aggressor profile specs = %#v, want %#v", aggressorProfileSpecs, wantSpecs)
	}
	for _, name := range wantNames {
		if !slices.Contains(DefaultFunctionNames(), name) {
			t.Errorf("DefaultFunctionNames does not contain %q", name)
		}
	}
}

func TestAggressorProfileProviderArgumentsResultsAndProvenance(t *testing.T) {
	t.Parallel()

	payload := BinaryString([]byte{0x00, 0x41, 0xff})
	architecture := ArrayValue(NewArray(String("x64")))
	provider := &recordingAggressorProfileProvider{
		handle: func(_ context.Context, request AggressorProfileRequest) (Value, error) {
			switch request.Operation {
			case AggressorProfileKillDate:
				return String("2030-12-31"), nil
			case AggressorProfileSetupStrings:
				return request.Payload, nil
			case AggressorProfileSetupTransformations:
				return request.Architecture, nil
			default:
				return Null(), fmt.Errorf("unexpected operation %q", request.Operation)
			}
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed profile request reached Host")
		})),
		WithAggressorProfileProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	tests := []struct {
		name      string
		arguments []Value
		want      Value
		operation AggressorProfileOperation
	}{
		{name: "killdate", want: String("2030-12-31"), operation: AggressorProfileKillDate},
		{name: "setup_strings", arguments: []Value{payload}, want: payload, operation: AggressorProfileSetupStrings},
		{name: "setup_transformations", arguments: []Value{payload, architecture}, want: architecture, operation: AggressorProfileSetupTransformations},
	}
	for _, test := range tests {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		if invokeErr != nil || !result.IdentityEqual(test.want) {
			t.Errorf("%s = (%s, %v), want identical %s", test.name, result.Describe(), invokeErr, test.want.Describe())
		}
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("configured profile provider reached Host %d time(s)", hostCalls.Load())
	}

	requests := provider.snapshot()
	if len(requests) != len(tests) {
		t.Fatalf("provider requests = %d, want %d", len(requests), len(tests))
	}
	for index, request := range requests {
		if request.Operation != tests[index].operation || request.Name != tests[index].name ||
			request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 ||
			request.Script != 0 || request.Span != (Span{}) {
			t.Errorf("request %d route/provenance = %#v", index, request)
		}
	}
	if !requests[0].Payload.IsNull() || !requests[0].Architecture.IsNull() {
		t.Errorf("killdate payload fields = %s/%s, want null/null", requests[0].Payload.Describe(), requests[0].Architecture.Describe())
	}
	if !requests[1].Payload.IdentityEqual(payload) || !requests[1].Architecture.IsNull() {
		t.Errorf("setup_strings fields = %s/%s", requests[1].Payload.Describe(), requests[1].Architecture.Describe())
	}
	if !requests[2].Payload.IdentityEqual(payload) || !requests[2].Architecture.IdentityEqual(architecture) {
		t.Errorf("setup_transformations fields = %s/%s", requests[2].Payload.Describe(), requests[2].Architecture.Describe())
	}
}

func TestAggressorProfileResolvesReferencesOnce(t *testing.T) {
	t.Parallel()

	payload := ArrayValue(NewArray(String("payload")))
	architecture := HashValue(NewHash())
	payloadCell := NewCell(payload)
	architectureCell := NewCell(architecture)
	span := Span{Source: "profile-values.cna", Start: Position{Line: 7, Column: 4}}
	var captured AggressorProfileRequest
	provider := AggressorProfileProviderFunc(func(_ context.Context, request AggressorProfileRequest) (Value, error) {
		captured = request
		payloadCell.Set(String("mutated payload"))
		architectureCell.Set(String("mutated architecture"))
		return request.Payload, nil
	})
	runtimeInstance, err := New(WithAggressorProfileProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.aggressorProfile(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  29,
		Name:    "setup_transformations",
		Span:    span,
		Arguments: []Argument{
			{Name: "$payload", Reference: payloadCell},
			{Name: "$arch", Reference: architectureCell},
		},
	})
	if err != nil || !result.IdentityEqual(payload) {
		t.Fatalf("profile request = (%s, %v), want original payload", result.Describe(), err)
	}
	if captured.RuntimeID != runtimeInstance.ID() || captured.Script != 29 || captured.Span != span ||
		!captured.Payload.IdentityEqual(payload) || !captured.Architecture.IdentityEqual(architecture) {
		t.Fatalf("captured request = %#v", captured)
	}
}

func TestAggressorProfileHostFallbackPreservesInvocation(t *testing.T) {
	t.Parallel()

	payloadCell := NewCell(String("original"))
	var captured Invocation
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		captured = invocation
		invocation.Arguments[0].Set(String("host mutation"))
		return String("host result"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.aggressorProfile(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  41,
		Name:    "setup_strings",
		Arguments: []Argument{{
			Name:      "$payload",
			Reference: payloadCell,
		}},
	})
	if err != nil || result.String() != "host result" {
		t.Fatalf("Host fallback = (%s, %v)", result.Describe(), err)
	}
	if len(captured.Arguments) != 1 || captured.Arguments[0].Reference != payloadCell || payloadCell.Get().String() != "host mutation" {
		t.Fatalf("Host invocation did not preserve source reference: %#v / %s", captured.Arguments, payloadCell.Get().Describe())
	}
}

func TestAggressorProfileArityStopsProviderAndHost(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorProfileProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorProfileProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for name, invalidCounts := range map[string][]int{
		"killdate":              {1},
		"setup_strings":         {0, 2},
		"setup_transformations": {0, 1, 3},
	} {
		for _, count := range invalidCounts {
			arguments := make([]Value, count)
			for index := range arguments {
				arguments[index] = String("argument")
			}
			result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
			if invokeErr == nil || !result.IsNull() || !strings.Contains(invokeErr.Error(), "expected exactly") {
				t.Errorf("%s/%d = (%s, %v), want null arity error", name, count, result.Describe(), invokeErr)
			}
		}
	}
	if got := len(provider.snapshot()); got != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid arities reached provider/Host = %d/%d", got, hostCalls.Load())
	}
}

func TestAggressorProfileProviderErrorAndOverridePrecedence(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("profile failed")
	provider := &recordingAggressorProfileProvider{
		handle: func(context.Context, AggressorProfileRequest) (Value, error) {
			return String("discarded"), sentinel
		},
	}
	runtimeInstance, err := New(
		WithAggressorProfileProvider(provider),
		WithFunction("setup_strings", func(context.Context, Invocation) (Value, error) {
			return String("override"), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Invoke(context.Background(), "setup_strings", String("payload"))
	if err != nil || result.String() != "override" || len(provider.snapshot()) != 0 {
		t.Fatalf("WithFunction precedence = (%s, %v), provider calls %d", result.Describe(), err, len(provider.snapshot()))
	}
	result, err = runtimeInstance.Invoke(context.Background(), "killdate")
	if !errors.Is(err, sentinel) || !result.IsNull() || len(provider.snapshot()) != 1 {
		t.Fatalf("provider error = (%s, %v), calls %d", result.Describe(), err, len(provider.snapshot()))
	}
}

func TestAggressorProfileRejectsTypedNilAndNilAdapter(t *testing.T) {
	t.Parallel()

	var typedNil *recordingAggressorProfileProvider
	if _, err := New(WithAggressorProfileProvider(typedNil)); err == nil || !strings.Contains(err.Error(), "Aggressor profile provider is nil") {
		t.Fatalf("typed-nil provider error = %v", err)
	}
	if _, err := New(WithAggressorProfileProvider(AggressorProfileProviderFunc(nil))); err == nil || !strings.Contains(err.Error(), "Aggressor profile provider is nil") {
		t.Fatalf("nil provider function option error = %v", err)
	}
	if _, err := AggressorProfileProviderFunc(nil).HandleAggressorProfile(context.Background(), AggressorProfileRequest{}); err == nil {
		t.Fatal("nil AggressorProfileProviderFunc returned no error")
	}
}

func TestPortableScriptLoaderInheritsAggressorProfileProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-profile.cna")
	if err := os.WriteFile(childPath, []byte(`setup_strings("child");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-profile.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
killdate();
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorProfileProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader profile route reached Host")
		})),
		WithAggressorProfileProvider(provider),
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
		requests[0].Span.Source != "parent-profile.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child profile provenance = %#v", requests)
	}
}
