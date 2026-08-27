package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const sleepMinMaxNamespaceProbe = `println("min=" . min(3, 1));
println("max=" . max(3, 1));
`

func TestSleepMinMaxRemainHostExtensions(t *testing.T) {
	for _, name := range []string{"min", "max"} {
		if slices.Contains(DefaultFunctionNames(), name) {
			t.Fatalf("DefaultFunctionNames unexpectedly contains non-Sleep function %q", name)
		}
	}

	var calls []string
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls = append(calls, invocation.Name)
		return String("host-" + invocation.Name), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, name := range []string{"min", "max"} {
		value, invokeErr := runtimeInstance.Invoke(context.Background(), name, Int(3), Int(1))
		if invokeErr != nil || value.String() != "host-"+name {
			t.Fatalf("%s Host fallback = (%s, %v), want host-%s", name, value.Describe(), invokeErr, name)
		}
	}
	if !slices.Equal(calls, []string{"min", "max"}) {
		t.Fatalf("Host calls = %q, want [min max]", calls)
	}
}

func TestSleepMinMaxNamespaceOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	path := filepath.Join(t.TempDir(), "min-max-namespace.sl")
	if err := os.WriteFile(path, []byte(sleepMinMaxNamespaceProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep min/max namespace probe: %v\n%s", err, want)
	}
	if !bytes.Contains(want, []byte("Attempted to call non-existent function &min")) ||
		!bytes.Contains(want, []byte("Attempted to call non-existent function &max")) {
		t.Fatalf("official Sleep unexpectedly resolved min/max:\n%s", want)
	}

	var got bytes.Buffer
	runtimeInstance, err := New(WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString(path, sleepMinMaxNamespaceProbe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("min/max namespace mismatch\nofficial:\n%s\nopfor:\n%s", want, got.Bytes())
	}
}
