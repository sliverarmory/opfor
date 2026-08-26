package opfor

import (
	"bytes"
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
)

const sleepDebugCoercionProbeName = "basic-utilities-debug-coercion.sl"

// BasicUtilities.debug consumes its optional flag with BridgeUtilities.getInt.
// A Sleep StringValue accepts only a base-ten integer spelling for intValue;
// it does not inherit Java Integer.decode or floating-point fallback behavior.
const sleepDebugCoercionProbe = `$hex = debug("0x10");
$hex_state = debug();
debug(0);
println("hex|" . $hex . "|" . $hex_state . "|" . typeOf($hex));
$fraction = debug("12.9");
$fraction_state = debug();
debug(0);
println("fraction|" . $fraction . "|" . $fraction_state . "|" . typeOf($fraction));
`

const sleepDebugCoercionOutput = `hex|0|0|class sleep.engine.types.IntValue
fraction|0|0|class sleep.engine.types.IntValue
`

func TestSleepBasicUtilitiesDebugStringCoercion(t *testing.T) {
	got := runSleepDebugCoercionProbe(t)
	if !bytes.Equal(got, []byte(sleepDebugCoercionOutput)) {
		t.Fatalf("debug coercion output mismatch\nwant:\n%s\ngot:\n%s", sleepDebugCoercionOutput, got)
	}
}

func TestSleepBasicUtilitiesDebugStringCoercionOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	path := filepath.Join(t.TempDir(), sleepDebugCoercionProbeName)
	if err := os.WriteFile(path, []byte(sleepDebugCoercionProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep debug coercion probe: %v\n%s", err, want)
	}
	got := runSleepDebugCoercionProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("debug coercion mismatch\nofficial:\n%s\nopfor:\n%s", want, got)
	}
}

func runSleepDebugCoercionProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepDebugCoercionProbeName, sleepDebugCoercionProbe); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}
