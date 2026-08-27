package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type boundaryMutableIterator struct {
	nextErr   error
	removeErr error
	yielded   bool
}

func (iterator *boundaryMutableIterator) Next(context.Context) (Value, bool, error) {
	if iterator.nextErr != nil {
		return String("discarded iterator partial result"), true, iterator.nextErr
	}
	if iterator.yielded {
		return Null(), false, nil
	}
	iterator.yielded = true
	return String("value"), true, nil
}

func (iterator *boundaryMutableIterator) Remove(context.Context) error {
	return iterator.removeErr
}

func TestImporterIteratorErrorsBypassVMAndNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run("Next/"+boundaryErr.Error(), func(t *testing.T) {
			iterator := &boundaryMutableIterator{nextErr: boundaryErr}
			runtimeInstance, err := New(WithInitialGlobals(map[string]Value{"boundary_iterator": ObjectValue(iterator)}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			_, err = runtimeInstance.Eval(context.Background(), "foreach-next-boundary-error.sl", `foreach $value ($boundary_iterator) { }`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("foreach Next error = %v, want authoritative %v", err, boundaryErr)
			}
		})

		t.Run("NestedNext/"+boundaryErr.Error(), func(t *testing.T) {
			iterator := &boundaryMutableIterator{nextErr: boundaryErr}
			runtimeInstance, err := New(WithInitialGlobals(map[string]Value{"boundary_iterator": ObjectValue(iterator)}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			_, err = runtimeInstance.Eval(context.Background(), "nested-foreach-next-boundary-error.sl", `
map({
    foreach $value ($boundary_iterator) { }
    return 1;
}, @(1));
`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("nested foreach Next error = %v, want authoritative %v", err, boundaryErr)
			}
		})

		t.Run("Remove/"+boundaryErr.Error(), func(t *testing.T) {
			iterator := &boundaryMutableIterator{removeErr: boundaryErr}
			runtimeInstance, err := New(WithInitialGlobals(map[string]Value{"boundary_iterator": ObjectValue(iterator)}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			_, err = runtimeInstance.Eval(context.Background(), "foreach-remove-boundary-error.sl", `foreach $value ($boundary_iterator) { remove(); }`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("foreach Remove error = %v, want authoritative %v", err, boundaryErr)
			}
		})
	}
}

const foreachNonIterableProbeSource = `$count = 0;
foreach $x ("text") { $count++; }
foreach $x ("") { $count++; }
foreach $x ($null) { $count++; }
foreach $x (0) { $count++; }
println("count=" . $count);
`

const foreachHashInsertProbeSource = `%values = ohash(a => "apple", b => "bat", c => "cat");
foreach $key => $value (%values) {
    println($key . "=" . $value);
    if ($key eq "a") {
        %values["d"] = "dog";
    }
}
println("done");
`

const foreachHashAccessProbeSource = `%values = ohasha(a => "apple", b => "bat", c => "cat");
foreach $key => $value (%values) {
    println($key . "=" . $value);
    if ($key eq "a") {
        $ignored = %values["b"];
    }
}
println("done");
`

const foreachHashRemoveProbeSource = `%values = ohash(a => "apple", b => "bat", c => "cat");
foreach $key => $value (%values) {
    println($key . "=" . $value);
    if ($key eq "a") {
        remove(%values, "bat");
    }
}
println("done");
`

const foreachHashMissProbeSource = `%values = ohash(a => "apple", b => "bat", c => "cat");
foreach $key => $value (%values) {
    println($key . "=" . $value);
    if ($key eq "a") {
        $ignored = %values["missing"];
    }
}
println("done");
`

func TestForeachRejectsNonIterableScalarsLikeSleep(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Eval(context.Background(), "foreach-non-iterable.sl", foreachNonIterableProbeSource); err != nil {
		t.Fatal(err)
	}
	want := "Warning: Attempted to use foreach on non-array: 'text' at foreach-non-iterable.sl:2\n" +
		"Warning: Attempted to use foreach on non-array: '' at foreach-non-iterable.sl:3\n" +
		"Warning: Attempted to use foreach on non-array: '' at foreach-non-iterable.sl:4\n" +
		"Warning: Attempted to use foreach on non-array: '0' at foreach-non-iterable.sl:5\n" +
		"count=0\n"
	if got := output.String(); got != want {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

type nonSleepForeachCallable struct{ calls int }

func (c *nonSleepForeachCallable) Invoke(context.Context, ...Value) (Value, error) {
	c.calls++
	return String("unexpected"), nil
}

func (*nonSleepForeachCallable) String() string { return "native-function" }

func TestForeachRejectsNonSleepFunctionObjects(t *testing.T) {
	callable := &nonSleepForeachCallable{}
	var output bytes.Buffer
	runtime, err := New(
		WithStdout(&output),
		WithStderr(&output),
		WithFunction("native_function", func(context.Context, Invocation) (Value, error) {
			return FunctionValue(callable), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(context.Background(), "foreach-native-function.sl", `
$count = 0;
foreach $item (native_function()) { $count++; }
return $count;
`)
	if err != nil {
		t.Fatal(err)
	}
	if value.Int32() != 0 || callable.calls != 0 {
		t.Fatalf("foreach result = %s, native calls = %d", value.Describe(), callable.calls)
	}
	want := "Warning: Attempted to use foreach on non-array: 'native-function' at foreach-native-function.sl:3\n"
	if got := output.String(); got != want {
		t.Fatalf("warning = %q, want %q", got, want)
	}
}

type importerIteratorFixture struct {
	values  []Value
	next    int
	current int
	removes int
}

func newImporterIteratorFixture(values ...Value) *importerIteratorFixture {
	return &importerIteratorFixture{values: append([]Value(nil), values...), current: -1}
}

func (i *importerIteratorFixture) Next(ctx context.Context) (Value, bool, error) {
	if err := ctx.Err(); err != nil {
		return Null(), false, err
	}
	if i.next >= len(i.values) {
		return Null(), false, nil
	}
	i.current = i.next
	i.next++
	return i.values[i.current], true, nil
}

func (i *importerIteratorFixture) Remove(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if i.current < 0 || i.current >= len(i.values) {
		return fmt.Errorf("iterator has no current element")
	}
	copy(i.values[i.current:], i.values[i.current+1:])
	i.values = i.values[:len(i.values)-1]
	i.next--
	i.current = -1
	i.removes++
	return nil
}

func TestImporterIteratorSupportsForeachRemovalAndSequenceBuiltins(t *testing.T) {
	var foreachFixture *importerIteratorFixture
	runtime, err := New(WithFunction("iterator_fixture", func(_ context.Context, invocation Invocation) (Value, error) {
		switch invocation.Arg(0).String() {
		case "foreach":
			foreachFixture = newImporterIteratorFixture(String("keep"), String("drop"), String("tail"))
			return ObjectValue(foreachFixture), nil
		case "map":
			return ObjectValue(newImporterIteratorFixture(String("a"), String("b"))), nil
		default:
			return ObjectValue(IteratorFunc(func(context.Context) (Value, bool, error) {
				return Null(), false, nil
			})), nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(context.Background(), "importer-iterator.sl", `
@walked = @();
$source = iterator_fixture("foreach");
foreach $index => $item ($source) {
    push(@walked, $index . ":" . $item);
    if ($item eq "drop") {
        push(@walked, "same:" . (remove() is $source));
    }
}
@mapped = map({ return uc($1); }, iterator_fixture("map"));
return @(@walked, @mapped);
`)
	if err != nil {
		t.Fatal(err)
	}
	outer, ok := value.Array()
	if !ok || outer.Len() != 2 {
		t.Fatalf("result = %s, want pair", value.Describe())
	}
	walkedValue, _ := outer.Get(0)
	walked, _ := walkedValue.Array()
	if got := valuesToStrings(walked.Values()); fmt.Sprint(got) != "[0:keep 1:drop same:1 1:tail]" {
		t.Fatalf("foreach walk = %v", got)
	}
	mappedValue, _ := outer.Get(1)
	mapped, _ := mappedValue.Array()
	if got := valuesToStrings(mapped.Values()); fmt.Sprint(got) != "[A B]" {
		t.Fatalf("mapped values = %v", got)
	}
	if foreachFixture == nil || foreachFixture.removes != 1 || fmt.Sprint(valuesToStrings(foreachFixture.values)) != "[keep tail]" {
		t.Fatalf("importer iterator after remove = %#v", foreachFixture)
	}
}

func TestForeachNonIterableOfficialJARDifferential(t *testing.T) {
	assertForeachOfficialJARDifferential(t, "foreach-non-iterable.sl", foreachNonIterableProbeSource)
}

func TestForeachHashMutationOfficialJARDifferential(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{name: "foreach-hash-insert.sl", source: foreachHashInsertProbeSource},
		{name: "foreach-hash-remove.sl", source: foreachHashRemoveProbeSource},
		{name: "foreach-hash-miss.sl", source: foreachHashMissProbeSource},
		{name: "foreach-hash-access.sl", source: foreachHashAccessProbeSource},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertForeachOfficialJARDifferential(t, test.name, test.source)
		})
	}
}

func assertForeachOfficialJARDifferential(t *testing.T, name, source string) {
	t.Helper()
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep foreach probe: %v\n%s", err, want)
	}

	var got bytes.Buffer
	runtime, err := New(WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Eval(context.Background(), filepath.Base(path), source); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("official Sleep foreach output mismatch\nwant:\n%sgot:\n%s", want, got.Bytes())
	}
}

func valuesToStrings(values []Value) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}
