package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"reflect"
	"testing"
)

func TestPortableJavaStringUnsupportedEncodingHierarchy(t *testing.T) {
	t.Parallel()

	exception := newPortableJavaException(errors.New("java.io.UnsupportedEncodingException: NO-SUCH"))
	for _, class := range []string{
		"java.io.UnsupportedEncodingException",
		"java.io.IOException",
		"java.lang.Exception",
		"java.lang.Throwable",
		"java.lang.Object",
	} {
		if !exception.isA(class) {
			t.Errorf("UnsupportedEncodingException isa %s = false", class)
		}
	}
	for _, class := range []string{"java.lang.RuntimeException", "java.net.SocketException"} {
		if exception.isA(class) {
			t.Errorf("UnsupportedEncodingException isa %s = true", class)
		}
	}
}

const portableJavaStringProbeSource = `
println([["ABC" toLowerCase] contains: "b"]);
println(["AbC" equalsIgnoreCase: "aBc"]);
println(["a😀b" indexOf: "b"]);
println(["a😀ba" lastIndexOf: "a"]);
println(["abcdef" startsWith: "bcd", 1]);
println(["abc" toUpperCase]);
println(["Straße" toUpperCase]);
println(["İ" toLowerCase]);
println(["ΟΣ" toLowerCase]);
println(["ΟΣΑ" toLowerCase]);
println(["𐐀" equalsIgnoreCase: "𐐨"]);
println(["𐐀" toLowerCase]);
println(["abc" indexOf: "", 99]);
println([[new String: " abc "] trim]);
$utf8 = ["é" getBytes: "UTF-8"];
println(strlen($utf8));
println(asc(charAt($utf8, 0)) . ":" . asc(charAt($utf8, 1)));
$utf16 = ["é😀" getBytes: "UTF-16LE"];
println(strlen($utf16));
$decoded = [new String: $utf16, "UTF-16LE"];
println(strlen($decoded));
println($decoded);
$onearg = [new String: $utf8];
println(strlen($onearg));
println(asc(charAt($onearg, 0)) . ":" . asc(charAt($onearg, 1)));
$unpaired = [chr(0xD800) getBytes: "UTF-8"];
println(strlen($unpaired) . ":" . asc($unpaired));
["abc" getBytes: "NO-SUCH"];
checkError($encode_error);
println($encode_error);
[new String: "abc", "NO-SUCH"];
checkError($decode_error);
println($decode_error);
`

const portableJavaStringProbeOutput = "1\n1\n3\n4\n1\nABC\nSTRASSE\ni̇\nος\nοσα\n1\n𐐨\n3\nabc\n2\n195:169\n6\n3\né😀\n" +
	"2\n195:169\n1:63\njava.io.UnsupportedEncodingException: NO-SUCH\n" +
	"java.io.UnsupportedEncodingException: NO-SUCH\n"

func TestPortableJavaStringAggressorMethodsExactOutput(t *testing.T) {
	t.Parallel()

	if got := runPortableJavaStringProbe(t); !bytes.Equal(got, []byte(portableJavaStringProbeOutput)) {
		t.Fatalf("portable String output mismatch\nwant:\n%sgot:\n%s", portableJavaStringProbeOutput, got)
	}
}

// TestPortableJavaStringAggressorMethodsOfficialJARDifferential is opt-in
// because the official BSD Sleep JAR is supplied separately. The pinned hash
// makes Java's java.lang.String behavior the compatibility oracle.
func TestPortableJavaStringAggressorMethodsOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for String differential verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	want, err := osexec.Command(java, "-jar", jar, "-e", portableJavaStringProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep String probe: %v\n%s", err, want)
	}
	got := runPortableJavaStringProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep String output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runPortableJavaStringProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Eval(context.Background(), "java-string-aggressor.sl", portableJavaStringProbeSource); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}

func TestPortableJavaStringUTF16Methods(t *testing.T) {
	t.Parallel()

	target := String("a😀b")
	character := invokePortableStringMethod(t, target, "charAt", Int(1))
	if got := sleepStringUnits(character); !reflect.DeepEqual(got, []uint16{0xd83d}) {
		t.Fatalf("charAt(1) units = %x, want [d83d]", got)
	}
	if got := invokePortableStringMethod(t, target, "codePointAt", Int(1)).Int32(); got != 0x1f600 {
		t.Fatalf("codePointAt(1) = %#x, want 0x1f600", got)
	}
	if got := invokePortableStringMethod(t, target, "codePointAt", Int(2)).Int32(); got != 0xde00 {
		t.Fatalf("codePointAt(2) = %#x, want 0xde00", got)
	}
	if got := invokePortableStringMethod(t, target, "codePointBefore", Int(3)).Int32(); got != 0x1f600 {
		t.Fatalf("codePointBefore(3) = %#x, want 0x1f600", got)
	}
	if got := invokePortableStringMethod(t, target, "codePointBefore", Int(2)).Int32(); got != 0xd83d {
		t.Fatalf("codePointBefore(2) = %#x, want 0xd83d", got)
	}
	for _, test := range []struct {
		start int32
		end   int32
		want  int32
	}{
		{start: 0, end: 4, want: 3},
		{start: 1, end: 3, want: 1},
		{start: 2, end: 3, want: 1},
	} {
		if got := invokePortableStringMethod(t, target, "codePointCount", Int(test.start), Int(test.end)).Int32(); got != test.want {
			t.Errorf("codePointCount(%d, %d) = %d, want %d", test.start, test.end, got, test.want)
		}
	}

	unpaired := sleepStringValueFromUnits([]uint16{0xd800, 'x', 0xdc00}, nil)
	if got := invokePortableStringMethod(t, unpaired, "codePointAt", Int(0)).Int32(); got != 0xd800 {
		t.Fatalf("unpaired codePointAt = %#x, want 0xd800", got)
	}
	if got := invokePortableStringMethod(t, unpaired, "codePointBefore", Int(3)).Int32(); got != 0xdc00 {
		t.Fatalf("unpaired codePointBefore = %#x, want 0xdc00", got)
	}
	if got := invokePortableStringMethod(t, unpaired, "codePointCount", Int(0), Int(3)).Int32(); got != 3 {
		t.Fatalf("unpaired codePointCount = %d, want 3", got)
	}
}

func TestPortableJavaStringTransformMethodsAndProvenance(t *testing.T) {
	t.Parallel()

	if got := invokePortableStringMethod(t, String("a😀"), "compareTo", String("a😁")).Int32(); got != -1 {
		t.Fatalf("supplementary compareTo = %d, want -1", got)
	}
	if got := invokePortableStringMethod(t, String("abc"), "compareTo", String("abcdef")).Int32(); got != -3 {
		t.Fatalf("prefix compareTo = %d, want -3", got)
	}

	binary := BinaryString([]byte{0xc3, 0xa9})
	concatenated := invokePortableStringMethod(t, binary, "concat", String("😀"))
	if got, want := sleepStringUnits(concatenated), []uint16{0xc3, 0xa9, 0xd83d, 0xde00}; !reflect.DeepEqual(got, want) {
		t.Fatalf("concat units = %x, want %x", got, want)
	}
	if got, want := sleepStringRawMask(concatenated), []bool{true, true, false, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("concat provenance = %v, want %v", got, want)
	}

	if got := invokePortableStringMethod(t, String(""), "isEmpty"); got.Int32() != 1 {
		t.Fatalf("empty isEmpty = %s, want true", got.Describe())
	}
	if got := invokePortableStringMethod(t, String("x"), "isEmpty"); got.Kind() != KindInt || got.Int32() != 0 {
		t.Fatalf("non-empty isEmpty = %s, want Java boolean integer 0", got.Describe())
	}
	if got := invokePortableStringMethod(t, String("\u2003\t"), "isBlank"); got.Int32() != 1 {
		t.Fatalf("Java whitespace isBlank = %s, want true", got.Describe())
	}
	if got := invokePortableStringMethod(t, String("\u00a0"), "isBlank"); got.Kind() != KindInt || got.Int32() != 0 {
		t.Fatalf("NBSP isBlank = %s, want Java boolean integer 0", got.Describe())
	}

	repeated := invokePortableStringMethod(t, BinaryString([]byte{0xc3}), "repeat", Int(3))
	if got, want := sleepStringUnits(repeated), []uint16{0xc3, 0xc3, 0xc3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("repeat units = %x, want %x", got, want)
	}
	if got, want := sleepStringRawMask(repeated), []bool{true, true, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("repeat provenance = %v, want %v", got, want)
	}

	if got := invokePortableStringMethod(t, String("aaa"), "replace", String("aa"), String("b")); got.String() != "ba" {
		t.Fatalf("replace overlap = %s, want ba", got.Describe())
	}
	emptyTarget := invokePortableStringMethod(t, String("😀"), "replace", String(""), String("-"))
	if got, want := sleepStringUnits(emptyTarget), []uint16{'-', 0xd83d, '-', 0xde00, '-'}; !reflect.DeepEqual(got, want) {
		t.Fatalf("empty-target replace units = %x, want %x", got, want)
	}
	rawReplacement := invokePortableStringMethod(t, BinaryString([]byte{'a', 0xff, 'a'}), "replace", String("a"), BinaryString([]byte{0xc3}))
	if got, want := sleepStringUnits(rawReplacement), []uint16{0xc3, 0xff, 0xc3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("raw replace units = %x, want %x", got, want)
	}
	if got, want := sleepStringRawMask(rawReplacement), []bool{true, true, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("raw replace provenance = %v, want %v", got, want)
	}
}

func TestPortableJavaStringUnicode17CaseMappings(t *testing.T) {
	t.Parallel()

	if got := invokePortableStringMethod(t, String("𐐀"), "equalsIgnoreCase", String("𐐨")); !got.Truth() {
		t.Fatalf("Deseret equalsIgnoreCase = %s, want true", got.Describe())
	}
	beriaUpper, beriaLower := string(rune(0x16ea0)), string(rune(0x16ebb))
	if got := invokePortableStringMethod(t, String(beriaUpper), "equalsIgnoreCase", String(beriaLower)); !got.Truth() {
		t.Fatalf("Unicode-17 Beria Erfe equalsIgnoreCase = %s, want true", got.Describe())
	}
	for _, test := range []struct {
		value  string
		method string
		want   string
	}{
		{value: "Straße", method: "toUpperCase", want: "STRASSE"},
		{value: "İ", method: "toLowerCase", want: "i\u0307"},
		{value: "ΟΣ", method: "toLowerCase", want: "ος"},
		{value: "ΟΣΑ", method: "toLowerCase", want: "οσα"},
		{value: "Σ", method: "toLowerCase", want: "σ"},
		{value: "AΣ\u0301", method: "toLowerCase", want: "aς\u0301"},
		{value: "AΣ\u0301B", method: "toLowerCase", want: "aσ\u0301b"},
		{value: "A Σ", method: "toLowerCase", want: "a σ"},
		{value: "𐐀", method: "toLowerCase", want: "𐐨"},
		{value: "𐐨", method: "toUpperCase", want: "𐐀"},
		{value: beriaUpper, method: "toLowerCase", want: beriaLower},
		{value: beriaLower, method: "toUpperCase", want: beriaUpper},
	} {
		if got := invokePortableStringMethod(t, String(test.value), test.method).String(); got != test.want {
			t.Errorf("%s(%q) = %q, want %q", test.method, test.value, got, test.want)
		}
	}

	raw := invokePortableStringMethod(t, BinaryString([]byte{0xff, 'A'}), "toLowerCase")
	if got, want := sleepStringUnits(raw), []uint16{0xff, 'a'}; !reflect.DeepEqual(got, want) {
		t.Fatalf("raw lowercase units = %x, want %x", got, want)
	}
	if got, want := sleepStringRawMask(raw), []bool{true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("raw lowercase provenance = %v, want %v", got, want)
	}
	unpaired := sleepStringValueFromUnits([]uint16{0xd800, 'A', 0xdc00}, []bool{false, true, false})
	mappedUnpaired := invokePortableStringMethod(t, unpaired, "toLowerCase")
	if got, want := sleepStringUnits(mappedUnpaired), []uint16{0xd800, 'a', 0xdc00}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unpaired lowercase units = %x, want %x", got, want)
	}
	if got, want := sleepStringRawMask(mappedUnpaired), []bool{false, false, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unpaired lowercase provenance = %v, want %v", got, want)
	}

	if got, want := javaStringDerivedCorePropertiesSHA256, "24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08"; got != want {
		t.Fatalf("DerivedCoreProperties digest = %q, want %q", got, want)
	}
}

func TestPortableJavaStringNullOverloadsAndRanges(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-null-ranges.sl", `
debug(0);
$concat = ["abc" concat: $null];
$concat_error = checkError();
$compare = ["abc" compareTo: $null];
$compare_error = checkError();
$contains = ["abc" contains: $null];
$contains_error = checkError();
$ends = ["abc" endsWith: $null];
$ends_error = checkError();
$index = ["abc" indexOf: $null];
$index_error = checkError();
$index_from = ["abc" indexOf: $null, 1];
$index_from_error = checkError();
$starts = ["abc" startsWith: $null];
$starts_error = checkError();
$bytes = ["abc" getBytes: $null];
$bytes_error = checkError();
$substring = ["abc" substring: 2, 1];
$substring_error = checkError();
$last_null_from = ["abc" lastIndexOf: $null, 1];
$last_null_from_error = checkError();
return @(
    $concat, [[$concat_error getClass] getName], [$concat_error getMessage],
    $compare, [[$compare_error getClass] getName], [$compare_error getMessage],
    $contains, [[$contains_error getClass] getName], [$contains_error getMessage],
    $ends, [[$ends_error getClass] getName], [$ends_error getMessage],
    $index, [[$index_error getClass] getName], [$index_error getMessage],
    $index_from, [[$index_from_error getClass] getName], [$index_from_error getMessage],
    $starts, [[$starts_error getClass] getName], [$starts_error getMessage],
    $bytes, [[$bytes_error getClass] getName], [$bytes_error getMessage],
    ["abc" indexOf: "", 99], ["abc" lastIndexOf: "", 99],
    $substring, [[$substring_error getClass] getName], [$substring_error getMessage],
    $last_null_from, $last_null_from_error
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	want := []string{
		"", "java.lang.NullPointerException", `Cannot invoke "String.isEmpty()" because "str" is null`,
		"", "java.lang.NullPointerException", `Cannot read field "value" because "anotherString" is null`,
		"", "java.lang.NullPointerException", `Cannot invoke "java.lang.CharSequence.toString()" because "s" is null`,
		"", "java.lang.NullPointerException", `Cannot invoke "String.length()" because "suffix" is null`,
		"", "java.lang.NullPointerException", `Cannot invoke "String.coder()" because "str" is null`,
		"", "java.lang.NullPointerException", `Cannot invoke "String.length()" because "tgtStr" is null`,
		"", "java.lang.NullPointerException", `Cannot invoke "String.length()" because "prefix" is null`,
		"", "java.lang.NullPointerException", "",
		"3", "3",
		"", "java.lang.StringIndexOutOfBoundsException", "Range [2, 1) out of bounds for length 3",
		"-1", "",
	}
	if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("String null/range results = %q, want %q", got, want)
	}
}

func TestPortableJavaStringStripAndToCharArray(t *testing.T) {
	t.Parallel()

	value := String("\u2003 \u00a0x\u2003")
	for _, test := range []struct {
		method string
		want   string
	}{
		{method: "strip", want: "\u00a0x"},
		{method: "stripLeading", want: "\u00a0x\u2003"},
		{method: "stripTrailing", want: "\u2003 \u00a0x"},
	} {
		if got := invokePortableStringMethod(t, value, test.method).String(); got != test.want {
			t.Errorf("%s = %q, want %q", test.method, got, test.want)
		}
	}
	rawStripped := invokePortableStringMethod(t, BinaryString([]byte{' ', 0xff, ' '}), "strip")
	if got, want := sleepStringUnits(rawStripped), []uint16{0xff}; !reflect.DeepEqual(got, want) {
		t.Fatalf("raw strip units = %x, want %x", got, want)
	}
	if got, want := sleepStringRawMask(rawStripped), []bool{true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("raw strip provenance = %v, want %v", got, want)
	}

	arrayValue := invokePortableStringMethod(t, String("A😀"), "toCharArray")
	if arrayValue.Kind() != KindString {
		t.Fatalf("toCharArray = %s, want Sleep scalar string", arrayValue.Describe())
	}
	if got, want := sleepStringUnits(arrayValue), []uint16{'A', 0xd83d, 0xde00}; !reflect.DeepEqual(got, want) {
		t.Fatalf("toCharArray units = %x, want %x", got, want)
	}
	if got, want := sleepStringRawMask(arrayValue), []bool{false, false, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("toCharArray raw provenance = %v, want %v", got, want)
	}
}

func TestPortableJavaStringErrorsAreSoftJavaExceptions(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-errors.sl", `
$char = ["abc" charAt: 3];
$char_error = checkError();
$before = ["abc" codePointBefore: 0];
$before_error = checkError();
$count = ["abc" codePointCount: 2, 1];
$count_error = checkError();
$repeat = ["abc" repeat: -1];
$repeat_error = checkError();
return @(
    $char, [[$char_error getClass] getName], [$char_error getMessage],
    $before, [[$before_error getClass] getName], [$before_error getMessage],
    $count, [[$count_error getClass] getName], [$count_error getMessage],
    $repeat, [[$repeat_error getClass] getName], [$repeat_error getMessage]
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	want := []string{
		"", "java.lang.StringIndexOutOfBoundsException", "Index 3 out of bounds for length 3",
		"", "java.lang.StringIndexOutOfBoundsException", "Index -1 out of bounds for length 3",
		"", "java.lang.IndexOutOfBoundsException", "Range [2, 1) out of bounds for length 3",
		"", "java.lang.IllegalArgumentException", "count is negative: -1",
	}
	if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("String soft errors = %q, want %q", got, want)
	}
}

func TestPortableJavaStringMethodsExecuteThroughSleep(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-methods.sl", `
$text = "a😀b";
$chars = [$text toCharArray];
$blank = chr(0x2003) . "\t";
$strip = chr(0x2003) . "x" . chr(0x2003);
return @(
    asc([$text charAt: 1]), [$text codePointAt: 1], [$text codePointBefore: 3],
    [$text codePointCount: 0, 4], ["a😀" compareTo: "a😁"],
    ["a" concat: "b"], ["" isEmpty], [$blank isBlank],
    ["ab" repeat: 2], ["aaa" replace: "aa", "b"], [$strip strip],
	strlen($chars), substr($chars, 0, 1), asc(substr($chars, 1, 2)), [$text charAt: 0]
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	want := []string{"55357", "128512", "128512", "3", "-1", "ab", "1", "1", "abab", "ba", "x", "4", "a", "55357", "a"}
	if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("Sleep String methods = %q, want %q", got, want)
	}
}

func TestPortableJavaStringStaticValueOfThroughSleep(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-static.sl", `
$chars = ["A😀" toCharArray];
$actual_chars = cast("A😀", "c");
return @(
    [String valueOf: $null], [String valueOf: "text"],
    [String valueOf: 42], [String valueOf: 3.5],
    [String valueOf: $chars], [String copyValueOf: $actual_chars]
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	want := []string{"null", "text", "42", "3.5", "A😀", "A😀"}
	if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("String static values = %q, want %q", got, want)
	}
}

func TestPortableJavaStringPreservesObjectHostPrecedence(t *testing.T) {
	t.Parallel()

	calls := 0
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		calls++
		if invocation.Target.Kind() == KindString && invocation.Message == "codePointAt" {
			return Int(777), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-host.sl", `return ["x" codePointAt: 0];`)
	if err != nil || value.Int32() != 777 || calls != 1 {
		t.Fatalf("ObjectHost override = (%s, %v, calls %d), want (777, nil, 1)", value.Describe(), err, calls)
	}

	staticCalls := 0
	staticRuntime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		staticCalls++
		if resolvePortableClassName(invocation.Class) == "java.lang.String" && invocation.Message == "valueOf" {
			return String("importer-static"), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = staticRuntime.Close(context.Background()) })
	value, err = staticRuntime.Eval(context.Background(), "java-string-static-host.sl", `return [String valueOf: 7];`)
	if err != nil || value.String() != "importer-static" || staticCalls != 1 {
		t.Fatalf("static ObjectHost override = (%s, %v, calls %d), want (importer-static, nil, 1)", value.Describe(), err, staticCalls)
	}
}

func invokePortableStringMethod(t *testing.T, target Value, method string, arguments ...Value) Value {
	t.Helper()
	invocation := ObjectInvocation{Op: ObjectInvoke, Target: target, Message: method}
	for _, argument := range arguments {
		invocation.Arguments = append(invocation.Arguments, Argument{Value: argument})
	}
	value, handled, err := portableString(invocation)
	if err != nil {
		t.Fatalf("%s(%v): %v", method, arguments, err)
	}
	if !handled {
		t.Fatalf("%s(%v) was not handled", method, arguments)
	}
	return value
}
