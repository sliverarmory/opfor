package opfor

import (
	"context"
	"errors"
	"testing"
)

func TestNestedClosureCallReusesActiveScriptExecution(t *testing.T) {
	var script *Script
	runtimeInstance, err := New(WithFunction("execution_depth", func(ctx context.Context, _ Invocation) (Value, error) {
		active := int32(0)
		for token, _ := ctx.Value(scriptExecutionContextKey{}).(*scriptExecutionToken); token != nil; token = token.parent {
			if token.script == script && token.active.Load() {
				active++
			}
		}
		return Int(active), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("nested-execution-fastpath.sl", `
sub inner { return execution_depth(); }
sub outer { return inner(); }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	result, err := script.Call(context.Background(), "outer")
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Int32(); got != 3 {
		// Script.Call owns one public lease and the initially invoked closure
		// owns one internal lease. The native bridge owns the third while it
		// observes the context; nested same-script calls reuse the closure lease.
		t.Fatalf("active same-script execution leases = %d, want 3", got)
	}
}

func TestNestedCallFastPathsBoundAllocations(t *testing.T) {
	runtimeInstance, err := New(WithFunction("native_increment", func(_ context.Context, invocation Invocation) (Value, error) {
		return Int(invocation.Arg(0).Int32() + 1), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("nested-call-allocation-fastpath.sl", `
sub increment { return $1 + 1; }
sub script_calls {
    $value = 0;
    for ($index = 0; $index < 100; $index++) { $value = increment($value); }
    return $value;
}
sub native_calls {
    $value = 0;
    for ($index = 0; $index < 100; $index++) { $value = native_increment($value); }
    return $value;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	assertAllocations := func(name string, maximum float64) {
		t.Helper()
		var result Value
		var callErr error
		allocations := testing.AllocsPerRun(5, func() {
			result, callErr = script.Call(context.Background(), name)
		})
		if callErr != nil {
			t.Fatalf("%s: %v", name, callErr)
		}
		if result.Int32() != 100 {
			t.Fatalf("%s result = %s, want 100", name, result.Describe())
		}
		if allocations > maximum {
			t.Fatalf("%s allocations = %.0f, want <= %.0f", name, allocations, maximum)
		}
	}
	assertAllocations("script_calls", 5_000)
	assertAllocations("native_calls", 3_500)
}

func TestDisabledTaintFastPathSkipsArgumentInspection(t *testing.T) {
	disabled, err := New(WithTaintMode(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = disabled.Close(context.Background()) })

	enabled, err := New(WithTaintMode(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = enabled.Close(context.Background()) })

	tainted := enabled.Taint(String("input"))
	arguments := []Argument{{Value: ArrayValue(NewArray(tainted))}}
	if values := disabled.taintedArguments(arguments); values != nil {
		t.Fatalf("disabled taint arguments = %v, want nil fast-path result", values)
	}
	if values := enabled.taintedArguments(arguments); len(values) != 1 || !values[0].IsTainted() {
		t.Fatalf("enabled taint arguments = %v, want one tainted container", values)
	}
	if values := disabled.taintedValues(tainted); values != nil {
		t.Fatalf("disabled taint values = %v, want nil fast-path result", values)
	}
}

func TestVMExecutionLimitsResolveOnceAndPreserveCancellation(t *testing.T) {
	unlimited, err := New(WithLimits(Limits{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unlimited.Close(context.Background()) })
	unlimitedContext := withExecutionMeter(context.Background(), unlimited)
	if meter, account := vmExecutionLimits(unlimitedContext); meter != nil || account != nil {
		t.Fatalf("unlimited execution limits = (%v, %v), want (nil, nil)", meter, account)
	}

	limited, err := New(WithLimits(Limits{
		MaxInstructionsPerExecution: 10,
		MaxOutputBytesPerRuntime:    10,
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limited.Close(context.Background()) })
	limitedContext := withExecutionMeter(context.Background(), limited)
	if meter, account := vmExecutionLimits(limitedContext); meter == nil || account != limited.resources {
		t.Fatalf("limited execution limits = (%v, %p), want non-nil meter and %p", meter, account, limited.resources)
	}

	program, err := CompileString("unmetered-cancel.sl", `while (true) { $count++; }`)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := unlimited.Execute(canceled, program); !errors.Is(err, context.Canceled) {
		t.Fatalf("unmetered canceled execution = %v, want context.Canceled", err)
	}
}
