package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepIORuntimeUnderflowProbeName = "opfor-io-runtime-underflow-probe.sl"

const sleepIORuntimeUnderflowProbe = `sub openf_zero { println("openf-before"); openf(); println("openf-after"); }
openf_zero(); println("openf-caller");
sub wait_zero { println("wait-before"); wait(); println("wait-after"); }
wait_zero(); println("wait-caller");
sub sizeof_zero { println("sizeof-before"); sizeof(); println("sizeof-after"); }
sizeof_zero(); println("sizeof-caller");
sub bread_zero { println("bread-before"); bread(); println("bread-after"); }
bread_zero(); println("bread-caller");
sub bwrite_zero { println("bwrite-before"); bwrite(); println("bwrite-after"); }
bwrite_zero(); println("bwrite-caller");
sub pack_zero { println("pack-before"); pack(); println("pack-after"); }
pack_zero(); println("pack-caller");
sub fork_zero { println("fork-before"); fork(); println("fork-after"); }
fork_zero(); println("fork-caller");
sub read_zero { println("read-before"); read(); println("read-after"); }
read_zero(); println("read-caller");
sub encoding_zero { println("encoding-before"); setEncoding(); println("encoding-after"); }
encoding_zero(); println("encoding-caller");
sub use_zero { println("use-before"); use(); println("use-after"); }
use_zero(); println("use-caller");
sub eval_zero { println("eval-before"); eval(); println("eval-after"); }
eval_zero(); println("eval-caller");
sub popl_zero { println("popl-before"); popl(); println("popl-after"); }
popl_zero(); println("popl-caller");
checkError($why); println("done|" . $why);
`

const sleepIORuntimeUnderflowProbeOutput = `openf-before
Warning: internal error - class java.util.EmptyStackException at opfor-io-runtime-underflow-probe.sl:1
openf-caller
wait-before
Warning: null value error at opfor-io-runtime-underflow-probe.sl:3
wait-caller
sizeof-before
Warning: null value error at opfor-io-runtime-underflow-probe.sl:5
sizeof-caller
bread-before
Warning: null value error at opfor-io-runtime-underflow-probe.sl:7
bread-caller
bwrite-before
Warning: null value error at opfor-io-runtime-underflow-probe.sl:9
bwrite-caller
pack-before
Warning: null value error at opfor-io-runtime-underflow-probe.sl:11
pack-caller
fork-before
Warning: expected &closure--received: $null at opfor-io-runtime-underflow-probe.sl:13
fork-caller
read-before
Warning: expected &closure--received: $null at opfor-io-runtime-underflow-probe.sl:15
read-caller
encoding-before
Warning: &setEncoding: specified a non-existent encoding '' at opfor-io-runtime-underflow-probe.sl:17
encoding-caller
use-before
Warning: internal error - class java.util.EmptyStackException at opfor-io-runtime-underflow-probe.sl:19
use-caller
eval-before
Warning: internal error - class java.util.EmptyStackException at opfor-io-runtime-underflow-probe.sl:21
eval-caller
popl-before
Warning: &popl: no more local frames exist at opfor-io-runtime-underflow-probe.sl:23
popl-caller
done|
`

func TestSleepIORuntimeUnderflowCompatibility(t *testing.T) {
	got := runSleepIORuntimeUnderflowProbe(t)
	if !bytes.Equal(got, []byte(sleepIORuntimeUnderflowProbeOutput)) {
		t.Fatalf("underflow output mismatch\nwant:\n%sgot:\n%s", sleepIORuntimeUnderflowProbeOutput, got)
	}
}

func TestSleepIORuntimeUnderflowOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, sleepIORuntimeUnderflowProbeName)
	if err := os.WriteFile(path, []byte(sleepIORuntimeUnderflowProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep IO/runtime underflow probe: %v\n%s", err, want)
	}

	got := runSleepIORuntimeUnderflowProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep IO/runtime underflow output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepIORuntimeUnderflowProbe(t *testing.T) []byte {
	t.Helper()

	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), sleepIORuntimeUnderflowProbeName, sleepIORuntimeUnderflowProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
