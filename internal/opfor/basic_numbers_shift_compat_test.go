package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepNumericShiftProbeName = "sleep-numeric-shift-distance.sl"

// BasicNumbers.operate at Cobalt-Strike/sleep@60ac3ff9dacc3e7b5a6c58be201c5830afbda398
// applies Java int/long << and >> directly. The official JAR makes the
// resulting low-five/low-six-bit distance masking observable here.
const sleepNumericShiftProbe = `println("int-wrap|" . (1 << 32) . "|" . typeOf(1 << 32));
println("int-negative|" . (1 << -1) . "|" . typeOf(1 << -1));
println("int-right|" . (-8 >> 32) . "|" . typeOf(-8 >> 32));
println("int-wide|" . (3 << 35) . "|" . typeOf(3 << 35));
println("long-wrap|" . (1L << 64) . "|" . typeOf(1L << 64));
println("long-negative|" . (1L << -1) . "|" . typeOf(1L << -1));
println("long-right|" . (-8L >> 64) . "|" . typeOf(-8L >> 64));
println("long-wide|" . (3L << 67) . "|" . typeOf(3L << 67));
println("double-int|" . (1.5 << 32) . "|" . typeOf(1.5 << 32));
`

const sleepNumericShiftOutput = `int-wrap|1|class sleep.engine.types.IntValue
int-negative|-2147483648|class sleep.engine.types.IntValue
int-right|-8|class sleep.engine.types.IntValue
int-wide|24|class sleep.engine.types.IntValue
long-wrap|1|class sleep.engine.types.LongValue
long-negative|-9223372036854775808|class sleep.engine.types.LongValue
long-right|-8|class sleep.engine.types.LongValue
long-wide|24|class sleep.engine.types.LongValue
double-int|1|class sleep.engine.types.IntValue
`

func TestSleepBasicNumbersShiftDistanceMasking(t *testing.T) {
	got := runSleepNumericShiftProbe(t)
	if !bytes.Equal(got, []byte(sleepNumericShiftOutput)) {
		t.Fatalf("numeric shift output mismatch\nwant:\n%s\ngot:\n%s", sleepNumericShiftOutput, got)
	}
}

func TestSleepBasicNumbersShiftDistanceOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	path := filepath.Join(t.TempDir(), sleepNumericShiftProbeName)
	if err := os.WriteFile(path, []byte(sleepNumericShiftProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep numeric shift probe: %v\n%s", err, want)
	}
	got := runSleepNumericShiftProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("numeric shift mismatch\nofficial:\n%s\nopfor:\n%s", want, got)
	}
}

func runSleepNumericShiftProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepNumericShiftProbeName, sleepNumericShiftProbe); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}
