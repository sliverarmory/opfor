package opfor

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sliverarmory/opfor/internal/bytecode"
)

type recordingAggressorBreakpointProvider struct {
	mu        sync.Mutex
	snapshots []AggressorBreakpointSnapshot
	handle    func(context.Context, AggressorBreakpointSnapshot) error
}

func (provider *recordingAggressorBreakpointProvider) HandleAggressorBreakpoint(
	ctx context.Context,
	snapshot AggressorBreakpointSnapshot,
) error {
	provider.mu.Lock()
	provider.snapshots = append(provider.snapshots, snapshot.Clone())
	handle := provider.handle
	provider.mu.Unlock()
	if handle == nil {
		return nil
	}
	return handle(ctx, snapshot)
}

func (provider *recordingAggressorBreakpointProvider) snapshot() []AggressorBreakpointSnapshot {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	result := make([]AggressorBreakpointSnapshot, len(provider.snapshots))
	for index, snapshot := range provider.snapshots {
		result[index] = snapshot.Clone()
	}
	return result
}

func TestAggressorBreakpointCapturesNestedInterpreterStateAndDetachesProvider(t *testing.T) {
	fixed := time.Date(2026, time.August, 24, 12, 34, 56, 789_000_000, time.FixedZone("test", -7*60*60))
	var output bytes.Buffer
	provider := &recordingAggressorBreakpointProvider{}
	provider.handle = func(_ context.Context, snapshot AggressorBreakpointSnapshot) error {
		shared, ok := snapshot.GlobalVariables["@shared"].Array()
		if !ok || shared.Set(0, String("provider-mutated")) != nil {
			t.Error("provider did not receive a mutable detached @shared array")
		}
		snapshot.LocalVariables["$provider_only"] = String("mutated")
		return nil
	}
	runtimeInstance, err := New(
		WithStdout(&output),
		WithClock(ClockFunc(func() time.Time { return fixed })),
		WithAggressorBreakpointProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Eval(context.Background(), "breakpoint-nested.sl", `
$global_value = 'global';
@shared = @('original');
sub outer {
    this('$state');
    local('$outer $callback');
    $state = 'this-state';
    $outer = 'outer-local';
    $callback = lambda({
        local('$inner');
        $inner = 'inner-local';
        return brk();
    }, $outer => $outer, $state => $state);
    return [$callback];
}
return outer();
`)
	if err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("configured provider also received headless output %q", output.String())
	}

	snapshots := provider.snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("provider snapshots = %d, want one", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.RuntimeID != runtimeInstance.ID() || snapshot.Script == 0 {
		t.Fatalf("snapshot provenance = runtime %d script %d", snapshot.RuntimeID, snapshot.Script)
	}
	if snapshot.ScriptName != "breakpoint-nested.sl" || snapshot.SourceLocation.Source != "breakpoint-nested.sl" || snapshot.SourceLocation.Start.Line <= 0 {
		t.Fatalf("snapshot source = %q %#v", snapshot.ScriptName, snapshot.SourceLocation)
	}
	if !snapshot.Timestamp.Equal(fixed) {
		t.Fatalf("snapshot timestamp = %s, want %s", snapshot.Timestamp, fixed)
	}
	if got := snapshot.LocalVariables["$inner"].String(); got != "inner-local" {
		t.Errorf("inner local = %q, want inner-local", got)
	}
	if got := snapshot.GlobalVariables["$global_value"].String(); got != "global" {
		t.Errorf("global = %q, want global", got)
	}
	if got := snapshot.ClosureVariables["$outer"].String(); got != "outer-local" {
		t.Errorf("captured outer local = %q, want outer-local", got)
	}
	if got := snapshot.ClosureVariables["$state"].String(); got != "this-state" {
		t.Errorf("captured this variable = %q, want this-state", got)
	}
	if snapshot.CurrentFunction != "<closure>" {
		t.Errorf("current function = %q, want <closure>", snapshot.CurrentFunction)
	}
	if want := []string{"<closure>", "outer", "<main>"}; !reflect.DeepEqual(snapshot.CallStack, want) {
		t.Errorf("call stack = %q, want %q", snapshot.CallStack, want)
	}
	if len(snapshot.StackFrames) != 3 || snapshot.StackFrames[0].Function != "<closure>" || snapshot.StackFrames[1].LocalVariables["$outer"].String() != "outer-local" {
		t.Errorf("stack frames = %#v", snapshot.StackFrames)
	}

	returned := breakpointTestHash(t, result, "result")
	if got := breakpointTestHashValue(t, returned, "current_function").String(); got != "<closure>" {
		t.Errorf("returned current_function = %q", got)
	}
	if got := breakpointTestHashValue(t, returned, "source_location").String(); got != "breakpoint-nested.sl:12" {
		t.Errorf("returned source_location = %q, want breakpoint-nested.sl:12", got)
	}
	if got := breakpointTestHashValue(t, returned, "timestamp").Int64(); got != fixed.UnixMilli() {
		t.Errorf("returned timestamp = %d, want %d", got, fixed.UnixMilli())
	}
	returnedGlobals := breakpointTestHash(t, breakpointTestHashValue(t, returned, "global_variables"), "global_variables")
	returnedShared, ok := breakpointTestHashValue(t, returnedGlobals, "@shared").Array()
	if !ok {
		t.Fatal("returned @shared is not an array")
	}
	if got, _ := returnedShared.Get(0); got.String() != "original" {
		t.Fatalf("provider mutation reached returned snapshot: %s", got.Describe())
	}
	if err := returnedShared.Set(0, String("returned-mutated")); err != nil {
		t.Fatal(err)
	}
	live, err := runtimeInstance.Eval(context.Background(), "breakpoint-live-check.sl", `return @shared[0];`)
	if err != nil || live.String() != "original" {
		t.Fatalf("snapshot mutation reached live script state = (%s, %v)", live.Describe(), err)
	}
}

func TestAggressorBreakpointHeadlessOutputAndNoHostFallback(t *testing.T) {
	var output bytes.Buffer
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithStdout(&output),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("brk reached Host")
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Eval(context.Background(), "breakpoint-headless.sl", `return brk();`)
	if err != nil {
		t.Fatal(err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("brk reached Host %d time(s)", hostCalls.Load())
	}
	if got, want := output.String(), result.Describe()+"\n"; got != want {
		t.Fatalf("headless output\ngot:  %q\nwant: %q", got, want)
	}
	resultHash := breakpointTestHash(t, result, "headless result")
	for _, key := range []string{
		"script_name", "source_location", "timestamp", "local_variables",
		"global_variables", "closure_variables", "stack_frames", "call_stack", "current_function",
	} {
		if _, ok := resultHash.Get(key); !ok {
			t.Errorf("headless snapshot lacks %q", key)
		}
	}
}

func TestAggressorBreakpointProviderErrorVariableErrorArityAndPrecedence(t *testing.T) {
	t.Run("provider error", func(t *testing.T) {
		sentinel := errors.New("breakpoint presentation failed")
		provider := &recordingAggressorBreakpointProvider{handle: func(context.Context, AggressorBreakpointSnapshot) error {
			return sentinel
		}}
		runtimeInstance, err := New(WithAggressorBreakpointProvider(provider))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		program, err := CompileString("breakpoint-provider-error.sl", `sub pause { return brk(); }`)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		value, err := script.Call(context.Background(), "pause")
		if !errors.Is(err, sentinel) || !value.IsNull() {
			t.Fatalf("provider error = (%s, %v)", value.Describe(), err)
		}
	})

	t.Run("variable provider error", func(t *testing.T) {
		sentinel := errors.New("breakpoint variable read failed")
		variables := newVariableProviderTestProvider()
		breakpoints := &recordingAggressorBreakpointProvider{}
		runtimeInstance, err := New(
			WithVariableProvider(variables),
			WithAggressorBreakpointProvider(breakpoints),
			WithFunction("arm_breakpoint_error", func(context.Context, Invocation) (Value, error) {
				variables.mu.Lock()
				variables.operationErr[variableProviderTestErrorKey(VariableProviderExists, "$probe")] = sentinel
				variables.mu.Unlock()
				return Null(), nil
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		program, err := CompileString("breakpoint-variable-error.sl", `
sub pause {
    local('$probe');
    $probe = 'value';
    arm_breakpoint_error();
    return brk();
}
`)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		value, err := script.Call(context.Background(), "pause")
		var providerErr *VariableProviderError
		if !errors.Is(err, sentinel) || !errors.As(err, &providerErr) || providerErr.Operation != VariableProviderExists || providerErr.Name != "$probe" || !value.IsNull() {
			t.Fatalf("variable snapshot error = (%s, %#v)", value.Describe(), err)
		}
		if len(breakpoints.snapshot()) != 0 {
			t.Fatal("partial snapshot reached breakpoint provider")
		}
	})

	t.Run("arity and active execution", func(t *testing.T) {
		runtimeInstance, err := New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		value, err := runtimeInstance.Invoke(context.Background(), "brk", Int(1))
		if err == nil || !value.IsNull() || !strings.Contains(err.Error(), "expected exactly 0 argument(s), received 1") {
			t.Fatalf("invalid brk arity = (%s, %v)", value.Describe(), err)
		}
		value, err = runtimeInstance.Invoke(context.Background(), "brk")
		if err == nil || !value.IsNull() || !strings.Contains(err.Error(), "requires active script execution") {
			t.Fatalf("out-of-script brk = (%s, %v)", value.Describe(), err)
		}
	})

	t.Run("WithFunction precedence", func(t *testing.T) {
		for _, overrideFirst := range []bool{false, true} {
			var calls atomic.Int32
			provider := &recordingAggressorBreakpointProvider{}
			override := WithFunction("brk", func(context.Context, Invocation) (Value, error) {
				calls.Add(1)
				return String("override"), nil
			})
			options := []Option{WithAggressorBreakpointProvider(provider), override}
			if overrideFirst {
				options = []Option{override, WithAggressorBreakpointProvider(provider)}
			}
			runtimeInstance, err := New(options...)
			if err != nil {
				t.Fatal(err)
			}
			value, err := runtimeInstance.Invoke(context.Background(), "brk", Int(1))
			if err != nil || value.String() != "override" || calls.Load() != 1 || len(provider.snapshot()) != 0 {
				t.Fatalf("override-first=%v = (%s, %v), override/provider calls %d/%d", overrideFirst, value.Describe(), err, calls.Load(), len(provider.snapshot()))
			}
			_ = runtimeInstance.Close(context.Background())
		}
	})
}

func TestAggressorBreakpointProviderCanBlockAndObservesCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "continue", true: "cancel"}[cancel], func(t *testing.T) {
			entered := make(chan struct{})
			continued := make(chan struct{})
			provider := AggressorBreakpointProviderFunc(func(ctx context.Context, _ AggressorBreakpointSnapshot) error {
				close(entered)
				select {
				case <-continued:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			runtimeInstance, err := New(WithAggressorBreakpointProvider(provider))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			program, err := CompileString("breakpoint-block.sl", `sub pause { return brk(); }`)
			if err != nil {
				t.Fatal(err)
			}
			script, err := runtimeInstance.Load(context.Background(), program)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancelCall := context.WithCancel(context.Background())
			defer cancelCall()
			type callResult struct {
				value Value
				err   error
			}
			finished := make(chan callResult, 1)
			go func() {
				value, callErr := script.Call(ctx, "pause")
				finished <- callResult{value: value, err: callErr}
			}()
			<-entered
			select {
			case got := <-finished:
				t.Fatalf("provider did not block: (%s, %v)", got.value.Describe(), got.err)
			default:
			}
			if cancel {
				cancelCall()
			} else {
				close(continued)
			}
			got := <-finished
			if cancel && (!errors.Is(got.err, context.Canceled) || !got.value.IsNull()) {
				t.Fatalf("canceled breakpoint = (%s, %v)", got.value.Describe(), got.err)
			}
			if !cancel && got.err != nil {
				t.Fatalf("continued breakpoint = (%s, %v)", got.value.Describe(), got.err)
			}
		})
	}
}

func TestAggressorBreakpointDoesNotTouchAccessOrderedHash(t *testing.T) {
	accessOrdered := NewAccessOrderedHash()
	accessOrdered.Set("a", String("one"))
	accessOrdered.Set("b", String("two"))
	accessOrdered.Set("c", String("three"))
	_, _ = accessOrdered.Get("a")
	before := breakpointTestHashKeys(accessOrdered)
	provider := &recordingAggressorBreakpointProvider{}

	runtimeInstance, err := New(
		WithInitialGlobals(map[string]Value{"%access": HashValue(accessOrdered)}),
		WithAggressorBreakpointProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "breakpoint-access-order.sl", `return brk();`); err != nil {
		t.Fatal(err)
	}
	after := breakpointTestHashKeys(accessOrdered)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("brk changed access-ordered hash traversal = %q, want %q", after, before)
	}
	snapshots := provider.snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("provider snapshots = %d, want one", len(snapshots))
	}
	detached, ok := snapshots[0].GlobalVariables["%access"].Hash()
	if !ok || detached == nil {
		t.Fatal("provider access-ordered hash snapshot is missing")
	}
	if got := breakpointTestHashKeys(detached); !reflect.DeepEqual(got, before) {
		t.Fatalf("detached access-ordered hash traversal = %q, want %q", got, before)
	}
	for key, want := range map[string]string{"a": "one", "b": "two", "c": "three"} {
		value, ok := accessOrdered.Get(key)
		if !ok || value.String() != want {
			t.Fatalf("access hash %q = (%s, %v), want %q", key, value.Describe(), ok, want)
		}
	}
}

func TestAggressorBreakpointProviderSupportsConcurrentExecutions(t *testing.T) {
	provider := &recordingAggressorBreakpointProvider{}
	runtimeInstance, err := New(WithAggressorBreakpointProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("breakpoint-concurrent.sl", `
sub pause {
    local('$value');
    $value = $1;
    return brk();
}
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	const executions = 8
	start := make(chan struct{})
	errorsByExecution := make(chan error, executions)
	var wait sync.WaitGroup
	for index := range executions {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			value, callErr := script.Call(context.Background(), "pause", Int(int32(index)))
			if callErr == nil {
				_, ok := value.Hash()
				if !ok {
					callErr = errors.New("brk did not return a hash")
				}
			}
			errorsByExecution <- callErr
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByExecution)
	for callErr := range errorsByExecution {
		if callErr != nil {
			t.Fatal(callErr)
		}
	}
	if got := len(provider.snapshot()); got != executions {
		t.Fatalf("provider snapshots = %d, want %d", got, executions)
	}
}

func TestPortableScriptLoaderInheritsAggressorBreakpointProvider(t *testing.T) {
	provider := &recordingAggressorBreakpointProvider{}
	runtimeInstance, err := New(WithAggressorBreakpointProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Eval(context.Background(), "breakpoint-parent.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: 'child-breakpoint.cna', 'return brk();', $null];
return [$child runScript];
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Hash(); !ok {
		t.Fatalf("child brk result = %s, want hash", result.Describe())
	}
	snapshots := provider.snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("child provider snapshots = %d, want one", len(snapshots))
	}
	if snapshot := snapshots[0]; snapshot.RuntimeID == 0 || snapshot.RuntimeID == runtimeInstance.ID() || snapshot.Script == 0 || snapshot.ScriptName != "child-breakpoint.cna" {
		t.Fatalf("child snapshot provenance = runtime %d script %d name %q; parent runtime %d", snapshot.RuntimeID, snapshot.Script, snapshot.ScriptName, runtimeInstance.ID())
	}
}

func TestAggressorBreakpointNilPolicyAndStableAnonymousLabel(t *testing.T) {
	var typedNil *recordingAggressorBreakpointProvider
	if _, err := New(WithAggressorBreakpointProvider(typedNil)); err == nil || !strings.Contains(err.Error(), "Aggressor breakpoint provider is nil") {
		t.Fatalf("typed-nil provider error = %v", err)
	}
	var nilFunction AggressorBreakpointProviderFunc
	if _, err := New(WithAggressorBreakpointProvider(nilFunction)); err == nil || !strings.Contains(err.Error(), "Aggressor breakpoint provider is nil") {
		t.Fatalf("nil provider function option error = %v", err)
	}
	if err := nilFunction.HandleAggressorBreakpoint(context.Background(), AggressorBreakpointSnapshot{}); err == nil {
		t.Fatal("direct nil breakpoint provider function call succeeded")
	}
	if got := breakpointFunctionName(&bytecode.Function{}); got != "<anonymous>" {
		t.Fatalf("empty function label = %q, want <anonymous>", got)
	}
	if got := breakpointFunctionName(&bytecode.Function{Name: "<main>"}); got != "<main>" {
		t.Fatalf("top-level function label = %q, want <main>", got)
	}
}

func breakpointTestHash(t *testing.T, value Value, label string) *Hash {
	t.Helper()
	hash, ok := value.Hash()
	if !ok || hash == nil {
		t.Fatalf("%s = %s, want hash", label, value.Describe())
	}
	return hash
}

func breakpointTestHashValue(t *testing.T, hash *Hash, key string) Value {
	t.Helper()
	value, ok := hash.Get(key)
	if !ok {
		t.Fatalf("hash lacks %q", key)
	}
	return value
}

func breakpointTestHashKeys(hash *Hash) []string {
	keys := hash.KeyValues()
	result := make([]string, len(keys))
	for index, key := range keys {
		result[index] = key.String()
	}
	return result
}
