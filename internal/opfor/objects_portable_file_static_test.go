package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestPortableJavaFileStaticFieldsAndRoots(t *testing.T) {
	fields := map[string]string{
		"separator":         string(os.PathSeparator),
		"separatorChar":     string(os.PathSeparator),
		"pathSeparator":     string(os.PathListSeparator),
		"pathSeparatorChar": string(os.PathListSeparator),
	}
	for field, want := range fields {
		value, handled, err := portableJavaFileStatic(context.Background(), ObjectInvocation{
			Op: ObjectGet, Class: portableJavaFileClass, Message: field,
		})
		if err != nil || !handled || value.String() != want {
			t.Errorf("File.%s = (%s, %t, %v), want %q", field, value.Describe(), handled, err, want)
		}
	}

	value, handled, err := portableJavaFileStatic(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Class: portableJavaFileClass, Message: "listRoots",
	})
	if err != nil || !handled {
		t.Fatalf("File.listRoots = (%s, %t, %v)", value.Describe(), handled, err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("File.listRoots = %s, want Sleep array wrapper", value.Describe())
	}
	roots := array.Values()
	if len(roots) == 0 {
		t.Fatal("File.listRoots unexpectedly returned no roots")
	}
	for index, root := range roots {
		file, ok := portableJavaFileValue(root)
		if !ok || !portableJavaFileIsAbsoluteValue(file.pathValue(), goruntime.GOOS) {
			t.Errorf("File.listRoots[%d] = %s, want absolute File", index, root.Describe())
		}
	}
}

func TestPortableJavaFileCreateTempFileContract(t *testing.T) {
	root := t.TempDir()
	directory := ObjectValue(newPortableJavaFile(String(root)))
	invoke := func(arguments ...Value) (Value, bool, error) {
		converted := make([]Argument, len(arguments))
		for index, argument := range arguments {
			converted[index] = Argument{Value: argument}
		}
		return portableJavaFileStatic(context.Background(), ObjectInvocation{
			Op: ObjectInvoke, Class: portableJavaFileClass, Message: "createTempFile", Arguments: converted,
		})
	}

	created, handled, err := invoke(String("alpha"), String(".dat"), directory)
	if err != nil || !handled {
		t.Fatalf("File.createTempFile = (%s, %t, %v)", created.Describe(), handled, err)
	}
	file, ok := portableJavaFileValue(created)
	if !ok {
		t.Fatalf("File.createTempFile = %s, want File", created.Describe())
	}
	path := portableJavaFileHostPath(file.pathValue())
	if filepath.Dir(path) != root || !strings.HasPrefix(filepath.Base(path), "alpha") || !strings.HasSuffix(path, ".dat") {
		t.Errorf("created path = %q, want %q/alpha*.dat", path, root)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("created file: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	defaultSuffix, _, err := invoke(String("prefix"), Null(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defaultFile, ok := portableJavaFileValue(defaultSuffix)
	if !ok || !strings.HasSuffix(defaultFile.String(), ".tmp") {
		t.Fatalf("null suffix result = %s, want *.tmp File", defaultSuffix.Describe())
	}
	if err := os.Remove(portableJavaFileHostPath(defaultFile.pathValue())); err != nil {
		t.Fatal(err)
	}

	strippedPrefix, _, err := invoke(String("a/leaf"), String(".x"), directory)
	if err != nil {
		t.Fatal(err)
	}
	strippedFile, ok := portableJavaFileValue(strippedPrefix)
	if !ok || !strings.HasPrefix(filepath.Base(strippedFile.String()), "leaf") {
		t.Fatalf("path-bearing prefix result = %s, want leaf*.x", strippedPrefix.Describe())
	}
	if err := os.Remove(portableJavaFileHostPath(strippedFile.pathValue())); err != nil {
		t.Fatal(err)
	}

	normalizedSuffix, _, err := invoke(String("abc"), String(string(os.PathSeparator)), directory)
	if err != nil {
		t.Fatal(err)
	}
	normalizedFile, ok := portableJavaFileValue(normalizedSuffix)
	if !ok || strings.Contains(filepath.Base(normalizedFile.String()), string(os.PathSeparator)) {
		t.Fatalf("separator-only suffix result = %s, want normalized File name", normalizedSuffix.Describe())
	}
	if err := os.Remove(portableJavaFileHostPath(normalizedFile.pathValue())); err != nil {
		t.Fatal(err)
	}

	nameMax := portableJavaFileNameMax(root)
	longPrefixText := strings.Repeat("p", nameMax+64)
	shortened, _, err := invoke(String(longPrefixText), String(".tmp"), directory)
	if err != nil {
		t.Fatal(err)
	}
	shortenedFile, ok := portableJavaFileValue(shortened)
	if !ok {
		t.Fatalf("long-prefix result = %s, want File", shortened.Describe())
	}
	shortenedName := portableJavaFileNameValue(shortenedFile.pathValue())
	if got := sleepStringLength(shortenedName); got != nameMax {
		t.Errorf("shortened temporary name length = %d, want nameMax %d", got, nameMax)
	}
	if !strings.HasPrefix(shortenedName.String(), "ppp") || !strings.HasSuffix(shortenedName.String(), ".tmp") {
		t.Errorf("shortened temporary name = %q, want preserved prefix/suffix", shortenedName.String())
	}
	if err := os.Remove(portableJavaFileHostPath(shortenedFile.pathValue())); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name      string
		arguments []Value
		wantError string
	}{
		{name: "null prefix", arguments: []Value{Null(), String(".x"), directory}, wantError: `java.lang.NullPointerException: Cannot invoke "String.length()" because "prefix" is null`},
		{name: "short prefix", arguments: []Value{String("ab"), Null(), directory}, wantError: `java.lang.IllegalArgumentException: Prefix string "ab" too short: length must be at least 3`},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, handled, err := invoke(test.arguments...)
			if !handled || !value.IsNull() || err == nil || err.Error() != test.wantError {
				t.Fatalf("createTempFile = (%s, %t, %v), want error %q", value.Describe(), handled, err, test.wantError)
			}
		})
	}
	value, handled, err := invoke(
		String("abc"), String(".x"),
		ObjectValue(newPortableJavaFile(String(filepath.Join(root, "missing")))),
	)
	if !handled || !value.IsNull() || err == nil || !strings.HasPrefix(err.Error(), "java.io.IOException: ") {
		t.Fatalf("missing directory = (%s, %t, %v), want IOException", value.Describe(), handled, err)
	}

	value, handled, err = invoke(String("abc"), String(string(os.PathSeparator)+"leaf"), directory)
	if !handled || !value.IsNull() || err == nil || !strings.HasPrefix(err.Error(), "java.io.IOException: Unable to create temporary file, abc") {
		t.Fatalf("separator suffix = (%s, %t, %v), want IOException", value.Describe(), handled, err)
	}
	value, handled, err = invoke(String("abc"), String(".x"), String(root))
	if !handled || !value.IsNull() || err != nil {
		t.Fatalf("non-File directory = (%s, %t, %v), want handled reflection miss", value.Describe(), handled, err)
	}
}

func TestPortableJavaFileNameUsesWindowsPrefixLength(t *testing.T) {
	for _, test := range []struct {
		path string
		want string
	}{
		{path: `C:alpha`, want: "alpha"},
		{path: `C:`, want: ""},
		{path: `C:\`, want: ""},
		{path: `C:\alpha`, want: "alpha"},
		{path: `\\server`, want: "server"},
	} {
		if got := portableJavaFileNameValueForGOOS(String(test.path), "windows").String(); got != test.want {
			t.Errorf("Windows File(%q).getName = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestPortableJavaFileCreateTempFileConcurrent(t *testing.T) {
	root := t.TempDir()
	directory := ObjectValue(newPortableJavaFile(String(root)))
	const workers = 24
	paths := make(chan string, workers)
	errorsChannel := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, handled, err := portableJavaFileStatic(context.Background(), ObjectInvocation{
				Op: ObjectInvoke, Class: portableJavaFileClass, Message: "createTempFile",
				Arguments: []Argument{{Value: String("race")}, {Value: String(".tmp")}, {Value: directory}},
			})
			if err != nil || !handled {
				errorsChannel <- fmt.Errorf("createTempFile = (%s, %t, %v)", value.Describe(), handled, err)
				return
			}
			file, ok := portableJavaFileValue(value)
			if !ok {
				errorsChannel <- fmt.Errorf("createTempFile = %s, want File", value.Describe())
				return
			}
			paths <- portableJavaFileHostPath(file.pathValue())
		}()
	}
	wait.Wait()
	close(paths)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
	seen := make(map[string]struct{}, workers)
	for path := range paths {
		if _, duplicate := seen[path]; duplicate {
			t.Errorf("duplicate temporary pathname %q", path)
		}
		seen[path] = struct{}{}
		if err := os.Remove(path); err != nil {
			t.Errorf("remove %q: %v", path, err)
		}
	}
	if len(seen) != workers {
		t.Errorf("created paths = %d, want %d", len(seen), workers)
	}
}

func TestPortableJavaFileCreateTempFileHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value, handled, err := portableJavaFileStatic(ctx, ObjectInvocation{
		Op: ObjectInvoke, Class: portableJavaFileClass, Message: "createTempFile",
		Arguments: []Argument{
			{Value: String("cancel")},
			{Value: String(".tmp")},
			{Value: ObjectValue(newPortableJavaFile(String(root)))},
		},
	})
	if !handled || !value.IsNull() || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled createTempFile = (%s, %t, %v)", value.Describe(), handled, err)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("canceled createTempFile entries = %d, %v", len(entries), readErr)
	}
}

func TestPortableJavaFileStaticRuntimeAndImporterPrecedence(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "file-static-runtime.sl", portableJavaFileStaticProbeSource(root)); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), portableJavaFileStaticProbeOutput(); got != want {
		t.Fatalf("portable File static output\nwant:\n%sgot:\n%s", want, got)
	}

	calls := 0
	overridden, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if resolvePortableClassName(invocation.Class) == portableJavaFileClass && invocation.Message == "listRoots" {
			calls++
			return String("importer roots"), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = overridden.Close(context.Background()) })
	value, err := overridden.Eval(context.Background(), "file-static-host-first.sl", `import java.io.File; return [File listRoots];`)
	if err != nil || value.String() != "importer roots" || calls != 1 {
		t.Fatalf("host-first File.listRoots = (%s, %v), calls %d", value.Describe(), err, calls)
	}
}

func portableJavaFileStaticProbeOutput() string {
	return fmt.Sprintf(`1
class sleep.engine.types.ListContainer
1
class java.io.File
1
%s
%s
%s
%s
1
1
1
1
1
1
1
1
class java.lang.IllegalArgumentException
java.lang.IllegalArgumentException: Prefix string "ab" too short: length must be at least 3
class java.io.IOException
1
1
1
1
1
1
1
1
`, string(os.PathSeparator), string(os.PathSeparator), string(os.PathListSeparator), string(os.PathListSeparator))
}

func portableJavaFileStaticProbeSource(root string) string {
	quotedRoot := strconv.Quote(filepath.ToSlash(root))
	return strings.Join([]string{
		"debug(0);",
		"import java.io.File;",
		"@roots = [File listRoots];",
		"if (size(@roots) > 0) { println(1); } else { println(0); }",
		"println([@roots getClass]);",
		"$root_count = size(@roots); push(@roots, $null);",
		"if (size(@roots) == $root_count + 1) { println(1); } else { println(0); }",
		"println([@roots[0] getClass]);",
		"println([@roots[0] isAbsolute]);",
		"println([File separator]);",
		"println([File separatorChar]);",
		"println([File pathSeparator]);",
		"println([File pathSeparatorChar]);",
		"$directory = [new File: " + quotedRoot + "];",
		"$file = [File createTempFile: 'alpha', '.dat', $directory];",
		"println([$file exists]);",
		"println([[$file getName] startsWith: 'alpha']);",
		"println([[$file getName] endsWith: '.dat']);",
		"println([$file delete]);",
		"$default = [File createTempFile: 'prefix', $null, $directory];",
		"println([[$default getName] endsWith: '.tmp']);",
		"println([$default delete]);",
		"$stripped = [File createTempFile: 'a/leaf', '.x', $directory];",
		"println([[$stripped getName] startsWith: 'leaf']);",
		"println([$stripped delete]);",
		"$short = [File createTempFile: 'ab', $null, $directory];",
		"$short_error = checkError();",
		"println([$short_error getClass]);",
		"println($short_error);",
		"$bad_suffix = [File createTempFile: 'abc', '/leaf', $directory];",
		"$suffix_error = checkError();",
		"println([$suffix_error getClass]);",
		"$bad_directory = [File createTempFile: 'abc', '.x', " + quotedRoot + "];",
		"if ($bad_directory is $null) { println(1); } else { println(0); }",
		"$long = [File createTempFile: " + strconv.Quote(strings.Repeat("p", 320)) + ", '.tmp', $directory];",
		"if ([[$long getName] length] < 320) { println(1); } else { println(0); }",
		"println([[$long getName] startsWith: 'ppp']);",
		"println([[$long getName] endsWith: '.tmp']);",
		"println([$long delete]);",
		"$normalized = [File createTempFile: 'abc', '/', $directory];",
		"println([$normalized exists]);",
		"if ([[$normalized getName] indexOf: '/'] == -1) { println(1); } else { println(0); }",
		"println([$normalized delete]);",
	}, "\n") + "\n"
}

func TestPortableJavaFileStaticOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	goRoot, javaRoot := t.TempDir(), t.TempDir()
	var goOutput bytes.Buffer
	runtimeInstance, err := New(WithStdout(&goOutput), WithStderr(&goOutput))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "file-static-differential.sl", portableJavaFileStaticProbeSource(goRoot)); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaFileStaticProbeSource(javaRoot))
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep File static probe: %v\n%s", err, javaOutput)
	}
	if !bytes.Equal(goOutput.Bytes(), javaOutput) {
		t.Fatalf("official Sleep File static mismatch\nwant:\n%sgot:\n%s", javaOutput, goOutput.Bytes())
	}
}
