package opfor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestBindingEntriesEnforceInstructionLimit(t *testing.T) {
	t.Parallel()

	const instructionLimit = 64
	tests := []struct {
		name        string
		source      string
		kind        BindingKind
		bindingName string
		invoke      func(context.Context, *Runtime, Binding) error
	}{
		{
			name:        "event dispatch",
			source:      `on limited { $count = 0; while ($count < 10000) { $count++; } }`,
			kind:        BindingEvent,
			bindingName: "limited",
			invoke: func(ctx context.Context, runtime *Runtime, _ Binding) error {
				_, err := runtime.DispatchEvent(ctx, "limited")
				return err
			},
		},
		{
			name:        "event by name",
			source:      `on limited { $count = 0; while ($count < 10000) { $count++; } }`,
			kind:        BindingEvent,
			bindingName: "limited",
			invoke: func(ctx context.Context, runtime *Runtime, _ Binding) error {
				_, err := runtime.InvokeBinding(ctx, BindingEvent, "limited")
				return err
			},
		},
		{
			name:        "hook by name",
			source:      `set LIMITED { $count = 0; while ($count < 10000) { $count++; } }`,
			kind:        BindingHook,
			bindingName: "LIMITED",
			invoke: func(ctx context.Context, runtime *Runtime, _ Binding) error {
				_, err := runtime.InvokeBinding(ctx, BindingHook, "LIMITED")
				return err
			},
		},
		{
			name:        "popup dispatch",
			source:      `popup limited { $count = 0; while ($count < 10000) { $count++; } }`,
			kind:        BindingPopup,
			bindingName: "limited",
			invoke: func(ctx context.Context, runtime *Runtime, _ Binding) error {
				_, err := runtime.DispatchPopupHook(ctx, "limited")
				return err
			},
		},
		{
			name:        "popup by name",
			source:      `popup limited { $count = 0; while ($count < 10000) { $count++; } }`,
			kind:        BindingPopup,
			bindingName: "limited",
			invoke: func(ctx context.Context, runtime *Runtime, _ Binding) error {
				_, err := runtime.InvokeBinding(ctx, BindingPopup, "limited")
				return err
			},
		},
		{
			name:        "event by ID",
			source:      `on limited { $count = 0; while ($count < 10000) { $count++; } }`,
			kind:        BindingEvent,
			bindingName: "limited",
			invoke: func(ctx context.Context, runtime *Runtime, binding Binding) error {
				_, err := runtime.InvokeBindingByID(ctx, binding.Script, binding.ID)
				return err
			},
		},
		{
			name:        "hook by ID",
			source:      `set LIMITED { $count = 0; while ($count < 10000) { $count++; } }`,
			kind:        BindingHook,
			bindingName: "LIMITED",
			invoke: func(ctx context.Context, runtime *Runtime, binding Binding) error {
				_, err := runtime.InvokeBindingByID(ctx, binding.Script, binding.ID)
				return err
			},
		},
		{
			name:        "popup by ID",
			source:      `popup limited { $count = 0; while ($count < 10000) { $count++; } }`,
			kind:        BindingPopup,
			bindingName: "limited",
			invoke: func(ctx context.Context, runtime *Runtime, binding Binding) error {
				_, err := runtime.InvokeBindingByID(ctx, binding.Script, binding.ID)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runtimeInstance, err := New(WithInstructionLimit(instructionLimit))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			program, err := CompileString("binding-entry-limit.cna", test.source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
				t.Fatal(err)
			}
			bindings := runtimeInstance.Bindings(test.kind, test.bindingName)
			if len(bindings) != 1 {
				t.Fatalf("bindings = %#v, want one %s %q binding", bindings, test.kind, test.bindingName)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err = test.invoke(ctx, runtimeInstance, bindings[0])
			assertBindingInstructionLimit(t, err, instructionLimit)
		})
	}
}

func TestBindingDispatchSharesInstructionMeterAcrossHandlersAndReentry(t *testing.T) {
	t.Parallel()

	type observation struct {
		label string
		meter *executionMeter
	}
	var observed []observation
	runtimeInstance, err := New(
		WithInstructionLimit(10_000),
		WithFunction("record_binding_meter", func(ctx context.Context, invocation Invocation) (Value, error) {
			meter, _ := ctx.Value(executionMeterKey{}).(*executionMeter)
			observed = append(observed, observation{label: invocation.Arg(0).String(), meter: meter})
			return Null(), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("binding-dispatch-meter.cna", `
on inner { record_binding_meter("inner"); }
on outer {
    record_binding_meter("outer-first");
    fire_event("inner");
}
on outer { record_binding_meter("outer-second"); }
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.DispatchEvent(context.Background(), "outer"); err != nil {
		t.Fatal(err)
	}

	wantLabels := []string{"outer-first", "inner", "outer-second"}
	gotLabels := make([]string, len(observed))
	for index, current := range observed {
		gotLabels[index] = current.label
		if current.meter == nil {
			t.Fatalf("observation %q has no instruction meter", current.label)
		}
		if index != 0 && current.meter != observed[0].meter {
			t.Fatalf("observation %q meter = %p, want shared dispatch meter %p", current.label, current.meter, observed[0].meter)
		}
	}
	if !reflect.DeepEqual(gotLabels, wantLabels) {
		t.Fatalf("meter observations = %q, want %q", gotLabels, wantLabels)
	}
}

func assertBindingInstructionLimit(t *testing.T, err error, instructionLimit uint64) {
	t.Helper()
	if !errors.Is(err, ErrInstructionLimit) {
		t.Fatalf("binding execution error = %v, want ErrInstructionLimit", err)
	}
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Resource != resourceInstruction || limit.Limit != instructionLimit {
		t.Fatalf("LimitError = %+v, want instruction limit %d", limit, instructionLimit)
	}
}
