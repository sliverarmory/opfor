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

const sleepCollectionTargetWarningProbe = `sub case_add_missing {
    addAll();
    println("add-missing-tail");
}
case_add_missing();
println("add-missing-resume");
sub case_add_scalar {
    addAll("scalar", @("x"));
    println("add-scalar-tail");
}
case_add_scalar();
println("add-scalar-resume");
sub case_remove_missing {
    removeAll();
    println("remove-missing-tail");
}
case_remove_missing();
println("remove-missing-resume");
sub case_remove_scalar {
    removeAll(7, @());
    println("remove-scalar-tail");
}
case_remove_scalar();
println("remove-scalar-resume");
sub case_retain_missing {
    retainAll();
    println("retain-missing-tail");
}
case_retain_missing();
println("retain-missing-resume");
sub case_retain_scalar {
    retainAll("scalar", @());
    println("retain-scalar-tail");
}
case_retain_scalar();
println("retain-scalar-resume");
sub case_sublist_missing {
    sublist();
    println("sublist-missing-tail");
}
case_sublist_missing();
println("sublist-missing-resume");
sub case_sublist_scalar {
    sublist("scalar");
    println("sublist-scalar-tail");
}
case_sublist_scalar();
println("sublist-scalar-resume");
sub case_splice_missing {
    splice();
    println("splice-missing-tail");
}
case_splice_missing();
println("splice-missing-resume");
sub case_splice_scalar {
    splice("scalar", @());
    println("splice-scalar-tail");
}
case_splice_scalar();
println("splice-scalar-resume");
sub case_miss_missing {
    setMissPolicy();
    println("miss-missing-tail");
}
case_miss_missing();
println("miss-missing-resume");
sub case_miss_scalar {
    setMissPolicy("scalar", lambda({ return $null; }));
    println("miss-scalar-tail");
}
case_miss_scalar();
println("miss-scalar-resume");
sub case_removal_missing {
    setRemovalPolicy();
    println("removal-missing-tail");
}
case_removal_missing();
println("removal-missing-resume");
sub case_removal_scalar {
    setRemovalPolicy("scalar", lambda({ return $null; }));
    println("removal-scalar-tail");
}
case_removal_scalar();
println("removal-scalar-resume");
sub case_miss_closure_missing {
    $hash = ohash();
    setMissPolicy($hash);
    println("miss-closure-missing-tail");
}
case_miss_closure_missing();
println("miss-closure-missing-resume");
sub case_removal_closure_missing {
    $hash = ohash();
    setRemovalPolicy($hash);
    println("removal-closure-missing-tail");
}
case_removal_closure_missing();
println("removal-closure-missing-resume");
`

const sleepCollectionTargetWarningOutput = `Warning: &addAll: expected array. received $null at opfor-collection-target-probe.sl:2
add-missing-resume
Warning: &addAll: expected array. received 'scalar' at opfor-collection-target-probe.sl:8
add-scalar-resume
Warning: &removeAll: expected array. received $null at opfor-collection-target-probe.sl:14
remove-missing-resume
Warning: &removeAll: expected array. received 7 at opfor-collection-target-probe.sl:20
remove-scalar-resume
Warning: &retainAll: expected array. received $null at opfor-collection-target-probe.sl:26
retain-missing-resume
Warning: &retainAll: expected array. received 'scalar' at opfor-collection-target-probe.sl:32
retain-scalar-resume
Warning: &sublist: expected array. received $null at opfor-collection-target-probe.sl:38
sublist-missing-resume
Warning: &sublist: expected array. received 'scalar' at opfor-collection-target-probe.sl:44
sublist-scalar-resume
Warning: &splice: expected array. received $null at opfor-collection-target-probe.sl:50
splice-missing-resume
Warning: &splice: expected array. received 'scalar' at opfor-collection-target-probe.sl:56
splice-scalar-resume
Warning: &setMissPolicy: expected an ordered hash, received: $null at opfor-collection-target-probe.sl:62
miss-missing-resume
Warning: &setMissPolicy: expected an ordered hash, received: 'scalar' at opfor-collection-target-probe.sl:68
miss-scalar-resume
Warning: &setRemovalPolicy: expected an ordered hash, received: $null at opfor-collection-target-probe.sl:74
removal-missing-resume
Warning: &setRemovalPolicy: expected an ordered hash, received: 'scalar' at opfor-collection-target-probe.sl:80
removal-scalar-resume
Warning: expected &closure--received: $null at opfor-collection-target-probe.sl:87
miss-closure-missing-resume
Warning: expected &closure--received: $null at opfor-collection-target-probe.sl:94
removal-closure-missing-resume
`

func TestSleepCollectionTargetWarningCompatibility(t *testing.T) {
	if got := runSleepCollectionTargetWarningProbe(t); !bytes.Equal(got, []byte(sleepCollectionTargetWarningOutput)) {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepCollectionTargetWarningOutput, got)
	}
}

func TestSleepCollectionTargetWarningOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for collection target-warning verification")
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
	path := filepath.Join(directory, "opfor-collection-target-probe.sl")
	if err := os.WriteFile(path, []byte(sleepCollectionTargetWarningProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep collection target-warning probe: %v\n%s", err, want)
	}
	if got := runSleepCollectionTargetWarningProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep collection target-warning output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepCollectionTargetWarningProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "opfor-collection-target-probe.sl", sleepCollectionTargetWarningProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
