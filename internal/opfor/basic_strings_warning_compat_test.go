package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const basicStringsWarningProbeName = "sleep-string-warning-probe.sl"

const basicStringsWarningProbe = `println(charAt("abc", -1));
println(replaceAt("abc", "x", 3, 1));
println(replaceAt("abc", "x", 2, 99));

sub asc_empty {
    println(asc(""));
    println("unreachable-asc");
}
asc_empty();
println("after-asc");

sub char_high {
    println(charAt("abc", 4));
    println("unreachable-char-high");
}
char_high();
println("after-char-high");

sub byte_low {
    println(byteAt("abc", -4));
    println("unreachable-byte-low");
}
byte_low();
println("after-byte-low");

sub replace_low {
    println(replaceAt("abc", "x", -20, 1));
    println("unreachable-replace-low");
}
replace_low();
println("after-replace-low");

sub replace_negative_count {
    println(replaceAt("abc", "x", 1, -1));
    println("unreachable-replace-count");
}
replace_negative_count();
println("after-replace-count");

sub replace_overflow {
    println(replaceAt("abc", "x", 2147483647, 1));
    println("unreachable-replace-overflow");
}
replace_overflow();
println("after-replace-overflow");
`

const basicStringsWarningOutput = `c
abcx
abx
Warning: attempted an invalid index: Index 0 out of bounds for length 0 at sleep-string-warning-probe.sl:6
after-asc
Warning: attempted an invalid index: Index 4 out of bounds for length 3 at sleep-string-warning-probe.sl:13
after-char-high
Warning: attempted an invalid index: Index -1 out of bounds for length 3 at sleep-string-warning-probe.sl:20
after-byte-low
Warning: attempted an invalid index: Range [-17, -16) out of bounds for length 3 at sleep-string-warning-probe.sl:27
after-replace-low
Warning: attempted an invalid index: Range [1, 0) out of bounds for length 3 at sleep-string-warning-probe.sl:34
after-replace-count
Warning: attempted an invalid index: Range [2147483647, -2147483648) out of bounds for length 3 at sleep-string-warning-probe.sl:41
after-replace-overflow
`

func TestSleepBasicStringsInvalidIndexWarnings(t *testing.T) {
	got := runBasicStringsWarningProbe(t)
	if got != basicStringsWarningOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", basicStringsWarningOutput, got)
	}
}

func TestSleepBasicStringsInvalidIndexWarningsOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, basicStringsWarningProbeName)
	if err := os.WriteFile(path, []byte(basicStringsWarningProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep BasicStrings warning probe: %v\n%s", err, want)
	}
	if got := []byte(runBasicStringsWarningProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runBasicStringsWarningProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), basicStringsWarningProbeName, basicStringsWarningProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
