package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sleepBasicIOPrintAllProbeName = "opfor-basicio-printall-probe.sl"

// This probe covers BasicIO.printArray and BridgeUtilities.getIterator at
// Cobalt-Strike/sleep@60ac3ff9dacc3e7b5a6c58be201c5830afbda398. printAll
// accepts an array, a Sleep closure, or a Java iterator; an ordinary scalar
// raises a recoverable bridge warning instead of becoming a one-item sequence.
const sleepBasicIOPrintAllProbe = `debug(2);
sub scalar_case {
  println("scalar-before");
  printAll("solo");
  println("scalar-after");
}
scalar_case();
println("scalar-caller");
printAll(@("array-one", "array-two"));
global('$generator_index');
$generator_index = 0;
sub generator {
  $generator_index++;
  if ($generator_index <= 2) {
    return "generator-$generator_index";
  }
  return $null;
}
printAll(&generator);
sub native_case {
  println("native-before");
  printAll(function("&println"));
  println("native-after");
}
native_case();
println("native-caller");
`

const sleepBasicIOPrintAllProbeOutput = `scalar-before
Warning: expected iterator (@array or &closure)--received: 'solo' at opfor-basicio-printall-probe.sl:4
scalar-caller
array-one
array-two
generator-1
generator-2
native-before
Warning: expected iterator (@array or &closure)--received: &println at opfor-basicio-printall-probe.sl:22
native-caller
`

type printAllNativeCallable func(context.Context, ...Value) (Value, error)

func (callable printAllNativeCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return callable(ctx, values...)
}

func TestSleepBasicIOPrintAllIteratorContract(t *testing.T) {
	got := runSleepBasicIOPrintAllProbe(t)
	if !bytes.Equal(got, []byte(sleepBasicIOPrintAllProbeOutput)) {
		t.Fatalf("BasicIO printAll output mismatch\nwant:\n%sgot:\n%s", sleepBasicIOPrintAllProbeOutput, got)
	}
}

func TestSleepBasicIOPrintAllOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, sleepBasicIOPrintAllProbeName)
	if err := os.WriteFile(path, []byte(sleepBasicIOPrintAllProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep BasicIO printAll probe: %v\n%s", err, want)
	}

	got := runSleepBasicIOPrintAllProbe(t)
	if normalizedGot, normalizedWant := normalizeSleepBasicIOPrintAllProbe(got), normalizeSleepBasicIOPrintAllProbe(want); !bytes.Equal(normalizedGot, normalizedWant) {
		t.Fatalf("official Sleep BasicIO printAll output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestSleepBasicIOPrintAllAcceptsImporterIteratorAndRejectsNativeFunction(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	index := 0
	iterator := IteratorFunc(func(context.Context) (Value, bool, error) {
		values := []Value{String("iterator-one"), String("iterator-two")}
		if index >= len(values) {
			return Null(), false, nil
		}
		value := values[index]
		index++
		return value, true, nil
	})
	if _, err := runtimeInstance.Invoke(context.Background(), "printAll", ObjectValue(iterator)); err != nil {
		t.Fatalf("printAll(importer iterator): %v", err)
	}
	if got := output.String(); got != "iterator-one\niterator-two\n" {
		t.Fatalf("iterator output = %q", got)
	}

	native := FunctionValue(printAllNativeCallable(func(context.Context, ...Value) (Value, error) {
		return String("must-not-run"), nil
	}))
	_, err = runtimeInstance.Invoke(context.Background(), "printAll", native)
	if err == nil || !strings.Contains(err.Error(), "expected iterator (@array or &closure)--received:") {
		t.Fatalf("native function error = %v", err)
	}
	if got := output.String(); got != "iterator-one\niterator-two\n" {
		t.Fatalf("native function was invoked or printed: %q", got)
	}
}

func runSleepBasicIOPrintAllProbe(t *testing.T) []byte {
	t.Helper()

	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepBasicIOPrintAllProbeName, sleepBasicIOPrintAllProbe); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	return output.Bytes()
}

func normalizeSleepBasicIOPrintAllProbe(output []byte) []byte {
	lines := strings.SplitAfter(string(output), "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, "Warning: expected iterator (@array or &closure)--received:") &&
			(strings.Contains(line, "--received: &println at ") || strings.Contains(line, "--received: sleep.bridges.BasicIO$println@")) {
			lines[index] = "Warning: expected iterator (@array or &closure)--received: <native function> at <source>:<line>\n"
		}
	}
	return []byte(strings.Join(lines, ""))
}
