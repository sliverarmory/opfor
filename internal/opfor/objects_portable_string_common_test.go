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
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
)

const portableJavaStringCommonProbeSource = `
debug(0);
@words = @("alpha", $null, "😀");
println([String join: "|", @words]);
println([[new String: "abc"] intern]);
println(["A😀Z" subSequence: 1, 3]);
println(["😀" hashCode]);
$chars = cast("A😀Z", "c");
println([String valueOf: $chars, 1, 2]);
println([String copyValueOf: $chars, 0, 1]);
@raw = @("text", 2, $null);
println([String format: "%2\$s|%1\$-6.3s|%<S|%3\$b|%3\$h|%%", @raw]);
@typed_values = @(42, -7, 2.5);
$typed = cast(@typed_values, ^Object);
println([String format: "%1\$+06d|%2\$(5d|%3\$.2f", $typed]);
println(["%2\$s/%1\$s" formatted: @("left", "right")]);
@mixed_values = @(255, -1, 65, 1.25, $null, $null, $null, 2.675);
$mixed = cast(@mixed_values, ^Object);
println([String format: "%1\$#08x|%2\$x|%3\$c|%3\$C|%4\$.1e|%4\$g|%5\$08d|%6\$.2f|%7\$B|%8\$.2f|%4\$a|%4\$.3a", $mixed]);
@strings = @("abcdef", "x");
println([String format: "%1\$8.3s|%-5s|%<S|%n", @strings]);
$bad_conversion = [String format: "%d", @strings];
println(checkError());
`

const portableJavaStringCommonProbeOutput = "alpha|null|😀\n" +
	"abc\n" +
	"😀\n" +
	"1772899\n" +
	"😀\n" +
	"A\n" +
	"2|tex   |TEXT|false|null|%\n" +
	"+00042|  (7)|2.50\n" +
	"right/left\n" +
	"0x0000ff|ffffffff|A|A|1.3e+00|1.25000|    null|nu|FALSE|2.68|0x1.4p0|0x1.400p0\n" +
	"     abc|abcdef|ABCDEF|\n\n" +
	"java.util.IllegalFormatConversionException: d != java.lang.String\n"

func TestPortableJavaStringCommonMethodsExactOutput(t *testing.T) {
	t.Parallel()
	want := portableJavaStringCommonExpectedOutput()
	if got := runPortableJavaStringCommonProbe(t); !bytes.Equal(got, []byte(want)) {
		t.Fatalf("portable common String output mismatch\nwant bytes: %q\ngot bytes:  %q\nwant:\n%sgot:\n%s", want, got, want, got)
	}
}

func portableJavaStringCommonExpectedOutput() string {
	if goruntime.GOOS != "windows" {
		return portableJavaStringCommonProbeOutput
	}
	// Formatter's %n conversion uses the platform line separator. The Sleep
	// println surrounding that formatted value contributes the following LF.
	return strings.Replace(portableJavaStringCommonProbeOutput, "ABCDEF|\n\n", "ABCDEF|\r\n\n", 1)
}

func TestPortableJavaStringCommonMethodsOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for common String differential verification")
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
		java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaStringCommonProbeSource,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep common String probe: %v\n%s", err, want)
	}
	got := runPortableJavaStringCommonProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep common String mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runPortableJavaStringCommonProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-string-common.sl", portableJavaStringCommonProbeSource); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}

func TestPortableJavaStringCommonErrorsAndHierarchy(t *testing.T) {
	t.Parallel()
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-common-errors.sl", `
debug(0);
$chars = cast("abc", "c");
$bad_range = [String valueOf: $chars, 2, 2];
$range_error = checkError();
@raw_number = @("seed", 2);
$bad_format = [String format: "%2\$d", @raw_number];
$format_error = checkError();
$missing_format = [String format: "%2\$s", @("one")];
$missing_error = checkError();
return @(
    $bad_range, $range_error,
    $bad_format, [[$format_error getClass] getName],
    $format_error isa ^java.util.IllegalFormatConversionException,
    $format_error isa ^java.util.IllegalFormatException,
    $format_error isa ^java.lang.IllegalArgumentException,
    $missing_format, [[$missing_error getClass] getName]
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("common String errors = %s, want array", value.Describe())
	}
	want := []string{
		"", "java.lang.StringIndexOutOfBoundsException: Range [2, 2 + 2) out of bounds for length 3",
		"", "java.util.IllegalFormatConversionException", "1", "1", "1",
		"", "java.util.MissingFormatArgumentException",
	}
	if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("common String errors = %q, want %q", got, want)
	}
}

func TestPortableJavaStringCommonProvenanceBoundsAndConcurrency(t *testing.T) {
	t.Parallel()
	joined, handled, err := portableJavaStringStaticContext(context.Background(), ObjectInvocation{
		Op:      ObjectInvoke,
		Class:   "java.lang.String",
		Message: "join",
		Arguments: []Argument{
			{Value: BinaryString([]byte{'|', 0xff})},
			{Value: ArrayValue(NewArray(BinaryString([]byte{'a', 0xfe}), String("b")))},
		},
	})
	if err != nil || !handled {
		t.Fatalf("binary join = (handled:%v, error:%v)", handled, err)
	}
	if got, want := sleepStringRawMask(joined), []bool{true, true, true, true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("binary join raw mask = %v, want %v", got, want)
	}

	large := BinaryString(bytes.Repeat([]byte{'x'}, portableJavaStringNativeLoopChunk+1))
	invocation := ObjectInvocation{
		Op:      ObjectInvoke,
		Class:   "java.lang.String",
		Message: "join",
		Arguments: []Argument{
			{Value: String(",")},
			{Value: ArrayValue(NewArray(large, large))},
		},
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := portableJavaStringStaticContext(cancelled, invocation); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled join error = %v, want context.Canceled", err)
	}
	limitedRuntime, err := New(WithInstructionLimit(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limitedRuntime.Close(context.Background()) })
	limitedContext := withExecutionMeter(context.Background(), limitedRuntime)
	if _, _, err := portableJavaStringStaticContext(limitedContext, invocation); !errors.Is(err, ErrInstructionLimit) {
		t.Fatalf("instruction-limited join error = %v, want ErrInstructionLimit", err)
	}

	const workers = 24
	var wait sync.WaitGroup
	errorsByWorker := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, handled, err := portableJavaStringStaticContext(context.Background(), ObjectInvocation{
				Op:      ObjectInvoke,
				Class:   "java.lang.String",
				Message: "format",
				Arguments: []Argument{
					{Value: String("%2$s/%1$s")},
					{Value: ArrayValue(NewArray(String("left"), String("right")))},
				},
			})
			if err != nil || !handled || value.String() != "right/left" {
				errorsByWorker <- fmt.Errorf("format = (%s, handled:%v, error:%v)", value.Describe(), handled, err)
			}
		}()
	}
	wait.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		t.Error(err)
	}
}

func TestPortableJavaStringCommonObjectHostPrecedence(t *testing.T) {
	t.Parallel()
	var calls int
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		calls++
		if invocation.Message == "join" || invocation.Message == "formatted" {
			return String("importer"), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-common-host.sl", `
@items = @("a", "b");
return @([String join: ",", @items], ["%s" formatted: @items]);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, _ := value.Array()
	if got, want := argvValueStrings(array.Values()), []string{"importer", "importer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("String host results = %q, want %q", got, want)
	}
	if calls < 2 {
		t.Fatalf("ObjectHost calls = %d, want at least 2", calls)
	}
}
