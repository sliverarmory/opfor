package opfor

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNormalizeVariableNamePreservesGeneralWhitespaceSemantics(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "$value", want: "$value"},
		{input: " value ", want: "$value"},
		{input: "@items\t", want: "@items"},
		{input: "%hash\u00a0", want: "%hash"},
		{input: "", want: "$"},
		{input: "plain", want: "$plain"},
	} {
		if got := normalizeVariableName(test.input); got != test.want {
			t.Fatalf("normalizeVariableName(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestPushlAndPoplReplaceAndRestoreTheActiveLocalLevel(t *testing.T) {
	program, err := CompileString("scope-stack.sl", `
inline transfer {
    pushl($a => $1, $b => $2);
    println("$x $+ / $+ $a $+ / $+ $b");
    popl($result => $a * $b);
}
sub run {
    local('$x $result');
    $x = "outer";
    transfer(6, 7);
    println("$x $+ / $+ $result");
}
$x = "global";
run();
`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "global/6/7\nouter/42\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestPoplUnderflowWarnsAndAbortsActiveBlock(t *testing.T) {
	program, err := CompileString("pop-underflow.sl", "popl(); println('continued');")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "Warning: &popl: no more local frames exist at pop-underflow.sl:1\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestEmptyPushlAndPoplLeaveRestoredArgumentsUntouched(t *testing.T) {
	program, err := CompileString("scope-stack-args.sl", `
sub run {
    pushl();
    $hidden = $1;
    popl();
    return @(@_, $1, $hidden);
}
return run("caller");
`)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if len(values) != 3 {
		t.Fatalf("result = %s, want three values", result.Describe())
	}
	arguments, argumentsOK := values[0].Array()
	if !argumentsOK || len(arguments.Values()) != 1 || arguments.Values()[0].String() != "caller" ||
		values[1].String() != "caller" || !values[2].IsNull() {
		t.Fatalf("result = %s, want @(@('caller'), 'caller', $null)", result.Describe())
	}
}

func TestDynamicSourceSharesTheCallerScopeStack(t *testing.T) {
	program, err := CompileString("eval-scope-stack.sl", `
sub run {
    local('$x $from');
    $x = "outer";
    eval("pushl(\$x => 'inner');");
    eval("popl(\$from => \$x);");
    return @($x, $from);
}
return run();
`)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if len(values) != 2 || values[0].String() != "outer" || values[1].String() != "inner" {
		t.Fatalf("result = %s, want @('outer', 'inner')", result.Describe())
	}
}

func TestYieldPreservesEveryPushedLocalLevel(t *testing.T) {
	program, err := CompileString("yield-scope-stack.sl", `
sub run {
    local('$x');
    $x = "a";
    pushl(); local('$x'); $x = "b";
    pushl(); local('$x'); $x = "c";
    yield "paused";
    println($x);
    popl(); println($x);
    popl(); println($x);
}
run();
run();
`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "c\nb\na\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestScopeStackFunctionsRequireAnActiveFiber(t *testing.T) {
	functions := (&Runtime{}).utilityExtraFunctions()
	for _, name := range []string{"pushl", "popl"} {
		_, err := functions[name](context.Background(), Invocation{Name: name})
		if err == nil || !strings.Contains(err.Error(), "requires an active script") {
			t.Fatalf("%s outside execution error = %v", name, err)
		}
	}
}
