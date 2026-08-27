package opfor

import (
	"bytes"
	"context"
	osexec "os/exec"
	"testing"
)

const portableJavaArrayBoundaryProbeSource = `
debug(0);
import java.lang.reflect.Array;
$converted = [Array newInstance: ^String, 2];
println($converted);
println([$converted getClass]);
println(size($converted));
push($converted, "x");
println($converted);
$ignored = [Array getLength: $converted];
println(checkError());
$raw = cast(@(1, 2, 3), "i");
println([$raw getClass]);
println([Array getLength: $raw]);
[Array set: $raw, 1, 9];
println([Array get: $raw, 1]);
println(size($raw));
@too_many = @();
for ($index = 0; $index < 256; $index++) { push(@too_many, 0); }
$too_many_result = [Array newInstance: ^String, @too_many];
$too_many_error = checkError();
println([$too_many_error getClass]);
println($too_many_error);
@component_dimensions = @();
for ($index = 0; $index < 255; $index++) { push(@component_dimensions, 0); }
$component_result = [Array newInstance: [$raw getClass], @component_dimensions];
$component_error = checkError();
println([$component_error getClass]);
println($component_error);
`

const portableJavaArrayBoundaryProbeOutput = "@($null, $null)\n" +
	"class sleep.engine.types.ListContainer\n" +
	"2\n" +
	"@($null, $null, 'x')\n" +
	"java.lang.IllegalArgumentException: Argument is not an array\n" +
	"class [I\n" +
	"3\n" +
	"9\n" +
	"\n" +
	"class java.lang.IllegalArgumentException\n" +
	"java.lang.IllegalArgumentException\n" +
	"class java.lang.IllegalArgumentException\n" +
	"java.lang.IllegalArgumentException\n"

func TestPortableJavaArrayReturnAndRawCastBoundary(t *testing.T) {
	t.Parallel()
	got := runPortableJavaArrayBoundaryProbe(t)
	if !bytes.Equal(got, []byte(portableJavaArrayBoundaryProbeOutput)) {
		t.Fatalf("portable Java array boundary mismatch\nwant:\n%sgot:\n%s", portableJavaArrayBoundaryProbeOutput, got)
	}
}

func TestPortableJavaArrayDimensionAndAllocationBounds(t *testing.T) {
	dimensions := make([]Value, portableJavaArrayMaximumDimensions+1)
	for index := range dimensions {
		dimensions[index] = Int(0)
	}
	invoke := func(component string, dimensionValue Value) (Value, bool, error) {
		return portableJavaReflectArray(ObjectInvocation{
			Op: ObjectInvoke, Message: "newInstance",
			Arguments: []Argument{
				{Value: ObjectValue(classReference(component))},
				{Value: dimensionValue},
			},
		})
	}

	value, handled, err := invoke("java.lang.String", ArrayValue(NewArray(dimensions...)))
	if !handled || !value.IsNull() || err == nil || err.Error() != "java.lang.IllegalArgumentException" {
		t.Fatalf("256-dimensional array = (%s, %t, %v), want IllegalArgumentException", value.Describe(), handled, err)
	}

	value, handled, err = invoke("[I", ArrayValue(NewArray(dimensions[:portableJavaArrayMaximumDimensions]...)))
	if !handled || !value.IsNull() || err == nil || err.Error() != "java.lang.IllegalArgumentException" {
		t.Fatalf("array-component total dimensions = (%s, %t, %v), want IllegalArgumentException", value.Describe(), handled, err)
	}

	value, handled, err = invoke("java.lang.String", Int(portableJavaArrayMaximumElements+1))
	if !handled || !value.IsNull() || err == nil || err.Error() != "java.lang.OutOfMemoryError: Required array size exceeds implementation limit" {
		t.Fatalf("oversized array = (%s, %t, %v), want bounded OutOfMemoryError", value.Describe(), handled, err)
	}

	if length, err := portableJavaArrayAllocationLength([]int{portableJavaArrayMaximumElements, 0}); err != nil || length != 0 {
		t.Fatalf("bounded zero-tail allocation = (%d, %v), want zero leaf allocation", length, err)
	}
	if _, err := portableJavaArrayAllocationLength([]int{portableJavaArrayMaximumElements, 1}); err == nil {
		t.Fatal("recursive materialization beyond the aggregate bound unexpectedly succeeded")
	}
}

func TestPortableJavaArrayReturnAndRawCastOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	want, err := osexec.Command(
		java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaArrayBoundaryProbeSource,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep Java-array probe: %v\n%s", err, want)
	}
	got := runPortableJavaArrayBoundaryProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep Java-array mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runPortableJavaArrayBoundaryProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-array-boundary.sl", portableJavaArrayBoundaryProbeSource); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}
