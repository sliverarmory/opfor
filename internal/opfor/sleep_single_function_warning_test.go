package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepSingleFunctionWarningProbeName = "sleep-single-function-warning.sl"

// BasicStrings.func_left and func_right are distinct Function objects whose
// evaluate methods pass the called environment key to BasicStrings.substring.
// The latter throws an IllegalArgumentException when its normalized start
// exceeds its end and delegates the remaining bounds check to String.substring;
// Sleep's Block converts both exception paths to warnings and resumes the
// caller block.
const sleepSingleFunctionWarningProbe = `sub direct_bad {
    println("direct-before");
    left("abc", -9);
    println("direct-tail");
}
direct_bad();
println("direct-resume");
setf("&zleft", function("&left"));
sub alias_bad {
    println("alias-before");
    zleft("abc", -9);
    println("alias-tail");
}
alias_bad();
println("alias-resume");
setf("&zright", function("&right"));
sub right_alias_bad {
    println("right-alias-before");
    zright("abc", -9);
    println("right-alias-tail");
}
right_alias_bad();
println("right-alias-resume");
sub right_bounds_bad {
    println("right-bounds-before");
    right("abc", 9);
    println("right-bounds-tail");
}
right_bounds_bad();
println("right-bounds-resume");
`

const sleepSingleFunctionWarningOutput = `direct-before
Warning: &left: illegal substring('abc', 0 -> 0, -9 -> -6) indices at sleep-single-function-warning.sl:3
direct-resume
alias-before
Warning: &zleft: illegal substring('abc', 0 -> 0, -9 -> -6) indices at sleep-single-function-warning.sl:11
alias-resume
right-alias-before
Warning: &zright: illegal substring('abc', 9 -> 9, 3 -> 3) indices at sleep-single-function-warning.sl:19
right-alias-resume
right-bounds-before
Warning: attempted an invalid index: Range [-6, 3) out of bounds for length 3 at sleep-single-function-warning.sl:26
right-bounds-resume
`

func TestSleepSingleFunctionCalledKeyWarningCompatibility(t *testing.T) {
	if got := runSleepSingleFunctionWarningProbe(t); !bytes.Equal(got, []byte(sleepSingleFunctionWarningOutput)) {
		t.Fatalf("single-function warning output mismatch\nwant:\n%sgot:\n%s", sleepSingleFunctionWarningOutput, got)
	}
}

func TestSleepSingleFunctionCalledKeyWarningOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepSingleFunctionWarningProbeName)
	if err := os.WriteFile(path, []byte(sleepSingleFunctionWarningProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep single-function warning probe: %v\n%s", err, want)
	}
	if got := runSleepSingleFunctionWarningProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep single-function warning output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepSingleFunctionWarningProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepSingleFunctionWarningProbeName, sleepSingleFunctionWarningProbe); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	return append([]byte(nil), output.Bytes()...)
}
