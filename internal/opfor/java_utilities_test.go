package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestPortableJavaUtilityCanonicalCompatibility(t *testing.T) {
	tests := []struct {
		name              string
		arguments         []Value
		taint             bool
		normalize         bool
		fixtureWorkingDir bool
	}{
		{name: "byteconvert", fixtureWorkingDir: true},
		{name: "byteconvert2", fixtureWorkingDir: true},
		{name: "cast", normalize: true},
		{name: "castbug"},
		{name: "native_arrays"},
		{name: "newInstance"},
		{name: "debugproxy"},
		{name: "setField2"},
		{name: "wrong", normalize: true},
		{name: "taint7", arguments: []Value{String("2 + 2")}, taint: true, normalize: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", test.name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", test.name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(test.name+".sl", programBytes))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			var output bytes.Buffer
			options := []Option{WithStdout(&output), WithStderr(&output)}
			if test.taint {
				options = append(options, WithTaintMode(true))
			}
			runtime, err := New(options...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if test.fixtureWorkingDir {
				fixtureRoot, err := filepath.Abs(filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := runtime.Invoke(context.Background(), "chdir", String(fixtureRoot)); err != nil {
					t.Fatalf("set fixture cwd: %v", err)
				}
			}
			if _, err := runtime.Execute(context.Background(), program, test.arguments...); err != nil {
				t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
			}
			got := output.String()
			expected := string(want)
			if test.normalize {
				got = normalizePortableJavaIdentity(got)
				expected = normalizePortableJavaIdentity(expected)
			}
			if got != expected {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", expected, got)
			}
		})
	}
}

var portableJavaIdentityPattern = regexp.MustCompile(`(?:\[[^\s@]+|java\.io\.PrintStream)@[[:xdigit:]]+`)

func normalizePortableJavaIdentity(value string) string {
	return portableJavaIdentityPattern.ReplaceAllStringFunc(value, func(identity string) string {
		return identity[:strings.LastIndexByte(identity, '@')+1] + "<identity>"
	})
}

func TestPortableJavaReflectArrayNewInstanceBuildScalarAndRawCharAssignment(t *testing.T) {
	t.Parallel()

	converted, handled, err := portableJavaReflectArray(ObjectInvocation{
		Op:      ObjectInvoke,
		Class:   "Array",
		Message: "newInstance",
		Arguments: []Argument{
			{Value: ObjectValue(classReference("char"))},
			{Value: Int(2)},
		},
	})
	if err != nil || !handled {
		t.Fatalf("Array.newInstance(char, 2) = (%s, %t, %v)", converted.Describe(), handled, err)
	}
	if got, want := sleepStringUnits(converted), []uint16{0, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Array.newInstance(char, 2) units = %x, want BuildScalar string %x", got, want)
	}
	if _, handled, err := portableJavaReflectArray(ObjectInvocation{
		Op: ObjectInvoke, Class: "Array", Message: "set",
		Arguments: []Argument{{Value: converted}, {Value: Int(0)}, {Value: String("Z")}},
	}); !handled || err == nil || err.Error() != "java.lang.IllegalArgumentException: Argument is not an array" {
		t.Fatalf("Array.set(BuildScalar char[]) = (handled %t, %v), want Argument is not an array", handled, err)
	}

	array := newPortableJavaArray(
		portableArrayType("char"), []int{2},
		[]Value{sleepUTF16CharacterValue(0), sleepUTF16CharacterValue(0)},
	)
	arrayValue := ObjectValue(array)

	set := func(value Value) error {
		_, handled, err := portableJavaReflectArray(ObjectInvocation{
			Op:      ObjectInvoke,
			Class:   "Array",
			Message: "set",
			Arguments: []Argument{
				{Value: arrayValue},
				{Value: Int(0)},
				{Value: value},
			},
		})
		if !handled {
			t.Fatal("Array.set was not handled")
		}
		return err
	}

	if err := set(portableBoxedObject(String("Z"))); err == nil || !strings.Contains(err.Error(), "argument type mismatch") {
		t.Fatalf("Array.set(char[], Object(String)) error = %v, want argument type mismatch", err)
	}
	character := ObjectValue(&portableJavaPrimitive{
		class: "java.lang.Character",
		value: sleepUTF16CharacterValue('Z'),
	})
	if err := set(character); err != nil {
		t.Fatalf("Array.set(char[], Character) error = %v", err)
	}
	if got, want := sleepStringUnits(array.toSleepValue()), []uint16{'Z', 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("char[] units = %x, want %x", got, want)
	}
}

func TestPortableJavaArrayConcurrentReflectAccess(t *testing.T) {
	t.Parallel()

	array := newPortableJavaArray(portableArrayType("char"), []int{1}, []Value{sleepUTF16CharacterValue('A')})
	arrayValue := ObjectValue(array)
	characters := []Value{
		ObjectValue(&portableJavaPrimitive{class: "java.lang.Character", value: sleepUTF16CharacterValue('A')}),
		ObjectValue(&portableJavaPrimitive{class: "java.lang.Character", value: sleepUTF16CharacterValue('Z')}),
	}

	const iterations = 2000
	errors := make(chan error, 4)
	var workers sync.WaitGroup
	workers.Add(4)
	go func() {
		defer workers.Done()
		for index := 0; index < iterations; index++ {
			_, handled, err := portableJavaReflectArray(ObjectInvocation{
				Op: ObjectInvoke, Class: "Array", Message: "set",
				Arguments: []Argument{{Value: arrayValue}, {Value: Int(0)}, {Value: characters[index&1]}},
			})
			if err != nil || !handled {
				errors <- fmt.Errorf("Array.set = (handled %t, %v)", handled, err)
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		for index := 0; index < iterations; index++ {
			value, err := array.get(0)
			if err != nil || (value.String() != "A" && value.String() != "Z") {
				errors <- fmt.Errorf("array.get = (%s, %v)", value.Describe(), err)
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		for index := 0; index < iterations; index++ {
			units := sleepStringUnits(array.toSleepValue())
			if len(units) != 1 || (units[0] != 'A' && units[0] != 'Z') {
				errors <- fmt.Errorf("array.toSleepValue units = %x", units)
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		for index := 0; index < iterations; index++ {
			for _, message := range []string{"equals", "binarySearch"} {
				arguments := []Argument{{Value: arrayValue}, {Value: characters[0]}}
				if message == "equals" {
					arguments[1] = Argument{Value: arrayValue}
				}
				if _, handled, err := portableJavaArrays(ObjectInvocation{
					Op: ObjectInvoke, Class: "Arrays", Message: message, Arguments: arguments,
				}); err != nil || !handled {
					errors <- fmt.Errorf("Arrays.%s = (handled %t, %v)", message, handled, err)
					return
				}
			}
		}
	}()
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestSetFieldUsesImporterObjectSetBoundary(t *testing.T) {
	type importerObject struct{ value string }
	target := &importerObject{}
	var calls []ObjectInvocation
	runtime, err := New(
		WithFunction("fixtureObject", func(context.Context, Invocation) (Value, error) {
			return ObjectValue(target), nil
		}),
		WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			if invocation.Op != ObjectSet {
				return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message}
			}
			calls = append(calls, invocation)
			if invocation.Target.Kind() == KindObject {
				target.value = invocation.Arg(0).String()
			}
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = runtime.Eval(context.Background(), "set-importer.sl", `
$target = fixtureObject();
setField($target, value => "updated");
setField(^Fixture, count => 7);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if target.value != "updated" || len(calls) != 2 {
		t.Fatalf("ObjectSet calls = %+v, target = %+v", calls, target)
	}
	if calls[0].Target.Kind() != KindObject || calls[0].Message != "value" || calls[0].Arg(0).String() != "updated" {
		t.Fatalf("instance ObjectSet = %+v", calls[0])
	}
	if !calls[1].Target.IsNull() || calls[1].Class != "Fixture" || calls[1].Message != "count" || calls[1].Arg(0).Int32() != 7 {
		t.Fatalf("static ObjectSet = %+v", calls[1])
	}
}

func TestSetFieldPreservesImporterErrors(t *testing.T) {
	sentinel := errors.New("importer rejected field write")
	runtime, err := New(
		WithFunction("fixtureObject", func(context.Context, Invocation) (Value, error) {
			return ObjectValue(&struct{}{}), nil
		}),
		WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			if invocation.Op == ObjectSet {
				return Null(), sentinel
			}
			return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message}
		})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = runtime.Eval(context.Background(), "set-importer-error.sl", `$target = fixtureObject(); setField($target, value => 1);`)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Eval error = %v, want importer sentinel", err)
	}
}

func TestSetFieldImporterErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			calls := 0
			target := ObjectValue(&struct{}{})
			runtimeInstance, err := New(
				WithInitialGlobals(map[string]Value{"field_target": target}),
				WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
					if invocation.Op == ObjectSet {
						calls++
						return String("discarded ObjectHost partial result"), boundaryErr
					}
					return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message}
				})),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			_, err = runtimeInstance.Eval(context.Background(), "set-field-boundary-error.sl", `setField($field_target, value => 1);`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("script error = %v, want authoritative %v", err, boundaryErr)
			}
			if calls != 1 {
				t.Fatalf("ObjectHost calls = %d, want one", calls)
			}
		})
	}
}

func TestObjectHostErrorsRemainAuthoritativeThroughNativeCallbacks(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			calls := 0
			runtimeInstance, err := New(
				WithInitialGlobals(map[string]Value{"boundary_object": ObjectValue(&struct{}{})}),
				WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
					if invocation.Op == ObjectInvoke && invocation.Message == "value" {
						calls++
						return String("discarded ObjectHost partial result"), boundaryErr
					}
					return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message}
				})),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			_, err = runtimeInstance.Eval(context.Background(), "nested-object-boundary-error.sl", `map({ return [$boundary_object value]; }, @("item"));`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("nested ObjectHost error = %v, want authoritative %v", err, boundaryErr)
			}
			if calls != 1 {
				t.Fatalf("ObjectHost calls = %d, want one", calls)
			}
		})
	}
}

func TestCastImmediateRetainsJavaWrapperClasses(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "casti-classes.sl", `
return @(
    [casti(1, "z") getClass],
    [casti(1, "b") getClass],
    [casti("x", "c") getClass],
    [casti(1, "h") getClass],
    [casti(1, "i") getClass],
    [casti(1, "l") getClass],
    [casti(1, "f") getClass],
    [casti(1, "d") getClass]
);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	got := make([]string, 0, array.Len())
	for _, item := range array.Values() {
		got = append(got, item.String())
	}
	want := []string{
		"class java.lang.Boolean", "class java.lang.Byte", "class java.lang.Character", "class java.lang.Short",
		"class java.lang.Integer", "class java.lang.Long", "class java.lang.Float", "class java.lang.Double",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("casti classes = %q, want %q", got, want)
	}
}
