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
	"reflect"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const portableJavaFileModified = int64(1_234_567_890_000)

// TestPortableJavaFileSourceContract covers the immutable java.io.File
// constructors and accessors used through Sleep's ObjectNew/ObjectAccess path.
// The source contract is OpenJDK java.io.File plus Sleep 2.1's reflection
// bridge at Cobalt-Strike/sleep@60ac3ff9.
func TestPortableJavaFileSourceContract(t *testing.T) {
	root := t.TempDir()
	filePath := preparePortableJavaFileFixture(t, root)

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	source := fmt.Sprintf(`
import java.io.File;
$path = %s;
$root = %s;
$file = [new File: $path];
$same = $file;
$other = [new File: $path];
$by_string = [new File: $root, "visible.txt"];
$parent = [new File: $root];
$by_file = [new File: $parent, "visible.txt"];
$directory = [new File: $root, "folder"];
$hidden = [new File: $root, ".missing-hidden"];
$missing = [new File: $root, "missing"];
$leaf = [new File: "leaf"];
return @(
    [$file exists], [$file isFile], [$file isDirectory],
    [$file canRead], [$file canWrite], [$file isHidden],
    [$file length], [$file lastModified], [$file getName],
    [$file getParent], [$file getPath], [$file getAbsolutePath],
    [$file toString], [$by_string getPath], [$by_file getPath],
    [$directory exists], [$directory isDirectory], [$hidden isHidden],
    [$missing exists], [$missing length], [$missing lastModified],
    $same is $file, $other is $file,
    $file isa ^File, $file isa ^Object,
    $file isa ^java.io.Serializable, $file isa ^java.lang.Comparable,
    [$file getClass], [$leaf getParent]
);
`, strconv.Quote(filepath.ToSlash(filePath)), strconv.Quote(filepath.ToSlash(root)))
	result, err := runtime.Eval(context.Background(), "portable-java-file.sl", source)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if len(values) != 29 {
		t.Fatalf("result length = %d, want 29: %s", len(values), result.Describe())
	}

	for _, index := range []int{0, 3, 4, 15, 16, 21, 23, 24, 25, 26} {
		if !values[index].Truth() {
			t.Errorf("result[%d] = %s, want true", index, values[index].Describe())
		}
	}
	for _, index := range []int{2, 5, 18, 22} {
		if values[index].Truth() {
			t.Errorf("result[%d] = %s, want false", index, values[index].Describe())
		}
	}
	if want := goruntime.GOOS != "windows"; values[17].Truth() != want {
		t.Errorf("missing dot-file isHidden = %s, want %t", values[17].Describe(), want)
	}
	if values[6].Kind() != KindLong || values[6].Int64() != int64(len("opfor!")) {
		t.Errorf("length = %s, want long %d", values[6].Describe(), len("opfor!"))
	}
	if values[7].Kind() != KindLong || values[7].Int64() != portableJavaFileModified {
		t.Errorf("lastModified = %s, want long %d", values[7].Describe(), portableJavaFileModified)
	}
	if values[19].Kind() != KindLong || values[19].Int64() != 0 || values[20].Kind() != KindLong || values[20].Int64() != 0 {
		t.Errorf("missing metadata = (%s, %s), want long zeroes", values[19].Describe(), values[20].Describe())
	}

	wantPath := filepath.FromSlash(filepath.ToSlash(filePath))
	wantRoot := filepath.FromSlash(filepath.ToSlash(root))
	for _, index := range []int{10, 11, 12, 13, 14} {
		if values[index].String() != wantPath {
			t.Errorf("result[%d] = %q, want %q", index, values[index].String(), wantPath)
		}
	}
	if values[8].String() != "visible.txt" || values[9].String() != wantRoot {
		t.Errorf("name/parent = (%q, %q), want (visible.txt, %q)", values[8].String(), values[9].String(), wantRoot)
	}
	if values[27].String() != "class java.io.File" {
		t.Errorf("getClass = %q, want class java.io.File", values[27].String())
	}
	if !values[28].IsNull() {
		t.Errorf("relative leaf parent = %s, want $null", values[28].Describe())
	}
}

func TestPortableJavaFileAuthoredConfigLoaderShape(t *testing.T) {
	root := t.TempDir()
	configuration := filepath.Join(root, "local.cna")
	if err := os.WriteFile(configuration, []byte("# fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	source := fmt.Sprintf(`
import java.io.File;
sub load_local_config {
    local('$resolved_path $file');
    $resolved_path = $1;
    $file = [new File: $resolved_path];
    if ([$file exists]) {
        return 1;
    }
    return 0;
}
return load_local_config(%s);
`, strconv.Quote(filepath.ToSlash(configuration)))
	result, err := runtime.Eval(context.Background(), "authored-config-loader.sl", source)
	if err != nil || !result.Truth() {
		t.Fatalf("authored File.exists loader = %s, %v; want true", result.Describe(), err)
	}
}

// TestPortableJavaFileConstructorCoercionContract covers Sleep 2.1
// ObjectUtilities' MAYBE scalar-to-String matching and its null-parent
// overload choice for java.io.File. Compound values and null child arguments
// still have no matching constructor.
func TestPortableJavaFileConstructorCoercionContract(t *testing.T) {
	var warnings bytes.Buffer
	runtime, err := New(WithStderr(&warnings))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "portable-java-file-coercion.sl", `
import java.io.File;
$integer = [new File: 123];
$long = [new File: 456L];
$double = [new File: 12.5];
$null_parent = [new File: $null, 'leaf'];
$empty_parent = [new File: '', 'leaf'];
$numeric_parent = [new File: 123, 'leaf'];
$numeric_child = [new File: 'parent', 456];
$file_numeric_child = [new File: [new File: 'parent'], 789L];
$null_only = [new File: $null];
$null_child = [new File: 'parent', $null];
$array_parent = [new File: @(), 'leaf'];
return @(
    [$integer getPath], [$long getPath], [$double getPath],
    [$null_parent getPath], [$empty_parent getPath],
    [$numeric_parent getPath], [$numeric_child getPath],
    [$file_numeric_child getPath],
    $null_only is $null, $null_child is $null, $array_parent is $null
);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok || array.Len() != 11 {
		t.Fatalf("result = %s, want eleven values", result.Describe())
	}
	values := array.Values()
	wantPaths := []string{
		"123", "456", "12.5", "leaf",
		string(filepath.Separator) + "leaf",
		filepath.Join("123", "leaf"), filepath.Join("parent", "456"), filepath.Join("parent", "789"),
	}
	for index, want := range wantPaths {
		if got := values[index].String(); got != want {
			t.Errorf("constructor path[%d] = %q, want %q", index, got, want)
		}
	}
	for index := 8; index < len(values); index++ {
		if !values[index].Truth() {
			t.Errorf("constructor null check[%d] = %s, want true", index, values[index].Describe())
		}
	}
	if got := strings.Count(warnings.String(), "no constructor matching java.io.File"); got != 3 {
		t.Errorf("constructor warning count = %d, want 3\n%s", got, warnings.String())
	}
}

// TestPortableJavaFileObjectMethodSourceContract pins the remaining immutable
// File/Object methods to OpenJDK's File, UnixFileSystem/WinNTFileSystem, and
// Sleep 2.1 ObjectUtilities reflection behavior. In particular, compareTo's
// synthetic Object bridge accepts a mismatched scalar before failing its cast.
func TestPortableJavaFileObjectMethodSourceContract(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.VolumeName(workingDirectory) + string(filepath.Separator)
	if rootPath == "" {
		rootPath = string(filepath.Separator)
	}
	absolutePath := filepath.Join(workingDirectory, "absolute-leaf")

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "portable-java-file-object-methods.sl", fmt.Sprintf(`
debug(0);
import java.io.File;
$relative = [new File: 'alpha/../leaf'];
$absolute = [new File: %s];
$root = [new File: %s];
$leaf = [new File: 'leaf'];
$parented = [new File: 'alpha/leaf'];
$number = [new File: 123];
$number_string = [new File: '123'];
$relative_absolute = [$relative getAbsoluteFile];
$absolute_copy = [$absolute getAbsoluteFile];
$parent_file = [$parented getParentFile];
$wrong_compare = [$relative compareTo: 'alpha'];
$wrong_error = checkError();
$null_compare = [$relative compareTo: $null];
$null_error = checkError();
return @(
    [$relative isAbsolute], [$absolute isAbsolute], [$root isAbsolute],
    $relative_absolute, [$relative_absolute getClass],
    [$relative_absolute equals: $relative],
    $absolute_copy, $absolute_copy is $absolute,
    [$absolute_copy equals: $absolute],
    $parent_file, [$parent_file getClass], [$parent_file getPath],
    [$leaf getParentFile], [$root getParentFile],
    [$number equals: $number_string], [$number compareTo: $number_string],
    [$number equals: 123], [$number equals: $null],
    [[new File: 'alpha'] compareTo: [new File: 'beta']],
    [[new File: 'beta'] compareTo: [new File: 'alpha']],
    [[new File: 'alpha'] compareTo: [new File: 'alphabet']],
    [$number hashCode], [[new File: ''] hashCode],
    $wrong_compare, [$wrong_error getClass],
    $null_compare, [$null_error getClass],
    [[$absolute isAbsolute] getClass],
    [[$number equals: $number_string] getClass],
    [[$number hashCode] getClass],
    [[$number compareTo: $number_string] getClass]
);
`, strconv.Quote(filepath.ToSlash(absolutePath)), strconv.Quote(filepath.ToSlash(rootPath))))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok || array.Len() != 31 {
		t.Fatalf("result = %s, want 31 values", result.Describe())
	}
	values := array.Values()

	for _, index := range []int{0, 5, 16, 17} {
		if values[index].Kind() != KindInt || values[index].Int32() != 0 {
			t.Errorf("result[%d] = %s, want integer false", index, values[index].Describe())
		}
	}
	if !values[7].IsNull() {
		t.Errorf("absolute File identity comparison = %s, want null false", values[7].Describe())
	}
	for _, index := range []int{1, 2, 8, 14} {
		if values[index].Kind() != KindInt || values[index].Int32() != 1 {
			t.Errorf("result[%d] = %s, want integer true", index, values[index].Describe())
		}
	}

	relativeAbsolute, ok := values[3].Object()
	if !ok {
		t.Fatalf("getAbsoluteFile = %s, want object", values[3].Describe())
	}
	relativeAbsoluteFile, ok := relativeAbsolute.(*portableJavaFile)
	if !ok || relativeAbsoluteFile == nil {
		t.Fatalf("getAbsoluteFile object = %T, want *portableJavaFile", relativeAbsolute)
	}
	wantRelativeAbsolute, err := portableJavaFileAbsolute(filepath.FromSlash("alpha/../leaf"))
	if err != nil {
		t.Fatal(err)
	}
	if relativeAbsoluteFile.path != wantRelativeAbsolute || values[4].String() != "class java.io.File" {
		t.Errorf("relative absolute file = (%q, %q), want (%q, class java.io.File)", relativeAbsoluteFile.path, values[4].String(), wantRelativeAbsolute)
	}

	absoluteCopy, ok := values[6].Object()
	if !ok {
		t.Fatalf("absolute getAbsoluteFile = %s, want object", values[6].Describe())
	}
	absoluteCopyFile, ok := absoluteCopy.(*portableJavaFile)
	if !ok || absoluteCopyFile == nil || absoluteCopyFile.path != absolutePath {
		t.Errorf("absolute getAbsoluteFile = %#v, want new File(%q)", absoluteCopy, absolutePath)
	}

	parent, ok := values[9].Object()
	if !ok {
		t.Fatalf("getParentFile = %s, want object", values[9].Describe())
	}
	parentFile, ok := parent.(*portableJavaFile)
	if !ok || parentFile == nil || parentFile.path != "alpha" {
		t.Errorf("getParentFile object = %#v, want File(alpha)", parent)
	}
	if values[10].String() != "class java.io.File" || values[11].String() != "alpha" {
		t.Errorf("parent class/path = (%q, %q), want (class java.io.File, alpha)", values[10].String(), values[11].String())
	}
	for _, index := range []int{12, 13, 23, 25} {
		if !values[index].IsNull() {
			t.Errorf("result[%d] = %s, want null", index, values[index].Describe())
		}
	}

	for index, want := range map[int]int32{15: 0, 18: -1, 19: 1, 20: -3, 21: 1207203, 22: 1234321} {
		if values[index].Kind() != KindInt || values[index].Int32() != want {
			t.Errorf("result[%d] = %s, want int %d", index, values[index].Describe(), want)
		}
	}
	if values[24].String() != "class java.lang.ClassCastException" {
		t.Errorf("mismatched compareTo error = %q, want ClassCastException", values[24].String())
	}
	if values[26].String() != "class java.lang.NullPointerException" {
		t.Errorf("null compareTo error = %q, want NullPointerException", values[26].String())
	}
	for _, index := range []int{27, 28, 29, 30} {
		if values[index].String() != "class java.lang.Integer" {
			t.Errorf("return class[%d] = %q, want class java.lang.Integer", index, values[index].String())
		}
	}
}

func TestPortableJavaFileJavaStringContracts(t *testing.T) {
	if got := portableJavaStringCompare("alpha", "alphabet"); got != -3 {
		t.Errorf("Java prefix comparison = %d, want -3", got)
	}
	if got := portableJavaStringCompare("a😀", "a😁"); got != -1 {
		t.Errorf("Java supplementary comparison = %d, want -1", got)
	}
	for path, want := range map[string]int32{
		"":     1234321,
		"123":  1207203,
		"café": 3977136,
		"😀":    645362,
	} {
		if got := portableJavaFileHash(path); got != want {
			t.Errorf("File(%q).hashCode = %d, want %d", path, got, want)
		}
	}
}

func TestPortableJavaFileExactUTF16PathIdentityAndProvenance(t *testing.T) {
	separator := uint16('/')
	if goruntime.GOOS == "windows" {
		separator = '\\'
	}

	binaryParentValue := BinaryString([]byte{0xc3, 0xa9})
	binaryParent := constructPortableJavaFileValue(t, binaryParentValue)
	binaryPath := invokePortableJavaFileValue(t, binaryParent, "getPath")
	assertPortableJavaFileStringState(t, binaryPath, []uint16{0x00c3, 0x00a9}, []bool{true, true})
	if got := portableJavaFileHostPath(binaryPath); got != "Ã©" {
		t.Fatalf("binary-valid-UTF8 host path = %q, want two Java chars Ã©", got)
	}

	textParent := constructPortableJavaFileValue(t, String("é"))
	if invokePortableJavaFileValue(t, binaryParent, "equals", ObjectValue(textParent)).Truth() {
		t.Fatal("binary C3 A9 pathname equals textual U+00E9 pathname, want distinct UTF-16 identity")
	}
	if got := invokePortableJavaFileValue(t, binaryParent, "compareTo", ObjectValue(textParent)).Int32(); got == 0 {
		t.Fatal("binary C3 A9 pathname compareTo textual U+00E9 pathname = 0, want nonzero")
	}
	if portableJavaFileHashValue(binaryParent.pathValue()) == portableJavaFileHashValue(textParent.pathValue()) {
		t.Fatal("binary C3 A9 and textual U+00E9 path hashes unexpectedly match")
	}

	invalidByteValue := String(string([]byte{0xff}))
	invalidByteFile := constructPortableJavaFileValue(t, invalidByteValue)
	invalidBytePath := invokePortableJavaFileValue(t, invalidByteFile, "getPath")
	assertPortableJavaFileStringState(t, invalidBytePath, []uint16{0x00ff}, []bool{true})
	if got := portableJavaFileHostPath(invalidBytePath); got != "ÿ" {
		t.Fatalf("invalid-byte host path = %q, want U+00FF", got)
	}
	textByteFile := constructPortableJavaFileValue(t, String("ÿ"))
	if !invokePortableJavaFileValue(t, invalidByteFile, "equals", ObjectValue(textByteFile)).Truth() {
		t.Fatal("raw-byte U+00FF pathname does not equal textual U+00FF pathname")
	}
	// Provenance is OPFOR metadata rather than part of Java equals/hashCode,
	// but getPath must still return the exact originating spelling.
	assertPortableJavaFileStringState(t,
		invokePortableJavaFileValue(t, textByteFile, "getPath"),
		[]uint16{0x00ff}, []bool{false},
	)

	unpairedChildValue := sleepStringValueFromUnits([]uint16{0xd800, 'x', 0xdc00}, []bool{false, false, false})
	child := constructPortableJavaFileValues(t, ObjectValue(binaryParent), unpairedChildValue)
	wantChildUnits := []uint16{0x00c3, 0x00a9, separator, 0xd800, 'x', 0xdc00}
	wantChildRaw := []bool{true, true, false, false, false, false}
	assertPortableJavaFileStringState(t,
		invokePortableJavaFileValue(t, child, "getPath"), wantChildUnits, wantChildRaw,
	)
	assertPortableJavaFileStringState(t,
		invokePortableJavaFileValue(t, child, "getName"), []uint16{0xd800, 'x', 0xdc00}, []bool{false, false, false},
	)
	assertPortableJavaFileStringState(t,
		invokePortableJavaFileValue(t, child, "getParent"), []uint16{0x00c3, 0x00a9}, []bool{true, true},
	)
	if got := portableJavaFileHostPathForGOOS(unpairedChildValue, "linux"); got != "?x?" {
		t.Fatalf("Unix unpaired-surrogate host path = %q, want platform-encoder replacements", got)
	}
	if got := portableJavaFileHostPathForGOOS(unpairedChildValue, "windows"); got != "�x�" {
		t.Fatalf("Windows unpaired-surrogate host path = %q, want Go UTF-16 replacements", got)
	}
	if got, want := invokePortableJavaFileValue(t, child, "hashCode").Int32(), portableJavaFileHashValue(child.pathValue()); got != want {
		t.Fatalf("exact-unit hashCode = %d, want %d", got, want)
	}
}

func TestPortableJavaFileExactPathSelectsDistinctHostFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Ã©"), []byte("binary-units"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "é"), []byte("text-unit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ÿ"), []byte("invalid-byte-unit"), 0o600); err != nil {
		t.Fatal(err)
	}

	parent := constructPortableJavaFileValue(t, String(root))
	tests := []struct {
		name  string
		value Value
		want  string
	}{
		{name: "binary-valid-utf8", value: BinaryString([]byte{0xc3, 0xa9}), want: "binary-units"},
		{name: "text", value: String("é"), want: "text-unit"},
		{name: "invalid-byte", value: String(string([]byte{0xff})), want: "invalid-byte-unit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := constructPortableJavaFileValues(t, ObjectValue(parent), test.value)
			if !invokePortableJavaFileValue(t, file, "exists").Truth() {
				t.Fatalf("File(%s).exists = false", test.value.Describe())
			}
			data, err := os.ReadFile(portableJavaFileFilesystemPathValue(file.pathValue()))
			if err != nil || string(data) != test.want {
				t.Fatalf("selected host file = %q, %v; want %q", data, err, test.want)
			}
		})
	}
}

func TestPortableJavaFileWindowsNormalizationAndInvalidPathContract(t *testing.T) {
	normalization := []struct {
		path string
		want string
	}{
		{path: `C:/alpha//beta/`, want: `C:\alpha\beta`},
		{path: `\\?\C:\alpha`, want: `C:\alpha`},
		{path: `\\?\UNC\host\share`, want: `\\host\share`},
		{path: `///C:/alpha`, want: `C:\alpha`},
		{path: `\\\host\\share\\`, want: `\\host\share`},
	}
	for _, test := range normalization {
		if got := portableJavaFileNormalizeValueForGOOS(String(test.path), "windows").String(); got != test.want {
			t.Errorf("Windows normalize(%q) = %q, want %q", test.path, got, test.want)
		}
	}

	invalid := []struct {
		path string
		want bool
	}{
		{path: "name", want: false},
		{path: "bad\x00name", want: true},
		{path: "name ", want: true},
		{path: `dir /leaf`, want: true},
		{path: `dir\name `, want: true},
		// The pinned OpenJDK enables alternate data streams by default and
		// leaves all remaining Win32-invalid characters to native calls.
		{path: `name:stream`, want: false},
		{path: `<bad>|*?`, want: false},
	}
	for _, test := range invalid {
		normalized := portableJavaFileNormalizeValueForGOOS(String(test.path), "windows")
		if got := portableJavaFileInvalidForGOOS(normalized, "windows"); got != test.want {
			t.Errorf("Windows invalid(%q -> %q) = %t, want %t", test.path, normalized.String(), got, test.want)
		}
	}
	if portableJavaFileInvalidForGOOS(String("name "), "linux") {
		t.Fatal("Unix File path ending in space reported invalid")
	}
}

func TestPortableJavaFileWindowsInvalidPathEffects(t *testing.T) {
	if goruntime.GOOS != "windows" {
		t.Skip("Windows-only File invalid-path effects")
	}
	file := constructPortableJavaFileValue(t, String(filepath.Join(t.TempDir(), "component ")))
	if value, handled, err := file.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "createNewFile"}); !handled || value.Kind() != KindNull || err == nil || err.Error() != "java.io.IOException: Invalid file path" {
		t.Fatalf("createNewFile invalid Windows path = (%s, %t, %v)", value.Describe(), handled, err)
	}
	for _, method := range []string{"delete", "mkdir", "mkdirs"} {
		if got := invokePortableJavaFileValue(t, file, method); got.Truth() {
			t.Errorf("%s invalid Windows path = %s, want false", method, got.Describe())
		}
	}
	for _, method := range []string{"exists", "isFile", "isDirectory", "canRead", "canWrite", "isHidden"} {
		if got := invokePortableJavaFileValue(t, file, method); got.Truth() {
			t.Errorf("%s invalid Windows path = %s, want false", method, got.Describe())
		}
	}
	for _, method := range []string{"length", "lastModified"} {
		if got := invokePortableJavaFileValue(t, file, method); got.Kind() != KindLong || got.Int64() != 0 {
			t.Errorf("%s invalid Windows path = %s, want long zero", method, got.Describe())
		}
	}
	for _, method := range []string{"list", "listFiles"} {
		if got := invokePortableJavaFileValue(t, file, method); !got.IsNull() {
			t.Errorf("%s invalid Windows path = %s, want null", method, got.Describe())
		}
	}
}

func TestPortableJavaFileAccessUsesEffectiveOpenForRegularFiles(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows chmod and read-only attribute semantics differ")
	}
	path := filepath.Join(t.TempDir(), "access")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	file := constructPortableJavaFileValue(t, String(path))

	readable := false
	if handle, err := os.Open(path); err == nil {
		readable = handle.Close() == nil
	}
	writable := false
	if handle, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		writable = handle.Close() == nil
	}
	if got := invokePortableJavaFileValue(t, file, "canRead").Truth(); got != readable {
		t.Errorf("canRead = %t, effective open = %t", got, readable)
	}
	if got := invokePortableJavaFileValue(t, file, "canWrite").Truth(); got != writable {
		t.Errorf("canWrite = %t, effective open = %t", got, writable)
	}
}

func TestPortableJavaFileDirectoryCanWriteNarrowPermissionFallback(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows directory modes are synthesized")
	}
	directory := filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	file := constructPortableJavaFileValue(t, String(directory))
	if got := invokePortableJavaFileValue(t, file, "canWrite"); got.Truth() {
		t.Fatalf("directory canWrite = %s, want permission-bit fallback false", got.Describe())
	}
}

func constructPortableJavaFileValue(t *testing.T, pathname Value) *portableJavaFile {
	t.Helper()
	return constructPortableJavaFileValues(t, pathname)
}

func constructPortableJavaFileValues(t *testing.T, arguments ...Value) *portableJavaFile {
	t.Helper()
	invocation := ObjectInvocation{Op: ObjectConstruct, Class: portableJavaFileClass}
	for _, argument := range arguments {
		invocation.Arguments = append(invocation.Arguments, Argument{Value: argument})
	}
	value, handled, err := portableJavaFileConstruct(invocation)
	if err != nil || !handled {
		t.Fatalf("construct File(%v) = (%s, %t, %v)", arguments, value.Describe(), handled, err)
	}
	object, ok := value.Object()
	if !ok {
		t.Fatalf("construct File(%v) = %s, want object", arguments, value.Describe())
	}
	file, ok := object.(*portableJavaFile)
	if !ok || file == nil {
		t.Fatalf("construct File(%v) object = %T, want *portableJavaFile", arguments, object)
	}
	return file
}

func invokePortableJavaFileValue(t *testing.T, file *portableJavaFile, method string, arguments ...Value) Value {
	t.Helper()
	invocation := ObjectInvocation{Op: ObjectInvoke, Target: ObjectValue(file), Message: method}
	for _, argument := range arguments {
		invocation.Arguments = append(invocation.Arguments, Argument{Value: argument})
	}
	value, handled, err := file.invoke(invocation)
	if err != nil || !handled {
		t.Fatalf("File.%s(%v) = (%s, %t, %v)", method, arguments, value.Describe(), handled, err)
	}
	return value
}

func assertPortableJavaFileStringState(t *testing.T, value Value, wantUnits []uint16, wantRaw []bool) {
	t.Helper()
	if value.Kind() != KindString {
		t.Fatalf("value = %s, want string", value.Describe())
	}
	if got := sleepStringUnits(value); !reflect.DeepEqual(got, wantUnits) {
		t.Errorf("UTF-16 units = %04x, want %04x", got, wantUnits)
	}
	if got := sleepStringRawMask(value); !reflect.DeepEqual(got, wantRaw) {
		t.Errorf("raw provenance = %v, want %v", got, wantRaw)
	}
}

func TestPortableJavaFileEmptyPathUsesProcessWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "entry"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "portable-java-file-empty.sl", `
import java.io.File;
$file = [new File: ''];
@names = scalar([$file list]);
@files = scalar([$file listFiles]);
return @(
    [$file getPath], [$file getName], [$file getParent],
    [$file exists], [$file isDirectory], [$file isFile],
    [$file canRead], [$file canWrite], [$file isHidden],
    [$file length], [$file lastModified], [$file getAbsolutePath],
    @names, @files
);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok || array.Len() != 14 {
		t.Fatalf("result = %s, want fourteen values", result.Describe())
	}
	values := array.Values()
	if values[0].String() != "" || values[1].String() != "" || !values[2].IsNull() {
		t.Errorf("empty pathname identity = %s, want empty/empty/null", result.Describe())
	}
	for _, index := range []int{3, 4, 6, 7} {
		if !values[index].Truth() {
			t.Errorf("empty pathname result[%d] = %s, want true", index, values[index].Describe())
		}
	}
	for _, index := range []int{5, 8} {
		if values[index].Truth() {
			t.Errorf("empty pathname result[%d] = %s, want false", index, values[index].Describe())
		}
	}
	if values[9].Kind() != KindLong || values[9].Int64() != info.Size() {
		t.Errorf("empty length = %s, want %d", values[9].Describe(), info.Size())
	}
	if values[10].Kind() != KindLong || values[10].Int64() != info.ModTime().UnixMilli() {
		t.Errorf("empty lastModified = %s, want %d", values[10].Describe(), info.ModTime().UnixMilli())
	}
	if values[11].String() != root {
		t.Errorf("empty absolute path = %q, want %q", values[11].String(), root)
	}
	assertPortableJavaFileNameSet(t, values[12], []string{"entry"})
	assertPortableJavaFileObjectSet(t, values[13], "", []string{"entry"})
}

// TestPortableJavaFileMutationAndListingSourceContract pins the File methods
// whose effects can be represented faithfully with the Go filesystem API.
// The source contract is OpenJDK File.java and UnixFileSystem/WinNTFileSystem
// at fd596940e81dd79a00f6f6360ae63a1c15eddf91.
func TestPortableJavaFileMutationAndListingSourceContract(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed")
	fresh := filepath.Join(root, "fresh.txt")
	destination := filepath.Join(root, "renamed.txt")
	single := filepath.Join(root, "single")
	deep := filepath.Join(root, "deep")
	middle := filepath.Join(deep, "middle")
	leaf := filepath.Join(middle, "leaf")

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "portable-java-file-effects.sl", fmt.Sprintf(`
debug(0);
import java.io.File;
$root = [new File: %s];
$fresh = [new File: %s];
$destination = [new File: %s];
$single = [new File: %s];
$leaf = [new File: %s];
$middle = [new File: %s];
$deep = [new File: %s];
$missing = [new File: %s];
$made_root = [$root mkdir];
$made_file = [$fresh createNewFile];
$made_file_again = [$fresh createNewFile];
$made_single = [$single mkdir];
$made_single_again = [$single mkdir];
$made_deep = [$leaf mkdirs];
$made_deep_again = [$leaf mkdirs];
$java_names = [$root list];
$java_files = [$root listFiles];
@names = scalar($java_names);
@files = scalar($java_files);
@null_names = scalar([$root list: $null]);
@null_files = scalar([$root listFiles: $null]);
$missing_list = [$missing list];
$renamed = [$fresh renameTo: $destination];
$old_exists = [$fresh exists];
$new_exists = [$destination exists];
$source_path = [$fresh getPath];
$early_root_delete = [$root delete];
$deleted_file = [$destination delete];
$deleted_leaf = [$leaf delete];
$deleted_middle = [$middle delete];
$deleted_deep = [$deep delete];
$deleted_single = [$single delete];
$deleted_root = [$root delete];
return @(
    $made_root, $made_file, $made_file_again,
    $made_single, $made_single_again, $made_deep, $made_deep_again,
    [$java_names getClass], [$java_files getClass],
    @names, @files, @null_names, @null_files, $missing_list,
    $renamed, $old_exists, $new_exists, $source_path,
    $early_root_delete, $deleted_file, $deleted_leaf, $deleted_middle,
    $deleted_deep, $deleted_single, $deleted_root
);
`,
		strconv.Quote(filepath.ToSlash(root)),
		strconv.Quote(filepath.ToSlash(fresh)),
		strconv.Quote(filepath.ToSlash(destination)),
		strconv.Quote(filepath.ToSlash(single)),
		strconv.Quote(filepath.ToSlash(leaf)),
		strconv.Quote(filepath.ToSlash(middle)),
		strconv.Quote(filepath.ToSlash(deep)),
		strconv.Quote(filepath.ToSlash(filepath.Join(root, "missing"))),
	))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok || array.Len() != 25 {
		t.Fatalf("result = %s, want 25 values", result.Describe())
	}
	values := array.Values()
	for _, index := range []int{0, 1, 3, 5, 14, 16, 19, 20, 21, 22, 23, 24} {
		if !values[index].Truth() {
			t.Errorf("result[%d] = %s, want true", index, values[index].Describe())
		}
	}
	for _, index := range []int{2, 4, 6, 15, 18} {
		if values[index].Truth() {
			t.Errorf("result[%d] = %s, want false", index, values[index].Describe())
		}
	}
	if got := values[7].String(); got != "class sleep.engine.types.ListContainer" {
		t.Errorf("list class = %q, want Sleep ListContainer", got)
	}
	if got := values[8].String(); got != "class sleep.engine.types.ListContainer" {
		t.Errorf("listFiles class = %q, want Sleep ListContainer", got)
	}
	for _, index := range []int{9, 11} {
		assertPortableJavaFileNameSet(t, values[index], []string{"deep", "fresh.txt", "single"})
	}
	for _, index := range []int{10, 12} {
		assertPortableJavaFileObjectSet(t, values[index], root, []string{"deep", "fresh.txt", "single"})
	}
	if !values[13].IsNull() {
		t.Errorf("missing list = %s, want null", values[13].Describe())
	}
	if got := values[17].String(); got != filepath.FromSlash(filepath.ToSlash(fresh)) {
		t.Errorf("source path after rename = %q, want immutable %q", got, fresh)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("root after delete sequence: %v, want not exist", err)
	}
}

func assertPortableJavaFileNameSet(t *testing.T, value Value, want []string) {
	t.Helper()
	array, ok := value.Array()
	if !ok || array.Len() != len(want) {
		t.Fatalf("name array = %s, want %d entries", value.Describe(), len(want))
	}
	seen := make(map[string]bool, array.Len())
	for _, entry := range array.Values() {
		seen[entry.String()] = true
	}
	for _, name := range want {
		if !seen[name] {
			t.Errorf("name array = %s, missing %q", value.Describe(), name)
		}
	}
}

func assertPortableJavaFileObjectSet(t *testing.T, value Value, root string, want []string) {
	t.Helper()
	array, ok := value.Array()
	if !ok || array.Len() != len(want) {
		t.Fatalf("File array = %s, want %d entries", value.Describe(), len(want))
	}
	seen := make(map[string]bool, array.Len())
	for _, entry := range array.Values() {
		object, ok := entry.Object()
		if !ok {
			t.Errorf("File array entry = %s, want object", entry.Describe())
			continue
		}
		file, ok := object.(*portableJavaFile)
		if !ok || file == nil {
			t.Errorf("File array entry object = %T, want *portableJavaFile", object)
			continue
		}
		seen[file.path] = true
	}
	for _, name := range want {
		path := filepath.Join(root, name)
		if !seen[path] {
			t.Errorf("File array = %s, missing %q", value.Describe(), path)
		}
	}
}

func TestPortableJavaFileMutationErrorContracts(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "portable-java-file-effect-errors.sl", `
debug(0);
import java.io.File;
$empty = [new File: ''];
$empty_result = [$empty createNewFile];
$empty_error = checkError();
$file = [new File: 'missing'];
$null_result = [$file renameTo: $null];
$null_error = checkError();
$wrong_result = [$file renameTo: 'destination'];
$wrong_error = checkError();
$invalid = [new File: 'bad' . chr(0) . 'path'];
$invalid_result = [$invalid createNewFile];
$invalid_error = checkError();
return @(
    $empty_result, [$empty_error getClass],
    $null_result, [$null_error getClass],
    $wrong_result, $wrong_error,
    $invalid_result, [$invalid_error getClass],
    [$invalid delete], [$invalid mkdir], [$invalid mkdirs],
    [$invalid list], [$invalid listFiles]
);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok || array.Len() != 13 {
		t.Fatalf("result = %s, want 13 values", result.Describe())
	}
	values := array.Values()
	for _, index := range []int{0, 2, 4, 5, 6, 8, 9, 10, 11, 12} {
		if !values[index].IsNull() && values[index].Truth() {
			t.Errorf("result[%d] = %s, want null/false", index, values[index].Describe())
		}
	}
	if got := values[1].String(); got != "class java.io.IOException" {
		t.Errorf("empty create error = %q, want IOException", got)
	}
	if got := values[3].String(); got != "class java.lang.NullPointerException" {
		t.Errorf("null rename error = %q, want NullPointerException", got)
	}
	if got := values[7].String(); got != "class java.io.IOException" {
		t.Errorf("invalid create error = %q, want IOException", got)
	}
}

func TestPortableJavaFileEmptyPathOfficialJDKDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for empty java.io.File differential verification")
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
	for _, root := range []string{goRoot, javaRoot} {
		if err := os.WriteFile(filepath.Join(root, "entry"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(goRoot)
	var goOutput bytes.Buffer
	runtime, err := New(WithStdout(&goOutput), WithStderr(&goOutput))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Eval(context.Background(), "portable-java-file-empty-probe.sl", portableJavaFileEmptyProbeSource); err != nil {
		t.Fatalf("pure-Go empty File probe: %v\n%s", err, goOutput.String())
	}
	command := osexec.Command(java, "-jar", jar, "-e", portableJavaFileEmptyProbeSource)
	command.Dir = javaRoot
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official empty File probe: %v\n%s", err, javaOutput)
	}
	got := normalizePortableJavaFileRoot(goOutput.Bytes(), goRoot)
	want := normalizePortableJavaFileRoot(javaOutput, javaRoot)
	if !bytes.Equal(got, want) {
		t.Fatalf("official empty File output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

const portableJavaFileEmptyProbeSource = `
import java.io.File;
$file = [new File: ''];
println([$file getPath]);
println([$file getName]);
if ([$file getParent] is $null) { println(1); } else { println(0); }
println([$file getAbsolutePath]);
println([$file exists]);
println([$file isDirectory]);
println([$file isFile]);
println([$file canRead]);
println([$file canWrite]);
println([$file isHidden]);
if ([$file length] > 0) { println(1); } else { println(0); }
if ([$file lastModified] > 0) { println(1); } else { println(0); }
println([$file hashCode]);
`

func normalizePortableJavaFileRoot(output []byte, root string) []byte {
	normalized := filepath.ToSlash(string(output))
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		normalized = strings.ReplaceAll(normalized, filepath.ToSlash(resolved), "<ROOT>")
	}
	normalized = strings.ReplaceAll(normalized, filepath.ToSlash(root), "<ROOT>")
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	return []byte(normalized)
}

func TestPortableJavaFilePreservesImporterUnsupportedMethodOverride(t *testing.T) {
	root := t.TempDir()
	filePath := preparePortableJavaFileFixture(t, root)
	constructs, overrideCalls := 0, 0
	methodCalls := make(map[string]int)
	runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		switch {
		case invocation.Op == ObjectConstruct && invocation.Class == portableJavaFileClass:
			constructs++
		case invocation.Op == ObjectInvoke:
			methodCalls[invocation.Message]++
			switch invocation.Message {
			case "hashCode":
				return Int(77), nil
			case "mkdir":
				return Int(88), nil
			case "getCanonicalPath":
				overrideCalls++
				return String("importer:" + invocation.Target.String()), nil
			}
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	source := fmt.Sprintf(`
import java.io.File;
$file = [new File: %s];
return @(
    [$file exists], [$file isAbsolute], [$file getAbsoluteFile],
    [$file getParentFile], [$file equals: $file], [$file hashCode],
    [$file compareTo: $file], [$file getCanonicalPath], [$file mkdir]
);
`, strconv.Quote(filepath.ToSlash(filePath)))
	result, err := runtime.Eval(context.Background(), "file-importer-fallback.sl", source)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok || array.Len() != 9 {
		t.Fatalf("result = %s, want nine values", result.Describe())
	}
	values := array.Values()
	if !values[0].Truth() || !values[1].Truth() || !values[4].Truth() || values[5].Int32() != 77 || values[6].Int32() != 0 || values[7].String() != "importer:"+filepath.FromSlash(filepath.ToSlash(filePath)) || values[8].Int32() != 88 {
		t.Fatalf("result = %s, want portable methods plus importer hash/canonical path", result.Describe())
	}
	for _, message := range []string{"isAbsolute", "getAbsoluteFile", "getParentFile", "equals", "hashCode", "compareTo", "mkdir"} {
		if methodCalls[message] != 1 {
			t.Errorf("ObjectHost %s calls = %d, want 1", message, methodCalls[message])
		}
	}
	if constructs != 1 || methodCalls["exists"] != 1 || overrideCalls != 1 {
		t.Fatalf("ObjectHost calls = constructs:%d exists:%d override:%d, want 1 each", constructs, methodCalls["exists"], overrideCalls)
	}
}

// TestPortableJavaFileOfficialJDKDifferential runs identical Sleep source
// through OPFOR and the hash-pinned official Sleep JAR. The latter delegates
// these object expressions to the active JDK's java.io.File implementation.
func TestPortableJavaFileOfficialJDKDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for java.io.File differential verification")
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
	preparePortableJavaFileFixture(t, goRoot)
	preparePortableJavaFileFixture(t, javaRoot)
	goOutput := runPureGoJavaFileProbe(t, goRoot)
	command := osexec.Command(java, "-jar", jar, "-e", portableJavaFileProbeSource(javaRoot))
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep java.io.File probe: %v\n%s", err, javaOutput)
	}

	got := normalizePortableJavaFileProbe(goOutput, goRoot)
	want := normalizePortableJavaFileProbe(javaOutput, javaRoot)
	if !bytes.Equal(got, want) {
		t.Fatalf("official JDK java.io.File output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func preparePortableJavaFileFixture(t *testing.T, root string) string {
	t.Helper()
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	path := filepath.Join(root, "visible.txt")
	if err := os.WriteFile(path, []byte("opfor!"), 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.UnixMilli(portableJavaFileModified)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	return path
}

func runPureGoJavaFileProbe(t *testing.T, root string) []byte {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Eval(context.Background(), "portable-java-file-probe.sl", portableJavaFileProbeSource(root)); err != nil {
		t.Fatalf("pure-Go java.io.File probe: %v\n%s", err, output.String())
	}
	return output.Bytes()
}

func portableJavaFileProbeSource(root string) string {
	quotedRoot := strconv.Quote(filepath.ToSlash(root))
	quotedPath := strconv.Quote(filepath.ToSlash(filepath.Join(root, "visible.txt")))
	quotedParent := strconv.Quote(filepath.ToSlash(filepath.Join(root, "parent")))
	return strings.Join([]string{
		"import java.io.File;",
		"$root = " + quotedRoot + ";",
		"$path = " + quotedPath + ";",
		"$file = [new File: $path];",
		"println([$file exists]);",
		"println([$file isFile]);",
		"println([$file isDirectory]);",
		"println([$file canRead]);",
		"println([$file canWrite]);",
		"println([$file isHidden]);",
		"println([$file length]);",
		"println([$file lastModified]);",
		"println([$file getName]);",
		"println([$file getParent]);",
		"println([$file getPath]);",
		"println([$file getAbsolutePath]);",
		"println([$file toString]);",
		"println([[new File: $root, 'visible.txt'] getPath]);",
		"$parent = [new File: $root];",
		"println([[new File: $parent, 'visible.txt'] getPath]);",
		"println([[new File: $root, 'folder'] isDirectory]);",
		"println([[new File: $root, '.missing-hidden'] isHidden]);",
		"println([[new File: $root, 'missing'] exists]);",
		"println([[new File: $root, 'missing'] length]);",
		"println([[new File: $root, 'missing'] lastModified]);",
		"println([[new File: 'alpha/../leaf'] getPath]);",
		"println([[new File: 'alpha/../leaf'] getAbsolutePath]);",
		"println([[new File: '', 'leaf'] getPath]);",
		"$empty = [new File: ''];",
		"println([[new File: $empty, 'leaf'] getPath]);",
		"println([[new File: 123] getPath]);",
		"println([[new File: 456L] getPath]);",
		"println([[new File: 12.5] getPath]);",
		"println([[new File: $null, 'leaf'] getPath]);",
		"println([[new File: 123, 'leaf'] getPath]);",
		"println([[new File: 'parent', 456] getPath]);",
		"println([[new File: [new File: 'parent'], 789L] getPath]);",
		"println([[new File: " + quotedParent + ", '/leaf'] getPath]);",
		"println([$file getClass]);",
		"$same = $file;",
		"if ($same is $file) { println(1); } else { println(0); }",
		"$other = [new File: $path];",
		"if ($other is $file) { println(1); } else { println(0); }",
		"if ($file isa ^File) { println(1); } else { println(0); }",
		"if ($file isa ^Object) { println(1); } else { println(0); }",
		"if ($file isa ^java.io.Serializable) { println(1); } else { println(0); }",
		"if ($file isa ^java.lang.Comparable) { println(1); } else { println(0); }",
		"$relative = [new File: 'alpha/../leaf'];",
		"println([$relative isAbsolute]);",
		"println([$file isAbsolute]);",
		"$relative_absolute = [$relative getAbsoluteFile];",
		"println([$relative_absolute getPath]);",
		"println([$relative_absolute getClass]);",
		"$absolute_copy = [$file getAbsoluteFile];",
		"println([$absolute_copy getPath]);",
		"println([$absolute_copy equals: $file]);",
		"if ($absolute_copy is $file) { println(1); } else { println(0); }",
		"$parent_file = [$file getParentFile];",
		"println([$parent_file getPath]);",
		"println([$parent_file getClass]);",
		"$leaf = [new File: 'leaf'];",
		"println([$leaf getParentFile]);",
		"$root_file = [new File: '/'];",
		"println([$root_file isAbsolute]);",
		"println([$root_file getParentFile]);",
		"$numeric_file = [new File: 123];",
		"$numeric_string_file = [new File: '123'];",
		"println([$numeric_file equals: $numeric_string_file]);",
		"println([$numeric_file compareTo: $numeric_string_file]);",
		"println([$numeric_file equals: 123]);",
		"println([$numeric_file equals: $null]);",
		"println([[new File: 'alpha'] compareTo: [new File: 'beta']]);",
		"println([[new File: 'beta'] compareTo: [new File: 'alpha']]);",
		"println([[new File: 'alpha'] compareTo: [new File: 'alphabet']]);",
		"println([$numeric_file hashCode]);",
		"println([[new File: ''] hashCode]);",
		"println([[$file isAbsolute] getClass]);",
		"println([[$numeric_file equals: $numeric_string_file] getClass]);",
		"println([[$numeric_file hashCode] getClass]);",
		"println([[$numeric_file compareTo: $numeric_string_file] getClass]);",
		"$wrong_compare = [$relative compareTo: 'alpha'];",
		"$wrong_error = checkError();",
		"println($wrong_compare);",
		"println([$wrong_error getClass]);",
		"$null_compare = [$relative compareTo: $null];",
		"$null_error = checkError();",
		"println($null_compare);",
		"println([$null_error getClass]);",
	}, "\n") + "\n"
}

func normalizePortableJavaFileProbe(output []byte, root string) []byte {
	normalized := filepath.ToSlash(string(output))
	normalized = strings.ReplaceAll(normalized, filepath.ToSlash(root), "<ROOT>")
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	return []byte(normalized)
}
