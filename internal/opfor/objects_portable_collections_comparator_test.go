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

type portableComparatorTestCallable struct {
	calls *int
}

func (callable *portableComparatorTestCallable) Invoke(_ context.Context, values ...Value) (Value, error) {
	if callable != nil && callable.calls != nil {
		*callable.calls++
	}
	if len(values) != 2 {
		return Null(), fmt.Errorf("Comparator values = %d, want 2", len(values))
	}
	return Int(int32(portableJavaCompare(values[0], values[1]))), nil
}

func TestPortableCollectionsComparatorFactoriesAndOverloads(t *testing.T) {
	ctx := context.Background()
	reverse := invokePortableCollectionsUtilityForTest(t, ctx, "reverseOrder")
	reverseNull := invokePortableCollectionsUtilityForTest(t, ctx, "reverseOrder", Null())
	if !reverse.IdentityEqual(reverseNull) {
		t.Fatal("reverseOrder() and reverseOrder(null) did not return the same singleton")
	}
	natural := invokePortableCollectionsUtilityForTest(t, ctx, "reverseOrder", reverse)
	if object, ok := natural.Object(); !ok || object != portableCollectionsNaturalComparator {
		t.Fatalf("reverseOrder(reverse singleton) = %s, want natural-order singleton", natural.Describe())
	}
	if roundTrip := invokePortableCollectionsUtilityForTest(t, ctx, "reverseOrder", natural); !roundTrip.IdentityEqual(reverse) {
		t.Fatal("reverseOrder(natural singleton) did not return reverse-order singleton")
	}

	calls := 0
	comparator := FunctionValue(&portableComparatorTestCallable{calls: &calls})
	list := newPortableJavaCollection("ArrayList", []Value{Int(3), Int(1), Int(2), Int(1)})
	invokePortableCollectionsUtilityForTest(t, ctx, "sort", ObjectValue(list), comparator)
	if got, want := argvValueStrings(list.snapshot()), []string{"1", "1", "2", "3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Comparator sort = %q, want %q", got, want)
	}
	if calls != 6 {
		t.Fatalf("small TimSort Comparator calls = %d, want OpenJDK order count 6", calls)
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "binarySearch", ObjectValue(list), Int(2), comparator); got.Int32() != 2 {
		t.Fatalf("Comparator binarySearch = %s, want 2", got.Describe())
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "min", ObjectValue(list), comparator); got.Int32() != 1 {
		t.Fatalf("Comparator min = %s, want 1", got.Describe())
	}
	if got := invokePortableCollectionsUtilityForTest(t, ctx, "max", ObjectValue(list), comparator); got.Int32() != 3 {
		t.Fatalf("Comparator max = %s, want 3", got.Describe())
	}

	reversed := invokePortableCollectionsUtilityForTest(t, ctx, "reverseOrder", comparator)
	wrapper, ok := reversed.data.(*portableJavaReverseComparator2)
	if !ok || wrapper == nil {
		t.Fatalf("reverseOrder(Comparator) = %s, want ReverseComparator2", reversed.Describe())
	}
	unwrapped := invokePortableCollectionsUtilityForTest(t, ctx, "reverseOrder", reversed)
	proxyObject, ok := unwrapped.Object()
	proxy, proxyOK := proxyObject.(*portableJavaProxy)
	if !ok || !proxyOK || proxy == nil || !proxy.implements("java.util.Comparator") || !proxy.closure.IdentityEqual(comparator) {
		t.Fatalf("double reverse = %s, want retained Comparator proxy around the original function", unwrapped.Describe())
	}
	invokePortableCollectionsUtilityForTest(t, ctx, "sort", ObjectValue(list), reversed)
	if got, want := argvValueStrings(list.snapshot()), []string{"3", "2", "1", "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reverse Comparator sort = %q, want %q", got, want)
	}
	if got, handled, err := wrapper.invokeContext(ctx, ObjectInvocation{
		Op: ObjectInvoke, Message: "reversed",
	}); err != nil || !handled || !got.IdentityEqual(unwrapped) {
		t.Fatalf("ReverseComparator2.reversed() = (%s, %t, %v), want retained Comparator proxy", got.Describe(), handled, err)
	}
}

func TestPortableCollectionsComparatorCallbackErrorsStayAuthoritative(t *testing.T) {
	want := errors.New("comparator failed")
	comparator := FunctionValue(CallableFunc(func(context.Context, ...Value) (Value, error) {
		return Null(), want
	}))
	list := ObjectValue(newPortableJavaCollection("ArrayList", []Value{Int(2), Int(1)}))
	handled, err := portableCollectionsUtilityErrorForTest(context.Background(), "sort", list, comparator)
	if !handled || !errors.Is(err, want) {
		t.Fatalf("Comparator callback error = (handled %t, %v), want authoritative callback error", handled, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	handled, err = portableCollectionsUtilityErrorForTest(canceled, "sort", list, comparator)
	if !handled || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Comparator sort = (handled %t, %v), want context.Canceled", handled, err)
	}

	proxy := &portableJavaProxy{
		closure: FunctionValue(CallableFunc(func(context.Context, ...Value) (Value, error) {
			return Null(), want
		})),
		interfaces: []string{"java.util.Comparator"},
	}
	first := &portableJavaReverseComparator2{comparator: ObjectValue(proxy)}
	second := &portableJavaReverseComparator2{comparator: ObjectValue(proxy)}
	_, handled, err = first.invokeContext(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "equals", Arguments: []Argument{{Value: ObjectValue(second)}},
	})
	if !handled || !errors.Is(err, want) {
		t.Fatalf("Comparator equals callback error = (handled %t, %v), want authoritative callback error", handled, err)
	}
	_, handled, err = first.invokeContext(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "hashCode",
	})
	if !handled || !errors.Is(err, want) {
		t.Fatalf("Comparator hashCode callback error = (handled %t, %v), want authoritative callback error", handled, err)
	}
}

const portableJavaCollectionsComparatorProbeSource = `import java.util.*;
$calls = 0;
$comparator = newInstance(^Comparator, {
   if ($0 eq "compare") {
      $calls++;
      return $1 <=> $2;
   }
});
$list = [new ArrayList: @(3, 1, 2, 1)];
[Collections sort: $list, $comparator];
println($list);
println("sort-calls=" . $calls);
$calls = 0;
println([Collections binarySearch: $list, 2, $comparator]);
println("search-calls=" . $calls);
$calls = 0;
println([Collections min: $list, $comparator]);
println([Collections max: $list, $comparator]);
println("extrema-calls=" . $calls);
$reversed = [Collections reverseOrder: $comparator];
$calls = 0;
[Collections sort: $list, $reversed];
println($list);
println("reverse-sort-calls=" . $calls);
println([Collections binarySearch: $list, 2, $reversed]);
println([Collections min: $list, $reversed]);
println([Collections max: $list, $reversed]);
$reverse0 = [Collections reverseOrder];
$reverse1 = [Collections reverseOrder: $null];
$natural = [Collections reverseOrder: $reverse0];
$reverse2 = [Collections reverseOrder: $natural];
println([$reverse0 getClass]);
println([$reverse1 getClass]);
println([$natural getClass]);
if ($natural isa ^Comparable) { println(1); } else { println(0); }
if ($reverse2 is $reverse0) { println(1); } else { println(0); }
$raw = {
   if ($0 eq "equals") { return "yes"; }
   return 0;
};
$rawWrapped = [Collections reverseOrder: $raw];
$rawRoundTrip = [Collections reverseOrder: $rawWrapped];
if ($rawRoundTrip isa ^Comparator) { println(1); } else { println(0); }
if ($rawRoundTrip isa ^java.lang.reflect.Proxy) { println(1); } else { println(0); }
if ($rawRoundTrip isa ^java.io.Serializable) { println(1); } else { println(0); }
if ($rawRoundTrip is $raw) { println(1); } else { println(0); }
if ([$rawRoundTrip getClass] is $null) { println(0); } else { println(1); }
println([$rawRoundTrip equals: $rawRoundTrip]);
$objectCalls = "";
$objectComparator = newInstance(^Comparator, {
   if ($0 eq "compare") { return $1 <=> $2; }
   if ($0 eq "equals") { $objectCalls .= "e"; return "yes"; }
   if ($0 eq "hashCode") { $objectCalls .= "h"; return 123; }
});
$objectReverse1 = [Collections reverseOrder: $objectComparator];
$objectReverse2 = [Collections reverseOrder: $objectComparator];
println([$objectReverse1 equals: $objectReverse2]);
println([$objectReverse1 hashCode]);
println($objectCalls);
`

const portableJavaCollectionsComparatorProbeOutput = `[1, 1, 2, 3]
sort-calls=6
2
search-calls=2
1
3
extrema-calls=6
[3, 2, 1, 1]
reverse-sort-calls=6
1
3
1
class java.util.Collections$ReverseComparator
class java.util.Collections$ReverseComparator
class java.util.Comparators$NaturalOrderComparator
1
1
1
1
1
0
1
0
0
-2147483525
eh
`

func TestPortableCollectionsComparatorRuntimeRouting(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-collections-comparator.sl", portableJavaCollectionsComparatorProbeSource); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != portableJavaCollectionsComparatorProbeOutput {
		t.Fatalf("runtime Comparator output\nwant:\n%sgot:\n%s", portableJavaCollectionsComparatorProbeOutput, got)
	}
}

func TestPortableCollectionsComparatorOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for Comparator differential verification")
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
	reference, err := osexec.Command(
		java,
		"--add-opens=java.base/java.util=ALL-UNNAMED",
		"-Dfile.encoding=UTF-8",
		"-jar", jar, "-e", portableJavaCollectionsComparatorProbeSource,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep Comparator probe: %v\n%s", err, reference)
	}
	if string(reference) != portableJavaCollectionsComparatorProbeOutput {
		t.Fatalf("official Sleep Comparator output changed\nwant:\n%sgot:\n%s", portableJavaCollectionsComparatorProbeOutput, reference)
	}
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-collections-comparator-differential.sl", portableJavaCollectionsComparatorProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official Sleep Comparator mismatch\nwant:\n%sgot:\n%s", reference, output.Bytes())
	}
}
