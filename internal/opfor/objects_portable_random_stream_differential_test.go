package opfor

import (
	"bytes"
	"context"
	osexec "os/exec"
	"testing"
)

const portableJavaRandomStreamDifferentialSource = `
debug(0);
import java.util.random.*;
import java.util.stream.*;

# Stream construction is lazy: the direct draw remains the first value, and
# traversal then continues from the same Random object.
$random = [new Random: 0L];
$lazy = [$random ints: 3L];
println("lazy-direct=" . [$random nextInt]);
println("lazy-stream=" . join(",", [$lazy toArray]));
println("lazy-after=" . [$random nextInt]);

# Random's spliterator is SIZED, so count returns the fence without drawing.
$random = [new Random: 0L];
$sized = [$random longs: 4L];
println("sized-count=" . [$sized count]);
println("sized-after=" . [$random nextInt]);

# Exercise each primitive bounded source and its OpenJDK state transition.
$random = [new Random: 0L];
println("bounded-ints=" . join(",", [[$random ints: 4L, -10, 10] toArray]));
println("bounded-ints-after=" . [$random nextInt]);
$random = [new Random: 0L];
println("bounded-longs=" . join(",", [[$random longs: 3L, -20L, 30L] toArray]));
println("bounded-longs-after=" . [$random nextInt]);
$random = [new Random: 0L];
println("bounded-doubles=" . join(",", [[$random doubles: 3L, -2.0, 3.0] toArray]));
println("bounded-doubles-after=" . [$random nextInt]);

# Spliterators.iterator().hasNext() advances once and caches that value.
$random = [new Random: 0L];
$iterator = [[$random ints: 2L] iterator];
println("iterator-has-1=" . [$iterator hasNext]);
println("iterator-has-2=" . [$iterator hasNext]);
println("iterator-direct=" . [$random nextInt]);
println("iterator-cached=" . [$iterator nextInt]);
println("iterator-next=" . [$iterator nextInt]);
println("iterator-done=[" . [$iterator hasNext] . "]");

# The portable static factory covers the classic Random algorithm exactly.
$factory = [RandomGenerator of: "Random"];
[$factory setSeed: 0L];
println("factory-class=" . [[$factory getClass] getName]);
println("factory-next=" . [$factory nextInt]);

# Java Boolean false crosses the Sleep reflection boundary as integer scalar 0.
$random = [new Random: 4096L];
println("nextBoolean=[" . [$random nextBoolean] . "]");
println("isDeprecated=[" . [$random isDeprecated] . "]");
`

func TestPortableJavaRandomStreamsAndFactoryOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	command := osexec.Command(
		java,
		"--add-opens=java.base/java.util=ALL-UNNAMED",
		"--add-opens=java.base/java.util.random=ALL-UNNAMED",
		"--add-opens=java.base/java.util.stream=ALL-UNNAMED",
		"-Dfile.encoding=UTF-8",
		"-jar", jar, "-e", portableJavaRandomStreamDifferentialSource,
	)
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep Random stream/factory probe: %v\n%s", err, want)
	}

	var got bytes.Buffer
	runtimeInstance, err := New(WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "random-stream-factory-differential.sl", portableJavaRandomStreamDifferentialSource); err != nil {
		t.Fatalf("pure-Go Random stream/factory probe: %v\n%s", err, got.String())
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("official Random stream/factory mismatch\nwant:\n%s\ngot:\n%s", want, got.Bytes())
	}
}
