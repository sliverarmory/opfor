package opfor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPortableJavaSoftWarningCanonicalCompatibility(t *testing.T) {
	t.Parallel()

	got, want := runCanonicalOutput(t, "hoeswarning")
	if !bytes.Equal(got, want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestPortableJavaCallTraceCanonicalCompatibility(t *testing.T) {
	t.Parallel()

	got, want := runCanonicalOutput(t, "trace")
	got = []byte(normalizePortableJavaIdentity(string(got)))
	want = []byte(normalizePortableJavaIdentity(string(want)))
	if !bytes.Equal(got, want) {
		t.Fatalf("identity-normalized output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func runCanonicalOutput(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", name+".sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", name+".sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource(name+".sl", programBytes))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
	}
	return output.Bytes(), want
}

func TestPortableReflectionWarningsHonorDebugAndLeaveCheckErrorEmpty(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "reflection-warning-policy.sl", `
debug(0);
$quiet = [Long missingQuietField];
$quiet = [Long valueOf: 1, 2, 3];
$quiet = [new StringTokenizer];
debug(1);
$loud = [Long missingLoudField];
if (checkError($error)) { println("unexpected: $error"); }
println("continued");
return $loud;
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !value.IsNull() {
		t.Fatalf("missing field result = %s, want null", value.Describe())
	}
	if got, want := output.String(), "Warning: no field/method named missingLoudField in class java.lang.Long at reflection-warning-policy.sl:7\ncontinued\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestPortableReflectionWarningsDoNotMaskImporterErrors(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("importer reflection failure")
	var output bytes.Buffer
	runtime, err := New(
		WithStdout(&output),
		WithStderr(&output),
		WithObjectHost(ObjectHostFunc(func(context.Context, ObjectInvocation) (Value, error) {
			return Null(), sentinel
		})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = runtime.Eval(context.Background(), "importer-reflection-error.sl", `return [Long missingField];`)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Eval error = %v, want importer sentinel", err)
	}
	if output.Len() != 0 {
		t.Fatalf("portable warning masked importer error: %q", output.String())
	}
}
