package opfor

import (
	"bytes"
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
	"time"
)

const sleepTimeDateLongCoercionProbeName = "time-date-long-coercion.sl"

// TimeDateBridge.formatDate consumes an explicit timestamp through
// BridgeUtilities.getLong. StringValue.longValue delegates to Long.parseLong,
// including Character.digit-compatible decimal text and zero on conversion
// failure; it does not use Integer.decode or floating-point fallback.
const sleepTimeDateLongCoercionProbe = `println("leading=" . formatDate("010", "ss.SSS"));
println("hex=" . formatDate("0x10", "ss.SSS"));
println("fraction=" . formatDate("10.9", "ss.SSS"));
println("spaced=" . formatDate(" 10 ", "ss.SSS"));
println("arabic=" . formatDate("٠١٠", "ss.SSS"));
println("fullwidth=" . formatDate("０１０", "ss.SSS"));
println("negative=" . formatDate("-010", "ss.SSS"));
println("overflow=" . formatDate("9223372036854775808", "ss.SSS"));
`

const sleepTimeDateLongCoercionOutput = `leading=00.010
hex=00.000
fraction=00.000
spaced=00.000
arabic=00.010
fullwidth=00.010
negative=59.990
overflow=00.000
`

func TestSleepTimeDateBridgeLongStringCoercion(t *testing.T) {
	got := runSleepTimeDateLongCoercionProbe(t)
	if !bytes.Equal(got, []byte(sleepTimeDateLongCoercionOutput)) {
		t.Fatalf("TimeDateBridge long coercion mismatch\nwant:\n%s\ngot:\n%s", sleepTimeDateLongCoercionOutput, got)
	}
}

func TestSleepTimeDateBridgeLongStringCoercionOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepTimeDateLongCoercionProbeName)
	if err := os.WriteFile(path, []byte(sleepTimeDateLongCoercionProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(
		java,
		"-Duser.timezone=UTC", "-Duser.language=en", "-Duser.country=US",
		"-jar", jar, path,
	)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep TimeDateBridge long-coercion probe: %v\n%s", err, want)
	}
	if got := runSleepTimeDateLongCoercionProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep TimeDateBridge long-coercion mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func runSleepTimeDateLongCoercionProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(
		WithClock(ClockFunc(func() time.Time { return time.UnixMilli(0).UTC() })),
		WithStdout(&output), WithStderr(&output),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepTimeDateLongCoercionProbeName, sleepTimeDateLongCoercionProbe); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	return append([]byte(nil), output.Bytes()...)
}
