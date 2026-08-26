package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
)

func TestPortableJavaFilePermissionTimeAndSpaceContracts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects")
	if err := os.WriteFile(path, []byte("opfor"), 0o640); err != nil {
		t.Fatal(err)
	}
	file := constructPortableJavaFileValue(t, String(path))

	const modified = int64(1_567_890_123_000)
	if result := invokePortableJavaFileValue(t, file, "setLastModified", Long(modified)); !result.Truth() {
		t.Fatalf("setLastModified = %s, want true", result.Describe())
	}
	if got := invokePortableJavaFileValue(t, file, "lastModified"); got.Kind() != KindLong || got.Int64() != modified {
		t.Fatalf("lastModified = %s, want long %d", got.Describe(), modified)
	}
	result, handled, err := file.invoke(ObjectInvocation{
		Op: ObjectInvoke, Message: "setLastModified", Arguments: []Argument{{Value: Long(-1)}},
	})
	if !handled || !result.IsNull() || err == nil || err.Error() != "java.lang.IllegalArgumentException: Negative time" {
		t.Fatalf("negative setLastModified = (%s, %t, %v)", result.Describe(), handled, err)
	}
	if value, handled, err := file.invoke(ObjectInvocation{
		Op: ObjectInvoke, Message: "setLastModified", Arguments: []Argument{{Value: String("123")}},
	}); !handled || err != nil || !value.IsNull() {
		t.Fatalf("string setLastModified = (%s, %t, %v), want no-match null", value.Describe(), handled, err)
	}

	spaceSupported := goruntime.GOOS == "linux" || goruntime.GOOS == "darwin" || goruntime.GOOS == "windows"
	for _, method := range []string{"getTotalSpace", "getFreeSpace", "getUsableSpace"} {
		value := invokePortableJavaFileValue(t, file, method)
		wrong := value.Kind() != KindLong
		if spaceSupported {
			wrong = wrong || value.Int64() <= 0
		} else {
			wrong = wrong || value.Int64() != 0
		}
		if wrong {
			t.Errorf("%s = %s, want supported positive long or explicit unsupported zero", method, value.Describe())
		}
	}
	total := invokePortableJavaFileValue(t, file, "getTotalSpace").Int64()
	free := invokePortableJavaFileValue(t, file, "getFreeSpace").Int64()
	usable := invokePortableJavaFileValue(t, file, "getUsableSpace").Int64()
	if spaceSupported && (free > total || usable > total) {
		t.Errorf("space = total:%d free:%d usable:%d, want free/usable <= total", total, free, usable)
	}

	missing := constructPortableJavaFileValue(t, String(filepath.Join(filepath.Dir(path), "missing")))
	for _, method := range []string{"getTotalSpace", "getFreeSpace", "getUsableSpace"} {
		if value := invokePortableJavaFileValue(t, missing, method); value.Kind() != KindLong || value.Int64() != 0 {
			t.Errorf("missing %s = %s, want long zero", method, value.Describe())
		}
	}

	invalid := constructPortableJavaFileValue(t, String("invalid\x00path"))
	for _, method := range []string{"setReadOnly", "canExecute", "getTotalSpace", "getFreeSpace", "getUsableSpace"} {
		value := invokePortableJavaFileValue(t, invalid, method)
		if value.Int64() != 0 {
			t.Errorf("invalid %s = %s, want zero/false", method, value.Describe())
		}
	}
	for _, method := range []string{"setWritable", "setReadable", "setExecutable"} {
		if value := invokePortableJavaFileValue(t, invalid, method, Int(1)); value.Truth() {
			t.Errorf("invalid %s = %s, want false", method, value.Describe())
		}
	}

	if goruntime.GOOS == "windows" {
		return
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	permissionCases := []struct {
		method    string
		arguments []Value
		want      os.FileMode
	}{
		{method: "setWritable", arguments: []Value{Int(0), Int(0)}, want: 0o440},
		{method: "setWritable", arguments: []Value{Int(1)}, want: 0o640},
		{method: "setReadable", arguments: []Value{Int(0)}, want: 0o240},
		{method: "setReadable", arguments: []Value{Int(1), Int(0)}, want: 0o644},
		{method: "setExecutable", arguments: []Value{Int(1), Int(0)}, want: 0o755},
		{method: "setExecutable", arguments: []Value{Int(0)}, want: 0o655},
	}
	for _, test := range permissionCases {
		if value := invokePortableJavaFileValue(t, file, test.method, test.arguments...); !value.Truth() {
			t.Fatalf("%s%v = %s, want true", test.method, test.arguments, value.Describe())
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if got := info.Mode().Perm(); got != test.want {
			t.Errorf("mode after %s%v = %04o, want %04o", test.method, test.arguments, got, test.want)
		}
	}
	if invokePortableJavaFileValue(t, file, "canExecute").Truth() {
		t.Fatal("canExecute after clearing owner execute = true, want false")
	}
	if value := invokePortableJavaFileValue(t, file, "setExecutable", Int(1)); !value.Truth() {
		t.Fatalf("setExecutable(true) = %s", value.Describe())
	}
	if !invokePortableJavaFileValue(t, file, "canExecute").Truth() {
		t.Fatal("canExecute after setting owner execute = false, want true")
	}
	if value := invokePortableJavaFileValue(t, file, "setReadOnly"); !value.Truth() {
		t.Fatalf("setReadOnly = %s", value.Describe())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got&0o222 != 0 {
		t.Errorf("mode after setReadOnly = %04o, want no write bits", got)
	}
}

func TestPortableJavaFilePrimitiveArgumentContracts(t *testing.T) {
	boxedBoolean := ObjectValue(&portableJavaPrimitive{class: "java.lang.Boolean", value: Int(1)})
	boxedLong := ObjectValue(&portableJavaPrimitive{class: "java.lang.Long", value: Long(42)})
	boxedInteger := ObjectValue(&portableJavaPrimitive{class: "java.lang.Integer", value: Int(42)})

	booleanCases := []struct {
		value Value
		want  bool
		ok    bool
	}{
		{value: Null(), want: false, ok: true},
		{value: Int(2), want: true, ok: true},
		{value: Long(0), want: false, ok: true},
		{value: Double(-0.5), want: false, ok: true}, // Java uses intValue(), not truthiness.
		{value: boxedBoolean, want: true, ok: true},
		{value: String("1"), ok: false},
		{value: boxedInteger, ok: false},
	}
	for _, test := range booleanCases {
		got, ok := portableJavaFileBooleanArgument(test.value)
		if got != test.want || ok != test.ok {
			t.Errorf("boolean argument %s = (%t, %t), want (%t, %t)", test.value.Describe(), got, ok, test.want, test.ok)
		}
	}
	longCases := []struct {
		value Value
		want  int64
		ok    bool
	}{
		{value: Null(), ok: true},
		{value: Int(7), want: 7, ok: true},
		{value: Double(8.9), want: 8, ok: true},
		{value: boxedLong, want: 42, ok: true},
		{value: String("9"), ok: false},
		{value: boxedInteger, ok: false},
	}
	for _, test := range longCases {
		got, ok := portableJavaFileLongArgument(test.value)
		if got != test.want || ok != test.ok {
			t.Errorf("long argument %s = (%d, %t), want (%d, %t)", test.value.Describe(), got, ok, test.want, test.ok)
		}
	}
}

func TestPortableJavaFileCanonicalPathContracts(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(realDirectory, "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "link")); err != nil {
		t.Skipf("host cannot create symlinks: %v", err)
	}
	if err := os.Symlink("nowhere", filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "deep"), filepath.Join(root, "nested-link")); err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "existing symlink prefix and missing suffix",
			path: root + string(filepath.Separator) + strings.Join(
				[]string{"link", "directory", "..", "missing", "leaf"}, string(filepath.Separator),
			),
			want: filepath.Join(resolvedRoot, "real", "missing", "leaf"),
		},
		{
			name: "dangling symlink retained",
			path: filepath.Join(root, "dangling", "child"),
			want: filepath.Join(resolvedRoot, "dangling", "child"),
		},
		{
			name: "dot parent applies after symlink target",
			path: root + string(filepath.Separator) + strings.Join(
				[]string{"nested-link", "..", "leaf"}, string(filepath.Separator),
			),
			want: filepath.Join(resolvedRoot, "real", "leaf"),
		},
		{
			name: "missing dot segments collapsed",
			path: root + string(filepath.Separator) + strings.Join(
				[]string{"missing", "..", "leaf"}, string(filepath.Separator),
			),
			want: filepath.Join(resolvedRoot, "leaf"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := constructPortableJavaFileValue(t, String(test.path))
			if got := invokePortableJavaFileValue(t, file, "getCanonicalPath").String(); got != test.want {
				t.Errorf("getCanonicalPath = %q, want %q", got, test.want)
			}
			value := invokePortableJavaFileValue(t, file, "getCanonicalFile")
			canonical, ok := portableJavaFileValue(value)
			if !ok || canonical.pathValue().String() != test.want {
				t.Errorf("getCanonicalFile = %s, want File(%q)", value.Describe(), test.want)
			}
		})
	}

	// A binary Sleep pathname crosses into the filesystem as its exact Java
	// chars. Canonicalization returns an OS-derived textual spelling rather than
	// falsely retaining byte provenance on the new path.
	binaryLeaf := BinaryString([]byte{0xc3, 0xa9})
	binaryFile := constructPortableJavaFileValues(t,
		ObjectValue(constructPortableJavaFileValue(t, String(root))), binaryLeaf,
	)
	canonical := invokePortableJavaFileValue(t, binaryFile, "getCanonicalPath")
	name := portableJavaFileNameValue(canonical)
	assertPortableJavaFileStringState(t, name, []uint16{0x00c3, 0x00a9}, []bool{false, false})

	invalid := constructPortableJavaFileValue(t, String("bad\x00path"))
	value, handled, err := invalid.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "getCanonicalPath"})
	if !handled || !value.IsNull() || err == nil || err.Error() != "java.io.IOException: Invalid file path" {
		t.Fatalf("invalid getCanonicalPath = (%s, %t, %v)", value.Describe(), handled, err)
	}
}

func TestPortableJavaFileMethodsHonorCanceledEvaluation(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runtimeInstance.Eval(ctx, "canceled-file-method.sl", `
import java.io.File;
$file = [new File: '.'];
return [$file getCanonicalPath];
`)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled File evaluation error = %v, want context.Canceled", err)
	}
}

func TestPortableJavaFileMethodsOfficialJDKDifferential(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows permission and symlink differential is covered by platform tests")
	}
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for java.io.File method differential verification")
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

	goRoot, javaRoot := t.TempDir(), t.TempDir()
	preparePortableJavaFileMethodProbe(t, goRoot)
	preparePortableJavaFileMethodProbe(t, javaRoot)
	var goOutput bytes.Buffer
	runtimeInstance, err := New(WithStdout(&goOutput), WithStderr(&goOutput))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "portable-java-file-methods-probe.sl", portableJavaFileMethodProbeSource(goRoot)); err != nil {
		t.Fatalf("pure-Go File method probe: %v\n%s", err, goOutput.String())
	}
	command := osexec.Command(java, "-jar", jar, "-e", portableJavaFileMethodProbeSource(javaRoot))
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep File method probe: %v\n%s", err, javaOutput)
	}
	got := normalizePortableJavaFileRoot(goOutput.Bytes(), goRoot)
	want := normalizePortableJavaFileRoot(javaOutput, javaRoot)
	if !bytes.Equal(got, want) {
		t.Fatalf("official JDK File method mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func preparePortableJavaFileMethodProbe(t *testing.T, root string) {
	t.Helper()
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDirectory, "data"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nowhere", filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
}

func portableJavaFileMethodProbeSource(root string) string {
	quote := func(path string) string { return strconv.Quote(filepath.ToSlash(path)) }
	return strings.Join([]string{
		"debug(0);",
		"import java.io.File;",
		"$file = [new File: " + quote(filepath.Join(root, "real", "data")) + "];",
		"println([[new File: " + quote(filepath.Join(root, "link", "missing", "leaf")) + "] getCanonicalPath]);",
		"println([[new File: " + quote(filepath.Join(root, "dangling", "child")) + "] getCanonicalPath]);",
		"println([$file setLastModified: 1567890123000L]);",
		"println([$file lastModified]);",
		"println([$file setExecutable: 1, 0]);",
		"println([$file canExecute]);",
		"println([$file setExecutable: 0, 0]);",
		"println([$file canExecute]);",
		"println([$file setReadOnly]);",
		"println([$file setWritable: 1]);",
		"if ([$file getTotalSpace] > 0) { println(1); } else { println(0); }",
		"if ([$file getFreeSpace] > 0) { println(1); } else { println(0); }",
		"if ([$file getUsableSpace] > 0) { println(1); } else { println(0); }",
		"$negative = [$file setLastModified: -1L];",
		"$negative_error = checkError();",
		"if ($negative is $null) { println(1); } else { println(0); }",
		"println([$negative_error getClass]);",
	}, "\n") + "\n"
}
