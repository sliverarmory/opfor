package opfor

import (
	"bytes"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
)

const sleepIndexGeneralProbeName = "index-general.sl"

const sleepIndexGeneralProbe = `debug(2);
@read = @(1);
$read_value = @read[5];
println("read-size=" . size(@read));
println("read-tail=[" . @read[1] . "]");
@binary = @(1);
println("binary-value=[" . @binary[8] . "]");
println("binary-size=" . size(@binary));
@negative = @("a", "b", "c");
println("negative=" . @negative[-8]);
@empty = $null;
$empty_value = @empty[4];
println("empty-size=" . size(@empty));
@assign = @(1);
@assign[5] = 9;
println("assign-size=" . size(@assign));
println("assign-tail=" . @assign[1]);
%nested = %();
%nested["a"]["b"] = 7;
println("nested=" . %nested["a"]["b"]);
sub scalar_assignment {
    $dynamic = $null;
    println("scalar-assign-before");
    $dynamic["k"] = 1;
    println("scalar-assign-after");
}
scalar_assignment();
println("scalar-assign-caller");
sub scalar_read {
    $dynamic = $null;
    println("scalar-read-before");
    $value = $dynamic["k"];
    println("scalar-read-after");
}
scalar_read();
println("scalar-read-caller");
`

const sleepIndexGeneralOutput = `read-size=2
read-tail=[]
binary-value=[]
binary-size=2
negative=b
empty-size=1
assign-size=2
assign-tail=9
nested=7
scalar-assign-before
Warning: invalid use of index operator: $null['k'] at index-general.sl:24
Warning: internal error - class java.util.EmptyStackException at index-general.sl:24
scalar-assign-caller
scalar-read-before
Warning: invalid use of index operator: $null['k'] at index-general.sl:32
scalar-read-caller
`

func TestSleepIndexGeneralScalarSemantics(t *testing.T) {
	if got := runSleepSourceProbe(t, sleepIndexGeneralProbeName, sleepIndexGeneralProbe); !bytes.Equal(got, []byte(sleepIndexGeneralOutput)) {
		t.Fatalf("general index output mismatch\nwant:\n%sgot:\n%s", sleepIndexGeneralOutput, got)
	}
}

func TestSleepIndexGeneralOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepIndexGeneralProbeName)
	if err := os.WriteFile(path, []byte(sleepIndexGeneralProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep general index probe: %v\n%s", err, want)
	}
	if got := runSleepSourceProbe(t, sleepIndexGeneralProbeName, sleepIndexGeneralProbe); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep general index output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}
