package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
)

// TestSleepFileSystemBridgeSourceContract covers the six functions registered
// by the pinned Sleep 2.1 FileSystemBridge.java that were not exercised by its
// canonical tests. The source contract is Cobalt-Strike/sleep@60ac3ff9,
// src/sleep/bridges/FileSystemBridge.java lines 61-78 and 83-220.
func TestSleepFileSystemBridgeSourceContract(t *testing.T) {
	root := t.TempDir()
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	proper, err := runtime.Invoke(context.Background(), "getFileProper",
		String("alpha"), String("beta"), String("leaf.txt"))
	if err != nil {
		t.Fatalf("getFileProper: %v", err)
	}
	if got, want := proper.String(), filepath.Join(root, "alpha", "beta", "leaf.txt"); got != want {
		t.Fatalf("getFileProper = %q, want %q", got, want)
	}
	if goruntime.GOOS != "windows" {
		proper, err = runtime.Invoke(context.Background(), "getFileProper",
			String("alpha/../beta"), String("./leaf.txt"))
		if err != nil {
			t.Fatalf("getFileProper with dot segments: %v", err)
		}
		if got, want := proper.String(), root+string(filepath.Separator)+filepath.FromSlash("alpha/../beta/./leaf.txt"); got != want {
			t.Fatalf("getFileProper with dot segments = %q, want %q", got, want)
		}
	}

	created, err := runtime.Invoke(context.Background(), "createNewFile", String("fresh"))
	if err != nil || !created.Truth() {
		t.Fatalf("first createNewFile = %s, %v; want 1, nil", created.Describe(), err)
	}
	created, err = runtime.Invoke(context.Background(), "createNewFile", String("fresh"))
	if err != nil || !created.IsNull() {
		t.Fatalf("second createNewFile = %s, %v; want $null, nil", created.Describe(), err)
	}

	missing, err := runtime.Invoke(context.Background(), "lastModified", String("missing"))
	if err != nil || missing.Kind() != KindLong || missing.Int64() != 0 {
		t.Fatalf("lastModified(missing) = %s, %v; want long 0, nil", missing.Describe(), err)
	}
	const modified = int64(1_234_567_890_000)
	changed, err := runtime.Invoke(context.Background(), "setLastModified", String("fresh"), Long(modified))
	if err != nil || !changed.Truth() {
		t.Fatalf("setLastModified = %s, %v; want 1, nil", changed.Describe(), err)
	}
	stamp, err := runtime.Invoke(context.Background(), "lastModified", String("fresh"))
	if err != nil || stamp.Kind() != KindLong || stamp.Int64() != modified {
		t.Fatalf("lastModified(fresh) = %s, %v; want long %d, nil", stamp.Describe(), err, modified)
	}
	changed, err = runtime.Invoke(context.Background(), "setLastModified", String("missing"), Long(modified))
	if err != nil || !changed.IsNull() {
		t.Fatalf("setLastModified(missing) = %s, %v; want $null, nil", changed.Describe(), err)
	}
	if _, err := runtime.Invoke(context.Background(), "setLastModified", String("fresh"), Long(-1)); err == nil || !strings.Contains(err.Error(), "Negative time") {
		t.Fatalf("negative setLastModified error = %v, want Java-compatible IllegalArgumentException", err)
	}

	readOnly, err := runtime.Invoke(context.Background(), "setReadOnly", String("fresh"))
	if err != nil || !readOnly.Truth() {
		t.Fatalf("setReadOnly = %s, %v; want 1, nil", readOnly.Describe(), err)
	}
	if info, statErr := os.Stat(filepath.Join(root, "fresh")); statErr != nil {
		t.Fatal(statErr)
	} else if goruntime.GOOS != "windows" && info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("setReadOnly mode = %04o, want no write bits", info.Mode().Perm())
	}
	// Windows cannot remove a read-only file during TempDir cleanup.
	if err := os.Chmod(filepath.Join(root, "fresh"), 0o600); err != nil {
		t.Fatal(err)
	}

	roots, err := runtime.Invoke(context.Background(), "listRoots")
	if err != nil {
		t.Fatalf("listRoots: %v", err)
	}
	rootArray, ok := roots.Array()
	if !ok || rootArray.Len() == 0 {
		t.Fatalf("listRoots = %s, want at least one filesystem root", roots.Describe())
	}
	for _, value := range rootArray.Values() {
		if !filepath.IsAbs(value.String()) {
			t.Errorf("listRoots entry %q is not absolute", value.String())
		}
	}
}

// TestSleepFileSystemBridgeBoundaryContract covers FileSystemBridge's direct
// java.io.File result contracts. In particular, metadata lookup failures are
// zero/empty values, listFiles(null) becomes an empty read-only Sleep array,
// and ordinary mutator false results do not populate
// ScriptEnvironment.flagError. The source contract is
// Cobalt-Strike/sleep@60ac3ff9, FileSystemBridge.java lines 83-220 and
// BridgeUtilities.java lines 251-280.
func TestSleepFileSystemBridgeBoundaryContract(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(existing, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "existing-dir"), 0o700); err != nil {
		t.Fatal(err)
	}

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	for _, test := range []struct {
		name string
		args []Value
	}{
		{name: "lof", args: []Value{String("missing")}},
		{name: "lof", args: []Value{String("invalid\x00path")}},
		{name: "lastModified", args: []Value{String("missing")}},
		{name: "lastModified", args: []Value{String("invalid\x00path")}},
	} {
		value, err := runtime.Invoke(context.Background(), test.name, test.args...)
		if err != nil || value.Kind() != KindLong || value.Int64() != 0 {
			t.Errorf("%s(%q) = %s, %v; want long 0, nil", test.name, test.args[0].String(), value.Describe(), err)
		}
	}
	existingLength, err := runtime.Invoke(context.Background(), "lof", String("existing.txt"))
	if err != nil || existingLength.Kind() != KindLong || existingLength.Int64() != 4 {
		t.Errorf("lof(existing) = %s, %v; want long 4, nil", existingLength.Describe(), err)
	}

	name, err := runtime.Invoke(context.Background(), "getFileName", String(""))
	if err != nil || name.Kind() != KindString || name.String() != "" {
		t.Errorf("getFileName(empty) = %s, %v; want empty string, nil", name.Describe(), err)
	}
	parent, err := runtime.Invoke(context.Background(), "getFileParent", String(""))
	if err != nil || !parent.IsNull() {
		t.Errorf("getFileParent(empty) = %s, %v; want $null, nil", parent.Describe(), err)
	}

	for _, path := range []string{"missing", "existing.txt", "invalid\x00path"} {
		listed, err := runtime.Invoke(context.Background(), "ls", String(path))
		array, ok := listed.Array()
		if err != nil || !ok || array.Len() != 0 || !array.isReadOnly() {
			t.Errorf("ls(%q) = %s, %v; want empty read-only array, nil", path, listed.Describe(), err)
		}
	}
	listed, err := runtime.Invoke(context.Background(), "ls")
	array, ok := listed.Array()
	if err != nil || !ok || array.Len() != 2 || !array.isReadOnly() {
		t.Errorf("ls() = %s, %v; want two-element read-only cwd array, nil", listed.Describe(), err)
	}

	hidden, err := runtime.Invoke(context.Background(), "-isHidden", String(".missing-hidden"))
	if err != nil {
		t.Fatalf("-isHidden(missing dot path): %v", err)
	}
	if want := goruntime.GOOS != "windows"; hidden.Truth() != want {
		t.Errorf("-isHidden(missing dot path) = %s, want %t", hidden.Describe(), want)
	}

	for _, test := range []struct {
		name string
		args []Value
	}{
		{name: "deleteFile", args: []Value{String("missing")}},
		{name: "deleteFile", args: []Value{String("")}},
		{name: "mkdir", args: []Value{String("existing.txt")}},
		{name: "mkdir", args: []Value{String("")}},
		{name: "rename", args: []Value{String("missing"), String("destination")}},
		{name: "rename", args: []Value{String(""), String("")}},
	} {
		value, err := runtime.Invoke(context.Background(), test.name, test.args...)
		if err != nil || !value.IsNull() {
			t.Errorf("%s failure = %s, %v; want $null, nil", test.name, value.Describe(), err)
		}
	}

	softErrors, err := runtime.Eval(context.Background(), "filesystem-failure-contract.sl", `
$deleted = deleteFile('missing-delete');
$delete_error = checkError();
$made = mkdir('existing.txt');
$mkdir_error = checkError();
$renamed = rename('missing-rename', 'destination');
$rename_error = checkError();
$created = createNewFile('');
$create_error = checkError();
return @(
    $deleted is $null, $delete_error is $null,
    $made is $null, $mkdir_error is $null,
    $renamed is $null, $rename_error is $null,
    $created is $null, $create_error is $null
);
`)
	if err != nil {
		t.Fatalf("failure contract Eval: %v", err)
	}
	softArray, ok := softErrors.Array()
	if !ok || softArray.Len() != 8 {
		t.Fatalf("failure contract = %s, want eight booleans", softErrors.Describe())
	}
	for index, value := range softArray.Values() {
		want := index != 7
		if value.Truth() != want {
			t.Errorf("failure contract[%d] = %s, want %t", index, value.Describe(), want)
		}
	}

	made, err := runtime.Invoke(context.Background(), "mkdir", String("made/child"))
	if err != nil || !made.Truth() {
		t.Fatalf("mkdir success = %s, %v; want 1, nil", made.Describe(), err)
	}
	renamed, err := runtime.Invoke(context.Background(), "rename", String("existing.txt"), String("renamed.txt"))
	if err != nil || !renamed.Truth() {
		t.Fatalf("rename success = %s, %v; want 1, nil", renamed.Describe(), err)
	}
	deleted, err := runtime.Invoke(context.Background(), "deleteFile", String("renamed.txt"))
	if err != nil || !deleted.Truth() {
		t.Fatalf("deleteFile success = %s, %v; want 1, nil", deleted.Describe(), err)
	}

	if _, err := runtime.Invoke(context.Background(), "chdir", String("existing-dir")); err != nil {
		t.Fatalf("chdir(existing directory): %v", err)
	}
	if got, want := mustInvokeFilesystemString(t, runtime, "cwd"), filepath.Join(root, "existing-dir"); got != want {
		t.Errorf("cwd after directory chdir = %q, want %q", got, want)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
		t.Fatalf("restore cwd: %v", err)
	}
	fileCWD := filepath.Join(root, "cwd-file")
	if err := os.WriteFile(fileCWD, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String("cwd-file")); err != nil {
		t.Fatalf("chdir(file): %v", err)
	}
	if got := mustInvokeFilesystemString(t, runtime, "cwd"); got != fileCWD {
		t.Errorf("cwd after file chdir = %q, want %q", got, fileCWD)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
		t.Fatalf("restore cwd: %v", err)
	}
	missingCWD := filepath.Join(root, "missing-cwd")
	if _, err := runtime.Invoke(context.Background(), "chdir", String("missing-cwd")); err != nil {
		t.Fatalf("chdir(missing): %v", err)
	}
	if got := mustInvokeFilesystemString(t, runtime, "cwd"); got != missingCWD {
		t.Errorf("cwd after missing chdir = %q, want %q", got, missingCWD)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String("")); err != nil {
		t.Fatalf("chdir(empty): %v", err)
	}
	processCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := mustInvokeFilesystemString(t, runtime, "cwd"); got != processCWD {
		t.Errorf("cwd after empty chdir = %q, want process cwd %q", got, processCWD)
	}
}

func mustInvokeFilesystemString(t *testing.T, runtime *Runtime, name string, arguments ...Value) string {
	t.Helper()
	value, err := runtime.Invoke(context.Background(), name, arguments...)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return value.String()
}

// TestSleepFileSystemBridgeOfficialJARDifferential compares the pure-Go
// implementation with the hash-pinned official Sleep 2.1 JAR when the same
// opt-in oracle used by the serialization tests is available. The probe omits
// File("") attribute and mutation calls: the original canonical Sleep fixture
// pins those to the historical false/zero behavior, while modern OpenJDKs
// resolve several of them against the process cwd. Both subprocesses still run
// from isolated temporary roots as a defense against future probe expansion.
func TestSleepFileSystemBridgeOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for differential verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	const officialSHA256 = "0ddde5e9e8d8d8d334d071b1f887c379f5d0be9b190566f05365997b3e375ff1"
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	goRoot := t.TempDir()
	javaRoot := t.TempDir()
	for _, root := range []string{goRoot, javaRoot} {
		if err := os.WriteFile(filepath.Join(root, "boundary.bin"), []byte("opfor"), 0o600); err != nil {
			t.Fatal(err)
		}
		root := root
		t.Cleanup(func() {
			// The probe intentionally exercises setReadOnly(""). OpenJDK resolves
			// File("") against the process working directory for filesystem
			// operations, so restore the isolated probe root before TempDir cleanup.
			_ = os.Chmod(root, 0o700)
			_ = os.Chmod(filepath.Join(root, "fresh"), 0o600)
		})
	}
	// Empty Java File pathnames resolve against the process working directory
	// for metadata and mutation. Run each implementation from its own temporary
	// root so setLastModified("") and setReadOnly("") can never touch the source
	// checkout (or one another).
	t.Chdir(goRoot)
	goOutput := runPureGoFilesystemProbe(t, goRoot)
	javaSource := sleepFilesystemProbeSource(javaRoot)
	command := osexec.Command(java, "-jar", jar, "-e", javaSource)
	command.Dir = javaRoot
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep filesystem probe: %v\n%s", err, javaOutput)
	}

	got := normalizeFilesystemProbe(goOutput, goRoot)
	want := normalizeFilesystemProbe(javaOutput, javaRoot)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep filesystem output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func runPureGoFilesystemProbe(t *testing.T, root string) []byte {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Eval(context.Background(), "filesystem-bridge-probe.sl", sleepFilesystemProbeSource(root)); err != nil {
		t.Fatalf("pure-Go filesystem probe: %v\n%s", err, output.String())
	}
	return output.Bytes()
}

func sleepFilesystemProbeSource(root string) string {
	quotedRoot := strconv.Quote(filepath.ToSlash(root))
	quotedAbsoluteBase := strconv.Quote(filepath.ToSlash(filepath.Join(root, "absolute-base")))
	lines := []string{
		"chdir(" + quotedRoot + ");",
		"println(getFileProper('alpha', 'beta', 'leaf.txt'));",
	}
	if goruntime.GOOS != "windows" {
		lines = append(lines,
			"println(getFileProper('alpha/../beta', './leaf.txt'));",
			"println(getFileProper("+quotedAbsoluteBase+", '/leaf.txt'));",
		)
	}
	lines = append(lines,
		"println(getFileProper(''));",
		"println(getFileProper('', 'leaf.txt'));",
		"println(getFileProper());",
		"println(lof('missing'));",
		"println(lof('boundary.bin'));",
		"$invalid_path = 'invalid' . chr(0) . 'path';",
		"println(lof($invalid_path));",
		"println(lastModified($invalid_path));",
		"@invalid_files = ls($invalid_path);",
		"if (-isarray @invalid_files) { println(1); } else { println(0); }",
		"println(size(@invalid_files));",
		"@missing_files = ls('missing');",
		"if (-isarray @missing_files) { println(1); } else { println(0); }",
		"println(size(@missing_files));",
		"println(getFileName(''));",
		"if (getFileParent('') is $null) { println(1); } else { println(0); }",
		"if (-isHidden '.missing-hidden') { println(1); } else { println(0); }",
		"println(lastModified('missing'));",
		"println(createNewFile('fresh'));",
		"println(createNewFile('fresh'));",
		"@file_files = ls('fresh');",
		"if (-isarray @file_files) { println(1); } else { println(0); }",
		"println(size(@file_files));",
		"if (lastModified('fresh') > 0) { println(1); } else { println(0); }",
		"println(setLastModified('fresh', 1234567890000L));",
		"println(lastModified('fresh'));",
		"println(setLastModified('missing', 1234567890000L));",
		"println(setReadOnly('fresh'));",
		"println(setReadOnly('missing'));",
		"$deleted = deleteFile('missing-delete');",
		"if ($deleted is $null) { println(1); } else { println(0); }",
		"if (checkError() is $null) { println(1); } else { println(0); }",
		"$made = mkdir('fresh');",
		"if ($made is $null) { println(1); } else { println(0); }",
		"if (checkError() is $null) { println(1); } else { println(0); }",
		"$renamed = rename('missing-rename', 'destination');",
		"if ($renamed is $null) { println(1); } else { println(0); }",
		"if (checkError() is $null) { println(1); } else { println(0); }",
		"$empty_created = createNewFile('');",
		"if ($empty_created is $null) { println(1); } else { println(0); }",
		"if (checkError() is $null) { println(0); } else { println(1); }",
		"println(mkdir('made/child'));",
		"println(rename('fresh', 'moved'));",
		"println(deleteFile('moved'));",
		"@roots = listRoots();",
		"if (size(@roots) > 0) { println(1); } else { println(0); }",
		"chdir('missing-cwd');",
		"println(cwd());",
		"chdir('');",
		"println(cwd());",
		"chdir("+quotedRoot+");",
	)
	return strings.Join(lines, "\n") + "\n"
}

func normalizeFilesystemProbe(output []byte, root string) []byte {
	normalized := filepath.ToSlash(string(output))
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		// macOS commonly exposes /tmp to child processes as /private/tmp.
		// Replace the resolved (longer) spelling before the lexical path so a
		// prefix such as /private does not remain in front of <ROOT>.
		normalized = strings.ReplaceAll(normalized, filepath.ToSlash(resolved), "<ROOT>")
	}
	normalized = strings.ReplaceAll(normalized, filepath.ToSlash(root), "<ROOT>")
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	return []byte(normalized)
}
