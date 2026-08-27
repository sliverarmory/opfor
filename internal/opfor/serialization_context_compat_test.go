package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

var serializedControlContextNames = []string{
	"try",
	"nested-foreach",
	"inline-foreach",
	"foreach-tail",
	"nested-try",
	"try-foreach",
}

var serializedControlContextVariables = []string{
	"$saved_try",
	"$nested_foreach",
	"$inline_foreach",
	"$foreach_body_tail",
	"$nested_try",
	"$try_foreach",
}

func TestOfficialSleepSerializedControlContextsInterop(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	temporary := t.TempDir()
	officialPaths := serializedControlContextPaths(temporary, "official")
	producerArgs := []string{"-jar", jar, filepath.Join("testdata", "serialization", "produce_context_shapes.sl")}
	producerArgs = append(producerArgs, officialPaths...)
	if output, err := officialSleepJavaCommand(java, producerArgs...).CombinedOutput(); err != nil {
		t.Fatalf("official Sleep context-shape producer: %v\n%s", err, output)
	} else if len(output) != 0 {
		t.Fatalf("official Sleep context-shape producer output = %q, want empty", output)
	}

	const referenceOutput = "try=caught:boom\n" +
		"Warning: null value error at produce_context_shapes.sl:32\n" +
		"Warning: internal error - class java.util.EmptyStackException at produce_context_shapes.sl:32\n" +
		"Warning: null value error at produce_context_shapes.sl:30\n" +
		"Warning: internal error - class java.util.EmptyStackException at produce_context_shapes.sl:30\n" +
		"nested-foreach=\n" +
		"inline-foreach=a-inline-tail\n" +
		"foreach-tail=body-tail:\n" +
		"nested-try=nested-caught:outer:inner\n" +
		"try-foreach=foreach-caught:item:\n"
	if got := runOfficialSerializedControlContextConsumer(t, java, jar, officialPaths); got != referenceOutput {
		t.Fatalf("official Sleep context-shape baseline mismatch\nwant:\n%sgot:\n%s", referenceOutput, got)
	}

	var output bytes.Buffer
	consumerRuntime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	ownerProgram, err := CompileString("serialized-context-owner.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := consumerRuntime.Load(context.Background(), ownerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumerRuntime.Close(context.Background()) })
	relayPaths := serializedControlContextPaths(temporary, "relay")
	expectations := []struct {
		result string
		output string
	}{
		{result: "caught:boom"},
		{output: "Warning: null value error at produce_context_shapes.sl:32\n" +
			"Warning: internal error - class java.util.EmptyStackException at produce_context_shapes.sl:32\n" +
			"Warning: null value error at produce_context_shapes.sl:30\n" +
			"Warning: internal error - class java.util.EmptyStackException at produce_context_shapes.sl:30\n"},
		{result: "a-inline-tail"},
		{result: "body-tail:"},
		{result: "nested-caught:outer:inner"},
		{result: "foreach-caught:item:"},
	}
	for index, path := range officialPaths {
		stream, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		value, consumed, err := decodeSleepScalarStreamForScript(bytes.NewReader(stream), owner)
		if err != nil {
			t.Fatalf("decode official %s: %v", serializedControlContextNames[index], err)
		}
		if consumed != int64(len(stream)) {
			t.Fatalf("official %s consumed = %d, want %d", serializedControlContextNames[index], consumed, len(stream))
		}
		relay, err := encodeSleepScalarStream(value)
		if err != nil {
			t.Fatalf("re-encode official %s: %v", serializedControlContextNames[index], err)
		}
		if err := os.WriteFile(relayPaths[index], relay, 0o600); err != nil {
			t.Fatal(err)
		}
		callable, ok := value.Function()
		if !ok {
			t.Fatalf("official %s decoded kind = %s, want function", serializedControlContextNames[index], value.Kind())
		}
		output.Reset()
		result, err := callable.Invoke(context.Background())
		if err != nil {
			t.Fatalf("invoke official %s: %v", serializedControlContextNames[index], err)
		}
		if got := result.String(); got != expectations[index].result {
			t.Fatalf("official %s result = %q, want %q", serializedControlContextNames[index], got, expectations[index].result)
		}
		if got := output.String(); got != expectations[index].output {
			t.Fatalf("official %s output = %q, want %q", serializedControlContextNames[index], got, expectations[index].output)
		}
	}
	if got := runOfficialSerializedControlContextConsumer(t, java, jar, relayPaths); got != referenceOutput {
		t.Fatalf("official Sleep relay-consumer mismatch\nwant:\n%sgot:\n%s", referenceOutput, got)
	}

	opforPaths := serializedControlContextPaths(temporary, "opfor")
	producerSource, err := os.ReadFile(filepath.Join("testdata", "serialization", "produce_context_shapes.sl"))
	if err != nil {
		t.Fatal(err)
	}
	producerProgram, err := Compile(NewSource("testdata/serialization/produce_context_shapes.sl", producerSource))
	if err != nil {
		t.Fatal(err)
	}
	producerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	arguments := make([]Value, len(opforPaths))
	for index, path := range opforPaths {
		arguments[index] = String(path)
	}
	producerScript, err := producerRuntime.Load(context.Background(), producerProgram, arguments...)
	if err != nil {
		_ = producerRuntime.Close(context.Background())
		t.Fatal(err)
	}
	for index, path := range opforPaths {
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			_, encodeErr := encodeSleepScalarStream(producerScript.Get(serializedControlContextVariables[index]))
			t.Fatalf("OPFOR producer %s stream is empty or missing: stat=%v encode=%v", serializedControlContextNames[index], err, encodeErr)
		}
	}
	if err := producerRuntime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := runOfficialSerializedControlContextConsumer(t, java, jar, opforPaths); got != referenceOutput {
		t.Fatalf("official Sleep OPFOR-context consumer mismatch\nwant:\n%sgot:\n%s", referenceOutput, got)
	}
}

func serializedControlContextPaths(directory, prefix string) []string {
	paths := make([]string, len(serializedControlContextNames))
	for index, name := range serializedControlContextNames {
		paths[index] = filepath.Join(directory, prefix+"-"+name+".ser")
	}
	return paths
}

func runOfficialSerializedControlContextConsumer(t *testing.T, java, jar string, paths []string) string {
	t.Helper()
	arguments := []string{"-jar", jar, filepath.Join("testdata", "serialization", "consume_context_shapes.sl")}
	arguments = append(arguments, paths...)
	output, err := officialSleepJavaCommand(java, arguments...).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep context-shape consumer: %v\n%s", err, output)
	}
	return string(output)
}
