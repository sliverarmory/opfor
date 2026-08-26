package opfor

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDynamicSourceContinuationGroupsResumeFIFO(t *testing.T) {
	t.Run("explicit-return-discards-old-tail", func(t *testing.T) {
		program := mustCompileSourceTest(t, "dynamic-group-return.sl", `
sub group {
    $a = eval('yield "a1"; return "a2";');
    $b = eval('yield "b1"; return "b2";');
    return $a . $b;
}
`)
		runtime := mustSourceRuntime(t)
		script, err := runtime.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = script.Unload(context.Background()) })
		closure := script.resolveFunction("group").(*scriptClosure)

		var got []string
		for range 4 {
			value, invokeErr := closure.Invoke(context.Background())
			if invokeErr != nil {
				t.Fatal(invokeErr)
			}
			got = append(got, value.String())
		}
		want := []string{"a1b1", "a2", "a1b1", "a2"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("continuation results = %#v, want %#v", got, want)
		}
	})

	t.Run("normal-completion-continues-old-tail", func(t *testing.T) {
		program := mustCompileSourceTest(t, "dynamic-group-normal.sl", `
sub group {
    eval('yield "a1"; println("A2");');
    eval('yield "b1"; println("B2");');
    println("OUTER");
    return "done";
}
`)
		var output bytes.Buffer
		runtime := mustSourceRuntime(t, WithStdout(&output))
		script, err := runtime.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = script.Unload(context.Background()) })
		closure := script.resolveFunction("group").(*scriptClosure)

		first, err := closure.Invoke(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		second, err := closure.Invoke(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		third, err := closure.Invoke(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got, want := []string{first.String(), second.String(), third.String()}, []string{"done", "", "done"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("continuation results = %#v, want %#v", got, want)
		}
		if got, want := output.String(), "OUTER\nA2\nB2\nOUTER\n"; got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	})
}

func TestDynamicSourceContinuationDefersNewContexts(t *testing.T) {
	program := mustCompileSourceTest(t, "dynamic-group-deferred.sl", `
$inner_code = 'yield "c1"; println("C2");';
sub group {
    eval('yield "a1"; println("A-start"); eval($inner_code); println("A-end");');
    eval('yield "b1"; println("B2");');
    println("OUTER");
    return "done";
}
`)
	var output bytes.Buffer
	runtime := mustSourceRuntime(t, WithStdout(&output))
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure := script.resolveFunction("group").(*scriptClosure)

	var got []string
	for range 4 {
		value, invokeErr := closure.Invoke(context.Background())
		if invokeErr != nil {
			t.Fatal(invokeErr)
		}
		got = append(got, value.String())
	}
	if want := []string{"done", "", "", "done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("continuation results = %#v, want %#v", got, want)
	}
	if gotOutput, want := output.String(), "OUTER\nA-start\nA-end\nB2\nC2\nOUTER\n"; gotOutput != want {
		t.Fatalf("output = %q, want %q", gotOutput, want)
	}
}

func TestResumedDynamicCallCCTransfersNormally(t *testing.T) {
	program := mustCompileSourceTest(t, "dynamic-callcc-resume.sl", `
global('$target_hits $captured');
sub target {
    $target_hits++;
    $captured = $1;
    return "T";
}
sub continuation {
    $value = eval('yield "a"; callcc &target; println("AFTER"); return "b";');
    println("OUTER=" . $value);
    return $value;
}
`)
	var output bytes.Buffer
	runtime := mustSourceRuntime(t, WithStdout(&output))
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure := script.resolveFunction("continuation").(*scriptClosure)

	var got []string
	for range 4 {
		value, invokeErr := closure.Invoke(context.Background())
		if invokeErr != nil {
			t.Fatal(invokeErr)
		}
		got = append(got, value.String())
	}
	if want := []string{"a", "T", "b", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("continuation results = %#v, want %#v", got, want)
	}
	if gotOutput, want := output.String(), "OUTER=a\nAFTER\nOUTER=a\n"; gotOutput != want {
		t.Fatalf("output = %q, want %q", gotOutput, want)
	}
	if got := script.Get("$target_hits").Int32(); got != 1 {
		t.Fatalf("target invocations = %d, want 1", got)
	}
	captured, ok := script.Get("$captured").Function()
	if !ok || captured != closure {
		t.Fatalf("callcc continuation = %s, want owning closure", script.Get("$captured").Describe())
	}
}

func TestDynamicSourceContinuationUsesFinalLocalLevels(t *testing.T) {
	program := mustCompileSourceTest(t, "dynamic-final-locals.sl", `
sub group {
    local('$x');
    $x = "base";
    eval("pushl(); local('\$x'); \$x = 'inner'; yield 'a'; println('RESUME=' . \$x);");
    popl();
    println("OUTER=" . $x);
    return "done";
}
`)
	var output bytes.Buffer
	runtime := mustSourceRuntime(t, WithStdout(&output))
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure := script.resolveFunction("group").(*scriptClosure)

	var got []string
	for range 3 {
		value, invokeErr := closure.Invoke(context.Background())
		if invokeErr != nil {
			t.Fatal(invokeErr)
		}
		got = append(got, value.String())
	}
	if want := []string{"done", "", "done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("continuation results = %#v, want %#v", got, want)
	}
	if gotOutput, want := output.String(), "OUTER=base\nRESUME=base\nOUTER=base\n"; gotOutput != want {
		t.Fatalf("output = %q, want %q", gotOutput, want)
	}
}

func TestDynamicSourceContinuationNormalizesNestedInlineLocalLevels(t *testing.T) {
	program := mustCompileSourceTest(t, "dynamic-final-inline-locals.sl", `
inline stage {
    pushl();
    local('$x');
    $x = "inner";
    yield "a";
    println("RESUME=" . $x);
}
sub group {
    local('$x');
    $x = "base";
    eval('stage();');
    popl();
    println("OUTER=" . $x);
    return "done";
}
`)
	var output bytes.Buffer
	runtime := mustSourceRuntime(t, WithStdout(&output))
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure := script.resolveFunction("group").(*scriptClosure)

	var got []string
	for range 3 {
		value, invokeErr := closure.Invoke(context.Background())
		if invokeErr != nil {
			t.Fatal(invokeErr)
		}
		got = append(got, value.String())
	}
	if want := []string{"done", "", "done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("continuation results = %#v, want %#v", got, want)
	}
	if gotOutput, want := output.String(), "OUTER=base\nRESUME=base\nOUTER=base\n"; gotOutput != want {
		t.Fatalf("output = %q, want %q", gotOutput, want)
	}
}

func TestExprInlineYieldSavesContextButDiscardsInitialValue(t *testing.T) {
	program := mustCompileSourceTest(t, "dynamic-expr-inline.sl", `
$inline = { yield "a"; return "b"; };
sub group { return "p" . expr("inline(\$inline)") . "q"; }
`)
	runtime := mustSourceRuntime(t)
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure := script.resolveFunction("group").(*scriptClosure)

	var got []string
	for range 3 {
		value, invokeErr := closure.Invoke(context.Background())
		if invokeErr != nil {
			t.Fatal(invokeErr)
		}
		got = append(got, value.String())
	}
	if want := []string{"pq", "b", "pq"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expr inline continuation = %#v, want %#v", got, want)
	}
}

func TestDynamicSourceRejectsCrossRuntimeFiber(t *testing.T) {
	other := mustSourceRuntime(t)
	runtime := mustSourceRuntime(t, WithFunction("cross_runtime_eval", func(ctx context.Context, _ Invocation) (Value, error) {
		return other.Invoke(ctx, "eval", String("return 7;"))
	}))
	program := mustCompileSourceTest(t, "cross-runtime-eval.sl", `cross_runtime_eval();`)
	_, err := runtime.Execute(context.Background(), program)
	if err == nil || !strings.Contains(err.Error(), "active script belongs to a different runtime") {
		t.Fatalf("cross-runtime eval error = %v", err)
	}
}

func TestDynamicSourceThroughRuntimeInvokeUsesActiveCollector(t *testing.T) {
	runtime := mustSourceRuntime(t, WithFunction("host_eval", func(ctx context.Context, invocation Invocation) (Value, error) {
		return invocation.Runtime.Invoke(ctx, "eval", invocation.Arg(0))
	}))
	program := mustCompileSourceTest(t, "indirect-runtime-eval.sl", `
sub group { return "p" . host_eval('yield "a"; return "b";') . "q"; }
`)
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })
	closure := script.resolveFunction("group").(*scriptClosure)

	var got []string
	for range 3 {
		value, invokeErr := closure.Invoke(context.Background())
		if invokeErr != nil {
			t.Fatal(invokeErr)
		}
		got = append(got, value.String())
	}
	if want := []string{"paq", "b", "paq"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("indirect eval continuation = %#v, want %#v", got, want)
	}
}

func TestDynamicClosureDescriptionsUseSleepZeroBasedLines(t *testing.T) {
	program := mustCompileSourceTest(t, "dynamic-lines.sl", `
$first = eval('return { return 1; };');
$second = eval('
return { return 2; };
');
$third = eval('

return { return 3; };
');
return @($first, $second, $third);
`)
	runtime := mustSourceRuntime(t)
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	wantLocations := []string{"[eval:0]", "[eval:1]", "[eval:2]"}
	for index, want := range wantLocations {
		if got := values[index].String(); !strings.Contains(got, want) {
			t.Errorf("closure %d = %q, want location %s", index, got, want)
		}
	}
}

func TestFreshDynamicCallCCTracesAsAnOrdinaryEvalResult(t *testing.T) {
	program := mustCompileSourceTest(t, "/tmp/dynamic-callcc-trace.sl", `debug(8);
$target = { return "T"; };
$owner = {
    $value = eval("callcc \$target; return 'done';");
    println("OUTER=" . $value);
    return $value;
};
println("ONE=" . [$owner]);
println("TWO=" . [$owner]);
`)
	var output bytes.Buffer
	runtime := mustSourceRuntime(t, WithStdout(&output), WithStderr(&output))
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "-goto-") || strings.Contains(got, " CALLCC:") {
		t.Fatalf("fresh dynamic callcc leaked transfer trace:\n%s", got)
	}
	wantFragments := []string{
		"Trace: &eval('callcc $target; return 'done';') = &closure[dynamic-callcc-trace.sl:2]#1 at dynamic-callcc-trace.sl:4\n",
		"OUTER=&closure[dynamic-callcc-trace.sl:2]#1\n",
		"ONE=&closure[dynamic-callcc-trace.sl:2]#1\n",
		"TWO=done\n",
	}
	for _, fragment := range wantFragments {
		if !strings.Contains(got, fragment) {
			t.Errorf("trace output missing %q:\n%s", fragment, got)
		}
	}
}
