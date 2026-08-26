package opfor

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestPortableJavaRandomConstructorsClassAndSeededScriptShape(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-object.sl", `
$short = [new Random: 0L];
$qualified = [new java.util.Random: 0L];
return @(
	[[$short getClass] getName],
	$short isa ^Random,
	$short isa ^java.util.Random,
	$short isa ^java.io.Serializable,
	[$short nextInt: 100],
	[$qualified nextInt: 100]
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{"java.util.Random", "1", "1", "1", "60", "60"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Random constructors/type/sequence = %q, want %q", got, want)
	}

	// This OPFOR-authored snippet covers the ordinary bounded-selection loop
	// without importing executable evidence from an external .cna project.
	value, err = runtimeInstance.Eval(context.Background(), "random-selection-loop.sl", `
$random = [new Random: 12345L];
$alphabet = "abcdefghij";
$result = "";
for ($index = 0; $index < 6; $index++) {
	$choice = [$random nextInt: int(strlen($alphabet))];
	$result .= substr($alphabet, $choice, $choice + 1);
}
return $result;
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != "babife" {
		t.Fatalf("seeded Random selection loop = %q, want babife", got)
	}
}

func TestPortableJavaRandomOpenJDKPrimitiveVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		args    []Value
		want    Value
	}{
		{name: "nextInt", message: "nextInt", want: Int(-1155484576)},
		{name: "nextInt bounded", message: "nextInt", args: []Value{Int(100)}, want: Int(60)},
		{name: "nextInt power of two", message: "nextInt", args: []Value{Int(16)}, want: Int(11)},
		{name: "nextLong", message: "nextLong", want: Long(-4962768465676381896)},
		{name: "nextBoolean", message: "nextBoolean", want: Bool(true)},
		{name: "nextFloat", message: "nextFloat", want: Double(0.7309677600860596)},
		{name: "nextDouble", message: "nextDouble", want: Double(0.730967787376657)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			random := newPortableJavaRandom(0)
			got := invokePortableRandom(t, random, test.message, test.args...)
			if got.Kind() == KindDouble {
				if math.Float64bits(got.Float64()) != math.Float64bits(test.want.Float64()) {
					t.Fatalf("%s = %.17g, want %.17g", test.message, got.Float64(), test.want.Float64())
				}
			} else if !got.IdentityEqual(test.want) {
				t.Fatalf("%s = %s, want %s", test.message, got.Describe(), test.want.Describe())
			}
		})
	}

	falseBoolean := invokePortableRandom(t, newPortableJavaRandom(4096), "nextBoolean")
	if falseBoolean.Kind() != KindInt || falseBoolean.Int32() != 0 {
		t.Fatalf("false nextBoolean = %s, want Java boolean integer 0", falseBoolean.Describe())
	}

	random := newPortableJavaRandom(12345)
	first := invokePortableRandom(t, random, "nextInt", Int(1000))
	if result := invokePortableRandom(t, random, "setSeed", Long(12345)); !result.IsNull() {
		t.Fatalf("setSeed result = %s, want null", result.Describe())
	}
	second := invokePortableRandom(t, random, "nextInt", Int(1000))
	if first.Int32() != second.Int32() {
		t.Fatalf("setSeed replay = %d then %d", first.Int32(), second.Int32())
	}
}

func TestPortableJavaRandomBadBoundUsesJavaSoftError(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtimeInstance.Eval(context.Background(), "random-bound.sl", `
$random = [new Random: 0L];
$result = [$random nextInt: 0];
$error = checkError();
return @($result, "$error", [[$error getClass] getName], [$error getMessage]);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{"", "java.lang.IllegalArgumentException: bound must be positive", "java.lang.IllegalArgumentException", "bound must be positive"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bad-bound soft error = %q, want %q", got, want)
	}
}

func TestPortableJavaRandomConcurrentStateTransitions(t *testing.T) {
	t.Parallel()

	random := newPortableJavaRandom(7)
	const workers = 8
	const calls = 200
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := 0; index < calls; index++ {
				value, handled, err := random.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "nextInt"})
				if err != nil || !handled || value.Kind() != KindInt {
					t.Errorf("concurrent nextInt = (%s, %v, %v)", value.Describe(), handled, err)
					return
				}
			}
		}()
	}
	wait.Wait()

	wantRandom := newSleepJavaRandom(7)
	for index := 0; index < workers*calls; index++ {
		wantRandom.next(32)
	}
	want := int32(wantRandom.next(32))
	got := invokePortableRandom(t, random, "nextInt")
	if got.Int32() != want {
		t.Fatalf("post-concurrency nextInt = %d, want %d after %d transitions", got.Int32(), want, workers*calls)
	}
}

func TestPortableJavaRandomPreservesObjectHostPrecedence(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("importer rejected Random")
	for _, test := range []struct {
		name    string
		host    ObjectHost
		want    string
		wantErr error
	}{
		{
			name: "handled",
			host: ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
				if invocation.Op == ObjectConstruct && resolvePortableClassName(invocation.Class) == "java.util.Random" {
					return String("importer-random"), nil
				}
				return Null(), &UnsupportedError{Operation: "object"}
			}),
			want: "importer-random",
		},
		{
			name: "unsupported falls through",
			host: ObjectHostFunc(func(context.Context, ObjectInvocation) (Value, error) {
				return Null(), &UnsupportedError{Operation: "object"}
			}),
			want: "60",
		},
		{
			name: "fatal",
			host: ObjectHostFunc(func(context.Context, ObjectInvocation) (Value, error) {
				return Null(), wantErr
			}),
			wantErr: wantErr,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, err := New(WithObjectHost(test.host))
			if err != nil {
				t.Fatal(err)
			}
			code := `return [new Random: 0L];`
			if test.name == "unsupported falls through" {
				code = `$random = [new Random: 0L]; return [$random nextInt: 100];`
			}
			value, evalErr := runtimeInstance.Eval(context.Background(), "random-host.sl", code)
			if test.wantErr != nil {
				if !errors.Is(evalErr, test.wantErr) {
					t.Fatalf("Eval error = %v, want %v", evalErr, test.wantErr)
				}
				return
			}
			if evalErr != nil || value.String() != test.want {
				t.Fatalf("Eval = (%s, %v), want %q", value.Describe(), evalErr, test.want)
			}
		})
	}
}

func TestPortableJavaRandomGeneratorOverloadPreservesObjectHostPrecedence(t *testing.T) {
	t.Parallel()

	var intercepted int
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Op == ObjectInvoke && invocation.Message == "nextLong" && len(invocation.Arguments) == 1 {
			intercepted++
			return Long(777), nil
		}
		return Null(), &UnsupportedError{Operation: "object"}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-generator-host.sl", `
$random = [new Random: 0L];
return [$random nextLong: 1000L];
`)
	if err != nil {
		t.Fatal(err)
	}
	if value.Int64() != 777 || intercepted != 1 {
		t.Fatalf("ObjectHost overload interception = (%s, calls %d), want (777, 1)", value.Describe(), intercepted)
	}
}

func TestPortableJavaRandomDistributionOverloadsPreserveObjectHostPrecedence(t *testing.T) {
	t.Parallel()

	var intercepted []string
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Op != ObjectInvoke {
			return Null(), &UnsupportedError{Operation: "object"}
		}
		switch {
		case invocation.Message == "nextGaussian" && len(invocation.Arguments) == 2:
			intercepted = append(intercepted, "nextGaussian")
			return Double(12.5), nil
		case invocation.Message == "nextExponential" && len(invocation.Arguments) == 0:
			intercepted = append(intercepted, "nextExponential")
			return Double(6.25), nil
		default:
			return Null(), &UnsupportedError{Operation: "object"}
		}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-distribution-host.sl", `
$random = [new Random: 0L];
return @([$random nextGaussian: 2.0, 3.0], [$random nextExponential]);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{"12.5", "6.25"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ObjectHost distribution interception = %q, want %q", got, want)
	}
	// Sleep evaluates array-literal elements right-to-left before materializing
	// them in source order.
	if want := []string{"nextExponential", "nextGaussian"}; !reflect.DeepEqual(intercepted, want) {
		t.Fatalf("ObjectHost distribution calls = %q, want %q", intercepted, want)
	}
}

func invokePortableRandom(t *testing.T, random *portableJavaRandom, message string, arguments ...Value) Value {
	t.Helper()
	invocationArguments := make([]Argument, len(arguments))
	for index, argument := range arguments {
		invocationArguments[index] = Argument{Value: argument}
	}
	value, handled, err := random.invoke(ObjectInvocation{
		Op:        ObjectInvoke,
		Message:   message,
		Arguments: invocationArguments,
	})
	if err != nil || !handled {
		t.Fatalf("%s = (%s, handled %v, %v)", message, value.Describe(), handled, err)
	}
	return value
}
