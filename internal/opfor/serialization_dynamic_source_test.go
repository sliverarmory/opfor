package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sliverarmory/opfor/internal/javaser"
)

const dynamicSourceSerializationProgram = `
inline dynamic_named {
   yield "inline-first";
   return "inline-tail";
}

$inline_value = {
   yield "special-first";
   return "special-tail";
};

$eval = {
   $value = eval('yield "eval-first"; return "eval-tail";');
   return "eval-outer:" . $value;
};
[$eval];

$two = {
   eval('yield "a1"; println("A2");');
   eval('yield "b1"; println("B2");');
   return "two-outer";
};
[$two];

$outer = {
   eval('yield "inner-first"; println("INNER-TAIL");');
   yield "outer-first";
   return "outer-tail";
};
[$outer];

$eval_inline = {
   eval('dynamic_named(); return "dynamic-tail";');
   return "eval-inline-outer";
};
[$eval_inline];

$expr_inline = {
   $value = expr('dynamic_named()');
   return "expr-inline-outer:" . $value;
};
[$expr_inline];

inline dynamic_empty {
   yield "empty-first";
   println("EMPTY-TAIL");
}
$expr_empty = {
   $value = expr('dynamic_empty()');
   return "expr-empty-outer:" . $value;
};
[$expr_empty];

$expr_special = {
   $value = expr('inline(\$inline_value)');
   return "expr-special-outer:" . $value;
};
[$expr_special];

$included = {
   include("virtual-dynamic-include.sl");
   return "include-outer";
};
[$included];

$dynamic_foreach = {
   eval('foreach $item (@("a", "b")) { yield $item; }');
   return "foreach-outer";
};
[$dynamic_foreach];
`

func TestSleepClosureDynamicSourceContinuationSerialization(t *testing.T) {
	program, err := CompileString("dynamic-serialization.sl", dynamicSourceSerializationProgram)
	if err != nil {
		t.Fatal(err)
	}
	producerResolver := SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
		if request.Name != "virtual-dynamic-include.sl" {
			return Source{}, fmt.Errorf("unexpected source %q", request.Name)
		}
		return NewSource("virtual-dynamic-include.sl", []byte(`yield "include-first"; return "include-tail";`)), nil
	})
	producerRuntime, err := New(WithSourceResolver(producerResolver))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := producerRuntime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producerRuntime.Close(context.Background()) })

	var consumerOutput bytes.Buffer
	var consumerResolveCalls atomic.Int32
	consumerRuntime, err := New(
		WithStdout(&consumerOutput),
		WithStderr(&consumerOutput),
		WithSourceResolver(SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
			consumerResolveCalls.Add(1)
			return Source{}, fmt.Errorf("deserialized continuation unexpectedly resolved %q", request.Name)
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	ownerProgram, err := CompileString("dynamic-consumer.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := consumerRuntime.Load(context.Background(), ownerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumerRuntime.Close(context.Background()) })

	tests := []struct {
		variable        string
		wantResult      string
		wantNull        bool
		wantOutput      string
		wantContexts    int
		wantTail        int
		wantCodeContext bool
		wantReturnTail  bool
		wantForeach     bool
	}{
		{variable: "$eval", wantResult: "eval-tail", wantContexts: 1},
		{variable: "$two", wantNull: true, wantOutput: "A2\nB2\n", wantContexts: 2, wantTail: 1},
		{variable: "$outer", wantResult: "outer-tail", wantOutput: "INNER-TAIL\n", wantContexts: 2, wantTail: 1, wantCodeContext: true},
		{variable: "$eval_inline", wantResult: "inline-tail", wantContexts: 2},
		{variable: "$expr_inline", wantResult: "inline-tail", wantContexts: 2, wantTail: 1, wantReturnTail: true},
		{variable: "$expr_empty", wantNull: true, wantOutput: "EMPTY-TAIL\n", wantContexts: 2, wantTail: 1, wantReturnTail: true},
		{variable: "$expr_special", wantResult: "special-tail", wantContexts: 1},
		{variable: "$included", wantResult: "include-tail", wantContexts: 1},
		{
			variable:     "$dynamic_foreach",
			wantNull:     true,
			wantOutput:   "Warning: null value error at eval:0\nWarning: internal error - class java.util.EmptyStackException at eval:0\n",
			wantContexts: 2,
			wantForeach:  true,
		},
	}
	for _, test := range tests {
		t.Run(strings.TrimPrefix(test.variable, "$"), func(t *testing.T) {
			stream, err := encodeSleepScalarStream(producer.Get(test.variable))
			if err != nil {
				t.Fatal(err)
			}
			assertDynamicSourceSerializedContextGraph(t, stream, test.wantContexts, test.wantCodeContext)

			decoded, consumed, err := decodeSleepScalarStreamForScript(bytes.NewReader(stream), owner)
			if err != nil {
				t.Fatal(err)
			}
			if consumed != int64(len(stream)) {
				t.Fatalf("consumed = %d, want %d", consumed, len(stream))
			}
			callable, ok := decoded.Function()
			if !ok {
				t.Fatalf("decoded kind = %s, want function", decoded.Kind())
			}
			closure := callable.(*scriptClosure)
			if len(closure.suspended) != 1 {
				t.Fatalf("suspended groups = %d, want 1", len(closure.suspended))
			}
			head := closure.suspended[0]
			if head.dynamicSource == nil {
				t.Fatal("decoded head has no dynamic-source identity")
			}
			if got := len(head.continuationTail); got != test.wantTail {
				t.Fatalf("continuation tail = %d, want %d", got, test.wantTail)
			}
			if test.wantReturnTail && (len(head.continuationTail) == 0 || !head.continuationTail[0].serializedReturn) {
				t.Fatal("expr inline context has no serialized Return tail")
			}
			if test.wantForeach && head.serializedForeach == nil {
				t.Fatal("dynamic foreach did not retain the metadata-omitted cursor marker")
			}

			consumerOutput.Reset()
			result, err := callable.Invoke(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if test.wantNull {
				if !result.IsNull() {
					t.Fatalf("result = %s, want null", result.Describe())
				}
			} else if got := result.String(); got != test.wantResult {
				t.Fatalf("result = %q, want %q", got, test.wantResult)
			}
			if got := consumerOutput.String(); got != test.wantOutput {
				t.Fatalf("output = %q, want %q", got, test.wantOutput)
			}
			if len(closure.suspended) != 0 {
				t.Fatalf("decoded closure retains %d context groups after resume", len(closure.suspended))
			}
		})
	}
	if got := consumerResolveCalls.Load(); got != 0 {
		t.Fatalf("deserialized include resolver calls = %d, want 0", got)
	}
}

func assertDynamicSourceSerializedContextGraph(t *testing.T, stream []byte, wantContexts int, wantCodeContext bool) {
	t.Helper()
	root, consumed, err := decodeSleepJavaStream(bytes.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if consumed != int64(len(stream)) {
		t.Fatalf("Java graph consumed = %d, want %d", consumed, len(stream))
	}
	closure := sleepClosureObjectFromScalarGraph(t, root)
	data, ok := closure.DataFor(sleepClosureDescriptor.Name)
	if !ok || len(data.Annotation) != 4 {
		t.Fatal("serialized SleepClosure custom data is malformed")
	}
	code := data.Annotation[1].(*javaser.Object)
	toplevels, err := sleepStackElements(data.Annotation[2].(*javaser.Object))
	if err != nil {
		t.Fatal(err)
	}
	if len(toplevels) != 1 {
		t.Fatalf("serialized toplevel groups = %d, want 1", len(toplevels))
	}
	entries, err := sleepStackElements(toplevels[0].(*javaser.Object))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(entries) - 1; got != wantContexts {
		t.Fatalf("serialized Context entries = %d, want %d", got, wantContexts)
	}
	hasCode := false
	for _, entry := range entries[:len(entries)-1] {
		decoded, err := decodeSleepContext(entry.(*javaser.Object))
		if err != nil {
			t.Fatal(err)
		}
		hasCode = hasCode || decoded.block == code
	}
	if hasCode != wantCodeContext {
		t.Fatalf("serialized Context references SleepClosure.code = %v, want %v", hasCode, wantCodeContext)
	}
}

func TestOfficialSleepDynamicSourceContinuationInterop(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for dynamic-source continuation interoperability")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	const officialSHA256 = "0ddde5e9e8d8d8d334d071b1f887c379f5d0be9b190566f05365997b3e375ff1"
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	temporary := t.TempDir()
	names := []string{"eval", "two", "outer", "eval-inline", "expr-inline", "include", "foreach"}
	officialPaths := dynamicSourceStreamPaths(temporary, "official", names)
	producerArgs := []string{"-jar", jar, filepath.Join("testdata", "serialization", "produce_dynamic_sources.sl")}
	producerArgs = append(producerArgs, officialPaths...)
	producer := osexec.Command(java, producerArgs...)
	if output, err := producer.CombinedOutput(); err != nil {
		t.Fatalf("official Sleep dynamic-source producer: %v\n%s", err, output)
	} else if len(output) != 0 {
		t.Fatalf("official Sleep dynamic-source producer output = %q, want empty", output)
	}

	var output bytes.Buffer
	consumerRuntime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	ownerProgram, err := CompileString("official-dynamic-owner.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := consumerRuntime.Load(context.Background(), ownerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumerRuntime.Close(context.Background()) })
	relayPaths := dynamicSourceStreamPaths(temporary, "relay", names)
	expectations := []struct {
		result string
		null   bool
		output string
	}{
		{result: "eval-tail"},
		{null: true, output: "A2\nB2\n"},
		{result: "outer-tail", output: "INNER-TAIL\n"},
		{result: "inline-tail"},
		{result: "inline-tail"},
		{result: "include-tail"},
		{null: true, output: "Warning: null value error at eval:0\nWarning: internal error - class java.util.EmptyStackException at eval:0\n"},
	}
	for index, path := range officialPaths {
		stream, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		value, consumed, err := decodeSleepScalarStreamForScript(bytes.NewReader(stream), owner)
		if err != nil {
			t.Fatalf("decode official %s: %v", names[index], err)
		}
		if consumed != int64(len(stream)) {
			t.Fatalf("official %s consumed = %d, want %d", names[index], consumed, len(stream))
		}
		relay, err := encodeSleepScalarStream(value)
		if err != nil {
			t.Fatalf("re-encode official %s: %v", names[index], err)
		}
		if err := os.WriteFile(relayPaths[index], relay, 0o600); err != nil {
			t.Fatal(err)
		}

		callable, ok := value.Function()
		if !ok {
			t.Fatalf("official %s decoded kind = %s, want function", names[index], value.Kind())
		}
		output.Reset()
		result, err := callable.Invoke(context.Background())
		if err != nil {
			t.Fatalf("invoke official %s: %v", names[index], err)
		}
		want := expectations[index]
		if want.null {
			if !result.IsNull() {
				t.Fatalf("official %s result = %s, want null", names[index], result.Describe())
			}
		} else if got := result.String(); got != want.result {
			t.Fatalf("official %s result = %q, want %q", names[index], got, want.result)
		}
		if got := output.String(); got != want.output {
			t.Fatalf("official %s output = %q, want %q", names[index], got, want.output)
		}
	}

	const officialConsumerOutput = "eval=eval-tail\nA2\nB2\ntwo=\nINNER-TAIL\nouter=outer-tail\neval-inline=inline-tail\nexpr-inline=inline-tail\ninclude=include-tail\nWarning: null value error at eval:0\nWarning: internal error - class java.util.EmptyStackException at eval:0\nforeach=\n"
	if got := runOfficialDynamicSourceConsumer(t, java, jar, relayPaths); got != officialConsumerOutput {
		t.Fatalf("official Sleep relay-consumer output mismatch\nwant:\n%sgot:\n%s", officialConsumerOutput, got)
	}

	opforPaths := dynamicSourceStreamPaths(temporary, "opfor", names)
	producerSource, err := os.ReadFile(filepath.Join("testdata", "serialization", "produce_dynamic_sources.sl"))
	if err != nil {
		t.Fatal(err)
	}
	producerProgram, err := Compile(NewSource("testdata/serialization/produce_dynamic_sources.sl", producerSource))
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
	if _, err := producerRuntime.Load(context.Background(), producerProgram, arguments...); err != nil {
		_ = producerRuntime.Close(context.Background())
		t.Fatal(err)
	}
	if err := producerRuntime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	const opforConsumerOutput = "eval=eval-tail\nA2\nB2\ntwo=\nINNER-TAIL\nouter=outer-tail\neval-inline=inline-tail\nexpr-inline=inline-tail\ninclude=include-tail\nWarning: null value error at eval:0\nWarning: internal error - class java.util.EmptyStackException at eval:0\nforeach=\n"
	if got := runOfficialDynamicSourceConsumer(t, java, jar, opforPaths); got != opforConsumerOutput {
		t.Fatalf("official Sleep OPFOR-consumer output mismatch\nwant:\n%sgot:\n%s", opforConsumerOutput, got)
	}
}

func dynamicSourceStreamPaths(directory, prefix string, names []string) []string {
	paths := make([]string, len(names))
	for index, name := range names {
		paths[index] = filepath.Join(directory, prefix+"-"+name+".ser")
	}
	return paths
}

func runOfficialDynamicSourceConsumer(t *testing.T, java, jar string, paths []string) string {
	t.Helper()
	arguments := []string{"-jar", jar, filepath.Join("testdata", "serialization", "consume_dynamic_sources.sl")}
	arguments = append(arguments, paths...)
	command := osexec.Command(java, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep dynamic-source consumer: %v\n%s", err, output)
	}
	return string(output)
}
