package opfor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

const processObjectHelperMarker = "OPFOR_PROCESS_OBJECT_HELPER"

func TestExecProcessIsDuplexAndWaitReturnsRepeatableExitStatus(t *testing.T) {
	var diagnostics bytes.Buffer
	runtime, err := New(WithStderr(&diagnostics))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("duplex-owner.sl", "return 1;\n")
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	defer script.Unload(context.Background())
	functions := runtime.ioFunctions()
	workingDirectory := t.TempDir()
	mustCallIOBuiltin(t, runtime, functions, "chdir", String(workingDirectory))
	t.Setenv("OPFOR_PROCESS_VALUE", "inherited")

	execContext, cancelExec := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelExec()
	handle, err := callIOBuiltinForScript(execContext, runtime, functions, script.ID(), "exec", processHelperCommand("duplex"))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, String("ping\n"))
	mustCallIOBuiltin(t, runtime, functions, "printEOF", handle)

	lines := arrayStrings(t, mustCallIOBuiltin(t, runtime, functions, "readAll", handle))
	want := []string{"stdin:ping", "cwd:" + workingDirectory, "env:inherited"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("process output = %q, want %q", lines, want)
	}
	for _, timeout := range []Value{Int(-1), Int(1)} {
		status, waitErr := callIOBuiltinForScript(context.Background(), runtime, functions, script.ID(), "wait", handle, timeout)
		if waitErr != nil {
			t.Fatalf("wait(%s): %v", timeout.Describe(), waitErr)
		}
		if status.Int32() != 13 {
			t.Fatalf("wait(%s) = %s, want 13", timeout.Describe(), status.Describe())
		}
	}
	if got := diagnostics.String(); got != "" {
		t.Fatalf("child stderr escaped process object: %q", got)
	}
	if problem, err := runtime.checkError(context.Background(), Invocation{Runtime: runtime, Script: script.ID(), Name: "checkError"}); err != nil || !problem.IsNull() {
		t.Fatalf("nonzero exit checkError = %s, %v; want $null", problem.Describe(), err)
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
}

func TestExecWaitIgnoresIOThreadTimeout(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	functions := runtime.ioFunctions()
	execContext, cancelExec := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelExec()
	handle, err := callIOBuiltin(execContext, runtime, functions, "exec", processHelperCommand("delay"))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	waitContext, cancelWait := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelWait()
	status, err := callIOBuiltin(waitContext, runtime, functions, "wait", handle, Int(1))
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if status.Int32() != 4 {
		t.Fatalf("wait = %s, want 4", status.Describe())
	}
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
		t.Fatalf("process wait honored the 1ms IO-thread timeout; returned after %v", elapsed)
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
}

func TestExecWaitReapsProcessAfterIOWorkerDebugThrow(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("process-wait-debug-owner.sl", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Unload(context.Background()) })
	if _, err := owner.SetDebugFlags(34); err != nil {
		t.Fatal(err)
	}
	functions := runtimeInstance.ioFunctions()
	handleValue, err := callIOBuiltinForScript(context.Background(), runtimeInstance, functions, owner.ID(), "exec", processHelperCommand("delay"))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	handle, ok := ioHandleValue(handleValue)
	if !ok || handle.getProcess() == nil {
		t.Fatalf("exec returned %s without a process", handleValue.Describe())
	}
	callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) { return Null(), nil }))
	if _, err := callIOBuiltinForScript(context.Background(), runtimeInstance, functions, owner.ID(), "read", handleValue, callback); err != nil {
		t.Fatalf("read: %v", err)
	}

	started := time.Now()
	result, waitErr := runtimeInstance.wait(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  owner.ID(),
		Name:    "wait",
		Arguments: []Argument{
			{Value: handleValue},
			{Value: Int(-1)},
		},
	})
	var thrown *scriptThrow
	if !errors.As(waitErr, &thrown) {
		t.Fatalf("wait error = %v, want debug-flow script throw", waitErr)
	}
	if result.Int32() != 4 {
		t.Fatalf("wait result = %s, want process exit 4", result.Describe())
	}
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
		t.Fatalf("wait returned before process completion after %v", elapsed)
	}
	select {
	case <-handle.getProcess().done:
	default:
		t.Fatal("wait returned before process was reaped")
	}
}

func TestExecArgumentsMatchSleepScalarArrayEnvironmentAndDirectoryRules(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	state := &ioBuiltinState{runtime: runtime, cwd: t.TempDir()}
	environment := NewOrderedHash()
	environment.Set("FIRST", String("one"))
	environment.Set("SECOND", Int(2))
	explicitDirectory := filepath.Join(state.workingDirectory(), "nested")

	spec, err := state.processArguments(invocationWithValues("exec",
		String("tool first  second\tthird "), HashValue(environment), String("nested")))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tool", "first", "", "second", "third"}; !reflect.DeepEqual(spec.command, want) {
		t.Fatalf("scalar command = %#v, want %#v", spec.command, want)
	}
	if want := []string{"FIRST=one", "SECOND=2"}; !reflect.DeepEqual(spec.environment, want) {
		t.Fatalf("environment = %#v, want %#v", spec.environment, want)
	}
	if spec.directory != explicitDirectory {
		t.Fatalf("directory = %q, want %q", spec.directory, explicitDirectory)
	}

	arraySpec, err := state.processArguments(invocationWithValues("exec",
		ArrayValue(NewArray(String("tool"), String("argument with spaces"), String("")))))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tool", "argument with spaces", ""}; !reflect.DeepEqual(arraySpec.command, want) {
		t.Fatalf("array command = %#v, want %#v", arraySpec.command, want)
	}
	if arraySpec.environment != nil {
		t.Fatalf("omitted environment = %#v, want inherited nil", arraySpec.environment)
	}
	if arraySpec.directory != state.workingDirectory() {
		t.Fatalf("omitted directory = %q, want runtime cwd", arraySpec.directory)
	}
}

func TestExecLaunchFailureIsASoftErrorAndReturnsInertHandle(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("process-owner.sl", "return 1;\n")
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	defer script.Unload(context.Background())

	missing := filepath.Join(t.TempDir(), "definitely-not-an-executable")
	functions := runtime.ioFunctions()
	value, err := callIOBuiltinForScript(context.Background(), runtime, functions, script.ID(), "exec",
		ArrayValue(NewArray(String(missing))))
	if err != nil {
		t.Fatalf("exec start failure escaped as fatal error: %v", err)
	}
	if _, ok := ioHandleValue(value); !ok {
		t.Fatalf("exec start failure returned %s, want inert I/O handle", value.Describe())
	}
	problem, err := runtime.checkError(context.Background(), Invocation{Runtime: runtime, Script: script.ID(), Name: "checkError"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(problem.String(), "start command") || !strings.Contains(problem.String(), filepath.Base(missing)) {
		t.Fatalf("checkError = %q, want launch detail", problem.String())
	}
	if second, err := runtime.checkError(context.Background(), Invocation{Runtime: runtime, Script: script.ID(), Name: "checkError"}); err != nil || !second.IsNull() {
		t.Fatalf("second checkError = %s, %v; want $null", second.Describe(), err)
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", value)
}

func TestExecEmptyCommandIsSoftButMalformedEnvironmentIsAnArgumentError(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("process-errors.sl", "return 1;\n")
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	defer script.Unload(context.Background())
	functions := runtime.ioFunctions()

	handle, err := callIOBuiltinForScript(context.Background(), runtime, functions, script.ID(), "exec", ArrayValue(NewArray()))
	if err != nil {
		t.Fatalf("empty command escaped as fatal error: %v", err)
	}
	if _, ok := ioHandleValue(handle); !ok {
		t.Fatalf("empty command returned %s, want inert I/O handle", handle.Describe())
	}
	problem, err := runtime.checkError(context.Background(), Invocation{Runtime: runtime, Script: script.ID(), Name: "checkError"})
	if err != nil || !strings.Contains(problem.String(), "command is empty") {
		t.Fatalf("empty command checkError = %q, %v", problem.String(), err)
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)

	if _, err := callIOBuiltinForScript(context.Background(), runtime, functions, script.ID(), "exec",
		processHelperCommand("duplex"), String("not-a-hash")); err == nil || !strings.Contains(err.Error(), "environment hash") {
		t.Fatalf("malformed environment error = %v", err)
	}
}

func TestClosefDestroysProcessAndWaitStillCompletes(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	functions := runtime.ioFunctions()
	execContext, cancelExec := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelExec()
	handle, err := callIOBuiltin(execContext, runtime, functions, "exec", processHelperCommand("block"))
	if err != nil {
		t.Fatal(err)
	}
	if ready := mustCallIOBuiltin(t, runtime, functions, "readln", handle).String(); ready != "ready" {
		t.Fatalf("readln = %q, want ready", ready)
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	waitContext, cancelWait := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelWait()
	if _, err := callIOBuiltin(waitContext, runtime, functions, "wait", handle); err != nil {
		t.Fatalf("wait after closef: %v", err)
	}
}

func TestExecContextCancellationDestroysChild(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	functions := runtime.ioFunctions()
	execContext, cancelExec := context.WithCancel(context.Background())
	handle, err := callIOBuiltin(execContext, runtime, functions, "exec", processHelperCommand("block"))
	if err != nil {
		t.Fatal(err)
	}
	if ready := mustCallIOBuiltin(t, runtime, functions, "readln", handle).String(); ready != "ready" {
		t.Fatalf("readln = %q, want ready", ready)
	}
	cancelExec()
	waitContext, cancelWait := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelWait()
	if _, err := callIOBuiltin(waitContext, runtime, functions, "wait", handle); err != nil {
		t.Fatalf("wait after cancellation: %v", err)
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
}

func TestScriptUnloadDestroysAndJoinsOwnedProcesses(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("process-owner.sl", "return 1;\n")
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	functions := runtime.ioFunctions()
	execContext, cancelExec := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelExec()
	handleValue, err := callIOBuiltinForScript(execContext, runtime, functions, script.ID(), "exec", processHelperCommand("block"))
	if err != nil {
		t.Fatal(err)
	}
	handle, ok := ioHandleValue(handleValue)
	if !ok || handle.getProcess() == nil {
		t.Fatalf("exec returned %s, want process handle", handleValue.Describe())
	}
	process := handle.getProcess()
	if ready := mustCallIOBuiltin(t, runtime, functions, "readln", handleValue).String(); ready != "ready" {
		t.Fatalf("readln = %q, want ready", ready)
	}

	unloadContext, cancelUnload := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelUnload()
	if err := script.Unload(unloadContext); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	select {
	case <-process.done:
	default:
		t.Fatal("Unload returned before the child process was reaped")
	}
	if script.Active() {
		t.Fatal("script remains active after unload")
	}
}

func TestExecReturnedFromScriptCallSurvivesEntryUntilUnload(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("returned-process.cna", `sub launch { return exec($1); }`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	execContext, cancelExec := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelExec()
	handleValue, err := script.Call(execContext, "launch", processHelperCommand("block"))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	handle, ok := ioHandleValue(handleValue)
	if !ok || handle.getProcess() == nil {
		t.Fatalf("launch returned %s, want process handle", handleValue.Describe())
	}
	process := handle.getProcess()
	if ready := mustCallIOBuiltin(t, runtimeInstance, runtimeInstance.ioFunctions(), "readln", handleValue).String(); ready != "ready" {
		t.Fatalf("readln = %q, want ready", ready)
	}
	select {
	case <-process.done:
		t.Fatal("process ended when the Script.Call execution lease released")
	default:
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	select {
	case <-process.done:
	default:
		t.Fatal("Unload returned before the returned process was reaped")
	}
}

func TestRuntimeCloseDestroysAndJoinsDirectInvokeProcess(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	execContext, cancelExec := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelExec()
	handleValue, err := runtimeInstance.Invoke(execContext, "exec", processHelperCommand("block"))
	if err != nil {
		t.Fatalf("Runtime.Invoke exec: %v", err)
	}
	handle, ok := ioHandleValue(handleValue)
	if !ok || handle.getProcess() == nil {
		t.Fatalf("exec returned %s, want process handle", handleValue.Describe())
	}
	process := handle.getProcess()
	ready, err := runtimeInstance.Invoke(context.Background(), "readln", handleValue)
	if err != nil || ready.String() != "ready" {
		t.Fatalf("readln = (%q, %v), want ready", ready.String(), err)
	}
	select {
	case <-process.done:
		t.Fatal("direct-invoke process ended when Runtime.Invoke released")
	default:
	}

	closeContext, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelClose()
	if err := runtimeInstance.Close(closeContext); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-process.done:
	default:
		t.Fatal("Close returned before the direct-invoke process was reaped")
	}
}

func TestScriptUnloadUnblocksAFullProcessInputPipe(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("blocked-process-write.cna", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	functions := runtimeInstance.ioFunctions()
	handleValue, err := callIOBuiltinForScript(context.Background(), runtimeInstance, functions, script.ID(), "exec", processHelperCommand("block"))
	if err != nil {
		t.Fatal(err)
	}
	handle, ok := ioHandleValue(handleValue)
	if !ok || handle.getProcess() == nil {
		t.Fatalf("exec returned %s, want process handle", handleValue.Describe())
	}
	if ready := mustCallIOBuiltin(t, runtimeInstance, functions, "readln", handleValue).String(); ready != "ready" {
		t.Fatalf("readln = %q, want ready", ready)
	}

	writeResult := make(chan error, 1)
	go func() {
		_, writeErr := callIOBuiltinForScript(context.Background(), runtimeInstance, functions, script.ID(), "writeb",
			handleValue, String(strings.Repeat("x", 8<<20)))
		writeResult <- writeErr
	}()
	deadline := time.After(quiescenceTestTimeout)
	for {
		if !handle.writeMu.TryLock() {
			break
		}
		handle.writeMu.Unlock()
		runtime.Gosched()
		select {
		case err := <-writeResult:
			t.Fatalf("large process write completed before filling the pipe: %v", err)
		case <-deadline:
			t.Fatal("large process write did not enter the handle write section")
		default:
		}
	}

	unloadResult := make(chan error, 1)
	go func() { unloadResult <- script.Unload(context.Background()) }()
	if err := awaitQuiescenceError(t, unloadResult, "Unload with blocked process write"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if err := awaitQuiescenceError(t, writeResult, "blocked process write"); err == nil {
		t.Fatal("blocked process write unexpectedly succeeded")
	}
	process := handle.getProcess()
	runtimeInstance.mu.RLock()
	_, retained := runtimeInstance.processes[process]
	runtimeInstance.mu.RUnlock()
	script.mu.RLock()
	_, retainedByScript := script.processes[process]
	script.mu.RUnlock()
	if retained || retainedByScript {
		t.Fatalf("closed process retained after terminal join: runtime %v script %v", retained, retainedByScript)
	}
}

func TestExecImporterOverrideRetainsFirstRefusal(t *testing.T) {
	called := 0
	runtime, err := New(WithFunction("exec", func(_ context.Context, invocation Invocation) (Value, error) {
		called++
		if got, want := invocation.Arg(0).String(), "must-not-run"; got != want {
			t.Fatalf("command = %q, want %q", got, want)
		}
		return String("overridden"), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Eval(context.Background(), "exec-override.sl", `return exec("must-not-run");`)
	if err != nil {
		t.Fatal(err)
	}
	if result.String() != "overridden" || called != 1 {
		t.Fatalf("result = %s, calls = %d", result.Describe(), called)
	}
}

func TestProcessObjectHelper(t *testing.T) {
	marker := -1
	for index, argument := range os.Args {
		if argument == processObjectHelperMarker {
			marker = index
			break
		}
	}
	if marker == -1 {
		return
	}
	if marker+1 >= len(os.Args) {
		os.Exit(91)
	}
	switch os.Args[marker+1] {
	case "duplex":
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(92)
		}
		workingDirectory, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(93)
		}
		fmt.Fprintln(os.Stderr, "hidden diagnostic")
		fmt.Fprintf(os.Stdout, "stdin:%s", line)
		fmt.Fprintln(os.Stdout, "cwd:"+workingDirectory)
		fmt.Fprintln(os.Stdout, "env:"+os.Getenv("OPFOR_PROCESS_VALUE"))
		os.Exit(13)
	case "block":
		fmt.Fprintln(os.Stdout, "ready")
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "delay":
		time.Sleep(100 * time.Millisecond)
		os.Exit(4)
	case "quota-stdout":
		fmt.Fprint(os.Stdout, "0123456789")
		os.Exit(0)
	case "quota-stderr":
		fmt.Fprint(os.Stderr, "0123456789")
		os.Exit(0)
	default:
		os.Exit(94)
	}
}

func processHelperCommand(mode string) Value {
	return ArrayValue(NewArray(
		String(os.Args[0]),
		String("-test.run=^TestProcessObjectHelper$"),
		String("--"),
		String(processObjectHelperMarker),
		String(mode),
	))
}

func invocationWithValues(name string, values ...Value) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{Name: name, Arguments: arguments}
}

func callIOBuiltinForScript(ctx context.Context, runtime *Runtime, functions map[string]NativeFunc, script ScriptID, name string, values ...Value) (Value, error) {
	invocation := invocationWithValues(name, values...)
	invocation.Runtime = runtime
	invocation.Script = script
	return functions[name](ctx, invocation)
}

func TestExecCanceledBeforeStartReturnsContextError(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	functions := runtime.ioFunctions()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = callIOBuiltin(ctx, runtime, functions, "exec", processHelperCommand("block"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("exec cancellation error = %v, want context.Canceled", err)
	}
}
