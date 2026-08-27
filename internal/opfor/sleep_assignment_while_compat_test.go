package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepAssignmentWhileCollectionProbeName = "sleep-assignment-while-collection-probe.sl"

const sleepAssignmentWhileCollectionProbe = `$array_step = 0;
sub next_array {
    if ($array_step == 0) {
        $array_step = 1;
        return @("alpha", "beta");
    }
    return $null;
}
$hash_step = 0;
sub next_hash {
    if ($hash_step == 0) {
        $hash_step = 1;
        return %(key => "value");
    }
    return $null;
}
while @array_value (next_array()) {
    println("array-size=" . size(@array_value));
}
while %hash_value (next_hash()) {
    println("hash-size=" . size(%hash_value));
}
println("done");
`

const sleepAssignmentWhileCollectionProbeOutput = `array-size=2
hash-size=1
done
`

func TestSleepAssignmentWhileCollectionBindingCompatibility(t *testing.T) {
	if got := runSleepAssignmentWhileCollectionProbe(t); got != sleepAssignmentWhileCollectionProbeOutput {
		t.Fatalf("assignment-while output mismatch\nwant:\n%sgot:\n%s", sleepAssignmentWhileCollectionProbeOutput, got)
	}
}

func TestSleepAssignmentWhileCollectionBindingOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepAssignmentWhileCollectionProbeName)
	if err := os.WriteFile(path, []byte(sleepAssignmentWhileCollectionProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep assignment-while probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepAssignmentWhileCollectionProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep assignment-while output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepAssignmentWhileCollectionProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepAssignmentWhileCollectionProbeName, sleepAssignmentWhileCollectionProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
