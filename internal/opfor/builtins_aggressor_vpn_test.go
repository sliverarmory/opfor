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

type recordingAggressorVPNProvider struct {
	mu       sync.Mutex
	requests []AggressorVPNRequest
	handle   func(context.Context, AggressorVPNRequest) (Value, error)
}

func (provider *recordingAggressorVPNProvider) HandleAggressorVPN(
	ctx context.Context,
	request AggressorVPNRequest,
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

func (provider *recordingAggressorVPNProvider) snapshot() []AggressorVPNRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorVPNRequest(nil), provider.requests...)
}

func TestAggressorVPNFunctionSetAndDocumentedSpecs(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorVPNFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	slices.Sort(names)
	wantNames := []string{"vpn_interface_info", "vpn_interfaces", "vpn_tap_create", "vpn_tap_delete"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("Aggressor VPN names = %q, want %q", names, wantNames)
	}
	wantSpecs := map[string]aggressorVPNSpec{
		"vpn_interface_info": {operation: AggressorVPNInterfaceInfo, minimum: 1, maximum: 2, returnsValue: true},
		"vpn_interfaces":     {operation: AggressorVPNInterfaces, minimum: 0, maximum: 0, returnsValue: true},
		"vpn_tap_create":     {operation: AggressorVPNTAPCreate, minimum: 5, maximum: 5},
		"vpn_tap_delete":     {operation: AggressorVPNTAPDelete, minimum: 1, maximum: 1},
	}
	if !reflect.DeepEqual(aggressorVPNSpecs, wantSpecs) {
		t.Fatalf("Aggressor VPN specs = %#v, want %#v", aggressorVPNSpecs, wantSpecs)
	}
	for _, name := range wantNames {
		if !slices.Contains(DefaultFunctionNames(), name) {
			t.Errorf("DefaultFunctionNames does not contain %q", name)
		}
	}
}

func TestAggressorVPNProviderRequestShapesAndReturns(t *testing.T) {
	t.Parallel()

	interfaces := ArrayValue(NewArray(String("phear0")))
	metadata := HashValue(NewHash())
	provider := &recordingAggressorVPNProvider{
		handle: func(_ context.Context, request AggressorVPNRequest) (Value, error) {
			switch request.Operation {
			case AggressorVPNInterfaces:
				return interfaces, nil
			case AggressorVPNInterfaceInfo:
				return metadata, nil
			case AggressorVPNTAPCreate, AggressorVPNTAPDelete:
				return String("discarded side-effect result"), nil
			default:
				return Null(), fmt.Errorf("unexpected VPN operation %q", request.Operation)
			}
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed VPN request reached Host")
		})),
		WithAggressorVPNProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Invoke(context.Background(), "vpn_interfaces")
	if err != nil || !result.IdentityEqual(interfaces) {
		t.Fatalf("vpn_interfaces = (%s, %v), want identical interface array", result.Describe(), err)
	}
	result, err = runtimeInstance.Invoke(context.Background(), "vpn_interface_info", String("phear0"))
	if err != nil || !result.IdentityEqual(metadata) {
		t.Fatalf("vpn_interface_info = (%s, %v), want identical metadata", result.Describe(), err)
	}
	result, err = runtimeInstance.Invoke(context.Background(), "vpn_interface_info", String("phear0"), Null())
	if err != nil || !result.IdentityEqual(metadata) {
		t.Fatalf("vpn_interface_info key = (%s, %v), want identical metadata", result.Describe(), err)
	}
	result, err = runtimeInstance.Invoke(context.Background(), "vpn_tap_create",
		String("phear0"), Null(), Null(), Int(7324), String("udp"))
	if err != nil || !result.IsNull() {
		t.Fatalf("vpn_tap_create = (%s, %v), want null/nil", result.Describe(), err)
	}
	result, err = runtimeInstance.Invoke(context.Background(), "vpn_tap_delete", String("phear0"))
	if err != nil || !result.IsNull() {
		t.Fatalf("vpn_tap_delete = (%s, %v), want null/nil", result.Describe(), err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("configured VPN provider reached Host %d time(s)", hostCalls.Load())
	}

	requests := provider.snapshot()
	if len(requests) != 5 {
		t.Fatalf("VPN requests = %d, want 5", len(requests))
	}
	for index, request := range requests {
		if request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 || request.Script != 0 || request.Span != (Span{}) {
			t.Errorf("request %d provenance = %#v", index, request)
		}
	}
	if requests[0].Operation != AggressorVPNInterfaces || !requests[0].Interface.IsNull() {
		t.Errorf("interfaces request = %#v", requests[0])
	}
	if requests[1].Operation != AggressorVPNInterfaceInfo || requests[1].Interface.String() != "phear0" || requests[1].HasKey {
		t.Errorf("info request without key = %#v", requests[1])
	}
	if requests[2].Operation != AggressorVPNInterfaceInfo || !requests[2].HasKey || !requests[2].Key.IsNull() {
		t.Errorf("info request with explicit null key = %#v", requests[2])
	}
	create := requests[3]
	if create.Operation != AggressorVPNTAPCreate || create.Interface.String() != "phear0" ||
		!create.MACAddress.IsNull() || !create.Reserved.IsNull() || create.Port.Int32() != 7324 || create.Channel.String() != "udp" {
		t.Errorf("create request = %#v", create)
	}
	if requests[4].Operation != AggressorVPNTAPDelete || requests[4].Interface.String() != "phear0" {
		t.Errorf("delete request = %#v", requests[4])
	}
}

func TestAggressorVPNResolvesReferencesOnceAndPreservesIdentity(t *testing.T) {
	t.Parallel()

	interfaceValue := ArrayValue(NewArray(String("phear0")))
	keyValue := HashValue(NewHash())
	interfaceCell := NewCell(interfaceValue)
	keyCell := NewCell(keyValue)
	span := Span{Source: "vpn-values.cna", Start: Position{Line: 5, Column: 2}}
	var captured AggressorVPNRequest
	provider := AggressorVPNProviderFunc(func(_ context.Context, request AggressorVPNRequest) (Value, error) {
		captured = request
		interfaceCell.Set(String("mutated interface"))
		keyCell.Set(String("mutated key"))
		return request.Key, nil
	})
	runtimeInstance, err := New(WithAggressorVPNProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.aggressorVPN(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  73,
		Name:    "vpn_interface_info",
		Span:    span,
		Arguments: []Argument{
			{Name: "$interface", Reference: interfaceCell},
			{Name: "$key", Reference: keyCell},
		},
	})
	if err != nil || !result.IdentityEqual(keyValue) {
		t.Fatalf("VPN request = (%s, %v), want original key", result.Describe(), err)
	}
	if captured.RuntimeID != runtimeInstance.ID() || captured.Script != 73 || captured.Span != span ||
		!captured.Interface.IdentityEqual(interfaceValue) || !captured.HasKey || !captured.Key.IdentityEqual(keyValue) {
		t.Fatalf("captured VPN request = %#v", captured)
	}
}

func TestAggressorVPNHostFallbackPreservesInvocation(t *testing.T) {
	t.Parallel()

	interfaceCell := NewCell(String("phear0"))
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

	result, err := runtimeInstance.aggressorVPN(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Name:    "vpn_tap_delete",
		Arguments: []Argument{{
			Name:      "$interface",
			Reference: interfaceCell,
		}},
	})
	if err != nil || result.String() != "host result" {
		t.Fatalf("VPN Host fallback = (%s, %v)", result.Describe(), err)
	}
	if len(captured.Arguments) != 1 || captured.Arguments[0].Reference != interfaceCell || interfaceCell.Get().String() != "host mutation" {
		t.Fatalf("Host invocation did not preserve source reference: %#v / %s", captured.Arguments, interfaceCell.Get().Describe())
	}
}

func TestAggressorVPNArityStopsProviderAndHost(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorVPNProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorVPNProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for name, invalidCounts := range map[string][]int{
		"vpn_interface_info": {0, 3},
		"vpn_interfaces":     {1},
		"vpn_tap_create":     {4, 6},
		"vpn_tap_delete":     {0, 2},
	} {
		for _, count := range invalidCounts {
			arguments := make([]Value, count)
			for index := range arguments {
				arguments[index] = String("argument")
			}
			result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
			if invokeErr == nil || !result.IsNull() || !strings.Contains(invokeErr.Error(), "expected") {
				t.Errorf("%s/%d = (%s, %v), want null arity error", name, count, result.Describe(), invokeErr)
			}
		}
	}
	if got := len(provider.snapshot()); got != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid arities reached provider/Host = %d/%d", got, hostCalls.Load())
	}
}

func TestAggressorVPNProviderErrorOverrideAndNilGuards(t *testing.T) {
	t.Parallel()

	var typedNil *recordingAggressorVPNProvider
	if _, err := New(WithAggressorVPNProvider(typedNil)); err == nil || !strings.Contains(err.Error(), "Aggressor VPN provider is nil") {
		t.Fatalf("typed-nil VPN provider error = %v", err)
	}
	if _, err := New(WithAggressorVPNProvider(AggressorVPNProviderFunc(nil))); err == nil {
		t.Fatal("nil VPN provider function option returned no error")
	}
	if _, err := AggressorVPNProviderFunc(nil).HandleAggressorVPN(context.Background(), AggressorVPNRequest{}); err == nil {
		t.Fatal("nil AggressorVPNProviderFunc returned no error")
	}

	sentinel := errors.New("VPN failed")
	provider := &recordingAggressorVPNProvider{
		handle: func(context.Context, AggressorVPNRequest) (Value, error) {
			return String("discarded"), sentinel
		},
	}
	runtimeInstance, err := New(
		WithAggressorVPNProvider(provider),
		WithFunction("vpn_interfaces", func(context.Context, Invocation) (Value, error) {
			return String("override"), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Invoke(context.Background(), "vpn_interfaces")
	if err != nil || result.String() != "override" || len(provider.snapshot()) != 0 {
		t.Fatalf("WithFunction precedence = (%s, %v), provider calls %d", result.Describe(), err, len(provider.snapshot()))
	}
	result, err = runtimeInstance.Invoke(context.Background(), "vpn_tap_delete", String("phear0"))
	if !errors.Is(err, sentinel) || !result.IsNull() || len(provider.snapshot()) != 1 {
		t.Fatalf("VPN provider error = (%s, %v), calls %d", result.Describe(), err, len(provider.snapshot()))
	}
}

func TestPortableScriptLoaderInheritsAggressorVPNProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-vpn.cna")
	if err := os.WriteFile(childPath, []byte(`vpn_interfaces();`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-vpn.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
vpn_interfaces();
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorVPNProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader VPN route reached Host")
		})),
		WithAggressorVPNProvider(provider),
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
		requests[0].Span.Source != "parent-vpn.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child VPN provenance = %#v", requests)
	}
}
