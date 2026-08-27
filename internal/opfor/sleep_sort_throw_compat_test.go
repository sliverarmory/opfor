package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const sleepSortThrowProbe = `$calls = 0;
sub cmp {
    $calls++;
    if ($calls == 5) { throw "boom"; }
    return $1 <=> $2;
}
@l = @(3,7,2,9,1,8,0,6,4,5);
try {
    sort(&cmp, @l);
    println("unreachable");
}
catch $e {
    println("caught=" . $e);
}
println("calls=" . $calls);
println(@l);
`

const sleepSortThrowOutput = `caught=boom
calls=5
@(2, 3, 7, 9, 1, 8, 0, 6, 4, 5)
`

const sleepCollectionsSortThrowProbe = `import java.util.*;
$calls = 0;
$comparator = newInstance(^Comparator, {
    if ($0 eq "compare") {
        $calls++;
        if ($calls == 5) { throw "boom"; }
        return $1 <=> $2;
    }
});
$list = [new ArrayList: @(3,7,2,9,1,8,0,6,4,5)];
try {
    [Collections sort: $list, $comparator];
    println("unreachable");
}
catch $e {
    println("caught=" . $e);
}
println("calls=" . $calls);
println($list);
`

const sleepCollectionsSortThrowOutput = `unreachable
calls=5
[2, 3, 7, 9, 1, 8, 0, 6, 4, 5]
`

const sleepCollectionsClosureSortNegativeThrowProbe = `import java.util.*;
$calls = 0;
sub cmp {
    $calls++;
    if ($calls == 5) { throw -1; }
    return $1 <=> $2;
}
$list = [new ArrayList: @(3,7,2,9,1,8,0,6,4,5)];
try {
    [Collections sort: $list, &cmp];
    println("unreachable");
}
catch $e {
    println("caught=" . $e);
}
println("calls=" . $calls);
println($list);
`

const sleepSortNegativeThrowOutput = `caught=-1
calls=5
@(5, 4, 6, 0, 8, 1, 9, 2, 3, 7)
`

const sleepCollectionsSortNegativeThrowOutput = `unreachable
calls=5
[2, 3, 7, 9, 1, 8, 0, 6, 4, 5]
`

func TestSleepSortDefersComparatorThrowUntilAfterCommit(t *testing.T) {
	if got := runSleepSortThrowProbe(t, "sleep-sort-throw.sl", sleepSortThrowProbe); got != sleepSortThrowOutput {
		t.Fatalf("stock sort throw output\nwant:\n%sgot:\n%s", sleepSortThrowOutput, got)
	}
}

func TestPortableCollectionsSortConsumesDeferredComparatorThrowAfterCommit(t *testing.T) {
	if got := runSleepSortThrowProbe(t, "collections-sort-throw.sl", sleepCollectionsSortThrowProbe); got != sleepCollectionsSortThrowOutput {
		t.Fatalf("Collections.sort throw output\nwant:\n%sgot:\n%s", sleepCollectionsSortThrowOutput, got)
	}
}

func TestSleepSortDeferredThrowUsesBridgeSpecificCoercion(t *testing.T) {
	stock := strings.Replace(sleepSortThrowProbe, `throw "boom"`, `throw -1`, 1)
	if got := runSleepSortThrowProbe(t, "sleep-sort-negative-throw.sl", stock); got != sleepSortNegativeThrowOutput {
		t.Fatalf("stock numeric throw output\nwant:\n%sgot:\n%s", sleepSortNegativeThrowOutput, got)
	}

	collections := strings.Replace(sleepCollectionsSortThrowProbe, `throw "boom"`, `throw -1`, 1)
	if got := runSleepSortThrowProbe(t, "collections-sort-negative-throw.sl", collections); got != sleepCollectionsSortNegativeThrowOutput {
		t.Fatalf("Collections.sort numeric throw output\nwant:\n%sgot:\n%s", sleepCollectionsSortNegativeThrowOutput, got)
	}
	if got := runSleepSortThrowProbe(
		t, "collections-closure-sort-negative-throw.sl", sleepCollectionsClosureSortNegativeThrowProbe,
	); got != sleepCollectionsSortNegativeThrowOutput {
		t.Fatalf("Collections.sort closure numeric throw output\nwant:\n%sgot:\n%s", sleepCollectionsSortNegativeThrowOutput, got)
	}
}

func TestSleepSortThrowOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	probes := []struct {
		name   string
		source string
		want   string
	}{
		{name: "stock sort", source: sleepSortThrowProbe, want: sleepSortThrowOutput},
		{name: "Collections.sort", source: sleepCollectionsSortThrowProbe, want: sleepCollectionsSortThrowOutput},
		{
			name:   "stock sort numeric throw",
			source: strings.Replace(sleepSortThrowProbe, `throw "boom"`, `throw -1`, 1),
			want:   sleepSortNegativeThrowOutput,
		},
		{
			name:   "Collections.sort numeric throw",
			source: strings.Replace(sleepCollectionsSortThrowProbe, `throw "boom"`, `throw -1`, 1),
			want:   sleepCollectionsSortNegativeThrowOutput,
		},
		{
			name:   "Collections.sort closure numeric throw",
			source: sleepCollectionsClosureSortNegativeThrowProbe,
			want:   sleepCollectionsSortNegativeThrowOutput,
		},
	}
	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			reference, err := officialSleepJavaCommand(java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", probe.source).CombinedOutput()
			if err != nil {
				t.Fatalf("official Sleep probe: %v\n%s", err, reference)
			}
			if string(reference) != probe.want {
				t.Fatalf("official Sleep output changed\nwant:\n%sgot:\n%s", probe.want, reference)
			}
			if got := []byte(runSleepSortThrowProbe(t, probe.name+".sl", probe.source)); !bytes.Equal(got, reference) {
				t.Fatalf("official Sleep output mismatch\nwant:\n%sgot:\n%s", reference, got)
			}
		})
	}
}

func TestSleepSortComparatorMixedThrowAndAuthoritativeErrorIsAtomic(t *testing.T) {
	functions := newSequenceFunctionSet()
	want := &LimitError{Resource: resourceInstruction, Limit: 7}
	values := ArrayValue(NewArray(Int(3), Int(2), Int(1)))
	callback := FunctionValue(testCallable(func(context.Context, ...Value) (Value, error) {
		return Null(), errors.Join(&scriptThrow{value: String("boom")}, want)
	}))

	_, err := functions["sort"](context.Background(), invocationOf("sort", callback, values))
	var limit *LimitError
	if !errors.Is(err, ErrResourceLimit) || !errors.As(err, &limit) || limit.Resource != resourceInstruction || limit.Limit != 7 {
		t.Fatalf("mixed comparator error = %v, want authoritative resource failure", err)
	}
	assertValueStrings(t, values, []string{"3", "2", "1"})
}

func TestPortableCollectionsSortMixedThrowAndAuthoritativeErrorIsAtomic(t *testing.T) {
	want := errors.New("importer comparator failed")
	comparator := FunctionValue(CallableFunc(func(context.Context, ...Value) (Value, error) {
		return Null(), errors.Join(&scriptThrow{value: Int(-1)}, want)
	}))
	list := newPortableJavaCollection("ArrayList", []Value{Int(3), Int(2), Int(1)})

	handled, err := portableCollectionsUtilityErrorForTest(
		context.Background(), "sort", ObjectValue(list), comparator,
	)
	if !handled || !errors.Is(err, want) {
		t.Fatalf("mixed Comparator error = (handled %t, %v), want authoritative importer failure", handled, err)
	}
	if got := argvValueStrings(list.snapshot()); fmt.Sprint(got) != "[3 2 1]" {
		t.Fatalf("list after authoritative Comparator failure = %v, want unchanged", got)
	}
}

func TestSleepStableTimSortContinuesWithDeferredThrowScalar(t *testing.T) {
	input := []Value{Int(3), Int(7), Int(2), Int(9), Int(1), Int(8), Int(0), Int(6), Int(4), Int(5)}
	flow := &sleepSortComparatorFlow{}
	calls := 0
	got, err := sleepStableTimSort(input, func(left, right Value) (int, error) {
		return flow.compare(context.Background(), func() (Value, error) {
			calls++
			if calls == 5 {
				return Null(), &scriptThrow{value: Int(-1)}
			}
			return Int(left.Int32() - right.Int32()), nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 5 {
		t.Fatalf("comparator calls = %d, want 5", calls)
	}
	if got := argvValueStrings(got); fmt.Sprint(got) != "[5 4 6 0 8 1 9 2 3 7]" {
		t.Fatalf("sort result = %v", got)
	}
}

func TestSortSequenceArrayCommitsBeforeDeferredThrowIsReturned(t *testing.T) {
	array := NewArray(Int(3), Int(7), Int(2), Int(9), Int(1), Int(8), Int(0), Int(6), Int(4), Int(5))
	flow := &sleepSortComparatorFlow{}
	calls := 0
	result, err := sortSequenceArray(context.Background(), "sort", array, func(left, right Value) (int, error) {
		return flow.compare(context.Background(), func() (Value, error) {
			calls++
			if calls == 5 {
				return Null(), &scriptThrow{value: String("boom")}
			}
			return Int(left.Int32() - right.Int32()), nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IdentityEqual(ArrayValue(array)) {
		t.Fatal("sort result did not retain the input array")
	}
	if got := argvValueStrings(array.Values()); fmt.Sprint(got) != "[2 3 7 9 1 8 0 6 4 5]" {
		t.Fatalf("committed array = %v", got)
	}
}

func runSleepSortThrowProbe(t *testing.T, name, source string) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), name, source); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
