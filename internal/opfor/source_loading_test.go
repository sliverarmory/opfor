package opfor

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEvalAndExprUseTheActiveSleepScope(t *testing.T) {
	program := mustCompileSourceTest(t, "eval-scope.sl", `
sub probe {
    local('$x');
    $x = 40;
    $answer = eval('$x = $x + 2; return $x . "/" . $1;');
    return @($answer, $x, $1, eval('2 + 2;'), expr('2 + 2'));
}
return probe('caller');
`)
	runtime := mustSourceRuntime(t)
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if got, want := len(values), 5; got != want {
		t.Fatalf("result length = %d, want %d", got, want)
	}
	if got := values[0].String(); got != "42/caller" {
		t.Errorf("eval return = %q, want 42/caller", got)
	}
	if got := values[1].Int32(); got != 42 {
		t.Errorf("caller local after eval = %d, want 42", got)
	}
	if got := values[2].String(); got != "caller" {
		t.Errorf("caller positional argument = %q, want caller", got)
	}
	if !values[3].IsNull() {
		t.Errorf("implicit eval result = %s, want $null", values[3].Describe())
	}
	if got := values[4].Int32(); got != 4 {
		t.Errorf("expr result = %d, want 4", got)
	}
}

func TestEvalSharesPersistentClosureScope(t *testing.T) {
	program := mustCompileSourceTest(t, "eval-this.sl", `
sub counter {
    this('$count');
    eval('if ($count is $null) { $count = 0; } $count++;');
    return $count;
}
return @(counter(), counter(), counter());
`)
	runtime := mustSourceRuntime(t)
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	array, _ := result.Array()
	values := array.Values()
	got := []int32{values[0].Int32(), values[1].Int32(), values[2].Int32()}
	// Array constructor arguments are evaluated last-to-first in Sleep.
	if want := []int32{3, 2, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("counter values = %#v, want %#v", got, want)
	}
}

func TestEvalYieldResumesDynamicProgramWithoutReplay(t *testing.T) {
	program := mustCompileSourceTest(t, "eval-yield.sl", `
sub generator {
    this('$steps');
    if ($steps is $null) { $steps = 0; }
    $inner = eval('
        $steps = $steps + 1;
        yield "first:" . $steps;
        $steps = $steps + 1;
        yield "second:" . $steps;
        $steps = $steps + 1;
        return "inner:" . $steps;
    ');
    return "outer:" . $inner . ":" . $steps;
}
`)
	runtime := mustSourceRuntime(t)
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure, ok := script.resolveFunction("generator").(*scriptClosure)
	if !ok || closure == nil {
		t.Fatal("generator did not resolve to a script closure")
	}

	for index, want := range []string{"outer:first:1:1", "second:2", "inner:3", "outer:first:4:4"} {
		got, invokeErr := closure.Invoke(context.Background())
		if invokeErr != nil {
			t.Fatalf("invoke %d: %v", index+1, invokeErr)
		}
		if got.String() != want {
			t.Fatalf("invoke %d = %q, want %q", index+1, got.String(), want)
		}
	}
}

func TestEvalCallCCParksTheOwningClosureWithoutReplay(t *testing.T) {
	program := mustCompileSourceTest(t, "eval-callcc.sl", `
sub continuation {
    this('$steps');
    if ($steps is $null) { $steps = 0; }
    $inner = eval('
        $steps = $steps + 1;
        callcc { return "parked:" . $steps; };
        $steps = $steps + 1;
        return "resumed:" . $steps;
    ');
    return "outer:" . $inner . ":" . $steps;
}
`)
	runtime := mustSourceRuntime(t)
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure, ok := script.resolveFunction("continuation").(*scriptClosure)
	if !ok || closure == nil {
		t.Fatal("continuation did not resolve to a script closure")
	}

	first, err := closure.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := first.String(); !strings.HasPrefix(got, "outer:&closure[eval:2]#") || !strings.HasSuffix(got, ":1") {
		t.Fatalf("first invocation = %q, want outer dynamic target closure and step 1", got)
	}
	second, err := closure.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := second.String(), "resumed:2"; got != want {
		t.Fatalf("resumed invocation = %q, want %q", got, want)
	}
}

func TestIncludeCallCCParksTheOwningClosureAndDiscardsIncludeResult(t *testing.T) {
	resolver := SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
		if request.Name != "continuation.sl" {
			return Source{}, os.ErrNotExist
		}
		return NewSource("virtual/continuation.sl", []byte(`
            $steps = $steps + 1;
            callcc { return "parked:" . $steps; };
            $steps = $steps + 1;
            return "included-return-is-discarded";
        `)), nil
	})
	runtime := mustSourceRuntime(t, WithSourceResolver(resolver))
	program := mustCompileSourceTest(t, "include-callcc.sl", `
sub continuation {
    this('$steps');
    if ($steps is $null) { $steps = 0; }
    $include_result = include("continuation.sl");
    return "outer:" . $include_result . ":" . $steps;
}
`)
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure := script.resolveFunction("continuation").(*scriptClosure)

	first, err := closure.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := first.String(), "outer::1"; got != want {
		t.Fatalf("first invocation = %q, want %q", got, want)
	}
	second, err := closure.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := second.String(), "included-return-is-discarded"; got != want {
		t.Fatalf("resumed invocation = %q, want %q", got, want)
	}
}

func TestIncludeYieldPreservesResolverStateSourceAndIncludeValue(t *testing.T) {
	sources := map[string]Source{
		"outer.sl": NewSource("virtual/outer.sl", []byte(`
            if ($trail is $null) { $trail = ""; }
            $trail = $trail . "a";
            $first_include = $__INCLUDE__;
            yield "first:" . $__INCLUDE__;
            $trail = $trail . "b";
            include("nested.sl");
            yield "second:" . $__INCLUDE__;
            $trail = $trail . "c";
        `)),
		"nested.sl": NewSource("virtual/nested.sl", []byte(`
            $trail = $trail . "n";
            $nested_include = $__INCLUDE__;
        `)),
	}
	var requests []SourceRequest
	resolver := SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
		requests = append(requests, request)
		source, ok := sources[request.Name]
		if !ok {
			return Source{}, os.ErrNotExist
		}
		return source, nil
	})
	runtime := mustSourceRuntime(t, WithSourceResolver(resolver))
	program := mustCompileSourceTest(t, "include-yield.sl", `
sub generator {
    this('$trail');
    include("outer.sl");
    return @($trail, $first_include, $nested_include, $__INCLUDE__);
}
`)
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure, ok := script.resolveFunction("generator").(*scriptClosure)
	if !ok || closure == nil {
		t.Fatal("generator did not resolve to a script closure")
	}

	first, err := closure.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstArray, ok := first.Array()
	if !ok {
		t.Fatalf("first invocation = %s, want outer result array", first.Describe())
	}
	firstValues := firstArray.Values()
	firstGot := []string{firstValues[0].String(), firstValues[1].String(), firstValues[2].String(), firstValues[3].String()}
	firstWant := []string{"a", "virtual/outer.sl", "", "virtual/outer.sl"}
	if !reflect.DeepEqual(firstGot, firstWant) {
		t.Fatalf("first invocation state = %#v, want %#v", firstGot, firstWant)
	}
	second, err := closure.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := second.String(), "second:virtual/nested.sl"; got != want {
		t.Fatalf("second yield = %q, want %q", got, want)
	}
	third, err := closure.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !third.IsNull() {
		t.Fatalf("completed included context = %s, want $null", third.Describe())
	}
	if got, want := closure.variableCell("$trail").Get().String(), "abnc"; got != want {
		t.Errorf("persistent trail after resumed include = %q, want %q", got, want)
	}
	if got, want := len(requests), 2; got != want {
		t.Fatalf("resolver calls = %d, want %d (outer source must not be reloaded)", got, want)
	}
	if requests[1].IncludingSource != "virtual/outer.sl" {
		t.Fatalf("nested IncludingSource = %q, want virtual/outer.sl", requests[1].IncludingSource)
	}
}

func TestArchiveIncludeYieldPreservesArchiveValue(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "scripts.jar")
	writeTestSourceArchive(t, archivePath, "pkg/suspend.sl", `yield $__INCLUDE__; return $__INCLUDE__;`)
	resolver, err := NewFileSourceResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	runtime := mustSourceRuntime(t, WithSourceResolver(resolver))
	program := mustCompileSourceTest(t, "archive-yield.sl", `
sub generator {
    include("scripts.jar", "pkg/suspend.sl");
    return @($__INCLUDE__, $resumed_include);
}
`)
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure := script.resolveFunction("generator").(*scriptClosure)

	first, err := closure.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstArray, ok := first.Array()
	if !ok {
		t.Fatalf("first invocation = %s, want array", first.Describe())
	}
	firstValues := firstArray.Values()
	if firstValues[0].String() != archivePath || !firstValues[1].IsNull() {
		t.Fatalf("first archive include state = %s, want @(%q, $null)", first.Describe(), archivePath)
	}
	second, err := closure.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := second.String(); got != archivePath {
		t.Fatalf("resumed archive include = %q, want %q", got, archivePath)
	}
}

func TestIncludeYieldRetainsCycleChainAndResumedErrorSpan(t *testing.T) {
	t.Run("cycle-chain", func(t *testing.T) {
		resolverCalls := 0
		resolver := SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
			resolverCalls++
			if request.Name != "loop.sl" {
				return Source{}, os.ErrNotExist
			}
			return NewSource("virtual/loop.sl", []byte(`yield "pause"; include("loop.sl");`)), nil
		})
		runtime := mustSourceRuntime(t, WithSourceResolver(resolver))
		script, err := runtime.Load(context.Background(), mustCompileSourceTest(t, "cycle-yield.sl", `sub generator { include("loop.sl"); }`))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = script.Unload(context.Background()) })
		closure := script.resolveFunction("generator").(*scriptClosure)
		if value, invokeErr := closure.Invoke(context.Background()); invokeErr != nil || !value.IsNull() {
			t.Fatalf("initial invocation = %s, %v", value.Describe(), invokeErr)
		}
		_, err = closure.Invoke(context.Background())
		var cycle *IncludeCycleError
		if !errors.As(err, &cycle) {
			t.Fatalf("resumed error = %v, want IncludeCycleError", err)
		}
		want := []string{"cycle-yield.sl", "virtual/loop.sl", "virtual/loop.sl"}
		if !reflect.DeepEqual(cycle.Chain, want) {
			t.Fatalf("cycle chain = %#v, want %#v", cycle.Chain, want)
		}
		if resolverCalls != 2 {
			t.Fatalf("resolver calls = %d, want 2", resolverCalls)
		}
	})

	t.Run("source-span", func(t *testing.T) {
		resolver := SourceResolverFunc(func(_ context.Context, _ SourceRequest) (Source, error) {
			return NewSource("virtual/resumed.sl", []byte("yield 'pause';\nfail_after_resume();")), nil
		})
		runtime := mustSourceRuntime(t,
			WithSourceResolver(resolver),
			WithFunction("fail_after_resume", func(context.Context, Invocation) (Value, error) {
				return Null(), errors.New("resumed failure")
			}),
		)
		script, err := runtime.Load(context.Background(), mustCompileSourceTest(t, "span-yield.sl", `sub generator { include("resumed.sl"); }`))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = script.Unload(context.Background()) })
		closure := script.resolveFunction("generator").(*scriptClosure)
		if _, err := closure.Invoke(context.Background()); err != nil {
			t.Fatal(err)
		}
		_, err = closure.Invoke(context.Background())
		var runtimeError *RuntimeError
		if !errors.As(err, &runtimeError) {
			t.Fatalf("resumed error = %v, want RuntimeError", err)
		}
		if got, want := runtimeError.Span.Source, "virtual/resumed.sl"; got != want {
			t.Fatalf("resumed source = %q, want %q", got, want)
		}
	})
}

func TestSuspendedDynamicSourceHonorsCancellationAndUnload(t *testing.T) {
	program := mustCompileSourceTest(t, "dynamic-lifecycle.sl", `
sub generator {
    this('$steps');
    if ($steps is $null) { $steps = 0; }
    return eval('$steps = $steps + 1; yield $steps; $steps = $steps + 1; return $steps;');
}
`)

	t.Run("cancellation", func(t *testing.T) {
		runtime := mustSourceRuntime(t)
		script, err := runtime.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = script.Unload(context.Background()) })
		closure := script.resolveFunction("generator").(*scriptClosure)
		if first, invokeErr := closure.Invoke(context.Background()); invokeErr != nil || first.Int32() != 1 {
			t.Fatalf("initial invocation = %s, %v", first.Describe(), invokeErr)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := closure.Invoke(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled invocation error = %v, want context.Canceled", err)
		}
		if got := closure.variableCell("$steps").Get().Int32(); got != 1 {
			t.Fatalf("steps after canceled resume = %d, want 1", got)
		}
	})

	t.Run("unload", func(t *testing.T) {
		runtime := mustSourceRuntime(t)
		script, err := runtime.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		closure := script.resolveFunction("generator").(*scriptClosure)
		if _, err := closure.Invoke(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := script.Unload(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := closure.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
			t.Fatalf("post-unload invocation error = %v, want ErrScriptUnloaded", err)
		}
	})
}

func TestEvalCompileFailureUsesCheckErrorAndEvalSpans(t *testing.T) {
	program := mustCompileSourceTest(t, "eval-error.sl", `
$result = eval('return (');
checkError($problem);
return @($result, $problem);
`)
	runtime := mustSourceRuntime(t)
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	array, _ := result.Array()
	values := array.Values()
	if !values[0].IsNull() {
		t.Errorf("failed eval result = %s, want $null", values[0].Describe())
	}
	if message := values[1].String(); !strings.HasPrefix(message, "YourCodeSucksException:") || !strings.Contains(message, "error(s):") {
		t.Fatalf("checkError = %q, want Sleep-compatible compile error", message)
	}
}

func TestIncludeUsesResolverAndCallerScope(t *testing.T) {
	sources := map[string]Source{
		"first.sl": NewSource("virtual/first.sl", []byte(`
$local = 'included';
$first_source = $__INCLUDE__;
sub loaded_sub { return 'loaded'; }
include('second.sl');
`)),
		"second.sl": NewSource("virtual/second.sl", []byte(`
$second_source = $__INCLUDE__;
`)),
	}
	var requests []SourceRequest
	resolver := SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
		requests = append(requests, request)
		source, ok := sources[request.Name]
		if !ok {
			return Source{}, os.ErrNotExist
		}
		return source, nil
	})
	runtime, err := New(WithSourceResolver(resolver))
	if err != nil {
		t.Fatal(err)
	}
	program := mustCompileSourceTest(t, "virtual/root.cna", `
sub load_source {
    local('$local $__INCLUDE__');
    $local = 'caller';
    include('first.sl');
    return @($local, $__INCLUDE__);
}
@state = load_source();
return @(@state[0], @state[1], $first_source, $second_source, loaded_sub());
`)
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	want := []string{"included", "virtual/second.sl", "virtual/first.sl", "virtual/second.sl", "loaded"}
	got := make([]string, len(values))
	for index := range values {
		got[index] = values[index].String()
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("include state = %#v, want %#v", got, want)
	}
	if got, want := len(requests), 2; got != want {
		t.Fatalf("resolver request count = %d, want %d", got, want)
	}
	if requests[0].Script == 0 || requests[0].IncludingSource != "virtual/root.cna" || requests[0].Name != "first.sl" {
		t.Errorf("first request = %+v", requests[0])
	}
	if requests[1].Script != requests[0].Script || requests[1].IncludingSource != "virtual/first.sl" || requests[1].Name != "second.sl" {
		t.Errorf("second request = %+v", requests[1])
	}
}

func TestIncludePreservesResolvedSourceSpan(t *testing.T) {
	resolver := SourceResolverFunc(func(_ context.Context, _ SourceRequest) (Source, error) {
		return NewSource("virtual/broken.sl", []byte("missing_dynamic_host_function();")), nil
	})
	runtime, err := New(WithSourceResolver(resolver))
	if err != nil {
		t.Fatal(err)
	}
	program := mustCompileSourceTest(t, "root.cna", `include('broken.sl');`)
	_, err = runtime.Execute(context.Background(), program)
	var runtimeError *RuntimeError
	if !errors.As(err, &runtimeError) {
		t.Fatalf("Execute error = %v, want RuntimeError", err)
	}
	if got := runtimeError.Span.Source; got != "virtual/broken.sl" {
		t.Fatalf("runtime source = %q, want virtual/broken.sl", got)
	}
}

func TestIncludeCycleIsDetected(t *testing.T) {
	// Sleep 2.1 permits recursive includes until the JVM exhausts resources.
	// OPFOR intentionally retains this deterministic safety extension.
	sources := map[string]string{
		"a.sl": `include('b.sl');`,
		"b.sl": `include('a.sl');`,
	}
	resolverCalls := 0
	resolver := SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
		resolverCalls++
		code, ok := sources[request.Name]
		if !ok {
			return Source{}, os.ErrNotExist
		}
		return NewSource("virtual/"+request.Name, []byte(code)), nil
	})
	runtime, err := New(WithSourceResolver(resolver))
	if err != nil {
		t.Fatal(err)
	}
	program := mustCompileSourceTest(t, "cycle-root.cna", `debug(34); include('a.sl');`)
	_, err = runtime.Execute(context.Background(), program)
	if !errors.Is(err, ErrIncludeCycle) {
		t.Fatalf("Execute error = %v, want ErrIncludeCycle", err)
	}
	var cycle *IncludeCycleError
	if !errors.As(err, &cycle) {
		t.Fatalf("Execute error = %v, want IncludeCycleError", err)
	}
	if got, want := cycle.Chain, []string{"cycle-root.cna", "virtual/a.sl", "virtual/b.sl", "virtual/a.sl"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cycle chain = %#v, want %#v", got, want)
	}
	if resolverCalls != 3 {
		t.Fatalf("resolver calls = %d, want 3", resolverCalls)
	}
}

func TestSleepCompatibleIncludeCyclesRunUntilExecutionLimit(t *testing.T) {
	t.Parallel()

	resolverCalls := 0
	resolver := SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
		resolverCalls++
		if request.Name != "again.sl" {
			return Source{}, os.ErrNotExist
		}
		return NewSource("virtual/again.sl", []byte(`include('again.sl');`)), nil
	})
	runtime, err := New(
		WithSourceResolver(resolver),
		WithIncludeCyclePolicy(IncludeCycleAllow),
		WithInstructionLimit(100),
	)
	if err != nil {
		t.Fatal(err)
	}
	program := mustCompileSourceTest(t, "recursive-root.cna", `include('again.sl');`)
	_, err = runtime.Execute(context.Background(), program)
	if !errors.Is(err, ErrInstructionLimit) {
		t.Fatalf("Execute error = %v, want ErrInstructionLimit", err)
	}
	if errors.Is(err, ErrIncludeCycle) {
		t.Fatalf("Execute error = %v, did not want ErrIncludeCycle", err)
	}
	if resolverCalls < 2 {
		t.Fatalf("resolver calls = %d, want recursive resolution", resolverCalls)
	}
}

func TestIncludeCyclePolicyRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	_, err := New(WithIncludeCyclePolicy(IncludeCyclePolicy(255)))
	if err == nil || !strings.Contains(err.Error(), "invalid include cycle policy 255") {
		t.Fatalf("New error = %v", err)
	}
}

func TestFileSourceResolverSupportsFilesDirectoriesAndJARs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plain.sl"), []byte(`$plain = 'file';`), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "scripts")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "nested.sl"), []byte(`$directory = 'directory';`), 0o600); err != nil {
		t.Fatal(err)
	}
	jarPath := filepath.Join(root, "scripts.jar")
	writeTestSourceArchive(t, jarPath, "pkg/archive.sl", `$archive = 'jar';`)

	resolver, err := NewFileSourceResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolver.BaseDirectory(), filepath.Clean(root); got != want {
		t.Fatalf("base directory = %q, want %q", got, want)
	}
	runtime, err := New(WithSourceResolver(resolver))
	if err != nil {
		t.Fatal(err)
	}
	program := mustCompileSourceTest(t, "filesystem.cna", `
include('plain.sl');
include('scripts', 'nested.sl');
include('scripts.jar', 'pkg/archive.sl');
return @($plain, $directory, $archive, $__INCLUDE__);
`)
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	array, _ := result.Array()
	values := array.Values()
	if got := []string{values[0].String(), values[1].String(), values[2].String()}; !reflect.DeepEqual(got, []string{"file", "directory", "jar"}) {
		t.Fatalf("loaded values = %#v", got)
	}
	// BasicUtilities.f_use exposes the archive File itself here; the member
	// name remains only the logical compiler source.
	wantSource := jarPath
	if got := values[3].String(); got != wantSource {
		t.Fatalf("$__INCLUDE__ = %q, want %q", got, wantSource)
	}
}

func TestFileSourceResolverPreservesSpacesAndSearchesSleepClasspath(t *testing.T) {
	root := t.TempDir()
	spacedName := " spaced source.sl "
	if err := os.WriteFile(filepath.Join(root, spacedName), []byte(`$spaced = 'kept';`), 0o600); err != nil {
		t.Fatal(err)
	}
	classPath := filepath.Join(root, "sleep-libs")
	if err := os.Mkdir(classPath, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(classPath, "hidden.jar")
	writeTestSourceArchive(t, archive, "pkg/from-classpath.sl", `$classpath = 'found';`)
	if err := os.WriteFile(filepath.Join(classPath, "single-from-classpath.sl"), []byte(`$single = 'standalone';`), 0o600); err != nil {
		t.Fatal(err)
	}
	classArchive, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures", "data", "test.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(classPath, "classes.jar"), classArchive, 0o600); err != nil {
		t.Fatal(err)
	}

	resolver, err := NewFileSourceResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	resolver.SetSleepClasspath("sleep-libs")
	if got, want := resolver.SleepClasspath(), []string{"sleep-libs"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SleepClasspath = %#v, want %#v", got, want)
	}
	runtime := mustSourceRuntime(t, WithSourceResolver(resolver))
	if runtime.defaultFileResolver != nil || runtime.concreteFileSourceResolver() != resolver {
		t.Fatal("importer-supplied FileSourceResolver lost its distinct ownership or concrete lookup")
	}
	program := mustCompileSourceTest(t, "classpath.cna", `
import org.hick.blah.SqueezeBox from: classes.jar;
include(" spaced source.sl ");
include("single-from-classpath.sl");
include("hidden.jar", "pkg/from-classpath.sl");
$box = [new SqueezeBox];
return @($spaced, $single, $classpath, $__INCLUDE__, [$box squeeze]);
`)
	value, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := consoleValueStrings(array.Values()), []string{"kept", "standalone", "found", archive, "34"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("include values = %#v, want %#v", got, want)
	}

	// Snapshots are detached and an empty classpath restores Sleep's '.'.
	snapshot := resolver.SleepClasspath()
	snapshot[0] = "changed"
	if got := resolver.SleepClasspath()[0]; got != "sleep-libs" {
		t.Fatalf("mutating snapshot changed resolver classpath to %q", got)
	}
	resolver.SetSleepClasspath("")
	if got, want := resolver.SleepClasspath(), []string{"."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("default SleepClasspath = %#v, want %#v", got, want)
	}
}

func TestScriptLoaderChildInheritsDefaultSleepClasspathForIncludeAndImport(t *testing.T) {
	root := t.TempDir()
	classPath := filepath.Join(root, "programs")
	if err := os.Mkdir(classPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(classPath, "noterm.sl"), []byte(`$included = "child-include";`), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures", "data", "test.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(classPath, "classes.jar"), archive, 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, err := New(WithSleepClasspath("programs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
		t.Fatal(err)
	}
	program := mustCompileSourceTest(t, "child-classpath.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "child.sl", 'include("noterm.sl"); import org.hick.blah.SqueezeBox from: classes.jar; $box = [new SqueezeBox]; return @($included, [$box squeeze]);', $null];
return [$child runScript];
`)
	value, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := consoleValueStrings(mustArrayValues(t, value)), []string{"child-include", "34"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("child classpath result = %#v, want %#v", got, want)
	}
}

func TestScriptLoaderFilenameOverloadDoesNotSearchSleepClasspath(t *testing.T) {
	root := t.TempDir()
	classPath := filepath.Join(root, "programs")
	if err := os.Mkdir(classPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(classPath, "noterm.sl"), []byte(`this is deliberately invalid Sleep source`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := New(WithSleepClasspath("programs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
		t.Fatal(err)
	}
	program := mustCompileSourceTest(t, "loader-direct-file.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$script = [$loader loadScript: "noterm.sl"];
return @($script, "" . checkError());
`)
	value, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	values := mustArrayValues(t, value)
	if len(values) != 2 || !values[0].IsNull() || !strings.Contains(values[1].String(), "java.io.FileNotFoundException") || strings.Contains(values[1].String(), "YourCodeSucksException") {
		t.Fatalf("direct ScriptLoader result = %s, want null/FileNotFoundException", value.Describe())
	}
}

func TestFileSourceResolverClasspathContinuesPastStatError(t *testing.T) {
	root := t.TempDir()
	classPath := filepath.Join(root, "programs")
	if err := os.Mkdir(classPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(classPath, "loop.sl"), []byte(`return "later";`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("loop.sl", filepath.Join(root, "loop.sl")); err != nil {
		t.Skipf("symlink loop unavailable: %v", err)
	}
	resolver, err := NewFileSourceResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	resolver.SetSleepClasspath("programs")
	resolved, err := resolver.resolveIncludeSource(context.Background(), SourceRequest{Name: "loop.sl"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(resolved.source.Data), `return "later";`; got != want {
		t.Fatalf("resolved source = %q, want %q", got, want)
	}
	if got, want := resolved.modificationPath, filepath.Join(classPath, "loop.sl"); got != want {
		t.Fatalf("resolved path = %q, want %q", got, want)
	}
}

func TestNewFileSourceResolverPreservesWhitespaceOnlyBaseDirectory(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "   ")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "source.sl"), []byte("return 1;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewFileSourceResolver(base)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolver.BaseDirectory(); got != filepath.Clean(want) {
		t.Fatalf("base directory = %q, want whitespace path %q", got, filepath.Clean(want))
	}
	source, err := resolver.ResolveSource(context.Background(), SourceRequest{Name: "source.sl"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(source.Data); got != "return 1;\n" {
		t.Fatalf("resolved data = %q", got)
	}
}

func TestTwoArgumentIncludePreservesLogicalMemberAndArchiveValue(t *testing.T) {
	root := t.TempDir()
	jarPath := filepath.Join(root, "scripts.jar")
	writeTestSourceArchive(t, jarPath, "pkg/nested.sl", `warn("nested warning");`)
	resolver, err := NewFileSourceResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	source, err := resolver.ResolveSource(context.Background(), SourceRequest{Container: "scripts.jar", Name: "pkg/nested.sl"})
	if err != nil {
		t.Fatal(err)
	}
	if got := source.Name; got != "pkg/nested.sl" {
		t.Fatalf("logical source = %q, want pkg/nested.sl", got)
	}

	var output bytes.Buffer
	runtime, err := New(WithSourceResolver(resolver), WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	program := mustCompileSourceTest(t, "root.sl", `include("scripts.jar", "pkg/nested.sl"); return $__INCLUDE__;`)
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.String(); got != jarPath {
		t.Fatalf("$__INCLUDE__ = %q, want %q", got, jarPath)
	}
	if got, want := output.String(), "Warning: nested warning at nested.sl:1\n"; got != want {
		t.Fatalf("warning = %q, want %q", got, want)
	}
}

func TestIncludeFailureUpdatesIncludeAtSleepBoundary(t *testing.T) {
	root := t.TempDir()
	jarPath := filepath.Join(root, "scripts.jar")
	writeTestSourceArchive(t, jarPath, "present.sl", `$present = 1;`)
	directory := filepath.Join(root, "scripts")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewFileSourceResolver(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name        string
		code        string
		wantInclude string
		wantError   string
	}{
		{
			name:        "missing-jar-member",
			code:        `$__INCLUDE__ = "before"; include("scripts.jar", "missing.sl"); return @($__INCLUDE__, checkError());`,
			wantInclude: jarPath,
			wantError:   "java.io.IOException: unable to locate missing.sl from: scripts.jar",
		},
		{
			name:        "missing-directory-member",
			code:        `$__INCLUDE__ = "before"; include("scripts", "missing.sl"); return @($__INCLUDE__, checkError());`,
			wantInclude: filepath.Join(directory, "missing.sl"),
			wantError:   "java.io.IOException: unable to locate missing.sl from: scripts",
		},
		{
			name:        "missing-file",
			code:        `$__INCLUDE__ = "before"; include("missing.sl"); return @($__INCLUDE__, checkError());`,
			wantInclude: "before",
			wantError:   "java.io.FileNotFoundException: " + filepath.Join(root, "missing.sl") + " (No such file or directory)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime, newErr := New(WithSourceResolver(resolver))
			if newErr != nil {
				t.Fatal(newErr)
			}
			result, executeErr := runtime.Execute(context.Background(), mustCompileSourceTest(t, test.name+".sl", test.code))
			if executeErr != nil {
				t.Fatal(executeErr)
			}
			array, _ := result.Array()
			values := array.Values()
			if got := values[0].String(); got != test.wantInclude {
				t.Errorf("$__INCLUDE__ = %q, want %q", got, test.wantInclude)
			}
			if got := values[1].String(); got != test.wantError {
				t.Errorf("checkError = %q, want %q", got, test.wantError)
			}
		})
	}
}

func TestMissingIncludeContainerAbortsBlockWithoutCheckError(t *testing.T) {
	root := t.TempDir()
	resolver, err := NewFileSourceResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithSourceResolver(resolver), WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	program := mustCompileSourceTest(t, "missing-container.sl", `sub load {
    include("missing.jar", "entry.sl");
    $continued = "bad";
}
load();
return @($continued, checkError());`)
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	array, _ := result.Array()
	values := array.Values()
	if !values[0].IsNull() || !values[1].IsNull() {
		t.Fatalf("result = %s, want no continuation and empty checkError", result.Describe())
	}
	if got, want := output.String(), "Warning: &include: could not locate source 'missing.jar' at missing-container.sl:2\n"; got != want {
		t.Fatalf("warning = %q, want %q", got, want)
	}
}

func TestIncludeDebug34ThrowsTypedConsumedError(t *testing.T) {
	root := t.TempDir()
	resolver, err := NewFileSourceResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	wantError := "java.io.FileNotFoundException: " + filepath.Join(root, "missing.sl") + " (No such file or directory)"
	t.Run("caught", func(t *testing.T) {
		runtime, newErr := New(WithSourceResolver(resolver))
		if newErr != nil {
			t.Fatal(newErr)
		}
		program := mustCompileSourceTest(t, "caught-include.sl", `debug(34);
try {
    include("missing.sl");
    $continued = "bad";
}
catch $error {
    return @($error, checkError(), $continued);
}
return "not caught";`)
		result, executeErr := runtime.Execute(context.Background(), program)
		if executeErr != nil {
			t.Fatal(executeErr)
		}
		array, _ := result.Array()
		values := array.Values()
		if got := values[0].String(); got != wantError {
			t.Fatalf("caught value = %q, want %q", got, wantError)
		}
		exception, ok := values[0].Object()
		if !ok {
			t.Fatalf("caught value kind = %s, want exception object", values[0].Kind())
		}
		portable, ok := exception.(*portableJavaException)
		if !ok || portable.class != "java.io.FileNotFoundException" {
			t.Fatalf("caught object = %#v, want FileNotFoundException", exception)
		}
		if !values[1].IsNull() || !values[2].IsNull() {
			t.Fatalf("caught tail = %s, want consumed checkError and no continuation", result.Describe())
		}
	})

	t.Run("compile-error-caught", func(t *testing.T) {
		fixtureRoot, absoluteErr := filepath.Abs(filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures"))
		if absoluteErr != nil {
			t.Fatal(absoluteErr)
		}
		fixtureResolver, resolverErr := NewFileSourceResolver(fixtureRoot)
		if resolverErr != nil {
			t.Fatal(resolverErr)
		}
		runtime, newErr := New(WithSourceResolver(fixtureResolver))
		if newErr != nil {
			t.Fatal(newErr)
		}
		program := mustCompileSourceTest(t, "caught-compile-include.sl", `debug(34);
try {
    include("data/scripts.jar", "scripts/errors1.sl");
}
catch $error {
    return @($error, checkError());
}
return "not caught";`)
		result, executeErr := runtime.Execute(context.Background(), program)
		if executeErr != nil {
			t.Fatal(executeErr)
		}
		array, _ := result.Array()
		values := array.Values()
		want := "YourCodeSucksException: 3 error(s): Mismatched Parentheses - missing close paren at 9; Mismatched Braces - missing close brace at 6; Runaway string at 9"
		if got := values[0].String(); got != want {
			t.Fatalf("caught value = %q, want %q", got, want)
		}
		exception, _ := values[0].Object()
		portable, ok := exception.(*portableJavaException)
		if !ok || portable.class != "sleep.error.YourCodeSucksException" {
			t.Fatalf("caught object = %#v, want YourCodeSucksException", exception)
		}
		if !values[1].IsNull() {
			t.Fatalf("checkError = %s, want consumed slot", values[1].Describe())
		}
	})

	t.Run("uncaught", func(t *testing.T) {
		var output bytes.Buffer
		runtime, newErr := New(WithSourceResolver(resolver), WithStdout(&output), WithStderr(&output))
		if newErr != nil {
			t.Fatal(newErr)
		}
		program := mustCompileSourceTest(t, "uncaught-include.sl", `debug(34);
include("missing.sl");
println("after");`)
		if _, executeErr := runtime.Execute(context.Background(), program); executeErr != nil {
			t.Fatal(executeErr)
		}
		want := "Warning: Uncaught exception: " + wantError + " at uncaught-include.sl:2\n"
		if got := output.String(); got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	})
}

func TestIncludeRunawaySummaryIgnoresBalancedPrefixes(t *testing.T) {
	for _, test := range []struct {
		name string
		code string
		want string
	}{
		{
			name: "parentheses-and-braces",
			code: "sub complete {\n    if (1) { println(\"ok\"); }\n}\nsub broken\n{\n    println(\"runaway);\n}\nprintln(\"tail\");\n",
			want: "YourCodeSucksException: 3 error(s): Mismatched Parentheses - missing close paren at 6; Mismatched Braces - missing close brace at 5; Runaway string at 6",
		},
		{
			name: "index-after-balanced-index",
			code: "@complete = @(1);\n$x = @complete[0 . \"runaway;\nprintln(\"tail\");\n",
			want: "YourCodeSucksException: 2 error(s): Runaway string at 2; Mismatched Indices - missing close index at 2",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := NewSource(test.name+".sl", []byte(test.code))
			_, err := Compile(source)
			var compileError *CompileError
			if !errors.As(err, &compileError) {
				t.Fatalf("Compile error = %v, want CompileError", err)
			}
			got := sleepSourceErrorMessage("include", &sourceCompileFailure{source: source, cause: err})
			if got != test.want {
				t.Fatalf("summary = %q, want %q", got, test.want)
			}
		})
	}
}

type includeArityResolver struct {
	requests []SourceRequest
}

func (resolver *includeArityResolver) ResolveSource(_ context.Context, request SourceRequest) (Source, error) {
	resolver.requests = append(resolver.requests, request)
	return NewSource(request.Name, []byte(`$included .= "[" . $__INCLUDE__ . "]";`)), nil
}

func TestIncludeArityMatchesSleepStackContract(t *testing.T) {
	resolver := &includeArityResolver{}
	var diagnostics bytes.Buffer
	runtime := mustSourceRuntime(t,
		WithSourceResolver(resolver),
		WithStderr(&diagnostics),
	)
	program := mustCompileSourceTest(t, "include-arity.sl", `
$__INCLUDE__ = "sentinel";
[{ include(); }];
$after_zero = @($__INCLUDE__, checkError());
include("container.jar", "member.sl");
include("three.sl", "ignored-container", "ignored-member");
include("four.sl", "ignored-1", "ignored-2", "ignored-3");
return @($after_zero, $included, $__INCLUDE__, checkError());
`)
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	type requestShape struct {
		including string
		container string
		name      string
	}
	gotRequests := make([]requestShape, len(resolver.requests))
	for index, request := range resolver.requests {
		gotRequests[index] = requestShape{
			including: request.IncludingSource,
			container: request.Container,
			name:      request.Name,
		}
	}
	wantRequests := []requestShape{
		{including: "include-arity.sl", container: "container.jar", name: "member.sl"},
		{including: "include-arity.sl", name: "three.sl"},
		{including: "include-arity.sl", name: "four.sl"},
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("include requests = %#v, want %#v", gotRequests, wantRequests)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if len(values) != 4 {
		t.Fatalf("result values = %d, want 4", len(values))
	}
	afterZero, ok := values[0].Array()
	if !ok {
		t.Fatalf("state after zero-argument include = %s, want array", values[0].Describe())
	}
	if got, want := argvValueStrings(afterZero.Values()), []string{"sentinel", ""}; !reflect.DeepEqual(got, want) {
		t.Fatalf("state after zero-argument include = %s, want sentinel and empty checkError", values[0].Describe())
	}
	if got, want := values[1].String(), "[member.sl][three.sl][four.sl]"; got != want {
		t.Fatalf("included trail = %q, want %q", got, want)
	}
	if got, want := values[2].String(), "four.sl"; got != want {
		t.Fatalf("final $__INCLUDE__ = %q, want %q", got, want)
	}
	if !values[3].IsNull() {
		t.Fatalf("final checkError = %s, want null", values[3].Describe())
	}
	if got := diagnostics.String(); !strings.Contains(got, "Warning: internal error - class java.util.EmptyStackException at include-arity.sl:") {
		t.Fatalf("zero-argument include warning = %q", got)
	}
}

func TestSleepSourceIncludeGoldenConformance(t *testing.T) {
	for _, test := range []struct {
		name             string
		workingDirectory string
	}{
		{name: "include", workingDirectory: filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures")},
		{name: "include2", workingDirectory: filepath.Join("testdata", "upstream", "sleep-2.1", "programs")},
	} {
		t.Run(test.name, func(t *testing.T) {
			programRoot := filepath.Join("testdata", "upstream", "sleep-2.1", "programs")
			programData, err := os.ReadFile(filepath.Join(programRoot, test.name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", test.name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(test.name+".sl", programData))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			var output bytes.Buffer
			runtime, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatal(err)
			}
			absoluteRoot, err := filepath.Abs(test.workingDirectory)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.Invoke(context.Background(), "chdir", String(absoluteRoot)); err != nil {
				t.Fatalf("set runtime cwd: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, err = runtime.Execute(ctx, program)
			cancel()
			if err != nil {
				t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
			}
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestSleepIncludeScopeCorpusGoldens(t *testing.T) {
	for _, name := range []string{"incit2", "incit3"} {
		t.Run(name, func(t *testing.T) {
			programRoot := filepath.Join("testdata", "upstream", "sleep-2.1", "programs")
			programData, readErr := os.ReadFile(filepath.Join(programRoot, name+".sl"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			want, readErr := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", name+".sl"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			program, compileErr := Compile(NewSource(name+".sl", programData))
			if compileErr != nil {
				t.Fatal(compileErr)
			}
			var output bytes.Buffer
			runtime, newErr := New(WithStdout(&output), WithStderr(&output))
			if newErr != nil {
				t.Fatal(newErr)
			}
			absoluteRoot, absoluteErr := filepath.Abs(programRoot)
			if absoluteErr != nil {
				t.Fatal(absoluteErr)
			}
			if _, chdirErr := runtime.Invoke(context.Background(), "chdir", String(absoluteRoot)); chdirErr != nil {
				t.Fatalf("set runtime cwd: %v", chdirErr)
			}
			if _, executeErr := runtime.Execute(context.Background(), program); executeErr != nil {
				t.Fatalf("Execute: %v\noutput:\n%s", executeErr, output.String())
			}
			// $__INCLUDE__ exposes the resolved File. Normalize only the host
			// checkout root recorded in the upstream golden.
			got := strings.ReplaceAll(filepath.ToSlash(output.String()), filepath.ToSlash(absoluteRoot), "/root/sleep/tests")
			if got != string(want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestWithSourceResolverRejectsNil(t *testing.T) {
	if _, err := New(WithSourceResolver(nil)); err == nil || !strings.Contains(err.Error(), "source resolver is nil") {
		t.Fatalf("New error = %v, want nil resolver error", err)
	}
	if _, err := New(WithSourceResolver(SourceResolverFunc(nil))); err == nil || !strings.Contains(err.Error(), "source resolver function is nil") {
		t.Fatalf("New typed-nil error = %v, want nil resolver function error", err)
	}
}

func TestWithSleepClasspathConfiguresDefaultResolver(t *testing.T) {
	t.Parallel()

	runtime, err := New(WithSleepClasspath("first;second:third"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if runtime.defaultFileResolver == nil {
		t.Fatal("WithSleepClasspath did not retain the runtime-owned file resolver")
	}
	if got, want := runtime.defaultFileResolver.SleepClasspath(), []string{"first", "second", "third"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SleepClasspath = %#v, want %#v", got, want)
	}
}

func TestWithSleepClasspathAndCustomResolverConflictInEitherOrder(t *testing.T) {
	t.Parallel()

	resolver := SourceResolverFunc(func(context.Context, SourceRequest) (Source, error) {
		return Source{}, errors.New("not called")
	})
	for _, options := range [][]Option{
		{WithSleepClasspath("lib"), WithSourceResolver(resolver)},
		{WithSourceResolver(resolver), WithSleepClasspath("lib")},
	} {
		if _, err := New(options...); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("New error = %v, want resolver/classpath conflict", err)
		}
	}
}

func writeTestSourceArchive(t *testing.T, filename, name, code string) {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	entry, err := archive.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(code)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustCompileSourceTest(t *testing.T, name, code string) *Program {
	t.Helper()
	program, err := CompileString(name, code)
	if err != nil {
		t.Fatalf("CompileString(%s): %v", name, err)
	}
	return program
}

func mustSourceRuntime(t *testing.T, options ...Option) *Runtime {
	t.Helper()
	runtime, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}
