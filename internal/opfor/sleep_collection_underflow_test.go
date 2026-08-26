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

const sleepCollectionUnderflowProbe = `sub pop_missing {
    pop();
    println("pop-missing-tail");
}
pop_missing();
println("pop-missing-resume");
sub pop_empty {
    @items = @();
    pop(@items);
    println("pop-empty-tail");
}
pop_empty();
println("pop-empty-resume");
sub shift_missing {
    shift();
    println("shift-missing-tail");
}
shift_missing();
println("shift-missing-resume");
sub remove_missing {
    removeAt();
    println("remove-missing-tail");
}
remove_missing();
println("remove-missing-resume");
`

const sleepCollectionUnderflowOutput = `Warning: &pop: expected array. received $null at opfor-collection-underflow-probe.sl:2
pop-missing-resume
Warning: attempted an invalid index: Index: -1, Size: 0 at opfor-collection-underflow-probe.sl:9
pop-empty-resume
Warning: attempted an invalid index: Index: 0, Size: 0 at opfor-collection-underflow-probe.sl:15
shift-missing-resume
Warning: internal error - class java.util.EmptyStackException at opfor-collection-underflow-probe.sl:21
remove-missing-resume
`

func TestSleepCollectionUnderflowCompatibility(t *testing.T) {
	if got := runSleepCollectionUnderflowProbe(t); !bytes.Equal(got, []byte(sleepCollectionUnderflowOutput)) {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepCollectionUnderflowOutput, got)
	}
}

func TestSleepCollectionUnderflowOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for collection underflow verification")
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
	path := filepath.Join(directory, "opfor-collection-underflow-probe.sl")
	if err := os.WriteFile(path, []byte(sleepCollectionUnderflowProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep collection underflow probe: %v\n%s", err, want)
	}
	if got := runSleepCollectionUnderflowProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep collection underflow output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepCollectionUnderflowProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "opfor-collection-underflow-probe.sl", sleepCollectionUnderflowProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
