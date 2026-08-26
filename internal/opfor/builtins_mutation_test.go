package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestMutationFunctionSet(t *testing.T) {
	functions := (&Runtime{}).mutationFunctions()
	got := make([]string, 0, len(functions))
	for name := range functions {
		got = append(got, name)
	}
	sort.Strings(got)
	if want := []string{"removeAt", "splice"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("function names = %q, want %q", got, want)
	}
}

func TestRemoveAtArrayUsesChangingSizeAndReturnsNull(t *testing.T) {
	functions := (&Runtime{}).mutationFunctions()
	target := ArrayValue(NewArray(
		String("a"), String("b"), String("c"), String("d"), String("e"),
	))

	result := callMutationBuiltin(t, functions, "removeAt", target, Int(-1), Int(-2))
	if !result.IsNull() {
		t.Fatalf("removeAt return = %s, want null", result.Describe())
	}
	assertValueStrings(t, target, []string{"a", "b", "d"})

	// Normalization adds the size once; it does not wrap modulo like array
	// indexing. Earlier removals remain visible when a later index is invalid.
	_, err := functions["removeAt"](context.Background(), invocationOf(
		"removeAt", target, Int(0), Int(-9),
	))
	if err == nil || !strings.Contains(err.Error(), "Index: -7, Size: 2") {
		t.Fatalf("removeAt invalid-index error = %v", err)
	}
	assertValueStrings(t, target, []string{"b", "d"})
}

func TestRemoveAtHashNullsKeysAndIgnoresScalars(t *testing.T) {
	functions := (&Runtime{}).mutationFunctions()
	hash := NewHash()
	hash.Set("keep", String("value"))
	hash.Set("remove", String("secret"))

	result := callMutationBuiltin(t, functions, "removeAt", HashValue(hash), String("remove"), String("missing"))
	if !result.IsNull() {
		t.Fatalf("removeAt(hash) return = %s, want null", result.Describe())
	}
	for _, key := range []string{"remove", "missing"} {
		value, exists := hash.Get(key)
		if !exists || !value.IsNull() {
			t.Fatalf("hash[%q] = %s, exists %v; want stored null", key, value.Describe(), exists)
		}
	}
	if got, _ := hash.Get("keep"); got.String() != "value" {
		t.Fatalf("hash[keep] = %s, want value", got.Describe())
	}
	if got := callMutationBuiltin(t, functions, "removeAt", String("not a collection"), Int(0)); !got.IsNull() {
		t.Fatalf("removeAt(scalar) = %s, want null", got.Describe())
	}
}

func TestSpliceDefaultsRemovalCountAndReturnsTarget(t *testing.T) {
	functions := (&Runtime{}).mutationFunctions()
	target := ArrayValue(NewArray(Int(1), Int(2), Int(3), Int(4), Int(5), Int(6)))
	insert := ArrayValue(NewArray(String("a"), String("b"), String("c")))

	result := callMutationBuiltin(t, functions, "splice", target, insert, Int(2))
	if !result.IdentityEqual(target) {
		t.Fatal("splice did not return its target array")
	}
	assertValueStrings(t, target, []string{"1", "2", "a", "b", "c", "6"})

	// A zero removal count inserts, and a negative start is normalized from the
	// current end before mutation.
	callMutationBuiltin(t, functions, "splice", target,
		ArrayValue(NewArray(String("tail"))), Int(-1), Int(0))
	assertValueStrings(t, target, []string{"1", "2", "a", "b", "c", "tail", "6"})
}

func TestSpliceClampsBoundsAndSharesInsertedCells(t *testing.T) {
	functions := (&Runtime{}).mutationFunctions()
	target := ArrayValue(NewArray(String("a"), String("b"), String("c")))
	insertArray := NewArray(String("x"), String("y"))
	insert := ArrayValue(insertArray)

	callMutationBuiltin(t, functions, "splice", target, insert, Int(99), Int(99))
	assertValueStrings(t, target, []string{"a", "b", "c", "x", "y"})
	if err := insertArray.Set(0, String("X")); err != nil {
		t.Fatalf("set inserted cell: %v", err)
	}
	assertValueStrings(t, target, []string{"a", "b", "c", "X", "y"})

	// After one-step negative normalization, a still-negative start behaves as
	// the iterator's beginning. Negative removal counts remove nothing.
	callMutationBuiltin(t, functions, "splice", target,
		ArrayValue(NewArray(String("front"))), Int(-99), Int(-1))
	assertValueStrings(t, target, []string{"front", "a", "b", "c", "X", "y"})

	if _, err := functions["splice"](context.Background(), invocationOf("splice", String("wrong"))); err == nil ||
		!strings.Contains(err.Error(), "expected array") {
		t.Fatalf("splice wrong-target error = %v", err)
	}
}

func TestAddInvalidIndexFromDirectInvokeRemainsAnError(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	target := ArrayValue(NewArray(String("a"), String("b"), String("c"), String("d")))

	_, err = runtime.Invoke(context.Background(), "add", target, String("bad"), Int(-10))
	if err == nil || !strings.Contains(err.Error(), "index -5 out of range for array of size 4") {
		t.Fatalf("Invoke error = %v, want invalid insertion index error", err)
	}
	assertValueStrings(t, target, []string{"a", "b", "c", "d"})
}

func TestSleepNativeMessagesGolden(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "nmesgs.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "nmesgs.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("nmesgs.sl", programBytes))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func callMutationBuiltin(t *testing.T, functions map[string]NativeFunc, name string, values ...Value) Value {
	t.Helper()
	function := functions[name]
	if function == nil {
		t.Fatalf("function %q is not registered", name)
	}
	result, err := function(context.Background(), invocationOf(name, values...))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return result
}
