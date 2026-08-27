package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepIndexArgumentCellProbeName = "index-argument-cell.sl"

const sleepIndexArgumentCellProbe = `debug(2);
sub invalid_string {
    $s = "abc";
    println("string-before");
    println($s[0]);
    println("string-after");
}
invalid_string();
println("string-caller");
sub null_target {
    $x = $null;
    println("null-before");
    println($x["k"]);
    println("null-after");
}
null_target();
println("null-caller");
@a = @(1);
println("array-before");
println(@a[5]);
println("array-size=" . size(@a));
@b = @(1, 2);
println("negative=" . @b[-3]);
println("negative-size=" . size(@b));
debug(6);
sub literal_null_target {
    println("literal-null-before");
    println($null["k"]);
    println("literal-null-after");
}
literal_null_target();
println("literal-null-caller");
`

const sleepIndexArgumentCellOutput = `string-before
Warning: invalid use of index operator: 'abc'[0] at index-argument-cell.sl:5
string-caller
null-before
Warning: invalid use of index operator: $null['k'] at index-argument-cell.sl:13
null-caller
array-before

array-size=2
negative=2
negative-size=2
literal-null-before
Warning: invalid use of index operator: $null['k'] at index-argument-cell.sl:28
literal-null-caller
`

const sleepCollectionWrapperIndexArgumentProbeName = "collection-wrapper-index-argument.sl"

const sleepCollectionWrapperIndexArgumentProbe = `import java.util.LinkedList;
import sleep.runtime.SleepUtils;
debug(2);
$list = [new LinkedList];
[$list add: "a"];
$wrapped = [SleepUtils getArrayWrapper: $list];
sub wrapper_oob {
    println("wrapper-before");
    println($wrapped[5]);
    println("wrapper-after");
}
wrapper_oob();
println("wrapper-caller");
`

const sleepCollectionWrapperIndexArgumentOutput = `wrapper-before
Warning: attempted an invalid index: Index 5 out of bounds for length 1 at collection-wrapper-index-argument.sl:9
wrapper-caller
`

func TestSleepIndexArgumentCellCompatibility(t *testing.T) {
	if got := runSleepIndexArgumentCellProbe(t); !bytes.Equal(got, []byte(sleepIndexArgumentCellOutput)) {
		t.Fatalf("index argument-cell output mismatch\nwant:\n%sgot:\n%s", sleepIndexArgumentCellOutput, got)
	}
}

func TestSleepIndexArgumentCellOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepIndexArgumentCellProbeName)
	if err := os.WriteFile(path, []byte(sleepIndexArgumentCellProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep index argument-cell probe: %v\n%s", err, want)
	}
	if got := runSleepIndexArgumentCellProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep index argument-cell output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestSleepCollectionWrapperIndexArgumentCompatibility(t *testing.T) {
	if got := runSleepSourceProbe(t, sleepCollectionWrapperIndexArgumentProbeName, sleepCollectionWrapperIndexArgumentProbe); !bytes.Equal(got, []byte(sleepCollectionWrapperIndexArgumentOutput)) {
		t.Fatalf("CollectionWrapper index argument output mismatch\nwant:\n%sgot:\n%s", sleepCollectionWrapperIndexArgumentOutput, got)
	}
}

func TestSleepCollectionWrapperIndexArgumentOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepCollectionWrapperIndexArgumentProbeName)
	if err := os.WriteFile(path, []byte(sleepCollectionWrapperIndexArgumentProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep CollectionWrapper index argument probe: %v\n%s", err, want)
	}
	if got := runSleepSourceProbe(t, sleepCollectionWrapperIndexArgumentProbeName, sleepCollectionWrapperIndexArgumentProbe); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep CollectionWrapper index argument output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepIndexArgumentCellProbe(t *testing.T) []byte {
	return runSleepSourceProbe(t, sleepIndexArgumentCellProbeName, sleepIndexArgumentCellProbe)
}

func runSleepSourceProbe(t *testing.T, name, source string) []byte {
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
	return output.Bytes()
}
