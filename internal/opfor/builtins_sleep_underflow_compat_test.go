package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepBridgeUnderflowProbe = `println("substr=" . substr());
println("reduce=" . reduce());
sub push_probe {
    push();
    println("push-tail");
}
push_probe();
println("push-resume");
sub map_probe {
    map();
    println("map-tail");
}
map_probe();
println("map-resume");
sub filter_probe {
    filter();
    println("filter-tail");
}
filter_probe();
println("filter-resume");
sub sort_probe {
    sort();
    println("sort-tail");
}
sort_probe();
println("sort-resume");
sub shift_probe {
    @items = @();
    shift(@items);
    println("shift-tail");
}
shift_probe();
println("shift-resume");
sub remove_probe {
    @items = @(1);
    removeAt(@items, 4);
    println("remove-tail");
}
remove_probe();
println("remove-resume");
sub acquire_probe {
    acquire();
    println("acquire-tail");
}
acquire_probe();
println("acquire-resume");
sub release_probe {
    release($null);
    println("release-tail");
}
release_probe();
println("release-resume");
`

const sleepBridgeUnderflowOutput = `substr=
reduce=
Warning: &push: expected array. received $null at bridge-underflow.sl:4
push-resume
Warning: expected &closure--received: $null at bridge-underflow.sl:10
map-resume
Warning: expected &closure--received: $null at bridge-underflow.sl:16
filter-resume
Warning: &sort requires a function to specify how to sort the data at bridge-underflow.sl:22
sort-resume
Warning: attempted an invalid index: Index: 0, Size: 0 at bridge-underflow.sl:29
shift-resume
Warning: attempted an invalid index: Index: 4, Size: 1 at bridge-underflow.sl:36
remove-resume
Warning: null value error at bridge-underflow.sl:42
acquire-resume
Warning: null value error at bridge-underflow.sl:48
release-resume
`

func TestSleepBridgeUnderflowAndDefaultSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge-underflow.sl")
	got := executeSleepBridgeUnderflowProbe(t, path)
	if !bytes.Equal(got, []byte(sleepBridgeUnderflowOutput)) {
		t.Fatalf("bridge underflow/default output mismatch\nwant:\n%s\ngot:\n%s", sleepBridgeUnderflowOutput, got)
	}
}

func TestSleepBridgeUnderflowOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	path := filepath.Join(t.TempDir(), "bridge-underflow.sl")
	if err := os.WriteFile(path, []byte(sleepBridgeUnderflowProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep bridge-underflow probe: %v\n%s", err, want)
	}
	got := executeSleepBridgeUnderflowProbe(t, path)
	if !bytes.Equal(got, want) {
		t.Fatalf("bridge underflow/default mismatch\nofficial:\n%s\nopfor:\n%s", want, got)
	}
}

func executeSleepBridgeUnderflowProbe(t *testing.T, path string) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString(path, sleepBridgeUnderflowProbe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}
