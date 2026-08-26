package opfor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type techniqueTestCallable func(context.Context, ...Value) (Value, error)

func (callable techniqueTestCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return callable(ctx, values...)
}

func TestAggressorBeaconTechniqueFamiliesFullContractAndCallbackABI(t *testing.T) {
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("unexpected host call"), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	script := loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-families.cna", `
$elevator_registration = beacon_elevator_register("shared", "elevator description", {
    return $1 . "|" . $2;
});
$exploit_registration = beacon_exploit_register("shared", "exploit description", {
    return $1 . "|" . $2;
});
$method_registration = beacon_remote_exec_method_register("shared", "method description", {
    return $1 . "|" . $2 . "|" . $3;
});
$remote_registration = beacon_remote_exploit_register("shared", "x64", "remote description", {
    return $1 . "|" . $2 . "|" . $3;
});
`)
	for _, name := range []string{
		"$elevator_registration", "$exploit_registration", "$method_registration", "$remote_registration",
	} {
		if value := script.Get(name); !value.IsNull() {
			t.Errorf("%s = %s, want provisional $null", name, value.Describe())
		}
	}

	descriptions := map[string]string{
		"beacon_elevator_describe":           "elevator description",
		"beacon_exploit_describe":            "exploit description",
		"beacon_remote_exec_method_describe": "method description",
		"beacon_remote_exploit_describe":     "remote description",
	}
	for function, want := range descriptions {
		value, invokeErr := runtimeInstance.Invoke(context.Background(), function, String("shared"))
		if invokeErr != nil || value.String() != want {
			t.Errorf("%s(shared) = (%q, %v), want %q", function, value.String(), invokeErr, want)
		}
	}
	arch, err := runtimeInstance.Invoke(context.Background(), "beacon_remote_exploit_arch", String("shared"))
	if err != nil || arch.String() != "x64" {
		t.Fatalf("beacon_remote_exploit_arch(shared) = (%q, %v), want x64", arch.String(), err)
	}
	for _, function := range []string{
		"beacon_elevators", "beacon_exploits", "beacon_remote_exec_methods", "beacon_remote_exploits",
	} {
		value, invokeErr := runtimeInstance.Invoke(context.Background(), function)
		if invokeErr != nil {
			t.Fatalf("%s: %v", function, invokeErr)
		}
		if got, want := techniqueValueNames(t, value), []string{"shared"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %q, want %q", function, got, want)
		}
	}

	tests := []struct {
		kind AggressorBeaconTechniqueKind
		args []Value
		want string
	}{
		{AggressorBeaconTechniqueElevator, []Value{String("B-ELEVATE"), String(`run   --flag "A B"`)}, `B-ELEVATE|run   --flag "A B"`},
		{AggressorBeaconTechniqueExploit, []Value{String("B-EXPLOIT"), String("listener one")}, "B-EXPLOIT|listener one"},
		{AggressorBeaconTechniqueRemoteExecMethod, []Value{String("B-METHOD"), String("target.example"), String(`cmd   /c "whoami /all"`)}, `B-METHOD|target.example|cmd   /c "whoami /all"`},
		{AggressorBeaconTechniqueRemoteExploit, []Value{String("B-REMOTE"), String("10.0.0.8"), String("listener two")}, "B-REMOTE|10.0.0.8|listener two"},
	}
	for _, test := range tests {
		value, invokeErr := runtimeInstance.InvokeAggressorBeaconTechnique(nil, test.kind, "shared", test.args...)
		if invokeErr != nil || value.String() != test.want {
			t.Errorf("InvokeAggressorBeaconTechnique(%s) = (%q, %v), want %q", test.kind, value.String(), invokeErr, test.want)
		}
	}
	if calls := hostCalls.Load(); calls != 0 {
		t.Fatalf("Host calls = %d, want no local tasking or Host dispatch", calls)
	}
}

func TestAggressorBeaconTechniqueFunctionNamesArityMissingAndNonCallable(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	wantFunctions := map[string]int{
		"beacon_elevator_describe": 1, "beacon_elevator_register": 3, "beacon_elevators": 0,
		"beacon_exploit_describe": 1, "beacon_exploit_register": 3, "beacon_exploits": 0,
		"beacon_remote_exec_method_describe": 1, "beacon_remote_exec_method_register": 3, "beacon_remote_exec_methods": 0,
		"beacon_remote_exploit_arch": 1, "beacon_remote_exploit_describe": 1, "beacon_remote_exploit_register": 4, "beacon_remote_exploits": 0,
	}
	available := make(map[string]struct{})
	for _, name := range runtimeInstance.FunctionNames() {
		available[name] = struct{}{}
	}
	for name, arity := range wantFunctions {
		if _, exists := available[name]; !exists {
			t.Errorf("FunctionNames missing %s", name)
		}
		arguments := make([]Value, arity+1)
		for index := range arguments {
			arguments[index] = String("x")
		}
		_, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
		if invokeErr == nil || !strings.Contains(invokeErr.Error(), "expected exactly") {
			t.Errorf("%s invalid arity error = %v, want exact-arity rejection", name, invokeErr)
		}
	}

	for _, function := range []string{
		"beacon_elevator_describe", "beacon_exploit_describe",
		"beacon_remote_exec_method_describe", "beacon_remote_exploit_describe",
		"beacon_remote_exploit_arch",
	} {
		value, invokeErr := runtimeInstance.Invoke(context.Background(), function, String("missing"))
		if invokeErr != nil || !value.IsNull() {
			t.Errorf("%s(missing) = (%s, %v), want $null", function, value.Describe(), invokeErr)
		}
		value, invokeErr = runtimeInstance.Invoke(context.Background(), function, Null())
		if invokeErr != nil || !value.IsNull() {
			t.Errorf("%s($null) = (%s, %v), want provisional $null", function, value.Describe(), invokeErr)
		}
	}

	nonCallable := []struct {
		name string
		args []Value
	}{
		{"beacon_elevator_register", []Value{String("e"), String("description"), Int(1)}},
		{"beacon_exploit_register", []Value{String("x"), String("description"), Int(1)}},
		{"beacon_remote_exec_method_register", []Value{String("m"), String("description"), Int(1)}},
		{"beacon_remote_exploit_register", []Value{String("r"), String("x86"), String("description"), Int(1)}},
	}
	for _, test := range nonCallable {
		value, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.args...)
		if !errors.Is(invokeErr, ErrInvalidCallable) || !value.IsNull() {
			t.Errorf("%s non-callable = (%s, %v), want $null/ErrInvalidCallable", test.name, value.Describe(), invokeErr)
		}
	}
}

func TestAggressorBeaconTechniqueCatalogValidationAndDefensiveCopies(t *testing.T) {
	invalid := []struct {
		name    string
		kind    AggressorBeaconTechniqueKind
		catalog AggressorBeaconTechniqueCatalog
		want    string
	}{
		{"kind", "invalid", AggressorBeaconTechniqueCatalog{}, "invalid Aggressor Beacon technique kind"},
		{"empty name", AggressorBeaconTechniqueElevator, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{}}}, "name is empty"},
		{"duplicate exact name", AggressorBeaconTechniqueExploit, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{Name: "same"}, {Name: "same"}}}, "duplicate name"},
		{"missing remote architecture", AggressorBeaconTechniqueRemoteExploit, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{Name: "remote"}}}, "expected x86 or x64"},
		{"invalid remote architecture case", AggressorBeaconTechniqueRemoteExploit, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{Name: "remote", Architecture: "X64"}}}, "expected x86 or x64"},
		{"architecture on elevator", AggressorBeaconTechniqueElevator, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{Name: "elevator", Architecture: "x64"}}}, "architecture is not valid"},
		{"architecture on exploit", AggressorBeaconTechniqueExploit, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{Name: "exploit", Architecture: "x86"}}}, "architecture is not valid"},
		{"architecture on method", AggressorBeaconTechniqueRemoteExecMethod, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{Name: "method", Architecture: "x64"}}}, "architecture is not valid"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(WithAggressorBeaconTechniqueCatalog(test.kind, test.catalog))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New error = %v, want %q", err, test.want)
			}
		})
	}

	input := AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{
		{Name: "base", Description: "original"},
		{Name: "Base", Description: "case distinct"},
	}}
	runtimeInstance, err := New(WithAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator, input))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	input.Techniques[0].Description = "mutated input"
	input.Techniques = append(input.Techniques, AggressorBeaconTechniqueMetadata{Name: "late"})

	snapshot, err := runtimeInstance.SnapshotAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator)
	if err != nil {
		t.Fatal(err)
	}
	want := []AggressorBeaconTechniqueMetadata{{Name: "base", Description: "original"}, {Name: "Base", Description: "case distinct"}}
	if !reflect.DeepEqual(snapshot.Techniques, want) {
		t.Fatalf("snapshot = %#v, want %#v", snapshot.Techniques, want)
	}
	snapshot.Techniques[0].Description = "mutated snapshot"
	snapshot.Techniques = nil
	again, _ := runtimeInstance.SnapshotAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator)
	if !reflect.DeepEqual(again.Techniques, want) {
		t.Fatalf("snapshot mutation affected runtime: %#v", again.Techniques)
	}

	metadataType := reflect.TypeOf(AggressorBeaconTechniqueMetadata{})
	if metadataType.NumField() != 3 {
		t.Fatalf("public metadata fields = %d, want exactly name/description/architecture", metadataType.NumField())
	}
	for index := 0; index < metadataType.NumField(); index++ {
		if strings.Contains(strings.ToLower(metadataType.Field(index).Name), "call") {
			t.Fatalf("public metadata exposes callable field %q", metadataType.Field(index).Name)
		}
	}
}

func TestAggressorBeaconTechniqueCatalogRepeatedOptionReplacesOnlyOneKind(t *testing.T) {
	runtimeInstance, err := New(
		WithAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{Name: "old", Description: "old"}}}),
		WithAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueExploit, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{Name: "exploit", Description: "preserved"}}}),
		WithAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{Name: "new", Description: "new"}}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	elevators, _ := runtimeInstance.SnapshotAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator)
	exploits, _ := runtimeInstance.SnapshotAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueExploit)
	if got, want := techniqueCatalogNames(elevators), []string{"new"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement elevator catalog = %q, want %q", got, want)
	}
	if got, want := techniqueCatalogNames(exploits), []string{"exploit"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unrelated exploit catalog = %q, want %q", got, want)
	}
}

func TestAggressorBeaconTechniqueScriptRegistrationRejectsArchitectureViolations(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"invalid remote architecture", `beacon_remote_exploit_register("bad", "arm64", "bad", { return; });`, "expected x86 or x64"},
		{"null remote architecture", `beacon_remote_exploit_register("bad", $null, "bad", { return; });`, "expected x86 or x64"},
		{"architecture argument on nonremote kind", `beacon_elevator_register("bad", "x64", "bad", { return; });`, "expected exactly 3"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, err := New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			program, err := CompileString("invalid-technique-registration.cna", test.source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runtimeInstance.Load(context.Background(), program); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAggressorBeaconTechniqueLayeringCoalescingOrderCaseAndUnload(t *testing.T) {
	base := AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{
		{Name: "shared", Description: "base"},
		{Name: "base-only", Description: "base only"},
	}}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator, base),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("unexpected Host tasking")
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	first := loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-first.cna", `
beacon_elevator_register("shared", "first", { return "first"; });
beacon_elevator_register("Alpha", "upper", { return "upper"; });
beacon_elevator_register("shared", "first replacement", { return "first replacement"; });
`)
	second := loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-second.cna", `
beacon_elevator_register("shared", "second", { return "second"; });
beacon_elevator_register("alpha", "lower", { return "lower"; });
`)

	runtimeInstance.aggressorBeaconTechniques.mu.RLock()
	layers := len(runtimeInstance.aggressorBeaconTechniques.namespaces[AggressorBeaconTechniqueElevator].techniques["shared"])
	runtimeInstance.aggressorBeaconTechniques.mu.RUnlock()
	if layers != 3 {
		t.Fatalf("shared layers = %d, want base + one coalesced layer per owner", layers)
	}
	assertAggressorBeaconTechniqueCatalog(t, runtimeInstance, AggressorBeaconTechniqueElevator,
		[]string{"shared", "base-only", "Alpha", "alpha"},
		[]string{"second", "base only", "upper", "lower"})
	value, err := runtimeInstance.InvokeAggressorBeaconTechnique(context.Background(), AggressorBeaconTechniqueElevator, "shared", String("id"), String("raw"))
	if err != nil || value.String() != "second" {
		t.Fatalf("effective second callback = (%q, %v)", value.String(), err)
	}

	if err := second.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertAggressorBeaconTechniqueCatalog(t, runtimeInstance, AggressorBeaconTechniqueElevator,
		[]string{"shared", "base-only", "Alpha"},
		[]string{"first replacement", "base only", "upper"})
	value, err = runtimeInstance.InvokeAggressorBeaconTechnique(context.Background(), AggressorBeaconTechniqueElevator, "shared", String("id"), String("raw"))
	if err != nil || value.String() != "first replacement" {
		t.Fatalf("restored first callback = (%q, %v)", value.String(), err)
	}

	if err := first.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertAggressorBeaconTechniqueCatalog(t, runtimeInstance, AggressorBeaconTechniqueElevator,
		[]string{"shared", "base-only"}, []string{"base", "base only"})
	_, err = runtimeInstance.InvokeAggressorBeaconTechnique(context.Background(), AggressorBeaconTechniqueElevator, "shared", String("id"), String("raw"))
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Operation != "Aggressor Beacon elevator callback" || unsupported.Name != "shared" {
		t.Fatalf("base-only invocation error = %v, want typed UnsupportedError", err)
	}
	unsupported = nil
	_, err = runtimeInstance.InvokeAggressorBeaconTechnique(context.Background(), AggressorBeaconTechniqueElevator, "missing", String("id"), String("raw"))
	if !errors.As(err, &unsupported) || unsupported.Operation != "Aggressor Beacon elevator callback" || unsupported.Name != "missing" {
		t.Fatalf("missing invocation error = %v, want typed UnsupportedError", err)
	}
	if calls := hostCalls.Load(); calls != 0 {
		t.Fatalf("base-only/missing Host calls = %d, want zero", calls)
	}
}

func TestAggressorBeaconTechniqueRollbackAndCallbackRevocation(t *testing.T) {
	runtimeInstance, err := New(
		WithAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueExploit, AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{Name: "rolled-back", Description: "base"}}}),
		WithFunction("fail_technique_load", func(context.Context, Invocation) (Value, error) {
			return Null(), errors.New("load failure")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("technique-rollback.cna", `
beacon_exploit_register("rolled-back", "temporary", { return "bad"; });
fail_technique_load();
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); err == nil {
		t.Fatal("Load succeeded, want rollback-triggering failure")
	}
	snapshot, _ := runtimeInstance.SnapshotAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueExploit)
	if got, want := snapshot.Techniques, []AggressorBeaconTechniqueMetadata{{Name: "rolled-back", Description: "base"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("failed-load rollback catalog = %#v, want restored base %#v", got, want)
	}

	script := loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-revocation.cna", `
beacon_exploit_register("owned", "owned", { return "live"; });
`)
	callback, exists := runtimeInstance.aggressorBeaconTechniques.callback(AggressorBeaconTechniqueExploit, "owned")
	if !exists || callback == nil {
		t.Fatal("registered callback missing")
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := callback.Invoke(context.Background(), String("id"), String("listener")); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("retained callback after unload error = %v, want ErrScriptUnloaded", err)
	}
}

func TestAggressorBeaconTechniqueRemovalClearsTruncatedCallbackTail(t *testing.T) {
	base := AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{
		Name: "layered", Description: "base",
	}}}
	runtimeInstance, err := New(WithAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator, base))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script := loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-tail-clear.cna", `
beacon_elevator_register("layered", "script", { return; });
`)
	runtimeInstance.aggressorBeaconTechniques.mu.RLock()
	layers := runtimeInstance.aggressorBeaconTechniques.namespaces[AggressorBeaconTechniqueElevator].techniques["layered"]
	if len(layers) != 2 {
		runtimeInstance.aggressorBeaconTechniques.mu.RUnlock()
		t.Fatalf("layers = %d, want two", len(layers))
	}
	backing := layers[:cap(layers)]
	runtimeInstance.aggressorBeaconTechniques.mu.RUnlock()
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(backing) < 2 {
		t.Fatalf("layer backing capacity = %d, want at least two", len(backing))
	}
	if backing[1].owner != 0 || backing[1].callback != nil || backing[1].metadata != (AggressorBeaconTechniqueMetadata{}) {
		t.Fatalf("truncated layer tail retained state: %#v", backing[1])
	}
}

func TestAggressorBeaconTechniqueLayersRevokeAtUnloadAdmission(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	runtimeInstance, err := New(WithFunction("block_technique_unload", func(context.Context, Invocation) (Value, error) {
		enteredOnce.Do(func() { close(entered) })
		<-release
		return Null(), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script := loadAggressorBeaconTechniqueScript(t, runtimeInstance, "blocked-technique-unload.cna", `
beacon_remote_exec_method_register("transient", "transient", { return; });
sub hold_technique_unload { block_technique_unload(); }
`)
	callDone := make(chan error, 1)
	go func() {
		_, callErr := script.Call(context.Background(), "hold_technique_unload")
		callDone <- callErr
	}()
	<-entered

	unloadContext, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	unloadErr := script.Unload(unloadContext)
	cancel()
	if !errors.Is(unloadErr, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("blocked Unload error = %v, want deadline exceeded", unloadErr)
	}
	snapshot, _ := runtimeInstance.SnapshotAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueRemoteExecMethod)
	if len(snapshot.Techniques) != 0 {
		close(release)
		t.Fatalf("techniques visible after unload admission: %#v", snapshot.Techniques)
	}
	_, err = runtimeInstance.InvokeAggressorBeaconTechnique(context.Background(), AggressorBeaconTechniqueRemoteExecMethod, "transient", String("id"), String("target"), String("raw"))
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		close(release)
		t.Fatalf("invocation after unload admission error = %v, want UnsupportedError", err)
	}
	close(release)
	if callErr := <-callDone; callErr != nil && !errors.Is(callErr, context.Canceled) && !errors.Is(callErr, ErrScriptUnloaded) {
		t.Fatalf("blocked call error = %v", callErr)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPortableScriptLoaderInheritsAggressorBeaconTechniqueBaseOnly(t *testing.T) {
	base := AggressorBeaconTechniqueCatalog{Techniques: []AggressorBeaconTechniqueMetadata{{
		Name: "base", Description: "base description",
	}}}
	runtimeInstance, err := New(WithAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator, base))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("loader-technique-catalog.sl", `
import sleep.runtime.ScriptLoader;
beacon_elevator_register("parent", "parent description", { return; });
$loader = [new ScriptLoader];
$child = [$loader loadScript: "child", 'beacon_elevator_register("child", "child description", { return; }); return @(beacon_elevator_describe("base"), beacon_elevator_describe("parent"), beacon_elevator_describe("child"));', $null];
return [$child runScript];
`)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	values := techniqueValueSlice(t, parent.Result())
	if len(values) != 3 || values[0].String() != "base description" || !values[1].IsNull() || values[2].String() != "child description" {
		t.Fatalf("child catalog result = %s", parent.Result().Describe())
	}
	parentCatalog, _ := runtimeInstance.SnapshotAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator)
	if got, want := techniqueCatalogNames(parentCatalog), []string{"base", "parent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parent catalog = %q, want %q", got, want)
	}
}

func TestInvokeAggressorBeaconTechniqueMetersScriptAndNativeReentry(t *testing.T) {
	t.Run("ordinary script callback", func(t *testing.T) {
		const instructionLimit = 100
		runtimeInstance, err := New(WithInstructionLimit(instructionLimit))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-script-meter.cna", `
beacon_elevator_register("loop", "loop", {
    $value = 0;
    while (1) { $value++; }
});
`)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err = runtimeInstance.InvokeAggressorBeaconTechnique(ctx, AggressorBeaconTechniqueElevator, "loop", String("id"), String("raw"))
		assertTechniqueInstructionLimit(t, err, instructionLimit)
	})

	t.Run("native callback recursive public entry", func(t *testing.T) {
		const instructionLimit = 100
		var runtimeInstance *Runtime
		var owner *Script
		var calls atomic.Int32
		callback := techniqueTestCallable(func(ctx context.Context, _ ...Value) (Value, error) {
			if _, err := owner.Call(ctx, "small_burn"); err != nil {
				return Null(), err
			}
			if calls.Add(1) < 512 {
				return runtimeInstance.InvokeAggressorBeaconTechnique(ctx, AggressorBeaconTechniqueExploit, "native-loop", String("id"), String("listener"))
			}
			return Null(), nil
		})
		var err error
		runtimeInstance, err = New(
			WithInstructionLimit(instructionLimit),
			WithInitialGlobals(map[string]Value{"native_callback": FunctionValue(callback)}),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		owner = loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-native-meter.cna", `
sub small_burn { $x = 1; $x++; return $x; }
beacon_exploit_register("native-loop", "native loop", $native_callback);
`)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err = runtimeInstance.InvokeAggressorBeaconTechnique(ctx, AggressorBeaconTechniqueExploit, "native-loop", String("id"), String("listener"))
		assertTechniqueInstructionLimit(t, err, instructionLimit)
		if calls.Load() < 2 {
			t.Fatalf("native callback calls = %d, want recursive reentry", calls.Load())
		}
	})
}

func TestAggressorBeaconTechniqueConcurrentSnapshotsInvocationsAndUnload(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	const count = 24
	scripts := make([]*Script, count)
	for index := range scripts {
		scripts[index] = loadAggressorBeaconTechniqueScript(t, runtimeInstance, fmt.Sprintf("concurrent-technique-%d.cna", index), fmt.Sprintf(`
beacon_remote_exploit_register("name-%d", "x86", "description-%d", { return "ok"; });
`, index, index))
	}

	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 100; iteration++ {
				_, _ = runtimeInstance.SnapshotAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueRemoteExploit)
				_, _ = runtimeInstance.Invoke(context.Background(), "beacon_remote_exploits")
				index := iteration % count
				_, invokeErr := runtimeInstance.InvokeAggressorBeaconTechnique(context.Background(), AggressorBeaconTechniqueRemoteExploit,
					fmt.Sprintf("name-%d", index), String("id"), String("target"), String("listener"))
				if invokeErr != nil {
					var unsupported *UnsupportedError
					if !errors.As(invokeErr, &unsupported) &&
						!errors.Is(invokeErr, ErrScriptUnloaded) &&
						!errors.Is(invokeErr, context.Canceled) {
						t.Errorf("concurrent invocation error = %v", invokeErr)
						return
					}
				}
			}
		}()
	}
	for _, script := range scripts {
		wait.Add(1)
		go func(script *Script) {
			defer wait.Done()
			_ = script.Unload(context.Background())
		}(script)
	}
	wait.Wait()
	snapshot, _ := runtimeInstance.SnapshotAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueRemoteExploit)
	if len(snapshot.Techniques) != 0 {
		t.Fatalf("script-owned techniques survived concurrent unload: %#v", snapshot.Techniques)
	}
}

func TestInvokeAggressorBeaconTechniqueValidationAndClosedRuntime(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtimeInstance.InvokeAggressorBeaconTechnique(nil, "invalid", "name")
	if err == nil || !strings.Contains(err.Error(), "invalid Aggressor Beacon technique kind") {
		t.Fatalf("invalid kind error = %v", err)
	}
	_, err = runtimeInstance.InvokeAggressorBeaconTechnique(nil, AggressorBeaconTechniqueElevator, "", Null(), Null())
	if err == nil || !strings.Contains(err.Error(), "name is empty") {
		t.Fatalf("empty name error = %v", err)
	}
	for _, test := range []struct {
		kind AggressorBeaconTechniqueKind
		want int
	}{
		{AggressorBeaconTechniqueElevator, 2},
		{AggressorBeaconTechniqueExploit, 2},
		{AggressorBeaconTechniqueRemoteExecMethod, 3},
		{AggressorBeaconTechniqueRemoteExploit, 3},
	} {
		arguments := make([]Value, test.want-1)
		_, err = runtimeInstance.InvokeAggressorBeaconTechnique(nil, test.kind, "name", arguments...)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("expects exactly %d", test.want)) {
			t.Errorf("%s callback arity error = %v", test.kind, err)
		}
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = runtimeInstance.InvokeAggressorBeaconTechnique(nil, AggressorBeaconTechniqueElevator, "name", Null(), Null())
	if !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("closed runtime error = %v, want ErrRuntimeClosed", err)
	}
	var nilRuntime *Runtime
	if _, err := nilRuntime.SnapshotAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueElevator); err == nil {
		t.Fatal("nil Runtime snapshot succeeded")
	}
	if _, err := nilRuntime.InvokeAggressorBeaconTechnique(nil, AggressorBeaconTechniqueElevator, "name", Null(), Null()); err == nil {
		t.Fatal("nil Runtime invocation succeeded")
	}
}

func loadAggressorBeaconTechniqueScript(t *testing.T, runtimeInstance *Runtime, name, source string) *Script {
	t.Helper()
	program, err := CompileString(name, source)
	if err != nil {
		t.Fatalf("CompileString(%s): %v", name, err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load(%s): %v", name, err)
	}
	return script
}

func techniqueValueSlice(t *testing.T, value Value) []Value {
	t.Helper()
	array, ok := value.Array()
	if !ok || array == nil {
		t.Fatalf("value = %s, want array", value.Describe())
	}
	return array.Values()
}

func techniqueValueNames(t *testing.T, value Value) []string {
	t.Helper()
	values := techniqueValueSlice(t, value)
	names := make([]string, len(values))
	for index, value := range values {
		names[index] = value.String()
	}
	return names
}

func techniqueCatalogNames(catalog AggressorBeaconTechniqueCatalog) []string {
	names := make([]string, len(catalog.Techniques))
	for index, metadata := range catalog.Techniques {
		names[index] = metadata.Name
	}
	return names
}

func assertAggressorBeaconTechniqueCatalog(
	t *testing.T,
	runtimeInstance *Runtime,
	kind AggressorBeaconTechniqueKind,
	wantNames []string,
	wantDescriptions []string,
) {
	t.Helper()
	catalog, err := runtimeInstance.SnapshotAggressorBeaconTechniqueCatalog(kind)
	if err != nil {
		t.Fatal(err)
	}
	if got := techniqueCatalogNames(catalog); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("%s names = %q, want %q", kind, got, wantNames)
	}
	descriptions := make([]string, len(catalog.Techniques))
	for index, metadata := range catalog.Techniques {
		descriptions[index] = metadata.Description
	}
	if !reflect.DeepEqual(descriptions, wantDescriptions) {
		t.Fatalf("%s descriptions = %q, want %q", kind, descriptions, wantDescriptions)
	}
}

func assertTechniqueInstructionLimit(t *testing.T, err error, limit uint64) {
	t.Helper()
	if !errors.Is(err, ErrInstructionLimit) {
		t.Fatalf("execution error = %v, want ErrInstructionLimit", err)
	}
	var limitError *LimitError
	if !errors.As(err, &limitError) || limitError.Resource != "instruction" || limitError.Limit != limit {
		t.Fatalf("LimitError = %+v, want instruction/%d", limitError, limit)
	}
}
