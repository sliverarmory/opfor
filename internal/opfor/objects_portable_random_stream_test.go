package opfor

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestPortableJavaRandomPrimitiveStreamsVectorsAndState(t *testing.T) {
	t.Parallel()

	random := newPortableJavaRandom(0)
	_, ints := portableRandomStreamForTest(t, random, "ints", Long(4), Int(-10), Int(10))
	intArray := portableRandomStreamCallForTest(t, context.Background(), ints, "toArray")
	intValues, _ := intArray.Array()
	if got, want := argvValueStrings(intValues.Values()), []string{"-10", "-2", "-1", "-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bounded ints = %v, want %v", got, want)
	}
	oracle := newPortableJavaRandom(0)
	for range 4 {
		_ = invokePortableRandom(t, oracle, "nextInt", Int(-10), Int(10))
	}
	if got, want := invokePortableRandom(t, random, "nextInt").Int32(), invokePortableRandom(t, oracle, "nextInt").Int32(); got != want {
		t.Fatalf("nextInt after stream = %d, want %d", got, want)
	}

	random = newPortableJavaRandom(0)
	_, longs := portableRandomStreamForTest(t, random, "longs", Long(3))
	longArray := portableRandomStreamCallForTest(t, context.Background(), longs, "toArray")
	longValues, _ := longArray.Array()
	oracle = newPortableJavaRandom(0)
	for index, value := range longValues.Values() {
		if got, want := value.Int64(), invokePortableRandom(t, oracle, "nextLong").Int64(); got != want {
			t.Fatalf("longs[%d] = %d, want %d", index, got, want)
		}
	}
	if got, want := invokePortableRandom(t, random, "nextInt").Int32(), invokePortableRandom(t, oracle, "nextInt").Int32(); got != want {
		t.Fatalf("nextInt after long stream = %d, want %d", got, want)
	}

	random = newPortableJavaRandom(0)
	_, doubles := portableRandomStreamForTest(t, random, "doubles", Long(3), Double(-2), Double(3))
	doubleArray := portableRandomStreamCallForTest(t, context.Background(), doubles, "toArray")
	doubleValues, _ := doubleArray.Array()
	oracle = newPortableJavaRandom(0)
	for index, value := range doubleValues.Values() {
		want := invokePortableRandom(t, oracle, "nextDouble", Double(-2), Double(3)).Float64()
		if got := value.Float64(); math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("doubles[%d] = %.17g (%#x), want %.17g (%#x)", index, got, math.Float64bits(got), want, math.Float64bits(want))
		}
	}
	if got, want := invokePortableRandom(t, random, "nextInt").Int32(), invokePortableRandom(t, oracle, "nextInt").Int32(); got != want {
		t.Fatalf("nextInt after double stream = %d, want %d", got, want)
	}
}

func TestPortableJavaRandomPrimitiveStreamAllOverloadShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		message    string
		arguments  []Value
		kind       portableJavaRandomStreamKind
		size       int64
		bounded    bool
		intOrigin  int32
		intBound   int32
		longOrigin int64
		longBound  int64
		dblOrigin  float64
		dblBound   float64
	}{
		{name: "ints unlimited", message: "ints", kind: portableJavaRandomIntStream, size: math.MaxInt64},
		{name: "ints sized", message: "ints", arguments: []Value{Long(7)}, kind: portableJavaRandomIntStream, size: 7},
		{name: "ints ranged", message: "ints", arguments: []Value{Int(-4), Int(9)}, kind: portableJavaRandomIntStream, size: math.MaxInt64, bounded: true, intOrigin: -4, intBound: 9},
		{name: "ints sized ranged", message: "ints", arguments: []Value{Long(7), Int(-4), Int(9)}, kind: portableJavaRandomIntStream, size: 7, bounded: true, intOrigin: -4, intBound: 9},
		{name: "longs unlimited", message: "longs", kind: portableJavaRandomLongStream, size: math.MaxInt64},
		{name: "longs sized", message: "longs", arguments: []Value{Long(7)}, kind: portableJavaRandomLongStream, size: 7},
		{name: "longs ranged", message: "longs", arguments: []Value{Long(-4), Long(9)}, kind: portableJavaRandomLongStream, size: math.MaxInt64, bounded: true, longOrigin: -4, longBound: 9},
		{name: "longs sized ranged", message: "longs", arguments: []Value{Long(7), Long(-4), Long(9)}, kind: portableJavaRandomLongStream, size: 7, bounded: true, longOrigin: -4, longBound: 9},
		{name: "doubles unlimited", message: "doubles", kind: portableJavaRandomDoubleStream, size: math.MaxInt64},
		{name: "doubles sized", message: "doubles", arguments: []Value{Long(7)}, kind: portableJavaRandomDoubleStream, size: 7},
		{name: "doubles ranged", message: "doubles", arguments: []Value{Double(-4), Double(9)}, kind: portableJavaRandomDoubleStream, size: math.MaxInt64, bounded: true, dblOrigin: -4, dblBound: 9},
		{name: "doubles sized ranged", message: "doubles", arguments: []Value{Long(7), Double(-4), Double(9)}, kind: portableJavaRandomDoubleStream, size: 7, bounded: true, dblOrigin: -4, dblBound: 9},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			random := newPortableJavaRandom(0)
			_, stream := portableRandomStreamForTest(t, random, test.message, test.arguments...)
			if stream.kind != test.kind || stream.size != test.size || stream.bounded != test.bounded {
				t.Fatalf("stream = (kind:%d size:%d bounded:%v), want (%d %d %v)", stream.kind, stream.size, stream.bounded, test.kind, test.size, test.bounded)
			}
			if stream.intOrigin != test.intOrigin || stream.intBound != test.intBound ||
				stream.longOrigin != test.longOrigin || stream.longBound != test.longBound ||
				math.Float64bits(stream.doubleOrigin) != math.Float64bits(test.dblOrigin) ||
				math.Float64bits(stream.doubleBound) != math.Float64bits(test.dblBound) {
				t.Fatalf("stream bounds = (%d,%d)/(%d,%d)/(%g,%g), want (%d,%d)/(%d,%d)/(%g,%g)",
					stream.intOrigin, stream.intBound, stream.longOrigin, stream.longBound, stream.doubleOrigin, stream.doubleBound,
					test.intOrigin, test.intBound, test.longOrigin, test.longBound, test.dblOrigin, test.dblBound)
			}
			if got := invokePortableRandom(t, random, "nextInt").Int32(); got != -1155484576 {
				t.Fatalf("stream creation consumed state; nextInt = %d", got)
			}
		})
	}
}

func TestPortableJavaRandomPrimitiveStreamSleepOverloadCoercion(t *testing.T) {
	t.Parallel()
	random := newPortableJavaRandom(0)
	for _, test := range []struct {
		message   string
		arguments []Value
	}{
		{message: "ints", arguments: []Value{String("3")}},
		{message: "ints", arguments: []Value{Int(0), String("3")}},
		{message: "longs", arguments: []Value{Int(0), String("3")}},
		{message: "doubles", arguments: []Value{Int(0), String("3")}},
		{message: "ints", arguments: []Value{ObjectValue(&portableJavaPrimitive{class: "java.lang.Integer", value: Int(3)})}},
	} {
		arguments := make([]Argument, len(test.arguments))
		for index, argument := range test.arguments {
			arguments[index] = Argument{Value: argument}
		}
		value, handled, err := random.invoke(ObjectInvocation{
			Op: ObjectInvoke, Message: test.message, Arguments: arguments,
		})
		if err != nil || !handled || !value.IsNull() {
			t.Errorf("%s invalid overload = (%s, handled:%t, %v), want no-matching-method null", test.message, value.Describe(), handled, err)
		}
	}

	boxedSize := ObjectValue(&portableJavaPrimitive{class: "java.lang.Long", value: Long(2)})
	_, sized := portableRandomStreamForTest(t, random, "ints", boxedSize)
	if sized.size != 2 {
		t.Fatalf("ints(boxed Long) size = %d, want 2", sized.size)
	}
	boxedOrigin := ObjectValue(&portableJavaPrimitive{class: "java.lang.Integer", value: Int(-2)})
	boxedBound := ObjectValue(&portableJavaPrimitive{class: "java.lang.Integer", value: Int(3)})
	_, bounded := portableRandomStreamForTest(t, random, "ints", boxedOrigin, boxedBound)
	if bounded.intOrigin != -2 || bounded.intBound != 3 {
		t.Fatalf("ints(boxed Integer bounds) = (%d,%d), want (-2,3)", bounded.intOrigin, bounded.intBound)
	}
}

func TestPortableJavaRandomPrimitiveStreamsAreLazySizedAndSingleUse(t *testing.T) {
	t.Parallel()

	random := newPortableJavaRandom(0)
	streamValue, stream := portableRandomStreamForTest(t, random, "ints", Long(3))
	if got := portableRandomStreamCallForTest(t, context.Background(), stream, "count").Int64(); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
	if got := invokePortableRandom(t, random, "nextInt").Int32(); got != -1155484576 {
		t.Fatalf("nextInt after sized count = %d, want unconsumed first value", got)
	}
	_, handled, err := stream.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Target: streamValue, Message: "toArray",
	})
	if !handled || err == nil || err.Error() != "java.lang.IllegalStateException: stream has already been operated upon or closed" {
		t.Fatalf("stream reuse = (handled:%v, %v), want operated-upon error", handled, err)
	}

	random = newPortableJavaRandom(0)
	_, empty := portableRandomStreamForTest(t, random, "doubles", Long(0))
	emptyArray := portableRandomStreamCallForTest(t, context.Background(), empty, "toArray")
	array, _ := emptyArray.Array()
	if array.Len() != 0 {
		t.Fatalf("empty stream array length = %d, want 0", array.Len())
	}
	if got := invokePortableRandom(t, random, "nextInt").Int32(); got != -1155484576 {
		t.Fatalf("zero-size stream consumed state; nextInt = %d", got)
	}
}

func TestPortableJavaRandomPrimitiveStreamBaseAndSumOperations(t *testing.T) {
	t.Parallel()

	random := newPortableJavaRandom(0)
	streamValue, stream := portableRandomStreamForTest(t, random, "ints", Long(3))
	if unordered := portableRandomStreamCallForTest(t, context.Background(), stream, "unordered"); !unordered.IdentityEqual(streamValue) {
		t.Fatalf("unordered = %s, want exact stream identity", unordered.Describe())
	}
	if parallel := portableRandomStreamCallForTest(t, context.Background(), stream, "parallel"); !parallel.IdentityEqual(streamValue) {
		t.Fatalf("parallel = %s, want exact stream identity", parallel.Describe())
	}
	if !portableRandomStreamCallForTest(t, context.Background(), stream, "isParallel").Truth() {
		t.Fatal("parallel stream did not report parallel")
	}
	if sequential := portableRandomStreamCallForTest(t, context.Background(), stream, "sequential"); !sequential.IdentityEqual(streamValue) {
		t.Fatalf("sequential = %s, want exact stream identity", sequential.Describe())
	}
	if portableRandomStreamCallForTest(t, context.Background(), stream, "isParallel").Truth() {
		t.Fatal("sequential stream still reports parallel")
	}
	if got := portableRandomStreamCallForTest(t, context.Background(), stream, "sum").Int32(); got != -846343918 {
		t.Fatalf("int stream sum = %d, want -846343918", got)
	}

	random = newPortableJavaRandom(0)
	_, stream = portableRandomStreamForTest(t, random, "longs", Long(4), Long(-20), Long(30))
	oracle := newPortableJavaRandom(0)
	var wantLong int64
	for range 4 {
		wantLong += invokePortableRandom(t, oracle, "nextLong", Long(-20), Long(30)).Int64()
	}
	if got := portableRandomStreamCallForTest(t, context.Background(), stream, "sum").Int64(); got != wantLong {
		t.Fatalf("long stream sum = %d, want %d", got, wantLong)
	}

	random = newPortableJavaRandom(0)
	_, stream = portableRandomStreamForTest(t, random, "doubles", Long(3))
	if got, want := portableRandomStreamCallForTest(t, context.Background(), stream, "sum").Float64(), 1.6089216283982513; math.Float64bits(got) != math.Float64bits(want) {
		t.Fatalf("double stream sum = %.17g (%#x), want %.17g (%#x)", got, math.Float64bits(got), want, math.Float64bits(want))
	}
}

func TestPortableJavaRandomPrimitiveStreamIteratorMatchesSpliteratorAdapter(t *testing.T) {
	t.Parallel()

	random := newPortableJavaRandom(0)
	_, stream := portableRandomStreamForTest(t, random, "ints", Long(2))
	iteratorValue := portableRandomStreamCallForTest(t, context.Background(), stream, "iterator")
	iteratorObject, _ := iteratorValue.Object()
	iterator := iteratorObject.(*portableJavaRandomStreamIterator)

	if !portableRandomIteratorCallForTest(t, iteratorValue, iterator, "hasNext").Truth() ||
		!portableRandomIteratorCallForTest(t, iteratorValue, iterator, "hasNext").Truth() {
		t.Fatal("iterator did not report its cached first element")
	}
	// Spliterators.iterator().hasNext() advances the source and caches the
	// value. Repeated hasNext calls do not draw again.
	if got := invokePortableRandom(t, random, "nextInt").Int32(); got != -723955400 {
		t.Fatalf("nextInt after iterator hasNext = %d, want second generator value", got)
	}
	if got := portableRandomIteratorCallForTest(t, iteratorValue, iterator, "nextInt").Int32(); got != -1155484576 {
		t.Fatalf("cached iterator value = %d, want first generator value", got)
	}
	if got := portableRandomIteratorCallForTest(t, iteratorValue, iterator, "next").Int32(); got != 1033096058 {
		t.Fatalf("boxed iterator value after external draw = %d, want third generator value", got)
	}
	if portableRandomIteratorCallForTest(t, iteratorValue, iterator, "hasNext").Truth() {
		t.Fatal("exhausted iterator hasNext = true")
	}
	_, handled, err := iterator.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Target: iteratorValue, Message: "nextInt",
	})
	if !handled || err == nil || err.Error() != "java.util.NoSuchElementException" {
		t.Fatalf("exhausted nextInt = (handled:%v, %v), want NoSuchElementException", handled, err)
	}
}

func TestPortableJavaRandomPrimitiveStreamTypes(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-stream-types.sl", `
import java.util.stream.*;
$random = [new Random: 0L];
$ints = [$random ints: 1L];
$longs = [$random longs: 1L];
$doubles = [$random doubles: 1L];
$intIterator = [$ints iterator];
$longIterator = [$longs iterator];
$doubleIterator = [$doubles iterator];
return @(
    [[$ints getClass] getName], $ints isa ^IntStream, $ints isa ^BaseStream,
    [[$longs getClass] getName], $longs isa ^LongStream, $longs isa ^BaseStream,
    [[$doubles getClass] getName], $doubles isa ^DoubleStream, $doubles isa ^BaseStream,
    $intIterator isa ^PrimitiveIterator$OfInt,
    $longIterator isa ^PrimitiveIterator$OfLong,
    $doubleIterator isa ^PrimitiveIterator$OfDouble,
    $doubleIterator isa ^Iterator
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, _ := value.Array()
	if got, want := argvValueStrings(array.Values()), []string{
		"java.util.stream.IntPipeline$Head", "1", "1",
		"java.util.stream.LongPipeline$Head", "1", "1",
		"java.util.stream.DoublePipeline$Head", "1", "1",
		"1", "1", "1", "1",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("random stream types = %v, want %v", got, want)
	}
}

func TestPortableJavaRandomPrimitiveStreamValidationIsSoftAndDoesNotConsumeState(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-stream-errors.sl", `
$random = [new Random: 0L];
$bad = [$random ints: -1L, 4, 3];
$sizeError = checkError();
$bad = [$random doubles: 0L, 0.0, (0.0 / 0.0)];
$rangeError = checkError();
$next = [$random nextInt];
return @(
    [[$sizeError getClass] getName], [$sizeError getMessage],
    [[$rangeError getClass] getName], [$rangeError getMessage],
    $next
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, _ := value.Array()
	if got, want := argvValueStrings(array.Values()), []string{
		"java.lang.IllegalArgumentException", "size must be non-negative",
		"java.lang.IllegalArgumentException", "bound must be greater than origin",
		"-1155484576",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stream validation = %v, want %v", got, want)
	}
}

func TestPortableJavaRandomPrimitiveStreamLimitsAndConcurrentTerminal(t *testing.T) {
	t.Parallel()

	random := newPortableJavaRandom(0)
	streamValue, stream := portableRandomStreamForTest(
		t, random, "ints", Long(portableCollectionsMaximumMaterializedElements+1),
	)
	_, handled, err := stream.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Target: streamValue, Message: "toArray",
	})
	if !handled || !errors.Is(err, errPortableJavaRandomStreamMaterializationLimit) {
		t.Fatalf("oversized toArray = (handled:%v, %v), want materialization error", handled, err)
	}
	if got := invokePortableRandom(t, random, "nextInt").Int32(); got != -1155484576 {
		t.Fatalf("oversized toArray consumed state; nextInt = %d", got)
	}

	runtimeInstance, err := New(WithInstructionLimit(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	random = newPortableJavaRandom(0)
	streamValue, stream = portableRandomStreamForTest(
		t, random, "ints", Long(portableJavaRandomStreamNativeLoopChunk+1),
	)
	_, handled, err = stream.invoke(withExecutionMeter(context.Background(), runtimeInstance), ObjectInvocation{
		Op: ObjectInvoke, Target: streamValue, Message: "sum",
	})
	if !handled || !errors.Is(err, ErrInstructionLimit) {
		t.Fatalf("instruction-limited sum = (handled:%v, %v), want ErrInstructionLimit", handled, err)
	}
	oracle := newPortableJavaRandom(0)
	for range portableJavaRandomStreamNativeLoopChunk {
		_ = invokePortableRandom(t, oracle, "nextInt")
	}
	if got, want := invokePortableRandom(t, random, "nextInt").Int32(), invokePortableRandom(t, oracle, "nextInt").Int32(); got != want {
		t.Fatalf("state after interrupted stream = %d, want %d", got, want)
	}

	random = newPortableJavaRandom(0)
	streamValue, stream = portableRandomStreamForTest(t, random, "longs", Long(5))
	const workers = 20
	var wait sync.WaitGroup
	results := make(chan error, workers)
	for range workers {
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
			if err == nil && value.Int64() != 5 {
				results <- errors.New("wrong count")
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
		t.Fatalf("successful concurrent terminals = %d, want 1", successes)
	}
}

func TestPortableJavaRandomPrimitiveStreamPreservesObjectHostPrecedence(t *testing.T) {
	t.Parallel()

	var calls int
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Op == ObjectInvoke && invocation.Message == "ints" {
			calls++
			return String("importer-stream"), nil
		}
		return Null(), &UnsupportedError{Operation: "object"}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-stream-host.sl", `
$random = [new Random: 0L];
return [$random ints: 3L, 0, 10];
`)
	if err != nil {
		t.Fatal(err)
	}
	if value.String() != "importer-stream" || calls != 1 {
		t.Fatalf("ObjectHost stream = (%s, calls %d), want importer-stream once", value.Describe(), calls)
	}

	terminalCalls := 0
	terminalRuntime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Op == ObjectInvoke && invocation.Message == "count" {
			if object, ok := invocation.Target.Object(); ok {
				if _, ok := object.(*portableJavaRandomStream); ok {
					terminalCalls++
					return Long(77), nil
				}
			}
		}
		return Null(), &UnsupportedError{Operation: "object"}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminalRuntime.Close(context.Background()) })
	value, err = terminalRuntime.Eval(context.Background(), "random-stream-terminal-host.sl", `
$random = [new Random: 0L];
$stream = [$random ints: 3L];
return [$stream count];
`)
	if err != nil {
		t.Fatal(err)
	}
	if value.Int64() != 77 || terminalCalls != 1 {
		t.Fatalf("ObjectHost terminal = (%s, calls %d), want 77 once", value.Describe(), terminalCalls)
	}
}

func portableRandomStreamForTest(
	t *testing.T,
	random *portableJavaRandom,
	message string,
	arguments ...Value,
) (Value, *portableJavaRandomStream) {
	t.Helper()
	invocationArguments := make([]Argument, len(arguments))
	for index, argument := range arguments {
		invocationArguments[index] = Argument{Value: argument}
	}
	value, handled, err := random.invoke(ObjectInvocation{
		Op: ObjectInvoke, Message: message, Arguments: invocationArguments,
	})
	if err != nil || !handled {
		t.Fatalf("%s stream = (%s, handled:%v, error:%v)", message, value.Describe(), handled, err)
	}
	object, ok := value.Object()
	if !ok {
		t.Fatalf("%s stream = %s, want object", message, value.Describe())
	}
	stream, ok := object.(*portableJavaRandomStream)
	if !ok || stream == nil {
		t.Fatalf("%s stream object = %T, want *portableJavaRandomStream", message, object)
	}
	return value, stream
}

func portableRandomStreamCallForTest(
	t *testing.T,
	ctx context.Context,
	stream *portableJavaRandomStream,
	message string,
) Value {
	t.Helper()
	streamValue := ObjectValue(stream)
	value, handled, err := stream.invoke(ctx, ObjectInvocation{
		Op: ObjectInvoke, Target: streamValue, Message: message,
	})
	if err != nil || !handled {
		t.Fatalf("stream %s = (%s, handled:%v, error:%v)", message, value.Describe(), handled, err)
	}
	return value
}

func portableRandomIteratorCallForTest(
	t *testing.T,
	iteratorValue Value,
	iterator *portableJavaRandomStreamIterator,
	message string,
) Value {
	t.Helper()
	value, handled, err := iterator.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Target: iteratorValue, Message: message,
	})
	if err != nil || !handled {
		t.Fatalf("iterator %s = (%s, handled:%v, error:%v)", message, value.Describe(), handled, err)
	}
	return value
}
