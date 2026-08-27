package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const sleepArrayUseCalledKeyOutput = `array-unknown-array=1,2
array-unknown-at=3,4
array-cross-at-to-array=5,6
array-cross-array-to-at=7,8
included
included
included
`

func sleepArrayUseCalledKeyProbe(includePath string) string {
	quotedPath := strconv.Quote(includePath)
	return `setf("&zarray_from_array", function("&array"));
println("array-unknown-array=" . join(",", zarray_from_array(1, 2)));
setf("&zarray_from_at", function("&@"));
println("array-unknown-at=" . join(",", zarray_from_at(3, 4)));
setf("&array", function("&@"));
println("array-cross-at-to-array=" . join(",", array(5, 6)));
setf("&@", function("&array"));
println("array-cross-array-to-at=" . join(",", @(7, 8)));
setf("&include", function("&use"));
include(` + quotedPath + `);
setf("&zuse", function("&use"));
zuse(` + quotedPath + `);
setf("&zinclude", function("&include"));
zinclude(` + quotedPath + `);
`
}

func TestStockSleepArrayAndUseHandlesDispatchCalledKey(t *testing.T) {
	directory := t.TempDir()
	includePath := filepath.Join(directory, "included source.sl")
	if err := os.WriteFile(includePath, []byte(`println("included");`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := runSleepArrayUseCalledKeyProbe(t, sleepArrayUseCalledKeyProbe(includePath), nil); got != sleepArrayUseCalledKeyOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepArrayUseCalledKeyOutput, got)
	}
}

func TestStockSleepArrayAndUseCalledKeyOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	includePath := filepath.Join(directory, "included source.sl")
	if err := os.WriteFile(includePath, []byte(`println("included");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program := sleepArrayUseCalledKeyProbe(includePath)
	programPath := filepath.Join(directory, "array-use-called-key.sl")
	if err := os.WriteFile(programPath, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, programPath).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep array/use called-key probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepArrayUseCalledKeyProbe(t, program, nil)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep array/use called-key output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestStockSleepArrayAndUseUnknownAliasesDoNotReachHost(t *testing.T) {
	directory := t.TempDir()
	includePath := filepath.Join(directory, "included source.sl")
	if err := os.WriteFile(includePath, []byte(`println("included");`), 0o600); err != nil {
		t.Fatal(err)
	}
	hostCalls := 0
	host := HostFunc(func(context.Context, Invocation) (Value, error) {
		hostCalls++
		return String("host"), nil
	})
	program := `setf("&zarray", function("&array"));
println(join(",", zarray(1, 2)));
setf("&zinclude", function("&include"));
zinclude(` + strconv.Quote(includePath) + `);
`
	if got, want := runSleepArrayUseCalledKeyProbe(t, program, host), "1,2\nincluded\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if hostCalls != 0 {
		t.Fatalf("host calls = %d, want 0", hostCalls)
	}
}

func runSleepArrayUseCalledKeyProbe(t *testing.T, program string, host Host) string {
	t.Helper()
	var output bytes.Buffer
	options := []Option{WithStdout(&output), WithStderr(&output)}
	if host != nil {
		options = append(options, WithHost(host))
	}
	runtimeInstance, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "array-use-called-key.sl", program); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
