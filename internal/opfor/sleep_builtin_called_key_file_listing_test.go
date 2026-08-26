package opfor

import (
	"bytes"
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sleepFileListingCalledKeyProbe = `setf("&ls", function("&listRoots"));
println("cross-ls=" . join("|", sorta(ls("."))));
setf("&listRoots", function("&ls"));
println("cross-roots=" . join("|", sorta(listRoots())));
setf("&zlist", function("&listRoots"));
println("unknown=" . join("|", sorta(zlist("."))));
`

func TestStockSleepFileListingCalledKeyDoesNotReachHost(t *testing.T) {
	directory := sleepFileListingCalledKeyFixture(t)
	output, hostCalls := runSleepFileListingCalledKeyProbe(t, directory)
	if hostCalls != 0 {
		t.Fatalf("Host calls = %d, want 0", hostCalls)
	}

	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("output lines = %d, want 3\n%s", len(lines), output)
	}
	crossListing := strings.TrimPrefix(lines[0], "cross-ls=")
	unknownListing := strings.TrimPrefix(lines[2], "unknown=")
	if crossListing == "" || crossListing != unknownListing {
		t.Fatalf("cross/unknown listing mismatch\ncross: %q\nunknown: %q", crossListing, unknownListing)
	}
	if roots := strings.TrimPrefix(lines[1], "cross-roots="); roots == "" {
		t.Fatalf("cross-key listRoots returned no roots: %q", lines[1])
	}
}

func TestStockSleepFileListingCalledKeyOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := sleepFileListingCalledKeyFixture(t)
	path := filepath.Join(directory, "file-listing-called-key.sl")
	if err := os.WriteFile(path, []byte(sleepFileListingCalledKeyProbe), 0o600); err != nil {
		t.Fatal(err)
	}

	command := osexec.Command(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep file-listing called-key probe: %v\n%s", err, want)
	}

	got, hostCalls := runSleepFileListingCalledKeyProbe(t, directory)
	if hostCalls != 0 {
		t.Fatalf("Host calls = %d, want 0", hostCalls)
	}
	if !bytes.Equal([]byte(got), want) {
		t.Fatalf("official Sleep file-listing called-key mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func sleepFileListingCalledKeyFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "alpha.txt"), []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatalf("canonicalize fixture directory: %v", err)
	}
	return canonical
}

func runSleepFileListingCalledKeyProbe(t *testing.T, directory string) (string, int) {
	t.Helper()
	var output bytes.Buffer
	hostCalls := 0
	runtimeInstance, err := New(
		WithStdout(&output),
		WithStderr(&output),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls++
			return String("unexpected-host"), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Invoke(context.Background(), "chdir", String(directory)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "file-listing-called-key.sl", sleepFileListingCalledKeyProbe); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	return output.String(), hostCalls
}
