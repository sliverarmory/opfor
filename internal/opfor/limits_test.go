package opfor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeResourceLimitConfigurationAndErrors(t *testing.T) {
	t.Parallel()

	configured := Limits{
		MaxInstructionsPerExecution:    11,
		MaxCollectionEntriesPerRuntime: 12,
		MaxOutputBytesPerRuntime:       13,
		MaxInputBytesPerRuntime:        14,
		MaxDecompressedBytesPerRuntime: 15,
		MaxSourceBytesPerRuntime:       16,
	}
	runtimeInstance, err := New(WithLimits(configured), WithInstructionLimit(21))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if runtimeInstance.limits.MaxInstructionsPerExecution != 21 ||
		runtimeInstance.limits.MaxCollectionEntriesPerRuntime != 12 ||
		runtimeInstance.limits.MaxOutputBytesPerRuntime != 13 ||
		runtimeInstance.limits.MaxInputBytesPerRuntime != 14 ||
		runtimeInstance.limits.MaxDecompressedBytesPerRuntime != 15 ||
		runtimeInstance.limits.MaxSourceBytesPerRuntime != 16 {
		t.Fatalf("configured limits = %+v", runtimeInstance.limits)
	}

	replaced, err := New(WithInstructionLimit(21), WithLimits(Limits{MaxSourceBytesPerRuntime: 5}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replaced.Close(context.Background()) })
	if replaced.maxInstructions != 0 || replaced.limits.MaxSourceBytesPerRuntime != 5 {
		t.Fatalf("replaced limits = %+v / instruction %d", replaced.limits, replaced.maxInstructions)
	}

	resourceErr := &LimitError{Resource: resourceOutputBytes, Limit: 13}
	if resourceOutputBytes != LimitResourceOutputBytes ||
		resourceInstruction != LimitResourceInstruction ||
		resourceCollectionEntries != LimitResourceCollectionEntries ||
		resourceInputBytes != LimitResourceInputBytes ||
		resourceDecompressedBytes != LimitResourceDecompressedBytes ||
		resourceSourceBytes != LimitResourceSourceBytes {
		t.Fatal("internal and exported resource identifiers diverged")
	}
	if !errors.Is(resourceErr, ErrResourceLimit) || errors.Is(resourceErr, ErrInstructionLimit) {
		t.Fatalf("output LimitError matches resource=%t instruction=%t", errors.Is(resourceErr, ErrResourceLimit), errors.Is(resourceErr, ErrInstructionLimit))
	}
	instructionErr := &LimitError{Resource: resourceInstruction, Limit: 21}
	if !errors.Is(instructionErr, ErrResourceLimit) || !errors.Is(instructionErr, ErrInstructionLimit) {
		t.Fatalf("instruction LimitError matches resource=%t instruction=%t", errors.Is(instructionErr, ErrResourceLimit), errors.Is(instructionErr, ErrInstructionLimit))
	}
	var nilLimit *LimitError
	if errors.Is(nilLimit, ErrResourceLimit) || errors.Is(nilLimit, ErrInstructionLimit) {
		t.Fatal("typed-nil LimitError unexpectedly matched a resource sentinel")
	}
}

func TestRuntimeResourceReservationIsConcurrentAndMonotonic(t *testing.T) {
	t.Parallel()

	account := newRuntimeResourceAccount(Limits{MaxOutputBytesPerRuntime: 100})
	const workers = 1000
	var successes atomic.Uint64
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := account.reserve(resourceOutputBytes, 1); err == nil {
				successes.Add(1)
				return
			} else if !errors.Is(err, ErrResourceLimit) {
				t.Errorf("reservation error = %v", err)
			}
		}()
	}
	wait.Wait()
	if got := successes.Load(); got != 100 {
		t.Fatalf("successful reservations = %d, want 100", got)
	}
	if got := account.used(resourceOutputBytes); got != 100 {
		t.Fatalf("used output = %d, want 100", got)
	}
	if err := account.reserve(resourceOutputBytes, ^uint64(0)); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("overflowing reservation = %v, want ErrResourceLimit", err)
	}
	if got := account.used(resourceOutputBytes); got != 100 {
		t.Fatalf("failed reservation changed usage to %d", got)
	}

	unlimited := newRuntimeResourceAccount(Limits{})
	if err := unlimited.reserve(resourceOutputBytes, ^uint64(0)); err != nil {
		t.Fatalf("unlimited reservation = %v", err)
	}
	if got := unlimited.used(resourceOutputBytes); got != 0 {
		t.Fatalf("unlimited counter retained %d bytes", got)
	}
}

func TestInstructionLimitStopsLoopsAndNestedCalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
	}{
		{name: "loop", code: `while (true) { $count++; }`},
		{name: "nested calls", code: `sub recurse { return recurse(); } recurse();`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, err := New(WithInstructionLimit(100))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			program, err := CompileString("limit.sl", test.code)
			if err != nil {
				t.Fatalf("CompileString: %v", err)
			}
			_, err = runtime.Execute(context.Background(), program)
			if !errors.Is(err, ErrInstructionLimit) {
				t.Fatalf("Execute error = %v, want ErrInstructionLimit", err)
			}
			var limit *LimitError
			if !errors.As(err, &limit) || limit.Resource != "instruction" || limit.Limit != 100 {
				t.Fatalf("LimitError = %+v", limit)
			}
		})
	}
}

func TestZeroInstructionLimitIsUnlimited(t *testing.T) {
	t.Parallel()
	runtime, err := New(WithInstructionLimit(0))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(context.Background(), "unlimited.sl", `return 42;`)
	if err != nil || value.Int32() != 42 {
		t.Fatalf("Eval = (%s, %v)", value.Describe(), err)
	}
}

func TestInstructionLimitStopsPublicNamedEntries(t *testing.T) {
	t.Parallel()

	t.Run("Script.Call", func(t *testing.T) {
		const instructionLimit = 100
		runtimeInstance, err := New(WithInstructionLimit(instructionLimit))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		program, err := CompileString("script-call-limit.sl", `
sub spin {
    while (true) { $count++; }
}
`)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err = script.Call(ctx, "spin")
		assertInstructionLimit(t, err, instructionLimit)
	})

	t.Run("InvokeConsole environment and function forms", func(t *testing.T) {
		const instructionLimit = 1_000
		runtimeInstance, err := New(WithInstructionLimit(instructionLimit))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		program, err := CompileString("console-limit.cna", `
command environment_command { while (true) { $count++; } }
alias environment_alias { while (true) { $count++; } }
ssh_alias environment_ssh_alias { while (true) { $count++; } }
alias("function_alias", { while (true) { $count++; } });
ssh_alias("function_ssh_alias", { while (true) { $count++; } });
`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
			t.Fatal(err)
		}
		tests := []struct {
			name string
			kind BindingKind
		}{
			{name: "environment_command", kind: BindingCommand},
			{name: "environment_alias", kind: BindingAlias},
			{name: "environment_ssh_alias", kind: BindingSSHAlias},
			{name: "function_alias", kind: BindingAlias},
			{name: "function_ssh_alias", kind: BindingSSHAlias},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_, err := runtimeInstance.InvokeConsole(ctx, ConsoleInvocation{
					Kind:      test.kind,
					Name:      test.name,
					SessionID: String("session"),
				})
				assertInstructionLimit(t, err, instructionLimit)
			})
		}
	})
}

func TestPublicNamedReentrySharesInstructionMeter(t *testing.T) {
	t.Parallel()

	t.Run("Script.Call", func(t *testing.T) {
		const instructionLimit = 100
		remaining := 128
		var script *Script
		runtimeInstance, err := New(
			WithInstructionLimit(instructionLimit),
			WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
				if invocation.Name != "reenter_script_call" || remaining == 0 {
					return Null(), nil
				}
				remaining--
				return script.Call(ctx, "bounce")
			})),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		program, err := CompileString("script-call-reentry-limit.sl", `
sub bounce { reenter_script_call(); }
`)
		if err != nil {
			t.Fatal(err)
		}
		script, err = runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err = script.Call(ctx, "bounce")
		assertInstructionLimit(t, err, instructionLimit)
		if remaining == 128 {
			t.Fatal("Script.Call did not reenter through Host")
		}
	})

	t.Run("InvokeConsole function alias", func(t *testing.T) {
		const instructionLimit = 100
		runtimeInstance, err := New(WithInstructionLimit(instructionLimit))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		program, err := CompileString("console-reentry-limit.cna", `
$calls = 0;
alias("bounce", {
    $calls++;
    if ($calls < 64) { fireAlias("session", "bounce", ""); }
});
`)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err = runtimeInstance.InvokeConsole(ctx, ConsoleInvocation{
			Kind:      BindingAlias,
			Name:      "bounce",
			SessionID: String("session"),
		})
		assertInstructionLimit(t, err, instructionLimit)
		if calls := script.Get("$calls").Int32(); calls < 2 {
			t.Fatalf("function alias calls = %d, want recursive InvokeConsole reentry", calls)
		}
	})
}

func assertInstructionLimit(t *testing.T, err error, instructionLimit uint64) {
	t.Helper()
	if !errors.Is(err, ErrInstructionLimit) {
		t.Fatalf("execution error = %v, want ErrInstructionLimit", err)
	}
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Resource != "instruction" || limit.Limit != instructionLimit {
		t.Fatalf("LimitError = %+v, want instruction limit %d", limit, instructionLimit)
	}
}
