package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPortableJavaObjectSoftErrorCanonicalCompatibility(t *testing.T) {
	t.Parallel()

	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "probug.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "probug.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("probug.sl", programBytes))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func TestPortableIntegerParseExceptionCanonicalCompatibility(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "tcatch3.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "tcatch3.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("tcatch3.sl", programBytes))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func TestPortableEnumerationExceptionCanonicalCompatibility(t *testing.T) {
	for _, name := range []string{"tcatch4", "tcatch5"} {
		t.Run(name, func(t *testing.T) {
			programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(name+".sl", programBytes))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			var output bytes.Buffer
			runtime, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := runtime.Execute(context.Background(), program); err != nil {
				t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
			}
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestPortableEnumerationTraceCanonicalCompatibility(t *testing.T) {
	for _, name := range []string{"proxy", "tracepo"} {
		t.Run(name, func(t *testing.T) {
			programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(name+".sl", programBytes))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			var output bytes.Buffer
			runtime, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := runtime.Execute(context.Background(), program); err != nil {
				t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
			}
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestPortableJavaObjectSoftErrorsHonorDebugFlags(t *testing.T) {
	t.Parallel()

	const exception = "java.lang.IndexOutOfBoundsException: Index: 3, Size: 0"
	for _, test := range []struct {
		flags       int
		wantWarning bool
	}{
		{flags: 0},
		{flags: 1},
		{flags: 2, wantWarning: true},
		// DEBUG_TRACE_PROFILE_ONLY suppresses traces, not checkError warnings.
		{flags: 31, wantWarning: true},
	} {
		test := test
		t.Run(fmt.Sprintf("debug-%d", test.flags), func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			runtime, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			code := fmt.Sprintf(`debug(%d); global('$list $error');
$list = [new LinkedList];
[$list remove: 3];
$error = checkError();
return @("$error", [$error getClass], [$error getMessage]);`, test.flags)
			value, err := runtime.Eval(context.Background(), "soft-object.sl", code)
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			array, ok := value.Array()
			if !ok {
				t.Fatalf("result = %s, want array", value.Describe())
			}
			if got, want := argvValueStrings(array.Values()), []string{
				exception, "class java.lang.IndexOutOfBoundsException", "Index: 3, Size: 0",
			}; !reflect.DeepEqual(got, want) {
				t.Fatalf("soft error = %q, want %q", got, want)
			}
			wantOutput := ""
			if test.wantWarning {
				wantOutput = "Warning: checkError(): " + exception + " at soft-object.sl:3\n"
			}
			if got := output.String(); got != wantOutput {
				t.Fatalf("warning = %q, want %q", got, wantOutput)
			}
		})
	}
}

func TestPortableJavaObjectSoftErrorCanBeThrown(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "throw-soft-object.sl", `
debug(34);
$list = [new LinkedList];
try {
	[$list remove: 3];
	return "not caught";
}
catch $error {
	return @("$error", [$error getClass], checkError());
}
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want caught error array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{
		"java.lang.IndexOutOfBoundsException: Index: 3, Size: 0",
		"class java.lang.IndexOutOfBoundsException", "",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("caught soft error = %q, want %q", got, want)
	}
	if output.Len() != 0 {
		t.Fatalf("caught soft error output = %q, want none", output.String())
	}
}

func TestPortableJavaCollectionExceptionsShareSoftErrorPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation string
		want      string
	}{
		{
			name:      "get-index",
			operation: `$list = [new LinkedList]; [$list get: 2];`,
			want:      "java.lang.IndexOutOfBoundsException: Index: 2, Size: 0",
		},
		{
			name:      "get-first-empty",
			operation: `$list = [new LinkedList]; [$list getFirst];`,
			want:      "java.util.NoSuchElementException",
		},
		{
			name:      "remove-index",
			operation: `$list = [new LinkedList]; [$list remove: 2];`,
			want:      "java.lang.IndexOutOfBoundsException: Index: 2, Size: 0",
		},
		{
			name:      "iterator-exhausted",
			operation: `$list = [new LinkedList]; $iterator = [$list iterator]; [$iterator next];`,
			want:      "java.util.NoSuchElementException",
		},
		{
			name:      "iterator-remove-state",
			operation: `$list = [new LinkedList: @("a")]; $iterator = [$list iterator]; [$iterator remove];`,
			want:      "java.lang.IllegalStateException",
		},
		{
			name:      "iterator-concurrent-modification",
			operation: `$list = [new LinkedList: @("a")]; $iterator = [$list iterator]; [$list add: "b"]; [$iterator next];`,
			want:      "java.util.ConcurrentModificationException",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runtime, err := New()
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			value, err := runtime.Eval(context.Background(), test.name+".sl", "debug(1); "+test.operation+" return checkError();")
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got := value.String(); got != test.want {
				t.Fatalf("checkError = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPortableJavaErrorsDoNotSoftenImporterObjectHostFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("java.lang.IndexOutOfBoundsException: importer failure")
	runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Message == "remove" {
			return Null(), sentinel
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = runtime.Eval(context.Background(), "importer-error.sl", `$list = [new LinkedList]; [$list remove: 3];`)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Eval error = %v, want importer sentinel", err)
	}
}

func TestPortableObjectHostBoundaryStillReturnsJavaErrors(t *testing.T) {
	t.Parallel()

	host := defaultObjectHost{primary: unsupportedObjectHost{}}
	list, err := host.Object(context.Background(), ObjectInvocation{Op: ObjectConstruct, Class: "LinkedList"})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	_, err = host.Object(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Target: list, Message: "remove",
		Arguments: []Argument{{Value: Int(3)}},
	})
	var exception *portableJavaException
	if !errors.As(err, &exception) {
		t.Fatalf("direct ObjectHost error = %v, want portable Java exception", err)
	}
	if got, want := exception.Error(), "java.lang.IndexOutOfBoundsException: Index: 3, Size: 0"; got != want {
		t.Fatalf("direct ObjectHost error = %q, want %q", got, want)
	}
}

func TestPortableJavaScalarObjects(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "java.sl", `
@values = @([Math pow: 3.0, 4.0], [Integer parseInt: "ff", 16], [Character isLetter: "A"]);
return @values;
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, _ := value.Array()
	values := array.Values()
	if values[0].Kind() != KindDouble || math.Abs(values[0].Float64()-81) > 0.0001 || values[1].Int32() != 255 || !values[2].Truth() {
		t.Fatalf("portable values = %s", value.Describe())
	}
}

func TestPortableStringTokenizer(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "tokenizer.sl", `
$tokens = [new StringTokenizer: "alpha,beta", ","];
$count = [$tokens countTokens];
$first = [$tokens nextToken];
$second = [$tokens nextElement];
$more = [$tokens hasMoreTokens];
$typed = $tokens isa ^java.util.StringTokenizer;
$delimiters = [new StringTokenizer: "a,b", ",", 1];
$d1 = [$delimiters nextToken];
$d2 = [$delimiters nextToken];
$d3 = [$delimiters nextToken];
return @($count, $first, $second, $more, $typed, $d1, $d2, $d3);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, _ := value.Array()
	values := array.Values()
	if got, want := argvValueStrings(values), []string{"2", "alpha", "beta", "", "1", "a", ",", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("StringTokenizer values = %q, want %q", got, want)
	}
}

func TestPortableJavaStringAndScalarMethods(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "java-string.sl", `
$tokenizer = [new StringTokenizer: "alpha"];
$sleepHash = [SleepUtils getHashScalar];
return @(
	["  value  " trim],
	["A😀B" length],
	["A😀B" substring: 1, 3],
	["same" equals: "same"],
	["same" equals: "other"],
	[["ABC" toLowerCase] contains: "b"],
	["AbC" equalsIgnoreCase: "aBc"],
	["a😀b" indexOf: "b"],
	["a😀ba" lastIndexOf: "a"],
	["abcdef" startsWith: "bcd", 1],
	["abc" toUpperCase],
	[[Long valueOf: "42"] getClass],
	[$tokenizer getClass],
	[Boolean TRUE],
	[Boolean FALSE],
	[SleepUtils getScalar: "sleep"],
	typeOf($sleepHash)
);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{
		"value", "4", "😀", "1", "0", "1", "1", "3", "4", "1", "ABC",
		"class java.lang.Long", "class java.util.StringTokenizer", "1", "0",
		"sleep", "class sleep.engine.types.HashContainer",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("portable Java values = %q, want %q", got, want)
	}
}

func TestPortableJavaCollectionsAndSleepWrappers(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "java-collections.sl", `
$list = [new LinkedList];
[$list add: "b"];
[$list addFirst: "a"];
[$list addLast: "c"];
$iterator = [$list iterator];
@walked = @();
while ([$iterator hasNext]) {
	push(@walked, [$iterator next]);
}
$wrapped = [SleepUtils getArrayWrapper: $list];
$set = [new TreeSet: @("b", "a", "b", "c")];
$map = [new TreeMap: %(z => "zebra", a => "apple")];
return @(
	$wrapped,
	@walked,
	[Collections binarySearch: $wrapped, "b"],
	[$list get: 1],
	[$list size],
	$list isa ^java.util.List,
	[$iterator getClass],
	"$set",
	"$map"
);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	values := array.Values()
	if got, want := values[0].Describe(), "@('a', 'b', 'c')"; got != want {
		t.Fatalf("wrapped collection = %s, want %s", got, want)
	}
	if got, want := values[1].Describe(), "@('a', 'b', 'c')"; got != want {
		t.Fatalf("iterator walk = %s, want %s", got, want)
	}
	if got, want := argvValueStrings(values[2:]), []string{
		"1", "b", "3", "1", "class java.util.Iterator", "[a, b, c]", "{a=apple, z=zebra}",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("portable collection values = %q, want %q", got, want)
	}
}

func TestPortableJavaCollectionIteratorRemovalAndMapWrapper(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "java-collection-mutation.sl", `
$list = [new LinkedList: @("a", "b", "c")];
$iterator = [$list iterator];
[$iterator next];
[$iterator remove];
$map = [new HashMap];
[$map put: "one", 1];
[$map put: "two", 2];
%wrapped = [SleepUtils getHashWrapper: $map];
return @("$list", %wrapped["one"], %wrapped["two"]);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, _ := value.Array()
	if got, want := argvValueStrings(array.Values()), []string{"[b, c]", "1", "2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("portable mutation values = %q, want %q", got, want)
	}
}

func TestPortableThreadYieldIsNoOp(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "thread-yield.sl", `[Thread yield]; return 7;`)
	if err != nil || value.Int32() != 7 {
		t.Fatalf("Thread.yield = (%s, %v), want (7, nil)", value.Describe(), err)
	}
}

func TestPortableThreadCurrentThreadOnSynchronousScript(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "thread-main.sl", `
$thread = [Thread currentThread];
return @("$thread", [$thread toString], [$thread getName], [$thread getPriority], $thread isa ^java.lang.Thread);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{
		"Thread[main,5,main]", "Thread[main,5,main]", "main", "5", "1",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Thread.currentThread values = %q, want %q", got, want)
	}
}

func TestPortableForkCurrentThreadCanonicalCompatibility(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "forkof.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "forkof.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("forkof.sl", programBytes))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := runtime.Execute(ctx, program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func TestImporterObjectHostPrecedesPortableCurrentThread(t *testing.T) {
	t.Parallel()

	calls := 0
	runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		calls++
		if invocation.Class == "Thread" && invocation.Message == "currentThread" {
			return String("importer-thread"), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "thread-override.sl", `return [Thread currentThread];`)
	if err != nil || value.String() != "importer-thread" || calls != 1 {
		t.Fatalf("override = (%s, %v, calls=%d)", value.Describe(), err, calls)
	}
}

func TestImporterObjectHostHasPrecedenceAndUnknownsStayExplicit(t *testing.T) {
	t.Parallel()

	called := 0
	runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		called++
		if invocation.Class == "Math" && invocation.Message == "pow" {
			return String("importer"), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "override.sl", `return [Math pow: 2, 3];`)
	if err != nil || value.String() != "importer" || called != 1 {
		t.Fatalf("override = (%s, %v, calls=%d)", value.Describe(), err, called)
	}
	_, err = runtime.Eval(context.Background(), "unknown.sl", `return [Unknown nope];`)
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("unknown error = %v, want UnsupportedError", err)
	}
}

func TestImporterObjectHostPrecedesPortableCollectionFallback(t *testing.T) {
	t.Parallel()

	calls := 0
	runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		calls++
		if invocation.Op == ObjectConstruct && invocation.Class == "LinkedList" {
			return String("importer-list"), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "collection-override.sl", `return [new LinkedList];`)
	if err != nil || value.String() != "importer-list" || calls != 1 {
		t.Fatalf("collection override = (%s, %v, calls=%d)", value.Describe(), err, calls)
	}
}

func TestMissingPortableCollectionMethodIsSoftWarning(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "collection-unknown.sl", `$list = [new LinkedList];
$value = [$list noSuchMethod];
println("continued");
return $value;`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !value.IsNull() {
		t.Fatalf("missing method result = %s, want null", value.Describe())
	}
	if got, want := output.String(), "Warning: no field/method named noSuchMethod in class java.util.LinkedList at collection-unknown.sl:2\ncontinued\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
