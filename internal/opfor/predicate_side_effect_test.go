package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const unaryPredicateSideEffectProbe = `$i = 0;
if (-isnumber $i++) { println("yes"); }
println("i=" . $i);
`

const unaryPredicateSideEffectOutput = "yes\ni=1\n"

func TestUnaryPredicateEvaluatesOperandOnce(t *testing.T) {
	if got := runUnaryPredicateSideEffectProbe(t); !bytes.Equal(got, []byte(unaryPredicateSideEffectOutput)) {
		t.Fatalf("output mismatch: got %q, want %q", got, unaryPredicateSideEffectOutput)
	}
}

func TestUnaryPredicateSideEffectOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, "unary-predicate-side-effect.sl")
	if err := os.WriteFile(path, []byte(unaryPredicateSideEffectProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep unary-predicate probe: %v\n%s", err, want)
	}
	if got := runUnaryPredicateSideEffectProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep unary-predicate output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runUnaryPredicateSideEffectProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "unary-predicate-side-effect.sl", unaryPredicateSideEffectProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
