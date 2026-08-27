package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepArithmeticWarningProbeName = "sleep-arithmetic-warning-probe.sl"

const sleepArithmeticWarningProbe = `println(1.0 / 0.0);
println(0.0 / 0.0);
println(5.0 % 0.0);
println(-2147483648 / -1);
println(-9223372036854775808L / -1L);

sub int_div_zero {
    println(5 / 0);
    println("unreachable-int-div");
}
int_div_zero();
println("after-int-div");

sub int_mod_zero {
    println(5 % 0);
    println("unreachable-int-mod");
}
int_mod_zero();
println("after-int-mod");

sub long_div_zero {
    println(5L / 0L);
    println("unreachable-long-div");
}
long_div_zero();
println("after-long-div");

sub long_mod_zero {
    println(5L % 0L);
    println("unreachable-long-mod");
}
long_mod_zero();
println("after-long-mod");
`

const sleepArithmeticWarningOutput = `Infinity
NaN
NaN
-2147483648
-9223372036854775808
Warning: / by zero at sleep-arithmetic-warning-probe.sl:8
after-int-div
Warning: / by zero at sleep-arithmetic-warning-probe.sl:15
after-int-mod
Warning: / by zero at sleep-arithmetic-warning-probe.sl:22
after-long-div
Warning: / by zero at sleep-arithmetic-warning-probe.sl:29
after-long-mod
`

func TestSleepBasicNumbersArithmeticWarnings(t *testing.T) {
	if got := runSleepArithmeticWarningProbe(t); got != sleepArithmeticWarningOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepArithmeticWarningOutput, got)
	}
}

func TestSleepBasicNumbersArithmeticWarningsOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, sleepArithmeticWarningProbeName)
	if err := os.WriteFile(path, []byte(sleepArithmeticWarningProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep arithmetic warning probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepArithmeticWarningProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepArithmeticWarningProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), sleepArithmeticWarningProbeName, sleepArithmeticWarningProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
