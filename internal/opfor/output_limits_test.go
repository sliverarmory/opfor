package opfor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func assertOutputLimitError(t *testing.T, err error, limit uint64) {
	t.Helper()
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("error = %v, want ErrResourceLimit", err)
	}
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || limitErr.Resource != resourceOutputBytes || limitErr.Limit != limit {
		t.Fatalf("LimitError = %+v, want %q/%d", limitErr, resourceOutputBytes, limit)
	}
}

func TestOutputLimitNoViolationDoesNotReturnTypedNil(t *testing.T) {
	runtimeInstance, err := New(WithLimits(Limits{MaxOutputBytesPerRuntime: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeInstance.outputLimitError(); err != nil {
		t.Fatalf("fresh output limit error = %#v, want nil", err)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "size", String("no output")); err != nil {
		t.Fatalf("no-output invocation = %#v, want nil", err)
	}
}

type blockingQuotaWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	buffer  bytes.Buffer
}

type countingOutputStringer struct {
	calls atomic.Int64
}

func (value *countingOutputStringer) String() string {
	value.calls.Add(1)
	return "value"
}

type fakeLimitErrorWriter struct {
	err error
}

type outputPoisonVariableProvider struct {
	container *outputPoisonVariableContainer
}

type outputPoisonBindingObserver struct {
	runtime func() *Runtime
	err     error
}

func (observer *outputPoisonBindingObserver) Registered(ctx context.Context, _ Binding) error {
	if observer != nil && observer.runtime != nil && observer.runtime() != nil {
		_, _ = observer.runtime().Invoke(ctx, "print", String("xx"))
	}
	return observer.err
}

func (*outputPoisonBindingObserver) Unregistered(context.Context, Binding) error {
	return nil
}

func (provider *outputPoisonVariableProvider) CreateGlobalVariableContainer(
	context.Context,
	VariableContainerRequest,
) (VariableContainer, error) {
	return provider.container, nil
}

type outputPoisonVariableContainer struct {
	mu        sync.Mutex
	cells     map[string]*Cell
	runtime   func() *Runtime
	operation VariableProviderOperation
}

func (container *outputPoisonVariableContainer) setPoison(operation VariableProviderOperation) {
	container.mu.Lock()
	container.operation = operation
	container.mu.Unlock()
}

func (container *outputPoisonVariableContainer) poison(ctx context.Context, operation VariableProviderOperation) {
	container.mu.Lock()
	shouldPoison := container.operation == operation
	runtime := container.runtime
	container.mu.Unlock()
	if shouldPoison && runtime != nil && runtime() != nil {
		_, _ = runtime().Invoke(ctx, "println", String("xx"))
	}
}

func (container *outputPoisonVariableContainer) ScalarExists(ctx context.Context, access VariableAccess) (bool, error) {
	container.poison(ctx, VariableProviderExists)
	container.mu.Lock()
	cell := container.cells[access.Name]
	container.mu.Unlock()
	return cell != nil, nil
}

func (container *outputPoisonVariableContainer) GetScalar(_ context.Context, access VariableAccess) (*Cell, error) {
	container.mu.Lock()
	cell := container.cells[access.Name]
	container.mu.Unlock()
	return cell, nil
}

func (container *outputPoisonVariableContainer) PutScalar(ctx context.Context, access VariableAccess, cell *Cell) (*Cell, error) {
	container.mu.Lock()
	previous := container.cells[access.Name]
	container.cells[access.Name] = cell
	container.mu.Unlock()
	container.poison(ctx, VariableProviderPut)
	return previous, nil
}

func (container *outputPoisonVariableContainer) RemoveScalar(ctx context.Context, access VariableAccess) error {
	container.mu.Lock()
	delete(container.cells, access.Name)
	container.mu.Unlock()
	container.poison(ctx, VariableProviderRemove)
	return nil
}

func (container *outputPoisonVariableContainer) CreateLocalVariableContainer(
	context.Context,
	VariableContainerRequest,
) (VariableContainer, error) {
	return &outputPoisonVariableContainer{cells: make(map[string]*Cell), runtime: container.runtime}, nil
}

func (container *outputPoisonVariableContainer) CreateInternalVariableContainer(
	context.Context,
	VariableContainerRequest,
) (VariableContainer, error) {
	return &outputPoisonVariableContainer{cells: make(map[string]*Cell), runtime: container.runtime}, nil
}

func (writer fakeLimitErrorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

func (writer *blockingQuotaWriter) Write(data []byte) (int, error) {
	writer.once.Do(func() { close(writer.entered) })
	<-writer.release
	return writer.buffer.Write(data)
}

func TestOutputLimitConsoleIsSingleCountedAndSharedWithStderr(t *testing.T) {
	t.Run("exact limit is not failure", func(t *testing.T) {
		var output bytes.Buffer
		runtimeInstance, err := New(
			WithLimits(Limits{MaxOutputBytesPerRuntime: 5}),
			WithStdout(&output),
			WithStderr(io.Discard),
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeInstance.Invoke(context.Background(), "print", String("abcde")); err != nil {
			t.Fatalf("exact-limit console write: %v", err)
		}
		if _, err := runtimeInstance.Invoke(context.Background(), "size", String("no output")); err != nil {
			t.Fatalf("no-output call after exact limit: %v", err)
		}
		if got := output.String(); got != "abcde" {
			t.Fatalf("stdout = %q, want abcde", got)
		}
	})

	var stdout, stderr bytes.Buffer
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: 7}),
		WithStdout(&stdout),
		WithStderr(&stderr),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runtimeInstance.Invoke(context.Background(), "print", String("abc")); err != nil {
		t.Fatalf("exact console prefix was double-counted: %v", err)
	}
	_, err = runtimeInstance.Invoke(context.Background(), "warn", String("ignored"))
	assertOutputLimitError(t, err, 7)
	if got := stdout.String(); got != "abc" {
		t.Fatalf("stdout = %q, want abc", got)
	}
	if got := stderr.String(); got != "Warn" {
		t.Fatalf("stderr partial prefix = %q, want Warn", got)
	}
	if got := runtimeInstance.resources.used(resourceOutputBytes); got != 7 {
		t.Fatalf("accounted output = %d, want 7", got)
	}
}

func TestOutputLimitStopsPortableFixtureTraversal(t *testing.T) {
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: 1}),
		WithStdout(io.Discard),
		WithStderr(io.Discard),
	)
	if err != nil {
		t.Fatal(err)
	}

	counted := &countingOutputStringer{}
	array := newPortableJavaArray(
		portableArrayType("java.lang.Object"),
		[]int{2},
		[]Value{ObjectValue(counted), ObjectValue(counted)},
	)
	value, handled, invokeErr := (&portableArrayTest1{}).invoke(ObjectInvocation{
		Runtime: runtimeInstance,
		Op:      ObjectInvoke,
		Message: "bar",
		Arguments: []Argument{
			{Value: ObjectValue(array)},
		},
	})
	if !handled || !value.IsNull() {
		t.Fatalf("ArrayTest1.bar = (%s, handled %t), want null/true", value.Describe(), handled)
	}
	assertOutputLimitError(t, invokeErr, 1)
	if got := counted.calls.Load(); got != 0 {
		t.Fatalf("fixture formatted %d array values after header quota failure, want 0", got)
	}
}

func TestOutputLimitExplicitMemoryWritersPreserveAllowedPrefix(t *testing.T) {
	tests := []struct {
		name  string
		write func(context.Context, *Runtime, Value) error
		want  string
	}{
		{
			name: "writeb",
			write: func(ctx context.Context, runtimeInstance *Runtime, handle Value) error {
				_, err := runtimeInstance.Invoke(ctx, "writeb", handle, String("abcde"))
				return err
			},
			want: "abc",
		},
		{
			name: "text print",
			write: func(ctx context.Context, runtimeInstance *Runtime, handle Value) error {
				_, err := runtimeInstance.Invoke(ctx, "print", handle, String("abcde"))
				return err
			},
			want: "abc",
		},
		{
			name: "bwrite",
			write: func(ctx context.Context, runtimeInstance *Runtime, handle Value) error {
				_, err := runtimeInstance.Invoke(ctx, "bwrite", handle, String("B5"),
					Int('a'), Int('b'), Int('c'), Int('d'), Int('e'))
				return err
			},
			want: "abc",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, err := New(
				WithLimits(Limits{MaxOutputBytesPerRuntime: 3}),
				WithStdout(io.Discard),
				WithStderr(io.Discard),
			)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := runtimeInstance.Invoke(context.Background(), "allocate")
			if err != nil {
				t.Fatal(err)
			}
			assertOutputLimitError(t, test.write(context.Background(), runtimeInstance, handle), 3)

			handleObject, ok := ioHandleValue(handle)
			if !ok {
				t.Fatalf("allocate returned %s", handle.Describe())
			}
			handleObject.mu.Lock()
			phase := handleObject.memoryPhase
			handleObject.mu.Unlock()
			if phase == memoryIOWrite {
				if err := handleObject.close(); err != nil {
					t.Fatalf("close memory handle: %v", err)
				}
			}
			data, err := handleObject.readBytes(-1)
			if err != nil {
				t.Fatalf("read partial memory output: %v", err)
			}
			if got := string(data); got != test.want {
				t.Fatalf("memory output = %q, want %q", got, test.want)
			}
			_, poisonedErr := runtimeInstance.Invoke(context.Background(), "size", String("no output"))
			assertOutputLimitError(t, poisonedErr, 3)
		})
	}
}

func TestOutputLimitCoversImporterWriterTargets(t *testing.T) {
	var target bytes.Buffer
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: 3}),
		WithStdout(io.Discard),
		WithStderr(io.Discard),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = runtimeInstance.Invoke(
		context.Background(),
		"print",
		ObjectValue(&target),
		String("abcde"),
	)
	assertOutputLimitError(t, err, 3)
	if got := target.String(); got != "abc" {
		t.Fatalf("importer writer prefix = %q, want abc", got)
	}
}

func TestOutputLimitExplicitFileAndCopyWritesPreserveAllowedPrefix(t *testing.T) {
	t.Run("openf", func(t *testing.T) {
		directory := t.TempDir()
		runtimeInstance, err := New(WithLimits(Limits{MaxOutputBytesPerRuntime: 3}))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeInstance.Invoke(context.Background(), "chdir", String(directory)); err != nil {
			t.Fatal(err)
		}
		handle, err := runtimeInstance.Invoke(context.Background(), "openf", String(">partial.bin"))
		if err != nil {
			t.Fatal(err)
		}
		handleObject, ok := ioHandleValue(handle)
		if !ok {
			t.Fatalf("openf returned %s", handle.Describe())
		}
		_, err = runtimeInstance.Invoke(context.Background(), "writeb", handle, String("abcde"))
		assertOutputLimitError(t, err, 3)
		handleObject.mu.Lock()
		writerOpen := handleObject.writer != nil
		handleObject.mu.Unlock()
		if writerOpen {
			t.Fatal("writeb quota failure left file handle open")
		}
		data, err := os.ReadFile(filepath.Join(directory, "partial.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(data); got != "abc" {
			t.Fatalf("file output = %q, want abc", got)
		}
	})

	t.Run("copyFile", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "source.bin"), []byte("abcdefghij"), 0o600); err != nil {
			t.Fatal(err)
		}
		runtimeInstance, err := New(WithLimits(Limits{MaxOutputBytesPerRuntime: 4}))
		if err != nil {
			t.Fatal(err)
		}
		// copyFile is a retained convenience implementation, not a stock
		// namespace claim. Opt it in with the same isolated I/O state as chdir.
		ioFunctions := runtimeInstance.ioFunctions()
		if err := runtimeInstance.RegisterFunction("chdir", ioFunctions["chdir"]); err != nil {
			t.Fatal(err)
		}
		if err := runtimeInstance.RegisterFunction("copyFile", ioFunctions["copyFile"]); err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeInstance.Invoke(context.Background(), "chdir", String(directory)); err != nil {
			t.Fatal(err)
		}
		_, err = runtimeInstance.Invoke(context.Background(), "copyFile", String("source.bin"), String("destination.bin"))
		assertOutputLimitError(t, err, 4)
		data, err := os.ReadFile(filepath.Join(directory, "destination.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(data); got != "abcd" {
			t.Fatalf("copied output = %q, want abcd", got)
		}
	})
}

func TestOutputLimitBinaryProducers(t *testing.T) {
	tests := []struct {
		name   string
		limit  uint64
		invoke func(context.Context, *Runtime) error
	}{
		{
			name:  "pack padded string",
			limit: 4,
			invoke: func(ctx context.Context, runtimeInstance *Runtime) error {
				_, err := runtimeInstance.Invoke(ctx, "pack", String("Z1000000000"), String("x"))
				return err
			},
		},
		{
			name:  "gzip",
			limit: 5,
			invoke: func(ctx context.Context, runtimeInstance *Runtime) error {
				_, err := runtimeInstance.Invoke(ctx, "gzip", String("payload"))
				return err
			},
		},
		{
			name:  "bof pack",
			limit: 3,
			invoke: func(ctx context.Context, runtimeInstance *Runtime) error {
				_, err := runtimeInstance.Invoke(ctx, "bof_pack", String("beacon"), String("i"), Int(1))
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, err := New(
				WithLimits(Limits{MaxOutputBytesPerRuntime: test.limit}),
				WithStdout(io.Discard),
				WithStderr(io.Discard),
			)
			if err != nil {
				t.Fatal(err)
			}
			assertOutputLimitError(t, test.invoke(context.Background(), runtimeInstance), test.limit)
			if got := runtimeInstance.resources.used(resourceOutputBytes); got != test.limit {
				t.Fatalf("accounted output = %d, want %d", got, test.limit)
			}
		})
	}
}

func TestOutputLimitBacktickCapturesStdoutAndStderr(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{name: "stdout", command: "printf abcdef", want: []string{"abcd"}},
		{name: "stderr", command: "printf abcdef >&2"},
	}
	if runtimePackageGOOS() == "windows" {
		tests[0].command = "<nul set /p=abcdef"
		tests[1].command = "<nul set /p=abcdef 1>&2"
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, err := New(WithLimits(Limits{MaxOutputBytesPerRuntime: 4}))
			if err != nil {
				t.Fatal(err)
			}
			value, err := runtimeInstance.Invoke(context.Background(), "__EXEC__", String(test.command))
			assertOutputLimitError(t, err, 4)
			if got := arrayStrings(t, value); strings.Join(got, "\x00") != strings.Join(test.want, "\x00") {
				t.Fatalf("partial backtick stdout = %q, want %q", got, test.want)
			}
		})
	}
}

func TestOutputLimitProcessObjectCapturesStdoutAndStderrWithoutDeadlock(t *testing.T) {
	for _, mode := range []string{"quota-stdout", "quota-stderr"} {
		t.Run(mode, func(t *testing.T) {
			runtimeInstance, err := New(
				WithLimits(Limits{MaxOutputBytesPerRuntime: 5}),
				WithStdout(io.Discard),
				WithStderr(io.Discard),
			)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			handle, err := runtimeInstance.Invoke(ctx, "exec", processHelperCommand(mode))
			if err != nil {
				t.Fatalf("exec: %v", err)
			}
			handleObject, ok := ioHandleValue(handle)
			if !ok {
				t.Fatalf("exec returned %s", handle.Describe())
			}
			process := handleObject.getProcess()
			if process == nil {
				t.Fatal("exec handle has no process")
			}
			if mode == "quota-stdout" {
				data, readErr := handleObject.readBytes(5)
				if readErr != nil {
					t.Fatalf("read allowed process prefix: %v", readErr)
				}
				if got := string(data); got != "01234" {
					t.Fatalf("process stdout prefix = %q, want 01234", got)
				}
			}
			_, readErr := handleObject.readBytes(-1)
			assertOutputLimitError(t, readErr, 5)
			_, waitErr := process.wait(ctx, runtimeInstance, Invocation{Name: "wait"})
			assertOutputLimitError(t, waitErr, 5)
			if closeErr := process.close(); closeErr != nil {
				t.Fatalf("closef: %v", closeErr)
			}
		})
	}
}

func TestOutputLimitConcurrentConsoleWritesRemainHardBounded(t *testing.T) {
	const limit = uint64(257)
	var output bytes.Buffer
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: limit}),
		WithStdout(&output),
		WithStderr(io.Discard),
	)
	if err != nil {
		t.Fatal(err)
	}
	var failures atomic.Int64
	var wait sync.WaitGroup
	for index := 0; index < 100; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, invokeErr := runtimeInstance.Invoke(context.Background(), "print", String("xxxxxxx"))
			if invokeErr != nil {
				if !errors.Is(invokeErr, ErrResourceLimit) {
					t.Errorf("concurrent print error = %v", invokeErr)
				}
				failures.Add(1)
			}
		}()
	}
	wait.Wait()
	if failures.Load() == 0 {
		t.Fatal("concurrent writes did not report exhaustion")
	}
	if got := output.Len(); got != int(limit) {
		t.Fatalf("bounded output length = %d, want %d", got, limit)
	}
}

func TestOutputLimitScriptLoaderChildSharesParentAccount(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: 5}),
		WithStdout(&output),
		WithStderr(io.Discard),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtimeInstance.Eval(context.Background(), "child-output-limit.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "child", 'print("abcdef");', $null];
[$child runScript];
`)
	assertOutputLimitError(t, err, 5)
	if got := output.String(); got != "abcde" {
		t.Fatalf("shared child output = %q, want abcde", got)
	}
}

func TestOutputLimitIgnoredWarningPoisonsRuntimeFamily(t *testing.T) {
	var warnings bytes.Buffer
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: 8}),
		WithStdout(io.Discard),
		WithStderr(&warnings),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtimeInstance.Eval(context.Background(), "ignored-warning-limit.sl", `missing_function();`)
	assertOutputLimitError(t, err, 8)
	if got := warnings.Len(); got != 8 {
		t.Fatalf("warning prefix length = %d, want 8", got)
	}
	_, err = runtimeInstance.Invoke(context.Background(), "size", String("no output"))
	assertOutputLimitError(t, err, 8)

	const callers = 16
	results := make(chan error, callers)
	var wait sync.WaitGroup
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, invokeErr := runtimeInstance.Invoke(context.Background(), "size", String("still no output"))
			results <- invokeErr
		}()
	}
	wait.Wait()
	close(results)
	for invokeErr := range results {
		assertOutputLimitError(t, invokeErr, 8)
	}
}

func TestOutputLimitFinalReturnDiagnosticCannotEscapeSuccess(t *testing.T) {
	program, err := CompileString("final-diagnostic-limit.sl", `
sub run {
    return missing_function();
}
`)
	if err != nil {
		t.Fatal(err)
	}
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: 1}),
		WithStdout(io.Discard),
		WithStderr(io.Discard),
	)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	value, callErr := script.Call(context.Background(), "run")
	assertOutputLimitError(t, callErr, 1)
	if !value.IsNull() {
		t.Fatalf("final diagnostic result = %s, want null", value.Describe())
	}
}

func TestOutputLimitStickyFailureRejectsLaterWorkBeforeSideEffects(t *testing.T) {
	program, err := CompileString("sticky-script.sl", `sub later { later_host(); }`)
	if err != nil {
		t.Fatal(err)
	}
	laterProgram, err := CompileString("sticky-load.sl", `later_host();`)
	if err != nil {
		t.Fatal(err)
	}

	var hostCalls atomic.Int64
	runtimeInstance, err := New(
		WithLimits(Limits{
			MaxCollectionEntriesPerRuntime: 1 << 20,
			MaxOutputBytesPerRuntime:       1,
			MaxSourceBytesPerRuntime:       1 << 20,
		}),
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if err := script.Set("marker", Int(1)); err != nil {
		t.Fatal(err)
	}

	_, err = runtimeInstance.Invoke(context.Background(), "print", String("xx"))
	assertOutputLimitError(t, err, 1)

	_, err = runtimeInstance.Invoke(context.Background(), "later_host")
	assertOutputLimitError(t, err, 1)
	_, err = script.Call(context.Background(), "later")
	assertOutputLimitError(t, err, 1)
	if err := script.Set("marker", Int(2)); err == nil {
		t.Fatal("poisoned Script.Set succeeded")
	} else {
		assertOutputLimitError(t, err, 1)
	}
	if got := script.Get("marker").Int64(); got != 1 {
		t.Fatalf("marker changed after rejected Set: %d", got)
	}
	if got := hostCalls.Load(); got != 0 {
		t.Fatalf("host side effects after poison = %d, want 0", got)
	}

	sourceBefore := runtimeInstance.resources.used(resourceSourceBytes)
	collectionsBefore := runtimeInstance.resources.used(resourceCollectionEntries)
	_, err = runtimeInstance.Eval(context.Background(), "sticky-eval.sl", `@values = @(1, 2, 3);`)
	assertOutputLimitError(t, err, 1)
	if _, err := runtimeInstance.Load(context.Background(), laterProgram); err == nil {
		t.Fatal("poisoned Runtime.Load succeeded")
	} else {
		assertOutputLimitError(t, err, 1)
	}
	if got := runtimeInstance.resources.used(resourceSourceBytes); got != sourceBefore {
		t.Fatalf("source bytes advanced after rejected work: %d -> %d", sourceBefore, got)
	}
	if got := runtimeInstance.resources.used(resourceCollectionEntries); got != collectionsBefore {
		t.Fatalf("collection entries advanced after rejected work: %d -> %d", collectionsBefore, got)
	}
}

func TestOutputLimitPartialReservationPoisonsBeforeBlockingSinkCompletes(t *testing.T) {
	writer := &blockingQuotaWriter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(writer.release) }) }
	defer release()

	var hostCalls atomic.Int64
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: 3}),
		WithStdout(writer),
		WithStderr(io.Discard),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}

	printResult := make(chan error, 1)
	go func() {
		_, printErr := runtimeInstance.Invoke(context.Background(), "print", String("abcdef"))
		printResult <- printErr
	}()
	select {
	case <-writer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("allowed prefix did not reach blocking sink")
	}

	_, err = runtimeInstance.Invoke(context.Background(), "later_host")
	assertOutputLimitError(t, err, 3)
	if got := hostCalls.Load(); got != 0 {
		t.Fatalf("host calls while rejected prefix was blocked = %d, want 0", got)
	}

	release()
	select {
	case printErr := <-printResult:
		assertOutputLimitError(t, printErr, 3)
	case <-time.After(5 * time.Second):
		t.Fatal("blocked print did not complete")
	}
	if got := writer.buffer.String(); got != "abc" {
		t.Fatalf("blocking sink prefix = %q, want abc", got)
	}
}

func TestOutputLimitStopsAlreadyAdmittedScriptBeforeLaterHostEffect(t *testing.T) {
	program, err := CompileString("active-output-poison.sl", `
sub run {
    barrier();
    later_host();
}
`)
	if err != nil {
		t.Fatal(err)
	}

	barrierEntered := make(chan struct{})
	barrierRelease := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(barrierRelease) }) }
	defer release()
	var laterCalls atomic.Int64
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: 1}),
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
			switch invocation.Name {
			case "barrier":
				close(barrierEntered)
				<-barrierRelease
			case "later_host":
				laterCalls.Add(1)
			}
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	callResult := make(chan error, 1)
	go func() {
		_, callErr := script.Call(context.Background(), "run")
		callResult <- callErr
	}()
	select {
	case <-barrierEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("script did not enter barrier Host call")
	}

	_, err = runtimeInstance.Invoke(context.Background(), "print", String("xx"))
	assertOutputLimitError(t, err, 1)
	release()
	select {
	case callErr := <-callResult:
		assertOutputLimitError(t, callErr, 1)
	case <-time.After(5 * time.Second):
		t.Fatal("already-admitted script did not stop after output poison")
	}
	if got := laterCalls.Load(); got != 0 {
		t.Fatalf("later Host side effects after concurrent output poison = %d, want 0", got)
	}
}

func TestOutputLimitImporterLimitErrorDoesNotPoisonRuntimeFamily(t *testing.T) {
	fakeErr := &LimitError{Resource: resourceOutputBytes, Limit: 999}
	var hostCalls atomic.Int64
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: 10}),
		WithStdout(fakeLimitErrorWriter{err: fakeErr}),
		WithStderr(io.Discard),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = runtimeInstance.Invoke(context.Background(), "print", String("x"))
	assertOutputLimitError(t, err, 999)
	if err := runtimeInstance.outputLimitError(); err != nil {
		t.Fatalf("importer error poisoned family: %v", err)
	}
	if got := runtimeInstance.resources.used(resourceOutputBytes); got != 1 {
		t.Fatalf("reserved output after importer failure = %d, want 1", got)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "later_host"); err != nil {
		t.Fatalf("later Host call after importer error: %v", err)
	}
	if got := hostCalls.Load(); got != 1 {
		t.Fatalf("later Host calls = %d, want 1", got)
	}
}

func TestOutputLimitCannotBeSwallowedByArbitraryCallableBoundaries(t *testing.T) {
	newOwner := func(t *testing.T) (*Runtime, *Script, Callable) {
		t.Helper()
		runtimeInstance, err := New(
			WithLimits(Limits{MaxOutputBytesPerRuntime: 1}),
			WithStdout(io.Discard),
			WithStderr(io.Discard),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		program, err := CompileString("arbitrary-callable-output-limit.sl", `sub placeholder { return 1; }`)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		callable := CallableFunc(func(ctx context.Context, _ ...Value) (Value, error) {
			// Importer callables may invoke OPFOR again and mishandle the returned
			// error. The enclosing public evaluator boundary must still observe
			// the family-wide fatal output latch.
			_, _ = runtimeInstance.Invoke(ctx, "print", String("xx"))
			return String("swallowed"), nil
		})
		return runtimeInstance, script, callable
	}

	t.Run("Script.Call", func(t *testing.T) {
		_, script, callable := newOwner(t)
		if err := script.setFunction("swallow", callable); err != nil {
			t.Fatal(err)
		}
		_, err := script.Call(context.Background(), "swallow")
		assertOutputLimitError(t, err, 1)
	})

	for _, test := range []struct {
		name       string
		invokeName string
	}{
		{name: "retained callback"},
		{name: "named retained callback", invokeName: "&callback"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, script, callable := newOwner(t)
			retained, err := (Invocation{
				Runtime: runtimeInstance,
				Script:  script.id,
				Arguments: []Argument{{
					Value: FunctionValue(callable),
				}},
			}).Callback(0)
			if err != nil {
				t.Fatal(err)
			}
			if test.invokeName == "" {
				_, err = retained.Invoke(context.Background())
			} else {
				callback, ok := retained.(*invocationCallback)
				if !ok {
					t.Fatalf("retained callback type = %T", retained)
				}
				_, err = callback.invokeNamed(context.Background(), test.invokeName)
			}
			assertOutputLimitError(t, err, 1)
		})
	}
}

func TestOutputLimitFatalLatchPrecedesCallableError(t *testing.T) {
	boom := errors.New("importer boom")
	newRuntime := func(t *testing.T, options ...Option) *Runtime {
		t.Helper()
		options = append([]Option{
			WithLimits(Limits{MaxOutputBytesPerRuntime: 1}),
			WithStdout(io.Discard),
			WithStderr(io.Discard),
		}, options...)
		runtimeInstance, err := New(options...)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		return runtimeInstance
	}
	assertBoth := func(t *testing.T, err error) {
		t.Helper()
		assertOutputLimitError(t, err, 1)
		if !errors.Is(err, boom) {
			t.Fatalf("error = %v, want importer boom joined after fatal latch", err)
		}
	}
	loadOwner := func(t *testing.T, runtimeInstance *Runtime) *Script {
		t.Helper()
		program, err := CompileString("poison-and-error.sl", `sub placeholder { return 1; }`)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		return script
	}

	t.Run("Runtime.Invoke Host", func(t *testing.T) {
		var runtimeInstance *Runtime
		runtimeInstance = newRuntime(t, WithHost(HostFunc(func(ctx context.Context, _ Invocation) (Value, error) {
			_, _ = runtimeInstance.Invoke(ctx, "print", String("xx"))
			return Null(), boom
		})))
		_, err := runtimeInstance.Invoke(context.Background(), "importer_call")
		assertBoth(t, err)
	})

	t.Run("Script.Call arbitrary callable", func(t *testing.T) {
		runtimeInstance := newRuntime(t)
		script := loadOwner(t, runtimeInstance)
		if err := script.setFunction("poison", CallableFunc(func(ctx context.Context, _ ...Value) (Value, error) {
			_, _ = runtimeInstance.Invoke(ctx, "print", String("xx"))
			return Null(), boom
		})); err != nil {
			t.Fatal(err)
		}
		_, err := script.Call(context.Background(), "poison")
		assertBoth(t, err)
	})

	t.Run("retained callback", func(t *testing.T) {
		runtimeInstance := newRuntime(t)
		script := loadOwner(t, runtimeInstance)
		callable := CallableFunc(func(ctx context.Context, _ ...Value) (Value, error) {
			_, _ = runtimeInstance.Invoke(ctx, "print", String("xx"))
			return Null(), boom
		})
		retained, err := (Invocation{
			Runtime: runtimeInstance,
			Script:  script.id,
			Arguments: []Argument{{
				Value: FunctionValue(callable),
			}},
		}).Callback(0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = retained.Invoke(context.Background())
		assertBoth(t, err)
	})

	t.Run("script native callable", func(t *testing.T) {
		runtimeInstance := newRuntime(t)
		script := loadOwner(t, runtimeInstance)
		if err := script.RegisterFunction("poison_native", func(ctx context.Context, _ Invocation) (Value, error) {
			_, _ = runtimeInstance.Invoke(ctx, "print", String("xx"))
			return Null(), boom
		}); err != nil {
			t.Fatal(err)
		}
		_, err := script.Call(context.Background(), "poison_native")
		assertBoth(t, err)
	})
}

func TestOutputLimitLatchErrorsAreImmutableSnapshots(t *testing.T) {
	t.Run("runtime family", func(t *testing.T) {
		runtimeInstance, err := New(
			WithLimits(Limits{MaxOutputBytesPerRuntime: 1}),
			WithStdout(io.Discard),
			WithStderr(io.Discard),
		)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runtimeInstance.Invoke(context.Background(), "print", String("xx"))
		assertOutputLimitError(t, err, 1)

		// Observe the stored family latch through a later evaluator boundary,
		// then mutate the caller-owned error value. Future observations must be
		// independent snapshots of the original output failure.
		_, err = runtimeInstance.Invoke(context.Background(), "size", String("ignored"))
		var observed *LimitError
		if !errors.As(err, &observed) {
			t.Fatalf("latched error = %v, want *LimitError", err)
		}
		observed.Resource = resourceInstruction
		observed.Limit = 999

		_, err = runtimeInstance.Invoke(context.Background(), "size", String("ignored"))
		assertOutputLimitError(t, err, 1)
		if errors.Is(err, ErrInstructionLimit) {
			t.Fatalf("mutated output latch matches ErrInstructionLimit: %v", err)
		}
	})

	t.Run("writer", func(t *testing.T) {
		account := newRuntimeResourceAccount(Limits{MaxOutputBytesPerRuntime: 1})
		writer := newRuntimeOutputWriter(account, io.Discard)
		if _, err := writer.Write([]byte("xx")); err == nil {
			t.Fatal("oversized writer call succeeded")
		}
		first, ok := writer.LimitError().(*LimitError)
		if !ok {
			t.Fatalf("writer limit error = %T, want *LimitError", writer.LimitError())
		}
		first.Resource = resourceInstruction
		first.Limit = 999
		assertOutputLimitError(t, writer.LimitError(), 1)
	})
}

func TestOutputLimitCannotBeSwallowedByVariableContainer(t *testing.T) {
	newScript := func(t *testing.T) (*Runtime, *Script, *outputPoisonVariableContainer) {
		t.Helper()
		var runtimeInstance *Runtime
		container := &outputPoisonVariableContainer{
			cells: map[string]*Cell{"$target": NewCell(String("before"))},
			runtime: func() *Runtime {
				return runtimeInstance
			},
		}
		var err error
		runtimeInstance, err = New(
			WithLimits(Limits{MaxOutputBytesPerRuntime: 1}),
			WithStdout(io.Discard),
			WithStderr(io.Discard),
			WithVariableProvider(&outputPoisonVariableProvider{container: container}),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		program, err := CompileString("variable-provider-output-limit.sl", `sub noop { return 1; }`)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		return runtimeInstance, script, container
	}

	t.Run("SetContext does not commit after poisoned lookup", func(t *testing.T) {
		_, script, container := newScript(t)
		container.setPoison(VariableProviderExists)
		err := script.SetContext(context.Background(), "$target", String("after"))
		assertOutputLimitError(t, err, 1)
		container.mu.Lock()
		value := container.cells["$target"].Get()
		container.mu.Unlock()
		if got := value.String(); got != "before" {
			t.Fatalf("provider cell after rejected SetContext = %q, want before", got)
		}
	})

	t.Run("BindVariable reports poison after provider commit", func(t *testing.T) {
		_, script, container := newScript(t)
		container.setPoison(VariableProviderPut)
		cell := NewCell(String("bound"))
		err := script.BindVariable(context.Background(), "$bound", cell)
		assertOutputLimitError(t, err, 1)
		container.mu.Lock()
		stored := container.cells["$bound"]
		container.mu.Unlock()
		if stored != cell {
			t.Fatal("provider PutScalar did not retain the committed cell")
		}
	})

	t.Run("UnsetVariable reports poison after provider commit", func(t *testing.T) {
		_, script, container := newScript(t)
		container.setPoison(VariableProviderRemove)
		err := script.UnsetVariable(context.Background(), "$target")
		assertOutputLimitError(t, err, 1)
		container.mu.Lock()
		stored := container.cells["$target"]
		container.mu.Unlock()
		if stored != nil {
			t.Fatal("provider RemoveScalar did not commit before reporting the fatal latch")
		}
	})
}

func TestOutputLimitStopsPoisonedSourceResolverBeforeAdmission(t *testing.T) {
	boom := errors.New("resolver boom")
	var runtimeInstance *Runtime
	resolver := SourceResolverFunc(func(ctx context.Context, request SourceRequest) (Source, error) {
		_, _ = runtimeInstance.Invoke(ctx, "print", String("xx"))
		return NewSource(request.Name, []byte(`return "must not compile";`)), boom
	})
	var err error
	runtimeInstance, err = New(
		WithLimits(Limits{
			MaxOutputBytesPerRuntime: 1,
			MaxSourceBytesPerRuntime: 1 << 20,
		}),
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithSourceResolver(resolver),
	)
	if err != nil {
		t.Fatal(err)
	}
	executionCtx, release, err := runtimeInstance.acquireRuntimeExecution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	before := runtimeInstance.resources.used(resourceSourceBytes)
	_, err = runtimeInstance.resolveSourceRequest(executionCtx, SourceRequest{Name: "child.sl"})
	assertOutputLimitError(t, err, 1)
	if !errors.Is(err, boom) {
		t.Fatalf("resolver error = %v, want resolver boom joined after fatal latch", err)
	}
	if got := runtimeInstance.resources.used(resourceSourceBytes); got != before {
		t.Fatalf("source usage advanced after poisoned resolver: %d -> %d", before, got)
	}
}

func TestOutputLimitStopsPoisonedObserversBeforeFollowOnState(t *testing.T) {
	boom := errors.New("observer boom")
	baseOptions := func() []Option {
		return []Option{
			WithLimits(Limits{MaxOutputBytesPerRuntime: 1}),
			WithStdout(io.Discard),
			WithStderr(io.Discard),
		}
	}

	t.Run("script lifecycle before top-level execution", func(t *testing.T) {
		var runtimeInstance *Runtime
		var hostCalls atomic.Int64
		options := append(baseOptions(),
			WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
				hostCalls.Add(1)
				return Null(), nil
			})),
			WithScriptLifecycleObserver(ScriptLifecycleFuncs{
				Loaded: func(ctx context.Context, _ *Script) error {
					_, _ = runtimeInstance.Invoke(ctx, "print", String("xx"))
					return boom
				},
			}),
		)
		var err error
		runtimeInstance, err = New(options...)
		if err != nil {
			t.Fatal(err)
		}
		program, err := CompileString("poisoned-lifecycle.sl", `after_load();`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runtimeInstance.Load(context.Background(), program)
		assertOutputLimitError(t, err, 1)
		if !errors.Is(err, boom) {
			t.Fatalf("lifecycle error = %v, want observer boom", err)
		}
		if got := hostCalls.Load(); got != 0 {
			t.Fatalf("top-level Host calls after poisoned lifecycle observer = %d, want 0", got)
		}
	})

	t.Run("binding observer before shared publication", func(t *testing.T) {
		var runtimeInstance *Runtime
		observer := &outputPoisonBindingObserver{
			runtime: func() *Runtime { return runtimeInstance },
			err:     boom,
		}
		options := append(baseOptions(), WithBindingObserver(observer))
		var err error
		runtimeInstance, err = New(options...)
		if err != nil {
			t.Fatal(err)
		}
		program, err := CompileString("poisoned-binding-observer.cna", ``)
		if err != nil {
			t.Fatal(err)
		}
		script, err := runtimeInstance.Load(context.Background(), program)
		if err != nil {
			t.Fatal(err)
		}
		executionCtx, release, err := script.acquireExecution(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = release() }()
		err = script.registerBinding(executionCtx, Binding{
			Kind: BindingAlias, Keyword: "alias", Name: "poison",
		}, CallableFunc(func(context.Context, ...Value) (Value, error) {
			return Null(), nil
		}))
		assertOutputLimitError(t, err, 1)
		if !errors.Is(err, boom) {
			t.Fatalf("binding observer error = %v, want observer boom", err)
		}
		if got := len(runtimeInstance.Bindings(BindingAlias, "poison")); got != 0 {
			t.Fatalf("bindings after poisoned Registered callback = %d, want 0", got)
		}
	})

	t.Run("client UI provider before popup clear", func(t *testing.T) {
		var runtimeInstance *Runtime
		provider := AggressorClientUIProviderFunc(func(ctx context.Context, _ AggressorClientUIRequest) (Value, error) {
			_, _ = runtimeInstance.Invoke(ctx, "print", String("xx"))
			return Null(), boom
		})
		options := append(baseOptions(), WithAggressorClientUIProvider(provider))
		var err error
		runtimeInstance, err = New(options...)
		if err != nil {
			t.Fatal(err)
		}
		program, err := CompileString("poisoned-popup-clear.cna", `
popup preserved_popup {
	item "child" { return; }
}
`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
			t.Fatal(err)
		}
		if got := len(runtimeInstance.Bindings(BindingPopup, "preserved_popup")); got != 1 {
			t.Fatalf("initial popup bindings = %d, want 1", got)
		}
		_, err = runtimeInstance.Invoke(context.Background(), "popup_clear", String("preserved_popup"))
		assertOutputLimitError(t, err, 1)
		if !errors.Is(err, boom) {
			t.Fatalf("client UI error = %v, want observer boom", err)
		}
		if got := len(runtimeInstance.Bindings(BindingPopup, "preserved_popup")); got != 1 {
			t.Fatalf("popup bindings after poisoned provider = %d, want 1", got)
		}
	})
}
