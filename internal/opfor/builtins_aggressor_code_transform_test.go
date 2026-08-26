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

type recordingAggressorCodeTransformProvider struct {
	mu       sync.Mutex
	requests []AggressorCodeTransformRequest
	handle   func(context.Context, AggressorCodeTransformRequest) (Value, error)
}

func (provider *recordingAggressorCodeTransformProvider) HandleAggressorCodeTransform(
	ctx context.Context,
	request AggressorCodeTransformRequest,
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

func (provider *recordingAggressorCodeTransformProvider) snapshot() []AggressorCodeTransformRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorCodeTransformRequest(nil), provider.requests...)
}

type aggressorCodeTransformTestCase struct {
	name      string
	operation AggressorCodeTransformOperation
	arguments []Value
}

func aggressorCodeTransformTestCases() []aggressorCodeTransformTestCase {
	return []aggressorCodeTransformTestCase{
		{
			name:      "encode",
			operation: AggressorCodeTransformEncode,
			arguments: []Value{BinaryString([]byte{0x00, 0x41, 0xff}), String("xor"), String("x64")},
		},
		{
			name:      "powershell_compress",
			operation: AggressorCodeTransformPowerShellCompress,
			arguments: []Value{String("Write-Output 'OPFOR'")},
		},
		{
			name:      "transform_vbs",
			operation: AggressorCodeTransformVBS,
			arguments: []Value{BinaryString([]byte{0x22, 0x80, 0x00}), String("17")},
		},
	}
}

func TestAggressorCodeTransformFunctionSetAndDocumentedSpecs(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorCodeTransformFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	slices.Sort(names)
	wantNames := make([]string, 0, len(aggressorCodeTransformTestCases()))
	for _, test := range aggressorCodeTransformTestCases() {
		wantNames = append(wantNames, test.name)
		spec, exists := aggressorCodeTransformSpecs[test.name]
		if !exists || spec.operation != test.operation || spec.arguments != len(test.arguments) {
			t.Errorf("%s spec = %#v/%t, want %q/%d", test.name, spec, exists, test.operation, len(test.arguments))
		}
		if string(test.operation) != test.name {
			t.Errorf("%s operation spelling = %q", test.name, test.operation)
		}
		if !slices.Contains(DefaultFunctionNames(), test.name) {
			t.Errorf("DefaultFunctionNames does not contain %q", test.name)
		}
	}
	slices.Sort(wantNames)
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("Aggressor code-transform names = %q, want %q", names, wantNames)
	}
}

func TestAggressorCodeTransformProviderArgumentsResultsAndProvenance(t *testing.T) {
	t.Parallel()

	providerResult := HashValue(NewOrderedHash())
	provider := &recordingAggressorCodeTransformProvider{
		handle: func(context.Context, AggressorCodeTransformRequest) (Value, error) {
			return providerResult, nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed code transform reached Host")
		})),
		WithAggressorCodeTransformProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	tests := aggressorCodeTransformTestCases()
	for _, test := range tests {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		if invokeErr != nil || !result.IdentityEqual(providerResult) {
			t.Errorf("%s = (%s, %v), want identical provider Value", test.name, result.Describe(), invokeErr)
		}
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("configured provider reached Host %d time(s)", hostCalls.Load())
	}

	requests := provider.snapshot()
	if len(requests) != len(tests) {
		t.Fatalf("code-transform requests = %d, want %d", len(requests), len(tests))
	}
	for index, request := range requests {
		test := tests[index]
		if request.Operation != test.operation || request.Name != test.name ||
			request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 ||
			request.Script != 0 || request.Span != (Span{}) {
			t.Errorf("request %d route/provenance = %#v", index, request)
		}
		if len(request.Arguments) != len(test.arguments) {
			t.Errorf("%s arguments = %d, want %d", test.name, len(request.Arguments), len(test.arguments))
			continue
		}
		for argumentIndex, argument := range test.arguments {
			if !request.Arg(argumentIndex).IdentityEqual(argument) {
				t.Errorf("%s argument %d lost Value identity", test.name, argumentIndex)
			}
		}
		if request.HasArgument(-1) || request.HasArgument(len(test.arguments)) ||
			!request.Arg(len(test.arguments)).IsNull() {
			t.Errorf("%s absent-argument policy failed", test.name)
		}
	}
}

func TestAggressorCodeTransformResolvesReferencesOnce(t *testing.T) {
	t.Parallel()

	content := ArrayValue(NewArray(String("content")))
	encoder := HashValue(NewOrderedHash())
	architecture := ObjectValue(&struct{ architecture bool }{true})
	cells := []*Cell{NewCell(content), NewCell(encoder), NewCell(architecture)}
	span := Span{Source: "code-transform-values.cna", Start: Position{Line: 7, Column: 4}}
	var captured AggressorCodeTransformRequest
	provider := AggressorCodeTransformProviderFunc(func(
		_ context.Context,
		request AggressorCodeTransformRequest,
	) (Value, error) {
		captured = request
		for _, cell := range cells {
			cell.Set(String("mutated after resolution"))
		}
		return request.Arg(0), nil
	})
	runtimeInstance, err := New(WithAggressorCodeTransformProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  37,
		Name:    "encode",
		Span:    span,
		Arguments: []Argument{
			{Name: "$content", Reference: cells[0]},
			{Name: "$encoder", Reference: cells[1]},
			{Name: "$architecture", Reference: cells[2]},
		},
	}
	result, invokeErr := runtimeInstance.aggressorCodeTransform(context.Background(), invocation)
	if invokeErr != nil || !result.IdentityEqual(content) {
		t.Fatalf("encode = (%s, %v), want original content identity", result.Describe(), invokeErr)
	}
	want := []Value{content, encoder, architecture}
	if captured.Operation != AggressorCodeTransformEncode || captured.Name != "encode" ||
		captured.RuntimeID != runtimeInstance.ID() || captured.Script != 37 || captured.Span != span ||
		len(captured.Arguments) != len(want) {
		t.Fatalf("captured request = %#v", captured)
	}
	for index, value := range want {
		if !captured.Arg(index).IdentityEqual(value) {
			t.Errorf("captured argument %d lost pre-mutation identity", index)
		}
		if invocation.Arguments[index].Reference != cells[index] {
			t.Errorf("provider snapshot replaced caller reference %d", index)
		}
	}
}

func TestAggressorCodeTransformHostFallbackPreservesInvocation(t *testing.T) {
	t.Parallel()

	cells := []*Cell{
		NewCell(BinaryString([]byte("original"))),
		NewCell(String("xor")),
		NewCell(String("x64")),
	}
	wantResult := ObjectValue(&struct{ hostOwned bool }{true})
	wantErr := errors.New("Host code-transform result")
	var captured Invocation
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		captured = invocation
		for _, argument := range invocation.Arguments {
			argument.Set(String("mutated by Host"))
		}
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  43,
		Name:    "encode",
		Span:    Span{Source: "code-transform-host.cna", Start: Position{Line: 4, Column: 2}},
		Arguments: []Argument{
			{Name: "$content", Reference: cells[0]},
			{Name: "$encoder", Reference: cells[1]},
			{Name: "$architecture", Reference: cells[2]},
		},
	}
	result, invokeErr := runtimeInstance.aggressorCodeTransform(context.Background(), invocation)
	if !result.IdentityEqual(wantResult) || !errors.Is(invokeErr, wantErr) || hostCalls.Load() != 1 {
		t.Fatalf("Host fallback = (%s, %v), calls %d", result.Describe(), invokeErr, hostCalls.Load())
	}
	if captured.Runtime != invocation.Runtime || captured.Script != invocation.Script ||
		captured.Name != invocation.Name || captured.Span != invocation.Span ||
		len(captured.Arguments) != len(invocation.Arguments) {
		t.Fatalf("Host fallback changed Invocation metadata: %#v", captured)
	}
	for index, cell := range cells {
		if captured.Arguments[index].Reference != cell || cell.Get().String() != "mutated by Host" {
			t.Errorf("Host fallback argument %d lost reference identity", index)
		}
	}
}

func TestAggressorPowerShellCompressWithoutHookPreservesRawHostInvocation(t *testing.T) {
	t.Parallel()

	scriptCell := NewCell(String("original script"))
	var captured Invocation
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		captured = invocation
		invocation.Arguments[0].Set(String("mutated by Host"))
		return invocation.Arg(0), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  51,
		Name:    "powershell_compress",
		Span:    Span{Source: "powershell-compress-host.cna", Start: Position{Line: 8, Column: 3}},
		Arguments: []Argument{{
			Name:      "$script",
			Reference: scriptCell,
		}},
	}
	result, invokeErr := runtimeInstance.aggressorCodeTransform(context.Background(), invocation)
	if invokeErr != nil || result.String() != "mutated by Host" {
		t.Fatalf("powershell_compress Host fallback = (%s, %v)", result.Describe(), invokeErr)
	}
	if captured.Runtime != invocation.Runtime || captured.Script != invocation.Script ||
		captured.Name != invocation.Name || captured.Span != invocation.Span ||
		len(captured.Arguments) != 1 || captured.Arguments[0].Reference != scriptCell ||
		scriptCell.Get().String() != "mutated by Host" {
		t.Fatalf("powershell_compress Host fallback changed raw Invocation: %#v", captured)
	}
}

func TestAggressorCodeTransformInvalidAritiesStopHookProviderAndHost(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorCodeTransformProvider{}
	var hookCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithFunction("record_code_transform_hook", func(context.Context, Invocation) (Value, error) {
			hookCalls.Add(1)
			return Null(), nil
		}),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorCodeTransformProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("code-transform-arity-hook.cna", `
set POWERSHELL_COMPRESS {
    record_code_transform_hook();
    return "hook";
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}

	for _, test := range aggressorCodeTransformTestCases() {
		for _, count := range []int{len(test.arguments) - 1, len(test.arguments) + 1} {
			result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, make([]Value, count)...)
			if invokeErr == nil || !result.IsNull() || !strings.Contains(invokeErr.Error(), "expected exactly") {
				t.Errorf("%s/%d = (%s, %v), want null exact-arity error", test.name, count, result.Describe(), invokeErr)
			}
		}
	}
	if got := len(provider.snapshot()); got != 0 || hookCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid arities reached provider/hook/Host = %d/%d/%d", got, hookCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorPowerShellCompressHookPrecedenceAndLifecycle(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorCodeTransformProvider{
		handle: func(context.Context, AggressorCodeTransformRequest) (Value, error) {
			return String("provider"), nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("hook/provider path reached Host")
		})),
		WithAggressorCodeTransformProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	loadHook := func(name, result string) *Script {
		t.Helper()
		program, compileErr := CompileString(name, fmt.Sprintf(`set POWERSHELL_COMPRESS { return %q . $1; }`, result))
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		script, loadErr := runtimeInstance.Load(context.Background(), program)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		return script
	}
	oldHook := loadHook("old-compress-hook.cna", "old:")
	newHook := loadHook("new-compress-hook.cna", "new:")

	result, err := runtimeInstance.Invoke(context.Background(), "powershell_compress", String("script"))
	if err != nil || result.String() != "new:script" {
		t.Fatalf("newest hook = (%s, %v), want new:script", result.Describe(), err)
	}
	if len(provider.snapshot()) != 0 || hostCalls.Load() != 0 {
		t.Fatalf("newest hook reached provider/Host = %d/%d", len(provider.snapshot()), hostCalls.Load())
	}

	if err := newHook.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err = runtimeInstance.Invoke(context.Background(), "powershell_compress", String("script"))
	if err != nil || result.String() != "old:script" {
		t.Fatalf("revealed older hook = (%s, %v), want old:script", result.Describe(), err)
	}

	if err := oldHook.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err = runtimeInstance.Invoke(context.Background(), "powershell_compress", String("script"))
	if err != nil || result.String() != "provider" {
		t.Fatalf("provider after hook unload = (%s, %v), want provider", result.Describe(), err)
	}
	requests := provider.snapshot()
	if len(requests) != 1 || requests[0].Operation != AggressorCodeTransformPowerShellCompress ||
		requests[0].Arg(0).String() != "script" || hostCalls.Load() != 0 {
		t.Fatalf("provider fallback requests/Host = %#v/%d", requests, hostCalls.Load())
	}
}

func TestAggressorPowerShellCompressHookReturnsResolvedValueUnchanged(t *testing.T) {
	t.Parallel()

	original := HashValue(NewOrderedHash())
	scriptCell := NewCell(original)
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithFunction("mutate_compression_source", func(context.Context, Invocation) (Value, error) {
			scriptCell.Set(String("mutated after hook argument resolution"))
			return Null(), nil
		}),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorCodeTransformProvider(AggressorCodeTransformProviderFunc(func(
			context.Context,
			AggressorCodeTransformRequest,
		) (Value, error) {
			providerCalls.Add(1)
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("compress-hook-value.cna", `
set POWERSHELL_COMPRESS {
    mutate_compression_source();
    return $1;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	result, invokeErr := runtimeInstance.aggressorCodeTransform(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  61,
		Name:    "powershell_compress",
		Arguments: []Argument{{
			Name:      "$script",
			Reference: scriptCell,
		}},
	})
	if invokeErr != nil || !result.IdentityEqual(original) {
		t.Fatalf("hook result = (%s, %v), want original compound identity", result.Describe(), invokeErr)
	}
	if scriptCell.Get().String() != "mutated after hook argument resolution" ||
		providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("hook snapshot/provider/Host = %s/%d/%d", scriptCell.Get().Describe(), providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorPowerShellCompressHookErrorIsAuthoritative(t *testing.T) {
	t.Parallel()

	hookErr := errors.New("PowerShell compression hook rejected request")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithFunction("fail_code_transform_hook", func(context.Context, Invocation) (Value, error) {
			return String("partial hook result"), hookErr
		}),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host"), nil
		})),
		WithAggressorCodeTransformProvider(AggressorCodeTransformProviderFunc(func(
			context.Context,
			AggressorCodeTransformRequest,
		) (Value, error) {
			providerCalls.Add(1)
			return String("provider"), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("compress-hook-error.cna", `
set POWERSHELL_COMPRESS { return fail_code_transform_hook($1); }
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	result, invokeErr := runtimeInstance.Invoke(context.Background(), "powershell_compress", String("script"))
	if !result.IsNull() || !errors.Is(invokeErr, hookErr) || providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("hook error = (%s, %v), provider/Host %d/%d", result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorPowerShellCompressHookSharesInstructionMeter(t *testing.T) {
	t.Parallel()

	const instructionLimit = 100
	var providerCalls atomic.Int32
	runtimeInstance, err := New(
		WithInstructionLimit(instructionLimit),
		WithAggressorCodeTransformProvider(AggressorCodeTransformProviderFunc(func(
			context.Context,
			AggressorCodeTransformRequest,
		) (Value, error) {
			providerCalls.Add(1)
			return String("provider"), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("compress-hook-meter.cna", `
$calls = 0;
set POWERSHELL_COMPRESS {
    $calls++;
    if ($calls < 512) { powershell_compress($1); }
    return $1;
}
powershell_compress("script");
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, executeErr := runtimeInstance.Execute(ctx, program)
	if !errors.Is(executeErr, ErrInstructionLimit) {
		t.Fatalf("recursive compression-hook error = %v, want ErrInstructionLimit", executeErr)
	}
	var limit *LimitError
	if !errors.As(executeErr, &limit) || limit.Resource != "instruction" || limit.Limit != instructionLimit {
		t.Fatalf("compression-hook LimitError = %+v", limit)
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("recursive hook reached provider %d time(s)", providerCalls.Load())
	}
}

func TestAggressorCodeTransformProviderErrorCancellationAndOverride(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("code-transform provider rejected request")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	providerOption := WithAggressorCodeTransformProvider(AggressorCodeTransformProviderFunc(func(
		context.Context,
		AggressorCodeTransformRequest,
	) (Value, error) {
		providerCalls.Add(1)
		return String("partial"), wantErr
	}))
	for _, overrideFirst := range []bool{false, true} {
		options := []Option{
			WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
				hostCalls.Add(1)
				return String("Host"), nil
			})),
			providerOption,
		}
		for _, test := range aggressorCodeTransformTestCases() {
			test := test
			option := WithFunction(test.name, func(context.Context, Invocation) (Value, error) {
				return String("override:" + test.name), nil
			})
			if overrideFirst {
				options = append([]Option{option}, options...)
			} else {
				options = append(options, option)
			}
		}
		runtimeInstance, err := New(options...)
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range aggressorCodeTransformTestCases() {
			// Invalid wrapper arity deliberately proves selection occurs before
			// native validation, independent of option order.
			result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name)
			if invokeErr != nil || result.String() != "override:"+test.name {
				t.Fatalf("override-first=%t %s = (%s, %v)", overrideFirst, test.name, result.Describe(), invokeErr)
			}
		}
		if closeErr := runtimeInstance.Close(context.Background()); closeErr != nil {
			t.Fatal(closeErr)
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
	result, invokeErr := runtimeInstance.Invoke(
		context.Background(), "encode", BinaryString([]byte("code")), String("xor"), String("x86"),
	)
	if !result.IsNull() || !errors.Is(invokeErr, wantErr) || providerCalls.Load() != 1 || hostCalls.Load() != 0 {
		t.Fatalf("provider error = (%s, %v), provider/Host %d/%d", result.Describe(), invokeErr, providerCalls.Load(), hostCalls.Load())
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	result, invokeErr = runtimeInstance.Invoke(canceled, "powershell_compress", String("script"))
	if !result.IsNull() || !errors.Is(invokeErr, context.Canceled) || providerCalls.Load() != 1 {
		t.Fatalf("pre-canceled request = (%s, %v), provider calls %d", result.Describe(), invokeErr, providerCalls.Load())
	}

	postCanceled, cancelPost := context.WithCancel(context.Background())
	postRuntime, err := New(WithAggressorCodeTransformProvider(AggressorCodeTransformProviderFunc(func(
		context.Context,
		AggressorCodeTransformRequest,
	) (Value, error) {
		cancelPost()
		return String("completed after cancellation"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = postRuntime.Close(context.Background()) })
	result, invokeErr = postRuntime.Invoke(postCanceled, "transform_vbs", BinaryString([]byte("code")), Int(3))
	if !result.IsNull() || !errors.Is(invokeErr, context.Canceled) {
		t.Fatalf("provider-canceled request = (%s, %v)", result.Describe(), invokeErr)
	}
}

func TestAggressorCodeTransformRejectsTypedNilAndNilAdapter(t *testing.T) {
	t.Parallel()

	var typedNil *recordingAggressorCodeTransformProvider
	if _, err := New(WithAggressorCodeTransformProvider(typedNil)); err == nil ||
		!strings.Contains(err.Error(), "Aggressor code-transform provider is nil") {
		t.Fatalf("typed-nil provider error = %v", err)
	}
	var nilFunction AggressorCodeTransformProviderFunc
	if _, err := New(WithAggressorCodeTransformProvider(nilFunction)); err == nil ||
		!strings.Contains(err.Error(), "Aggressor code-transform provider is nil") {
		t.Fatalf("nil provider function option error = %v", err)
	}
	if result, err := nilFunction.HandleAggressorCodeTransform(
		context.Background(), AggressorCodeTransformRequest{},
	); err == nil || !result.IsNull() {
		t.Fatalf("nil provider adapter = (%s, %v), want null/error", result.Describe(), err)
	}
}

func TestAggressorCodeTransformProviderCallsMayOverlap(t *testing.T) {
	t.Parallel()

	const calls = 64
	entered := make(chan struct{}, calls)
	release := make(chan struct{})
	provider := &recordingAggressorCodeTransformProvider{
		handle: func(context.Context, AggressorCodeTransformRequest) (Value, error) {
			entered <- struct{}{}
			<-release
			return String("transformed"), nil
		},
	}
	runtimeInstance, err := New(WithAggressorCodeTransformProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	// Register after Runtime.Close so LIFO cleanup releases blocked provider
	// calls before Close waits for in-flight execution on an early failure.
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
			result, invokeErr := runtimeInstance.Invoke(
				context.Background(), "transform_vbs", BinaryString([]byte(fmt.Sprintf("code-%d", index))), Int(int32(index)),
			)
			if invokeErr != nil {
				errorsByCall[index] = invokeErr
				return
			}
			if result.String() != "transformed" {
				errorsByCall[index] = fmt.Errorf("result = %s, want transformed", result.Describe())
			}
		}()
	}
	for index := 0; index < calls; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d/%d code-transform calls entered concurrently", index, calls)
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
		if request.Operation != AggressorCodeTransformVBS || request.Name != "transform_vbs" ||
			request.RuntimeID != runtimeInstance.ID() || len(request.Arguments) != 2 {
			t.Fatalf("concurrent request metadata = %#v", request)
		}
		seen[request.Arg(0).String()] = true
	}
	if len(seen) != calls {
		t.Fatalf("concurrent input snapshots = %d unique, want %d", len(seen), calls)
	}
}

func TestPortableScriptLoaderInheritsAggressorCodeTransformProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-code-transform.cna")
	if err := os.WriteFile(childPath, []byte(`powershell_compress("child");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-code-transform.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
encode("parent", "xor", "x64");
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorCodeTransformProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader code-transform route reached Host")
		})),
		WithAggressorCodeTransformProvider(provider),
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
		requests[0].Span.Source != "parent-code-transform.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) ||
		requests[0].Operation != AggressorCodeTransformEncode || requests[0].Arg(0).String() != "parent" ||
		requests[1].Operation != AggressorCodeTransformPowerShellCompress || requests[1].Arg(0).String() != "child" {
		t.Fatalf("parent/child code-transform provenance = %#v", requests)
	}
}
