package opfor

import (
	"bytes"
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
)

const sleepClosureScopeProbeName = "sleep-closure-scope.sl"

// CreateClosure.java constructs every SleepClosure with a fresh internal
// variable container. SleepClosure.evaluate pushes that container as the one
// active closure level, while ScriptVariables.getScalarLevel searches only the
// invocation local, that closure level, and globals. Consequently a closure
// created beneath another local() or this() scope never captures that scope.
const sleepClosureScopeProbe = `$global = "global";
sub local_factory {
    local('$global');
    $global = "local";
    return { println("literal=$global"); };
}
$literal = local_factory();
[$literal];

sub dynamic_sub_factory {
    local('$global');
    $global = "local-sub";
    sub dynamic_sub { println("sub=$global"); }
}
dynamic_sub_factory();
dynamic_sub();

sub outer_this_factory {
    this('$global');
    $global = "outer-this";
    return { println("nested=$global"); };
}
$nested = outer_this_factory();
[$nested];

$stateful = {
    this('$state');
    $state++;
    println("state=$state");
};
[$stateful];
[$stateful];
`

const sleepClosureScopeProbeOutput = `literal=global
sub=global
nested=global
state=1
state=2
`

func TestSleepClosureScopeCompatibility(t *testing.T) {
	if got := runSleepClosureScopeProbe(t); !bytes.Equal(got, []byte(sleepClosureScopeProbeOutput)) {
		t.Fatalf("closure scope output mismatch\nwant:\n%sgot:\n%s", sleepClosureScopeProbeOutput, got)
	}
}

func TestSleepClosureScopeOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepClosureScopeProbeName)
	if err := os.WriteFile(path, []byte(sleepClosureScopeProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep closure-scope probe: %v\n%s", err, want)
	}
	if got := runSleepClosureScopeProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep closure-scope output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepClosureScopeProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepClosureScopeProbeName, sleepClosureScopeProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
