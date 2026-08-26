package aggressor

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestBeaconInlineExecuteExposesGuardedCallbackThroughAggressorHost(t *testing.T) {
	t.Parallel()

	host := NewHost()
	var retained ScriptCallback
	if err := host.Register("beacon_inline_execute", func(_ context.Context, request Request) (Value, error) {
		if request.Name != "beacon_inline_execute" || request.ScriptID == 0 {
			return opfor.Null(), errors.New("missing inline-execute request identity")
		}
		bofArgument, ok := request.Arg(1)
		if !ok {
			return opfor.Null(), errors.New("missing BOF argument")
		}
		bof := bofArgument.Value()
		bofBytes, ok := bof.Bytes()
		if !ok || !bof.IsBinaryString() || !bytes.Equal(bofBytes, []byte("adapter:original")) {
			return opfor.Null(), errors.New("hooked BOF was not forwarded as a binary string")
		}
		callbackArgument, ok := request.Arg(4)
		if !ok {
			return opfor.Null(), errors.New("missing callback argument")
		}
		var callbackErr error
		retained, callbackErr = callbackArgument.Callback()
		return opfor.String("discarded host metadata"), callbackErr
	}); err != nil {
		t.Fatal(err)
	}
	runtimeInstance, err := opfor.New(opfor.WithHost(host))
	if err != nil {
		t.Fatal(err)
	}
	program, err := opfor.CompileString("aggressor-inline-adapter.cna", `
set BEACON_INLINE_EXECUTE { return "adapter:" . $1; }
$result = beacon_inline_execute("beacon", "original", "go", "", {
    return $1 . ":" . $2 . ":" . $3["type"];
});
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if !script.Get("$result").IsNull() || !retained.Valid() {
		t.Fatalf("inline result/callback = %s/%v, want $null/valid", script.Get("$result").Describe(), retained.Valid())
	}
	information := opfor.NewOrderedHash()
	information.Set("type", opfor.String("output"))
	result, err := retained.Invoke(
		context.Background(), opfor.String("beacon"), opfor.String("output"), opfor.HashValue(information),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.String(), "beacon:output:output"; got != want {
		t.Fatalf("inline callback = %q, want %q", got, want)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := retained.Invoke(context.Background()); !errors.Is(err, opfor.ErrScriptUnloaded) {
		t.Fatalf("inline callback after unload error = %v, want ErrScriptUnloaded", err)
	}
}

func TestScriptCallbackCapabilitySurvivesHostReturn(t *testing.T) {
	t.Parallel()

	host := NewHost()
	var retained ScriptCallback
	if err := host.Register("retain_callback", func(_ context.Context, request Request) (Value, error) {
		argument, ok := request.Arg(0)
		if !ok {
			return opfor.Null(), errors.New("missing callback argument")
		}
		var callbackErr error
		retained, callbackErr = argument.Callback()
		return opfor.Null(), callbackErr
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	runtime, err := opfor.New(opfor.WithHost(host))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := opfor.CompileString("aggressor-retained.cna", `
$callback = { return "callback:" . $1; };
retain_callback($callback);
$callback = { return "replacement"; };
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	if _, err := runtime.Load(context.Background(), program); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !retained.Valid() {
		t.Fatal("retained ScriptCallback is invalid")
	}

	value, err := retained.Invoke(context.Background(), opfor.String("later"))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := value.String(); got != "callback:later" {
		t.Fatalf("callback result = %q, want %q", got, "callback:later")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := retained.Invoke(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled callback error = %v, want context.Canceled", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := retained.Invoke(context.Background()); !errors.Is(err, opfor.ErrScriptUnloaded) {
		t.Fatalf("post-close callback error = %v, want ErrScriptUnloaded", err)
	}
}

func TestScriptCallbackCapabilityRejectsOrdinaryArgumentsAndZeroValue(t *testing.T) {
	t.Parallel()

	var zero ScriptCallback
	if zero.Valid() {
		t.Fatal("zero ScriptCallback is valid")
	}
	if _, err := zero.Invoke(context.Background()); !errors.Is(err, opfor.ErrInvalidCallable) {
		t.Fatalf("zero ScriptCallback error = %v, want ErrInvalidCallable", err)
	}

	host := NewHost()
	var callbackErr error
	if err := host.Register("inspect_argument", func(_ context.Context, request Request) (Value, error) {
		argument, ok := request.Arg(0)
		if !ok {
			return opfor.Null(), errors.New("missing ordinary argument")
		}
		_, callbackErr = argument.Callback()
		return opfor.Null(), nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	runtime, err := opfor.New(opfor.WithHost(host))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := opfor.CompileString("aggressor-invalid-callback.cna", `inspect_argument("ordinary");`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	if _, err := runtime.Load(context.Background(), program); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !errors.Is(callbackErr, opfor.ErrInvalidCallable) {
		t.Fatalf("ordinary Argument.Callback error = %v, want ErrInvalidCallable", callbackErr)
	}
}

func TestScriptlessHostCallStillExposesFunctionValue(t *testing.T) {
	t.Parallel()

	host := NewHost()
	var callbackErr error
	if err := host.Register("inspect_function", func(_ context.Context, request Request) (Value, error) {
		argument, ok := request.Arg(0)
		if !ok || argument.Value().Kind() != opfor.KindFunction {
			return opfor.Null(), errors.New("missing function value")
		}
		_, callbackErr = argument.Callback()
		return opfor.String("inspected"), nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	runtime, err := opfor.New(opfor.WithHost(host))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	function := opfor.FunctionValue(callbackFixture(func(_ context.Context, values ...opfor.Value) (opfor.Value, error) {
		return values[0], nil
	}))
	value, err := runtime.Invoke(context.Background(), "inspect_function", function)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := value.String(); got != "inspected" {
		t.Fatalf("scriptless host result = %q, want %q", got, "inspected")
	}
	if !errors.Is(callbackErr, opfor.ErrScriptUnloaded) {
		t.Fatalf("scriptless Argument.Callback error = %v, want ErrScriptUnloaded", callbackErr)
	}
}

type callbackFixture func(context.Context, ...opfor.Value) (opfor.Value, error)

func (fixture callbackFixture) Invoke(ctx context.Context, values ...opfor.Value) (opfor.Value, error) {
	return fixture(ctx, values...)
}
