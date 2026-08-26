package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	osexec "os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const portableJavaStringLinesProbeSource = `
debug(0);
$cr = chr(13);
$lf = chr(10);
$slash = chr(92);
$positive = [("a" . $cr . $lf . "\tb" . $cr . "c" . $lf) indent: 2];
println(unpack("H*", [$positive getBytes: "UTF-16BE"])[0]);
$negative = [("\t  a" . $lf . " \tb") indent: -2];
println(unpack("H*", [$negative getBytes: "UTF-16BE"])[0]);
$minimum = [("\t  a" . $lf . "  b") indent: (-2147483647 - 1)];
println(unpack("H*", [$minimum getBytes: "UTF-16BE"])[0]);
$stripped = [("  a  " . $cr . $lf . "    b  ") stripIndent];
println(unpack("H*", [$stripped getBytes: "UTF-16BE"])[0]);
$terminal = [("  a  " . $cr . $lf . "    b  " . $cr . $lf) stripIndent];
println(unpack("H*", [$terminal getBytes: "UTF-16BE"])[0]);
$unicode_space = [(chr(0x2003) . "x" . chr(0x2003)) stripIndent];
println(unpack("H*", [$unicode_space getBytes: "UTF-16BE"])[0]);
$translated = [(
    "A" . $slash . "b" . $slash . "f" . $slash . "n" . $slash . "r" .
    $slash . "s" . $slash . "t" . $slash . chr(39) . $slash . chr(34) .
    $slash . $slash . $slash . "141" . $slash . "400" . $slash . "77" .
    $slash . "08" . $slash . $cr . $lf . "C" . $slash . $lf . "D"
) translateEscapes];
println(unpack("H*", [$translated getBytes: "UTF-16BE"])[0]);
$bad = [($slash . "u0041") translateEscapes];
println(checkError());
`

const portableJavaStringLinesProbeOutput = "002000200061000a0020002000090062000a002000200063000a\n" +
	"00200061000a0062000a\n" +
	"0061000a0062000a\n" +
	"0061000a002000200062\n" +
	"002000200061000a00200020002000200062000a\n" +
	"0078\n" +
	"00410008000c000a000d0020000900270022005c006100200030003f0000003800430044\n" +
	"java.lang.IllegalArgumentException: Invalid escape sequence: \\u \\\\u0075\n"

func TestPortableJavaStringLineAndEscapeMethodsExactOutput(t *testing.T) {
	t.Parallel()
	if got := runPortableJavaStringLinesProbe(t); !bytes.Equal(got, []byte(portableJavaStringLinesProbeOutput)) {
		t.Fatalf("portable line/escape String output mismatch\nwant:\n%sgot:\n%s", portableJavaStringLinesProbeOutput, got)
	}
}

func TestPortableJavaStringLineAndEscapeMethodsOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for line/escape String differential verification")
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
	want, err := osexec.Command(
		java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaStringLinesProbeSource,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep line/escape String probe: %v\n%s", err, want)
	}
	got := runPortableJavaStringLinesProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep line/escape String mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runPortableJavaStringLinesProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-string-lines.sl", portableJavaStringLinesProbeSource); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}

func TestPortableJavaStringIndentUTF16AndProvenance(t *testing.T) {
	t.Parallel()
	target := sleepStringValueFromUnits(
		[]uint16{'a', '\r', '\n', 0xff},
		[]bool{true, true, true, true},
	)
	got := invokePortableStringMethod(t, target, "indent", Int(1))
	if units, want := sleepStringUnits(got), []uint16{' ', 'a', '\n', ' ', 0xff, '\n'}; !reflect.DeepEqual(units, want) {
		t.Fatalf("indent units = %x, want %x", units, want)
	}
	if raw, want := sleepStringRawMask(got), []bool{false, true, false, false, true, false}; !reflect.DeepEqual(raw, want) {
		t.Fatalf("indent provenance = %v, want %v", raw, want)
	}
	for _, test := range []struct {
		input string
		count int32
		want  []uint16
	}{
		{input: "", count: 3, want: []uint16{}},
		{input: "a", count: 0, want: []uint16{'a', '\n'}},
		{input: "a\r\nb\rc\n", count: 0, want: []uint16{'a', '\n', 'b', '\n', 'c', '\n'}},
		{input: "\n\n", count: -1, want: []uint16{'\n', '\n'}},
		{input: "  x", count: -1, want: []uint16{' ', 'x', '\n'}},
		{input: "\u00a0x", count: -1, want: []uint16{0x00a0, 'x', '\n'}},
	} {
		value := invokePortableStringMethod(t, String(test.input), "indent", Int(test.count))
		if units := sleepStringUnits(value); !reflect.DeepEqual(units, test.want) {
			t.Errorf("indent(%q, %d) units = %x, want %x", test.input, test.count, units, test.want)
		}
	}

	minimum := invokePortableStringMethod(
		t,
		String("\u2003\t x\r\n  y"),
		"indent",
		Int(math.MinInt32),
	)
	if units, want := sleepStringUnits(minimum), []uint16{'x', '\n', 'y', '\n'}; !reflect.DeepEqual(units, want) {
		t.Fatalf("minimum indent units = %x, want %x", units, want)
	}

	invocation := stringMethodInvocation(String("x"), "indent", Int(math.MaxInt32))
	if _, handled, err := portableStringContext(context.Background(), invocation); !handled || err == nil || err.Error() != "java.lang.OutOfMemoryError: Required length exceeds implementation limit" {
		t.Fatalf("overflow indent = (handled:%v, %v)", handled, err)
	}
}

func TestPortableJavaStringStripIndentUTF16AndProvenance(t *testing.T) {
	t.Parallel()
	target := BinaryString([]byte{' ', ' ', 'a', ' ', '\r', '\n', ' ', ' ', ' ', ' ', 'b', ' '})
	got := invokePortableStringMethod(t, target, "stripIndent")
	if units, want := sleepStringUnits(got), []uint16{'a', '\n', ' ', ' ', 'b'}; !reflect.DeepEqual(units, want) {
		t.Fatalf("stripIndent units = %x, want %x", units, want)
	}
	if raw, want := sleepStringRawMask(got), []bool{true, false, true, true, true}; !reflect.DeepEqual(raw, want) {
		t.Fatalf("stripIndent provenance = %v, want %v", raw, want)
	}

	terminal := BinaryString([]byte{' ', 'a', ' ', '\r', '\n', ' ', ' ', 'b', ' ', '\n'})
	got = invokePortableStringMethod(t, terminal, "stripIndent")
	if units, want := sleepStringUnits(got), []uint16{' ', 'a', '\n', ' ', ' ', 'b', '\n'}; !reflect.DeepEqual(units, want) {
		t.Fatalf("terminal stripIndent units = %x, want %x", units, want)
	}
	if raw, want := sleepStringRawMask(got), []bool{true, true, false, true, true, true, false}; !reflect.DeepEqual(raw, want) {
		t.Fatalf("terminal stripIndent provenance = %v, want %v", raw, want)
	}

	for _, test := range []struct {
		input string
		want  []uint16
	}{
		{input: "", want: []uint16{}},
		{input: "   ", want: []uint16{}},
		{input: " \n  ", want: []uint16{'\n'}},
		{input: "\r\n", want: []uint16{'\n'}},
		{input: "\n\n", want: []uint16{'\n', '\n'}},
	} {
		value := invokePortableStringMethod(t, String(test.input), "stripIndent")
		if units := sleepStringUnits(value); !reflect.DeepEqual(units, test.want) {
			t.Errorf("stripIndent(%q) units = %x, want %x", test.input, units, test.want)
		}
	}
}

func TestPortableJavaStringTranslateEscapesUTF16ProvenanceAndErrors(t *testing.T) {
	t.Parallel()
	target := BinaryString([]byte{0xff, 'A', '\\', 'n', '\\', '1', '4', '1'})
	got := invokePortableStringMethod(t, target, "translateEscapes")
	if units, want := sleepStringUnits(got), []uint16{0xff, 'A', '\n', 'a'}; !reflect.DeepEqual(units, want) {
		t.Fatalf("translateEscapes units = %x, want %x", units, want)
	}
	if raw, want := sleepStringRawMask(got), []bool{true, true, false, false}; !reflect.DeepEqual(raw, want) {
		t.Fatalf("translateEscapes provenance = %v, want %v", raw, want)
	}

	untouched := BinaryString([]byte{'a', 0xff})
	if got := invokePortableStringMethod(t, untouched, "translateEscapes"); !got.IdentityEqual(untouched) {
		t.Fatalf("unchanged translateEscapes = %s, want exact receiver", got.Describe())
	}

	octal := invokePortableStringMethod(t, String("\\377\\400\\77\\08\\s\\\r\nZ"), "translateEscapes")
	if units, want := sleepStringUnits(octal), []uint16{0xff, ' ', '0', '?', 0, '8', ' ', 'Z'}; !reflect.DeepEqual(units, want) {
		t.Fatalf("octal/continuation translation units = %x, want %x", units, want)
	}

	for _, test := range []struct {
		name  string
		input Value
		want  string
	}{
		{name: "unsupported", input: String(`\q`), want: `java.lang.IllegalArgumentException: Invalid escape sequence: \q \\u0071`},
		{name: "unicode", input: String(`\u0041`), want: `java.lang.IllegalArgumentException: Invalid escape sequence: \u \\u0075`},
		{name: "trailing", input: String(`\`), want: "java.lang.IllegalArgumentException: Invalid escape sequence: \\" + string(rune(0)) + " \\\\u0000"},
	} {
		t.Run(test.name, func(t *testing.T) {
			invocation := stringMethodInvocation(test.input, "translateEscapes")
			_, handled, err := portableStringContext(context.Background(), invocation)
			if !handled || err == nil || err.Error() != test.want {
				t.Fatalf("translateEscapes error = (handled:%v, %q), want %q", handled, err, test.want)
			}
		})
	}
}

func TestPortableJavaStringTranslateEscapesSoftErrorHierarchy(t *testing.T) {
	t.Parallel()
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-escape-error.sl", `
debug(0);
$bad = [(chr(92) . "u0041") translateEscapes];
$problem = checkError();
return @(
    $bad,
    [$problem getMessage],
    [[$problem getClass] getName],
    $problem isa ^java.lang.IllegalArgumentException,
    $problem isa ^java.lang.RuntimeException
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("soft-error result = %s, want array", value.Describe())
	}
	want := []string{
		"",
		`Invalid escape sequence: \u \\u0075`,
		"java.lang.IllegalArgumentException",
		"1",
		"1",
	}
	if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("soft-error result = %q, want %q", got, want)
	}
}

func TestPortableJavaStringLineAndEscapeMethodArityAndObjectHostPrecedence(t *testing.T) {
	t.Parallel()
	for _, invocation := range []ObjectInvocation{
		stringMethodInvocation(String("x"), "indent"),
		stringMethodInvocation(String("x"), "stripIndent", Int(1)),
		stringMethodInvocation(String("x"), "translateEscapes", Int(1)),
	} {
		value, handled, err := portableStringContext(context.Background(), invocation)
		if !handled || err != nil || !value.IsNull() {
			t.Errorf("%s wrong arity = (%s, handled:%v, %v)", invocation.Message, value.Describe(), handled, err)
		}
	}

	calls := 0
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		calls++
		if invocation.Target.Kind() == KindString && invocation.Message == "indent" {
			return String("importer-indent"), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-indent-host.sl", `return ["x" indent: 1];`)
	if err != nil || value.String() != "importer-indent" || calls != 1 {
		t.Fatalf("ObjectHost indent override = (%s, %v, calls:%d)", value.Describe(), err, calls)
	}
}

func TestPortableJavaStringLineAndEscapeMethodsCancellationAndInstructionLimit(t *testing.T) {
	large := String(strings.Repeat("x", portableJavaStringNativeLoopChunk*6+1))
	cases := []ObjectInvocation{
		stringMethodInvocation(large, "indent", Int(0)),
		stringMethodInvocation(large, "stripIndent"),
		stringMethodInvocation(large, "translateEscapes"),
	}
	for _, invocation := range cases {
		t.Run(invocation.Message+"-cancellation", func(t *testing.T) {
			ctx := newPortableStringCheckCancelContext(5)
			_, handled, err := portableStringContext(ctx, invocation)
			if !handled || !errors.Is(err, context.Canceled) {
				t.Fatalf("native-loop cancellation = (handled:%v, %v), want context.Canceled", handled, err)
			}
		})
		t.Run(invocation.Message+"-instruction-limit", func(t *testing.T) {
			runtimeInstance, err := New(WithInstructionLimit(1))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			ctx := withExecutionMeter(context.Background(), runtimeInstance)
			_, handled, err := portableStringContext(ctx, invocation)
			if !handled || !errors.Is(err, ErrInstructionLimit) {
				t.Fatalf("native-loop limit = (handled:%v, %v), want ErrInstructionLimit", handled, err)
			}
		})
	}
}

func TestPortableJavaStringLineAndEscapeMethodsConcurrentUse(t *testing.T) {
	t.Parallel()
	const workers = 16
	var wait sync.WaitGroup
	errorsByWorker := make(chan error, workers)
	target := BinaryString([]byte{' ', ' ', 'a', ' ', '\r', '\n', ' ', ' ', ' ', ' ', 'b', ' '})
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			stripped, handled, err := portableStringContext(
				context.Background(), stringMethodInvocation(target, "stripIndent"),
			)
			if err != nil || !handled || !reflect.DeepEqual(sleepStringUnits(stripped), []uint16{'a', '\n', ' ', ' ', 'b'}) {
				errorsByWorker <- fmt.Errorf("worker %d stripIndent = (%x, handled:%v, %v)", worker, sleepStringUnits(stripped), handled, err)
				return
			}
			translated, handled, err := portableStringContext(
				context.Background(), stringMethodInvocation(String(`a\tb`), "translateEscapes"),
			)
			if err != nil || !handled || translated.String() != "a\tb" {
				errorsByWorker <- fmt.Errorf("worker %d translateEscapes = (%q, handled:%v, %v)", worker, translated.String(), handled, err)
			}
		}(worker)
	}
	wait.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		t.Error(err)
	}
}
