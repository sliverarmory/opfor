package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const sleepClasspathProbeSource = `
include(" spaced source.sl ");
println($spaced);
include("single-from-classpath.sl");
println($single);
include("hidden.jar", "pkg/from-classpath.sl");
println($classpath);
println($__INCLUDE__);
`

const sleepIncludeArityProbeSource = `
import java.io.File;
include("container", "member.sl");
include("standalone.sl", "ignored-1", "ignored-2");
include("standalone.sl", "ignored-1", "ignored-2", "ignored-3");
println($trail);
println([[new File: $__INCLUDE__] getName]);
println(checkError());
`

func TestFileSourceResolverSleepClasspathSeparatorsPreserveVolumes(t *testing.T) {
	tests := []struct {
		name      string
		classPath string
		want      []string
	}{
		{name: "empty", classPath: "", want: []string{"."}},
		{name: "colon separated", classPath: "/one:/two", want: []string{"/one", "/two"}},
		{name: "Windows volume separators", classPath: `C:\sleep-libs;D:\opfor-libs`, want: []string{`C:\sleep-libs`, `D:\opfor-libs`}},
		{name: "mixed separators", classPath: "/one:/two;/three", want: []string{"/one", "/two", "/three"}},
		{name: "single Windows volume", classPath: `C:\sleep-libs`, want: []string{`C:\sleep-libs`}},
		{name: "empty entries", classPath: "/one::/three", want: []string{"/one", ".", "/three"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver, err := NewFileSourceResolver(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			resolver.SetSleepClasspath(test.classPath)
			want := make([]string, len(test.want))
			for index, entry := range test.want {
				want[index] = filepath.FromSlash(entry)
			}
			if got := resolver.SleepClasspath(); !reflect.DeepEqual(got, want) {
				t.Fatalf("SleepClasspath() = %#v, want %#v", got, want)
			}
		})
	}
}

// TestFileSourceResolverSleepClasspathOfficialJARDifferential validates both
// ParserConfig.findJarFile-style lookup and Java File's whitespace-preserving
// names. It is opt-in because the official BSD Sleep JAR is supplied outside
// the repository.
func TestFileSourceResolverSleepClasspathOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, " spaced source.sl "), []byte(`$spaced = 'kept';`), 0o600); err != nil {
		t.Fatal(err)
	}
	classPath := filepath.Join(root, "sleep-libs")
	if err := os.Mkdir(classPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(classPath, "single-from-classpath.sl"), []byte(`$single = 'standalone';`), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(classPath, "hidden.jar")
	writeTestSourceArchive(t, archive, "pkg/from-classpath.sl", `$classpath = 'found';`)
	programPath := filepath.Join(root, "classpath-probe.sl")
	if err := os.WriteFile(programPath, []byte(sleepClasspathProbeSource), 0o600); err != nil {
		t.Fatal(err)
	}

	command := officialSleepJavaCommand(java, "-Dsleep.classpath="+classPath, "-jar", jar, programPath)
	command.Dir = root
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep classpath probe: %v\n%s", err, want)
	}

	var got bytes.Buffer
	runtime, err := New(WithSleepClasspath(classPath), WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Eval(context.Background(), filepath.Base(programPath), sleepClasspathProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("official Sleep classpath output mismatch\nwant:\n%sgot:\n%s", want, got.Bytes())
	}
}

func TestScriptLoaderFilenameDoesNotSearchSleepClasspathOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	root := t.TempDir()
	classPath := filepath.Join(root, "programs")
	if err := os.Mkdir(classPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(classPath, "noterm.sl"), []byte(`this is deliberately invalid Sleep source`), 0o600); err != nil {
		t.Fatal(err)
	}
	const source = `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$script = [$loader loadScript: "noterm.sl"];
println($script);
println(checkError());
`
	programPath := filepath.Join(root, "loader-direct-probe.sl")
	if err := os.WriteFile(programPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-Dsleep.classpath=programs", "-jar", jar, filepath.Base(programPath))
	command.Dir = root
	reference, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep direct filename probe: %v\n%s", err, reference)
	}
	if !bytes.Contains(reference, []byte("FileNotFoundException")) || bytes.Contains(reference, []byte("YourCodeSucksException")) {
		t.Fatalf("official Sleep direct filename classification = %q, want FileNotFoundException", reference)
	}

	var output bytes.Buffer
	runtime, err := New(WithSleepClasspath("programs"), WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Eval(context.Background(), filepath.Base(programPath), source); err != nil {
		t.Fatal(err)
	}
	if got := output.Bytes(); !bytes.Contains(got, []byte("FileNotFoundException")) || bytes.Contains(got, []byte("YourCodeSucksException")) {
		t.Fatalf("OPFOR direct filename classification = %q, want FileNotFoundException", got)
	}
}

// TestIncludePositiveArityOfficialJARDifferential locks the unusual Sleep 2.1
// stack contract: exactly two arguments select container/member loading, while
// every other positive arity uses argument zero and discards the extras.
func TestIncludePositiveArityOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	root := t.TempDir()
	container := filepath.Join(root, "container")
	if err := os.Mkdir(container, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(container, "member.sl"), []byte(`$trail .= "M";`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "standalone.sl"), []byte(`$trail .= "S";`), 0o600); err != nil {
		t.Fatal(err)
	}
	programPath := filepath.Join(root, "include-arity-probe.sl")
	if err := os.WriteFile(programPath, []byte(sleepIncludeArityProbeSource), 0o600); err != nil {
		t.Fatal(err)
	}

	command := officialSleepJavaCommand(java, "-jar", jar, filepath.Base(programPath))
	command.Dir = root
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep include arity probe: %v\n%s", err, want)
	}

	resolver, err := NewFileSourceResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	runtime, err := New(WithSourceResolver(resolver), WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Eval(context.Background(), filepath.Base(programPath), sleepIncludeArityProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("official Sleep include arity output mismatch\nwant:\n%sgot:\n%s", want, got.Bytes())
	}
}
