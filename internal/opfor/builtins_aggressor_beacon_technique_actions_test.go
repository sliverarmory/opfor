package opfor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAggressorBeaconTechniqueActionsDispatchDocumentedABIs(t *testing.T) {
	t.Parallel()

	type call struct {
		family string
		args   []Value
	}
	var mu sync.Mutex
	var calls []call
	callback := func(family string) Value {
		return FunctionValue(techniqueTestCallable(func(_ context.Context, values ...Value) (Value, error) {
			mu.Lock()
			calls = append(calls, call{family: family, args: append([]Value(nil), values...)})
			mu.Unlock()
			return String("private callback result"), nil
		}))
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithInitialGlobals(map[string]Value{
			"elevator_callback": callback("elevator"),
			"exploit_callback":  callback("exploit"),
			"method_callback":   callback("method"),
			"remote_callback":   callback("remote"),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-actions.cna", `
beacon_elevator_register("lift", "lift", $elevator_callback);
beacon_exploit_register("local", "local", $exploit_callback);
beacon_remote_exec_method_register("method", "method", $method_callback);
beacon_remote_exploit_register("jump", "x64", "jump", $remote_callback);
`)

	rawElevator := BinaryString([]byte{'r', 'u', 'n', 0, 'x'})
	rawMethod := String(`cmd   /c "whoami /all"`)
	tests := []struct {
		name   string
		values []Value
	}{
		{"belevate_command", []Value{String("B-E"), String("lift"), rawElevator}},
		{"belevate", []Value{String("B-L"), String("local"), String("listener-local")}},
		{"bremote_exec", []Value{String("B-M"), String("method"), String("target.example"), rawMethod}},
		{"bjump", []Value{String("B-J"), String("jump"), String("10.0.0.8"), String("listener-remote")}},
	}
	for _, test := range tests {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.values...)
		if invokeErr != nil {
			t.Errorf("%s: %v", test.name, invokeErr)
		}
		if !result.IsNull() {
			t.Errorf("%s result = %s, want $null", test.name, result.Describe())
		}
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("Host calls = %d, want 0", hostCalls.Load())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 4 {
		t.Fatalf("callback calls = %d, want 4", len(calls))
	}
	want := []call{
		{family: "elevator", args: []Value{String("B-E"), rawElevator}},
		{family: "exploit", args: []Value{String("B-L"), String("listener-local")}},
		{family: "method", args: []Value{String("B-M"), String("target.example"), rawMethod}},
		{family: "remote", args: []Value{String("B-J"), String("10.0.0.8"), String("listener-remote")}},
	}
	for index := range want {
		if calls[index].family != want[index].family || !valueSlicesIdentityEqual(calls[index].args, want[index].args) {
			t.Errorf("call %d = %s/%s, want %s/%s", index, calls[index].family,
				describeTechniqueActionValues(calls[index].args), want[index].family,
				describeTechniqueActionValues(want[index].args))
		}
	}
}

func TestAggressorBeaconTechniqueActionFanoutSnapshotsIDsAndIsolatesArguments(t *testing.T) {
	t.Parallel()

	nested := NewArray(String("nested-a"), String("nested-b"))
	ids := NewArray(String("one"), ArrayValue(nested), String("three"))
	var received [][]Value
	callback := techniqueTestCallable(func(_ context.Context, values ...Value) (Value, error) {
		received = append(received, values)
		if len(received) == 1 {
			if err := ids.Set(0, String("changed")); err != nil {
				return Null(), err
			}
			ids.Append(String("late"))
			// Mutating an importer's variadic slice must not corrupt the next
			// callback's argument tail.
			values[1] = String("corrupted")
		}
		return String("ignored"), nil
	})
	runtimeInstance, err := New(WithInitialGlobals(map[string]Value{
		"fanout_callback": FunctionValue(callback),
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-fanout.cna", `
beacon_elevator_register("fanout", "fanout", $fanout_callback);
`)

	result, err := runtimeInstance.Invoke(
		context.Background(), "belevate_command",
		ArrayValue(ids), String("fanout"), String("raw original"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsNull() {
		t.Fatalf("result = %s, want $null", result.Describe())
	}
	if len(received) != 3 {
		t.Fatalf("callback calls = %d, want top-level snapshot length 3", len(received))
	}
	if got := received[0][0].String(); got != "one" {
		t.Errorf("first ID = %q, want one", got)
	}
	if !received[1][0].IdentityEqual(ArrayValue(nested)) {
		t.Errorf("nested ID = %s, want the nested array as one unflattened ID", received[1][0].Describe())
	}
	if got := received[2][0].String(); got != "three" {
		t.Errorf("third ID = %q, want three", got)
	}
	if got := received[0][1].String(); got != "corrupted" {
		t.Errorf("first retained slice tail = %q, want callback mutation", got)
	}
	for index := 1; index < len(received); index++ {
		if got := received[index][1].String(); got != "raw original" {
			t.Errorf("call %d tail = %q, want isolated raw original", index, got)
		}
	}
	if received[0][0].String() != "one" || !received[1][0].IdentityEqual(ArrayValue(nested)) {
		t.Fatal("retained callback slices were overwritten by a later fan-out call")
	}

	before := len(received)
	result, err = runtimeInstance.Invoke(
		context.Background(), "belevate_command",
		ArrayValue(NewArray()), String("fanout"), String("raw"),
	)
	if err != nil || !result.IsNull() {
		t.Fatalf("empty fan-out = (%s, %v), want $null/nil", result.Describe(), err)
	}
	if len(received) != before {
		t.Fatalf("empty fan-out added %d callback calls", len(received)-before)
	}
}

func TestAggressorBeaconTechniqueActionSnapshotsCallbackBeforeFanout(t *testing.T) {
	t.Parallel()

	var runtimeInstance *Runtime
	var replacementOwner *Script
	var originalCalls atomic.Int32
	var replacementCalls atomic.Int32
	original := techniqueTestCallable(func(ctx context.Context, _ ...Value) (Value, error) {
		if originalCalls.Add(1) == 1 {
			if _, err := replacementOwner.Call(ctx, "install_replacement"); err != nil {
				return Null(), err
			}
		}
		return String("ignored original result"), nil
	})
	replacement := techniqueTestCallable(func(context.Context, ...Value) (Value, error) {
		replacementCalls.Add(1)
		return String("ignored replacement result"), nil
	})
	var err error
	runtimeInstance, err = New(WithInitialGlobals(map[string]Value{
		"original_callback":    FunctionValue(original),
		"replacement_callback": FunctionValue(replacement),
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-original-action.cna", `
beacon_elevator_register("stable", "original", $original_callback);
`)
	replacementOwner = loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-replacement-action.cna", `
sub install_replacement {
    beacon_elevator_register("stable", "replacement", $replacement_callback);
}
`)

	result, err := runtimeInstance.Invoke(
		context.Background(), "belevate_command",
		ArrayValue(NewArray(String("one"), String("two"))),
		String("stable"), String("raw"),
	)
	if err != nil || !result.IsNull() {
		t.Fatalf("fan-out with replacement = (%s, %v), want $null/nil", result.Describe(), err)
	}
	if got := originalCalls.Load(); got != 2 {
		t.Fatalf("original callback calls = %d, want both snapshotted fan-out calls", got)
	}
	if got := replacementCalls.Load(); got != 0 {
		t.Fatalf("replacement callback calls during existing fan-out = %d, want 0", got)
	}

	result, err = runtimeInstance.Invoke(
		context.Background(), "belevate_command",
		String("three"), String("stable"), String("raw"),
	)
	if err != nil || !result.IsNull() {
		t.Fatalf("later dispatch = (%s, %v), want $null/nil", result.Describe(), err)
	}
	if got := replacementCalls.Load(); got != 1 {
		t.Fatalf("replacement callback calls on later dispatch = %d, want 1", got)
	}
}

func TestAggressorBeaconTechniqueActionStopsOnFirstLocalErrorWithoutHostFallback(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("callback rejected second ID")
	var callbackCalls atomic.Int32
	var hostCalls atomic.Int32
	callback := techniqueTestCallable(func(context.Context, ...Value) (Value, error) {
		if callbackCalls.Add(1) == 2 {
			return Null(), wantErr
		}
		return String("ignored"), nil
	})
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithInitialGlobals(map[string]Value{"error_callback": FunctionValue(callback)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-stop.cna", `
beacon_exploit_register("stop", "stop", $error_callback);
`)

	result, err := runtimeInstance.Invoke(
		context.Background(), "belevate",
		ArrayValue(NewArray(String("one"), String("two"), String("three"))),
		String("stop"), String("listener"),
	)
	if !errors.Is(err, wantErr) || !result.IsNull() {
		t.Fatalf("dispatch = (%s, %v), want $null/%v", result.Describe(), err, wantErr)
	}
	if callbackCalls.Load() != 2 || hostCalls.Load() != 0 {
		t.Fatalf("callback/Host calls = %d/%d, want 2/0", callbackCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorBeaconTechniqueActionHostFallbackPreservesInvocation(t *testing.T) {
	t.Parallel()

	type contextKey struct{}
	contextValue := &struct{}{}
	var capturedCtx context.Context
	var captured Invocation
	var hostCalls int
	runtimeInstance, err := New(
		WithAggressorBeaconTechniqueCatalog(AggressorBeaconTechniqueExploit, AggressorBeaconTechniqueCatalog{
			Techniques: []AggressorBeaconTechniqueMetadata{{Name: "base", Description: "metadata only"}},
		}),
		WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
			hostCalls++
			capturedCtx = ctx
			captured = invocation
			return String("private host result"), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	idReference := NewCell(String("B-1"))
	techniqueReference := NewCell(String("base"))
	listenerReference := NewCell(String("listener"))
	span := Span{
		Source: "fallback.cna",
		Start:  Position{Offset: 7, Line: 2, Column: 3},
		End:    Position{Offset: 42, Line: 2, Column: 38},
	}
	original := Invocation{
		Runtime: runtimeInstance,
		Script:  77,
		Name:    "belevate",
		Arguments: []Argument{
			{Name: "$ids", Reference: idReference},
			{Name: "$exploit", Reference: techniqueReference},
			{Name: "$listener", Reference: listenerReference},
		},
		Span: span,
	}
	ctx := context.WithValue(context.Background(), contextKey{}, contextValue)
	result, err := runtimeInstance.belevate(ctx, original)
	if err != nil || !result.IsNull() {
		t.Fatalf("base-only fallback = (%s, %v), want $null/nil", result.Describe(), err)
	}
	if hostCalls != 1 {
		t.Fatalf("Host calls = %d, want exactly 1", hostCalls)
	}
	if capturedCtx.Value(contextKey{}) != contextValue {
		t.Fatal("Host did not receive the original context values")
	}
	if captured.Runtime != original.Runtime || captured.Script != original.Script || captured.Name != original.Name || captured.Span != original.Span {
		t.Fatalf("captured invocation identity = %#v, want %#v", captured, original)
	}
	if len(captured.Arguments) != len(original.Arguments) || &captured.Arguments[0] != &original.Arguments[0] {
		t.Fatal("Host did not receive the original argument slice")
	}
	for index := range original.Arguments {
		if captured.Arguments[index].Name != original.Arguments[index].Name ||
			captured.Arguments[index].Reference != original.Arguments[index].Reference ||
			!captured.Arg(index).IdentityEqual(original.Arg(index)) {
			t.Errorf("argument %d changed during Host fallback", index)
		}
	}
}

func TestAggressorBeaconTechniqueActionMissingEmptyAndHostResultPolicy(t *testing.T) {
	t.Parallel()

	hostErr := errors.New("host tasking failed")
	var hostCalls atomic.Int32
	var fail atomic.Bool
	runtimeInstance, err := New(WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
		hostCalls.Add(1)
		if fail.Load() {
			return String("discard this too"), hostErr
		}
		return String("discard this"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, name := range []Value{String("missing"), String(""), Null()} {
		result, invokeErr := runtimeInstance.Invoke(
			context.Background(), "belevate_command", String("B"), name, String("raw"),
		)
		if invokeErr != nil || !result.IsNull() {
			t.Errorf("fallback for %s = (%s, %v), want $null/nil", name.Describe(), result.Describe(), invokeErr)
		}
	}
	if hostCalls.Load() != 3 {
		t.Fatalf("Host calls = %d, want 3", hostCalls.Load())
	}
	fail.Store(true)
	result, err := runtimeInstance.Invoke(
		context.Background(), "bremote_exec",
		String("B"), String("missing"), String("target"), String("raw"),
	)
	if !errors.Is(err, hostErr) || !result.IsNull() {
		t.Fatalf("Host error fallback = (%s, %v), want $null/%v", result.Describe(), err, hostErr)
	}
}

func TestAggressorBeaconTechniqueActionHostBoundaryErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			var hostCalls atomic.Int32
			runtimeInstance, err := New(WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
				hostCalls.Add(1)
				return String("discarded Host partial result"), boundaryErr
			})))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(
				context.Background(), "belevate_command",
				String("B"), String("missing"), String("raw"),
			)
			if !errors.Is(err, boundaryErr) || !result.IsNull() {
				t.Fatalf("Invoke = (%s, %v), want null/%v", result.Describe(), err, boundaryErr)
			}
			_, err = runtimeInstance.Eval(context.Background(), "technique-boundary-error.cna", `belevate_command("B", "missing", "raw");`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("script error = %v, want authoritative %v", err, boundaryErr)
			}
			if hostCalls.Load() != 2 {
				t.Fatalf("Host calls = %d, want two", hostCalls.Load())
			}
		})
	}
}

func TestAggressorBeaconTechniqueActionLocalErrorsRemainNative(t *testing.T) {
	var hostCalls atomic.Int32
	callback := techniqueTestCallable(func(context.Context, ...Value) (Value, error) {
		return String("discarded callback partial result"), ErrUnsafeArrayView
	})
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithInitialGlobals(map[string]Value{"unsafe_callback": FunctionValue(callback)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-native-error.cna", `
beacon_exploit_register("unsafe", "unsafe", $unsafe_callback);
`)

	result, err := runtimeInstance.Invoke(
		context.Background(), "belevate",
		String("B"), String("unsafe"), String("listener"),
	)
	if err != nil || !result.IsNull() {
		t.Fatalf("local native error translation = (%s, %v), want null/nil", result.Describe(), err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("local callback error reached Host %d time(s)", hostCalls.Load())
	}
}

func TestAggressorBeaconTechniqueActionCaughtBoundaryErrorDoesNotReclassifyLaterLocalError(t *testing.T) {
	var runtimeInstance *Runtime
	var callbackCalls atomic.Int32
	var hostCalls atomic.Int32
	callback := techniqueTestCallable(func(ctx context.Context, _ ...Value) (Value, error) {
		if callbackCalls.Add(1) == 1 {
			_, err := runtimeInstance.Invoke(ctx, "host_fail")
			if !errors.Is(err, ErrReadOnlyArray) {
				return Null(), fmt.Errorf("nested Host error = %v, want ErrReadOnlyArray", err)
			}
			// The importer deliberately handles this boundary error locally.
			return Null(), nil
		}
		// This is a distinct local/native error and must retain native warning
		// translation even though the earlier Host error used the same outer call.
		return Null(), ErrUnsafeArrayView
	})
	var err error
	runtimeInstance, err = New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), ErrReadOnlyArray
		})),
		WithInitialGlobals(map[string]Value{"mixed_error_callback": FunctionValue(callback)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-mixed-errors.cna", `
beacon_exploit_register("mixed-errors", "mixed-errors", $mixed_error_callback);
`)

	result, err := runtimeInstance.Invoke(
		context.Background(), "belevate",
		ArrayValue(NewArray(String("one"), String("two"))), String("mixed-errors"), String("listener"),
	)
	if err != nil || !result.IsNull() {
		t.Fatalf("later local error translation = (%s, %v), want null/nil", result.Describe(), err)
	}
	if callbackCalls.Load() != 2 || hostCalls.Load() != 1 {
		t.Fatalf("callback/Host calls = %d/%d, want 2/1", callbackCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorBeaconTechniqueActionUnloadRevokesSelectedCallbackWithoutHostFallback(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	var startOnce sync.Once
	var hostCalls atomic.Int32
	callback := techniqueTestCallable(func(ctx context.Context, _ ...Value) (Value, error) {
		startOnce.Do(func() { close(started) })
		<-ctx.Done()
		return Null(), ctx.Err()
	})
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithInitialGlobals(map[string]Value{"blocking_callback": FunctionValue(callback)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-unload-action.cna", `
beacon_remote_exec_method_register("blocked", "blocked", $blocking_callback);
`)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dispatchDone := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.Invoke(
			ctx, "bremote_exec", String("B"), String("blocked"), String("target"), String("raw"),
		)
		dispatchDone <- invokeErr
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("local callback did not start")
	}
	unloadDone := make(chan error, 1)
	go func() { unloadDone <- owner.Unload(ctx) }()

	select {
	case invokeErr := <-dispatchDone:
		if invokeErr == nil {
			t.Fatal("selected callback succeeded after unload admission")
		}
	case <-ctx.Done():
		t.Fatal("dispatch did not stop after unload admission")
	}
	select {
	case unloadErr := <-unloadDone:
		if unloadErr != nil {
			t.Fatalf("Unload: %v", unloadErr)
		}
	case <-ctx.Done():
		t.Fatal("Unload did not finish")
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("Host calls after local selection = %d, want 0", hostCalls.Load())
	}
}

func TestAggressorBeaconTechniqueActionsStrictArityAndFunctionOverride(t *testing.T) {
	t.Parallel()

	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
		hostCalls.Add(1)
		return Null(), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	tests := []struct {
		name  string
		arity int
	}{
		{"belevate_command", 3},
		{"belevate", 3},
		{"bremote_exec", 4},
		{"bjump", 4},
	}
	for _, test := range tests {
		for _, received := range []int{test.arity - 1, test.arity + 1} {
			arguments := make([]Value, received)
			for index := range arguments {
				arguments[index] = String("x")
			}
			_, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, arguments...)
			if invokeErr == nil || !strings.Contains(invokeErr.Error(), "expected exactly") {
				t.Errorf("%s arity %d error = %v, want exact-arity rejection", test.name, received, invokeErr)
			}
		}
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("Host calls after arity rejection = %d, want 0", hostCalls.Load())
	}

	var overrideCalls atomic.Int32
	overridden, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithFunction("belevate", func(_ context.Context, invocation Invocation) (Value, error) {
			overrideCalls.Add(1)
			return invocation.Arg(0), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = overridden.Close(context.Background()) })
	result, err := overridden.Invoke(
		context.Background(), "belevate", String("override"), String("missing"), String("listener"),
	)
	if err != nil || result.String() != "override" || overrideCalls.Load() != 1 {
		t.Fatalf("override = (%s, %v), calls %d", result.Describe(), err, overrideCalls.Load())
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("Host calls after override = %d, want 0", hostCalls.Load())
	}
}

func TestAggressorBeaconTechniqueActionMetersNativeRecursiveReentry(t *testing.T) {
	t.Parallel()

	const instructionLimit = 100
	var runtimeInstance *Runtime
	var owner *Script
	var calls atomic.Int32
	callback := techniqueTestCallable(func(ctx context.Context, _ ...Value) (Value, error) {
		if _, err := owner.Call(ctx, "small_burn"); err != nil {
			return Null(), err
		}
		if calls.Add(1) < 512 {
			return runtimeInstance.Invoke(
				ctx, "belevate", String("B"), String("meter"), String("listener"),
			)
		}
		return Null(), nil
	})
	var err error
	runtimeInstance, err = New(
		WithInstructionLimit(instructionLimit),
		WithInitialGlobals(map[string]Value{"meter_callback": FunctionValue(callback)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner = loadAggressorBeaconTechniqueScript(t, runtimeInstance, "technique-action-meter.cna", `
sub small_burn { $x = 1; $x++; return $x; }
beacon_exploit_register("meter", "meter", $meter_callback);
`)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = runtimeInstance.Invoke(
		ctx, "belevate", String("B"), String("meter"), String("listener"),
	)
	assertTechniqueInstructionLimit(t, err, instructionLimit)
	if calls.Load() < 2 {
		t.Fatalf("native callback calls = %d, want recursive reentry", calls.Load())
	}
}

func valueSlicesIdentityEqual(left, right []Value) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !left[index].IdentityEqual(right[index]) {
			return false
		}
	}
	return true
}

func describeTechniqueActionValues(values []Value) []string {
	described := make([]string, len(values))
	for index, value := range values {
		described[index] = value.Describe()
	}
	return described
}
