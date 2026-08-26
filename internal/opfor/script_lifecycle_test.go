package opfor

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type scriptLifecycleEvent struct {
	phase  string
	id     ScriptID
	name   string
	active bool
}

type recordingScriptLifecycle struct {
	mu           sync.Mutex
	events       []scriptLifecycleEvent
	loaded       func(context.Context, *Script) error
	unloaded     func(context.Context, *Script) error
	loadedScript *Script
}

func (observer *recordingScriptLifecycle) ScriptLoaded(ctx context.Context, script *Script) error {
	observer.mu.Lock()
	observer.events = append(observer.events, scriptLifecycleEvent{
		phase: "loaded", id: script.ID(), name: script.Program().Source().Name, active: script.Active(),
	})
	observer.loadedScript = script
	callback := observer.loaded
	observer.mu.Unlock()
	if callback == nil {
		return nil
	}
	return callback(ctx, script)
}

func (observer *recordingScriptLifecycle) ScriptUnloaded(ctx context.Context, script *Script) error {
	observer.mu.Lock()
	observer.events = append(observer.events, scriptLifecycleEvent{
		phase: "unloaded", id: script.ID(), name: script.Program().Source().Name, active: script.Active(),
	})
	callback := observer.unloaded
	observer.mu.Unlock()
	if callback == nil {
		return nil
	}
	return callback(ctx, script)
}

func (observer *recordingScriptLifecycle) snapshot() []scriptLifecycleEvent {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return append([]scriptLifecycleEvent(nil), observer.events...)
}

func TestInitialGlobalsAreVisibleBeforeTopLevelAndPreserveIdentity(t *testing.T) {
	items := NewArray(String("seed"))
	configuration := NewHash()
	configuration.Set("mode", String("initial"))
	type hostObject struct{ name string }
	object := &hostObject{name: "client"}

	provided := map[string]Value{
		"scalar":  String("seed"),
		"@items":  ArrayValue(items),
		"%config": HashValue(configuration),
		"$client": ObjectValue(object),
	}
	observer := &recordingScriptLifecycle{
		loaded: func(_ context.Context, script *Script) error {
			if got := script.Get("$scalar").String(); got != "seed" {
				t.Fatalf("ScriptLoaded scalar = %q, want seed", got)
			}
			if got := script.Get("$__SCRIPT__").String(); got != "bootstrap.cna" {
				t.Fatalf("ScriptLoaded $__SCRIPT__ = %q, want bootstrap.cna", got)
			}
			argv, ok := script.Get("@ARGV").Array()
			if !ok {
				t.Fatalf("ScriptLoaded @ARGV = %s, want array", script.Get("@ARGV").Describe())
			}
			values := argv.Values()
			if len(values) != 1 || values[0].String() != "launcher" {
				t.Fatalf("ScriptLoaded @ARGV = %v, want launcher", values)
			}
			return script.Set("$from_lifecycle", String("ready"))
		},
		unloaded: func(_ context.Context, script *Script) error {
			if got := script.Get("$scalar").String(); got != "mutated" {
				t.Fatalf("ScriptUnloaded final scalar = %q, want mutated", got)
			}
			if err := script.Set("$too_late", Int(1)); !errors.Is(err, ErrScriptUnloaded) {
				t.Fatalf("ScriptUnloaded Set error = %v, want ErrScriptUnloaded", err)
			}
			return nil
		},
	}
	runtime, err := New(
		WithInitialGlobals(provided),
		WithScriptLifecycleObserver(observer),
	)
	if err != nil {
		t.Fatal(err)
	}
	// WithInitialGlobals owns a shallow copy of the map. Compound Values in that
	// copy intentionally retain their backing identity.
	provided["scalar"] = String("changed-outside")
	provided["later"] = String("not-installed")

	program, err := CompileString("bootstrap.cna", `
if ($scalar ne "seed") { throw "initial scalar missing"; }
if ($from_lifecycle ne "ready") { throw "lifecycle scalar missing"; }
if (@ARGV[0] ne "launcher") { throw "launcher argument missing"; }
$scalar = "mutated";
push(@items, "script");
%config["seen"] = "yes";
return $from_lifecycle;
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program, String("launcher"))
	if err != nil {
		t.Fatal(err)
	}
	if got := script.Result().String(); got != "ready" {
		t.Fatalf("top-level result = %q, want ready", got)
	}
	if got := script.Get("$scalar").String(); got != "mutated" {
		t.Fatalf("mutated scalar = %q, want mutated", got)
	}
	if !script.Get("$later").IsNull() {
		t.Fatalf("post-New input map entry was installed: %s", script.Get("$later").Describe())
	}
	gotItems, ok := script.Get("@items").Array()
	if !ok || gotItems != items {
		t.Fatalf("array identity = %p, %v; want %p, true", gotItems, ok, items)
	}
	if got := items.Values(); len(got) != 2 || got[1].String() != "script" {
		t.Fatalf("shared array values = %v, want seed/script", got)
	}
	gotConfig, ok := script.Get("%config").Hash()
	if !ok || gotConfig != configuration {
		t.Fatalf("hash identity = %p, %v; want %p, true", gotConfig, ok, configuration)
	}
	if seen, ok := configuration.Get("seen"); !ok || seen.String() != "yes" {
		t.Fatalf("shared hash seen = %s, %v; want yes, true", seen.Describe(), ok)
	}
	gotObject, ok := script.Get("$client").Object()
	if !ok || gotObject != object {
		t.Fatalf("object identity = %#v, %v; want %#v, true", gotObject, ok, object)
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("second Unload: %v", err)
	}
	events := observer.snapshot()
	wantPhases := []string{"loaded", "unloaded"}
	gotPhases := []string{events[0].phase, events[1].phase}
	if !reflect.DeepEqual(gotPhases, wantPhases) || !events[0].active || events[1].active {
		t.Fatalf("lifecycle events = %#v, want loaded-active then unloaded-inactive", events)
	}
}

func TestInitialGlobalValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		globals map[string]Value
	}{
		{name: "empty", globals: map[string]Value{"": Int(1)}},
		{name: "sigil-only", globals: map[string]Value{"@": ArrayValue(NewArray())}},
		{name: "whitespace", globals: map[string]Value{"bad name": Int(1)}},
		{name: "script", globals: map[string]Value{"$__SCRIPT__": Int(1)}},
		{name: "script-name", globals: map[string]Value{"$__SCRIPT_NAME__": Int(1)}},
		{name: "argv", globals: map[string]Value{"@ARGV": ArrayValue(NewArray())}},
		{name: "normalized-duplicate", globals: map[string]Value{"value": Int(1), "$value": Int(2)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(WithInitialGlobals(test.globals)); err == nil {
				t.Fatal("New accepted invalid initial globals")
			}
		})
	}
	if _, err := New(WithScriptLifecycleObserver(nil)); err == nil {
		t.Fatal("New accepted nil lifecycle observer")
	}
}

func TestScriptLifecycleRollsBackTopLevelAndObserverFailures(t *testing.T) {
	t.Run("top-level", func(t *testing.T) {
		topLevelErr := errors.New("top-level failure")
		observer := &recordingScriptLifecycle{
			unloaded: func(_ context.Context, script *Script) error {
				if got := script.Get("$partial").String(); got != "ready" {
					t.Fatalf("rollback observer partial state = %q, want ready", got)
				}
				return nil
			},
		}
		runtime, err := New(
			WithScriptLifecycleObserver(observer),
			WithFunction("fail_load", func(context.Context, Invocation) (Value, error) {
				return Null(), topLevelErr
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		program, err := CompileString("failed.cna", `$partial = "ready"; fail_load();`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Load(context.Background(), program); !errors.Is(err, topLevelErr) {
			t.Fatalf("Load error = %v, want top-level failure", err)
		}
		if scripts := runtime.Scripts(); len(scripts) != 0 {
			t.Fatalf("scripts after rollback = %d, want 0", len(scripts))
		}
		if observer.loadedScript == nil || observer.loadedScript.Active() {
			t.Fatal("rolled-back script did not become inactive")
		}
		if got := lifecyclePhases(observer.snapshot()); !reflect.DeepEqual(got, []string{"loaded", "unloaded"}) {
			t.Fatalf("lifecycle phases = %v", got)
		}
		if err := runtime.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := lifecyclePhases(observer.snapshot()); !reflect.DeepEqual(got, []string{"loaded", "unloaded"}) {
			t.Fatalf("Close repeated rollback notification: %v", got)
		}
	})

	t.Run("load-observer", func(t *testing.T) {
		loadErr := errors.New("observer load failure")
		cleanupErr := errors.New("observer cleanup failure")
		observer := &recordingScriptLifecycle{
			loaded:   func(context.Context, *Script) error { return loadErr },
			unloaded: func(context.Context, *Script) error { return cleanupErr },
		}
		topLevelRan := false
		runtime, err := New(
			WithScriptLifecycleObserver(observer),
			WithFunction("must_not_run", func(context.Context, Invocation) (Value, error) {
				topLevelRan = true
				return Null(), nil
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		program, err := CompileString("observer-failed.cna", `must_not_run();`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runtime.Load(context.Background(), program)
		if !errors.Is(err, loadErr) || !errors.Is(err, cleanupErr) {
			t.Fatalf("Load error = %v, want load and cleanup failures", err)
		}
		if topLevelRan {
			t.Fatal("top-level body ran after ScriptLoaded failed")
		}
		if got := lifecyclePhases(observer.snapshot()); !reflect.DeepEqual(got, []string{"loaded", "unloaded"}) {
			t.Fatalf("lifecycle phases = %v", got)
		}
	})
}

func TestScriptLifecycleRuntimeCloseIsReverseAndExactlyOnce(t *testing.T) {
	observer := &recordingScriptLifecycle{}
	runtime, err := New(WithScriptLifecycleObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.cna", "two.cna"} {
		program, compileErr := CompileString(name, `return 1;`)
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		if _, loadErr := runtime.Load(context.Background(), program); loadErr != nil {
			t.Fatal(loadErr)
		}
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	events := observer.snapshot()
	got := make([]string, len(events))
	for index, event := range events {
		got[index] = event.phase + ":" + event.name
	}
	want := []string{"loaded:one.cna", "loaded:two.cna", "unloaded:two.cna", "unloaded:one.cna"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestInitialGlobalsAndLifecycleCoverPersistentEval(t *testing.T) {
	observer := &recordingScriptLifecycle{}
	runtime, err := New(
		WithInitialGlobals(map[string]Value{"seed": Int(40)}),
		WithScriptLifecycleObserver(observer),
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := runtime.Eval(context.Background(), "first.cna", `$seed = $seed + 1; return $seed;`)
	if err != nil || first.Int32() != 41 {
		t.Fatalf("first Eval = %s, %v; want 41", first.Describe(), err)
	}
	second, err := runtime.Eval(context.Background(), "second.cna", `$seed = $seed + 1; return $seed;`)
	if err != nil || second.Int32() != 42 {
		t.Fatalf("second Eval = %s, %v; want 42", second.Describe(), err)
	}
	if got := lifecyclePhases(observer.snapshot()); !reflect.DeepEqual(got, []string{"loaded"}) {
		t.Fatalf("pre-Close lifecycle phases = %v", got)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := lifecyclePhases(observer.snapshot()); !reflect.DeepEqual(got, []string{"loaded", "unloaded"}) {
		t.Fatalf("post-Close lifecycle phases = %v", got)
	}
}

func TestScriptLifecycleFailedFirstEvalRollsBackSession(t *testing.T) {
	topLevelErr := errors.New("first eval failure")
	observer := &recordingScriptLifecycle{}
	runtime, err := New(
		WithScriptLifecycleObserver(observer),
		WithFunction("fail_first_eval", func(context.Context, Invocation) (Value, error) {
			return Null(), topLevelErr
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Eval(context.Background(), "failed-eval.cna", `fail_first_eval();`); !errors.Is(err, topLevelErr) {
		t.Fatalf("Eval error = %v, want first eval failure", err)
	}
	if scripts := runtime.Scripts(); len(scripts) != 0 {
		t.Fatalf("scripts after first Eval rollback = %d, want 0", len(scripts))
	}
	if got := lifecyclePhases(observer.snapshot()); !reflect.DeepEqual(got, []string{"loaded", "unloaded"}) {
		t.Fatalf("failed Eval lifecycle phases = %v", got)
	}
	value, err := runtime.Eval(context.Background(), "replacement-eval.cna", `return 42;`)
	if err != nil || value.Int32() != 42 {
		t.Fatalf("replacement Eval = %s, %v; want 42", value.Describe(), err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := lifecyclePhases(observer.snapshot()); !reflect.DeepEqual(got, []string{"loaded", "unloaded", "loaded", "unloaded"}) {
		t.Fatalf("replacement lifecycle phases = %v", got)
	}
}

func TestScriptLifecycleUnloadErrorIsDeliveredOnce(t *testing.T) {
	cleanupErr := errors.New("cleanup failure")
	observer := &recordingScriptLifecycle{
		unloaded: func(context.Context, *Script) error { return cleanupErr },
	}
	runtime, err := New(WithScriptLifecycleObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("unload-error.cna", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if err := script.Unload(context.Background()); !errors.Is(err, cleanupErr) {
		t.Fatalf("first Unload error = %v, want cleanup failure", err)
	}
	if script.Active() {
		t.Fatal("script remained active after unload callback failed")
	}
	if scripts := runtime.Scripts(); len(scripts) != 0 {
		t.Fatalf("scripts after unload callback failure = %d, want 0", len(scripts))
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("second Unload = %v, want nil", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close repeated unload callback error: %v", err)
	}
	if got := lifecyclePhases(observer.snapshot()); !reflect.DeepEqual(got, []string{"loaded", "unloaded"}) {
		t.Fatalf("lifecycle phases = %v, want one load/unload pair", got)
	}
}

func TestPortableScriptLoaderInheritsInitialGlobalsAndLifecycle(t *testing.T) {
	observer := &recordingScriptLifecycle{}
	runtime, err := New(
		WithInitialGlobals(map[string]Value{"seed": String("child-visible")}),
		WithScriptLifecycleObserver(observer),
	)
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loader-parent.cna", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "loader-child.cna", 'return $seed;', $null];
return [$child runScript];
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.String(); got != "child-visible" {
		t.Fatalf("child initial global = %q, want child-visible", got)
	}
	events := observer.snapshot()
	got := make([]string, len(events))
	for index, event := range events {
		got[index] = event.phase + ":" + event.name
	}
	want := []string{
		"loaded:loader-parent.cna",
		"loaded:loader-child.cna",
		"unloaded:loader-child.cna",
		"unloaded:loader-parent.cna",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ScriptLoader lifecycle events = %v, want %v", got, want)
	}
}

func TestParentUnloadWaitsForOwnedScriptLoaderChildLifecycle(t *testing.T) {
	childUnloaded := make(chan struct{})
	releaseChild := make(chan struct{})
	parentUnloaded := make(chan struct{})
	var childOnce sync.Once
	var parentOnce sync.Once
	observer := &recordingScriptLifecycle{
		unloaded: func(_ context.Context, script *Script) error {
			switch script.Program().Source().Name {
			case "loader-child-blocked.cna":
				childOnce.Do(func() { close(childUnloaded) })
				<-releaseChild
			case "loader-parent-blocked.cna":
				parentOnce.Do(func() { close(parentUnloaded) })
			}
			return nil
		},
	}
	runtime, err := New(WithScriptLifecycleObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loader-parent-blocked.cna", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "loader-child-blocked.cna", 'return 7;', $null];
return [$child runScript];
`)
	if err != nil {
		t.Fatal(err)
	}

	type executeResult struct {
		value Value
		err   error
	}
	result := make(chan executeResult, 1)
	go func() {
		value, executeErr := runtime.Execute(context.Background(), program)
		result <- executeResult{value: value, err: executeErr}
	}()

	select {
	case <-childUnloaded:
	case <-time.After(5 * time.Second):
		close(releaseChild)
		t.Fatal("child ScriptUnloaded observer did not start")
	}
	select {
	case <-parentUnloaded:
		close(releaseChild)
		t.Fatal("parent ScriptUnloaded ran before its owned child observer completed")
	case <-result:
		close(releaseChild)
		t.Fatal("Execute returned before the owned child lifecycle completed")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseChild)

	select {
	case got := <-result:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.value.Int64() != 7 {
			t.Fatalf("Execute result = %s, want 7", got.value.Describe())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not finish after releasing child lifecycle observer")
	}
	select {
	case <-parentUnloaded:
	default:
		t.Fatal("parent ScriptUnloaded observer did not run")
	}
}

func TestForkDoesNotReinstallGlobalsOrEmitLifecycle(t *testing.T) {
	observer := &recordingScriptLifecycle{}
	runtime, err := New(
		WithInitialGlobals(map[string]Value{"seed": String("parent")}),
		WithScriptLifecycleObserver(observer),
	)
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("fork.cna", `
$handle = fork({ return $seed; });
return wait($handle);
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsNull() {
		t.Fatalf("fork inherited initial scalar: %s", result.Describe())
	}
	if got := lifecyclePhases(observer.snapshot()); !reflect.DeepEqual(got, []string{"loaded", "unloaded"}) {
		t.Fatalf("fork lifecycle phases = %v, want parent load/unload only", got)
	}
}

func lifecyclePhases(events []scriptLifecycleEvent) []string {
	result := make([]string, len(events))
	for index, event := range events {
		result[index] = event.phase
	}
	return result
}
