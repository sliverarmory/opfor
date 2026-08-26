package opfor

import (
	"bytes"
	"context"
	"testing"
)

func TestForeachValueVariableRetainsArrayCellIdentity(t *testing.T) {
	program, err := CompileString("foreach-cells.sl", `
@values = @("a", "b", "c");
foreach $value (@values) {
    $value = uc($value);
}
return @values;
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
	if len(values) != 3 || values[0].String() != "A" || values[1].String() != "B" || values[2].String() != "C" {
		t.Fatalf("foreach-mutated values = %s", result.Describe())
	}
}

func TestForeachStructuralArrayMutationWarnsReplaysCurrentAndStops(t *testing.T) {
	program, err := CompileString("femod.sl", `@a = @("a", "b", "c", "d");

foreach $index => $value (@a)
{
   println("$index => $value");
   if ($value eq "c")
   {
      push(@a, "d");
   }
}`)
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
	want := "0 => a\n1 => b\n2 => c\n" +
		"Warning: unsafe data modification: @array changed during iteration at femod.sl:3\n" +
		"2 => c\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestForeachMutationAtNewEndStopsWithoutWarning(t *testing.T) {
	program, err := CompileString("foreach-new-end.sl", `@values = @("a", "b", "c");
foreach $value (@values) {
    println($value);
    if ($value eq "b") {
        pop(@values);
    }
}`)
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
	if got, want := output.String(), "a\nb\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestForeachStructuralHashMutationWarnsReplaysCurrentAndStops(t *testing.T) {
	for _, test := range []struct {
		name   string
		hash   string
		mutate string
	}{
		{name: "insert", hash: "ohash", mutate: `%values["d"] = "dog";`},
		{name: "remove", hash: "ohash", mutate: `remove(%values, "bat");`},
		{name: "miss", hash: "ohash", mutate: `$ignored = %values["missing"];`},
		{name: "access-order", hash: "ohasha", mutate: `$ignored = %values["b"];`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `%values = ` + test.hash + `(a => "apple", b => "bat", c => "cat");
foreach $key => $value (%values) {
    println($key . "=" . $value);
    if ($key eq "a") {
        ` + test.mutate + `
    }
}
println("done");
`
			program, err := CompileString("foreach-hash-"+test.name+".sl", source)
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
			want := "a=apple\n" +
				"Warning: detected unsafe data modification at foreach-hash-" + test.name + ".sl:2\n" +
				"a=apple\n" +
				"done\n"
			if got := output.String(); got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestForeachBreakDestroysActiveIterator(t *testing.T) {
	var warnings bytes.Buffer
	program, err := CompileString("foreach-break.sl", `
@values = @(1, 2, 3);
foreach $value (@values) {
    break;
}
try {
    remove();
}
catch $error {
	println("caught=" . $error);
}
println("continued");
return "done";
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New(WithStdout(&warnings), WithStderr(&warnings))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := result.String(); got != "done" {
		t.Fatalf("result = %s, want done", result.Describe())
	}
	want := "Warning: &remove: no active foreach loop to remove element from at foreach-break.sl:7\n" +
		"continued\n"
	if got := warnings.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestForeachHashSingleVariableAndIteratorRemoval(t *testing.T) {
	program, err := CompileString("foreach-hash.sl", `
%values = ohash(a => "apple", b => "bat", c => "cat");
@keys = @();
foreach $key (%values) {
    push(@keys, $key);
}
foreach $key => $value (%values) {
    if ($key eq "b") {
        remove();
    }
}
return @(@keys, %values);
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
	outer, ok := result.Array()
	if !ok || outer.Len() != 2 {
		t.Fatalf("result = %s, want pair", result.Describe())
	}
	keysValue, _ := outer.Get(0)
	keys, _ := keysValue.Array()
	keyValues := keys.Values()
	if len(keyValues) != 3 || keyValues[0].String() != "a" || keyValues[1].String() != "b" || keyValues[2].String() != "c" {
		t.Fatalf("single-variable hash foreach = %s", keysValue.Describe())
	}
	hashValue, _ := outer.Get(1)
	hash, _ := hashValue.Hash()
	if _, exists := hash.Get("b"); exists || hash.Len() != 2 {
		t.Fatalf("hash after iterator remove = %s", hashValue.Describe())
	}
}

func TestForeachRemoveFromNestedFunctionUsesCallerIterator(t *testing.T) {
	program, err := CompileString("foreach-nested-remove.sl", `
sub drop { return remove(); }
@values = @("a", "b", "c", "d");
@returned = @();
foreach $index => $value (@values) {
    if ($value eq "b" || $value eq "c") {
        push(@returned, drop() is @values);
    }
}
return @(@values, @returned);
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
	outer, ok := result.Array()
	if !ok || outer.Len() != 2 {
		t.Fatalf("result = %s, want pair", result.Describe())
	}
	valuesResult, _ := outer.Get(0)
	values, _ := valuesResult.Array()
	remaining := values.Values()
	if len(remaining) != 2 || remaining[0].String() != "a" || remaining[1].String() != "d" {
		t.Fatalf("nested removal result = %s", valuesResult.Describe())
	}
	returnedResult, _ := outer.Get(1)
	returned, _ := returnedResult.Array()
	identities := returned.Values()
	if len(identities) != 2 || !identities[0].Truth() || !identities[1].Truth() {
		t.Fatalf("nested remove identities = %s", returnedResult.Describe())
	}
}
