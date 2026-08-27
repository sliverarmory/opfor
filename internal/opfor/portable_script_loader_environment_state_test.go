package opfor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const portableScriptEnvironmentFirstRunHelper = "OPFOR_SCRIPT_ENVIRONMENT_FIRST_RUN_HELPER"

func TestPortableScriptEnvironmentSetEnvironmentDuringFirstRun(t *testing.T) {
	if mode := os.Getenv(portableScriptEnvironmentFirstRunHelper); mode != "" {
		var output strings.Builder
		output.WriteString(executePortableScriptEnvironmentProbe(t, portableScriptEnvironmentParentClosureProbe()))
		if mode == "all" {
			output.WriteString(executePortableScriptEnvironmentProbe(t, portableScriptEnvironmentFirstLoadRoutingProbe()))
		}
		fmt.Printf("OPFOR_FIRST_RUN_BEGIN\n%sOPFOR_FIRST_RUN_END\n", output.String())
		return
	}

	got := runPortableScriptEnvironmentFirstRunHelper(t, "all")
	want := "7\ndone\nvalue=47\nsame=1\nnext=47\n"
	if got != want {
		t.Fatalf("first-run setEnvironment output\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func runPortableScriptEnvironmentFirstRunHelper(t *testing.T, mode string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := osexec.CommandContext(ctx, os.Args[0], "-test.run=^TestPortableScriptEnvironmentSetEnvironmentDuringFirstRun$")
	command.Env = append(os.Environ(), portableScriptEnvironmentFirstRunHelper+"="+mode)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("first-run setEnvironment helper exceeded bounded timeout: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("first-run setEnvironment helper: %v\n%s", err, output)
	}
	const begin = "OPFOR_FIRST_RUN_BEGIN\n"
	const end = "OPFOR_FIRST_RUN_END\n"
	start := strings.Index(string(output), begin)
	finish := strings.Index(string(output), end)
	if start < 0 || finish < start+len(begin) {
		t.Fatalf("first-run setEnvironment helper omitted output markers:\n%s", output)
	}
	return string(output)[start+len(begin) : finish]
}

func executePortableScriptEnvironmentProbe(t *testing.T, source string) string {
	t.Helper()
	program, err := CompileString("script-environment-first-run.sl", source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		_ = runtime.Close(context.Background())
		t.Fatalf("Execute: %v\n%s", err, output.String())
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return output.String()
}

func portableScriptEnvironmentParentClosureProbe() string {
	return `import java.util.Hashtable;
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$shared = [new Hashtable];
$child = [$loader loadScript: "x", 'swap(); return 7;', $shared];
$env = [$child getScriptEnvironment];
$replacement = [new Hashtable];
[$replacement putAll: $shared];
[$shared put: "&swap", lambda({ [$saved_env setEnvironment: $saved_replacement]; }, $saved_env => $env, $saved_replacement => $replacement)];
println([$child runScript]);
println("done");
`
}

func portableScriptEnvironmentFirstLoadRoutingProbe() string {
	return `import java.util.Hashtable;
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$shared = [new Hashtable];
$child = [$loader loadScript: "x",
    '$e = get_env(); $r = get_replacement(); [$e setEnvironment: $r]; return swapped();',
    $shared];
$env = [$child getScriptEnvironment];
$replacement = [new Hashtable];
[$replacement putAll: $shared];
[$replacement put: "&swapped", { return 47; }];
[$shared put: "&get_env", lambda({ return $saved_env; }, $saved_env => $env)];
[$shared put: "&get_replacement", lambda({ return $saved_replacement; }, $saved_replacement => $replacement)];

$value = [$child runScript];
println("value=" . $value);
$same = 0;
if ([$env getEnvironment] is $replacement) { $same = 1; }
println("same=" . $same);
println("next=" . [$env evaluateExpression: 'swapped()']);
`
}

func TestPortableScriptEnvironmentMutableTableAndStackDirect(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	loader := &portableScriptLoader{runtime: runtime}
	initial := newPortableJavaMap("Hashtable", nil)
	initialShared := portableSharedEnvironment(initial)
	initialShared.installGlobalBridges(loader, runtime)
	replacement := newPortableJavaMap("Hashtable", nil)
	if _, handled, invokeErr := replacement.invoke(ObjectInvocation{
		Op: ObjectInvoke, Message: "putAll",
		Arguments: []Argument{{Value: ObjectValue(initial)}},
	}); !handled || invokeErr != nil {
		t.Fatalf("replacement.putAll(initial) = (handled %v, %v)", handled, invokeErr)
	}

	instance := &portableScriptInstance{loader: loader, shared: initialShared}
	environment := &portableScriptEnvironment{
		instance:         instance,
		table:            initial,
		environmentStack: newPortableJavaCollection("Stack", nil),
	}
	instance.env = environment

	firstStack, handled, err := environment.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "getEnvironmentStack",
	})
	if !handled || err != nil {
		t.Fatalf("getEnvironmentStack = (handled %v, %v)", handled, err)
	}
	secondStack, _, err := environment.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "getEnvironmentStack",
	})
	if err != nil || !firstStack.IdentityEqual(secondStack) {
		t.Fatalf("repeated stack identity = (%s, %s, %v), want same object", firstStack.Describe(), secondStack.Describe(), err)
	}
	stackObject, ok := firstStack.Object()
	if !ok {
		t.Fatalf("stack = %s, want object", firstStack.Describe())
	}
	stack := stackObject.(*portableJavaCollection)
	for _, class := range []string{
		"java.util.Stack", "java.util.Vector", "java.util.List", "java.util.RandomAccess",
		"java.lang.Cloneable", "java.io.Serializable", "java.util.Collection",
	} {
		value, typeHandled, typeErr := stack.invoke(ObjectInvocation{Op: ObjectTypeCheck, Class: class})
		if !typeHandled || typeErr != nil || !value.Truth() {
			t.Fatalf("stack isa %s = (%s, handled %v, %v), want true", class, value.Describe(), typeHandled, typeErr)
		}
	}
	if _, addHandled, addErr := stack.invoke(ObjectInvocation{
		Op: ObjectInvoke, Message: "add", Arguments: []Argument{{Value: String("live")}},
	}); !addHandled || addErr != nil {
		t.Fatalf("stack.add = (handled %v, %v)", addHandled, addErr)
	}
	liveStack, _, _ := environment.invoke(context.Background(), ObjectInvocation{Op: ObjectInvoke, Message: "getEnvironmentStack"})
	liveObject, _ := liveStack.Object()
	live, _, liveErr := liveObject.(*portableJavaCollection).invoke(ObjectInvocation{
		Op: ObjectInvoke, Message: "get", Arguments: []Argument{{Value: Int(0)}},
	})
	if liveErr != nil || live.String() != "live" {
		t.Fatalf("live stack value = (%s, %v), want live", live.Describe(), liveErr)
	}
	pushed, pushHandled, pushErr := stack.invoke(ObjectInvocation{
		Op: ObjectInvoke, Message: "push", Arguments: []Argument{{Value: String("top")}},
	})
	if !pushHandled || pushErr != nil || pushed.String() != "top" {
		t.Fatalf("ScriptEnvironment Stack.push = (%s, handled %v, %v)", pushed.Describe(), pushHandled, pushErr)
	}
	if top, _, topErr := stack.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "peek"}); topErr != nil || top.String() != "top" {
		t.Fatalf("ScriptEnvironment Stack.peek = (%s, %v), want top", top.Describe(), topErr)
	}
	if top, _, topErr := stack.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "pop"}); topErr != nil || top.String() != "top" {
		t.Fatalf("ScriptEnvironment Stack.pop = (%s, %v), want top", top.Describe(), topErr)
	}
	if top, _, topErr := stack.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "peek"}); topErr != nil || top.String() != "live" {
		t.Fatalf("ScriptEnvironment Stack.peek after pop = (%s, %v), want live", top.Describe(), topErr)
	}

	sibling := &portableScriptEnvironment{}
	siblingStack, _, _ := sibling.invoke(context.Background(), ObjectInvocation{Op: ObjectInvoke, Message: "getEnvironmentStack"})
	if firstStack.IdentityEqual(siblingStack) {
		t.Fatal("sibling ScriptEnvironments shared an environment stack")
	}

	value, handled, err := environment.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "setEnvironment",
		Arguments: []Argument{{Value: ObjectValue(replacement)}},
	})
	if !handled || err != nil || !value.IsNull() {
		t.Fatalf("setEnvironment(replacement) = (%s, handled %v, %v)", value.Describe(), handled, err)
	}
	gotTable, _, err := environment.invoke(context.Background(), ObjectInvocation{Op: ObjectInvoke, Message: "getEnvironment"})
	if err != nil || !gotTable.IdentityEqual(ObjectValue(replacement)) {
		t.Fatalf("getEnvironment after replacement = (%s, %v), want exact replacement", gotTable.Describe(), err)
	}
	if instance.shared == nil || instance.shared.table != replacement {
		t.Fatal("ScriptInstance execution routing did not follow the replacement table")
	}
	if _, typedErr := portableScriptEnvironmentTypedEntry(environment.environmentTable(), String("&abs"), "sleep.interfaces.Function"); typedErr != nil {
		t.Fatalf("replacement typed entry: %v", typedErr)
	}

	if value, handled, err = environment.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "setEnvironment", Arguments: []Argument{{Value: Null()}},
	}); !handled || err != nil || !value.IsNull() {
		t.Fatalf("setEnvironment(null) = (%s, handled %v, %v)", value.Describe(), handled, err)
	}
	gotTable, _, err = environment.invoke(context.Background(), ObjectInvocation{Op: ObjectInvoke, Message: "getEnvironment"})
	if err != nil || !gotTable.IsNull() || environment.environmentTable() != nil || instance.shared != nil {
		t.Fatalf("getEnvironment after null = (%s, %v), table=%p shared=%p", gotTable.Describe(), err, environment.environmentTable(), instance.shared)
	}
	missing, _, typedErr := environment.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "getFunction", Arguments: []Argument{{Value: String("&abs")}},
	})
	if typedErr != nil || !missing.IsNull() {
		t.Fatalf("getFunction with null environment = (%s, %v), want null", missing.Describe(), typedErr)
	}

	environment.setEnvironmentTable(context.Background(), replacement)
	wrong := ObjectValue(newPortableJavaCollection("ArrayList", nil))
	value, handled, err = environment.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "setEnvironment", Arguments: []Argument{{Value: wrong}},
	})
	if !handled || err != nil || !value.IsNull() {
		t.Fatalf("setEnvironment(ArrayList) = (%s, handled %v, %v), want soft no-match", value.Describe(), handled, err)
	}
	if environment.environmentTable() != replacement || instance.shared == nil || instance.shared.table != replacement {
		t.Fatal("wrong-typed setEnvironment changed the active table")
	}
	finalStack, _, _ := environment.invoke(context.Background(), ObjectInvocation{Op: ObjectInvoke, Message: "getEnvironmentStack"})
	if !finalStack.IdentityEqual(firstStack) {
		t.Fatal("setEnvironment replaced the independent environment stack")
	}
}

func TestPortableScriptEnvironmentMutableStateConcurrent(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	loader := &portableScriptLoader{runtime: runtime}
	tables := []*portableJavaMap{
		newPortableJavaMap("Hashtable", nil),
		newPortableJavaMap("Hashtable", nil),
	}
	for _, table := range tables {
		portableSharedEnvironment(table).installGlobalBridges(loader, runtime)
	}
	instance := &portableScriptInstance{loader: loader, shared: portableSharedEnvironment(tables[0])}
	environment := &portableScriptEnvironment{instance: instance, table: tables[0]}
	instance.env = environment

	const workers = 16
	const iterations = 200
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(offset int) {
			defer wait.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				table := tables[(offset+iteration)%len(tables)]
				_, _, _ = environment.invoke(context.Background(), ObjectInvocation{
					Op: ObjectInvoke, Message: "setEnvironment",
					Arguments: []Argument{{Value: ObjectValue(table)}},
				})
				_, _, _ = environment.invoke(context.Background(), ObjectInvocation{
					Op: ObjectInvoke, Message: "getFunction",
					Arguments: []Argument{{Value: String("&abs")}},
				})
				stackValue, _, _ := environment.invoke(context.Background(), ObjectInvocation{
					Op: ObjectInvoke, Message: "getEnvironmentStack",
				})
				stackObject, _ := stackValue.Object()
				_, _, _ = stackObject.(*portableJavaCollection).invoke(ObjectInvocation{
					Op: ObjectInvoke, Message: "push", Arguments: []Argument{{Value: Int(int32(iteration))}},
				})
			}
		}(worker)
	}
	wait.Wait()
	stack := environment.getEnvironmentStack()
	size, handled, err := stack.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "size"})
	if !handled || err != nil || size.Int32() != workers*iterations {
		t.Fatalf("concurrent stack size = (%s, handled %v, %v), want %d", size.Describe(), handled, err, workers*iterations)
	}
}

func TestOfficialSleepPortableScriptEnvironmentMutableTableAndStack(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	mainPath := filepath.Join(directory, "loader-environment-state.sl")
	source := portableScriptEnvironmentMutationProbe()
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-Dfile.encoding=UTF-8", "-jar", jar, mainPath)
	reference, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep: %v\n%s", err, reference)
	}

	program, err := CompileString(mainPath, source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official mutable ScriptEnvironment mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}

func TestOfficialSleepPortableScriptEnvironmentSetEnvironmentDuringFirstRun(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	var reference bytes.Buffer
	for index, source := range []string{
		portableScriptEnvironmentParentClosureProbe(),
		portableScriptEnvironmentFirstLoadRoutingProbe(),
	} {
		mainPath := filepath.Join(directory, fmt.Sprintf("loader-environment-first-run-%d.sl", index))
		if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		command := officialSleepJavaCommandContext(ctx, java, "-Dfile.encoding=UTF-8", "-jar", jar, mainPath)
		output, commandErr := command.CombinedOutput()
		deadlineErr := ctx.Err()
		cancel()
		if deadlineErr != nil {
			t.Fatalf("official Sleep probe %d exceeded bounded timeout: %v\n%s", index, deadlineErr, output)
		}
		if commandErr != nil {
			t.Fatalf("official Sleep probe %d: %v\n%s", index, commandErr, output)
		}
		reference.Write(output)
	}

	got := []byte(runPortableScriptEnvironmentFirstRunHelper(t, "all"))
	if !bytes.Equal(got, reference.Bytes()) {
		t.Fatalf("official first-run ScriptEnvironment mismatch\nwant:\n%s\ngot:\n%s", reference.Bytes(), got)
	}
}

func portableScriptEnvironmentMutationProbe() string {
	return `import java.util.ArrayList;
import java.util.Hashtable;
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$shared = [new Hashtable];
$one = [$loader loadScript: "one", 'return 1;', $shared];
$two = [$loader loadScript: "two", 'return 2;', $shared];
$e1 = [$one getScriptEnvironment];
$e2 = [$two getScriptEnvironment];
$s11 = [$e1 getEnvironmentStack];
$s12 = [$e1 getEnvironmentStack];
$s2 = [$e2 getEnvironmentStack];
$same = 0; if ($s11 is $s12) { $same = 1; }
$isolated = 0; if ($s11 is $s2) { $isolated = 1; }
$stacktype = 0; if ($s11 isa ^java.util.Stack) { $stacktype = 1; }
println("stack=" . $same . "/" . $isolated . "/" . $stacktype . "/" . [$s11 size] . "/" . [$s2 size]);
[$s11 add: "x"];
println("live=" . [[$e1 getEnvironmentStack] get: 0] . "/" . [[$e1 getEnvironmentStack] size] . "/" . [$s2 size]);
$pushed = [$s11 push: "top"];
$peeked = [$s11 peek];
$popped = [$s11 pop];
$remaining = [$s11 peek];
println("stacktop=" . $pushed . "/" . $peeked . "/" . $popped . "/" . $remaining);
$old = [$e1 getEnvironment];
$replacement = [new Hashtable];
[$replacement putAll: $old];
[$replacement put: "marker", "yes"];
[$replacement put: "&swapped", { return 47; }];
[$e1 setEnvironment: $replacement];
$new = [$e1 getEnvironment];
$envsame = 0; if ($new is $replacement) { $envsame = 1; }
$oldsame = 0; if ($new is $old) { $oldsame = 1; }
$siblingold = 0; if ([$e2 getEnvironment] is $old) { $siblingold = 1; }
$envtype = 0; if ($new isa ^java.util.Hashtable) { $envtype = 1; }
$dispatch = [$e1 evaluateExpression: 'swapped()'];
println("environment=" . $envsame . "/" . $oldsame . "/" . $siblingold . "/" . [$new get: "marker"] . "/" . $envtype . "/" . $dispatch);
$original_abs = [$replacement put: "&abs", "wrong"];
$bad = [$e1 getFunction: "&abs"];
checkError($cast_problem);
$casttype = 0; if ($cast_problem isa ^java.lang.ClassCastException) { $casttype = 1; }
[$replacement put: "&abs", $original_abs];
println("cast=" . $casttype . "/" . $bad);
$capture = [new ArrayList];
[$e1 setEnvironment: $null];
[$capture add: [$e1 getEnvironment]];
$null_eval = [$e1 evaluateExpression: '1'];
checkError($null_eval_problem);
$null_run = [$one runScript];
checkError($null_run_problem);
[$e1 setEnvironment: $replacement];
$wasnull = 0; if ([$capture get: 0] is $null) { $wasnull = 1; }
$stacksame = 0; if ([$e1 getEnvironmentStack] is $s11) { $stacksame = 1; }
$evalnull = 0; if ($null_eval_problem isa ^java.lang.NullPointerException) { $evalnull = 1; }
$runnull = 0; if ($null_run_problem isa ^java.lang.NullPointerException) { $runnull = 1; }
println("null=" . $wasnull . "/" . $stacksame . "/" . $evalnull . "/" . $runnull . "/" . $null_eval . "/" . $null_run . "/" . [[$e1 getEnvironmentStack] get: 0]);
[$e1 setEnvironment: [new ArrayList]];
`
}
