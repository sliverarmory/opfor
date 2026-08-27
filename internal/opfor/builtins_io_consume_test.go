package opfor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestSleepBasicIOConsumeSkipSourceContract covers the contracts established
// by Cobalt-Strike/sleep@60ac3ff9 in BasicIO.java lines 63, 88, 106, and
// 1357-1426, plus IOObject.java lines 335-339. In particular, binary EOF does
// not close the input pipeline and therefore does not make -eof true.
func TestSleepBasicIOConsumeSkipSourceContract(t *testing.T) {
	t.Parallel()

	runtime, err := New(WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	handle := mustCallIOBuiltin(t, runtime, functions, "allocate")
	assertIOEOF(t, runtime, functions, handle, true)
	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, String("abcdef"))
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	assertIOEOF(t, runtime, functions, handle, false)

	mustCallIOBuiltin(t, runtime, functions, "mark", handle, Int(32))
	if got := mustCallIOBuiltin(t, runtime, functions, "consume", handle, Int(2), Int(1)); got.Kind() != KindInt || got.Int64() != 2 {
		t.Fatalf("consume = %s, want integer 2", got.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(1)).String(); got != "c" {
		t.Fatalf("byte after consume = %q, want c", got)
	}
	mustCallIOBuiltin(t, runtime, functions, "reset", handle)
	if got := mustCallIOBuiltin(t, runtime, functions, "skip", handle); got.Kind() != KindInt || got.Int64() != 1 {
		t.Fatalf("default skip = %s, want integer 1", got.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(2)).String(); got != "bc" {
		t.Fatalf("bytes after reset and skip = %q, want bc", got)
	}
	for _, count := range []int32{0, -1, -99} {
		if got := mustCallIOBuiltin(t, runtime, functions, "consume", handle, Int(count)); !got.IsNull() {
			t.Fatalf("consume count %d = %s, want null", count, got.Describe())
		}
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "skip", handle, Int(99), Int(2)); got.Kind() != KindInt || got.Int64() != 3 {
		t.Fatalf("partial skip at EOF = %s, want integer 3", got.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "consume", handle); !got.IsNull() {
		t.Fatalf("consume at EOF = %s, want null", got.Describe())
	}
	assertIOEOF(t, runtime, functions, handle, false)
	if got := mustCallIOBuiltin(t, runtime, functions, "readln", handle); !got.IsNull() {
		t.Fatalf("readln at EOF = %s, want null", got.Describe())
	}
	assertIOEOF(t, runtime, functions, handle, true)
	if got := mustCallIOBuiltin(t, runtime, functions, "skip", handle, Int(1), Int(-1)); !got.IsNull() {
		t.Fatalf("skip on closed input = %s, want null", got.Describe())
	}

	wide := readableMemoryHandle(t, runtime, functions, "xy")
	if got := mustCallIOBuiltin(t, runtime, functions, "consume", wide, Long(1<<32+1)); got.Int64() != 1 {
		t.Fatalf("32-bit count conversion = %s, want integer 1", got.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", wide, Int(-1)).String(); got != "y" {
		t.Fatalf("tail after 32-bit count conversion = %q, want y", got)
	}

	negativeBuffer := readableMemoryHandle(t, runtime, functions, "z")
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "consume", negativeBuffer, Int(1), Int(-1)); err == nil || !strings.Contains(err.Error(), "-1") {
		t.Fatalf("negative consume buffer error = %v, want NegativeArraySize-compatible detail", err)
	}
	assertIOEOF(t, runtime, functions, negativeBuffer, false)
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", negativeBuffer, Int(1)).String(); got != "z" {
		t.Fatalf("negative buffer advanced or closed input: got %q, want z", got)
	}

	if _, err := callIOBuiltin(context.Background(), runtime, functions, "consume", String("not a handle"), Int(1)); err == nil || !strings.Contains(err.Error(), "expected I/O handle") {
		t.Fatalf("consume invalid handle error = %v", err)
	}
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "-eof", String("not a handle")); err == nil || !strings.Contains(err.Error(), "expected I/O handle") {
		t.Fatalf("-eof invalid handle error = %v", err)
	}
}

func TestSleepBasicIOConsumeUsesConsoleArgumentConvention(t *testing.T) {
	t.Parallel()

	runtime, err := New(WithStdin(strings.NewReader("abc")), WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	if got := mustCallIOBuiltin(t, runtime, functions, "consume", Int(2)); got.Kind() != KindInt || got.Int64() != 2 {
		t.Fatalf("console consume = %s, want integer 2", got.Describe())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readb").String(); got != "c" {
		t.Fatalf("console tail = %q, want c", got)
	}
}

type consumeFaultReader struct {
	delivered bool
}

func (reader *consumeFaultReader) Read(destination []byte) (int, error) {
	if !reader.delivered {
		reader.delivered = true
		return copy(destination, "ab"), nil
	}
	return 0, errors.New("injected consume failure")
}

func TestSleepBasicIOConsumePartialReadSoftError(t *testing.T) {
	t.Parallel()

	handle := newIOHandle("consume-fault", &consumeFaultReader{}, nil, false, false, false)
	var output bytes.Buffer
	runtime, err := New(
		WithStdout(&output),
		WithStderr(&output),
		WithFunction("consumeFault", func(context.Context, Invocation) (Value, error) {
			return ObjectValue(handle), nil
		}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program := "debug(2);\n" +
		"$h = consumeFault();\n" +
		"println(consume($h, 5, 2));\n" +
		"if (-eof $h) { println('closed'); } else { println('open'); }\n" +
		"checkError($problem);\n" +
		"println($problem);\n" +
		"println('continued');\n"
	if _, err := runtime.Eval(context.Background(), "consume-soft-error.sl", program); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	want := "Warning: checkError(): java.io.IOException: injected consume failure at consume-soft-error.sl:3\n" +
		"2\nclosed\njava.io.IOException: injected consume failure\ncontinued\n"
	if got := output.String(); got != want {
		t.Fatalf("soft-error output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestSleepBasicIOConsumeImporterErrorsKeepBoundaryAuthorityOnDirectInvoke(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			runtimeInstance, err := New(WithStdin(rawImporterErrorReader{err: boundaryErr}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), "consume", Int(1))
			if !errors.Is(err, boundaryErr) || !result.IsNull() {
				t.Fatalf("consume = (%s, %v), want null/authoritative %v", result.Describe(), err, boundaryErr)
			}
		})
	}
}

func TestSleepBasicIOConsumeImporterErrorsRemainScriptSoftErrors(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			var output bytes.Buffer
			runtimeInstance, err := New(
				WithStdin(rawImporterErrorReader{err: boundaryErr}),
				WithStdout(&output),
				WithStderr(&output),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			_, err = runtimeInstance.Eval(context.Background(), "consume-importer-soft-error.sl", `
debug(2);
consume(1);
checkError($problem);
println($problem);
`)
			if err != nil {
				t.Fatalf("Eval: %v\n%s", err, output.String())
			}
			if got := output.String(); !strings.Contains(got, "java.io.IOException: "+boundaryErr.Error()) {
				t.Fatalf("soft-error output = %q, want wrapped importer error %q", got, boundaryErr.Error())
			}
		})
	}
}

func TestSleepBasicIOConsumeNegativeBufferIsBridgeWarning(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	program := "sub fail {\n" +
		"  consume($1, 1, -1);\n" +
		"  println('unreachable');\n" +
		"}\n" +
		"$h = allocate(); writeb($h, 'z'); closef($h);\n" +
		"fail($h);\n" +
		"println(readb($h, -1));\n" +
		"println('continued');\n"
	if _, err := runtime.Eval(context.Background(), "consume-negative-buffer.sl", program); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	want := "Warning: -1 at consume-negative-buffer.sl:2\nz\ncontinued\n"
	if got := output.String(); got != want {
		t.Fatalf("bridge-warning output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestSleepBasicIOConsumeSkipExactOutput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "input.bin")
	if err := os.WriteFile(path, []byte("012345"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := runPureGoBasicIOConsumeProbe(t, path)
	if want := []byte(sleepBasicIOConsumeProbeOutput); !bytes.Equal(got, want) {
		t.Fatalf("probe output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

// TestSleepBasicIOConsumeSkipOfficialJARDifferential compares deterministic
// memory and file handles with the separately supplied, hash-pinned official
// Sleep 2.1 JAR. The licensed JAR is never required by ordinary pure-Go CI.
func TestSleepBasicIOConsumeSkipOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	path := filepath.Join(t.TempDir(), "input.bin")
	if err := os.WriteFile(path, []byte("012345"), 0o600); err != nil {
		t.Fatal(err)
	}
	goOutput := runPureGoBasicIOConsumeProbe(t, path)
	command := officialSleepJavaCommand(java, "-jar", jar, "-e", sleepBasicIOConsumeProbeSource(path))
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep BasicIO probe: %v\n%s", err, javaOutput)
	}
	if !bytes.Equal(goOutput, javaOutput) {
		t.Fatalf("official Sleep BasicIO output mismatch\nwant:\n%sgot:\n%s", javaOutput, goOutput)
	}
}

func readableMemoryHandle(t *testing.T, runtime *Runtime, functions map[string]NativeFunc, contents string) Value {
	t.Helper()
	handle := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, BinaryString([]byte(contents)))
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	return handle
}

func assertIOEOF(t *testing.T, runtime *Runtime, functions map[string]NativeFunc, handle Value, want bool) {
	t.Helper()
	if got := mustCallIOBuiltin(t, runtime, functions, "-eof", handle); got.Truth() != want {
		t.Fatalf("-eof = %s, want %t", got.Describe(), want)
	}
}

func runPureGoBasicIOConsumeProbe(t *testing.T, path string) []byte {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Eval(context.Background(), "basic-io-consume-probe.sl", sleepBasicIOConsumeProbeSource(path)); err != nil {
		t.Fatalf("pure-Go BasicIO probe: %v\n%s", err, output.String())
	}
	return output.Bytes()
}

func sleepBasicIOConsumeProbeSource(path string) string {
	quotedPath := strconv.Quote(filepath.ToSlash(path))
	return "sub show_eof { if (-eof $1) { println('eof:1'); } else { println('eof:0'); } }\n" +
		"$memory = allocate();\n" +
		"show_eof($memory);\n" +
		"writeb($memory, 'abcdef'); closef($memory);\n" +
		"show_eof($memory);\n" +
		"mark($memory, 32);\n" +
		"println('consume:' . consume($memory, 2, 1));\n" +
		"println('next:' . readb($memory, 1));\n" +
		"reset($memory);\n" +
		"println('skip:' . skip($memory));\n" +
		"println('replay:' . readb($memory, 2));\n" +
		"println('zero:' . consume($memory, 0));\n" +
		"println('negative:' . consume($memory, -2));\n" +
		"println('tail:' . skip($memory, 99, 2));\n" +
		"println('binary-eof:' . readb($memory, 1));\n" +
		"show_eof($memory);\n" +
		"println('line-eof:' . readln($memory));\n" +
		"show_eof($memory);\n" +
		"println('closed:' . consume($memory));\n" +
		"$wide = allocate(); writeb($wide, 'xy'); closef($wide);\n" +
		"println('wide:' . consume($wide, 4294967297L));\n" +
		"println('wide-tail:' . readb($wide, -1));\n" +
		"$file = openf(" + quotedPath + ");\n" +
		"show_eof($file);\n" +
		"println('file-skip:' . skip($file, 2, 1));\n" +
		"println('file-bytes:' . readb($file, 2));\n" +
		"println('file-tail:' . consume($file, 99, 2));\n" +
		"show_eof($file);\n" +
		"println('file-line-eof:' . readln($file));\n" +
		"show_eof($file);\n"
}

const sleepBasicIOConsumeProbeOutput = "eof:1\n" +
	"eof:0\n" +
	"consume:2\n" +
	"next:c\n" +
	"skip:1\n" +
	"replay:bc\n" +
	"zero:\n" +
	"negative:\n" +
	"tail:3\n" +
	"binary-eof:\n" +
	"eof:0\n" +
	"line-eof:\n" +
	"eof:1\n" +
	"closed:\n" +
	"wide:1\n" +
	"wide-tail:y\n" +
	"eof:0\n" +
	"file-skip:2\n" +
	"file-bytes:23\n" +
	"file-tail:2\n" +
	"eof:0\n" +
	"file-line-eof:\n" +
	"eof:1\n"
