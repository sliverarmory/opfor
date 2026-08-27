package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPortableJavaStringRegexMethodsAndProvenance(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		method      string
		target      Value
		arguments   []Value
		wantUnits   []uint16
		wantRawMask []bool
	}{
		{
			method: "replaceAll", target: String("😀"),
			arguments: []Value{String(""), String("-")},
			wantUnits: []uint16{'-', 0xd83d, '-', 0xde00, '-'},
		},
		{
			method: "replaceFirst", target: String("😀"),
			arguments: []Value{String(""), String("-")},
			wantUnits: []uint16{'-', 0xd83d, 0xde00},
		},
		{
			method: "replaceAll", target: BinaryString([]byte{'a', 0xff, 'a'}),
			arguments:   []Value{String("(a)"), BinaryString([]byte{0xc3, '$', '1'})},
			wantUnits:   []uint16{0xc3, 'a', 0xff, 0xc3, 'a'},
			wantRawMask: []bool{true, true, true, true, true},
		},
	} {
		got := invokePortableStringMethod(t, test.target, test.method, test.arguments...)
		if units := sleepStringUnits(got); !reflect.DeepEqual(units, test.wantUnits) {
			t.Errorf("%s units = %x, want %x", test.method, units, test.wantUnits)
		}
		wantRaw := test.wantRawMask
		if wantRaw == nil {
			wantRaw = make([]bool, len(test.wantUnits))
		}
		if raw := sleepStringRawMask(got); !reflect.DeepEqual(raw, wantRaw) {
			t.Errorf("%s provenance = %v, want %v", test.method, raw, wantRaw)
		}
	}

	replaced := invokePortableStringMethod(
		t,
		String("abc12"),
		"replaceAll",
		String(`(?<word>[a-z]+)(\d+)`),
		String(`${word}-$2-$0-\$-\\`),
	)
	if got, want := replaced.String(), `abc-12-abc12-$-\`; got != want {
		t.Fatalf("named/numeric/escaped replacement = %q, want %q", got, want)
	}
	if got := invokePortableStringMethod(t, String("a1"), "replaceAll", String(`(a)(1)`), String(`$12`)).String(); got != "a2" {
		t.Fatalf("largest legal numeric group = %q, want a2", got)
	}
	if got := invokePortableStringMethod(t, String("abc"), "replaceAll", String(`(?<=a)b`), String("X")).String(); got != "aXc" {
		t.Fatalf("lookbehind replacement = %q, want aXc", got)
	}
	if got := invokePortableStringMethod(t, String("aaa"), "matches", String("a+")).Truth(); !got {
		t.Fatal("matches did not require and accept the whole input")
	}
	if got := invokePortableStringMethod(t, String("baaa"), "matches", String("a+")).Truth(); got {
		t.Fatal("matches accepted a suffix-only match")
	}
}

func TestPortableJavaStringRegexReplacementErrorsAndNoMatch(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		replacement Value
		want        string
	}{
		{name: "numeric", replacement: String(`$9`), want: "java.lang.IndexOutOfBoundsException: No group 9"},
		{name: "named", replacement: String(`${missing}`), want: "java.lang.IllegalArgumentException: No group with name {missing}"},
		{name: "digit-name", replacement: String(`${1}`), want: "java.lang.IllegalArgumentException: capturing group name {1} starts with digit character"},
		{name: "trailing-backslash", replacement: String(`\`), want: "java.lang.IllegalArgumentException: character to be escaped is missing"},
		{name: "trailing-dollar", replacement: String(`$`), want: "java.lang.IllegalArgumentException: Illegal group reference: group index is missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			invocation := stringMethodInvocation(String("a"), "replaceAll", String("(a)"), test.replacement)
			_, handled, err := portableStringContext(context.Background(), invocation)
			if !handled || err == nil || err.Error() != test.want {
				t.Fatalf("replaceAll error = (handled:%v, %v), want %q", handled, err, test.want)
			}
		})
	}

	for _, method := range []string{"replaceAll", "replaceFirst"} {
		invocation := stringMethodInvocation(String("abc"), method, String("z"), String(`\`))
		value, handled, err := portableStringContext(context.Background(), invocation)
		if !handled || err != nil || !value.IdentityEqual(invocation.Target) {
			t.Errorf("%s no-match invalid replacement = (%s, %v), want unchanged receiver", method, value.Describe(), err)
		}
	}

	replaceAllNull := stringMethodInvocation(String("abc"), "replaceAll", String("z"), Null())
	if value, handled, err := portableStringContext(context.Background(), replaceAllNull); !handled || err != nil || !value.IdentityEqual(replaceAllNull.Target) {
		t.Fatalf("replaceAll no-match null = (%s, %v), want unchanged receiver", value.Describe(), err)
	}
	replaceFirstNull := stringMethodInvocation(String("abc"), "replaceFirst", String("z"), Null())
	if _, handled, err := portableStringContext(context.Background(), replaceFirstNull); !handled || err == nil || err.Error() != "java.lang.NullPointerException: replacement" {
		t.Fatalf("replaceFirst null = (handled:%v, %v)", handled, err)
	}
}

func TestPortableJavaStringSplitZeroWidthAndLimits(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		target Value
		regex  string
		limit  *int32
		want   []string
	}{
		{name: "ordinary", target: String("boo:and:foo"), regex: ":", want: []string{"boo", "and", "foo"}},
		{name: "positive-limit", target: String("boo:and:foo"), regex: ":", limit: int32Pointer(2), want: []string{"boo", "and:foo"}},
		{name: "trailing-discard", target: String("boo:and:foo"), regex: "o", want: []string{"b", "", ":and:f"}},
		{name: "trailing-retain", target: String("boo:and:foo"), regex: "o", limit: int32Pointer(-1), want: []string{"b", "", ":and:f", "", ""}},
		{name: "empty-zero", target: String("abc"), regex: "", want: []string{"a", "b", "c"}},
		{name: "empty-negative", target: String("abc"), regex: "", limit: int32Pointer(-1), want: []string{"a", "b", "c", ""}},
		{name: "empty-limited", target: String("abc"), regex: "", limit: int32Pointer(2), want: []string{"a", "bc"}},
		{name: "leading-positive-width", target: String(":a"), regex: ":", want: []string{"", "a"}},
		{name: "leading-zero-width", target: String("abc"), regex: "(?=a)", want: []string{"abc"}},
		{name: "empty-input", target: String(""), regex: "", want: []string{""}},
	} {
		t.Run(test.name, func(t *testing.T) {
			arguments := []Value{String(test.regex)}
			if test.limit != nil {
				arguments = append(arguments, Int(*test.limit))
			}
			value := invokePortableStringMethod(t, test.target, "split", arguments...)
			array, ok := value.Array()
			if !ok {
				t.Fatalf("split = %s, want array", value.Describe())
			}
			if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("split = %q, want %q", got, test.want)
			}
		})
	}

	raw := BinaryString([]byte{'a', 0xff, 'b'})
	value := invokePortableStringMethod(t, raw, "split", String("x"))
	array, _ := value.Array()
	pieces := array.Values()
	if len(pieces) != 1 || !pieces[0].IdentityEqual(raw) || !reflect.DeepEqual(sleepStringRawMask(pieces[0]), []bool{true, true, true}) {
		t.Fatalf("no-match split did not retain exact receiver: %#v", pieces)
	}

	supplementary := invokePortableStringMethod(t, String("😀"), "split", String(""), Int(-1))
	supplementaryArray, _ := supplementary.Array()
	supplementaryPieces := supplementaryArray.Values()
	if len(supplementaryPieces) != 3 ||
		!reflect.DeepEqual(sleepStringUnits(supplementaryPieces[0]), []uint16{0xd83d}) ||
		!reflect.DeepEqual(sleepStringUnits(supplementaryPieces[1]), []uint16{0xde00}) ||
		sleepStringLength(supplementaryPieces[2]) != 0 {
		t.Fatalf("supplementary empty split units = %#v", supplementaryPieces)
	}
}

func TestPortableJavaStringComparisonRegionBuilderOffsetAndGetChars(t *testing.T) {
	t.Parallel()

	if got := invokePortableStringMethod(t, String("𐐀"), "compareToIgnoreCase", String("𐐨")).Int32(); got != 0 {
		t.Fatalf("Deseret compareToIgnoreCase = %d, want 0", got)
	}
	if got := invokePortableStringMethod(t, String("abc"), "compareToIgnoreCase", String("ABd")).Int32(); got != -1 {
		t.Fatalf("compareToIgnoreCase = %d, want -1", got)
	}
	paired := sleepStringValueFromUnits([]uint16{0xd801, 0xdc00}, nil)
	unpairedHigh := sleepStringValueFromUnits([]uint16{0xd801}, nil)
	if got := invokePortableStringMethod(t, paired, "compareToIgnoreCase", unpairedHigh).Int32(); got != 1 {
		t.Fatalf("paired/unpaired compareToIgnoreCase = %d, want UTF-16 length difference 1", got)
	}
	if got := invokePortableStringMethod(t, String("a"), "compareToIgnoreCase", paired).Int32(); got != int32('a')-0xd801 {
		t.Fatalf("mixed-coder compareToIgnoreCase = %d, want %d", got, int32('a')-0xd801)
	}
	if _, handled, err := portableStringContext(
		context.Background(), stringMethodInvocation(String("a"), "compareToIgnoreCase", Null()),
	); !handled || err == nil || err.Error() != `java.lang.NullPointerException: Cannot read field "value" because "s2" is null` {
		t.Fatalf("compareToIgnoreCase(null) = (handled:%v, %v)", handled, err)
	}

	left, right := String("zz𐐀yy"), String("xx𐐨ww")
	if got := invokePortableStringMethod(t, left, "regionMatches", Bool(true), Int(2), right, Int(2), Int(2)); !got.Truth() {
		t.Fatalf("supplementary case-insensitive region = %s, want true", got.Describe())
	}
	if got := invokePortableStringMethod(t, left, "regionMatches", Int(2), right, Int(2), Int(2)); got.Truth() {
		t.Fatalf("supplementary exact region = %s, want false", got.Describe())
	}
	if got := invokePortableStringMethod(t, left, "regionMatches", Bool(true), Int(3), right, Int(3), Int(1)); got.Truth() {
		t.Fatalf("low-surrogate-only region = %s, want false", got.Describe())
	}
	if got := invokePortableStringMethod(t, left, "regionMatches", Int(7), right, Int(0), Int(-1)); !got.Truth() {
		t.Fatalf("negative-length valid OpenJDK region = %s, want true", got.Describe())
	}
	if got := invokePortableStringMethod(t, left, "regionMatches", Int(-1), right, Int(0), Int(1)); got.Truth() {
		t.Fatalf("negative-offset region = %s, want false", got.Describe())
	}
	for _, arguments := range [][]Value{
		{Bool(true), Int(-1), Null(), Int(0), Int(1)},
		{Bool(true), Int(7), Null(), Int(0), Int(1)},
	} {
		value, handled, err := portableStringContext(context.Background(), stringMethodInvocation(left, "regionMatches", arguments...))
		if err != nil || !handled || value.Truth() {
			t.Errorf("five-argument null short circuit = (%s, %v, %v), want false", value.Describe(), handled, err)
		}
	}
	for _, arguments := range [][]Value{
		{Bool(false), Int(-1), Null(), Int(0), Int(1)},
		{Bool(false), Int(7), Null(), Int(0), Int(1)},
		{Bool(true), Int(0), Null(), Int(0), Int(1)},
	} {
		_, handled, err := portableStringContext(context.Background(), stringMethodInvocation(left, "regionMatches", arguments...))
		if !handled || err == nil || err.Error() != "java.lang.NullPointerException" {
			t.Errorf("five-argument null dereference = (handled:%v, %v), want NullPointerException", handled, err)
		}
	}

	builder := constructPortableStringBuilder(t, "StringBuilder", String("hello"))
	buffer := constructPortableStringBuilder(t, "StringBuffer", String("hello"))
	for _, object := range []*portableJavaStringBuffer{builder, buffer} {
		if got := invokePortableStringMethod(t, String("hello"), "contentEquals", ObjectValue(object)); !got.Truth() {
			t.Errorf("contentEquals(%s) = false", object.class)
		}
	}
	invokePortableStringBuilder(t, builder, "append", String("!"))
	if got := invokePortableStringMethod(t, String("hello"), "contentEquals", ObjectValue(builder)); got.Truth() {
		t.Fatal("contentEquals ignored builder mutation")
	}

	target := String("a😀b")
	for _, test := range []struct {
		index, offset, want int32
	}{
		{index: 0, offset: 2, want: 3},
		{index: 3, offset: -1, want: 1},
		{index: 2, offset: -1, want: 1},
		{index: 2, offset: 1, want: 3},
	} {
		if got := invokePortableStringMethod(t, target, "offsetByCodePoints", Int(test.index), Int(test.offset)).Int32(); got != test.want {
			t.Errorf("offsetByCodePoints(%d,%d) = %d, want %d", test.index, test.offset, got, test.want)
		}
	}

	destination := newPortableJavaArray(
		portableJavaArrayType{name: "char", descriptor: "C", primitive: true},
		[]int{5},
		[]Value{String("x"), String("x"), String("x"), String("x"), String("x")},
	)
	invokePortableStringMethod(t, target, "getChars", Int(1), Int(4), ObjectValue(destination), Int(1))
	if got, want := sleepStringUnits(destination.toSleepValue()), []uint16{'x', 0xd83d, 0xde00, 'b', 'x'}; !reflect.DeepEqual(got, want) {
		t.Fatalf("getChars destination = %x, want %x", got, want)
	}
	rawDestination := newPortableJavaArray(
		portableJavaArrayType{name: "char", descriptor: "C", primitive: true},
		[]int{1}, []Value{String("x")},
	)
	invokePortableStringMethod(t, BinaryString([]byte{0xff}), "getChars", Int(0), Int(1), ObjectValue(rawDestination), Int(0))
	if got, want := sleepStringRawMask(rawDestination.toSleepValue()), []bool{false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("char[] retained non-Java raw provenance: %v", got)
	}
}

func TestPortableJavaStringNewMethodExceptionsAndLimits(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "java-string-next-errors.sl", `
debug(0);
$bad_pattern = ["abc" matches: "["];
$pattern_error = checkError();
$bad_group = ["a" replaceAll: "(a)", "\${missing}"];
$group_error = checkError();
$bad_offset = ["abc" offsetByCodePoints: 0, -1];
$offset_error = checkError();
$bad_chars = ["abc" getChars: 2, 1, $null, 0];
$chars_error = checkError();
$null_region = ["abc" regionMatches: 0, $null, 0, 1];
$region_error = checkError();
$short_null_region = ["abc" regionMatches: 1, -1, $null, 0, 1];
$short_region_error = checkError();
return @(
    $bad_pattern, [[$pattern_error getClass] getName], $pattern_error isa ^java.util.regex.PatternSyntaxException, $pattern_error isa ^java.lang.IllegalArgumentException,
    $bad_group, [[$group_error getClass] getName],
    $bad_offset, [[$offset_error getClass] getName],
    $bad_chars, [[$chars_error getClass] getName],
    $null_region, [[$region_error getClass] getName],
    $short_null_region, $short_region_error
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, _ := value.Array()
	want := []string{
		"", "java.util.regex.PatternSyntaxException", "1", "1",
		"", "java.lang.IllegalArgumentException",
		"", "java.lang.IndexOutOfBoundsException",
		"", "java.lang.StringIndexOutOfBoundsException",
		"", "java.lang.NullPointerException",
		"0", "",
	}
	if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("new String exception results = %q, want %q", got, want)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runtimeInstance.objectHost.Object(cancelled, stringMethodInvocation(String(strings.Repeat("a", 4096)+"!"), "matches", String(`^(a+)+$`)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled matches error = %v, want context.Canceled", err)
	}

	limitedRuntime, err := New(WithInstructionLimit(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limitedRuntime.Close(context.Background()) })
	limitedContext := withExecutionMeter(context.Background(), limitedRuntime)
	_, err = limitedRuntime.objectHost.Object(limitedContext, stringMethodInvocation(String("a,a,a"), "split", String(",")))
	if !errors.Is(err, ErrInstructionLimit) {
		t.Fatalf("instruction-limited split error = %v, want ErrInstructionLimit", err)
	}

	large := String(strings.Repeat("a", portableJavaStringNativeLoopChunk+1))
	destinationValues := make([]Value, portableJavaStringNativeLoopChunk+1)
	for index := range destinationValues {
		destinationValues[index] = String("x")
	}
	destination := newPortableJavaArray(
		portableJavaArrayType{name: "char", descriptor: "C", primitive: true},
		[]int{len(destinationValues)}, destinationValues,
	)
	loopInvocations := []struct {
		name       string
		invocation ObjectInvocation
	}{
		{name: "compareToIgnoreCase", invocation: stringMethodInvocation(large, "compareToIgnoreCase", large)},
		{name: "contentEquals", invocation: stringMethodInvocation(large, "contentEquals", large)},
		{name: "regionMatches", invocation: stringMethodInvocation(large, "regionMatches", Int(0), large, Int(0), Int(portableJavaStringNativeLoopChunk+1))},
		{name: "offsetByCodePoints", invocation: stringMethodInvocation(large, "offsetByCodePoints", Int(0), Int(portableJavaStringNativeLoopChunk+1))},
		{name: "getChars", invocation: stringMethodInvocation(large, "getChars", Int(0), Int(portableJavaStringNativeLoopChunk+1), ObjectValue(destination), Int(0))},
	}
	for _, test := range loopInvocations {
		t.Run(test.name+"-cancellation", func(t *testing.T) {
			ctx := newPortableStringCheckCancelContext(5)
			_, handled, err := portableStringContext(ctx, test.invocation)
			if !handled || !errors.Is(err, context.Canceled) {
				t.Fatalf("native-loop cancellation = (handled:%v, %v), want context.Canceled", handled, err)
			}
		})
		t.Run(test.name+"-instruction-limit", func(t *testing.T) {
			ctx := withExecutionMeter(context.Background(), limitedRuntime)
			_, handled, err := portableStringContext(ctx, test.invocation)
			if !handled || !errors.Is(err, ErrInstructionLimit) {
				t.Fatalf("native-loop limit = (handled:%v, %v), want ErrInstructionLimit", handled, err)
			}
		})
	}
	for index, value := range destination.values {
		if value.String() != "x" {
			t.Fatalf("canceled/limited getChars mutated destination[%d] to %s", index, value.Describe())
		}
	}
	invalidDestination := stringMethodInvocation(
		large, "getChars", Int(0), Int(portableJavaStringNativeLoopChunk+1), ObjectValue(destination), Int(-1),
	)
	_, handled, err := portableStringContext(withExecutionMeter(context.Background(), limitedRuntime), invalidDestination)
	if !handled || err == nil || errors.Is(err, ErrInstructionLimit) || !strings.HasPrefix(err.Error(), "java.lang.StringIndexOutOfBoundsException") {
		t.Fatalf("invalid getChars destination preflight = (handled:%v, %v), want immediate StringIndexOutOfBoundsException", handled, err)
	}
}

func TestPortableJavaStringRegexAndBuilderConcurrentUse(t *testing.T) {
	t.Parallel()

	builder := constructPortableStringBuilder(t, "StringBuffer", String("ABC𐐀"))
	const workers = 16
	var wait sync.WaitGroup
	errorsByWorker := make(chan error, workers*2)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			got, handled, err := portableStringContext(
				context.Background(),
				stringMethodInvocation(String("abc𐐨"), "contentEquals", ObjectValue(builder)),
			)
			if err != nil || !handled {
				errorsByWorker <- fmt.Errorf("worker %d contentEquals = (handled:%v, error:%v)", worker, handled, err)
				return
			}
			if got.Truth() {
				errorsByWorker <- fmt.Errorf("worker %d contentEquals unexpectedly case-folded", worker)
			}
			got, handled, err = portableStringContext(
				context.Background(),
				stringMethodInvocation(String("a1b2"), "replaceAll", String(`(\d)`), String(`[$1]`)),
			)
			if err != nil || !handled {
				errorsByWorker <- fmt.Errorf("worker %d replaceAll = (handled:%v, error:%v)", worker, handled, err)
				return
			}
			if got.String() != "a[1]b[2]" {
				errorsByWorker <- fmt.Errorf("worker %d replaceAll = %q", worker, got.String())
			}
		}(worker)
	}
	wait.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		t.Error(err)
	}
}

const portableJavaStringNextProbeSource = `
println(["aaa" matches: "a+"]);
println(["abc12" replaceAll: "([a-z]+)([0-9]+)", "\$2-\$1"]);
println(["abc12abc" replaceFirst: "[0-9]+", "#"]);
println([["😀" replaceAll: "", "-"] length]);
@zero = ["abc" split: "", -1];
println(size(@zero) . ":" . @zero[0] . ":" . @zero[1] . ":" . @zero[2] . ":" . @zero[3]);
@supplementary = ["😀" split: "", -1];
println(size(@supplementary) . ":" . [@supplementary[0] length] . ":" . [@supplementary[1] length] . ":" . [@supplementary[2] length]);
println(["𐐀" compareToIgnoreCase: "𐐨"]);
println(["zz𐐀yy" regionMatches: 1, 2, "xx𐐨ww", 2, 2]);
$builder = [new StringBuilder: "hello"];
println(["hello" contentEquals: $builder]);
println(["a😀b" offsetByCodePoints: 0, 2]);
`

func TestPortableJavaStringNextMethodsOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	want, err := officialSleepJavaCommand(java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaStringNextProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep next String probe: %v\n%s", err, want)
	}
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-string-next.sl", portableJavaStringNextProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("official Sleep next String mismatch\nwant:\n%sgot:\n%s", want, output.Bytes())
	}
}

func stringMethodInvocation(target Value, method string, arguments ...Value) ObjectInvocation {
	invocation := ObjectInvocation{Op: ObjectInvoke, Target: target, Message: method}
	for _, argument := range arguments {
		invocation.Arguments = append(invocation.Arguments, Argument{Value: argument})
	}
	return invocation
}

func int32Pointer(value int32) *int32 { return &value }

type portableStringCheckCancelContext struct {
	context.Context
	cancelAt int32
	checks   atomic.Int32
	done     chan struct{}
	once     sync.Once
}

func newPortableStringCheckCancelContext(cancelAt int32) *portableStringCheckCancelContext {
	return &portableStringCheckCancelContext{
		Context:  context.Background(),
		cancelAt: cancelAt,
		done:     make(chan struct{}),
	}
}

func (ctx *portableStringCheckCancelContext) Done() <-chan struct{} { return ctx.done }

func (ctx *portableStringCheckCancelContext) Err() error {
	if ctx.checks.Add(1) >= ctx.cancelAt {
		ctx.once.Do(func() { close(ctx.done) })
		return context.Canceled
	}
	return nil
}
