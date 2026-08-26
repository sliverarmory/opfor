package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPortableScriptLoaderCanonicalProcess(t *testing.T) {
	programData, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "process.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "process.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("process.sl", programData))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	programRoot, err := filepath.Abs(filepath.Join("testdata", "upstream", "sleep-2.1", "programs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(programRoot)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func TestPortableScriptLoaderDefersExecutionAndRepeats(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "nested.sl"), []byte(`$nested = "included";`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "child.sl"), []byte(`include("nested.sl"); println("child " . $nested . " " . getFileName($__INCLUDE__) . " " . getFileName($__SCRIPT__));`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loader.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$script = [$loader loadScript: "child.sl"];
$buffer = allocate();
[sleep.bridges.io.IOObject setConsole: [$script getScriptEnvironment], $buffer];
[$script runScript];
[$script runScript];
closef($buffer);
return readb($buffer, available($buffer));
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var parentOutput bytes.Buffer
	runtime, err := New(WithStdout(&parentOutput), WithStderr(&parentOutput))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(directory)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, parentOutput.String())
	}
	if got, want := result.String(), "child included nested.sl child.sl\nchild included nested.sl child.sl\n"; got != want {
		t.Fatalf("redirected child output = %q, want %q", got, want)
	}
	if got := parentOutput.String(); got != "" {
		t.Fatalf("loadScript executed before setConsole or leaked child output: %q", got)
	}
}

func TestPortableScriptLoaderCompileErrorsUseCallerErrorSlot(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "broken.sl"), []byte("if (\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("compile-error.sl", `
import sleep.error.YourCodeSucksException;
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$value = [$loader loadScript: "broken.sl"];
checkError($problem);
return @($value, $problem isa ^YourCodeSucksException, [$problem formatErrors], $problem);
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(directory)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if len(values) != 4 {
		t.Fatalf("result length = %d, want 4", len(values))
	}
	if !values[0].IsNull() {
		t.Errorf("loadScript result = %s, want null", values[0].Describe())
	}
	if !values[1].Truth() {
		t.Errorf("compile error isa YourCodeSucksException = false")
	}
	if formatted := values[2].String(); !strings.Contains(formatted, "Error:") || !strings.Contains(formatted, "at line") {
		t.Errorf("formatErrors = %q, want formatted source diagnostics", formatted)
	}
	if summary := values[3].String(); !strings.HasPrefix(summary, "YourCodeSucksException:") {
		t.Errorf("compile error = %q, want YourCodeSucksException summary", summary)
	}
}

func TestPortableScriptLoaderWarningWatcherReceivesScriptWarning(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "child-warning.sl"), []byte("missing_bridge(); println('after');\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("warning-loader.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$script = [$loader loadScript: "child-warning.sl"];
[$script addWarningWatcher: { println("*** $1"); }];
$buffer = allocate();
[sleep.bridges.io.IOObject setConsole: [$script getScriptEnvironment], $buffer];
[$script runScript];
closef($buffer);
return readb($buffer, available($buffer));
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(directory)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}
	if got, want := result.String(), "after\n"; got != want {
		t.Fatalf("child console = %q, want %q", got, want)
	}
	wantWarning := "*** Warning: Attempted to call non-existent function &missing_bridge at child-warning.sl:1\n"
	if got := output.String(); got != wantWarning {
		t.Fatalf("watcher output = %q, want %q", got, wantWarning)
	}
}

func TestPortableScriptLoaderKeepsImporterObjectHostFirstRefusal(t *testing.T) {
	t.Run("handled", func(t *testing.T) {
		var calls int
		runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			if invocation.Op == ObjectConstruct && invocation.Class == "sleep.runtime.ScriptLoader" {
				calls++
				return String("importer-loader"), nil
			}
			return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
		})))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		result, err := runtime.Eval(context.Background(), "first-refusal.sl", `import sleep.runtime.ScriptLoader; return [new ScriptLoader];`)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		if got, want := result.String(), "importer-loader"; got != want {
			t.Fatalf("result = %q, want %q", got, want)
		}
		if calls != 1 {
			t.Fatalf("importer calls = %d, want 1", calls)
		}
	})

	t.Run("fatal", func(t *testing.T) {
		fatal := errors.New("importer refused ScriptLoader fatally")
		runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			if invocation.Op == ObjectConstruct && invocation.Class == "sleep.runtime.ScriptLoader" {
				return Null(), fatal
			}
			return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
		})))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_, err = runtime.Eval(context.Background(), "fatal-first-refusal.sl", `import sleep.runtime.ScriptLoader; return [new ScriptLoader];`)
		if !errors.Is(err, fatal) {
			t.Fatalf("Eval error = %v, want importer fatal error", err)
		}
	})
}

func TestPortableScriptLoaderRepeatPreservesGlobalsRegistryAndLiveConsole(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "counter.sl")
	if err := os.WriteFile(childPath, []byte(`$counter++; println("child=" . $counter); return $counter;`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loader-repeat.sl", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
import sleep.bridges.io.IOObject;
$loader = [new ScriptLoader];
$script = [$loader loadScript: %q];
$before = @([$loader isLoaded: %q], [[$loader getScripts] size], [[$loader getScriptsByKey] containsKey: %q], [$script isLoaded], [$script getDebugFlags]);
$first = allocate();
[IOObject setConsole: [$script getScriptEnvironment], $first];
$r1 = [$script runScript];
$second = allocate();
[IOObject setConsole: [$script getScriptEnvironment], $second];
$r2 = [$script runScript];
[$loader unloadScript: $script];
$after = @([$loader isLoaded: %q], [[$loader getScripts] size], [$script isLoaded]);
$r3 = [$script runScript];
closef($first);
closef($second);
return @($before, $r1, $r2, $after, $r3, readb($first, available($first)), readb($second, available($second)), [IOObject getConsole: [$script getScriptEnvironment]] == $second);
`, filepath.ToSlash(childPath), filepath.ToSlash(childPath), filepath.ToSlash(childPath), filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New(WithDebugFlags(3))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	values := mustArrayValues(t, result)
	if len(values) != 8 {
		t.Fatalf("result length = %d, want 8", len(values))
	}
	before := mustArrayValues(t, values[0])
	for index, value := range before[:4] {
		if !value.Truth() {
			t.Errorf("before[%d] = %s, want true", index, value.Describe())
		}
	}
	if got := before[4].Int32(); got != 3 {
		t.Errorf("initial child debug flags = %d, want 3", got)
	}
	if got, want := values[1].Int32(), int32(1); got != want {
		t.Errorf("first run = %d, want %d", got, want)
	}
	if got, want := values[2].Int32(), int32(2); got != want {
		t.Errorf("second run = %d, want %d", got, want)
	}
	after := mustArrayValues(t, values[3])
	for index, value := range after {
		if value.Truth() {
			t.Errorf("after[%d] = %s, want false/zero", index, value.Describe())
		}
	}
	if got, want := values[4].Int32(), int32(3); got != want {
		t.Errorf("post-unload run = %d, want %d", got, want)
	}
	if got, want := values[5].String(), "child=1\n"; got != want {
		t.Errorf("first console = %q, want %q", got, want)
	}
	if got, want := values[6].String(), "child=2\nchild=3\n"; got != want {
		t.Errorf("replacement console = %q, want %q", got, want)
	}
	if !values[7].Truth() {
		t.Errorf("IOObject.getConsole did not return the live replacement")
	}
}

func TestPortableScriptLoaderRetainedClosureRunsAfterUnload(t *testing.T) {
	program, err := CompileString("loader-retained-closure.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "retained-closure-child.sl", 'if ($saved is $null) { $saved = lambda({ $counter++; return $counter; }); } return [$saved];', $null];
$first = [$child runScript];
[$loader unloadScript: $child];
$second = [$child runScript];
return @($first, $second, [$child isLoaded]);
`)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	values := mustArrayValues(t, result)
	if len(values) != 3 || values[0].Int32() != 1 || values[1].Int32() != 2 || values[2].Truth() {
		t.Fatalf("retained closure result = %s, want @(1, 2, false)", result.Describe())
	}
}

func TestPortableScriptLoaderInheritsEmbeddingFunctionsAndParentUnloadCancelsChild(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "blocking-child.sl")
	if err := os.WriteFile(childPath, []byte(`sub child_owned { return; } return embedded_block();`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loader-owner.sl", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
sub run_child { return [$child runScript]; }
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	started := make(chan struct{})
	var startOnce sync.Once
	observer := &scriptLoaderBindingObserver{}
	runtime, err := New(WithBindingObserver(observer), WithFunction("embedded_block", func(ctx context.Context, _ Invocation) (Value, error) {
		startOnce.Do(func() { close(started) })
		<-ctx.Done()
		return Null(), ctx.Err()
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	parent, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	parent.mu.RLock()
	var loader *portableScriptLoader
	for candidate := range parent.scriptLoaders {
		loader = candidate
		break
	}
	parent.mu.RUnlock()
	if loader == nil {
		t.Fatal("parent did not retain its ScriptLoader")
	}
	loader.mu.Lock()
	if len(loader.loaded) != 1 {
		loader.mu.Unlock()
		t.Fatalf("loaded child count = %d, want 1", len(loader.loaded))
	}
	child := loader.loaded[0]
	loader.mu.Unlock()

	callDone := make(chan error, 1)
	go func() {
		_, callErr := parent.Call(context.Background(), "run_child")
		callDone <- callErr
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("child did not inherit or invoke the embedding function")
	}
	if !observer.registeredName("child_owned") {
		t.Fatal("ScriptLoader child did not inherit the parent binding observer")
	}
	unloadContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := parent.Unload(unloadContext); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	select {
	case <-callDone:
	case <-time.After(2 * time.Second):
		t.Fatal("parent unload did not cancel the active ScriptLoader child")
	}
	child.stateMu.Lock()
	closing := child.closing
	child.stateMu.Unlock()
	child.runMu.Lock()
	childRuntime := child.child
	child.runMu.Unlock()
	if !closing || childRuntime != nil {
		t.Fatalf("child cleanup = closing %v runtime %p, want true/nil", closing, childRuntime)
	}
	if !observer.unregisteredName("child_owned") {
		t.Fatal("parent unload did not unregister the ScriptLoader child's bindings")
	}
}

func TestPortableScriptLoaderInheritsEmbeddingCoreOverride(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "override-child.sl")
	if err := os.WriteFile(childPath, []byte(`println("child override"); return 9;`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loader-override.sl", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
return [$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var mu sync.Mutex
	var calls []string
	runtime, err := New(WithFunction("println", func(_ context.Context, invocation Invocation) (Value, error) {
		mu.Lock()
		calls = append(calls, invocation.Arg(0).String())
		mu.Unlock()
		return Null(), nil
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := result.Int32(), int32(9); got != want {
		t.Fatalf("result = %d, want %d", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != "child override" {
		t.Fatalf("override calls = %#v, want [child override]", calls)
	}
}

func TestPortableScriptLoaderInheritsAggressorEncodingAndDispatchBoundaries(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "aggressor-boundary-child.cna")
	childSource := `
$ran = 0;
$packed = bof_pack("child-beacon", "z", "child-text");
$dispatched = dispatch_event({ $ran++; });
return @($packed, $dispatched, $ran);
`
	if err := os.WriteFile(childPath, []byte(childSource), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("aggressor-boundary-parent.sl", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
return [$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}

	encoderCalls := 0
	dispatcherCalls := 0
	runtimeInstance, err := New(
		WithBeaconStringEncoder(BeaconStringEncoderFunc(func(_ context.Context, beaconID Value, text Value) ([]byte, error) {
			encoderCalls++
			if beaconID.String() != "child-beacon" || text.String() != "child-text" {
				return nil, fmt.Errorf("unexpected encoder arguments %s/%s", beaconID.Describe(), text.Describe())
			}
			return []byte("encoded"), nil
		})),
		WithAggressorEventDispatcher(AggressorEventDispatcherFunc(func(ctx context.Context, callback Callable) error {
			dispatcherCalls++
			_, err := callback.Invoke(ctx)
			return err
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	values := mustArrayValues(t, result)
	wantPacked := []byte{0, 0, 0, 8, 'e', 'n', 'c', 'o', 'd', 'e', 'd', 0}
	packed, ok := values[0].Bytes()
	if !ok || !values[0].IsBinaryString() || !bytes.Equal(packed, wantPacked) {
		t.Fatalf("child bof_pack = %x/binary=%v, want %x/binary", packed, values[0].IsBinaryString(), wantPacked)
	}
	if !values[1].IsNull() || values[2].Int32() != 1 {
		t.Fatalf("child dispatch result/calls = %s/%s, want $null/1", values[1].Describe(), values[2].Describe())
	}
	if encoderCalls != 1 || dispatcherCalls != 1 {
		t.Fatalf("inherited boundary calls = encoder:%d dispatcher:%d, want 1/1", encoderCalls, dispatcherCalls)
	}
}

func TestPortableScriptLoaderCompileLoadAndEnvironmentOverloads(t *testing.T) {
	directory := t.TempDir()
	streamPath := filepath.Join(directory, "stream.sl")
	if err := os.WriteFile(streamPath, []byte("$streamCounter++;\nreturn $streamCounter + 20;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$block = [$loader compileScript: "direct-name", '$counter++; return $counter;'];
$script = [$loader loadScript: "compiled-name", $block, $null];
$env = [$script getScriptEnvironment];
$vars = [$env getScriptVariables];
[$env evaluateStatement: '$seed = 41; $other = 2; $before = 9;'];
$values = @([$block getSource], [$block getApproximateLineRange], [$env getScalar: '$seed'], [$vars getScalar: '$other'], [$env evaluateExpression: '$seed + $other'], [$env evaluatePredicate: '$seed > $other'], [$env evaluateParsedLiteral: 'x=$before']);
[$env flagError: "oops"];
$errors = @([$env checkError], [$env checkError]);
$runs = @([$script runScript], [$script runScript]);
$streamHandle = openf(%q);
$stream = [$streamHandle getInputStream];
$streamBlock = [$loader compileScript: "stream-name", $stream];
$streamScript = [$loader loadScript: "stream-script", $streamBlock, $null];
$sourceScript = [$loader loadScript: "source-script", 'return 17;', $null];
$directHandle = openf(%q);
$directStreamScript = [$loader loadScript: "direct-stream", [$directHandle getInputStream]];
$noReference = [$loader loadScriptNoReference: "no-reference", $block, $null];
return @($values, $errors, $runs, [$streamBlock getSource], [$streamScript runScript], [$sourceScript runScript], [$directStreamScript runScript], [$loader isLoaded: "no-reference"], [$noReference isLoaded], [$noReference runScript], $stream is [$streamHandle getInputStream]);
`, filepath.ToSlash(streamPath), filepath.ToSlash(streamPath))
	program, err := CompileString("loader-overloads.sl", source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	values := mustArrayValues(t, result)
	if len(values) != 11 {
		t.Fatalf("result length = %d, want 11", len(values))
	}
	metadata := mustArrayValues(t, values[0])
	wantMetadata := []string{"direct-name", "0", "41", "2", "43", "1", "x=9"}
	for index, want := range wantMetadata {
		if got := metadata[index].String(); got != want {
			t.Errorf("metadata[%d] = %q, want %q", index, got, want)
		}
	}
	errors := mustArrayValues(t, values[1])
	// Sleep evaluates object-expression arguments from right to left: the
	// second check consumes the pending error before the first is evaluated.
	if got := errors[0].String(); got != "" {
		t.Errorf("first rendered checkError = %q, want empty", got)
	}
	if got := errors[1].String(); got != "oops" {
		t.Errorf("second rendered checkError = %q, want oops", got)
	}
	runs := mustArrayValues(t, values[2])
	if got, want := []int32{runs[0].Int32(), runs[1].Int32()}, []int32{2, 1}; got[0] != want[0] || got[1] != want[1] {
		t.Errorf("runs = %v, want %v", got, want)
	}
	wantTail := []string{"stream-name", "21", "17", "21", "0", "1", "1", "1"}
	for index, want := range wantTail {
		if got := values[index+3].String(); got != want {
			t.Errorf("tail[%d] = %q, want %q", index, got, want)
		}
	}
}

func TestPortableScriptLoaderCharsetControls(t *testing.T) {
	directory := t.TempDir()
	windowsPath := filepath.Join(directory, "windows-1252.sl")
	rawPath := filepath.Join(directory, "raw.sl")
	invalidPath := filepath.Join(directory, "invalid.sl")
	if err := os.WriteFile(windowsPath, []byte("return '\x80';\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawPath, []byte("return '\xff';\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidPath, []byte("return 1;\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	source := fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$defaults = @([$loader getCharset] is $null, [$loader isCharsetConversions]);
[$loader setCharset: "windows-1252"];
$windowsHandle = openf(%q);
$windowsBlock = [$loader compileScript: "windows-source", [$windowsHandle getInputStream]];
$windows = [$loader loadScript: "windows-child", $windowsBlock, $null];
$selected = @([$loader getCharset], [$windows runScript]);
[$loader setCharsetConversion: 0];
$rawHandle = openf(%q);
$rawBlock = [$loader compileScript: "raw-source", [$rawHandle getInputStream]];
$raw = [$loader loadScript: "raw-child", $rawBlock, $null];
$disabled = @([$loader isCharsetConversions], [$raw runScript]);
[$loader setCharsetConversion: 1];
[$loader setCharset: "not-a-portable-charset"];
$invalidHandle = openf(%q);
$invalidBlock = [$loader compileScript: "invalid-source", [$invalidHandle getInputStream]];
$invalid = [$loader loadScript: "invalid-child", $invalidBlock, $null];
[$loader setCharset: $null];
return @($defaults, $selected, $disabled, [$invalid runScript], [$loader getCharset] is $null);
`, filepath.ToSlash(windowsPath), filepath.ToSlash(rawPath), filepath.ToSlash(invalidPath))

	program, err := CompileString("loader-charsets.sl", source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var diagnostics bytes.Buffer
	runtime, err := New(WithStderr(&diagnostics))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	values := mustArrayValues(t, result)
	if len(values) != 5 {
		t.Fatalf("result length = %d, want 5", len(values))
	}
	defaults := mustArrayValues(t, values[0])
	if defaults[0].Truth() || !defaults[1].Truth() {
		t.Fatalf("defaults = %s/%s, want Java-null empty scalar and enabled conversions", defaults[0].Describe(), defaults[1].Describe())
	}
	selected := mustArrayValues(t, values[1])
	if got := selected[0].String(); got != "windows-1252" {
		t.Errorf("selected charset = %q, want windows-1252", got)
	}
	if got := selected[1].String(); got != "€" {
		t.Errorf("Windows-1252 result = %q, want euro sign", got)
	}
	disabled := mustArrayValues(t, values[2])
	if disabled[0].Truth() {
		t.Error("isCharsetConversions remained true after disabling conversions")
	}
	if got := disabled[1].String(); got != "ÿ" {
		t.Errorf("NoConversion result = %q, want U+00FF", got)
	}
	if got := values[3].Int32(); got != 1 {
		t.Errorf("unsupported charset fallback result = %d, want 1", got)
	}
	if got := diagnostics.String(); !strings.Contains(got, "java.io.UnsupportedEncodingException: not-a-portable-charset") {
		t.Errorf("unsupported charset diagnostic = %q", got)
	}
	if values[4].Truth() {
		t.Errorf("setCharset(null) getCharset identity = %s, want Java-null empty scalar", values[4].Describe())
	}
}

func TestOfficialSleepPortableScriptLoaderCharsetControls(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for ScriptLoader charset differential verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java = "java"
	}

	directory := t.TempDir()
	windowsPath := filepath.Join(directory, "windows-1252.sl")
	rawPath := filepath.Join(directory, "raw.sl")
	mainPath := filepath.Join(directory, "charset-loader.sl")
	if err := os.WriteFile(windowsPath, []byte("return '\x80';\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawPath, []byte("return '\xff';\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
println("default=" . ([$loader getCharset] is $null) . "/" . [$loader isCharsetConversions]);
[$loader setCharset: "windows-1252"];
$windowsHandle = openf(%q);
$windowsBlock = [$loader compileScript: "windows-source", [$windowsHandle getInputStream]];
$windows = [$loader loadScript: "windows-child", $windowsBlock, $null];
println("selected=" . [$loader getCharset] . "/" . [$windows runScript]);
[$loader setCharsetConversion: 0];
$rawHandle = openf(%q);
$rawBlock = [$loader compileScript: "raw-source", [$rawHandle getInputStream]];
$raw = [$loader loadScript: "raw-child", $rawBlock, $null];
println("disabled=" . [$loader isCharsetConversions] . "/" . [$raw runScript]);
[$loader setCharset: $null];
println("reset=" . ([$loader getCharset] is $null));
`, filepath.ToSlash(windowsPath), filepath.ToSlash(rawPath))
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	command := osexec.Command(java, "-Dfile.encoding=UTF-8", "-jar", jar, mainPath)
	reference, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep: %v\n%s", err, reference)
	}
	program, err := CompileString(mainPath, source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official ScriptLoader charset mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}

func TestPortableScriptLoaderConfiguredSetDeltas(t *testing.T) {
	program, err := CompileString("loader-set-deltas.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$block = [$loader compileScript: "body", 'return 1;'];
[$loader loadScript: "a", $block, $null];
[$loader loadScript: "b", $block, $null];
$configured = [new LinkedHashSet];
[$configured add: "b"];
[$configured add: "c"];
$toLoad = [$loader getScriptsToLoad: $configured];
$toUnload = [$loader getScriptsToUnload: $configured];
return @([$toLoad toArray], [$toUnload toArray], $toLoad isa ^java.util.Set, $toUnload isa ^java.util.Set);
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	values := mustArrayValues(t, result)
	if len(values) != 4 {
		t.Fatalf("result length = %d, want 4", len(values))
	}
	toLoad := mustArrayValues(t, values[0])
	toUnload := mustArrayValues(t, values[1])
	if len(toLoad) != 1 || toLoad[0].String() != "c" {
		t.Errorf("getScriptsToLoad = %#v, want [c]", toLoad)
	}
	if len(toUnload) != 1 || toUnload[0].String() != "a" {
		t.Errorf("getScriptsToUnload = %#v, want [a]", toUnload)
	}
	if !values[2].Truth() || !values[3].Truth() {
		t.Errorf("delta result types = %s/%s, want Set/Set", values[2].Describe(), values[3].Describe())
	}
}

func TestOfficialSleepPortableScriptLoaderConfiguredSetDeltas(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for ScriptLoader set-delta differential verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java = "java"
	}
	source := `import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$block = [$loader compileScript: "body", 'return 1;'];
[$loader loadScript: "a", $block, $null];
[$loader loadScript: "b", $block, $null];
$configured = [new LinkedHashSet];
[$configured add: "b"];
[$configured add: "c"];
println("load=" . [$loader getScriptsToLoad: $configured]);
println("unload=" . [$loader getScriptsToUnload: $configured]);
`
	directory := t.TempDir()
	mainPath := filepath.Join(directory, "set-deltas.sl")
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(java, "-Dfile.encoding=UTF-8", "-jar", jar, mainPath)
	reference, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep: %v\n%s", err, reference)
	}
	program, err := CompileString(mainPath, source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official ScriptLoader set-delta mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}

func TestPortableScriptLoaderRejectsUnsupportedMutableState(t *testing.T) {
	program, err := CompileString("loader-rejections.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$script = [$loader loadScript: "child", 'return 1;', $null];
$variables = [[$script getScriptEnvironment] getScriptVariables];
[$variables putScalar: '$value', 1];
checkError($mutable);
return @($mutable isa ^java.lang.UnsupportedOperationException, $mutable);
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	values := mustArrayValues(t, result)
	if !values[0].Truth() {
		t.Fatalf("typed rejection check = %s, want true", values[0].Describe())
	}
	if got := values[1].String(); !strings.Contains(got, "mutable ScriptVariables operations") {
		t.Errorf("mutable rejection = %q", got)
	}
}

func TestPortableScriptLoaderLiveRegistryViews(t *testing.T) {
	program, err := CompileString("loader-live-registries.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$scripts = [$loader getScripts];
$bykey = [$loader getScriptsByKey];
$block = [$loader compileScript: "body", 'return 1;'];
$a = [$loader loadScript: "a", $block, $null];
$b = [$loader loadScript: "b", $block, $null];
$before = @([$scripts size], [$bykey size], [[$scripts get: 0] getName], [[$bykey get: "b"] getName]);
[$scripts clear];
$after_list = @([[$loader getScripts] size], [[$loader getScriptsByKey] size], [$loader isLoaded: "a"]);
[$bykey remove: "a"];
$after_map = @([[$loader getScripts] size], [[$loader getScriptsByKey] size], [$loader isLoaded: "a"], [$loader isLoaded: "b"]);
[$bykey put: "alias", $a];
$after_put = @([[$loader getScriptsByKey] size], [$loader isLoaded: "alias"], [[[$loader getScriptsByKey] get: "alias"] getName]);
return @($before, $after_list, $after_map, $after_put);
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	groups := mustArrayValues(t, result)
	want := [][]string{
		{"2", "2", "a", "b"},
		{"0", "2", "1"},
		{"0", "1", "0", "1"},
		{"2", "1", "a"},
	}
	if len(groups) != len(want) {
		t.Fatalf("group count = %d, want %d", len(groups), len(want))
	}
	for groupIndex, group := range groups {
		values := mustArrayValues(t, group)
		if len(values) != len(want[groupIndex]) {
			t.Fatalf("group %d length = %d, want %d", groupIndex, len(values), len(want[groupIndex]))
		}
		for valueIndex, value := range values {
			if got := value.String(); got != want[groupIndex][valueIndex] {
				t.Errorf("group %d value %d = %q, want %q", groupIndex, valueIndex, got, want[groupIndex][valueIndex])
			}
		}
	}
}

func TestPortableScriptLoaderKeyRegistryMaintainsLiveEntryNode(t *testing.T) {
	program, err := CompileString("loader-live-entry.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$first_block = [$loader compileScript: "first-body", 'return 1;'];
$first = [$loader loadScript: "same", $first_block, $null];
$iterator = [[[$loader getScriptsByKey] entrySet] iterator];
$entry = [$iterator next];
$second_block = [$loader compileScript: "second-body", 'return 2;'];
$second = [$loader loadScript: "same", $second_block, $null];
$updated = [[$entry getValue] runScript];
[$entry setValue: $first];
$restored = [[[$loader getScriptsByKey] get: "same"] runScript];
return @($updated, $restored, [$entry getKey]);
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	values := mustArrayValues(t, result)
	if got := argvValueStrings(values); len(got) != 3 || got[0] != "2" || got[1] != "1" || got[2] != "same" {
		t.Fatalf("live entry values = %q, want [2 1 same]", got)
	}
}

func TestPortableScriptLoaderRegistryViewIdentity(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loader := &portableScriptLoader{
		runtime:       runtime,
		loadedScripts: newPortableJavaCollection("LinkedList", nil),
		scriptsByKey:  newPortableJavaMap("HashMap", nil),
	}
	for _, method := range []string{"getScripts", "getScriptsByKey"} {
		first, handled, err := loader.invoke(context.Background(), ObjectInvocation{Op: ObjectInvoke, Message: method})
		if err != nil || !handled {
			t.Fatalf("first %s: handled=%t err=%v", method, handled, err)
		}
		second, handled, err := loader.invoke(context.Background(), ObjectInvocation{Op: ObjectInvoke, Message: method})
		if err != nil || !handled {
			t.Fatalf("second %s: handled=%t err=%v", method, handled, err)
		}
		if !first.IdentityEqual(second) {
			t.Errorf("%s returned distinct registry objects", method)
		}
	}
}

func TestPortableScriptLoaderLiveRegistriesConcurrent(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := runtime.CompileString("concurrent-child.sl", "return 1;")
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	loader := &portableScriptLoader{
		runtime:       runtime,
		loadedScripts: newPortableJavaCollection("LinkedList", nil),
		scriptsByKey:  newPortableJavaMap("HashMap", nil),
		instances:     make(map[*portableScriptInstance]struct{}),
	}
	const workers = 8
	const perWorker = 16
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(worker int) {
			defer wait.Done()
			for index := 0; index < perWorker; index++ {
				name := fmt.Sprintf("child-%02d-%02d", worker, index)
				value := loader.registerScript(ObjectInvocation{Runtime: runtime}, name, program, "", true, nil)
				if value.IsNull() {
					t.Errorf("registerScript(%q) returned null", name)
					return
				}
				if _, _, invokeErr := loader.loadedScripts.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "size"}); invokeErr != nil {
					t.Errorf("list size: %v", invokeErr)
					return
				}
				if _, _, invokeErr := loader.scriptsByKey.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "entrySet"}); invokeErr != nil {
					t.Errorf("map entrySet: %v", invokeErr)
					return
				}
			}
		}(worker)
	}
	wait.Wait()
	listSize, _, err := loader.loadedScripts.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "size"})
	if err != nil {
		t.Fatal(err)
	}
	mapSize, _, err := loader.scriptsByKey.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "size"})
	if err != nil {
		t.Fatal(err)
	}
	want := int32(workers * perWorker)
	if listSize.Int32() != want || mapSize.Int32() != want {
		t.Fatalf("concurrent registry sizes = %d/%d, want %d/%d", listSize.Int32(), mapSize.Int32(), want, want)
	}
}

func TestPortableScriptLoaderSharedHashtableAndGlobalBridgeOnce(t *testing.T) {
	program, err := CompileString("loader-shared-environment.sl", `
import sleep.runtime.ScriptLoader;
import java.util.Hashtable;
$loader = [new ScriptLoader];
$environment = [new Hashtable];
$first = [$loader loadScript: "first", 'sub shared_value { return "shared"; } return "first";', $environment];
$same_environment = [[[$first getScriptEnvironment] getEnvironment] containsKey: "(isloaded)"];
$first_result = [$first runScript];
[$environment remove: "&println"];
$second = [$loader loadScript: "second", 'return shared_value();', $environment];
$not_reinstalled = 1 - [$environment containsKey: "&println"];
$second_result = [$second runScript];
[$environment remove: "(isloaded)"];
$third = [$loader loadScript: "third", 'return shared_value();', $environment];
$reinstalled = [$environment containsKey: "&println"];
$hash_environment = %();
$bad = [$loader loadScript: "bad", 'return 9;', $hash_environment];
return @($same_environment, $first_result, $second_result, $not_reinstalled, $reinstalled, $bad, [$loader isLoaded: "bad"], $environment isa ^java.util.Hashtable);
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var warnings bytes.Buffer
	runtime, err := New(WithStderr(&warnings))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	values := mustArrayValues(t, result)
	want := []string{"1", "first", "shared", "1", "1", "", "0", "1"}
	if len(values) != len(want) {
		t.Fatalf("values = %d, want %d", len(values), len(want))
	}
	for index, value := range values {
		if got := value.String(); got != want[index] {
			t.Errorf("value %d = %q, want %q", index, got, want[index])
		}
	}
	if got := warnings.String(); !strings.Contains(got, "there is no method that matches loadScript") || !strings.Contains(got, "%()") {
		t.Errorf("type-exact Hashtable warning = %q", got)
	}
}

func TestPortableScriptLoaderSharedHashtableRetainsLoadableProviderFunction(t *testing.T) {
	var mu sync.Mutex
	var requests []LoadableRequest
	provider := LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		return LoadableBridgeFuncs{Loaded: func(_ context.Context, script *Script) error {
			return script.RegisterFunction("shared_provider_value", func(_ context.Context, invocation Invocation) (Value, error) {
				return Long(int64(invocation.Runtime.ID())), nil
			})
		}}, nil
	})
	runtime, err := New(WithLoadableProvider(provider))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := CompileString("loader-shared-provider.sl", `
import sleep.runtime.ScriptLoader;
import java.util.Hashtable;
$loader = [new ScriptLoader];
$environment = [new Hashtable];
$first = [$loader loadScript: "first", 'use("example.SharedBridge"); return shared_provider_value();', $environment];
$first_result = [$first runScript];
$second = [$loader loadScript: "second", 'return shared_provider_value();', $environment];
$second_result = [$second runScript];
return @($first_result, $second_result);
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	values := mustArrayValues(t, result)
	if len(values) != 2 || values[0].Int64() == 0 || values[0].Int64() != values[1].Int64() || RuntimeID(values[0].Int64()) == runtime.ID() {
		t.Fatalf("shared provider results = %s, want identical non-parent child RuntimeIDs", result.Describe())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 || requests[0].RuntimeID != RuntimeID(values[0].Int64()) || requests[0].ClassName != "example.SharedBridge" {
		t.Fatalf("Loadable requests = %#v", requests)
	}
}

func TestOfficialSleepPortableScriptLoaderLiveViewsAndSharedHashtable(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for ScriptLoader live-view/shared-environment verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java = "java"
	}
	directory := t.TempDir()
	mainPath := filepath.Join(directory, "loader-live-shared.sl")
	source := `import sleep.runtime.ScriptLoader;
import java.util.Hashtable;
$loader = [new ScriptLoader];
$scripts = [$loader getScripts];
$bykey = [$loader getScriptsByKey];
$block = [$loader compileScript: "body", 'return 1;'];
$a = [$loader loadScript: "a", $block, $null];
$b = [$loader loadScript: "b", $block, $null];
println("registries=" . [$scripts size] . "/" . [$bykey size] . "/" . [[$scripts get: 0] getName] . "/" . [[$bykey get: "b"] getName]);
println("keys=" . [$bykey containsKey: "a"] . "/" . [$bykey containsKey: "b"] . "/" . (1 - [$bykey containsKey: "missing"]));
[$scripts clear];
println("list=" . [[$loader getScripts] size] . "/" . [[$loader getScriptsByKey] size] . "/" . [$loader isLoaded: "a"]);
[$bykey remove: "a"];
println("map=" . [[$loader getScripts] size] . "/" . [[$loader getScriptsByKey] size] . "/" . [$loader isLoaded: "a"] . "/" . [$loader isLoaded: "b"]);
$environment = [new Hashtable];
$first = [$loader loadScript: "first", 'sub shared_value { return "shared"; } return "first";', $environment];
$marker = [$environment containsKey: "(isloaded)"];
$first_result = [$first runScript];
[$environment remove: "&println"];
$second = [$loader loadScript: "second", 'return shared_value();', $environment];
$not_reinstalled = 1 - [$environment containsKey: "&println"];
$second_result = [$second runScript];
[$environment remove: "(isloaded)"];
$third = [$loader loadScript: "third", 'return shared_value();', $environment];
$reinstalled = [$environment containsKey: "&println"];
println("shared=" . $first_result . "/" . $second_result . "/" . $marker . "/" . $not_reinstalled . "/" . $reinstalled);
`
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(java, "-jar", jar, mainPath)
	reference, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep: %v\n%s", err, reference)
	}
	program, err := CompileString(mainPath, source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official ScriptLoader live/shared mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}

func TestPortableScriptLoaderFilesystemChangeTracking(t *testing.T) {
	loadTime := time.Now().Truncate(time.Millisecond)
	oldTime := loadTime.Add(-2 * time.Second)
	changedTime := loadTime.Add(2 * time.Second)
	clock := ClockFunc(func() time.Time { return loadTime })

	t.Run("main file with whitespace", func(t *testing.T) {
		root := t.TempDir()
		name := " child script.sl "
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("return 7;\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mustSetScriptLoaderModTime(t, path, oldTime)

		runtime, err := New(WithClock(clock))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtime.Close(context.Background()) })
		if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
			t.Fatal(err)
		}
		parent, _ := mustLoadPortableChangeTracker(t, runtime, name)
		if portableChangeTrackerChanged(t, parent) {
			t.Fatal("fresh main file reported changed")
		}
		mustSetScriptLoaderModTime(t, path, changedTime)
		if !portableChangeTrackerChanged(t, parent) {
			t.Fatal("newer main file did not report changed")
		}
		mustSetScriptLoaderModTime(t, path, oldTime)
		if portableChangeTrackerChanged(t, parent) {
			t.Fatal("older replacement timestamp reported changed")
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if portableChangeTrackerChanged(t, parent) {
			t.Fatal("deleted source reported changed; Sleep treats missing lastModified as zero")
		}
	})

	t.Run("relative include after runtime chdir", func(t *testing.T) {
		root := t.TempDir()
		dependencyDirectory := filepath.Join(root, " dependency directory ")
		if err := os.Mkdir(dependencyDirectory, 0o700); err != nil {
			t.Fatal(err)
		}
		dependencyName := " included source.sl "
		dependencyPath := filepath.Join(dependencyDirectory, dependencyName)
		if err := os.WriteFile(dependencyPath, []byte("$included = 1;\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		childName := "include-child.sl"
		childPath := filepath.Join(root, childName)
		childSource := fmt.Sprintf("chdir(%q); include(%q); chdir('..'); return $included;\n", filepath.Base(dependencyDirectory), dependencyName)
		if err := os.WriteFile(childPath, []byte(childSource), 0o600); err != nil {
			t.Fatal(err)
		}
		mustSetScriptLoaderModTime(t, childPath, oldTime)
		mustSetScriptLoaderModTime(t, dependencyPath, oldTime)

		runtime, err := New(WithClock(clock), WithIncludeCyclePolicy(IncludeCycleAllow))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtime.Close(context.Background()) })
		if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
			t.Fatal(err)
		}
		parent, instance := mustLoadPortableChangeTracker(t, runtime, childName)
		if got := portableRunChangeTracker(t, parent).Int32(); got != 1 {
			t.Fatalf("child result = %d, want 1", got)
		}
		if portableChangeTrackerChanged(t, parent) {
			t.Fatal("unchanged included source reported changed")
		}
		instance.runMu.Lock()
		childRuntime := instance.child
		instance.runMu.Unlock()
		if childRuntime == nil || childRuntime.includeCycles != IncludeCycleAllow {
			t.Fatalf("child include policy = %v, want IncludeCycleAllow", childRuntime)
		}
		mustSetScriptLoaderModTime(t, dependencyPath, changedTime)
		if !portableChangeTrackerChanged(t, parent) {
			t.Fatal("newer included source did not report changed after child chdir moved away")
		}
	})

	t.Run("archive member tracks container", func(t *testing.T) {
		root := t.TempDir()
		archiveName := " dependency archive.jar "
		archivePath := filepath.Join(root, archiveName)
		writeTestSourceArchive(t, archivePath, "pkg/included.sl", "$archive_value = 9;\n")
		childName := "archive-child.sl"
		childPath := filepath.Join(root, childName)
		childSource := fmt.Sprintf("include(%q, 'pkg/included.sl'); return $archive_value;\n", archiveName)
		if err := os.WriteFile(childPath, []byte(childSource), 0o600); err != nil {
			t.Fatal(err)
		}
		mustSetScriptLoaderModTime(t, archivePath, oldTime)
		mustSetScriptLoaderModTime(t, childPath, oldTime)

		runtime, err := New(WithClock(clock))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtime.Close(context.Background()) })
		if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
			t.Fatal(err)
		}
		parent, _ := mustLoadPortableChangeTracker(t, runtime, childName)
		if got := portableRunChangeTracker(t, parent).Int32(); got != 9 {
			t.Fatalf("archive child result = %d, want 9", got)
		}
		if portableChangeTrackerChanged(t, parent) {
			t.Fatal("unchanged archive reported changed")
		}
		mustSetScriptLoaderModTime(t, archivePath, changedTime)
		if !portableChangeTrackerChanged(t, parent) {
			t.Fatal("newer archive container did not report changed")
		}
	})
}

func TestPortableScriptLoaderChangeTrackingResolverEvidence(t *testing.T) {
	loadTime := time.Now().Truncate(time.Millisecond)
	oldTime := loadTime.Add(-2 * time.Second)
	changedTime := loadTime.Add(2 * time.Second)
	clock := ClockFunc(func() time.Time { return loadTime })

	t.Run("explicit FileSourceResolver", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "tracked.sl")
		if err := os.WriteFile(path, []byte("return;\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mustSetScriptLoaderModTime(t, path, oldTime)
		resolver, err := NewFileSourceResolver(root)
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := New(WithClock(clock), WithSourceResolver(resolver))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtime.Close(context.Background()) })
		parent, _ := mustLoadPortableChangeTracker(t, runtime, "tracked.sl")
		mustSetScriptLoaderModTime(t, path, changedTime)
		if !portableChangeTrackerChanged(t, parent) {
			t.Fatal("FileSourceResolver modification evidence was not tracked")
		}
	})

	t.Run("virtual resolver names are not statted", func(t *testing.T) {
		root := t.TempDir()
		mainPath := filepath.Join(root, "virtual-main.sl")
		dependencyPath := filepath.Join(root, "virtual-dependency.sl")
		for _, path := range []string{mainPath, dependencyPath} {
			if err := os.WriteFile(path, []byte("filesystem decoy\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			mustSetScriptLoaderModTime(t, path, oldTime)
		}
		resolver := SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
			switch request.Name {
			case "virtual-main":
				return NewSource(mainPath, []byte(`include("virtual-dependency"); return $virtual_value;`)), nil
			case "virtual-dependency":
				return NewSource(dependencyPath, []byte(`$virtual_value = 23;`)), nil
			default:
				return Source{}, fmt.Errorf("unexpected virtual source %q", request.Name)
			}
		})
		runtime, err := New(WithClock(clock), WithSourceResolver(resolver))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtime.Close(context.Background()) })
		parent, _ := mustLoadPortableChangeTracker(t, runtime, "virtual-main")
		if got := portableRunChangeTracker(t, parent).Int32(); got != 23 {
			t.Fatalf("virtual child result = %d, want 23", got)
		}
		for _, path := range []string{mainPath, dependencyPath} {
			mustSetScriptLoaderModTime(t, path, changedTime)
		}
		if portableChangeTrackerChanged(t, parent) {
			t.Fatal("custom resolver Source.Name values were incorrectly treated as local mtime evidence")
		}
	})
}

func TestPortableScriptLoaderManualAssociationAndCacheBoundary(t *testing.T) {
	loadTime := time.Now().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "manual dependency.sl")
	if err := os.WriteFile(path, []byte("manual\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSetScriptLoaderModTime(t, path, loadTime.Add(-2*time.Second))
	source := fmt.Sprintf(`
import java.io.File;
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$tracked = [$loader loadScript: "manual", 'return 1;', $null];
[$tracked associateFile: [new File: %q]];
$disabled = [$loader setGlobalCache: 0];
[$loader touch: "manual", ticks()];
[$loader setGlobalCache: 1];
checkError($cache_error);
sub tracked_changed { return [$tracked hasChanged]; }
return @($disabled is $null, $cache_error isa ^java.lang.UnsupportedOperationException, $cache_error);
`, filepath.ToSlash(path))
	program, err := CompileString("manual-association.sl", source)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(WithClock(ClockFunc(func() time.Time { return loadTime })))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	parent, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	result := mustArrayValues(t, parent.Result())
	if len(result) != 3 || !result[0].Truth() || !result[1].Truth() {
		t.Fatalf("cache boundary result = %s, want disabled-null and typed enable rejection", parent.Result().Describe())
	}
	if got := result[2].String(); !strings.Contains(got, "global parsed-Block caches") {
		t.Fatalf("cache enable rejection = %q", got)
	}
	changed, err := parent.Call(context.Background(), "tracked_changed")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Truth() {
		t.Fatal("fresh manually associated file reported changed")
	}
	mustSetScriptLoaderModTime(t, path, loadTime.Add(2*time.Second))
	changed, err = parent.Call(context.Background(), "tracked_changed")
	if err != nil {
		t.Fatal(err)
	}
	if !changed.Truth() {
		t.Fatal("newer manually associated file did not report changed")
	}
}

func TestPortableScriptInstanceChangeTrackingConcurrent(t *testing.T) {
	loadTime := time.Now().Truncate(time.Millisecond)
	root := t.TempDir()
	paths := make([]string, 16)
	for index := range paths {
		paths[index] = filepath.Join(root, fmt.Sprintf("dependency-%02d.sl", index))
		if err := os.WriteFile(paths[index], []byte("return;\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mustSetScriptLoaderModTime(t, paths[index], loadTime.Add(-time.Second))
	}
	instance := &portableScriptInstance{loadTimeMillis: loadTime.UnixMilli()}
	var wait sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wait.Add(1)
		go func(offset int) {
			defer wait.Done()
			for iteration := 0; iteration < 200; iteration++ {
				instance.associateSourceFile(paths[(offset+iteration)%len(paths)])
				_ = instance.hasChanged()
			}
		}(worker)
	}
	wait.Wait()
	mustSetScriptLoaderModTime(t, paths[0], loadTime.Add(time.Second))
	if !instance.hasChanged() {
		t.Fatal("concurrently associated newer dependency did not report changed")
	}
}

func TestOfficialSleepPortableScriptLoaderHasChanged(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for ScriptLoader change-tracking verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	const source = `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$tracked = [$loader loadScript: "child script.sl"];
println("before=" . [$tracked hasChanged]);
[$tracked runScript];
println("after-run=" . [$tracked hasChanged]);
sleep(1200);
$handle = openf(">>included dependency.sl");
writeb($handle, "\n# changed");
closef($handle);
println("after-change=" . [$tracked hasChanged]);
[$loader touch: "unused", ticks()];
[$loader setGlobalCache: 0];
`
	prepare := func(root string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "child script.sl"), []byte(`include("included dependency.sl");`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "included dependency.sl"), []byte(`$included = 1;`), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	javaRoot := t.TempDir()
	prepare(javaRoot)
	command := osexec.Command(java, "-jar", jar, "-e", source)
	command.Dir = javaRoot
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep change-tracking probe: %v\n%s", err, want)
	}

	goRoot := t.TempDir()
	prepare(goRoot)
	program, err := CompileString("scriptloader-change.sl", source)
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	runtime, err := New(WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Invoke(context.Background(), "chdir", String(goRoot)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("pure-Go change-tracking probe: %v\n%s", err, got.String())
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("official ScriptLoader hasChanged mismatch\nwant:\n%s\ngot:\n%s", want, got.Bytes())
	}
}

func mustLoadPortableChangeTracker(t *testing.T, runtime *Runtime, childName string) (*Script, *portableScriptInstance) {
	t.Helper()
	source := fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$change_loader = [new ScriptLoader];
$change_tracked = [$change_loader loadScript: %q];
sub change_tracked { return [$change_tracked hasChanged]; }
sub run_change_tracked { return [$change_tracked runScript]; }
`, filepath.ToSlash(childName))
	program, err := CompileString("change-tracker.sl", source)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	parent.mu.RLock()
	var instance *portableScriptInstance
	for loader := range parent.scriptLoaders {
		loader.mu.Lock()
		if len(loader.loaded) == 1 {
			instance = loader.loaded[0]
		}
		loader.mu.Unlock()
		break
	}
	parent.mu.RUnlock()
	if instance == nil {
		t.Fatal("change-tracking ScriptInstance was not registered")
	}
	return parent, instance
}

func portableChangeTrackerChanged(t *testing.T, parent *Script) bool {
	t.Helper()
	value, err := parent.Call(context.Background(), "change_tracked")
	if err != nil {
		t.Fatal(err)
	}
	return value.Truth()
}

func portableRunChangeTracker(t *testing.T, parent *Script) Value {
	t.Helper()
	value, err := parent.Call(context.Background(), "run_change_tracked")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustSetScriptLoaderModTime(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestOfficialSleepPortableScriptLoaderRepeatAndUnload(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for ScriptLoader differential verification")
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java = "java"
	}
	directory := t.TempDir()
	childPath := filepath.Join(directory, "counter.sl")
	mainPath := filepath.Join(directory, "loader.sl")
	if err := os.WriteFile(childPath, []byte(`$counter++; println("child=" . $counter); return $counter;`), 0o600); err != nil {
		t.Fatal(err)
	}
	childName := filepath.ToSlash(childPath)
	source := fmt.Sprintf(`import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$script = [$loader loadScript: %q];
println("before=" . [$loader isLoaded: %q] . "/" . [[$loader getScripts] size]);
println("r1=" . [$script runScript]);
println("r2=" . [$script runScript]);
[$loader unloadScript: $script];
println("after=" . [$loader isLoaded: %q] . "/" . [[$loader getScripts] size] . "/" . [$script isLoaded]);
println("r3=" . [$script runScript]);
`, childName, childName, childName)
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(java, "-jar", jar, mainPath)
	reference, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep: %v\n%s", err, reference)
	}
	program, err := CompileString(mainPath, source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official ScriptLoader mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}

func TestOfficialSleepPortableScriptLoaderRetainedClosureAfterUnload(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for retained ScriptLoader closure verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}
	directory := t.TempDir()
	childPath := filepath.Join(directory, "retained-closure-child.sl")
	mainPath := filepath.Join(directory, "retained-closure-loader.sl")
	if err := os.WriteFile(childPath, []byte(`if ($saved is $null) { $saved = lambda({ $counter++; return $counter; }); } return [$saved];`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
println([$child runScript]);
[$loader unloadScript: $child];
println([$child runScript]);
println([$child isLoaded]);
`, filepath.ToSlash(childPath))
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(java, "-jar", jar, mainPath)
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep retained closure: %v\n%s", err, want)
	}
	program, err := CompileString(mainPath, source)
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	runtime, err := New(WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("OPFOR retained closure: %v\n%s", err, got.Bytes())
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("official retained closure mismatch\nwant:\n%s\ngot:\n%s", want, got.Bytes())
	}
}

func TestOfficialSleepPortableScriptLoaderCompileAndEnvironment(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for ScriptLoader differential verification")
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java = "java"
	}
	directory := t.TempDir()
	streamPath := filepath.Join(directory, "stream.sl")
	mainPath := filepath.Join(directory, "loader.sl")
	if err := os.WriteFile(streamPath, []byte("$streamCounter++;\nreturn $streamCounter + 20;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$block = [$loader compileScript: "direct-name", '$counter++; return $counter;'];
println("blockSource=" . [$block getSource]);
println("blockLines=" . [$block getApproximateLineRange]);
$script = [$loader loadScript: "compiled-name", $block, $null];
$env = [$script getScriptEnvironment];
$vars = [$env getScriptVariables];
[$env evaluateStatement: '$seed = 41; $other = 2; $before = 9;'];
println("vars=" . [$env getScalar: '$seed'] . "/" . [$vars getScalar: '$other']);
println("eval=" . [$env evaluateExpression: '$seed + $other'] . "/" . [$env evaluatePredicate: '$seed > $other']);
[$env flagError: "oops"];
println("error=" . [$env checkError] . "/" . [$env checkError]);
println("literal=" . [$env evaluateParsedLiteral: 'x=$before']);
println("runs=" . [$script runScript] . "/" . [$script runScript]);
$handle = openf(%q);
$stream = [$handle getInputStream];
$streamBlock = [$loader compileScript: "stream-name", $stream];
println("streamSource=" . [$streamBlock getSource]);
$streamScript = [$loader loadScript: "stream-script", $streamBlock, $null];
println("streamRun=" . [$streamScript runScript]);
$sourceScript = [$loader loadScript: "source-script", 'return 17;', $null];
println("sourceRun=" . [$sourceScript runScript]);
$nr = [$loader loadScriptNoReference: "no-reference", $block, $null];
println("noRef=" . [$loader isLoaded: "no-reference"] . "/" . [$nr isLoaded] . "/" . [$nr runScript]);
`, filepath.ToSlash(streamPath))
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(java, "-jar", jar, mainPath)
	reference, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep: %v\n%s", err, reference)
	}
	program, err := CompileString(mainPath, source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official ScriptLoader compile/environment mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}

func mustArrayValues(t *testing.T, value Value) []Value {
	t.Helper()
	array, ok := value.Array()
	if !ok {
		t.Fatalf("value = %s, want array", value.Describe())
	}
	return array.Values()
}

type scriptLoaderBindingObserver struct {
	mu           sync.Mutex
	registered   []string
	unregistered []string
}

func (observer *scriptLoaderBindingObserver) Registered(_ context.Context, binding Binding) error {
	observer.mu.Lock()
	observer.registered = append(observer.registered, binding.Name)
	observer.mu.Unlock()
	return nil
}

func (observer *scriptLoaderBindingObserver) Unregistered(_ context.Context, binding Binding) error {
	observer.mu.Lock()
	observer.unregistered = append(observer.unregistered, binding.Name)
	observer.mu.Unlock()
	return nil
}

func (observer *scriptLoaderBindingObserver) registeredName(name string) bool {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	for _, candidate := range observer.registered {
		if candidate == name {
			return true
		}
	}
	return false
}

func (observer *scriptLoaderBindingObserver) unregisteredName(name string) bool {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	for _, candidate := range observer.unregistered {
		if candidate == name {
			return true
		}
	}
	return false
}
