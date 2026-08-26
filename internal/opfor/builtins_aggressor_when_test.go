package opfor

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type whenTestCallable func(context.Context, ...Value) (Value, error)

func (callable whenTestCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return callable(ctx, values...)
}

type whenTypedNilCallable struct{}

func (*whenTypedNilCallable) Invoke(context.Context, ...Value) (Value, error) {
	panic("typed-nil when callback was invoked")
}

// withWhenFunction keeps these focused core tests independent from the
// separate default-function inventory integration. Importer overrides use the
// same NativeFunc signature and the same registerWhen implementation.
func withWhenFunction() Option {
	return WithFunction("when", func(ctx context.Context, invocation Invocation) (Value, error) {
		return invocation.Runtime.registerWhen(ctx, invocation)
	})
}

func TestWhenFunctionAndDeclarationAreOneShotInRegistrationOrder(t *testing.T) {
	observer := &dynamicBindingObserver{}
	runtimeInstance, err := New(
		withWhenFunction(),
		WithBindingObserver(observer),
		WithFunction("active_when_count", func(_ context.Context, invocation Invocation) (Value, error) {
			count := int32(0)
			for _, binding := range invocation.Runtime.Bindings(BindingEvent, "ready") {
				if binding.Lifetime == BindingOnce {
					count++
				}
			}
			return Int(count), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	program, err := CompileString("when-order.cna", `
@seen = @();
$when_result = when("ready", {
    push(@seen, "function-one:" . $0 . ":" . $1 . ":" . @_[0] . ":" . active_when_count());
    when("ready", { push(@seen, "replacement:" . $0 . ":" . $1); return "replacement"; });
    return "function-one";
});
on ready {
    push(@seen, "persistent:" . $0 . ":" . $1);
    return "persistent";
}
when ready {
    push(@seen, "declaration:" . $0 . ":" . $1);
    return "declaration";
}
when("ready", {
    push(@seen, "function-two:" . $0 . ":" . $1);
    return "function-two";
});
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !script.Get("$when_result").IsNull() {
		t.Fatalf("when registration result = %s, want $null", script.Get("$when_result").Describe())
	}

	bindings := runtimeInstance.Bindings(BindingEvent, "ready")
	if len(bindings) != 4 {
		t.Fatalf("ready binding count = %d, want 4", len(bindings))
	}
	wantKeywords := []string{"when", "on", "when", "when"}
	wantLifetimes := []BindingLifetime{BindingOnce, BindingPersistent, BindingOnce, BindingOnce}
	for index, binding := range bindings {
		if binding.Keyword != wantKeywords[index] || binding.Lifetime != wantLifetimes[index] {
			t.Fatalf("binding[%d] = keyword %q lifetime %d, want %q/%d: %#v",
				index, binding.Keyword, binding.Lifetime, wantKeywords[index], wantLifetimes[index], binding)
		}
	}
	if results, err := runtimeInstance.DispatchEvent(context.Background(), "READY", String("ignored")); err != nil || len(results) != 0 {
		t.Fatalf("case-different DispatchEvent = (%v, %v), want no match", results, err)
	}
	if got := len(runtimeInstance.Bindings(BindingEvent, "ready")); got != 4 {
		t.Fatalf("case-different event consumed bindings: got %d, want 4", got)
	}

	results, err := runtimeInstance.DispatchEvent(context.Background(), "ready", String("first"))
	if err != nil {
		t.Fatalf("first DispatchEvent: %v", err)
	}
	if got, want := dynamicBindingValueStrings(results), []string{"function-one", "persistent", "declaration", "function-two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first results = %q, want %q", got, want)
	}
	assertDynamicBindingStrings(t, script.Get("@seen"), []string{
		"function-one:ready:first:first:0",
		"persistent:ready:first",
		"declaration:ready:first",
		"function-two:ready:first",
	})
	bindings = runtimeInstance.Bindings(BindingEvent, "ready")
	if len(bindings) != 2 || bindings[0].Keyword != "on" || bindings[0].Lifetime != BindingPersistent ||
		bindings[1].Keyword != "when" || bindings[1].Lifetime != BindingOnce {
		t.Fatalf("bindings after first dispatch = %#v, want persistent on plus replacement when", bindings)
	}

	results, err = runtimeInstance.DispatchEvent(context.Background(), "ready", String("second"))
	if err != nil {
		t.Fatalf("second DispatchEvent: %v", err)
	}
	if got, want := dynamicBindingValueStrings(results), []string{"persistent", "replacement"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second results = %q, want %q", got, want)
	}
	results, err = runtimeInstance.DispatchEvent(context.Background(), "ready", String("third"))
	if err != nil {
		t.Fatalf("third DispatchEvent: %v", err)
	}
	if got, want := dynamicBindingValueStrings(results), []string{"persistent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("third results = %q, want %q", got, want)
	}
	assertDynamicBindingStrings(t, script.Get("@seen"), []string{
		"function-one:ready:first:first:0",
		"persistent:ready:first",
		"declaration:ready:first",
		"function-two:ready:first",
		"persistent:ready:second",
		"replacement:ready:second",
		"persistent:ready:third",
	})

	_, unregistered := observer.snapshot()
	if got, want := bindingIDs(unregistered), []uint64{1, 3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("consumed binding IDs = %v, want %v", got, want)
	}
}

func TestWhenWildcardUsesConcreteNamedAndPositionalEventABI(t *testing.T) {
	runtimeInstance, err := New(withWhenFunction())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("when-wildcard.cna", `
@seen = @();
when("*", {
    @seen = @($0, $1, $2, $3, @_[0], @_[1], @_[2]);
    return "wildcard";
});
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	results, err := runtimeInstance.DispatchEvent(
		context.Background(), "beacon_output", String("beacon-7"), String("text"),
	)
	if err != nil {
		t.Fatalf("DispatchEvent: %v", err)
	}
	if got, want := dynamicBindingValueStrings(results), []string{"wildcard"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %q, want %q", got, want)
	}
	assertDynamicBindingStrings(t, script.Get("@seen"), []string{
		"beacon_output", "beacon_output", "beacon-7", "text",
		"beacon_output", "beacon-7", "text",
	})
	if results, err := runtimeInstance.DispatchEvent(context.Background(), "second"); err != nil || len(results) != 0 {
		t.Fatalf("second DispatchEvent = (%v, %v), want no callback", results, err)
	}
}

func TestWhenWildcardRegisteredReentrantlyWaitsForNextEvent(t *testing.T) {
	runtimeInstance, err := New(withWhenFunction())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("when-wildcard-reentrant.cna", `
@seen = @();
on ready {
    push(@seen, "exact:" . $0);
    when("*", { push(@seen, "wildcard:" . $0 . ":" . $1); });
}
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.DispatchEvent(context.Background(), "ready"); err != nil {
		t.Fatalf("ready DispatchEvent: %v", err)
	}
	assertDynamicBindingStrings(t, script.Get("@seen"), []string{"exact:ready"})
	bindings := runtimeInstance.Bindings(BindingEvent, "*")
	if len(bindings) != 1 || bindings[0].Lifetime != BindingOnce {
		t.Fatalf("reentrant wildcard bindings = %#v, want one waiting one-shot", bindings)
	}
	if _, err := runtimeInstance.DispatchEvent(context.Background(), "later"); err != nil {
		t.Fatalf("later DispatchEvent: %v", err)
	}
	assertDynamicBindingStrings(t, script.Get("@seen"), []string{"exact:ready", "wildcard:later:later"})
	if bindings := runtimeInstance.Bindings(BindingEvent, "*"); len(bindings) != 0 {
		t.Fatalf("wildcard bindings after later event = %#v, want consumed", bindings)
	}
}

type whenErrorObserver struct {
	mu              sync.Mutex
	unregistered    []Binding
	unregisteredErr error
	runtime         *Runtime
	activeCounts    []int
}

func (*whenErrorObserver) Registered(context.Context, Binding) error { return nil }

func (observer *whenErrorObserver) Unregistered(_ context.Context, binding Binding) error {
	count := 0
	for _, candidate := range observer.runtime.Bindings(BindingEvent, binding.Name) {
		if candidate.Lifetime == BindingOnce {
			count++
		}
	}
	observer.mu.Lock()
	observer.unregistered = append(observer.unregistered, binding)
	observer.activeCounts = append(observer.activeCounts, count)
	observer.mu.Unlock()
	return observer.unregisteredErr
}

func TestWhenConsumesAllSelectedBindingsBeforeObserverAndCallbackErrors(t *testing.T) {
	callbackErr := errors.New("when callback failed")
	laterCallbackErr := errors.New("later when callback failed")
	observerErr := errors.New("when observer failed")
	var firstCalls atomic.Int32
	var laterCalls atomic.Int32
	observer := &whenErrorObserver{unregisteredErr: observerErr}
	runtimeInstance, err := New(
		withWhenFunction(),
		WithBindingObserver(observer),
		WithFunction("fail_when", func(context.Context, Invocation) (Value, error) {
			firstCalls.Add(1)
			return Null(), callbackErr
		}),
		WithFunction("later_when", func(context.Context, Invocation) (Value, error) {
			laterCalls.Add(1)
			return Null(), laterCallbackErr
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	observer.runtime = runtimeInstance
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("when-errors.cna", `
when("failure", { fail_when(); });
when("failure", { later_when(); });
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}

	_, err = runtimeInstance.DispatchEvent(context.Background(), "failure")
	if !errors.Is(err, callbackErr) || !errors.Is(err, laterCallbackErr) || !errors.Is(err, observerErr) {
		t.Fatalf("DispatchEvent error = %v, want both callback and observer failures", err)
	}
	errorText := err.Error()
	if firstIndex, laterIndex := strings.Index(errorText, callbackErr.Error()), strings.Index(errorText, laterCallbackErr.Error()); firstIndex < 0 || laterIndex <= firstIndex {
		t.Fatalf("callback error order = %q, want first callback before later callback", errorText)
	}
	if got := firstCalls.Load(); got != 1 {
		t.Fatalf("first callback calls = %d, want 1", got)
	}
	if got := laterCalls.Load(); got != 1 {
		t.Fatalf("later callback calls = %d, want 1 despite first callback error", got)
	}
	if bindings := runtimeInstance.Bindings(BindingEvent, "failure"); len(bindings) != 0 {
		t.Fatalf("failure bindings after error = %#v, want none", bindings)
	}
	observer.mu.Lock()
	activeCounts := append([]int(nil), observer.activeCounts...)
	unregistered := append([]Binding(nil), observer.unregistered...)
	observer.mu.Unlock()
	if !reflect.DeepEqual(activeCounts, []int{0, 0}) {
		t.Fatalf("active one-shots observed during unregistration = %v, want [0 0]", activeCounts)
	}
	if got, want := bindingIDs(unregistered), []uint64{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unregistered IDs = %v, want %v", got, want)
	}
	if results, err := runtimeInstance.DispatchEvent(context.Background(), "failure"); err != nil || len(results) != 0 {
		t.Fatalf("second DispatchEvent = (%v, %v), want consumed listeners", results, err)
	}
}

func TestWhenConcurrentDispatchAndDirectInvocationClaimCallbackExactlyOnce(t *testing.T) {
	observer := &dynamicBindingObserver{}
	runtimeInstance, err := New(WithBindingObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("when-concurrent-owner.cna", ``)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	var callbackCalls atomic.Int32
	callback := whenTestCallable(func(context.Context, ...Value) (Value, error) {
		callbackCalls.Add(1)
		return String("claimed"), nil
	})
	registrationResult, err := runtimeInstance.registerWhen(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  owner.ID(),
		Name:    "when",
		Arguments: []Argument{
			{Value: String("race")},
			{Value: FunctionValue(callback)},
		},
	})
	if err != nil {
		t.Fatalf("registerWhen: %v", err)
	}
	if !registrationResult.IsNull() {
		t.Fatalf("registerWhen result = %s, want $null", registrationResult.Describe())
	}
	bindings := runtimeInstance.Bindings(BindingEvent, "race")
	if len(bindings) != 1 || bindings[0].Lifetime != BindingOnce || bindings[0].Keyword != "when" {
		t.Fatalf("registered binding = %#v", bindings)
	}

	const dispatchers = 64
	start := make(chan struct{})
	errCh := make(chan error, dispatchers)
	var resultCount atomic.Int32
	var wait sync.WaitGroup
	wait.Add(dispatchers)
	for index := 0; index < dispatchers; index++ {
		go func(index int) {
			defer wait.Done()
			<-start
			var result Value
			var results []Value
			var invokeErr error
			switch index % 3 {
			case 0:
				results, invokeErr = runtimeInstance.DispatchEvent(context.Background(), "race")
				resultCount.Add(int32(len(results)))
			case 1:
				result, invokeErr = runtimeInstance.InvokeBinding(context.Background(), BindingEvent, "race")
			case 2:
				result, invokeErr = runtimeInstance.InvokeBindingByID(
					context.Background(), bindings[0].Script, bindings[0].ID,
				)
			}
			if invokeErr != nil {
				var unsupported *UnsupportedError
				if errors.As(invokeErr, &unsupported) {
					errCh <- nil
					return
				}
				errCh <- invokeErr
				return
			}
			if index%3 != 0 && result.String() == "claimed" {
				resultCount.Add(1)
			}
			errCh <- nil
		}(index)
	}
	close(start)
	wait.Wait()
	close(errCh)
	for dispatchErr := range errCh {
		if dispatchErr != nil {
			t.Fatalf("concurrent DispatchEvent: %v", dispatchErr)
		}
	}
	if got := callbackCalls.Load(); got != 1 {
		t.Fatalf("callback calls = %d, want exactly 1", got)
	}
	if got := resultCount.Load(); got != 1 {
		t.Fatalf("total result count = %d, want exactly 1", got)
	}
	if bindings := runtimeInstance.Bindings(BindingEvent, "race"); len(bindings) != 0 {
		t.Fatalf("race bindings after dispatch = %#v, want none", bindings)
	}
	_, unregistered := observer.snapshot()
	if got, want := bindingIDs(unregistered), []uint64{1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unregistered IDs = %v, want %v", got, want)
	}
}

func TestWhenDirectBindingAPIsConsumeOneShotAndUseNamedEventABI(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(context.Context, *Runtime, Binding) (Value, error)
	}{
		{
			name: "by name",
			invoke: func(ctx context.Context, runtimeInstance *Runtime, _ Binding) (Value, error) {
				return runtimeInstance.InvokeBinding(ctx, BindingEvent, "direct", String("payload"))
			},
		},
		{
			name: "by id",
			invoke: func(ctx context.Context, runtimeInstance *Runtime, binding Binding) (Value, error) {
				return runtimeInstance.InvokeBindingByID(ctx, binding.Script, binding.ID, String("payload"))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observer := &dynamicBindingObserver{}
			runtimeInstance, err := New(withWhenFunction(), WithBindingObserver(observer))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			program, err := CompileString("when-direct.cna", `
@seen = @();
when("direct", {
    @seen = @($0, $1, @_[0]);
    return "direct-result";
});
`)
			if err != nil {
				t.Fatal(err)
			}
			script, err := runtimeInstance.Load(context.Background(), program)
			if err != nil {
				t.Fatal(err)
			}
			bindings := runtimeInstance.Bindings(BindingEvent, "direct")
			if len(bindings) != 1 {
				t.Fatalf("direct bindings = %#v, want one", bindings)
			}
			binding := bindings[0]
			value, err := test.invoke(context.Background(), runtimeInstance, binding)
			if err != nil || value.String() != "direct-result" {
				t.Fatalf("first direct invocation = (%s, %v), want direct-result", value.Describe(), err)
			}
			assertDynamicBindingStrings(t, script.Get("@seen"), []string{"direct", "payload", "payload"})
			if bindings := runtimeInstance.Bindings(BindingEvent, "direct"); len(bindings) != 0 {
				t.Fatalf("direct bindings after invocation = %#v, want consumed", bindings)
			}
			_, err = test.invoke(context.Background(), runtimeInstance, binding)
			var unsupported *UnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("second direct invocation error = %v, want UnsupportedError", err)
			}
			_, unregistered := observer.snapshot()
			if got, want := bindingIDs(unregistered), []uint64{binding.ID}; !reflect.DeepEqual(got, want) {
				t.Fatalf("unregistered IDs = %v, want %v", got, want)
			}
		})
	}
}

func TestWhenPortableDefaultYieldsToWithFunctionOverride(t *testing.T) {
	for _, test := range []struct {
		name    string
		options func(NativeFunc) []Option
	}{
		{
			name: "override first",
			options: func(override NativeFunc) []Option {
				return []Option{WithFunction("when", override), WithDebugFlags(0)}
			},
		},
		{
			name: "override last",
			options: func(override NativeFunc) []Option {
				return []Option{WithDebugFlags(0), WithFunction("when", override)}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			override := NativeFunc(func(context.Context, Invocation) (Value, error) {
				calls.Add(1)
				return String("importer-override"), nil
			})
			runtimeInstance, err := New(test.options(override)...)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			program, err := CompileString("when-override.cna", `$result = when("ready", { return "native"; });`)
			if err != nil {
				t.Fatal(err)
			}
			script, err := runtimeInstance.Load(context.Background(), program)
			if err != nil {
				t.Fatal(err)
			}
			if got := script.Get("$result").String(); got != "importer-override" {
				t.Fatalf("override result = %q, want importer-override", got)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("override calls = %d, want 1", got)
			}
			if bindings := runtimeInstance.Bindings(BindingEvent, "ready"); len(bindings) != 0 {
				t.Fatalf("portable when ran despite override: %#v", bindings)
			}
		})
	}
}

func TestRegisterWhenRejectsInvalidCallbacks(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("when-invalid-owner.cna", ``)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	valid := FunctionValue(whenTestCallable(func(context.Context, ...Value) (Value, error) {
		return Null(), nil
	}))
	var typedNil *whenTypedNilCallable
	malformedTypedNil := Value{kind: KindFunction, data: Callable(typedNil)}
	tests := []struct {
		name      string
		arguments []Argument
		want      string
		invalid   bool
	}{
		{name: "missing callback", arguments: []Argument{{Value: String("ready")}}, want: "expected at least 2 argument"},
		{name: "non-callable", arguments: []Argument{{Value: String("ready")}, {Value: String("no")}}, want: "argument 2 is not callable", invalid: true},
		{name: "typed nil", arguments: []Argument{{Value: String("ready")}, {Value: malformedTypedNil}}, want: "argument 2 is not callable", invalid: true},
		{name: "empty name", arguments: []Argument{{Value: String("")}, {Value: valid}}, want: "binding name is empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, registerErr := runtimeInstance.registerWhen(context.Background(), Invocation{
				Runtime:   runtimeInstance,
				Script:    owner.ID(),
				Name:      "when",
				Arguments: test.arguments,
			})
			if registerErr == nil || !strings.Contains(registerErr.Error(), test.want) {
				t.Fatalf("registerWhen error = %v, want containing %q", registerErr, test.want)
			}
			if test.invalid && !errors.Is(registerErr, ErrInvalidCallable) {
				t.Fatalf("registerWhen error = %v, want ErrInvalidCallable", registerErr)
			}
		})
	}
	if bindings := runtimeInstance.Bindings(BindingEvent, ""); len(bindings) != 0 {
		t.Fatalf("invalid registrations left bindings: %#v", bindings)
	}
}

func TestBindingLifetimeZeroValueIsPersistent(t *testing.T) {
	var binding Binding
	if binding.Lifetime != BindingPersistent {
		t.Fatalf("zero-value lifetime = %d, want BindingPersistent", binding.Lifetime)
	}
}

func bindingIDs(bindings []Binding) []uint64 {
	result := make([]uint64, len(bindings))
	for index, binding := range bindings {
		result[index] = binding.ID
	}
	return result
}
