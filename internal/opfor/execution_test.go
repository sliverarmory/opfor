package opfor

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestExecuteSleepCore(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := CompileString("core.sl", `
sub twice { local('$value'); $value = $1 * 2; return $value; }
@values = @(1, 2, 3);
%seen = %();
foreach $index => $value (@values) {
    %seen[$index] = twice($value);
}
if (size(%seen) == 3) {
    println("values=" . values(%seen));
}
return %seen["2"];
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	value, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if value.Int32() != 6 {
		t.Fatalf("result = %s, want 6", value.Describe())
	}
	if got, want := output.String(), "values=@(6, 4, 2)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestIffUsesSleepDefaultBranches(t *testing.T) {
	t.Parallel()

	program, err := CompileString("iff-defaults.sl", `
$calls = 0;
sub touched { $calls++; return $1; }
return @(
	$calls,
    iff(1),
    iff(0),
    iff(1, touched('yes')),
    iff(0, touched('unreached')),
	iff(0, touched('also-unreached'), touched('no'))
);
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
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if got, want := len(values), 6; got != want {
		t.Fatalf("result length = %d, want %d", got, want)
	}
	if values[0].Int32() != 2 || values[1].Int32() != 1 || !values[2].IsNull() || values[3].String() != "yes" ||
		!values[4].IsNull() || values[5].String() != "no" {
		t.Fatalf("iff defaults/laziness = %s", result.Describe())
	}
}

func TestNegatedWordComparisons(t *testing.T) {
	t.Parallel()

	program, err := CompileString("negated-comparisons.sl", `
return @(
	"a" !gt "b",
	"b" !lt "a",
	"a" !eq "b",
	"a" !ne "a",
	"b" !le "a",
	"a" !ge "b"
);
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	for index, item := range array.Values() {
		if !item.Truth() {
			t.Errorf("negated comparison %d = %s, want true", index, item.Describe())
		}
	}
}

func TestMissingSleepFunctionWarnsAndContinues(t *testing.T) {
	t.Parallel()

	program, err := CompileString("missing.sl", `println("before"); missing_function(); println("after");`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := output.String(), "before\nWarning: Attempted to call non-existent function &missing_function at missing.sl:1\nafter\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestTopLevelArgumentsUseSleepARGVContract(t *testing.T) {
	t.Parallel()

	program, err := CompileString("argv.sl", `
return @(
    @ARGV,
    @_,
    $1,
    $__SCRIPT__,
    $__SCRIPT_NAME__
);
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Execute(context.Background(), program, String("one"), String("two"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	outer, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	items := outer.Values()
	if len(items) != 5 {
		t.Fatalf("result = %s, want five values", value.Describe())
	}
	argv, ok := items[0].Array()
	if !ok || !reflect.DeepEqual(argvValueStrings(argv.Values()), []string{"one", "two"}) {
		t.Fatalf("@ARGV = %s, want one/two", items[0].Describe())
	}
	underscore, ok := items[1].Array()
	if !ok || underscore.Len() != 0 {
		t.Fatalf("@_ = %s, want empty top-level argument array", items[1].Describe())
	}
	if !items[2].IsNull() || items[3].String() != "argv.sl" || items[4].String() != "argv.sl" {
		t.Fatalf("$1/script globals = %s/%s/%s", items[2].Describe(), items[3].Describe(), items[4].Describe())
	}
}

func TestSingleQuotedStringsOnlyDecodeQuoteAndBackslash(t *testing.T) {
	t.Parallel()

	program, err := CompileString("single-quotes.sl", `return @('\n', '\.', '\\', '\'', '\x41');`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{`\n`, `\.`, `\`, `'`, `\x41`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("single-quoted values = %q, want %q", got, want)
	}
}

func argvValueStrings(values []Value) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func TestLoadDispatchAndUnload(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := CompileString("events.cna", `
$prefix = "event";
on ready { println("$prefix $+ : $+ $1"); return $1; }
on * { println("$1 $+ : $+ $2"); return $2; }
alias hello { return "hello $1"; }
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !script.Active() || len(runtime.Scripts()) != 1 {
		t.Fatalf("loaded script state = active:%v scripts:%d", script.Active(), len(runtime.Scripts()))
	}

	results, err := runtime.DispatchEvent(context.Background(), "ready", String("payload"))
	if err != nil {
		t.Fatalf("DispatchEvent: %v", err)
	}
	if got := []string{results[0].String(), results[1].String()}; !reflect.DeepEqual(got, []string{"payload", "payload"}) {
		t.Fatalf("event results = %q", got)
	}
	alias, err := runtime.InvokeBinding(context.Background(), BindingAlias, "hello", String("world"))
	if err != nil || alias.String() != "hello world" {
		t.Fatalf("alias = (%s, %v)", alias.Describe(), err)
	}
	retained := script.Bindings()[0].Callback
	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if script.Active() || len(runtime.Scripts()) != 0 || len(runtime.Bindings(BindingEvent, "ready")) != 0 {
		t.Fatal("unload retained active script state")
	}
	if _, err := retained.Invoke(context.Background()); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("retained callback error = %v, want ErrScriptUnloaded", err)
	}
	if got, want := output.String(), "event:payload\nready:payload\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestSleepInterpolationPunctuationBoundary(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "interpolation.sl", `
$name = "visible";
return @("<$name>", "<$name $+ >", "Done!@#$", "Check!@#$%^&", "<$name\$tail>");
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{"<", "<visible>", "Done!@#$", "Check!@#", "<visible$tail>"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("interpolated values = %q, want %q", got, want)
	}
}

func TestNativePassByNameAndHostFallbackFromScript(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	var hostInvocation Invocation
	runtime, err := New(
		WithStdout(&output),
		WithFunction("mutate", func(_ context.Context, invocation Invocation) (Value, error) {
			if !invocation.Arguments[0].Set(Int(42)) {
				return Null(), errors.New("argument was not a reference")
			}
			return Null(), nil
		}),
		WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
			hostInvocation = invocation
			return String("host:" + invocation.Arg(0).String()), nil
		})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := CompileString("host.cna", `$x = 1; mutate($x); println($x); $x = 2; mutate(\$x); println($x); println(host_value("ok"));`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if hostInvocation.Name != "host_value" || hostInvocation.Script == 0 || hostInvocation.Span.Source != "host.cna" {
		t.Fatalf("host invocation = %+v", hostInvocation)
	}
	if got, want := output.String(), "42\n42\nhost:ok\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestEvalSessionPersistsGlobalsAndFunctions(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Eval(context.Background(), "<one>", `$x = 40; sub plus { return $1 + $x; }`); err != nil {
		t.Fatalf("first Eval: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "<two>", `return plus(2);`)
	if err != nil {
		t.Fatalf("second Eval: %v", err)
	}
	if value.Int32() != 42 {
		t.Fatalf("persistent Eval result = %s, want 42", value.Describe())
	}
	if len(runtime.Scripts()) != 1 {
		t.Fatalf("Eval scripts = %d, want one session", len(runtime.Scripts()))
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(runtime.Scripts()) != 0 {
		t.Fatal("Close retained Eval session")
	}
}

func TestClosureYieldAndThisState(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := CompileString("yield.sl", `
sub sequence {
    this('$count');
    $count++;
    yield $count;
    $count++;
    yield $count;
    return $null;
}
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var got []string
	for index := 0; index < 3; index++ {
		value, callErr := script.Call(context.Background(), "sequence")
		if callErr != nil {
			t.Fatalf("sequence call %d: %v", index, callErr)
		}
		got = append(got, value.Describe())
	}
	if want := []string{"1", "2", "$null"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sequence = %q, want %q", got, want)
	}
}

func TestObjectOperationsAreDelegated(t *testing.T) {
	t.Parallel()

	type thing struct{ name string }
	var operations []ObjectOperation
	runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		operations = append(operations, invocation.Op)
		switch invocation.Op {
		case ObjectConstruct:
			if invocation.Class != "Thing" || invocation.Arguments[0].Resolve().String() != "one" {
				t.Fatalf("construct invocation = %+v", invocation)
			}
			return ObjectValue(&thing{name: "one"}), nil
		case ObjectInvoke:
			object, _ := invocation.Target.Object()
			if invocation.Message != "name" || object.(*thing).name != "one" {
				t.Fatalf("method invocation = %+v", invocation)
			}
			return String(object.(*thing).name), nil
		default:
			return Null(), errors.New("unexpected object operation")
		}
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := CompileString("objects.sl", `$object = [new Thing: "one"]; return [$object name];`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	value, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if value.String() != "one" || !reflect.DeepEqual(operations, []ObjectOperation{ObjectConstruct, ObjectInvoke}) {
		t.Fatalf("object result = %q, operations = %v", value.String(), operations)
	}
}

func TestPrintRejectsMissingHandleWhenGivenTwoArguments(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = runtime.Invoke(context.Background(), "println", String("one"), String("two"))
	if err == nil || !strings.Contains(err.Error(), "expected I/O handle") {
		t.Fatalf("println error = %v", err)
	}
}

func TestSleepSpaceshipUsesNumericComparison(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "spaceship.sl", `return @(1.5 <=> 2.0, 2.0 <=> 1.5, 2L <=> 2, "10" <=> "2");`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{"-1", "1", "0", "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("spaceship results = %q, want %q", got, want)
	}
}
