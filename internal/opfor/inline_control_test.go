package opfor

import (
	"bytes"
	"context"
	"testing"
)

func TestInlineReturnExitsTheOwningClosure(t *testing.T) {
	program, err := CompileString("inline-return.sl", `
inline early {
    println("inline");
    return $1;
    println("unreachable inline");
}
sub owner {
    println("owner");
    early("returned");
    println("unreachable owner");
    return "outer";
}
return owner();
`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.String(), "returned"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
	if got, want := output.String(), "owner\ninline\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestNestedInlineReturnCrossesEveryInlineBody(t *testing.T) {
	program, err := CompileString("nested-inline-return.sl", `
inline inner { return "nested"; }
inline middle { inner(); println("unreachable middle"); }
sub owner { middle(); println("unreachable owner"); }
return owner();
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
	if got, want := result.String(), "nested"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
}

func TestInlineBuiltinClearsReturnAndKeepsOwningArguments(t *testing.T) {
	program, err := CompileString("inline-builtin-return.sl", `
sub owner {
    inline({ return "$0 $+ / $+ $1"; });
    return "unreachable";
}
return owner("argument");
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
	if got, want := result.String(), "unreachable"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
}

func TestInlineBuiltinYieldContinuesOwnerAndSavesNestedContext(t *testing.T) {
	program, err := CompileString("inline-builtin-yield.sl", `
$inline = { yield "a"; return "b"; };
$owner = {
    $value = inline($inline);
    println("OUTER=" . $value);
    return "done";
};
println("1=" . [$owner]);
println("2=" . [$owner]);
println("3=" . [$owner]);
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
	want := "OUTER=\n1=done\n2=b\nOUTER=\n3=done\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestInlineDeclarationRetainsOwnerMessageWhileReplacingArguments(t *testing.T) {
	program, err := CompileString("inline-message.sl", `
inline probe { return "$0 $+ / $+ $1"; }
sub owner {
    probe("inline argument");
    return "unreachable";
}
return owner("owner argument");
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
	if got, want := result.String(), "&owner/inline argument"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
}

func TestInlineYieldSuspendsAndResumesTheOwningClosure(t *testing.T) {
	program, err := CompileString("inline-yield.sl", `
inline stage {
    println("inline before");
    yield "paused";
    println("inline after");
}
sub owner {
    println("owner before");
    stage();
    println("owner after");
}
$first = owner();
println("first=$first");
$second = owner();
println("second=$second");
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
	want := "owner before\ninline before\nfirst=paused\ninline after\nowner after\nsecond=\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestResumedInlineBodyObservesTheOwnersRestoredArguments(t *testing.T) {
	program, err := CompileString("inline-yield-arguments.sl", `
inline stage {
    println("before " . @_);
    yield "paused";
    println("after " . @_);
}
sub owner {
    stage("inline argument");
    println("owner " . @_);
}
$first = owner("first owner argument");
$second = owner("second owner argument");
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
	want := "before @('inline argument')\nafter @('second owner argument')\nowner @('second owner argument')\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestNestedInlineYieldRetainsTheInlineCallStack(t *testing.T) {
	program, err := CompileString("nested-inline-yield.sl", `
inline inner {
    println("inner before");
    yield "paused";
    println("inner after");
}
inline middle {
    println("middle before");
    inner();
    println("middle after");
}
sub owner {
    middle();
    println("owner after");
}
println(owner());
println(owner());
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
	want := "middle before\ninner before\npaused\ninner after\nmiddle after\nowner after\n\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
