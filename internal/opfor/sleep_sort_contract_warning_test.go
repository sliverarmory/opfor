package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

const sleepSortContractWarningProbeName = "sleep-sort-contract-warning-probe.sl"

const sleepSortContractWarningProbe = `@sortn_values = @();
srand(1);
@choices = @(0, 2147483647, -1);
$index = 0;
while ($index < 56) {
    push(@sortn_values, rand(@choices));
    $index++;
}
sub invalid_sortn {
    sortn(@sortn_values);
    println("unreachable-sortn");
}
invalid_sortn();
println("sortn-after=" . size(@sortn_values) . ":" . @sortn_values[0] . ":" . @sortn_values[-1]);

global('$sort_name_seen');
$sort_name_seen = 0;
sub cyclic_compare {
    if (!$sort_name_seen) {
        println("custom-name=" . $0);
        $sort_name_seen = 1;
    }
    $left = $1 % 3;
    $right = $2 % 3;
    if ($left == $right) { return 0; }
    if ((($left + 1) % 3) == $right) { return -1; }
    return 1;
}
@custom_values = @();
$index = 0;
while ($index < 73) {
    push(@custom_values, (($index * 37) + 11) % 101);
    $index++;
}
sub invalid_custom_sort {
    sort(&cyclic_compare, @custom_values);
    println("unreachable-custom");
}
invalid_custom_sort();
println("custom-after=" . size(@custom_values) . ":" . @custom_values[0] . ":" . @custom_values[-1]);
`

const sleepSortContractWarningOutput = `Warning: Comparison method violates its general contract! at sleep-sort-contract-warning-probe.sl:10
sortn-after=56:0:2147483647
custom-name=&sort
Warning: Comparison method violates its general contract! at sleep-sort-contract-warning-probe.sl:36
custom-after=73:11:49
`

func TestSleepSortComparatorContractWarningRecovery(t *testing.T) {
	if got := runSleepSortContractWarningProbe(t); got != sleepSortContractWarningOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepSortContractWarningOutput, got)
	}
}

func TestSleepSortComparatorContractWarningOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for sort contract verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	directory := t.TempDir()
	path := filepath.Join(directory, sleepSortContractWarningProbeName)
	if err := os.WriteFile(path, []byte(sleepSortContractWarningProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep sort contract probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepSortContractWarningProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep sort contract output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepSortContractWarningProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(
		context.Background(), sleepSortContractWarningProbeName, sleepSortContractWarningProbe,
	); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestSleepStableTimSortComparatorErrorIsAtomic(t *testing.T) {
	input := []int{
		29, 3, 17, 1, 23, 5, 19, 7, 31, 11, 13, 2, 37, 0, 41, 4,
		43, 6, 47, 8, 53, 10, 59, 12, 61, 14, 67, 16, 71, 18, 73, 20,
		79, 22, 83, 24, 89, 26, 97, 28, 101, 30, 103, 32, 107, 34, 109, 36,
	}
	wantInput := slices.Clone(input)
	comparisons := 0
	got, err := sleepStableTimSort(input, func(left, right int) (int, error) {
		comparisons++
		if comparisons == 37 {
			return 0, context.Canceled
		}
		switch {
		case left < right:
			return -1, nil
		case left > right:
			return 1, nil
		default:
			return 0, nil
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Fatalf("result on comparator error = %v, want nil", got)
	}
	if !slices.Equal(input, wantInput) {
		t.Fatalf("input mutated after comparator error: got %v, want %v", input, wantInput)
	}
}

type sleepTimSortPropertyItem struct {
	key      int
	original int
}

func TestSleepStableTimSortMatchesStableReference(t *testing.T) {
	state := uint64(0x6a09e667f3bcc909)
	for length := 0; length <= 1024; length++ {
		input := make([]sleepTimSortPropertyItem, length)
		for index := range input {
			state = state*6364136223846793005 + 1442695040888963407
			input[index] = sleepTimSortPropertyItem{key: int(int32(state>>32) % 23), original: index}
		}
		want := append([]sleepTimSortPropertyItem(nil), input...)
		sort.SliceStable(want, func(left, right int) bool { return want[left].key < want[right].key })
		got, err := sleepStableTimSort(input, func(left, right sleepTimSortPropertyItem) (int, error) {
			switch {
			case left.key < right.key:
				return -1, nil
			case left.key > right.key:
				return 1, nil
			default:
				return 0, nil
			}
		})
		if err != nil {
			t.Fatalf("length %d: %v", length, err)
		}
		if len(got) != len(want) {
			t.Fatalf("length %d: result length = %d, want %d", length, len(got), len(want))
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("length %d index %d = %+v, want %+v", length, index, got[index], want[index])
			}
		}
	}
}
