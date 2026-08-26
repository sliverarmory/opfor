package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSleepBacktickInputCanonicalCompatibility(t *testing.T) {
	requireHermeticPOSIXCommands(t, map[string]string{
		"cat": `for file do
    while IFS= read -r line || [ -n "$line" ]; do
        printf '%s\n' "$line"
    done < "$file"
done`,
	})

	source, golden := readProcessFilesystemFixture(t, "backtickin")
	program, err := Compile(NewSource("backtickin.sl", source))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	workingDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(workingDirectory, "backtickin.sl"), source, 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(workingDirectory)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}
	if got := output.Bytes(); !bytes.Equal(got, golden) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", golden, got)
	}
}

func TestSleepChdirCanonicalCompatibility(t *testing.T) {
	requireHermeticPOSIXCommands(t, map[string]string{
		"ls": `for path in ./*; do
    [ -e "$path" ] || continue
    printf '%s\n' "${path#./}"
done`,
	})

	source, golden := readProcessFilesystemFixture(t, "chdir")
	program, err := Compile(NewSource("chdir.sl", source))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	workingDirectory := canonicalChdirFixture(t)

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(workingDirectory)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}

	got := filepath.ToSlash(output.String())
	got = strings.ReplaceAll(got, filepath.ToSlash(workingDirectory), "/root/sleep/tests")
	if got != string(golden) {
		t.Fatalf("normalized output mismatch\nwant:\n%s\ngot:\n%s", golden, got)
	}
}

func TestSleepBTestCanonicalCompatibilityWithInertJARFixture(t *testing.T) {
	source, golden := readProcessFilesystemFixture(t, "btest")
	program, err := Compile(NewSource("btest.sl", source))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	jarData, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures", "data", "scripts.jar"))
	if err != nil {
		t.Fatal(err)
	}

	fixtureRoot := t.TempDir()
	programRoot := filepath.Join(fixtureRoot, "tests")
	if err := os.Mkdir(programRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	// btest.sl treats ../sleep.jar purely as an opaque byte source. Reusing the
	// pinned, BSD-licensed scripts.jar fixture exercises the same lof/readb/
	// byteAt path without vendoring or executing a JVM runtime artifact.
	if err := os.WriteFile(filepath.Join(fixtureRoot, "sleep.jar"), jarData, 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(programRoot)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}
	if got := output.Bytes(); !bytes.Equal(got, golden) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", golden, got)
	}
}

func TestBacktickKeepsConsoleInputPrivateAndReturnsOnlyStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the canonical backtick command uses POSIX shell builtins")
	}
	var output bytes.Buffer
	input := strings.NewReader("console secret\n")
	runtime, err := New(WithStdin(input), WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := CompileString("backtick-streams.sl", ""+
		"@lines = `if IFS= read -r line; then printf leaked; else printf closed; fi; printf noise >&2`;\n"+
		"println(join('', @lines));\n"+
		"println(readln());\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := output.String(), "closed\nconsole secret\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestBacktickAbnormalTerminationIsASoftSourceError(t *testing.T) {
	command := "printf partial; exit 7"
	if runtime.GOOS == "windows" {
		command = "echo partial&&exit /b 7"
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := CompileString("backtick-status.sl", "@lines = `"+command+"`;\n"+
		"println(join('', @lines));\n"+
		"checkError($problem);\n"+
		"return $problem;\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := strings.TrimSpace(output.String()), "partial"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := result.String(), "abnormal termination: 7"; got != want {
		t.Fatalf("checkError = %q, want %q", got, want)
	}
}

func TestBacktickDebugWarningUsesCallSite(t *testing.T) {
	command := "exit 7"
	if runtime.GOOS == "windows" {
		command = "exit /b 7"
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program, err := CompileString("backtick-warning.sl", "debug(2);\n`"+command+"`;\nprintln('continued');\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "Warning: checkError(): abnormal termination: 7 at backtick-warning.sl:2\ncontinued\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestBacktickImporterOverrideRetainsFirstRefusal(t *testing.T) {
	called := 0
	runtime, err := New(WithFunction("__EXEC__", func(_ context.Context, invocation Invocation) (Value, error) {
		called++
		if got, want := invocation.Arg(0).String(), "must-not-run"; got != want {
			t.Fatalf("command = %q, want %q", got, want)
		}
		return ArrayValue(NewArray(String("overridden"))), nil
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "override.sl", "return `must-not-run`;\n")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok || len(array.Values()) != 1 || array.Values()[0].String() != "overridden" || called != 1 {
		t.Fatalf("result = %s, calls = %d", result.Describe(), called)
	}
}

func TestFilePredicatesUseRuntimeWorkingDirectory(t *testing.T) {
	workingDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(workingDirectory, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workingDirectory, "present.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workingDirectory, ".hidden"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(workingDirectory)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "file-predicates.sl", `
$exists = 0; if (-exists "present.txt") { $exists = 1; }
$file = 0; if (-isFile "present.txt") { $file = 1; }
$directory = 0; if (-isDir "nested") { $directory = 1; }
$hidden = 0; if (-isHidden ".hidden") { $hidden = 1; }
$missing = 0; if (!-exists "missing.txt") { $missing = 1; }
$empty = 0; if (!-exists "") { $empty = 1; }
return @($exists, $file, $directory, $hidden, $missing, $empty);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok || len(array.Values()) != 6 {
		t.Fatalf("result = %s, want six predicates", result.Describe())
	}
	for index, value := range array.Values() {
		if !value.Truth() {
			t.Errorf("predicate %d = %s, want true", index, value.Describe())
		}
	}
}

func readProcessFilesystemFixture(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	root := filepath.Join("testdata", "upstream", "sleep-2.1")
	source, err := os.ReadFile(filepath.Join(root, "programs", name+".sl"))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join(root, "golden", name+".sl"))
	if err != nil {
		t.Fatal(err)
	}
	return source, golden
}

func requireHermeticPOSIXCommands(t *testing.T, commands map[string]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the canonical fixture names POSIX commands")
	}
	directory := t.TempDir()
	for name, body := range commands {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func canonicalChdirFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{"data/src", "data2/src"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{
		"data/build.xml", "data/readme.txt", "data/scripts.jar", "data/test.jar",
		"data2/build.xml", "data2/test.jar",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
