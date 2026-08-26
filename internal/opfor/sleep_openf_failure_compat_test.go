package opfor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSleepOpenFFailuresReturnInertHandles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "child")
	probe := sleepOpenFFailureProbe(missing)
	output := runSleepOpenFFailureProbe(t, probe)
	want := fmt.Sprintf(`missing-type=class sleep.engine.types.ObjectValue
missing-line=[]
missing-eof=yes
missing-error=[java.io.FileNotFoundException: %s (No such file or directory)]
empty-type=class sleep.engine.types.ObjectValue
empty-line=[]
empty-eof=yes
empty-error=[java.lang.StringIndexOutOfBoundsException: Index 0 out of bounds for length 0]
short-type=class sleep.engine.types.ObjectValue
short-error=[java.lang.StringIndexOutOfBoundsException: Index 1 out of bounds for length 1]
tail
`, missing)
	if output != want {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", want, output)
	}
}

func TestSleepOpenFFailureOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	missing := filepath.Join(t.TempDir(), "missing", "child")
	probe := sleepOpenFFailureProbe(missing)
	want, err := exec.Command(java, "-jar", jar, "-e", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep openf failure probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepOpenFFailureProbe(t, probe)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep openf failure output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestSleepOpenFDirectoryReturnsInertHandle(t *testing.T) {
	directory := t.TempDir()
	probe := sleepOpenFDirectoryProbe(directory)
	want := fmt.Sprintf(`directory-type=class sleep.engine.types.ObjectValue
directory-line=[]
directory-eof=yes
directory-error=[java.io.FileNotFoundException: %s (Is a directory)]
tail
`, directory)
	if got := runSleepOpenFFailureProbe(t, probe); got != want {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestSleepOpenFDirectoryOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	probe := sleepOpenFDirectoryProbe(directory)
	want, err := exec.Command(java, "-jar", jar, "-e", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep openf directory probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepOpenFFailureProbe(t, probe)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep openf directory output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func sleepOpenFDirectoryProbe(directory string) string {
	return fmt.Sprintf(`
$directory = openf(%q);
println("directory-type=" . typeOf($directory));
println("directory-line=[" . readln($directory) . "]");
println("directory-eof=" . iff(-eof $directory, "yes", "no"));
println("directory-error=[" . checkError() . "]");
closef($directory);
println("tail");
`, directory)
}

func sleepOpenFFailureProbe(missing string) string {
	return fmt.Sprintf(`
$missing = openf(%q);
println("missing-type=" . typeOf($missing));
println("missing-line=[" . readln($missing) . "]");
println("missing-eof=" . iff(-eof $missing, "yes", "no"));
println("missing-error=[" . checkError() . "]");
$empty = openf("");
println("empty-type=" . typeOf($empty));
println("empty-line=[" . readln($empty) . "]");
println("empty-eof=" . iff(-eof $empty, "yes", "no"));
println("empty-error=[" . checkError() . "]");
$short = openf(">");
println("short-type=" . typeOf($short));
println("short-error=[" . checkError() . "]");
closef($missing);
closef($empty);
closef($short);
println("tail");
`, missing)
}

func runSleepOpenFFailureProbe(t *testing.T, probe string) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "openf-failure.sl", probe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
