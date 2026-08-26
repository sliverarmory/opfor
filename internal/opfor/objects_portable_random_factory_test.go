package opfor

import (
	"context"
	"reflect"
	"testing"
)

func TestPortableJavaRandomGeneratorOfClassicRandom(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-generator-factory.sl", `
import java.util.random.*;
$random = [RandomGenerator of: "Random"];
[$random setSeed: 0L];
$first = [$random nextInt];
[RandomGenerator of: $null];
$nullError = checkError();
[RandomGenerator of: "not-an-algorithm"];
$nameError = checkError();
return @(
    [[$random getClass] getName],
    $random isa ^RandomGenerator,
    $random isa ^java.util.Random,
    $first,
    [[$nullError getClass] getName], [$nullError getMessage],
    [[$nameError getClass] getName], [$nameError getMessage]
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, _ := value.Array()
	if got, want := argvValueStrings(array.Values()), []string{
		"java.util.Random", "1", "1", "-1155484576",
		"java.lang.NullPointerException", "",
		"java.lang.IllegalArgumentException", `No implementation of the random number generator algorithm "not-an-algorithm" is available`,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RandomGenerator.of(Random) = %v, want %v", got, want)
	}
}

func TestPortableJavaRandomGeneratorOfReturnsDistinctObjects(t *testing.T) {
	t.Parallel()

	invocation := ObjectInvocation{
		Op:      ObjectInvoke,
		Class:   "java.util.random.RandomGenerator",
		Message: "of",
		Arguments: []Argument{{
			Value: String("Random"),
		}},
	}
	first, handled, err := portableJavaRandomGeneratorClass(invocation)
	if err != nil || !handled {
		t.Fatalf("first of(Random) = (%s, handled:%v, %v)", first.Describe(), handled, err)
	}
	second, handled, err := portableJavaRandomGeneratorClass(invocation)
	if err != nil || !handled {
		t.Fatalf("second of(Random) = (%s, handled:%v, %v)", second.Describe(), handled, err)
	}
	if first.IdentityEqual(second) {
		t.Fatal("RandomGenerator.of(Random) returned the same object twice")
	}
}

func TestPortableJavaRandomGeneratorFactoryPreservesObjectHostPrecedence(t *testing.T) {
	t.Parallel()

	var calls []string
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Op == ObjectInvoke &&
			resolvePortableClassName(invocation.Class) == "java.util.random.RandomGenerator" {
			switch invocation.Message {
			case "of":
				calls = append(calls, "of")
				return String("importer-generator"), nil
			case "getDefault":
				calls = append(calls, "getDefault")
				return String("importer-default"), nil
			}
		}
		return Null(), &UnsupportedError{Operation: "object"}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-generator-factory-host.sl", `
import java.util.random.*;
$named = [RandomGenerator of: "Random"];
$default = [RandomGenerator getDefault];
return @($named, $default);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, _ := value.Array()
	if got, want := argvValueStrings(array.Values()), []string{"importer-generator", "importer-default"}; !reflect.DeepEqual(got, want) || !reflect.DeepEqual(calls, []string{"of", "getDefault"}) {
		t.Fatalf("ObjectHost factory = (%v, calls %v), want (%v, [of getDefault])", got, calls, want)
	}
}

func TestPortableJavaRandomGeneratorKnownUnsupportedAlgorithmFallsThrough(t *testing.T) {
	t.Parallel()

	for _, message := range []string{"getDefault", "of"} {
		invocation := ObjectInvocation{
			Op: ObjectInvoke, Class: "java.util.random.RandomGenerator", Message: message,
		}
		if message == "of" {
			invocation.Arguments = []Argument{{Value: String("L32X64MixRandom")}}
		}
		value, handled, err := portableJavaRandomGeneratorClass(invocation)
		if err != nil || handled || !value.IsNull() {
			t.Fatalf("%s portable boundary = (%s, handled:%v, %v), want unhandled", message, value.Describe(), handled, err)
		}
	}
}
