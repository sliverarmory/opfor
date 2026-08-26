package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type recordingAggressorPEProvider struct {
	mu       sync.Mutex
	requests []AggressorPERequest
	handle   func(context.Context, AggressorPERequest) (Value, error)
}

func (provider *recordingAggressorPEProvider) HandleAggressorPE(
	ctx context.Context,
	request AggressorPERequest,
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

func (provider *recordingAggressorPEProvider) snapshot() []AggressorPERequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorPERequest(nil), provider.requests...)
}

type aggressorPEProviderTestCase struct {
	name      string
	operation AggressorPEOperation
	arguments []Value
}

func aggressorPEProviderTestCases() []aggressorPEProviderTestCase {
	content := BinaryString([]byte{0x00, 0x41, 0xff})
	return []aggressorPEProviderTestCase{
		{name: "pe_insert_rich_header", operation: AggressorPEInsertRichHeader, arguments: []Value{content, BinaryString([]byte("rich"))}},
		{name: "pe_mask_section", operation: AggressorPEMaskSection, arguments: []Value{content, String(".text"), Int(23)}},
		{name: "pe_patch_code", operation: AggressorPEPatchCode, arguments: []Value{content, BinaryString([]byte("find")), BinaryString([]byte("replace"))}},
		{name: "pe_remove_rich_header", operation: AggressorPERemoveRichHeader, arguments: []Value{content}},
		{name: "pe_set_compile_time_with_string", operation: AggressorPESetCompileTimeWithString, arguments: []Value{content, String("01 Jan 2020 15:16:17")}},
		{name: "pe_set_export_name", operation: AggressorPESetExportName, arguments: []Value{content, String("beacon.dll")}},
		{name: "pe_set_value_at", operation: AggressorPESetValueAt, arguments: []Value{content, String("SizeOfImage"), Long(22334455)}},
		{name: "pedump", operation: AggressorPEDump, arguments: []Value{content}},
	}
}

func TestAggressorPEProviderFunctionSetAndDocumentedSpecs(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPEProviderFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	wantNames := []string{
		"pe_insert_rich_header",
		"pe_mask_section",
		"pe_patch_code",
		"pe_remove_rich_header",
		"pe_set_compile_time_with_string",
		"pe_set_export_name",
		"pe_set_value_at",
		"pedump",
	}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("Aggressor PE provider names = %q, want %q", names, wantNames)
	}
	for _, test := range aggressorPEProviderTestCases() {
		spec, ok := aggressorPEProviderSpecs[test.name]
		minimum, maximum := len(test.arguments), len(test.arguments)
		if test.name == "pe_set_export_name" {
			minimum = 1
		}
		if !ok || spec.operation != test.operation || spec.minimum != minimum || spec.maximum != maximum {
			t.Errorf("Aggressor PE provider spec %q = %#v/%v, want %q/%d..%d",
				test.name, spec, ok, test.operation, minimum, maximum)
		}
		if !slices.Contains(DefaultFunctionNames(), test.name) {
			t.Errorf("DefaultFunctionNames does not contain %q", test.name)
		}
	}
}

func TestAggressorPEProviderArgumentsResultsAndProvenance(t *testing.T) {
	t.Parallel()

	provided := HashValue(NewOrderedHash())
	provider := &recordingAggressorPEProvider{
		handle: func(context.Context, AggressorPERequest) (Value, error) {
			return provided, nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed PE provider route reached Host")
		})),
		WithAggressorPEProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	tests := aggressorPEProviderTestCases()
	for _, test := range tests {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		if invokeErr != nil || !result.IdentityEqual(provided) {
			t.Errorf("%s = (%s, %v), want identical provider Value", test.name, result.Describe(), invokeErr)
		}
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("configured PE provider reached Host %d time(s)", hostCalls.Load())
	}
	requests := provider.snapshot()
	if len(requests) != len(tests) {
		t.Fatalf("PE provider requests = %d, want %d", len(requests), len(tests))
	}
	for index, request := range requests {
		test := tests[index]
		if request.Operation != test.operation || request.Name != test.name ||
			request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 ||
			request.Script != 0 || request.Span != (Span{}) {
			t.Errorf("request %d metadata = %#v", index, request)
		}
		if len(request.Arguments) != len(test.arguments) {
			t.Errorf("request %s arguments = %d, want %d", request.Name, len(request.Arguments), len(test.arguments))
			continue
		}
		for argumentIndex, argument := range test.arguments {
			if !request.Arg(argumentIndex).IdentityEqual(argument) {
				t.Errorf("request %s argument %d = %s, want identity %s",
					request.Name, argumentIndex, request.Arg(argumentIndex).Describe(), argument.Describe())
			}
		}
		if request.HasArgument(-1) || request.HasArgument(len(test.arguments)) || !request.Arg(len(test.arguments)).IsNull() {
			t.Errorf("request %s absent argument policy failed", request.Name)
		}
	}
}

func TestAggressorPEProviderExportNameEvidenceUnion(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorPEProvider{
		handle: func(_ context.Context, request AggressorPERequest) (Value, error) {
			return request.Arg(0), nil
		},
	}
	runtimeInstance, err := New(WithAggressorPEProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	contentOnly := BinaryString([]byte{0x00, 0x41, 0xff})
	contentAndName := BinaryString([]byte{0x4d, 0x5a})
	exportName := String("beacon.dll")
	for _, invocation := range []struct {
		arguments []Value
		want      Value
	}{
		{arguments: []Value{contentOnly}, want: contentOnly},
		{arguments: []Value{contentAndName, exportName}, want: contentAndName},
	} {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), "pe_set_export_name", invocation.arguments...)
		if invokeErr != nil || !result.IdentityEqual(invocation.want) {
			t.Fatalf("pe_set_export_name(%d arguments) = (%s, %v), want identical content",
				len(invocation.arguments), result.Describe(), invokeErr)
		}
	}

	requests := provider.snapshot()
	if len(requests) != 2 {
		t.Fatalf("pe_set_export_name requests = %d, want 2", len(requests))
	}
	if requests[0].Operation != AggressorPESetExportName || requests[0].HasArgument(1) ||
		!requests[0].Arg(1).IsNull() || !requests[0].Arg(0).IdentityEqual(contentOnly) {
		t.Fatalf("one-argument export-name request = %#v", requests[0])
	}
	if requests[1].Operation != AggressorPESetExportName || !requests[1].HasArgument(1) ||
		!requests[1].Arg(0).IdentityEqual(contentAndName) || !requests[1].Arg(1).IdentityEqual(exportName) {
		t.Fatalf("two-argument export-name request = %#v", requests[1])
	}
}

func TestAggressorPEProviderResolvesReferencesOnce(t *testing.T) {
	t.Parallel()

	content := ArrayValue(NewArray(String("content")))
	find := HashValue(NewOrderedHash())
	replacement := BinaryString([]byte{0x00, 0xff})
	contentCell := NewCell(content)
	findCell := NewCell(find)
	replacementCell := NewCell(replacement)
	span := Span{Source: "pe-provider-values.cna", Start: Position{Line: 7, Column: 4}}
	var captured AggressorPERequest
	provider := AggressorPEProviderFunc(func(_ context.Context, request AggressorPERequest) (Value, error) {
		captured = request
		contentCell.Set(String("late content"))
		findCell.Set(String("late find"))
		replacementCell.Set(String("late replacement"))
		return request.Arg(2), nil
	})
	runtimeInstance, err := New(WithAggressorPEProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.aggressorPEProviderCall(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  61,
		Name:    "pe_patch_code",
		Span:    span,
		Arguments: []Argument{
			{Name: "$content", Reference: contentCell},
			{Name: "$find", Reference: findCell},
			{Name: "$replacement", Reference: replacementCell},
		},
	})
	if err != nil || !result.IdentityEqual(replacement) {
		t.Fatalf("PE provider result = (%s, %v), want original replacement", result.Describe(), err)
	}
	if captured.RuntimeID != runtimeInstance.ID() || captured.Script != 61 || captured.Span != span ||
		!captured.Arg(0).IdentityEqual(content) || !captured.Arg(1).IdentityEqual(find) ||
		!captured.Arg(2).IdentityEqual(replacement) {
		t.Fatalf("captured PE request = %#v", captured)
	}
}

func TestAggressorPEProviderInvalidAritiesStopProviderAndHost(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorPEProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorPEProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, test := range aggressorPEProviderTestCases() {
		spec := aggressorPEProviderSpecs[test.name]
		for _, count := range []int{testCountBelow(spec.minimum), spec.maximum + 1} {
			arguments := make([]Value, count)
			for index := range arguments {
				arguments[index] = Int(int32(index))
			}
			result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, arguments...)
			wantError := "expected exactly"
			if spec.minimum != spec.maximum {
				wantError = fmt.Sprintf("expected %d to %d", spec.minimum, spec.maximum)
			}
			if invokeErr == nil || !result.IsNull() || !strings.Contains(invokeErr.Error(), wantError) {
				t.Errorf("%s/%d = (%s, %v), want null arity error containing %q",
					test.name, count, result.Describe(), invokeErr, wantError)
			}
		}
	}
	if len(provider.snapshot()) != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid PE calls reached provider/Host = %d/%d", len(provider.snapshot()), hostCalls.Load())
	}
}

func testCountBelow(count int) int {
	if count == 0 {
		return 0
	}
	return count - 1
}

func TestAggressorPEProviderHostFallbackPreservesInvocation(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("PE Host result")
	wantResult := ObjectValue(&struct{ result string }{"host"})
	contentCell := NewCell(BinaryString([]byte("original")))
	sectionCell := NewCell(String(".text"))
	keyCell := NewCell(Int(23))
	span := Span{Source: "pe-provider-host.cna", Start: Position{Line: 3, Column: 2}}
	var captured Invocation
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		captured = invocation
		invocation.Arguments[1].Set(String("Host mutation"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	original := Invocation{
		Runtime: runtimeInstance,
		Script:  31,
		Name:    "pe_mask_section",
		Span:    span,
		Arguments: []Argument{
			{Name: "$content", Reference: contentCell},
			{Name: "$section", Reference: sectionCell},
			{Name: "$key", Reference: keyCell},
		},
	}
	result, err := runtimeInstance.aggressorPEProviderCall(context.Background(), original)
	if !errors.Is(err, wantErr) || !result.IdentityEqual(wantResult) {
		t.Fatalf("PE Host fallback = (%s, %v), want identical result/%v", result.Describe(), err, wantErr)
	}
	if hostCalls.Load() != 1 || captured.Runtime != original.Runtime || captured.Script != original.Script ||
		captured.Name != original.Name || captured.Span != original.Span || len(captured.Arguments) != 3 ||
		captured.Arguments[0].Reference != contentCell || captured.Arguments[1].Reference != sectionCell ||
		captured.Arguments[2].Reference != keyCell || sectionCell.Get().String() != "Host mutation" {
		t.Fatalf("captured Host invocation/calls = %#v/%d", captured, hostCalls.Load())
	}
}

func TestAggressorPEProviderExportNameHostFallbackPreservesEvidenceForms(t *testing.T) {
	t.Parallel()

	for _, argumentCount := range []int{1, 2} {
		argumentCount := argumentCount
		t.Run(fmt.Sprintf("arguments-%d", argumentCount), func(t *testing.T) {
			t.Parallel()

			contentCell := NewCell(BinaryString([]byte("original")))
			exportCell := NewCell(String("beacon.dll"))
			arguments := []Argument{{Name: "$content", Reference: contentCell}}
			if argumentCount == 2 {
				arguments = append(arguments, Argument{Name: "$name", Reference: exportCell})
			}
			original := Invocation{
				Script:    47,
				Name:      "pe_set_export_name",
				Span:      Span{Source: "pe-export-host.cna", Start: Position{Line: 4, Column: 3}},
				Arguments: arguments,
			}
			var captured Invocation
			runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
				captured = invocation
				if argumentCount == 2 {
					invocation.Arguments[1].Set(String("host-mutated.dll"))
				}
				return invocation.Arguments[0].Resolve(), nil
			})))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			original.Runtime = runtimeInstance

			result, err := runtimeInstance.aggressorPEProviderCall(context.Background(), original)
			if err != nil || !result.IdentityEqual(contentCell.Get()) {
				t.Fatalf("Host fallback = (%s, %v), want original content", result.Describe(), err)
			}
			if captured.Runtime != runtimeInstance || captured.Script != original.Script ||
				captured.Name != original.Name || captured.Span != original.Span ||
				len(captured.Arguments) != argumentCount || captured.Arguments[0].Reference != contentCell {
				t.Fatalf("captured export-name Host invocation = %#v", captured)
			}
			if argumentCount == 2 && (captured.Arguments[1].Reference != exportCell || exportCell.Get().String() != "host-mutated.dll") {
				t.Fatalf("two-argument export-name Host reference was not preserved: %#v", captured.Arguments[1])
			}
		})
	}
}

func TestAggressorPEProviderErrorsCancellationOverridesAndNilPolicy(t *testing.T) {
	t.Parallel()

	var typedNil *recordingAggressorPEProvider
	if _, err := New(WithAggressorPEProvider(typedNil)); err == nil || !strings.Contains(err.Error(), "Aggressor PE provider is nil") {
		t.Fatalf("typed-nil PE provider error = %v", err)
	}
	var nilFunction AggressorPEProviderFunc
	if _, err := New(WithAggressorPEProvider(nilFunction)); err == nil {
		t.Fatal("nil PE provider function was accepted")
	}
	if _, err := nilFunction.HandleAggressorPE(context.Background(), AggressorPERequest{}); err == nil {
		t.Fatal("direct nil PE provider function call succeeded")
	}

	sentinel := errors.New("PE provider failed")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	provider := WithAggressorPEProvider(AggressorPEProviderFunc(func(context.Context, AggressorPERequest) (Value, error) {
		providerCalls.Add(1)
		return String("discarded"), sentinel
	}))
	host := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
		hostCalls.Add(1)
		return Null(), nil
	}))
	override := WithFunction("pedump", func(context.Context, Invocation) (Value, error) {
		return String("override"), nil
	})
	for _, options := range [][]Option{{host, provider, override}, {host, override, provider}} {
		runtimeInstance, err := New(options...)
		if err != nil {
			t.Fatal(err)
		}
		result, invokeErr := runtimeInstance.Invoke(context.Background(), "pedump")
		if invokeErr != nil || result.String() != "override" {
			t.Errorf("PE WithFunction override = (%s, %v)", result.Describe(), invokeErr)
		}
		_ = runtimeInstance.Close(context.Background())
	}
	if providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("PE override reached provider/Host = %d/%d", providerCalls.Load(), hostCalls.Load())
	}

	runtimeInstance, err := New(host, provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Invoke(context.Background(), "pedump", String("content"))
	if !errors.Is(err, sentinel) || !result.IsNull() || providerCalls.Load() != 1 || hostCalls.Load() != 0 {
		t.Fatalf("PE provider error = (%s, %v), provider/Host %d/%d",
			result.Describe(), err, providerCalls.Load(), hostCalls.Load())
	}

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = runtimeInstance.Invoke(preCanceled, "pedump", String("content"))
	if !errors.Is(err, context.Canceled) || !result.IsNull() || providerCalls.Load() != 1 {
		t.Fatalf("pre-canceled PE call = (%s, %v), provider calls %d",
			result.Describe(), err, providerCalls.Load())
	}

	var cancelDuring context.CancelFunc
	cancelProvider := AggressorPEProviderFunc(func(_ context.Context, _ AggressorPERequest) (Value, error) {
		cancelDuring()
		return String("discarded after cancellation"), nil
	})
	cancelRuntime, err := New(WithAggressorPEProvider(cancelProvider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cancelRuntime.Close(context.Background()) })
	during, cancel := context.WithCancel(context.Background())
	cancelDuring = cancel
	result, err = cancelRuntime.Invoke(during, "pedump", String("content"))
	if !errors.Is(err, context.Canceled) || !result.IsNull() {
		t.Fatalf("cancel-during PE call = (%s, %v)", result.Describe(), err)
	}
}

func TestAggressorPEProviderConcurrentRequestsAreDetached(t *testing.T) {
	t.Parallel()

	const calls = 32
	var providerCalls atomic.Int32
	provider := AggressorPEProviderFunc(func(_ context.Context, request AggressorPERequest) (Value, error) {
		providerCalls.Add(1)
		value := request.Arg(0)
		request.Arguments[0] = String("provider-local mutation")
		return value, nil
	})
	runtimeInstance, err := New(WithAggressorPEProvider(provider))
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
			want := fmt.Sprintf("content-%d", index)
			result, invokeErr := runtimeInstance.Invoke(context.Background(), "pedump", String(want))
			if invokeErr != nil || result.String() != want {
				errorsChannel <- fmt.Errorf("pedump %d = (%s, %v), want %q", index, result.Describe(), invokeErr, want)
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
	if providerCalls.Load() != calls {
		t.Fatalf("concurrent PE provider calls = %d, want %d", providerCalls.Load(), calls)
	}
}

func TestPortableScriptLoaderInheritsAggressorPEProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-pe-provider.cna")
	if err := os.WriteFile(childPath, []byte(`pe_set_export_name("child", "beacon.dll");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-pe-provider.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
pedump("parent");
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorPEProvider{
		handle: func(_ context.Context, request AggressorPERequest) (Value, error) {
			return request.Arg(0), nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader PE provider route reached Host")
		})),
		WithAggressorPEProvider(provider),
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
		t.Fatalf("PE child provider/Host requests = %d/%d, want 2/0", len(requests), hostCalls.Load())
	}
	if requests[0].Operation != AggressorPEDump || requests[0].Arg(0).String() != "parent" ||
		requests[1].Operation != AggressorPESetExportName || requests[1].Arg(0).String() != "child" ||
		!requests[1].HasArgument(1) || requests[1].Arg(1).String() != "beacon.dll" ||
		requests[0].RuntimeID != runtimeInstance.ID() || requests[1].RuntimeID == 0 ||
		requests[1].RuntimeID == requests[0].RuntimeID || requests[0].Script != 1 || requests[1].Script != 1 ||
		requests[0].Span.Source != "parent-pe-provider.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child PE requests = %#v", requests)
	}
}
