package opfor

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBeaconInlineExecuteAppliesHookAndForwardsGuardedCallback(t *testing.T) {
	t.Parallel()

	var received Invocation
	var retained Callable
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		received = invocation
		if invocation.Arguments[4].Reference != nil {
			return Null(), errors.New("host callback retained the caller's mutable reference")
		}
		callback, ok := invocation.Arg(4).Function()
		if !ok {
			return Null(), errors.New("host callback argument is not callable")
		}
		retained = callback
		return String("private task metadata"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("inline-bof.cna", `
set BEACON_INLINE_EXECUTE {
    if ($2 !is $null) { return "unexpected-name"; }
    return "hooked:" . $1;
}

$callback = {
    return $1 . ":" . $2 . ":" . $3["type"];
};
$result = beacon_inline_execute("beacon-7", "original", "go", "arguments", $callback);
$callback = { return "replacement"; };
if ($result !is $null) { warn("unexpected synchronous result"); }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if received.Name != "beacon_inline_execute" || received.Script != script.ID() || received.Runtime != runtimeInstance {
		t.Fatalf("forwarded invocation = name:%q script:%d runtime:%p", received.Name, received.Script, received.Runtime)
	}
	if got, want := received.Arg(0).String(), "beacon-7"; got != want {
		t.Fatalf("beacon ID = %q, want %q", got, want)
	}
	bof, ok := received.Arg(1).Bytes()
	if !ok || !received.Arg(1).IsBinaryString() || !bytes.Equal(bof, []byte("hooked:original")) {
		t.Fatalf("hooked BOF = %x/binary=%v, want %x/binary", bof, received.Arg(1).IsBinaryString(), []byte("hooked:original"))
	}
	if got, want := received.Arg(2).String(), "go"; got != want {
		t.Fatalf("entry point = %q, want %q", got, want)
	}
	if got, want := received.Arg(3).String(), "arguments"; got != want {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
	if retained == nil {
		t.Fatal("host did not receive the callback")
	}
	information := NewOrderedHash()
	information.Set("type", String("output"))
	callbackResult, err := retained.Invoke(
		context.Background(), String("beacon-7"), String("output"), HashValue(information),
	)
	if err != nil {
		t.Fatalf("retained callback: %v", err)
	}
	if got, want := callbackResult.String(), "beacon-7:output:output"; got != want {
		t.Fatalf("retained callback = %q, want %q", got, want)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if _, err := retained.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("callback after unload error = %v, want ErrScriptUnloaded", err)
	}
}

func TestBeaconInlineExecuteNullHookPreservesBOFAndDiscardsHostResult(t *testing.T) {
	t.Parallel()

	var received Invocation
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		received = invocation
		return String("not script-visible"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("inline-bof-null-hook.cna", `
set BEACON_INLINE_EXECUTE { return $null; }
$callback = $null;
return beacon_inline_execute("beacon", "same", "go", "", $callback);
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsNull() {
		t.Fatalf("result = %s, want $null", result.Describe())
	}
	if len(received.Arguments) != 5 || received.Arguments[4].Reference != nil ||
		!received.Arg(4).IsNull() || received.Arguments[4].Set(String("must-not-mutate")) {
		t.Fatalf("null callback forwarding retained a mutable reference: %#v", received.Arguments)
	}
	got, ok := received.Arg(1).Bytes()
	if !ok || !received.Arg(1).IsBinaryString() || !bytes.Equal(got, []byte("same")) {
		t.Fatalf("forwarded BOF = %x/binary=%v, want 73616d65/binary", got, received.Arg(1).IsBinaryString())
	}
}

func TestBeaconInlineExecuteValidationCancellationAndHostError(t *testing.T) {
	t.Parallel()

	hostErr := errors.New("tasking rejected")
	hostCalls := 0
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, _ Invocation) (Value, error) {
		hostCalls++
		return Null(), hostErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "beacon_inline_execute", String("too-few")); err == nil || !strings.Contains(err.Error(), "expected 4 or 5 arguments") {
		t.Fatalf("arity error = %v", err)
	}
	if hostCalls != 0 {
		t.Fatalf("host calls after arity error = %d, want 0", hostCalls)
	}
	if _, err := runtimeInstance.Invoke(
		context.Background(), "beacon_inline_execute",
		String("beacon"), String("bof"), String("go"), String(""), String("not callable"),
	); !errors.Is(err, ErrInvalidCallable) {
		t.Fatalf("callback error = %v, want ErrInvalidCallable", err)
	}
	if hostCalls != 0 {
		t.Fatalf("host calls after callback error = %d, want 0", hostCalls)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtimeInstance.Invoke(canceled, "beacon_inline_execute", String("beacon"), String("bof"), String("go"), String("")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled call error = %v, want context.Canceled", err)
	}
	if hostCalls != 0 {
		t.Fatalf("host calls after cancellation = %d, want 0", hostCalls)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "beacon_inline_execute", String("beacon"), String("bof"), String("go"), String("")); !errors.Is(err, hostErr) {
		t.Fatalf("host error = %v, want %v", err, hostErr)
	}
	if hostCalls != 1 {
		t.Fatalf("host calls = %d, want 1", hostCalls)
	}
}

func TestBeaconInlineExecuteHostBoundaryErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			hostCalls := 0
			runtimeInstance, err := New(WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
				hostCalls++
				return String("discarded Host partial result"), boundaryErr
			})))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(
				context.Background(), "beacon_inline_execute",
				String("B"), String("bof"), String("go"), String(""),
			)
			if !errors.Is(err, boundaryErr) || !result.IsNull() {
				t.Fatalf("Invoke = (%s, %v), want null/%v", result.Describe(), err, boundaryErr)
			}
			_, err = runtimeInstance.Eval(context.Background(), "inline-boundary-error.cna", `beacon_inline_execute("B", "bof", "go", "");`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("script error = %v, want authoritative %v", err, boundaryErr)
			}
			if hostCalls != 2 {
				t.Fatalf("Host calls = %d, want two", hostCalls)
			}
		})
	}
}

func TestBeaconInlineExecuteImporterFunctionOverrideWins(t *testing.T) {
	t.Parallel()

	hostCalls := 0
	overrideCalls := 0
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls++
			return Null(), nil
		})),
		WithFunction("beacon_inline_execute", func(_ context.Context, invocation Invocation) (Value, error) {
			overrideCalls++
			return invocation.Arg(0), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Invoke(context.Background(), "beacon_inline_execute", String("override"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.String(), "override"; got != want || overrideCalls != 1 || hostCalls != 0 {
		t.Fatalf("override = %q calls:%d host:%d, want override calls:1 host:0", got, overrideCalls, hostCalls)
	}
}
