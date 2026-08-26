package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
)

const sleepBasicIOWarningProbeName = "opfor-basicio-warning-probe.sl"

// This probe covers BasicIO.java at Cobalt-Strike/sleep@60ac3ff9dacc3e7b5a6c58be201c5830afbda398:
// digest/checksum (232-353), pack/unpack (1229-1262), readb (1290-1354),
// chooseSource (641-663), and available/mark/reset (1161-1225).
const sleepBasicIOWarningProbe = `debug(2);
sub digest_bad {
  println("digest-before");
  $value = digest("abc", "bogus");
  println("digest-after|" . $value);
}
digest_bad();
checkError($problem);
println("digest-error|" . $problem);
println("digest-caller");
sub checksum_case {
  println("checksum-before");
  $value = checksum("abc", "crc32");
  println("checksum-after|" . $value);
}
checksum_case();
checkError($problem);
println("checksum-error|" . $problem);
println("checksum-caller");
@malformed = unpack("1", "abc");
println("unpack-count|" . size(@malformed));
sub pack_bad {
  println("pack-before");
  $value = pack("H*", "abc");
  println("pack-after|" . $value);
}
pack_bad();
println("pack-caller");
sub readb_negative {
  $handle = allocate();
  writeb($handle, "abc");
  closef($handle);
  println("readb-negative-before");
  $value = readb($handle, -2);
  println("readb-negative-after|" . $value);
  checkError($problem);
  println("readb-negative-error|" . $problem);
}
readb_negative();
println("readb-negative-caller");
sub readb_invalid {
  println("readb-invalid-before");
  readb("bad", 1);
  println("readb-invalid-after");
}
readb_invalid();
println("readb-invalid-caller");
sub writeb_invalid {
  println("writeb-invalid-before");
  writeb("bad", "x");
  println("writeb-invalid-after");
}
writeb_invalid();
println("writeb-invalid-caller");
sub mark_invalid {
  println("mark-invalid-before");
  mark("bad", 1);
  println("mark-invalid-after");
}
mark_invalid();
println("mark-invalid-caller");
$wide = allocate();
writeb($wide, "xy");
closef($wide);
println("readb-wide|" . readb($wide, 4294967297L));
println("readb-wide-tail|" . readb($wide, -1));
println("available-invalid|" . available("bad"));
reset("bad");
println("reset-invalid|continued");
`

const sleepBasicIOWarningProbeOutput = `digest-before
Warning: checkError(): java.security.NoSuchAlgorithmException: bogus MessageDigest not available at opfor-basicio-warning-probe.sl:4
digest-after|
digest-error|java.security.NoSuchAlgorithmException: bogus MessageDigest not available
digest-caller
checksum-before
Warning: null value error at opfor-basicio-warning-probe.sl:13
checksum-error|
checksum-caller
unpack-count|0
pack-before
Warning: can not pack 'abc' as hex string, number of characters must be even at opfor-basicio-warning-probe.sl:24
pack-caller
readb-negative-before
Warning: checkError(): java.lang.NegativeArraySizeException: -2 at opfor-basicio-warning-probe.sl:34
readb-negative-after|
readb-negative-error|java.lang.NegativeArraySizeException: -2
readb-negative-caller
readb-invalid-before
Warning: expected I/O handle argument, received: 'bad' at opfor-basicio-warning-probe.sl:43
readb-invalid-caller
writeb-invalid-before
Warning: expected I/O handle argument, received: 'bad' at opfor-basicio-warning-probe.sl:50
writeb-invalid-caller
mark-invalid-before
Warning: expected I/O handle argument, received: 'bad' at opfor-basicio-warning-probe.sl:57
mark-invalid-caller
readb-wide|x
readb-wide-tail|y
available-invalid|
reset-invalid|continued
`

func TestSleepBasicIOWarningCompatibility(t *testing.T) {
	got := runSleepBasicIOWarningProbe(t)
	if !bytes.Equal(got, []byte(sleepBasicIOWarningProbeOutput)) {
		t.Fatalf("BasicIO warning output mismatch\nwant:\n%sgot:\n%s", sleepBasicIOWarningProbeOutput, got)
	}
}

func TestSleepBasicIOWarningOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for BasicIO warning verification")
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
	path := filepath.Join(directory, sleepBasicIOWarningProbeName)
	if err := os.WriteFile(path, []byte(sleepBasicIOWarningProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep BasicIO warning probe: %v\n%s", err, want)
	}

	got := runSleepBasicIOWarningProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep BasicIO warning output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestSleepBasicIOMarkUsesJavaIntAndAvailableSwallowsFailures(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	functions := runtimeInstance.ioFunctions()
	handleValue := readableMemoryHandle(t, runtimeInstance, functions, "abc")
	mustCallIOBuiltin(t, runtimeInstance, functions, "mark", handleValue, Long(1<<32+1))
	handle, ok := ioHandleValue(handleValue)
	if !ok || handle.markLimit != 1 {
		t.Fatalf("mark limit = %d, want Java int conversion to 1", handle.markLimit)
	}

	file, err := os.CreateTemp(t.TempDir(), "available-closed-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	closedUnderlying := ObjectValue(newIOHandle("closed-underlying", file, nil, false, false, false))
	value, err := callIOBuiltin(context.Background(), runtimeInstance, functions, "available", closedUnderlying)
	if err != nil || !value.IsNull() {
		t.Fatalf("available on failed underlying stream = (%s, %v), want ($null, nil)", value.Describe(), err)
	}
}

func runSleepBasicIOWarningProbe(t *testing.T) []byte {
	t.Helper()

	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), sleepBasicIOWarningProbeName, sleepBasicIOWarningProbe); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	return output.Bytes()
}
