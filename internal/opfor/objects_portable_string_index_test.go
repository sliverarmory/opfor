package opfor

import (
	"bytes"
	"context"
	"math"
	"testing"
)

func TestPortableJavaStringCodePointIndexOverloads(t *testing.T) {
	target := String("A😀B😀")
	tests := []struct {
		method string
		args   []Value
		want   int32
	}{
		{method: "indexOf", args: []Value{Int(0x1f600)}, want: 1},
		{method: "indexOf", args: []Value{Int(0x1f600), Int(2)}, want: 4},
		{method: "lastIndexOf", args: []Value{Int(0x1f600)}, want: 4},
		{method: "lastIndexOf", args: []Value{Int(0x1f600), Int(2)}, want: 1},
		{method: "indexOf", args: []Value{Int(0xd83d)}, want: 1},
		{method: "indexOf", args: []Value{Int(0xde00)}, want: 2},
		{method: "indexOf", args: []Value{Int(-1)}, want: -1},
		{method: "indexOf", args: []Value{Int(0x110000)}, want: -1},
		{method: "indexOf", args: []Value{Int('B')}, want: 3},
		{method: "indexOf", args: []Value{Int('B'), Int(99)}, want: -1},
		{method: "indexOf", args: []Value{Int('B'), Int(math.MaxInt32)}, want: -1},
		{method: "indexOf", args: []Value{Int('B'), Int(-9)}, want: 3},
		{method: "indexOf", args: []Value{Double(66.9)}, want: 3},
		{method: "indexOf", args: []Value{String("66")}, want: -1},
		{method: "lastIndexOf", args: []Value{Int('A'), Int(-1)}, want: -1},
	}
	for _, test := range tests {
		got := invokePortableStringMethod(t, target, test.method, test.args...)
		if got.Int32() != test.want {
			t.Errorf("%s(%v) = %d, want %d", test.method, test.args, got.Int32(), test.want)
		}
	}

	boxed := ObjectValue(&portableJavaPrimitive{class: "java.lang.Integer", value: Int('B')})
	if got := invokePortableStringMethod(t, target, "indexOf", boxed); got.Int32() != 3 {
		t.Fatalf("indexOf(boxed Integer) = %d, want 3", got.Int32())
	}
	if result, handled, err := portableStringContext(context.Background(), stringMethodInvocation(target, "indexOf", Int('B'), String("2"))); err != nil || !handled || !result.IsNull() {
		t.Fatalf("indexOf(int, String) = (%s, %t, %v), want no-matching-method null", result.Describe(), handled, err)
	}
}

func TestPortableJavaStringContainsMutableCharSequence(t *testing.T) {
	builder := &portableJavaStringBuffer{
		class: "java.lang.StringBuilder",
		units: sleepStringUnits(String("😀B")),
		raw:   sleepStringRawMask(String("😀B")),
	}
	if got := invokePortableStringMethod(t, String("A😀B😀"), "contains", ObjectValue(builder)); !got.Truth() {
		t.Fatalf("contains(StringBuilder) = %s, want true", got.Describe())
	}
	invokePortableStringBuilder(t, builder, "append", String("missing"))
	if got := invokePortableStringMethod(t, String("A😀B😀"), "contains", ObjectValue(builder)); got.Truth() {
		t.Fatalf("contains(mutated StringBuilder) = %s, want false", got.Describe())
	}
}

const portableJavaStringCodePointIndexProbeSource = `$text = "A😀B😀";
println([$text indexOf: 128512]);
println([$text indexOf: 128512, 2]);
println([$text lastIndexOf: 128512]);
println([$text lastIndexOf: 128512, 2]);
println([$text indexOf: 55357]);
println([$text indexOf: 56832]);
println([$text indexOf: -1]);
println([$text indexOf: 1114112]);
println([$text indexOf: 66]);
println([$text indexOf: 66, 99]);
println([$text indexOf: 66, 2147483647]);
println([$text indexOf: 66, -9]);
println([$text indexOf: 66.9]);
println([$text indexOf: "66"]);
$boxed = [new java.lang.Integer: 66];
println([$text indexOf: $boxed]);
$builder = [new java.lang.StringBuilder: "😀B"];
println([$text contains: $builder]);
[$builder append: "missing"];
println([$text contains: $builder]);
println(["x" isEmpty]);
println([" " isBlank]);
println(["abc" matches: "z"]);
println(["abc" startsWith: "z"]);
println(["abc" equalsIgnoreCase: "ABD"]);
println(["abc" contentEquals: $builder]);
println(["abc" regionMatches: 0, "z", 0, 1]);
$emptyBuilder = [new java.lang.StringBuilder];
println([$emptyBuilder isEmpty]);
[$emptyBuilder append: "x"];
println([$emptyBuilder isEmpty]);
`

const portableJavaStringCodePointIndexProbeOutput = `1
4
4
1
1
2
-1
-1
3
-1
-1
3
3
-1
3
1
0
0
0
0
0
0
0
0
1
0
`

func TestPortableJavaStringCodePointIndexRuntimeRouting(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-string-code-point-index.sl", portableJavaStringCodePointIndexProbeSource); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != portableJavaStringCodePointIndexProbeOutput {
		t.Fatalf("runtime code-point index output\nwant:\n%sgot:\n%s", portableJavaStringCodePointIndexProbeOutput, got)
	}
}

func TestPortableJavaStringCodePointIndexOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	reference, err := officialSleepJavaCommand(java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaStringCodePointIndexProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep code-point index probe: %v\n%s", err, reference)
	}
	if string(reference) != portableJavaStringCodePointIndexProbeOutput {
		t.Fatalf("official Sleep code-point index output changed\nwant:\n%sgot:\n%s", portableJavaStringCodePointIndexProbeOutput, reference)
	}
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-string-code-point-index-differential.sl", portableJavaStringCodePointIndexProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official Sleep code-point index mismatch\nwant:\n%sgot:\n%s", reference, output.Bytes())
	}
}
