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
	"strings"
	"sync"
	"testing"
)

const portableJavaStringModernProbeSource = `
debug(0);
import java.util.Locale;
import java.util.stream.*;
import java.util.function.Function;

$cr = chr(13); $lf = chr(10);
$text = "a" . $cr . $lf . "b" . $cr . "c" . $lf . $lf . "d";
$lines = [$text lines];
println([$lines getClass]);
println([$lines isParallel]);
[$lines parallel]; println([$lines isParallel]);
[$lines sequential]; println([$lines isParallel]);
$unordered = [$lines unordered];
println([$unordered count]);
$reused = [$lines count]; println(checkError());
$line_array = [[$text lines] toArray];
println(size($line_array) . "/" . join("|", $line_array));
$line_iterator = [[$text lines] iterator];
while ([$line_iterator hasNext]) { println("line=" . [$line_iterator next]); }
$missing_line = [$line_iterator next]; println(checkError());
$removed_line = [$line_iterator remove]; println(checkError());

$chars = ["A😀Z" chars];
println([$chars getClass]);
println([$chars sum]);
println(join(",", [["A😀Z" chars] toArray]));
$char_iterator = [["A😀Z" chars] iterator];
while ([$char_iterator hasNext]) { println("char=" . [$char_iterator nextInt]); }
$missing_char = [$char_iterator nextInt]; println(checkError());
println(join(",", [["A😀Z" codePoints] toArray]));

$plain = ["abc" transform: { return "<" . $1 . ">/" . $0; }];
println($plain);
$function = newInstance(^Function, { return "[" . $1 . "]/" . $0; });
println(["abc" transform: $function]);
$null_transform = ["abc" transform: $null]; println(checkError());

$root = [Locale ROOT];
$english = [Locale ENGLISH];
$turkish = [new Locale: "TR"];
$azeri = [new Locale: "AZ"];
$lithuanian = [new Locale: "LT"];
$posix = [new Locale: "EN", "us", "Posix"];
println([$root toLanguageTag] . "/" . [$english toLanguageTag] . "/" . [$turkish toLanguageTag] . "/" . [$posix toString] . "/" . [$posix toLanguageTag]);
println([$turkish getLanguage] . "/" . [$posix getCountry] . "/" . [$posix getVariant] . "/" . [$turkish hashCode] . "/" . [$turkish equals: [new Locale: "tr"]]);
println(["IİΣ" toLowerCase] . "/" . ["IİΣ" toLowerCase: $root] . "/" . ["IİΣ" toLowerCase: $turkish] . "/" . ["IİΣ" toLowerCase: $azeri]);
println(["iıß" toUpperCase] . "/" . ["iıß" toUpperCase: $root] . "/" . ["iıß" toUpperCase: $turkish]);
$above = chr(0x301);
println([("I" . $above) toLowerCase: $lithuanian] . "/" . [("J" . $above) toLowerCase: $lithuanian] . "/" . [("Į" . $above) toLowerCase: $lithuanian]);
println(["Ì" toLowerCase: $lithuanian] . "/" . ["Í" toLowerCase: $lithuanian] . "/" . ["Ĩ" toLowerCase: $lithuanian]);
println([("i" . "̇") toUpperCase: $lithuanian]);
$null_case = ["I" toLowerCase: $null]; println(checkError());
`

const portableJavaStringModernProbeOutput = "class java.util.stream.ReferencePipeline$Head\n" +
	"0\n1\n0\n5\n" +
	"java.lang.IllegalStateException: stream has already been operated upon or closed\n" +
	"5/a|b|c||d\n" +
	"line=a\nline=b\nline=c\nline=\nline=d\n" +
	"java.util.NoSuchElementException\n" +
	"java.lang.UnsupportedOperationException: remove\n" +
	"class java.util.stream.IntPipeline$Head\n" +
	"112344\n65,55357,56832,90\n" +
	"char=65\nchar=55357\nchar=56832\nchar=90\n" +
	"java.util.NoSuchElementException\n65,128512,90\n" +
	"<abc>/apply\n[abc]/apply\n" +
	"java.lang.NullPointerException: Cannot invoke \"java.util.function.Function.apply(Object)\" because \"f\" is null\n" +
	"und/en/tr/en_US_Posix/en-US-Posix\n" +
	"tr/US/Posix/3710/1\n" +
	"ii̇ς/ii̇ς/ıiς/ıiς\n" +
	"IISS/IISS/İISS\n" +
	"i̇́/j̇́/į̇́\n" +
	"i̇̀/i̇́/i̇̃\n" +
	"I\njava.lang.NullPointerException\n"

func TestPortableJavaStringModernMethodsExactOutput(t *testing.T) {
	t.Parallel()
	if got := runPortableJavaStringModernProbe(t); !bytes.Equal(got, []byte(portableJavaStringModernProbeOutput)) {
		t.Fatalf("portable modern String output mismatch\nwant:\n%sgot:\n%s", portableJavaStringModernProbeOutput, got)
	}
}

func TestPortableJavaStringModernMethodsOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for modern String differential verification")
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
		java,
		"--add-opens=java.base/java.util.stream=ALL-UNNAMED",
		"--add-opens=java.base/java.util=ALL-UNNAMED",
		"-Dfile.encoding=UTF-8",
		"-Duser.language=en",
		"-Duser.country=US",
		"-jar", jar, "-e", portableJavaStringModernProbeSource,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep modern String probe: %v\n%s", err, want)
	}
	got := runPortableJavaStringModernProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep modern String mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runPortableJavaStringModernProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-string-modern.sl", portableJavaStringModernProbeSource); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}

func TestPortableJavaStringStreamUTF16ProvenanceAndTypes(t *testing.T) {
	t.Parallel()
	target := sleepStringValueFromUnits(
		[]uint16{'a', '\r', '\n', 0xff, '\n', 0xd83d, 0xde00, 0xd800},
		[]bool{true, true, true, true, true, false, false, true},
	)

	lines := portableStringStreamForTest(t, target, "lines")
	lineArray := portableStringStreamCallForTest(t, lines, "toArray")
	array, ok := lineArray.Array()
	if !ok {
		t.Fatalf("lines toArray = %s, want Sleep array", lineArray.Describe())
	}
	values := array.Values()
	if len(values) != 3 {
		t.Fatalf("line count = %d, want 3", len(values))
	}
	if units, want := sleepStringUnits(values[0]), []uint16{'a'}; !reflect.DeepEqual(units, want) {
		t.Fatalf("first line units = %x, want %x", units, want)
	}
	if raw, want := sleepStringRawMask(values[1]), []bool{true}; !reflect.DeepEqual(raw, want) {
		t.Fatalf("second line provenance = %v, want %v", raw, want)
	}
	if units, want := sleepStringUnits(values[2]), []uint16{0xd83d, 0xde00, 0xd800}; !reflect.DeepEqual(units, want) {
		t.Fatalf("third line units = %x, want %x", units, want)
	}

	chars := portableStringStreamForTest(t, target, "chars")
	charArray := portableStringStreamCallForTest(t, chars, "toArray")
	charValues, _ := charArray.Array()
	if got, want := argvValueStrings(charValues.Values()), []string{"97", "13", "10", "255", "10", "55357", "56832", "55296"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chars = %v, want %v", got, want)
	}

	points := portableStringStreamForTest(t, target, "codePoints")
	pointArray := portableStringStreamCallForTest(t, points, "toArray")
	pointValues, _ := pointArray.Array()
	if got, want := argvValueStrings(pointValues.Values()), []string{"97", "13", "10", "255", "10", "128512", "55296"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("codePoints = %v, want %v", got, want)
	}

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Eval(context.Background(), "java-string-stream-types.sl", `
import java.util.stream.*;
$reference = ["x" lines];
$ints = ["x" chars];
$iterator = [$ints iterator];
return @(
    $reference isa ^Stream,
    $reference isa ^BaseStream,
    $ints isa ^IntStream,
    $ints isa ^BaseStream,
    $iterator isa ^Iterator,
    $iterator isa ^PrimitiveIterator,
    $iterator isa ^PrimitiveIterator$OfInt
);
`)
	if err != nil {
		t.Fatal(err)
	}
	types, _ := result.Array()
	if got, want := argvValueStrings(types.Values()), []string{"1", "1", "1", "1", "1", "1", "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stream types = %v, want %v", got, want)
	}
}

func portableStringStreamForTest(t *testing.T, target Value, method string) Value {
	t.Helper()
	value, handled, err := portableStringContext(context.Background(), stringMethodInvocation(target, method))
	if err != nil || !handled {
		t.Fatalf("%s stream = (%s, handled:%v, error:%v)", method, value.Describe(), handled, err)
	}
	return value
}

func portableStringStreamCallForTest(t *testing.T, stream Value, method string) Value {
	t.Helper()
	object, ok := stream.Object()
	if !ok {
		t.Fatalf("stream = %s, want object", stream.Describe())
	}
	value, handled, err := object.(*portableJavaStringStream).invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Target: stream, Message: method,
	})
	if err != nil || !handled {
		t.Fatalf("stream %s = (%s, handled:%v, error:%v)", method, value.Describe(), handled, err)
	}
	return value
}

func TestPortableJavaStringStreamCancellationInstructionLimitAndSingleUse(t *testing.T) {
	large := String(strings.Repeat("x", portableJavaStringNativeLoopChunk*6+1))
	for _, method := range []string{"lines", "chars", "codePoints"} {
		t.Run(method+"-cancellation", func(t *testing.T) {
			streamValue := portableStringStreamForTest(t, large, method)
			streamObject, _ := streamValue.Object()
			stream := streamObject.(*portableJavaStringStream)
			invocation := ObjectInvocation{Op: ObjectInvoke, Target: streamValue, Message: "count"}
			_, handled, err := stream.invoke(newPortableStringCheckCancelContext(5), invocation)
			if !handled || !errors.Is(err, context.Canceled) {
				t.Fatalf("stream cancellation = (handled:%v, %v), want context.Canceled", handled, err)
			}
			_, handled, err = stream.invoke(context.Background(), invocation)
			if !handled || err == nil || err.Error() != "java.lang.IllegalStateException: stream has already been operated upon or closed" {
				t.Fatalf("failed terminal reuse = (handled:%v, %v)", handled, err)
			}
		})
		t.Run(method+"-instruction-limit", func(t *testing.T) {
			runtimeInstance, err := New(WithInstructionLimit(1))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			streamValue := portableStringStreamForTest(t, large, method)
			streamObject, _ := streamValue.Object()
			_, handled, err := streamObject.(*portableJavaStringStream).invoke(
				withExecutionMeter(context.Background(), runtimeInstance),
				ObjectInvocation{Op: ObjectInvoke, Target: streamValue, Message: "count"},
			)
			if !handled || !errors.Is(err, ErrInstructionLimit) {
				t.Fatalf("stream limit = (handled:%v, %v), want ErrInstructionLimit", handled, err)
			}
		})
	}
}

func TestPortableJavaStringStreamConcurrentTerminalIsRaceSafe(t *testing.T) {
	t.Parallel()
	streamValue := portableStringStreamForTest(t, String(strings.Repeat("x", 1024)), "chars")
	streamObject, _ := streamValue.Object()
	stream := streamObject.(*portableJavaStringStream)
	const workers = 24
	var wait sync.WaitGroup
	results := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, handled, err := stream.invoke(context.Background(), ObjectInvocation{
				Op: ObjectInvoke, Target: streamValue, Message: "count",
			})
			if !handled {
				results <- errors.New("terminal was not handled")
				return
			}
			if err == nil && value.Int64() != 1024 {
				results <- fmt.Errorf("terminal count = %s", value.Describe())
				return
			}
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if err.Error() != "java.lang.IllegalStateException: stream has already been operated upon or closed" {
			t.Errorf("concurrent terminal error = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful terminals = %d, want 1", successes)
	}
}

func TestPortableJavaStringTransformIdentityUnsupportedAndObjectHostPrecedence(t *testing.T) {
	t.Parallel()
	compound := ArrayValue(NewArray(String("kept")))
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Target.Kind() == KindString && invocation.Message == "lines" {
			return compound, nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-transform-identity.sl", `
debug(0);
import java.util.function.Function;
$kept = @("value");
$result = ["abc" transform: { return $kept; }];
$wrong = newInstance(^Runnable, { return 1; });
$unsupported = ["abc" transform: $wrong];
return @($result, $kept, ["abc" lines], $unsupported, checkError());
`)
	if err != nil {
		t.Fatal(err)
	}
	array, _ := value.Array()
	values := array.Values()
	transformed, _ := values[0].Array()
	if got := argvValueStrings(transformed.Values()); !reflect.DeepEqual(got, []string{"value"}) {
		t.Fatalf("transform compound = %v", got)
	}
	if !values[0].IdentityEqual(values[1]) {
		t.Fatalf("transform result = %s, want exact closure result", values[0].Describe())
	}
	if !values[2].IdentityEqual(compound) {
		t.Fatalf("ObjectHost lines = %s, want exact importer value", values[2].Describe())
	}
	if !values[3].IsNull() || !values[4].IsNull() {
		t.Fatalf("unsupported Function proxy = (%s, %s), want soft no-match", values[3].Describe(), values[4].Describe())
	}

	streamCalls := 0
	streamRuntime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if object, ok := invocation.Target.Object(); ok {
			if _, ok := object.(*portableJavaStringStream); ok && invocation.Message == "count" {
				streamCalls++
				return Long(777), nil
			}
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = streamRuntime.Close(context.Background()) })
	streamResult, err := streamRuntime.Eval(context.Background(), "java-string-stream-host.sl", `return [["abc" chars] count];`)
	if err != nil || streamResult.Int64() != 777 || streamCalls != 1 {
		t.Fatalf("ObjectHost stream count = (%s, %v, calls:%d), want (777, nil, 1)", streamResult.Describe(), err, streamCalls)
	}
}

func TestPortableJavaStringLocaleCaseProvenanceAndUnicodePins(t *testing.T) {
	t.Parallel()
	if javaStringLocaleUnicodeDataSHA256 != "2e1efc1dcb59c575eedf5ccae60f95229f706ee6d031835247d843c11d96470c" ||
		javaStringLocalePropListSHA256 != "130dcddcaadaf071008bdfce1e7743e04fdfbc910886f017d9f9ac931d8c64dd" {
		t.Fatal("locale Unicode provenance digests changed")
	}

	turkish := ObjectValue(&portableJavaLocale{language: "tr"})
	target := sleepStringValueFromUnits([]uint16{'I', 0x0307, 0xff}, []bool{true, true, true})
	value, handled, err := portableStringContext(context.Background(), stringMethodInvocation(target, "toLowerCase", turkish))
	if err != nil || !handled {
		t.Fatalf("Turkish lower = (handled:%v, error:%v)", handled, err)
	}
	if units, want := sleepStringUnits(value), []uint16{'i', 0xff}; !reflect.DeepEqual(units, want) {
		t.Fatalf("Turkish lower units = %x, want %x", units, want)
	}
	if raw, want := sleepStringRawMask(value), []bool{false, true}; !reflect.DeepEqual(raw, want) {
		t.Fatalf("Turkish lower provenance = %v, want %v", raw, want)
	}

	unchanged := BinaryString([]byte{'1', '?'})
	value, handled, err = portableStringContext(context.Background(), stringMethodInvocation(unchanged, "toUpperCase", turkish))
	if err != nil || !handled || !value.IdentityEqual(unchanged) {
		t.Fatalf("unchanged locale case = (%s, handled:%v, error:%v), want exact receiver", value.Describe(), handled, err)
	}

	limitedRuntime, err := New(WithInstructionLimit(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limitedRuntime.Close(context.Background()) })
	large := String(strings.Repeat("I", portableJavaStringNativeLoopChunk+1))
	_, handled, err = portableStringContext(
		withExecutionMeter(context.Background(), limitedRuntime),
		stringMethodInvocation(large, "toLowerCase", turkish),
	)
	if !handled || !errors.Is(err, ErrInstructionLimit) {
		t.Fatalf("locale case limit = (handled:%v, %v), want ErrInstructionLimit", handled, err)
	}
}
