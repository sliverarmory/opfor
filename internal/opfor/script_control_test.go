package opfor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScriptControllerDebugFlagsAndDetachedProfile(t *testing.T) {
	var clockStep atomic.Int64
	base := time.Unix(1_700_000_000, 0)
	runtimeInstance, err := New(WithClock(ClockFunc(func() time.Time {
		return base.Add(time.Duration(clockStep.Add(1)) * time.Millisecond)
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("controller-profile.sl", `
sub work { return 7; }
sub exercise { work(); return 1; }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := runtimeInstance.ScriptByID(script.ID())
	if err != nil || resolved != script {
		t.Fatalf("ScriptByID(%d) = (%p, %v), want %p", script.ID(), resolved, err, script)
	}
	if flags, err := script.DebugFlags(); err != nil || flags != 1 {
		t.Fatalf("initial DebugFlags = (%d, %v), want 1", flags, err)
	}
	if flags, err := script.SetDebugFlags(debugTraceCalls | debugTraceSuppress); err != nil || flags != 24 {
		t.Fatalf("SetDebugFlags = (%d, %v), want 24", flags, err)
	}
	for iteration := 0; iteration < 2; iteration++ {
		if _, err := script.Call(context.Background(), "exercise"); err != nil {
			t.Fatalf("exercise %d: %v", iteration, err)
		}
	}
	report, err := script.SnapshotProfile()
	if err != nil {
		t.Fatal(err)
	}
	if report.Script != script.ID() || len(report.Statistics) != 1 {
		t.Fatalf("profile = %#v, want one statistic for script %d", report, script.ID())
	}
	statistic := report.Statistics[0]
	if statistic.FunctionName != "&work" || statistic.Calls != 2 || statistic.Ticks != 2 {
		t.Fatalf("profile statistic = %#v, want &work/2 calls/2 ticks", statistic)
	}

	// The report and every entry are values detached from live profiler state.
	report.Statistics[0].FunctionName = "mutated"
	report.Statistics[0].Calls = 999
	report.Statistics = append(report.Statistics, ProfileStatisticSnapshot{FunctionName: "injected"})
	again, err := script.SnapshotProfile()
	if err != nil || len(again.Statistics) != 1 || again.Statistics[0].FunctionName != "&work" || again.Statistics[0].Calls != 2 {
		t.Fatalf("detached profile after caller mutation = (%#v, %v)", again, err)
	}
	encoded, err := json.Marshal(again)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := fmt.Sprintf(`{"script":%d,"statistics":[{"function_name":"\u0026work","ticks":2,"calls":2}]}`, script.ID())
	if string(encoded) != wantJSON {
		t.Fatalf("profile JSON = %s, want %s", encoded, wantJSON)
	}

	if _, err := runtimeInstance.ScriptByID(0); err == nil || err.Error() != "opfor: script ID must be nonzero" {
		t.Fatalf("ScriptByID(0) error = %v", err)
	}
	if _, err := runtimeInstance.ScriptByID(script.ID() + 100); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("missing ScriptByID error = %v, want ErrScriptUnloaded", err)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.ScriptByID(script.ID()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("post-unload ScriptByID error = %v", err)
	}
	if _, err := script.DebugFlags(); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("post-unload DebugFlags error = %v", err)
	}
	if _, err := script.SetDebugFlags(8); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("post-unload SetDebugFlags error = %v", err)
	}
	if _, err := script.SnapshotProfile(); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("post-unload SnapshotProfile error = %v", err)
	}
}

func TestScriptControllerConcurrentAccess(t *testing.T) {
	var clockStep atomic.Int64
	runtimeInstance, err := New(WithClock(ClockFunc(func() time.Time {
		return time.Unix(0, clockStep.Add(1)*int64(time.Millisecond))
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("controller-concurrent.sl", `
sub work { return 1; }
sub exercise { return work(); }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := script.SetDebugFlags(debugTraceCalls | debugTraceSuppress); err != nil {
		t.Fatal(err)
	}

	const workers = 12
	const iterations = 100
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				flags := int32(debugTraceCalls | debugTraceSuppress | int32((worker+iteration)&1))
				_, _ = script.SetDebugFlags(flags)
				_, _ = script.DebugFlags()
				_, _ = runtimeInstance.ScriptByID(script.ID())
				_, _ = script.Call(context.Background(), "exercise")
				_, _ = script.SnapshotProfile()
			}
		}(worker)
	}
	wait.Wait()
	report, err := script.SnapshotProfile()
	if err != nil || len(report.Statistics) == 0 || report.Statistics[0].Calls == 0 {
		t.Fatalf("concurrent profile = (%#v, %v)", report, err)
	}
}
