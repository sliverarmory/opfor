package opfor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

const aggressorKeywordFunctionNamespaceProbe = `sub on {
    println("on:" . $1 . ":" . $2);
}

sub alias {
    println("alias:" . $1 . ":" . $2);
}

on("event", { return "event"; });
alias("name", { return "alias"; });
`

func TestAggressorFunctionFormOnAliasAndFireAlias(t *testing.T) {
	observer := &dynamicBindingObserver{}
	runtimeInstance, err := New(
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithBindingObserver(observer),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	program, err := CompileString("dynamic-bindings.cna", `
@event_seen = @();
@alias_seen = @();

$on_register_result = on("ready", {
    push(@event_seen, "function-one:" . $1);
    return "function-one";
});
on ready {
    push(@event_seen, "declaration:" . $1);
    return "declaration";
}
on("ready", {
    push(@event_seen, "function-two:" . $1);
    return "function-two";
});

$alias_register_result = alias("echo", {
    @alias_seen = @($0, $1, $2, $3);
    return "callback-result-is-not-fireAlias-result";
});
$fire_result = fireAlias("beacon-7", "echo", '"two words" tail');
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !script.Get("$fire_result").IsNull() {
		t.Fatalf("fireAlias result = %s, want $null", script.Get("$fire_result").Describe())
	}
	if !script.Get("$on_register_result").IsNull() || !script.Get("$alias_register_result").IsNull() {
		t.Fatalf("registration results = (%s, %s), want ($null, $null)",
			script.Get("$on_register_result").Describe(), script.Get("$alias_register_result").Describe())
	}
	assertDynamicBindingStrings(t, script.Get("@alias_seen"), []string{
		`echo "two words" tail`, "beacon-7", "two words", "tail",
	})

	events := runtimeInstance.Bindings(BindingEvent, "ready")
	if len(events) != 3 {
		t.Fatalf("ready binding count = %d, want 3", len(events))
	}
	for index, binding := range events {
		if binding.Script != script.ID() || binding.ID != uint64(index+1) || binding.Keyword != "on" ||
			binding.Environment != EnvironmentOrdinary || binding.Name != "ready" || len(binding.Selectors) != 1 ||
			!binding.Selectors[0].Evaluated || binding.Selectors[0].Value.String() != "ready" {
			t.Fatalf("ready binding[%d] metadata = %#v", index, binding)
		}
	}
	aliases := runtimeInstance.Bindings(BindingAlias, "echo")
	if len(aliases) != 1 || aliases[0].Script != script.ID() || aliases[0].ID != 4 || aliases[0].Keyword != "alias" {
		t.Fatalf("echo bindings = %#v", aliases)
	}

	results, err := runtimeInstance.DispatchEvent(context.Background(), "ready", String("payload"))
	if err != nil {
		t.Fatalf("DispatchEvent: %v", err)
	}
	if got := dynamicBindingValueStrings(results); !reflect.DeepEqual(got, []string{"function-one", "declaration", "function-two"}) {
		t.Fatalf("event results = %q", got)
	}
	assertDynamicBindingStrings(t, script.Get("@event_seen"), []string{
		"function-one:payload", "declaration:payload", "function-two:payload",
	})

	registered, unregistered := observer.snapshot()
	if got := dynamicBindingIdentities(registered); !reflect.DeepEqual(got, []string{"on:ready", "on:ready", "on:ready", "alias:echo"}) {
		t.Fatalf("Registered notifications = %q", got)
	}
	if len(unregistered) != 0 {
		t.Fatalf("premature Unregistered notifications = %#v", unregistered)
	}

	retained := aliases[0].Callback
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if got := runtimeInstance.Bindings(BindingEvent, "ready"); len(got) != 0 {
		t.Fatalf("ready bindings after unload = %#v", got)
	}
	if got := runtimeInstance.Bindings(BindingAlias, "echo"); len(got) != 0 {
		t.Fatalf("echo bindings after unload = %#v", got)
	}
	if _, err := retained.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("retained dynamic callback after unload error = %v, want ErrScriptUnloaded", err)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "fireAlias",
		String("beacon-7"), String("echo"), String("tail")); err == nil {
		t.Fatal("fireAlias unexpectedly found an unloaded dynamic alias")
	} else {
		var unsupported *UnsupportedError
		if !errors.As(err, &unsupported) || unsupported.Operation != "alias" || unsupported.Name != "echo" {
			t.Fatalf("fireAlias after unload error = %v, want unsupported alias echo", err)
		}
	}
	registered, unregistered = observer.snapshot()
	if got := dynamicBindingIdentities(unregistered); !reflect.DeepEqual(got, []string{"alias:echo", "on:ready", "on:ready", "on:ready"}) {
		t.Fatalf("Unregistered notifications = %q", got)
	}
}

func TestAggressorDynamicBindingsDuplicateNamesUseExistingRegistrySemantics(t *testing.T) {
	runtimeInstance, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("dynamic-duplicates.cna", `
@seen = @();
alias("duplicate", { push(@seen, "first"); return "first"; });
alias("duplicate", { push(@seen, "second"); return "second"; });
on("duplicate", { return "event-first"; });
on("duplicate", { return "event-second"; });
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	aliases := runtimeInstance.Bindings(BindingAlias, "duplicate")
	if len(aliases) != 2 || aliases[0].ID != 1 || aliases[1].ID != 2 {
		t.Fatalf("duplicate aliases = %#v", aliases)
	}
	value, err := runtimeInstance.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingAlias, Name: "duplicate", RawInput: "duplicate", SessionID: String("beacon"),
	})
	if err != nil || value.String() != "second" {
		t.Fatalf("InvokeConsole duplicate = (%s, %v), want second", value.Describe(), err)
	}
	assertDynamicBindingStrings(t, script.Get("@seen"), []string{"second"})

	results, err := runtimeInstance.DispatchEvent(context.Background(), "duplicate")
	if err != nil {
		t.Fatal(err)
	}
	if got := dynamicBindingValueStrings(results); !reflect.DeepEqual(got, []string{"event-first", "event-second"}) {
		t.Fatalf("duplicate event results = %q", got)
	}
}

type dynamicNativeCallable struct {
	mu     sync.Mutex
	values []Value
}

func (callable *dynamicNativeCallable) Invoke(_ context.Context, values ...Value) (Value, error) {
	callable.mu.Lock()
	callable.values = append([]Value(nil), values...)
	callable.mu.Unlock()
	return String("native-result"), nil
}

func (callable *dynamicNativeCallable) snapshot() []Value {
	callable.mu.Lock()
	defer callable.mu.Unlock()
	return append([]Value(nil), callable.values...)
}

func TestAggressorDynamicAliasRetainsNonScriptCallable(t *testing.T) {
	native := &dynamicNativeCallable{}
	runtimeInstance, err := New(
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithFunction("dynamic_native_callback", func(context.Context, Invocation) (Value, error) {
			return FunctionValue(native), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("dynamic-native.cna", `alias("native", dynamic_native_callback());`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	binding := runtimeInstance.Bindings(BindingAlias, "native")[0]
	value, err := runtimeInstance.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingAlias, Name: "native", RawInput: "native argument", SessionID: String("beacon-9"),
	})
	if err != nil || value.String() != "native-result" {
		t.Fatalf("InvokeConsole(native) = (%s, %v)", value.Describe(), err)
	}
	if got := dynamicBindingValueStrings(native.snapshot()); !reflect.DeepEqual(got, []string{"beacon-9", "argument"}) {
		t.Fatalf("native callable values = %q, want positional alias values", got)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.Callback.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("native callback after unload error = %v, want ErrScriptUnloaded", err)
	}
}

func TestAggressorDynamicFunctionImporterOverridesTakePrecedence(t *testing.T) {
	seen := make([]string, 0, 3)
	var mu sync.Mutex
	override := func(ctx context.Context, invocation Invocation) (Value, error) {
		mu.Lock()
		seen = append(seen, invocation.Name)
		mu.Unlock()
		return String("override-" + invocation.Name), ctx.Err()
	}
	runtimeInstance, err := New(
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithFunction("on", override),
		WithFunction("alias", override),
		WithFunction("fireAlias", override),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("dynamic-overrides.cna", `
$on_result = on("event", $null);
$alias_result = alias("name", $null);
$fire_result = fireAlias("beacon", "name", "tail");
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"$on_result": "override-on", "$alias_result": "override-alias", "$fire_result": "override-fireAlias",
	} {
		if got := script.Get(name).String(); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	mu.Lock()
	gotSeen := append([]string(nil), seen...)
	mu.Unlock()
	if !reflect.DeepEqual(gotSeen, []string{"on", "alias", "fireAlias"}) {
		t.Fatalf("override calls = %q", gotSeen)
	}
	if len(runtimeInstance.Bindings(BindingEvent, "")) != 0 || len(runtimeInstance.Bindings(BindingAlias, "")) != 0 {
		t.Fatal("portable dynamic registration ran despite importer overrides")
	}
}

type blockingDynamicCallable struct {
	started  chan struct{}
	canceled chan struct{}
}

func (callable *blockingDynamicCallable) Invoke(ctx context.Context, _ ...Value) (Value, error) {
	close(callable.started)
	<-ctx.Done()
	close(callable.canceled)
	return Null(), ctx.Err()
}

func TestAggressorDynamicAliasUnloadCancelsAdmittedCallback(t *testing.T) {
	blocking := &blockingDynamicCallable{started: make(chan struct{}), canceled: make(chan struct{})}
	runtimeInstance, err := New(
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithFunction("blocking_dynamic_callback", func(context.Context, Invocation) (Value, error) {
			return FunctionValue(blocking), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("dynamic-blocking.cna", `alias("slow", blocking_dynamic_callback());`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	invokeDone := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.InvokeConsole(context.Background(), ConsoleInvocation{
			Kind: BindingAlias, Name: "slow", RawInput: "slow", SessionID: String("beacon"),
		})
		invokeDone <- invokeErr
	}()
	dynamicBindingAwait(t, blocking.started, "dynamic alias callback start")
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	dynamicBindingAwait(t, blocking.canceled, "dynamic alias callback cancellation")
	select {
	case err := <-invokeDone:
		if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrScriptUnloaded) {
			t.Fatalf("InvokeConsole cancellation error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled dynamic alias invocation")
	}
	if got := runtimeInstance.Bindings(BindingAlias, "slow"); len(got) != 0 {
		t.Fatalf("slow bindings after unload = %#v", got)
	}
}

func TestAggressorDynamicBindingArgumentErrors(t *testing.T) {
	runtimeInstance, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("dynamic-errors.cna", `on("event", "not callable");`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); !errors.Is(err, ErrInvalidCallable) {
		t.Fatalf("non-callable on error = %v, want ErrInvalidCallable", err)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "fireAlias", String("beacon"), String("name")); err == nil {
		t.Fatal("fireAlias with a missing argument succeeded")
	}
}

// The official Sleep JAR does not provide Aggressor's on/alias bridges, but it
// does establish the namespace rule they rely on: keyword spellings followed
// by '(' are ordinary function calls and accept closure-valued arguments.
func TestAggressorKeywordFunctionNamespaceOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	path := filepath.Join(t.TempDir(), "keyword-functions.sl")
	if err := os.WriteFile(path, []byte(aggressorKeywordFunctionNamespaceProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep namespace probe: %v\n%s", err, want)
	}
	var got bytes.Buffer
	runtimeInstance, err := New(WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString(path, aggressorKeywordFunctionNamespaceProbe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("keyword/function namespace mismatch\nofficial:\n%s\nopfor:\n%s", want, got.Bytes())
	}
}

type dynamicBindingObserver struct {
	mu           sync.Mutex
	registered   []Binding
	unregistered []Binding
}

type boundaryBindingObserver struct {
	registeredErr   error
	unregisteredErr error
}

func (observer *boundaryBindingObserver) Registered(context.Context, Binding) error {
	return observer.registeredErr
}

func (observer *boundaryBindingObserver) Unregistered(context.Context, Binding) error {
	return observer.unregisteredErr
}

func TestAggressorDynamicBindingObserverErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run("register/"+boundaryErr.Error(), func(t *testing.T) {
			observer := &boundaryBindingObserver{registeredErr: boundaryErr}
			runtimeInstance, err := New(WithBindingObserver(observer))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			_, err = runtimeInstance.Eval(context.Background(), "binding-observer-register.cna", `alias("boundary", { return 1; });`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("registration error = %v, want authoritative %v", err, boundaryErr)
			}
			if bindings := runtimeInstance.Bindings(BindingAlias, "boundary"); len(bindings) != 0 {
				t.Fatalf("failed registration left bindings: %#v", bindings)
			}
		})

		t.Run("unregister/"+boundaryErr.Error(), func(t *testing.T) {
			observer := &boundaryBindingObserver{}
			runtimeInstance, err := New(WithBindingObserver(observer))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			if _, err := runtimeInstance.Eval(context.Background(), "binding-observer-setup.cna", `alias("boundary", { return 1; });`); err != nil {
				t.Fatal(err)
			}
			observer.unregisteredErr = boundaryErr

			_, err = runtimeInstance.Eval(context.Background(), "binding-observer-unregister.cna", `alias_clear("boundary");`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("unregistration error = %v, want authoritative %v", err, boundaryErr)
			}
			if bindings := runtimeInstance.Bindings(BindingAlias, "boundary"); len(bindings) != 0 {
				t.Fatalf("failed notification restored removed bindings: %#v", bindings)
			}
		})
	}
}

func (observer *dynamicBindingObserver) Registered(_ context.Context, binding Binding) error {
	observer.mu.Lock()
	observer.registered = append(observer.registered, binding)
	observer.mu.Unlock()
	return nil
}

func (observer *dynamicBindingObserver) Unregistered(_ context.Context, binding Binding) error {
	observer.mu.Lock()
	observer.unregistered = append(observer.unregistered, binding)
	observer.mu.Unlock()
	return nil
}

func (observer *dynamicBindingObserver) snapshot() ([]Binding, []Binding) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return append([]Binding(nil), observer.registered...), append([]Binding(nil), observer.unregistered...)
}

func dynamicBindingIdentities(bindings []Binding) []string {
	result := make([]string, len(bindings))
	for index, binding := range bindings {
		result[index] = string(binding.Kind) + ":" + binding.Name
	}
	return result
}

func dynamicBindingValueStrings(values []Value) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func assertDynamicBindingStrings(t *testing.T, value Value, want []string) {
	t.Helper()
	array, ok := value.Array()
	if !ok {
		t.Fatalf("value = %s, want array", value.Describe())
	}
	if got := dynamicBindingValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("array values = %q, want %q", got, want)
	}
}

func dynamicBindingAwait(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
	}
}
