package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type rawImporterErrorReader struct{ err error }

func (reader rawImporterErrorReader) Read([]byte) (int, error) { return 0, reader.err }

type rawImporterErrorWriter struct{ err error }

func (writer rawImporterErrorWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestIOHandleAllowsConcurrentFullDuplexTraffic(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	handle := newIOHandle("duplex-test", left, left, true, true, false)
	defer handle.close()

	peerDone := make(chan error, 1)
	go func() {
		request := make([]byte, 4)
		if _, err := io.ReadFull(right, request); err != nil {
			peerDone <- err
			return
		}
		if string(request) != "ping" {
			peerDone <- fmt.Errorf("request = %q", request)
			return
		}
		_, err := right.Write([]byte("pong"))
		peerDone <- err
	}()

	response := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		data := make([]byte, 4)
		_, err := io.ReadFull(handle, data)
		if err != nil {
			readErr <- err
			return
		}
		response <- string(data)
	}()

	if _, err := handle.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("peer blocked; read and write appear to share one structural lock")
	}
	select {
	case got := <-response:
		if got != "pong" {
			t.Fatalf("response = %q, want pong", got)
		}
	case err := <-readErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("duplex read blocked after peer response")
	}
}

func TestIOFunctionsExposePortableSurface(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	for _, name := range []string{
		"allocate", "getConsole", "closef", "connect", "listen", "readln", "readAll", "readc", "read",
		"readb", "bread", "consume", "skip", "writeb", "bwrite", "available", "mark", "reset", "printEOF", "openf", "sleep",
		"setEncoding",
		"cwd", "pwd", "getCurrentDirectory", "chdir", "ls", "listRoots", "lof", "mkdir",
		"createNewFile", "getFileProper", "lastModified", "setLastModified", "setReadOnly",
		"deleteFile", "move", "rename", "copyFile", "dirname", "getFileParent",
		"getFileName", "-canread", "-canwrite", "-exists", "-isDir", "-isFile",
		"-isHidden", "-eof", "-e", "-f", "-d", "__EXEC__", "exec",
	} {
		if functions[name] == nil {
			t.Errorf("ioFunctions()[%q] is nil", name)
		}
	}
}

func TestRawImporterIOErrorsRemainAuthoritativeThroughPublicRuntimeEntries(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		for _, test := range []struct {
			name      string
			function  string
			arguments []Value
			source    string
			option    Option
		}{
			{
				name:     "Invoke/readln",
				function: "readln",
				option:   WithStdin(rawImporterErrorReader{err: boundaryErr}),
			},
			{
				name:   "Eval/readln",
				source: `readln();`,
				option: WithStdin(rawImporterErrorReader{err: boundaryErr}),
			},
			{
				name:      "Invoke/writeb",
				function:  "writeb",
				arguments: []Value{String("raw bytes")},
				option:    WithStdout(rawImporterErrorWriter{err: boundaryErr}),
			},
			{
				name:   "Eval/writeb",
				source: `writeb("raw bytes");`,
				option: WithStdout(rawImporterErrorWriter{err: boundaryErr}),
			},
		} {
			test := test
			t.Run(test.name+"/"+boundaryErr.Error(), func(t *testing.T) {
				runtimeInstance, err := New(test.option)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

				var result Value
				if test.source != "" {
					result, err = runtimeInstance.Eval(context.Background(), "raw-importer-io-error.sl", test.source)
				} else {
					result, err = runtimeInstance.Invoke(context.Background(), test.function, test.arguments...)
				}
				if !errors.Is(err, boundaryErr) || !result.IsNull() {
					t.Fatalf("public call = (%s, %v), want null/%v", result.Describe(), err, boundaryErr)
				}
			})
		}
	}
}

func TestAllocatedBufferLifecycleAndLineEndings(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	handle := mustCallIOBuiltin(t, runtime, functions, "allocate", Int(8))

	if available := mustCallIOBuiltin(t, runtime, functions, "available", handle); !available.IsNull() {
		t.Fatalf("available before close = %s, want $null", available.Describe())
	}
	content := "one\r\ntwo\rthree\nraw"
	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, String(content))
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	if available := mustCallIOBuiltin(t, runtime, functions, "available", handle); available.Int64() != int64(len(content)) {
		t.Fatalf("available = %s, want %d", available.Describe(), len(content))
	}

	for _, want := range []string{"one", "two", "three"} {
		if got := mustCallIOBuiltin(t, runtime, functions, "readln", handle).String(); got != want {
			t.Fatalf("readln = %q, want %q", got, want)
		}
	}
	// Sleep's InputStreamReader reads ahead from the shared binary stream.
	// The final bytes are still available to readln, but no longer visible to
	// BufferedInputStream.available or readb.
	if available := mustCallIOBuiltin(t, runtime, functions, "available", handle); available.Int64() != 0 || available.Kind() != KindInt {
		t.Fatalf("available after text read-ahead = %s, want integer 0", available.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(-1)); !got.IsNull() {
		t.Fatalf("readb after text read-ahead = %s, want $null", got.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readln", handle).String(); got != "raw" {
		t.Fatalf("readln from text read-ahead = %q, want raw", got)
	}
	if available := mustCallIOBuiltin(t, runtime, functions, "available", handle); !available.IsNull() {
		t.Fatalf("available after unterminated final line = %s, want $null", available.Describe())
	}

	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	if value := mustCallIOBuiltin(t, runtime, functions, "readln", handle); !value.IsNull() {
		t.Fatalf("readln after second close = %s, want $null", value.Describe())
	}
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "writeb", handle, String("late")); err == nil || !strings.Contains(err.Error(), "not open for writing") {
		t.Fatalf("writeb after close error = %v", err)
	}
}

func TestMemoryIOMarkResetAndTextReadAhead(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	handle := mustCallIOBuiltin(t, runtime, functions, "allocate", Int(32))
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "mark", handle); err == nil || !strings.Contains(err.Error(), "input buffer") || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("mark before write-to-read transition error = %v", err)
	}
	// The reference reset bridge intentionally ignores closed and invalid-mark
	// errors.
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "reset", handle); err != nil {
		t.Fatalf("reset before close: %v", err)
	}

	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, String("one\ntwo\n"))
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	// reset without a prior mark is a no-op.
	mustCallIOBuiltin(t, runtime, functions, "reset", handle)
	if available := mustCallIOBuiltin(t, runtime, functions, "available", handle); available.Int64() != 8 {
		t.Fatalf("available after invalid reset = %s, want 8", available.Describe())
	}

	mustCallIOBuiltin(t, runtime, functions, "mark", handle, Int(8))
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(1)).String(); got != "o" {
		t.Fatalf("initial byte = %q, want o", got)
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readln", handle).String(); got != "ne" {
		t.Fatalf("first line = %q, want ne", got)
	}
	if available := mustCallIOBuiltin(t, runtime, functions, "available", handle); available.Int64() != 0 || available.Kind() != KindInt {
		t.Fatalf("available after readln read-ahead = %s, want integer 0", available.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readln", handle).String(); got != "two" {
		t.Fatalf("second line from text read-ahead = %q, want two", got)
	}

	mustCallIOBuiltin(t, runtime, functions, "reset", handle)
	if available := mustCallIOBuiltin(t, runtime, functions, "available", handle); available.Int64() != 8 {
		t.Fatalf("available after reset = %s, want 8", available.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(-1)).String(); got != "one\ntwo\n" {
		t.Fatalf("replayed bytes = %q", got)
	}
	// Reset retains the mark and can replay the same range repeatedly.
	mustCallIOBuiltin(t, runtime, functions, "reset", handle)
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(-1)).String(); got != "one\ntwo\n" {
		t.Fatalf("second replay = %q", got)
	}
}

func TestMemoryIOMarkReadLimitAndResetErrors(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	handle := mustCallIOBuiltin(t, runtime, functions, "allocate")
	data := strings.Repeat("a", sleepIOReadBufferSize+1) + "z"
	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, String(data))
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	mustCallIOBuiltin(t, runtime, functions, "mark", handle, Int(2))
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(sleepIOReadBufferSize+1)).String(); got != data[:sleepIOReadBufferSize+1] {
		t.Fatalf("read beyond buffered mark capacity length = %d", len(got))
	}
	// A limit smaller than BufferedInputStream's 8192-byte storage remains valid
	// until a refill crosses that storage. BasicIO.reset then suppresses the
	// invalid-mark exception, so the next read continues at the current point.
	mustCallIOBuiltin(t, runtime, functions, "reset", handle)
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(-1)).String(); got != "z" {
		t.Fatalf("bytes after expired reset = %q, want z", got)
	}
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "mark", String("not a handle"), Int(1)); err == nil || !strings.Contains(err.Error(), "expected I/O handle") {
		t.Fatalf("mark invalid handle error = %v", err)
	}
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "reset", String("not a handle")); err != nil {
		t.Fatalf("reset invalid handle should be suppressed: %v", err)
	}
}

func TestSleepBufferGoldenCompatibility(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "buffer.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "buffer.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("buffer.sl", programBytes))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func TestMemoryIOMarkResetConcurrentSafety(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	handle := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, String(strings.Repeat("line\n", 512)))
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)

	start := make(chan struct{})
	errorsFound := make(chan error, 4)
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		worker := worker
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for iteration := 0; iteration < 100; iteration++ {
				var err error
				switch worker {
				case 0:
					_, err = callIOBuiltin(context.Background(), runtime, functions, "mark", handle, Int(128))
				case 1:
					_, err = callIOBuiltin(context.Background(), runtime, functions, "reset", handle)
				case 2:
					_, err = callIOBuiltin(context.Background(), runtime, functions, "available", handle)
				case 3:
					_, err = callIOBuiltin(context.Background(), runtime, functions, "readb", handle, Int(1))
				}
				if err != nil {
					errorsFound <- err
					return
				}
			}
		}()
	}
	close(start)
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent I/O operation: %v", err)
	}
}

func TestPrintEOFClosesMemoryOutputWithoutDiscardingData(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	handle := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, String("payload"))
	mustCallIOBuiltin(t, runtime, functions, "printEOF", handle)
	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, String("late"))
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "setEncoding", handle, String("NO-SUCH")); err == nil {
		t.Fatal("setEncoding after printEOF skipped validation")
	}

	// A following closef performs allocate's write-to-read transition.
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(-1)).String(); got != "payloadlate" {
		t.Fatalf("readb = %q, want payloadlate", got)
	}
}

func TestReadDispatchesLinesAndBinaryChunks(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()

	lineHandle := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "writeb", lineHandle, String("first\n\nthird"))
	mustCallIOBuiltin(t, runtime, functions, "closef", lineHandle)
	var lineCalls [][]Value
	lineCallback := FunctionValue(ioTestCallable(func(_ context.Context, values ...Value) (Value, error) {
		lineCalls = append(lineCalls, append([]Value(nil), values...))
		return Null(), nil
	}))
	mustCallIOBuiltin(t, runtime, functions, "read", lineHandle, lineCallback)
	mustCallIOBuiltin(t, runtime, functions, "wait", lineHandle)
	if got := callbackData(lineCalls); !reflect.DeepEqual(got, []string{"first", "", "third"}) {
		t.Fatalf("line callback data = %q", got)
	}
	for index, call := range lineCalls {
		if len(call) != 2 || !call[0].IdentityEqual(lineHandle) {
			t.Fatalf("line callback %d arguments = %#v", index, call)
		}
	}

	chunkHandle := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "writeb", chunkHandle, String("abcde"))
	mustCallIOBuiltin(t, runtime, functions, "closef", chunkHandle)
	var chunkCalls [][]Value
	chunkCallback := FunctionValue(ioTestCallable(func(_ context.Context, values ...Value) (Value, error) {
		chunkCalls = append(chunkCalls, append([]Value(nil), values...))
		return Null(), nil
	}))
	mustCallIOBuiltin(t, runtime, functions, "read", chunkHandle, chunkCallback, Int(2))
	mustCallIOBuiltin(t, runtime, functions, "wait", chunkHandle)
	if got := callbackData(chunkCalls); !reflect.DeepEqual(got, []string{"ab", "cd", "e"}) {
		t.Fatalf("chunk callback data = %q", got)
	}
}

func TestConsoleUsesRuntimeStreamsAndRemainsBorrowed(t *testing.T) {
	t.Parallel()

	input := strings.NewReader("console input\r\n")
	var output bytes.Buffer
	runtime, err := New(WithStdin(input), WithStdout(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	console := mustCallIOBuiltin(t, runtime, functions, "getConsole")
	if other := mustCallIOBuiltin(t, runtime, functions, "getConsole"); !console.IdentityEqual(other) {
		t.Fatal("getConsole returned different handles")
	}
	if available := mustCallIOBuiltin(t, runtime, functions, "available"); available.Int64() != int64(input.Len()) {
		t.Fatalf("console available = %s, want %d", available.Describe(), input.Len())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readln").String(); got != "console input" {
		t.Fatalf("console readln = %q", got)
	}

	mustCallIOBuiltin(t, runtime, functions, "writeb", console, String("one"))
	mustCallIOBuiltin(t, runtime, functions, "printEOF", console)
	mustCallIOBuiltin(t, runtime, functions, "writeb", console, String(" two"))
	mustCallIOBuiltin(t, runtime, functions, "closef", console)
	mustCallIOBuiltin(t, runtime, functions, "writeb", console, String(" three"))
	if got := output.String(); got != "one two three" {
		t.Fatalf("console output = %q", got)
	}
}

func TestPortableFilesystemBuiltins(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	root := t.TempDir()
	mustCallIOBuiltin(t, runtime, functions, "chdir", String(root))
	for _, name := range []string{"cwd", "pwd", "getCurrentDirectory"} {
		if got := mustCallIOBuiltin(t, runtime, functions, name).String(); got != root {
			t.Fatalf("%s = %q, want %q", name, got, root)
		}
	}

	if made := mustCallIOBuiltin(t, runtime, functions, "mkdir", String("nested")); !made.Truth() {
		t.Fatalf("mkdir = %s, want true", made.Describe())
	}
	if madeAgain := mustCallIOBuiltin(t, runtime, functions, "mkdir", String("nested")); !madeAgain.IsNull() {
		t.Fatalf("second mkdir = %s, want $null", madeAgain.Describe())
	}

	writeHandle := mustCallIOBuiltin(t, runtime, functions, "openf", String(">nested/data.bin"))
	mustCallIOBuiltin(t, runtime, functions, "writeb", writeHandle, String("abc"))
	mustCallIOBuiltin(t, runtime, functions, "closef", writeHandle)
	appendHandle := mustCallIOBuiltin(t, runtime, functions, "openf", String(">> nested/data.bin"))
	mustCallIOBuiltin(t, runtime, functions, "writeb", appendHandle, String("def"))
	mustCallIOBuiltin(t, runtime, functions, "closef", appendHandle)
	if length := mustCallIOBuiltin(t, runtime, functions, "lof", String("nested/data.bin")); length.Kind() != KindLong || length.Int64() != 6 {
		t.Fatalf("lof = %s, want long 6", length.Describe())
	}

	readHandle := mustCallIOBuiltin(t, runtime, functions, "openf", String("nested/data.bin"))
	if available := mustCallIOBuiltin(t, runtime, functions, "available", readHandle); available.Int64() != 6 {
		t.Fatalf("file available = %s, want 6", available.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", readHandle, Int(2)).String(); got != "ab" {
		t.Fatalf("first readb = %q", got)
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", readHandle, Int(-1)).String(); got != "cdef" {
		t.Fatalf("remaining readb = %q", got)
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", readHandle)

	if copied := mustCallIOBuiltin(t, runtime, functions, "copyFile", String("nested/data.bin"), String("nested/copy.bin")); !copied.Truth() {
		t.Fatalf("copyFile = %s, want true", copied.Describe())
	}
	if moved := mustCallIOBuiltin(t, runtime, functions, "move", String("nested/copy.bin"), String("nested/moved.bin")); !moved.Truth() {
		t.Fatalf("move = %s, want true", moved.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "getFileName", String("nested/moved.bin")).String(); got != "moved.bin" {
		t.Fatalf("getFileName = %q", got)
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "dirname", String("nested/moved.bin")).String(); got != filepath.Join(root, "nested") {
		t.Fatalf("dirname = %q", got)
	}

	listed := mustCallIOBuiltin(t, runtime, functions, "ls", String("nested"))
	array, ok := listed.Array()
	if !ok {
		t.Fatalf("ls = %s, want array", listed.Describe())
	}
	gotPaths := valueStrings(array.Values())
	wantPaths := []string{filepath.Join(root, "nested", "data.bin"), filepath.Join(root, "nested", "moved.bin")}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("ls = %q, want %q", gotPaths, wantPaths)
	}

	if deleted := mustCallIOBuiltin(t, runtime, functions, "deleteFile", String("nested/data.bin")); !deleted.Truth() {
		t.Fatalf("deleteFile = %s, want true", deleted.Describe())
	}
	mustCallIOBuiltin(t, runtime, functions, "deleteFile", String("nested/moved.bin"))
	if _, err := os.Stat(filepath.Join(root, "nested", "data.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file stat error = %v", err)
	}

	if _, err := callIOBuiltin(context.Background(), runtime, functions, "copyFile", String("missing"), String("destination")); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("copy missing error = %v", err)
	}
	fileHandle := mustCallIOBuiltin(t, runtime, functions, "openf", String(">not-a-directory"))
	mustCallIOBuiltin(t, runtime, functions, "closef", fileHandle)
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "chdir", String("not-a-directory")); err != nil {
		t.Fatalf("chdir file: %v", err)
	}
	if got, want := mustCallIOBuiltin(t, runtime, functions, "cwd").String(), filepath.Join(root, "not-a-directory"); got != want {
		t.Fatalf("cwd after chdir(file) = %q, want %q", got, want)
	}
	mustCallIOBuiltin(t, runtime, functions, "chdir", String(root))
}

func TestBackticksReturnOutputLinesAndReportExitStatus(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()

	shellSource := "printf 'shell-one\\nshell-two\\n'"
	if runtimePackageGOOS() == "windows" {
		shellSource = "echo shell-one&&echo shell-two"
	}
	backticks := mustCallIOBuiltin(t, runtime, functions, "__EXEC__", String(shellSource))
	if got := arrayStrings(t, backticks); !reflect.DeepEqual(got, []string{"shell-one", "shell-two"}) {
		t.Fatalf("__EXEC__ output = %q", got)
	}
}

func TestOutputLineCountMatchesMaterialization(t *testing.T) {
	t.Parallel()

	for _, output := range [][]byte{
		nil,
		[]byte("plain"),
		[]byte("one\n"),
		[]byte("one\r"),
		[]byte("one\r\ntwo\rthree\n"),
		[]byte("\r\n\r\n"),
		{'a', 0xff, '\r', 'b'},
	} {
		if got, want := outputLineCount(output), len(outputLines(output)); got != want {
			t.Fatalf("outputLineCount(%q) = %d, want %d", output, got, want)
		}
	}
}

func TestSleepHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := callIOBuiltin(ctx, runtime, functions, "sleep", Int(1000)); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleep cancellation error = %v", err)
	}

	started := time.Now()
	mustCallIOBuiltin(t, runtime, functions, "sleep", Int(2))
	if elapsed := time.Since(started); elapsed < time.Millisecond {
		t.Fatalf("sleep returned too soon after %v", elapsed)
	}
	mustCallIOBuiltin(t, runtime, functions, "sleep", Int(-1))
}

func TestIOBuiltinErrorsAreExplicit(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	tests := []struct {
		name string
		args []Value
		want string
	}{
		{name: "allocate", args: []Value{Int(-1)}, want: "capacity must not be negative"},
		{name: "openf", want: "missing file descriptor"},
		{name: "exec", args: []Value{String("")}, want: "command is empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := callIOBuiltin(context.Background(), runtime, functions, test.name, test.args...)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
	// SocketObject.release overloads closef's non-IOObject form. A nonnumeric
	// scalar coerces to requested port zero and releasing a missing listener is
	// a no-op rather than a bad-handle bridge error.
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "closef", String("not a handle")); err != nil {
		t.Fatalf("closef port coercion: %v", err)
	}
}

func TestOpenFUsesJavaFileNotFoundSoftError(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	program, err := CompileString("missing.sl", `debug(34);
try {
    $handle = openf("missing-file");
}
catch $exception {
    return $exception;
}
return "not caught";
`)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	want := "java.io.FileNotFoundException: " + filepath.Join(root, "missing-file") + " (No such file or directory)"
	if got := value.String(); got != want {
		t.Fatalf("caught error = %q, want %q", got, want)
	}
}

func TestOpenFMissingFileCanonicalWarning(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "warn2.sl"))
	if err != nil {
		t.Fatal(err)
	}
	wantBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "warn2.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("warn2.sl", programBytes))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := runtime.Invoke(context.Background(), "chdir", String(root)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
	}
	got := strings.ReplaceAll(filepath.ToSlash(output.String()), filepath.ToSlash(root), "<cwd>")
	want := strings.ReplaceAll(string(wantBytes), "/root/sleep/tests", "<cwd>")
	if got != want {
		t.Fatalf("normalized output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

const ioHelperMarker = "opfor-io-helper"

func TestIOProcessHelper(t *testing.T) {
	marker := -1
	for index, argument := range os.Args {
		if argument == ioHelperMarker {
			marker = index
			break
		}
	}
	if marker == -1 {
		return
	}
	if marker+1 >= len(os.Args) {
		os.Exit(9)
	}
	switch os.Args[marker+1] {
	case "success":
		workingDirectory, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(8)
		}
		fmt.Fprintln(os.Stdout, "first")
		fmt.Fprintln(os.Stdout, "second")
		fmt.Fprintln(os.Stdout, workingDirectory)
		os.Exit(0)
	case "failure":
		fmt.Fprintln(os.Stdout, "partial")
		fmt.Fprintln(os.Stderr, "failure detail")
		os.Exit(7)
	default:
		os.Exit(9)
	}
}

type ioTestCallable func(context.Context, ...Value) (Value, error)

func (function ioTestCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return function(ctx, values...)
}

// Test callbacks used through a script-owned BasicIO invocation model an
// actual SleepClosure. Production importer callbacks deliberately cannot
// implement this private marker.
func (ioTestCallable) isSleepSequenceClosure() {}

func callIOBuiltin(ctx context.Context, runtime *Runtime, functions map[string]NativeFunc, name string, values ...Value) (Value, error) {
	function := functions[name]
	if function == nil {
		return Null(), fmt.Errorf("missing I/O builtin %q", name)
	}
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return function(ctx, Invocation{Runtime: runtime, Name: name, Arguments: arguments})
}

func mustCallIOBuiltin(t *testing.T, runtime *Runtime, functions map[string]NativeFunc, name string, values ...Value) Value {
	t.Helper()
	value, err := callIOBuiltin(context.Background(), runtime, functions, name, values...)
	if err != nil {
		t.Fatalf("&%s: %v", name, err)
	}
	return value
}

func callbackData(calls [][]Value) []string {
	data := make([]string, len(calls))
	for index, call := range calls {
		if len(call) > 1 {
			data[index] = call[1].String()
		}
	}
	return data
}

func valueStrings(values []Value) []string {
	strings := make([]string, len(values))
	for index, value := range values {
		strings[index] = value.String()
	}
	return strings
}

func arrayStrings(t *testing.T, value Value) []string {
	t.Helper()
	array, ok := value.Array()
	if !ok {
		t.Fatalf("value = %s, want array", value.Describe())
	}
	return valueStrings(array.Values())
}

func runtimePackageGOOS() string {
	return runtime.GOOS
}
