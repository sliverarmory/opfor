package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"sync"
	"testing"
)

func TestPortableJavaStackLIFO(t *testing.T) {
	stack := newPortableJavaCollection("Stack", nil)
	for _, class := range []string{
		"java.util.Stack", "java.util.Vector", "java.util.List", "java.util.RandomAccess",
		"java.lang.Cloneable", "java.io.Serializable", "java.util.Collection", "java.lang.Object",
	} {
		value, handled, err := stack.invoke(ObjectInvocation{Op: ObjectTypeCheck, Class: class})
		if err != nil || !handled || !value.Truth() {
			t.Fatalf("Stack isa %s = (%s, handled %v, %v)", class, value.Describe(), handled, err)
		}
	}
	for _, item := range []string{"bottom", "middle", "top"} {
		pushed := invokePortableCollectionForTest(t, stack, "push", String(item))
		if pushed.Kind() != KindString || pushed.String() != item {
			t.Fatalf("Stack.push(%q) = %s, want the pushed item", item, pushed.Describe())
		}
	}
	if got := stack.String(); got != "[bottom, middle, top]" {
		t.Fatalf("Stack storage order = %q", got)
	}
	if top := invokePortableCollectionForTest(t, stack, "peek"); top.String() != "top" {
		t.Fatalf("Stack.peek = %s, want top", top.Describe())
	}
	for item, distance := range map[string]int32{"top": 1, "bottom": 3, "missing": -1} {
		if got := invokePortableCollectionForTest(t, stack, "search", String(item)); got.Int32() != distance {
			t.Fatalf("Stack.search(%q) = %s, want %d", item, got.Describe(), distance)
		}
	}
	for _, want := range []string{"top", "middle", "bottom"} {
		if got := invokePortableCollectionForTest(t, stack, "pop"); got.String() != want {
			t.Fatalf("Stack.pop = %s, want %q", got.Describe(), want)
		}
	}
	if empty := invokePortableCollectionForTest(t, stack, "empty"); empty.Kind() != KindInt || empty.Int32() != 1 {
		t.Fatalf("Stack.empty = %s, want Java boolean integer 1", empty.Describe())
	}
	for _, message := range []string{"peek", "pop"} {
		if err := invokePortableCollectionErrorForTest(stack, message); err == nil || err.Error() != "java.util.EmptyStackException" {
			t.Fatalf("empty Stack.%s = %v", message, err)
		}
	}
}

func TestPortableJavaStackRaceSafe(t *testing.T) {
	stack := newPortableJavaCollection("Stack", nil)
	const workers = 8
	const iterations = 200
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				value := String(fmt.Sprintf("%d/%d", worker, iteration))
				_, _, _ = stack.invoke(ObjectInvocation{
					Op: ObjectInvoke, Message: "push", Arguments: []Argument{{Value: value}},
				})
				_, _, _ = stack.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "peek"})
				_, _, _ = stack.invoke(ObjectInvocation{
					Op: ObjectInvoke, Message: "search", Arguments: []Argument{{Value: value}},
				})
			}
		}(worker)
	}
	wait.Wait()
	if size := invokePortableCollectionForTest(t, stack, "size"); size.Int32() != workers*iterations {
		t.Fatalf("concurrent Stack size = %s, want %d", size.Describe(), workers*iterations)
	}
}

const portableJavaStackProbeSource = `$loader = [new ScriptLoader];
$shared = [new Hashtable];
$script = [$loader loadScript: "stack-probe", 'return 1;', $shared];
$s = [[$script getScriptEnvironment] getEnvironmentStack];
$stackType = 0; if ($s isa ^java.util.Stack) { $stackType = 1; }
$vectorType = 0; if ($s isa ^java.util.Vector) { $vectorType = 1; }
$listType = 0; if ($s isa ^java.util.List) { $listType = 1; }
$randomType = 0; if ($s isa ^java.util.RandomAccess) { $randomType = 1; }
$cloneType = 0; if ($s isa ^java.lang.Cloneable) { $cloneType = 1; }
$serialType = 0; if ($s isa ^java.io.Serializable) { $serialType = 1; }
$collectionType = 0; if ($s isa ^java.util.Collection) { $collectionType = 1; }
$objectType = 0; if ($s isa ^java.lang.Object) { $objectType = 1; }
println("types=" . $stackType . "/" . $vectorType . "/" . $listType . "/" . $randomType . "/" . $cloneType . "/" . $serialType . "/" . $collectionType . "/" . $objectType);
$a = [$s push: "bottom"];
$b = [$s push: "middle"];
$c = [$s push: "top"];
println("push=" . $a . "/" . $b . "/" . $c);
println("state=" . $s . "/" . [$s get: 0] . "/" . [$s get: 2]);
println("peek=" . [$s peek]);
println("search=" . [$s search: "top"] . "/" . [$s search: "bottom"] . "/" . [$s search: "missing"]);
$p1 = [$s pop];
$p2 = [$s pop];
$p3 = [$s pop];
println("pop=" . $p1 . "/" . $p2 . "/" . $p3);
println("empty=" . [$s empty]);
[$s peek];
println(checkError());
[$s pop];
println(checkError());
`

const portableJavaStackProbeOutput = `types=1/1/1/1/1/1/1/1
push=bottom/middle/top
state=[bottom, middle, top]/bottom/top
peek=top
search=1/3/-1
pop=top/middle/bottom
empty=1
java.util.EmptyStackException
java.util.EmptyStackException
`

func TestPortableJavaStackRuntimeRouting(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-stack.sl", portableJavaStackProbeSource); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != portableJavaStackProbeOutput {
		t.Fatalf("runtime Stack output\nwant:\n%sgot:\n%s", portableJavaStackProbeOutput, got)
	}
}

func TestPortableJavaStackOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for Stack differential verification")
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
	reference, err := osexec.Command(java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", portableJavaStackProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep Stack probe: %v\n%s", err, reference)
	}
	if string(reference) != portableJavaStackProbeOutput {
		t.Fatalf("official Sleep Stack output changed\nwant:\n%sgot:\n%s", portableJavaStackProbeOutput, reference)
	}
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "java-stack-differential.sl", portableJavaStackProbeSource); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official Sleep Stack mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}
