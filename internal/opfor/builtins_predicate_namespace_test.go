package opfor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sleepBasicStringsPredicateGroupingProbe = `sub concat_predicate {
    if ("a" . "b") { println("concat-true"); }
    println("concat-tail");
}
concat_predicate();
println("after-concat");
sub multiply_predicate {
    if ("a" x 2) { println("multiply-true"); }
    println("multiply-tail");
}
multiply_predicate();
println("after-multiply");
sub compare_predicate {
    if ("a" cmp "b") { println("compare-true"); }
    println("compare-tail");
}
compare_predicate();
println("after-compare");
sub spaceship_predicate {
    if (1 <=> 2) { println("spaceship-true"); }
    println("spaceship-tail");
}
spaceship_predicate();
println("after-spaceship");
`

const sleepBasicStringsPredicateGroupingOutput = `Warning: attempted an invalid cast: class sleep.bridges.BasicStrings$oper_concat cannot be cast to class sleep.interfaces.Predicate (sleep.bridges.BasicStrings$oper_concat and sleep.interfaces.Predicate are in unnamed module of loader 'app') at basicstrings-predicate-grouping.sl:2
after-concat
Warning: attempted an invalid cast: class sleep.bridges.BasicStrings$oper_multiply cannot be cast to class sleep.interfaces.Predicate (sleep.bridges.BasicStrings$oper_multiply and sleep.interfaces.Predicate are in unnamed module of loader 'app') at basicstrings-predicate-grouping.sl:8
after-multiply
Warning: attempted an invalid cast: class sleep.bridges.BasicStrings$oper_compare cannot be cast to class sleep.interfaces.Predicate (sleep.bridges.BasicStrings$oper_compare and sleep.interfaces.Predicate are in unnamed module of loader 'app') at basicstrings-predicate-grouping.sl:14
after-compare
Warning: attempted an invalid cast: class sleep.bridges.BasicStrings$oper_spaceship cannot be cast to class sleep.interfaces.Predicate (sleep.bridges.BasicStrings$oper_spaceship and sleep.interfaces.Predicate are in unnamed module of loader 'app') at basicstrings-predicate-grouping.sl:20
after-spaceship
`

func TestSleepBasicStringsOperatorsAreNotPredicates(t *testing.T) {
	if got := runSleepBasicStringsPredicateGroupingProbe(t); !bytes.Equal(got, []byte(sleepBasicStringsPredicateGroupingOutput)) {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepBasicStringsPredicateGroupingOutput, got)
	}
}

func TestSleepBasicStringsPredicateGroupingOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	path := filepath.Join(t.TempDir(), "basicstrings-predicate-grouping.sl")
	if err := os.WriteFile(path, []byte(sleepBasicStringsPredicateGroupingProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep BasicStrings predicate grouping probe: %v\n%s", err, want)
	}
	got := runSleepBasicStringsPredicateGroupingProbe(t)
	if !bytes.Equal(normalizeSleepBasicStringsPredicateGroupingJVMOutput(got), normalizeSleepBasicStringsPredicateGroupingJVMOutput(want)) {
		t.Fatalf("official Sleep BasicStrings predicate grouping mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

// ClassCastException added module/loader qualification in Java 9. The bridge
// types and block-recovery behavior are the compatibility contract; normalize
// only that JVM-version-dependent decoration so a user-supplied Java 8 oracle
// remains useful too. The fixed OPFOR golden above still checks the complete
// Java 26 spelling emitted by the pure-Go runtime.
func normalizeSleepBasicStringsPredicateGroupingJVMOutput(output []byte) []byte {
	text := string(output)
	target := "sleep.interfaces.Predicate"
	for _, helper := range []string{"oper_concat", "oper_multiply", "oper_compare", "oper_spaceship"} {
		actual := "sleep.bridges.BasicStrings$" + helper
		text = strings.ReplaceAll(text, "class "+actual, actual)
		text = strings.ReplaceAll(text, "class "+target, target)
		text = strings.ReplaceAll(text, fmt.Sprintf(
			" (%s and %s are in unnamed module of loader 'app')", actual, target,
		), "")
	}
	return []byte(text)
}

func TestSleepBasicStringsOperatorsRemainOperators(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Eval(context.Background(), "basicstrings-operators.sl", `
return @("a" . "b", "ab" x 2, "a" cmp "b", 1 <=> 2);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := result.Array()
	if !ok || array.Len() != 4 {
		t.Fatalf("operator result = %s, want four values", result.Describe())
	}
	want := []Value{String("ab"), String("abab"), Int(-1), Int(-1)}
	for index, expected := range want {
		value, present := array.Get(index)
		if !present || !value.IdentityEqual(expected) {
			t.Errorf("operator result %d = (%s, %v), want %s", index, value.Describe(), present, expected.Describe())
		}
	}
}

func runSleepBasicStringsPredicateGroupingProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "basicstrings-predicate-grouping.sl", sleepBasicStringsPredicateGroupingProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
