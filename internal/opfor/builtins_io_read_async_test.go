package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSleepReadReturnsImmediatelyAndPreservesCallbackABI(t *testing.T) {
	const source = `
$gate = semaphore(0);
$handle = allocate();
writeb($handle, "line\n");
closef($handle);
$message = "pending";
read($handle, {
    acquire($gate);
    $message = $0 . "|" . ($1 is $handle) . "|" . $2 . "|" . size(@_);
});
$returned = "returned";
release($gate);
return @($returned, $handle);
`
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeInstance.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := runtimeInstance.Eval(ctx, "read-async-abi.sl", source)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if len(values) != 2 || values[0].String() != "returned" {
		t.Fatalf("read launch result = %s", result.Describe())
	}
	script := runtimeInstance.evalScript
	if script == nil {
		t.Fatal("Eval did not retain its script")
	}
	if _, err := runtimeInstance.wait(ctx, Invocation{
		Runtime: runtimeInstance, Script: script.ID(), Name: "wait",
		Arguments: []Argument{{Value: values[1]}},
	}); err != nil {
		t.Fatalf("wait outside Sleep variable lock: %v", err)
	}
	message, err := script.GetContext(ctx, "$message")
	if err != nil {
		t.Fatalf("get callback message: %v", err)
	}
	if got := message.String(); got != "&read|1|line|2" {
		t.Fatalf("callback ABI = %q, want &read|1|line|2", got)
	}
}

func TestSleepReadWaitOrderingAndPartialBinaryEOF(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeInstance.Close(context.Background())
	program, err := CompileString("read-wait-owner.sl", `return 1;`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	defer script.Unload(context.Background())
	functions := runtimeInstance.ioFunctions()

	line := mustCallIOBuiltin(t, runtimeInstance, functions, "allocate")
	mustCallIOBuiltin(t, runtimeInstance, functions, "writeb", line, String("blocked\n"))
	mustCallIOBuiltin(t, runtimeInstance, functions, "closef", line)
	entered := make(chan struct{})
	release := make(chan struct{})
	callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
		close(entered)
		<-release
		return Null(), nil
	}))
	if _, err := callIOBuiltinForScript(context.Background(), runtimeInstance, functions, script.ID(), "read", line, callback); err != nil {
		t.Fatalf("read line: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("line callback did not start")
	}
	waitInvocation := Invocation{
		Runtime: runtimeInstance, Script: script.ID(), Name: "wait",
		Arguments: []Argument{{Value: line}, {Value: Int(1)}},
	}
	if value, err := runtimeInstance.wait(context.Background(), waitInvocation); err != nil || !value.IsNull() {
		t.Fatalf("timed wait = (%s, %v), want null/nil", value.Describe(), err)
	}
	problem, err := runtimeInstance.checkError(context.Background(), Invocation{Runtime: runtimeInstance, Script: script.ID(), Name: "checkError"})
	if err != nil || !strings.Contains(problem.String(), "java.io.IOException: wait on object timed out") {
		t.Fatalf("timeout problem = (%s, %v)", problem.Describe(), err)
	}
	close(release)
	waitInvocation.Arguments = waitInvocation.Arguments[:1]
	if _, err := runtimeInstance.wait(context.Background(), waitInvocation); err != nil {
		t.Fatalf("completed wait: %v", err)
	}
	waitInvocation.Arguments = append(waitInvocation.Arguments, Argument{Value: Int(-1)})
	if value, err := runtimeInstance.wait(context.Background(), waitInvocation); err != nil || !value.IsNull() {
		t.Fatalf("completed wait(-1) = (%s, %v), want null/nil", value.Describe(), err)
	}
	if problem, err := runtimeInstance.checkError(context.Background(), Invocation{Runtime: runtimeInstance, Script: script.ID(), Name: "checkError"}); err != nil || !problem.IsNull() {
		t.Fatalf("completed negative wait problem = (%s, %v), want null/nil", problem.Describe(), err)
	}

	binary := mustCallIOBuiltin(t, runtimeInstance, functions, "allocate")
	mustCallIOBuiltin(t, runtimeInstance, functions, "writeb", binary, String("abc"))
	mustCallIOBuiltin(t, runtimeInstance, functions, "closef", binary)
	var chunks, names []string
	var callbackMu sync.Mutex
	binaryCallback := FunctionValue(namedIOTestCallable{
		invoke: func(_ context.Context, name string, values ...Value) (Value, error) {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			names = append(names, name)
			if len(values) != 2 || !values[0].IdentityEqual(binary) {
				t.Errorf("binary callback arguments = %#v", values)
				return Null(), nil
			}
			chunks = append(chunks, values[1].String())
			return Null(), nil
		},
	})
	if _, err := callIOBuiltinForScript(context.Background(), runtimeInstance, functions, script.ID(), "read", binary, binaryCallback, Int(2)); err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if _, err := runtimeInstance.wait(context.Background(), Invocation{
		Runtime: runtimeInstance, Script: script.ID(), Name: "wait", Arguments: []Argument{{Value: binary}},
	}); err != nil {
		t.Fatalf("wait binary: %v", err)
	}
	callbackMu.Lock()
	gotChunks := strings.Join(chunks, ",")
	gotNames := strings.Join(names, ",")
	callbackMu.Unlock()
	if gotChunks != "ab,c" || gotNames != "&read,&read" {
		t.Fatalf("binary callbacks = chunks %q, names %q", gotChunks, gotNames)
	}
	problem, err = runtimeInstance.checkError(context.Background(), Invocation{Runtime: runtimeInstance, Script: script.ID(), Name: "checkError"})
	if err != nil || problem.String() != "java.io.EOFException" {
		t.Fatalf("binary EOF problem = (%s, %v), want java.io.EOFException/nil", problem.Describe(), err)
	}
}

type namedIOTestCallable struct {
	invoke func(context.Context, string, ...Value) (Value, error)
}

func (callable namedIOTestCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return callable.invoke(ctx, "", values...)
}

func (callable namedIOTestCallable) invokeNamed(ctx context.Context, name string, values ...Value) (Value, error) {
	return callable.invoke(ctx, name, values...)
}

func (namedIOTestCallable) isSleepSequenceClosure() {}

const sleepReadNativeCallbackProbeName = "read-native-callback.sl"

const sleepReadNativeCallbackProbe = `
sub rejected_read {
    $handle = allocate();
    writeb($handle, "line\n");
    closef($handle);
    println("before");
    read($handle, function("&println"));
    println("unreachable");
}
rejected_read();
println("caller-resumed");
`

func TestSleepReadRejectsNativeFunctionCallbackAndStopsOnlyActiveBlock(t *testing.T) {
	got := runSleepReadNativeCallbackProbe(t)
	if !strings.Contains(got, "Warning: expected &closure--received: &println at read-native-callback.sl:") {
		t.Fatalf("callback warning = %q", got)
	}
	if strings.Contains(got, "unreachable") || !strings.Contains(got, "before\n") || !strings.HasSuffix(got, "caller-resumed\n") {
		t.Fatalf("callback rejection did not stop only the active block: %q", got)
	}
}

func TestSleepReadNativeFunctionCallbackOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepReadNativeCallbackProbeName)
	if err := os.WriteFile(path, []byte(sleepReadNativeCallbackProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep read callback probe: %v\n%s", err, want)
	}
	got := []byte(runSleepReadNativeCallbackProbe(t))
	if normalizedGot, normalizedWant := normalizeSleepReadNativeCallbackProbe(got), normalizeSleepReadNativeCallbackProbe(want); !bytes.Equal(normalizedGot, normalizedWant) {
		t.Fatalf("official Sleep read callback output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepReadNativeCallbackProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepReadNativeCallbackProbeName, sleepReadNativeCallbackProbe); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	return output.String()
}

func normalizeSleepReadNativeCallbackProbe(output []byte) []byte {
	lines := strings.SplitAfter(string(output), "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, "Warning: expected &closure--received:") {
			lines[index] = "Warning: expected &closure--received: <native function> at <source>:<line>\n"
		}
	}
	return []byte(strings.Join(lines, ""))
}

func TestSleepReadWaitLatchesAuthoritativeFailures(t *testing.T) {
	t.Run("line input quota", func(t *testing.T) {
		runtimeInstance, err := New(WithLimits(Limits{MaxInputBytesPerRuntime: 1}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		handle := newIOHandle("async-line-limit", bytes.NewReader([]byte("ab\n")), nil, true, false, false).
			withRuntimeOutputAccount(runtimeInstance.resources)
		var calls atomic.Int32
		callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
			calls.Add(1)
			return Null(), nil
		}))
		if _, err := runtimeInstance.Invoke(context.Background(), "read", ObjectValue(handle), callback); err != nil {
			t.Fatalf("read: %v", err)
		}
		invocation := invocationWithValues("wait", ObjectValue(handle))
		for attempt := 0; attempt < 2; attempt++ {
			_, waitErr := runtimeInstance.wait(context.Background(), invocation)
			assertInputLimitError(t, waitErr, 1)
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("callbacks after line input limit = %d, want 0", got)
		}
	})

	t.Run("binary importer failure is not retried", func(t *testing.T) {
		runtimeInstance, err := New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		handle := newIOHandle("async-binary-fatal", bytes.NewReader([]byte("ab")), nil, true, false, false).
			withRuntimeOutputAccount(runtimeInstance.resources)
		fatalErr := errors.New("binary callback failed")
		var calls atomic.Int32
		callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
			calls.Add(1)
			return Null(), fatalErr
		}))
		if _, err := runtimeInstance.Invoke(context.Background(), "read", ObjectValue(handle), callback, Int(2)); err != nil {
			t.Fatalf("read: %v", err)
		}
		invocation := invocationWithValues("wait", ObjectValue(handle))
		for attempt := 0; attempt < 2; attempt++ {
			if _, waitErr := runtimeInstance.wait(context.Background(), invocation); !errors.Is(waitErr, fatalErr) {
				t.Fatalf("wait %d error = %v, want %v", attempt, waitErr, fatalErr)
			}
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("fatal binary callback calls = %d, want 1", got)
		}
	})

	t.Run("script callback native failure is not retried", func(t *testing.T) {
		fatalErr := errors.New("script callback native failure")
		var calls atomic.Int32
		runtimeInstance, err := New(WithFunction("fail_read_callback", func(context.Context, Invocation) (Value, error) {
			calls.Add(1)
			return Null(), fatalErr
		}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		handleValue, err := runtimeInstance.Eval(context.Background(), "read-native-failure.cna", `
$handle = allocate();
writeb($handle, "x");
closef($handle);
read($handle, { fail_read_callback(); }, 1);
return $handle;
`)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		owner := runtimeInstance.evalScript
		if owner == nil {
			t.Fatal("Eval did not retain its script")
		}
		invocation := Invocation{
			Runtime: runtimeInstance,
			Script:  owner.ID(),
			Name:    "wait",
			Arguments: []Argument{
				{Value: handleValue},
			},
		}
		for attempt := 0; attempt < 2; attempt++ {
			if _, waitErr := runtimeInstance.wait(context.Background(), invocation); !errors.Is(waitErr, fatalErr) {
				t.Fatalf("wait %d error = %v, want %v", attempt, waitErr, fatalErr)
			}
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("fatal script callback calls = %d, want 1", got)
		}
	})

	t.Run("binary instruction limit is not retried", func(t *testing.T) {
		const limit = uint64(1)
		runtimeInstance, err := New(WithInstructionLimit(limit))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		handle := newIOHandle("async-binary-instruction-limit", bytes.NewReader([]byte("x")), nil, true, false, false).
			withRuntimeOutputAccount(runtimeInstance.resources)
		var calls atomic.Int32
		callback := FunctionValue(ioTestCallable(func(ctx context.Context, _ ...Value) (Value, error) {
			calls.Add(1)
			if err := consumeInstruction(ctx); err != nil {
				return Null(), err
			}
			return Null(), consumeInstruction(ctx)
		}))
		if _, err := runtimeInstance.Invoke(context.Background(), "read", ObjectValue(handle), callback, Int(1)); err != nil {
			t.Fatalf("read: %v", err)
		}
		invocation := invocationWithValues("wait", ObjectValue(handle))
		for attempt := 0; attempt < 2; attempt++ {
			_, waitErr := runtimeInstance.wait(context.Background(), invocation)
			if !errors.Is(waitErr, ErrInstructionLimit) {
				t.Fatalf("wait %d error = %v, want ErrInstructionLimit", attempt, waitErr)
			}
			var limitErr *LimitError
			if !errors.As(waitErr, &limitErr) || limitErr.Resource != resourceInstruction || limitErr.Limit != limit {
				t.Fatalf("wait %d LimitError = %#v, want instruction/%d", attempt, limitErr, limit)
			}
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("instruction-limited callback calls = %d, want 1", got)
		}
	})
}

func TestSleepReadCallbackExceptionsRemainWorkerLocal(t *testing.T) {
	tests := []struct {
		name      string
		chunkSize int32
		err       error
		wantCalls int32
	}{
		{name: "line throw", err: &scriptThrow{value: String("line exception")}, wantCalls: 1},
		{name: "binary warning retry", chunkSize: 1, err: &uncaughtScriptWarning{err: errors.New("binary exception")}, wantCalls: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, err := New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			input := []byte("line\n")
			if test.chunkSize > 0 {
				input = []byte("x")
			}
			handle := newIOHandle(test.name, bytes.NewReader(input), nil, true, false, false).
				withRuntimeOutputAccount(runtimeInstance.resources)
			var calls atomic.Int32
			callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
				calls.Add(1)
				return Null(), test.err
			}))
			arguments := []Value{ObjectValue(handle), callback}
			if test.chunkSize > 0 {
				arguments = append(arguments, Int(test.chunkSize))
			}
			if _, err := runtimeInstance.Invoke(context.Background(), "read", arguments...); err != nil {
				t.Fatalf("read: %v", err)
			}
			if _, waitErr := runtimeInstance.wait(context.Background(), invocationWithValues("wait", ObjectValue(handle))); waitErr != nil {
				t.Fatalf("wait surfaced Sleep callback exception: %v", waitErr)
			}
			if got := calls.Load(); got != test.wantCalls {
				t.Fatalf("callback calls = %d, want %d", got, test.wantCalls)
			}
		})
	}
}

type readCloseFailureWriter struct{ err error }

func (*readCloseFailureWriter) Write(data []byte) (int, error) { return len(data), nil }
func (writer *readCloseFailureWriter) Close() error            { return writer.err }

func TestSleepReadDispatchesFinalLineBeforeSuppressingDuplexCloseFailure(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	closeErr := errors.New("duplex writer close failed")
	handle := newIOHandle("duplex-final-line", strings.NewReader("unterminated"), &readCloseFailureWriter{err: closeErr}, false, true, false).
		withRuntimeOutputAccount(runtimeInstance.resources)
	var calls atomic.Int32
	var got string
	callback := FunctionValue(ioTestCallable(func(_ context.Context, values ...Value) (Value, error) {
		calls.Add(1)
		got = values[1].String()
		return Null(), nil
	}))
	if _, err := runtimeInstance.Invoke(context.Background(), "read", ObjectValue(handle), callback); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, waitErr := runtimeInstance.wait(context.Background(), invocationWithValues("wait", ObjectValue(handle))); waitErr != nil {
		t.Fatalf("wait surfaced ordinary close error %v: %v", closeErr, waitErr)
	}
	if count := calls.Load(); count != 1 || got != "unterminated" {
		t.Fatalf("final-line callback = calls %d, data %q", count, got)
	}
}

func TestSleepReadReplacesCurrentWorkerButRetainsForkToken(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	handle := newIOHandle("worker-replacement", nil, nil, false, false, false)
	fork := &forkTask{done: make(chan struct{}), result: String("fork-token")}
	handle.setTask(fork)
	readDone := make(chan struct{})
	close(readDone)
	handle.setWorker(&sleepReadTask{done: readDone})

	value, err := runtimeInstance.wait(context.Background(), invocationWithValues("wait", ObjectValue(handle), Int(-1)))
	if err != nil || !value.IsNull() {
		t.Fatalf("completed replacement wait(-1) = (%s, %v), want null/nil", value.Describe(), err)
	}
	fork.result = String("fork-token")
	close(fork.done)
	value, err = runtimeInstance.wait(context.Background(), invocationWithValues("wait", ObjectValue(handle)))
	if err != nil || value.String() != "fork-token" {
		t.Fatalf("retained fork token = (%s, %v), want fork-token/nil", value.Describe(), err)
	}
}

func TestSleepReadLifecycleCancelsOwnedAndBorrowedInputs(t *testing.T) {
	t.Run("script-owned", func(t *testing.T) {
		runtimeInstance, err := New()
		if err != nil {
			t.Fatal(err)
		}
		program, err := CompileString("read-owner.sl", `return 1;`)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		handle := newIOHandle("owned-read-pipe", reader, nil, true, false, false)
		var calls atomic.Int32
		callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
			calls.Add(1)
			return Null(), nil
		}))
		if _, err := callIOBuiltinForScript(context.Background(), runtimeInstance, runtimeInstance.ioFunctions(), script.ID(), "read", ObjectValue(handle), callback); err != nil {
			t.Fatalf("read: %v", err)
		}
		worker := handle.getWorker().(*sleepReadTask)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := script.Unload(ctx); err != nil {
			t.Fatalf("Unload: %v", err)
		}
		select {
		case <-worker.done:
		default:
			t.Fatal("Unload returned before owned read worker completed")
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("callbacks after cancellation = %d", got)
		}
	})

	t.Run("runtime-owned", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		runtimeInstance, err := New()
		if err != nil {
			t.Fatal(err)
		}
		handle := newIOHandle("runtime-read-pipe", reader, nil, true, false, false)
		callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) { return Null(), nil }))
		if _, err := callIOBuiltin(context.Background(), runtimeInstance, runtimeInstance.ioFunctions(), "read", ObjectValue(handle), callback); err != nil {
			t.Fatalf("read: %v", err)
		}
		worker := handle.getWorker().(*sleepReadTask)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := runtimeInstance.Close(ctx); err != nil {
			t.Fatalf("Close: %v", err)
		}
		select {
		case <-worker.done:
		default:
			t.Fatal("Close returned before owned read worker completed")
		}
	})

	t.Run("borrowed-console", func(t *testing.T) {
		reader := newBorrowedBlockingReadCloser()
		defer reader.unblock()
		var calls atomic.Int32
		runtimeInstance, err := New(WithStdin(reader))
		if err != nil {
			t.Fatal(err)
		}
		functions := runtimeInstance.ioFunctions()
		console := mustCallIOBuiltin(t, runtimeInstance, functions, "getConsole")
		callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
			calls.Add(1)
			return Null(), nil
		}))
		if _, err := callIOBuiltin(context.Background(), runtimeInstance, functions, "read", console, callback); err != nil {
			t.Fatalf("read: %v", err)
		}
		select {
		case <-reader.started:
		case <-time.After(time.Second):
			t.Fatal("borrowed read did not block")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := runtimeInstance.Close(ctx); !errors.Is(err, ErrReadCancellationUnsupported) {
			t.Fatalf("Close with borrowed input error = %v, want ErrReadCancellationUnsupported", err)
		}
		if reader.closed.Load() {
			t.Fatal("Close closed the host-owned input")
		}
		handle, _ := ioHandleValue(console)
		worker := handle.getWorker().(*sleepReadTask)
		if !worker.revoked.Load() {
			t.Fatal("Close returned without logically revoking the read callback")
		}
		select {
		case <-worker.done:
			t.Fatal("Close falsely reported the blocked borrowed read worker done")
		default:
		}
		reader.unblock()
		select {
		case <-reader.returned:
		case <-time.After(time.Second):
			t.Fatal("borrowed Read did not return after the host unblocked it")
		}
		select {
		case <-worker.done:
		case <-time.After(time.Second):
			t.Fatal("borrowed read worker did not complete after Read returned")
		}
		if !reader.consumed.Load() {
			t.Fatal("late borrowed byte was not consumed; test did not exercise the unavoidable discard boundary")
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("borrowed callback after Close = %d", got)
		}
	})

	t.Run("borrowed-context-reader", func(t *testing.T) {
		reader := newBorrowedContextReader()
		var calls atomic.Int32
		runtimeInstance, err := New(WithStdin(reader))
		if err != nil {
			t.Fatal(err)
		}
		functions := runtimeInstance.ioFunctions()
		console := mustCallIOBuiltin(t, runtimeInstance, functions, "getConsole")
		callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
			calls.Add(1)
			return Null(), nil
		}))
		if _, err := callIOBuiltin(context.Background(), runtimeInstance, functions, "read", console, callback); err != nil {
			t.Fatalf("read: %v", err)
		}
		select {
		case <-reader.started:
		case <-time.After(time.Second):
			t.Fatal("context-aware borrowed read did not block")
		}
		handle, _ := ioHandleValue(console)
		worker := handle.getWorker().(*sleepReadTask)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := runtimeInstance.Close(ctx); err != nil {
			t.Fatalf("Close with context-aware borrowed input: %v", err)
		}
		select {
		case <-worker.done:
		default:
			t.Fatal("Close returned before the context-aware read worker completed")
		}
		if got := reader.rawCalls.Load(); got != 0 {
			t.Fatalf("ordinary Read calls = %d, want ReadContext only", got)
		}
		if reader.closed.Load() {
			t.Fatal("Close closed the context-aware host-owned input")
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("context-aware borrowed callback after Close = %d", got)
		}
	})

	t.Run("script-borrowed-console", func(t *testing.T) {
		reader := newBorrowedBlockingReadCloser()
		defer reader.unblock()
		runtimeInstance, err := New(WithStdin(reader))
		if err != nil {
			t.Fatal(err)
		}
		defer runtimeInstance.Close(context.Background())
		program, err := CompileString("borrowed-read-owner.sl", `return 1;`)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		functions := runtimeInstance.ioFunctions()
		console := mustCallIOBuiltin(t, runtimeInstance, functions, "getConsole")
		var calls atomic.Int32
		callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
			calls.Add(1)
			return Null(), nil
		}))
		if _, err := callIOBuiltinForScript(context.Background(), runtimeInstance, functions, script.ID(), "read", console, callback); err != nil {
			t.Fatalf("read: %v", err)
		}
		select {
		case <-reader.started:
		case <-time.After(time.Second):
			t.Fatal("script-owned borrowed read did not block")
		}
		handle, _ := ioHandleValue(console)
		worker := handle.getWorker().(*sleepReadTask)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := script.Unload(ctx); !errors.Is(err, ErrReadCancellationUnsupported) {
			t.Fatalf("Unload with borrowed input error = %v, want ErrReadCancellationUnsupported", err)
		}
		if reader.closed.Load() {
			t.Fatal("Unload closed the host-owned input")
		}
		select {
		case <-worker.done:
			t.Fatal("Unload falsely reported the blocked borrowed read worker done")
		default:
		}
		reader.unblock()
		select {
		case <-worker.done:
		case <-time.After(time.Second):
			t.Fatal("script-owned borrowed read did not finish after host release")
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("borrowed callback after Unload = %d", got)
		}
	})
}

type borrowedBlockingReadCloser struct {
	started  chan struct{}
	release  chan struct{}
	returned chan struct{}

	startOnce   sync.Once
	releaseOnce sync.Once
	returnOnce  sync.Once
	closed      atomic.Bool
	consumed    atomic.Bool
}

func newBorrowedBlockingReadCloser() *borrowedBlockingReadCloser {
	return &borrowedBlockingReadCloser{
		started: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{}),
	}
}

func (reader *borrowedBlockingReadCloser) Read(data []byte) (int, error) {
	reader.startOnce.Do(func() { close(reader.started) })
	<-reader.release
	reader.returnOnce.Do(func() { close(reader.returned) })
	if len(data) == 0 {
		return 0, nil
	}
	if !reader.consumed.CompareAndSwap(false, true) {
		return 0, io.EOF
	}
	data[0] = 'x'
	return 1, nil
}

func (reader *borrowedBlockingReadCloser) Close() error {
	reader.closed.Store(true)
	reader.unblock()
	return nil
}

func (reader *borrowedBlockingReadCloser) unblock() {
	reader.releaseOnce.Do(func() { close(reader.release) })
}

type borrowedContextReader struct {
	started    chan struct{}
	returnOnce sync.Once
	startOnce  sync.Once
	returned   chan struct{}
	rawCalls   atomic.Int32
	closed     atomic.Bool
}

func newBorrowedContextReader() *borrowedContextReader {
	return &borrowedContextReader{started: make(chan struct{}), returned: make(chan struct{})}
}

func (reader *borrowedContextReader) Read([]byte) (int, error) {
	reader.rawCalls.Add(1)
	return 0, errors.New("ordinary Read called instead of ReadContext")
}

func (reader *borrowedContextReader) ReadContext(ctx context.Context, _ []byte) (int, error) {
	reader.startOnce.Do(func() { close(reader.started) })
	<-ctx.Done()
	reader.returnOnce.Do(func() { close(reader.returned) })
	return 0, ctx.Err()
}

func (reader *borrowedContextReader) Close() error {
	reader.closed.Store(true)
	return nil
}

const sleepReadDifferentialProbe = `
global('$line $message $binary $chunks');
$gate = semaphore(0);
$line = allocate();
writeb($line, "line\n");
closef($line);
$message = "pending";
sub start_line {
    read($line, {
        acquire($gate);
        $message = $0 . "|" . $2 . "|" . size(@_);
    });
    return $line;
}
$binary = allocate();
writeb($binary, "abc");
closef($binary);
$chunks = "";
sub start_binary {
    read($binary, {
        if ($chunks ne "") { $chunks .= ","; }
        $chunks .= $2;
    }, 2);
    return $binary;
}
`

func TestSleepReadPinnedJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	javac := officialSleepJavaCompiler(t, java)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	absoluteJar, err := filepath.Abs(jar)
	if err != nil {
		t.Fatalf("Sleep JAR path: %v", err)
	}
	jar = absoluteJar
	harnessDirectory := t.TempDir()
	harnessPath := filepath.Join(harnessDirectory, "SleepReadHarness.java")
	harness := `
import java.util.Hashtable;
import java.util.Stack;
import sleep.bridges.Semaphore;
import sleep.bridges.io.IOObject;
import sleep.interfaces.Function;
import sleep.runtime.Scalar;
import sleep.runtime.ScriptInstance;
import sleep.runtime.ScriptLoader;
import sleep.runtime.SleepUtils;

public final class SleepReadHarness {
    private static Scalar call(ScriptInstance script, String name) {
        Function function = script.getScriptEnvironment().getFunction(name);
        return SleepUtils.runCode(function, name, script, new Stack());
    }

    public static void main(String[] arguments) throws Exception {
        ScriptLoader loader = new ScriptLoader();
        ScriptInstance script = loader.loadScript("read-harness.sl", ` + strconv.Quote(sleepReadDifferentialProbe) + `, new Hashtable());
        script.runScript();

        Scalar line = call(script, "&start_line");
        System.out.println("returned");
        ((Semaphore)script.getScriptVariables().getScalar("$gate").objectValue()).V();
        ((IOObject)line.objectValue()).wait(script.getScriptEnvironment(), 0L);
        System.out.println(script.getScriptVariables().getScalar("$message"));

        Scalar binary = call(script, "&start_binary");
        ((IOObject)binary.objectValue()).wait(script.getScriptEnvironment(), 0L);
        System.out.println(script.getScriptVariables().getScalar("$chunks"));
        System.out.println(script.getScriptEnvironment().checkError());
        loader.unloadScript(script);
    }
}
`
	if err := os.WriteFile(harnessPath, []byte(harness), 0o600); err != nil {
		t.Fatalf("write Java harness: %v", err)
	}
	compileOutput, err := osexec.CommandContext(ctx, javac, "-cp", jar, harnessPath).CombinedOutput()
	if err != nil {
		t.Fatalf("compile official Sleep harness: %v\n%s", err, compileOutput)
	}
	classPath := harnessDirectory + string(os.PathListSeparator) + jar
	reference, err := officialSleepJavaCommandContext(ctx, java, "-Dfile.encoding=UTF-8", "-cp", classPath, "SleepReadHarness").CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep probe: %v\n%s", err, reference)
	}

	var output bytes.Buffer
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(ctx, "read-differential.sl", sleepReadDifferentialProbe); err != nil {
		t.Fatalf("OPFOR setup: %v", err)
	}
	script := runtimeInstance.evalScript
	if script == nil {
		t.Fatal("OPFOR differential has no Eval script")
	}
	line, err := runtimeInstance.Eval(ctx, "read-line-start.sl", `return start_line();`)
	if err != nil {
		t.Fatalf("OPFOR line start: %v", err)
	}
	fmt.Fprintln(&output, "returned")
	gate, err := script.GetContext(ctx, "$gate")
	if err != nil {
		t.Fatalf("OPFOR gate: %v", err)
	}
	if _, err := runtimeInstance.Invoke(ctx, "release", gate); err != nil {
		t.Fatalf("OPFOR gate release: %v", err)
	}
	if _, err := runtimeInstance.wait(ctx, Invocation{
		Runtime: runtimeInstance, Script: script.ID(), Name: "wait", Arguments: []Argument{{Value: line}},
	}); err != nil {
		t.Fatalf("OPFOR line wait: %v", err)
	}
	message, err := script.GetContext(ctx, "$message")
	if err != nil {
		t.Fatalf("OPFOR message: %v", err)
	}
	fmt.Fprintln(&output, message.String())

	binary, err := runtimeInstance.Eval(ctx, "read-binary-start.sl", `return start_binary();`)
	if err != nil {
		t.Fatalf("OPFOR binary start: %v", err)
	}
	if _, err := runtimeInstance.wait(ctx, Invocation{
		Runtime: runtimeInstance, Script: script.ID(), Name: "wait", Arguments: []Argument{{Value: binary}},
	}); err != nil {
		t.Fatalf("OPFOR binary wait: %v", err)
	}
	chunks, err := script.GetContext(ctx, "$chunks")
	if err != nil {
		t.Fatalf("OPFOR chunks: %v", err)
	}
	fmt.Fprintln(&output, chunks.String())
	problem, err := runtimeInstance.checkError(ctx, Invocation{Runtime: runtimeInstance, Script: script.ID(), Name: "checkError"})
	if err != nil {
		t.Fatalf("OPFOR binary error: %v", err)
	}
	fmt.Fprintln(&output, problem.String())
	if err := runtimeInstance.Close(ctx); err != nil {
		t.Fatalf("OPFOR Close: %v", err)
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("read differential mismatch\nofficial:\n%s\nopfor:\n%s", reference, output.Bytes())
	}
}
