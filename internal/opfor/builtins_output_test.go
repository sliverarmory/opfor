package opfor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type outputErrorWriter struct {
	err   error
	calls int
}

func (writer *outputErrorWriter) Write([]byte) (int, error) {
	writer.calls++
	return 0, writer.err
}

type recordingWriter struct {
	mu     sync.Mutex
	writes [][]byte
}

func (writer *recordingWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	writer.writes = append(writer.writes, append([]byte(nil), data...))
	writer.mu.Unlock()
	return len(data), nil
}

func (writer *recordingWriter) snapshot() [][]byte {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	result := make([][]byte, len(writer.writes))
	for index, data := range writer.writes {
		result[index] = append([]byte(nil), data...)
	}
	return result
}

func TestLineOutputUsesOneAtomicWrite(t *testing.T) {
	var stdout, stderr recordingWriter
	runtime, err := New(WithStdout(&stdout), WithStderr(&stderr))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Invoke(context.Background(), "println", String("one line")); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Invoke(context.Background(), "warn", String("one warning")); err != nil {
		t.Fatal(err)
	}
	if writes := stdout.snapshot(); len(writes) != 1 || string(writes[0]) != "one line\n" {
		t.Fatalf("println writes = %q, want one complete record", writes)
	}
	if writes := stderr.snapshot(); len(writes) != 1 || string(writes[0]) != "Warning: one warning\n" {
		t.Fatalf("warn writes = %q, want one complete record", writes)
	}
}

func TestImporterOutputWriterErrorsBypassNativeWarningTranslation(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "print"},
		{name: "println"},
		{name: "printf"},
		{name: "printAll"},
		{name: "warn"},
	}
	for _, test := range tests {
		test := test
		for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
			boundaryErr := boundaryErr
			t.Run(test.name+"/"+boundaryErr.Error(), func(t *testing.T) {
				writer := &outputErrorWriter{err: boundaryErr}
				runtimeInstance, err := New(WithInitialGlobals(map[string]Value{"output_writer": ObjectValue(writer)}))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

				value := String("text")
				scriptValue := `"text"`
				if test.name == "printAll" {
					value = ArrayValue(NewArray(String("text")))
					scriptValue = `@("text")`
				}
				result, err := runtimeInstance.Invoke(context.Background(), test.name, ObjectValue(writer), value)
				if !errors.Is(err, boundaryErr) || !result.IsNull() {
					t.Fatalf("Invoke = (%s, %v), want null/%v", result.Describe(), err, boundaryErr)
				}
				if test.name == "warn" {
					// BasicUtilities.warn always reports through ScriptInstance's warning
					// stream. Unlike BasicIO output functions, source calls do not accept
					// an explicit I/O target.
					if writer.calls != 1 {
						t.Fatalf("writer calls = %d, want one direct importer call", writer.calls)
					}
					return
				}
				_, err = runtimeInstance.Eval(context.Background(), "output-boundary-error.sl", test.name+`($output_writer, `+scriptValue+`);`)
				if !errors.Is(err, boundaryErr) {
					t.Fatalf("script error = %v, want authoritative %v", err, boundaryErr)
				}
				if writer.calls != 2 {
					t.Fatalf("writer calls = %d, want two", writer.calls)
				}
			})
		}
	}
}

func TestInvalidOutputHandleWarnsAndEndsOnlyCurrentScriptBlock(t *testing.T) {
	program, err := CompileString("output-warning.sl", `
println("before");
eval('println($null, "bad"); println("unreachable eval");');
println("after");
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := "before\n" +
		"Warning: expected I/O handle argument, received: $null at eval:0\n" +
		"after\n"
	if got := output.String(); got != want {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestInvalidOutputHandleFromDirectInvokeRemainsAnError(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = runtime.Invoke(context.Background(), "println", Null(), String("bad"))
	if err == nil || !strings.Contains(err.Error(), "expected I/O handle argument, received: $null") {
		t.Fatalf("Invoke error = %v, want invalid I/O handle error", err)
	}
}

func TestSleepIOErrorGolden(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "ioerr.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "ioerr.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("ioerr.sl", programBytes))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}
