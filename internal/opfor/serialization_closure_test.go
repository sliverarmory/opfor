package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sliverarmory/opfor/internal/javaser"
)

func TestOfficialSleepUnsuspendedClosuresExecuteInOPFOR(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("closure-owner.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	t.Run("print", func(t *testing.T) {
		encoded := readOfficialSleepSerializationVector(t, "closure-print")
		value, consumed, err := decodeSleepScalarStreamForScript(bytes.NewReader(encoded), owner)
		if err != nil {
			t.Fatal(err)
		}
		if consumed != int64(len(encoded)) {
			t.Fatalf("consumed = %d, want %d", consumed, len(encoded))
		}
		callable, ok := value.Function()
		if !ok {
			t.Fatalf("decoded kind = %s, want function", value.Kind())
		}
		if _, err := callable.Invoke(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := output.String(); got != "Hello World!\n" {
			t.Fatalf("output = %q", got)
		}
		output.Reset()
	})

	t.Run("captures cells self and owner", func(t *testing.T) {
		encoded := readOfficialSleepSerializationVector(t, "closure-unsuspended")
		value, consumed, err := decodeSleepScalarStreamForScript(bytes.NewReader(encoded), owner)
		if err != nil {
			t.Fatal(err)
		}
		if consumed != int64(len(encoded)) {
			t.Fatalf("consumed = %d, want %d", consumed, len(encoded))
		}
		callable, ok := value.Function()
		if !ok {
			t.Fatalf("decoded kind = %s, want function", value.Kind())
		}
		closure, ok := callable.(*scriptClosure)
		if !ok {
			t.Fatalf("decoded callable = %T, want *scriptClosure", callable)
		}
		if closure.script != owner {
			t.Fatal("decoded closure was not rebound to the receiving Script")
		}
		captured, ok := closure.state.lookup("$captured")
		if !ok || captured.Get().String() != "seven" {
			t.Fatalf("$captured = %v, present %v", captured, ok)
		}
		if closure.variableCell("$captured") != captured {
			t.Fatal("captured Scalar cell identity was not retained")
		}
		self, ok := closure.state.lookup("$this")
		if !ok {
			t.Fatal("decoded closure has no $this cell")
		}
		selfCallable, ok := self.Get().Function()
		if !ok || selfCallable != closure {
			t.Fatal("decoded $this does not point to the decoded closure")
		}
		result, err := callable.Invoke(context.Background(), String("tail"))
		if err != nil {
			t.Fatal(err)
		}
		// Sleep 2.1 includes ':' in the parsed-literal variable spelling
		// "$captured:", so this exact official vector returns only $1.
		if got := result.String(); got != "tail" {
			t.Fatalf("result = %q, want tail", got)
		}
	})
}

func TestOfficialSleepRestorableClosureContextsExecuteInOPFOR(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("closure-owner.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	for _, test := range []struct {
		name string
		want string
	}{
		{name: "closure-yielded", want: "tail"},
		{name: "closure-local-stack", want: "outer"},
		{name: "closure-callcc", want: "callcc state"},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded := readOfficialSleepSerializationVector(t, test.name)
			value, consumed, err := decodeSleepScalarStreamForScript(bytes.NewReader(encoded), owner)
			if err != nil {
				t.Fatal(err)
			}
			if consumed != int64(len(encoded)) {
				t.Fatalf("consumed = %d, want complete one-root stream %d", consumed, len(encoded))
			}
			callable, ok := value.Function()
			if !ok {
				t.Fatalf("decoded kind = %s, want function", value.Kind())
			}
			closure := callable.(*scriptClosure)
			if closure.script != owner {
				t.Fatal("decoded suspended closure was not rebound to the receiving Script")
			}
			result, err := callable.Invoke(context.Background(), String("tail"))
			if err != nil {
				t.Fatal(err)
			}
			if got := result.String(); got != test.want {
				t.Fatalf("resumed result = %q, want %q", got, test.want)
			}
			if len(closure.suspended) != 0 {
				t.Fatalf("closure retains %d contexts after resume", len(closure.suspended))
			}
		})
	}
}

func TestOfficialSleepSuspendedForeachPreservesMetadataOmissionFailure(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("closure-owner.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	encoded := readOfficialSleepSerializationVector(t, "closure-foreach")
	value, consumed, err := decodeSleepScalarStreamForScript(bytes.NewReader(encoded), owner)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != int64(len(encoded)) {
		t.Fatalf("consumed = %d, want complete one-root stream %d", consumed, len(encoded))
	}
	callable, ok := value.Function()
	if !ok {
		t.Fatalf("decoded kind = %s, want function", value.Kind())
	}
	closure := callable.(*scriptClosure)
	if len(closure.suspended) != 1 || closure.suspended[0].serializedForeach == nil {
		t.Fatal("decoded closure did not retain its metadata-omitted foreach context")
	}
	result, err := callable.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsNull() {
		t.Fatalf("resumed result = %s, want null", result.Describe())
	}
	const want = "Warning: null value error at STDIN:43\n" +
		"Warning: internal error - class java.util.EmptyStackException at STDIN:43\n"
	if got := output.String(); got != want {
		t.Fatalf("warning mismatch\nwant:\n%sgot:\n%s", want, got)
	}
	if len(closure.suspended) != 0 {
		t.Fatalf("closure retains %d contexts after failed resume", len(closure.suspended))
	}
	fresh, err := callable.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := fresh.String(); got != "a" {
		t.Fatalf("fresh invocation after consumed context = %q, want a", got)
	}
	if got := output.String(); got != want {
		t.Fatalf("fresh invocation added warnings\nwant:\n%sgot:\n%s", want, got)
	}
	if len(closure.suspended) != 1 || len(closure.suspended[0].iterators) != 1 {
		t.Fatal("fresh invocation did not create a live foreach suspension")
	}
	if _, err := encodeSleepScalarStream(value); err != nil {
		t.Fatalf("re-encode fresh official foreach suspension: %v", err)
	}
}

func TestSleepClosureGoGraphRoundTripExecutes(t *testing.T) {
	producerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	producerProgram, err := CompileString("producer.sl", `
$callback = lambda({ return $captured . ':' . $1; }, $captured => 'seven');
`)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := producerRuntime.Load(context.Background(), producerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producerRuntime.Close(context.Background()) })
	original := producer.Get("$callback")
	encoded, err := encodeSleepScalarStream(original)
	if err != nil {
		t.Fatal(err)
	}

	consumerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	consumerProgram, err := CompileString("consumer.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := consumerRuntime.Load(context.Background(), consumerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumerRuntime.Close(context.Background()) })
	decoded, _, err := decodeSleepScalarStreamForScript(bytes.NewReader(encoded), consumer)
	if err != nil {
		t.Fatal(err)
	}
	callable, ok := decoded.Function()
	if !ok {
		t.Fatalf("decoded kind = %s", decoded.Kind())
	}
	result, err := callable.Invoke(context.Background(), String("tail"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.String(); got != "seven:tail" {
		t.Fatalf("result = %q, want seven:tail", got)
	}
	closure := callable.(*scriptClosure)
	self, ok := closure.state.lookup("$this")
	selfCallable, selfOK := self.Get().Function()
	if !ok || !selfOK || selfCallable != closure {
		t.Fatal("round-tripped $this identity was not preserved")
	}
}

func TestSleepClosureSuspendedGraphRoundTripExecutes(t *testing.T) {
	producerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("suspended.sl", `
$callback = {
   local('$state');
   $state = 'outer';
   pushl();
   local('$state');
   $state = 'inner';
   yield 'first';
   popl();
   return $state . ':' . $1;
};
[$callback];
`)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := producerRuntime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producerRuntime.Close(context.Background()) })
	encoded, err := encodeSleepScalarStream(producer.Get("$callback"))
	if err != nil {
		t.Fatal(err)
	}

	consumerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ownerProgram, err := CompileString("owner.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := consumerRuntime.Load(context.Background(), ownerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumerRuntime.Close(context.Background()) })
	value, consumed, err := decodeSleepScalarStreamForScript(bytes.NewReader(encoded), owner)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != int64(len(encoded)) {
		t.Fatalf("consumed = %d, want %d", consumed, len(encoded))
	}
	callable, ok := value.Function()
	if !ok {
		t.Fatalf("decoded kind = %s, want function", value.Kind())
	}
	result, err := callable.Invoke(context.Background(), String("tail"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.String(); got != "outer:" {
		t.Fatalf("resumed result = %q, want outer:", got)
	}
}

func TestSleepClosureCallCCGraphRoundTripExecutes(t *testing.T) {
	producerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("callcc-serialization.sl", `
sub capture { $continuation = $1; return "parked"; }
sub source {
   local('$state');
   $state = "retained";
   callcc &capture;
   return $state . ':' . $1;
}

source();
`)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := producerRuntime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producerRuntime.Close(context.Background()) })
	encoded, err := encodeSleepScalarStream(producer.Get("$continuation"))
	if err != nil {
		t.Fatal(err)
	}

	consumerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ownerProgram, err := CompileString("callcc-owner.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := consumerRuntime.Load(context.Background(), ownerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumerRuntime.Close(context.Background()) })
	value, _, err := decodeSleepScalarStreamForScript(bytes.NewReader(encoded), owner)
	if err != nil {
		t.Fatal(err)
	}
	callable, ok := value.Function()
	if !ok {
		t.Fatalf("decoded kind = %s, want function", value.Kind())
	}
	result, err := callable.Invoke(context.Background(), String("tail"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.String(); got != "retained:tail" {
		t.Fatalf("resumed result = %q, want retained:tail", got)
	}
}

func TestSleepClosureIffUsesLazyDecideGraph(t *testing.T) {
	producerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("iff-serialization.sl", `
$factorial = {
   return iff($1 == 0, 1, $1 * [$this: $1 - 1]);
};
`)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := producerRuntime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producerRuntime.Close(context.Background()) })
	encoded, err := encodeSleepScalarStream(producer.Get("$factorial"))
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := decodeSleepJavaStream(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	closure := sleepClosureObjectFromScalarGraph(t, root)
	data, ok := closure.DataFor(sleepClosureDescriptor.Name)
	if !ok || len(data.Annotation) != 4 {
		t.Fatal("encoded SleepClosure custom data is malformed")
	}
	code, ok := data.Annotation[1].(*javaser.Object)
	if !ok {
		t.Fatal("encoded SleepClosure code is not a Block")
	}
	decide := sleepBlockStepWithDescriptor(t, code, sleepDecideDescriptor.Name)
	if decide == nil {
		t.Fatal("encoded iff expression has no lazy Decide Step")
	}
	start, err := sleepObjectField(decide, sleepDecideDescriptor.Name, "start")
	if err != nil {
		t.Fatal(err)
	}
	check, ok := start.(*javaser.Object)
	if !ok || check.Descriptor == nil || check.Descriptor.Name != sleepCheckEvalDescriptor.Name {
		t.Fatalf("Decide.start = %T, want CheckEval", start)
	}
	name, err := sleepStepStringField(check, sleepCheckEvalDescriptor.Name, "name")
	if err != nil {
		t.Fatal(err)
	}
	if name != "==" {
		t.Fatalf("CheckEval.name = %q, want ==", name)
	}

	consumerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ownerProgram, err := CompileString("iff-owner.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := consumerRuntime.Load(context.Background(), ownerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumerRuntime.Close(context.Background()) })
	value, _, err := decodeSleepScalarStreamForScript(bytes.NewReader(encoded), owner)
	if err != nil {
		t.Fatal(err)
	}
	callable, ok := value.Function()
	if !ok {
		t.Fatalf("decoded kind = %s, want function", value.Kind())
	}
	result, err := callable.Invoke(context.Background(), Int(7))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.String(); got != "5040" {
		t.Fatalf("factorial result = %q, want 5040", got)
	}
}

func TestSleepClosureSerializedContextsShareExactBlockAndStepHandles(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("context-identity.sl", `
inline identity_inline
{
   yield "inline";
   return "inline done";
}
sub identity_outer
{
   identity_inline();
   return "outer done";
}
$local = {
   local('$value');
   $value = "outer";
   pushl();
   local('$value');
   $value = "inner";
   yield "local";
   popl();
   return $value;
};
$inline = lambda(&identity_outer);
[$local];
[$inline];
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	for _, test := range []struct {
		name         string
		contextCount int
		localCount   int
	}{
		{name: "local", contextCount: 1, localCount: 2},
		{name: "inline", contextCount: 2, localCount: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := encodeSleepScalarStream(script.Get("$" + test.name))
			if err != nil {
				t.Fatal(err)
			}
			root, consumed, err := decodeSleepJavaStream(bytes.NewReader(encoded))
			if err != nil {
				t.Fatal(err)
			}
			if consumed != int64(len(encoded)) {
				t.Fatalf("consumed = %d, want %d", consumed, len(encoded))
			}
			closure := sleepClosureObjectFromScalarGraph(t, root)
			data, ok := closure.DataFor(sleepClosureDescriptor.Name)
			if !ok || len(data.Annotation) != 4 {
				t.Fatal("encoded SleepClosure custom data is malformed")
			}
			code := data.Annotation[1].(*javaser.Object)
			outer := data.Annotation[2].(*javaser.Object)
			toplevels, err := sleepStackElements(outer)
			if err != nil {
				t.Fatal(err)
			}
			if len(toplevels) != 1 {
				t.Fatalf("toplevel contexts = %d, want 1", len(toplevels))
			}
			entries, err := sleepStackElements(toplevels[0].(*javaser.Object))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != test.contextCount+1 {
				t.Fatalf("saved entries = %d, want %d Contexts plus locals", len(entries), test.contextCount)
			}
			for index, entry := range entries[:test.contextCount] {
				contextObject := entry.(*javaser.Object)
				context, err := decodeSleepContext(contextObject)
				if err != nil {
					t.Fatal(err)
				}
				if index+1 == test.contextCount && context.block != code {
					t.Fatal("outer Context.block is not the exact SleepClosure.code object handle")
				}
				if index+1 < test.contextCount && context.block == code {
					t.Fatal("inline Context.block unexpectedly aliases the outer code Block")
				}
				if context.last == nil || !sleepBlockHasExactStep(t, context.block, context.last) {
					t.Fatal("Context.last is not an exact Step handle in Context.block")
				}
			}
			locals := entries[len(entries)-1].(*javaser.Object)
			localData, ok := locals.DataFor(javaLinkedListDescriptor.Name)
			if !ok {
				t.Fatal("saved locals are not a LinkedList")
			}
			count, values, err := sleepAnnotationCount(localData.Annotation)
			if err != nil {
				t.Fatal(err)
			}
			if count != test.localCount || len(values) != test.localCount {
				t.Fatalf("saved local levels = %d/%d, want %d", count, len(values), test.localCount)
			}
		})
	}
}

func TestSleepClosureSuspendedForeachUsesOfficialContextHandles(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("foreach-context.sl", `
$closure = {
   local('$item');
   foreach $item (@("a", "b")) { yield $item; }
};
[$closure];
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	encoded, err := encodeSleepScalarStream(script.Get("$closure"))
	if err != nil {
		t.Fatal(err)
	}
	root, consumed, err := decodeSleepJavaStream(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if consumed != int64(len(encoded)) {
		t.Fatalf("consumed = %d, want %d", consumed, len(encoded))
	}
	closure := sleepClosureObjectFromScalarGraph(t, root)
	data, ok := closure.DataFor(sleepClosureDescriptor.Name)
	if !ok || len(data.Annotation) != 4 {
		t.Fatal("encoded SleepClosure custom data is malformed")
	}
	code := data.Annotation[1].(*javaser.Object)
	gotoStep := sleepBlockStepWithDescriptor(t, code, sleepGotoDescriptor.Name)
	if gotoStep == nil {
		t.Fatal("encoded foreach has no Goto Step")
	}
	if err := validateSleepForeachCheck(dataObjectField(t, gotoStep, sleepGotoDescriptor.Name, "start")); err != nil {
		t.Fatal(err)
	}
	body := dataObjectField(t, gotoStep, sleepGotoDescriptor.Name, "iftrue")
	contextStack := data.Annotation[2].(*javaser.Object)
	toplevels, err := sleepStackElements(contextStack)
	if err != nil {
		t.Fatal(err)
	}
	if len(toplevels) != 1 {
		t.Fatalf("toplevel contexts = %d, want 1", len(toplevels))
	}
	entries, err := sleepStackElements(toplevels[0].(*javaser.Object))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("saved entries = %d, want inner Context, outer Context, locals", len(entries))
	}
	inner, err := decodeSleepContext(entries[0].(*javaser.Object))
	if err != nil {
		t.Fatal(err)
	}
	outer, err := decodeSleepContext(entries[1].(*javaser.Object))
	if err != nil {
		t.Fatal(err)
	}
	if inner.block != body || inner.last != nil {
		t.Fatal("inner Context does not use the exact Goto.iftrue Block with a null resume Step")
	}
	if outer.block != code || outer.last != gotoStep {
		t.Fatal("outer Context does not use the exact SleepClosure.code Block and Goto Step handles")
	}
}

func dataObjectField(t *testing.T, object *javaser.Object, className, fieldName string) *javaser.Object {
	t.Helper()
	value, err := sleepObjectField(object, className, fieldName)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(*javaser.Object)
	if !ok {
		t.Fatalf("%s.%s = %T, want object", className, fieldName, value)
	}
	return result
}

func TestSleepClosureWriterRejectsUnsupportedContextShapesPrecisely(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		value  string
		want   string
	}{
		{
			name: "foreach-body-tail",
			source: `
$closure = {
   foreach $item (@("a", "b")) { yield $item; println("tail"); }
};
[$closure];
`,
			value: "$closure",
			want:  "foreach Sleep closure context with resumable body tail",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime, err := New()
			if err != nil {
				t.Fatal(err)
			}
			program, err := CompileString(test.name+"-context.sl", test.source)
			if err != nil {
				t.Fatal(err)
			}
			script, err := runtime.Load(context.Background(), program)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtime.Close(context.Background()) })
			_, err = encodeSleepScalarStream(script.Get(test.value))
			if err == nil {
				t.Fatal("encode unexpectedly succeeded")
			}
			var unsupported *UnsupportedError
			if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %T %v, want typed %q rejection", err, err, test.want)
			}
		})
	}
}

func sleepClosureObjectFromScalarGraph(t *testing.T, root javaser.Value) *javaser.Object {
	t.Helper()
	scalar, ok := root.(*javaser.Object)
	if !ok {
		t.Fatalf("root = %T, want Scalar object", root)
	}
	data, ok := scalar.DataFor(sleepScalarDescriptor.Name)
	if !ok || len(data.Annotation) != 3 {
		t.Fatal("Scalar custom data is malformed")
	}
	objectValue := data.Annotation[0].(*javaser.Object)
	value, err := sleepObjectField(objectValue, sleepObjectValueDescriptor.Name, "value")
	if err != nil {
		t.Fatal(err)
	}
	closure, ok := value.(*javaser.Object)
	if !ok || closure.Descriptor == nil || closure.Descriptor.Name != sleepClosureDescriptor.Name {
		t.Fatalf("Scalar object value = %T, want SleepClosure", value)
	}
	return closure
}

func sleepBlockHasExactStep(t *testing.T, block, target *javaser.Object) bool {
	t.Helper()
	first, err := sleepObjectField(block, sleepBlockDescriptor.Name, "first")
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := sleepStepReference(first)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[*javaser.Object]struct{})
	for current != nil {
		if current == target {
			return true
		}
		if _, duplicate := seen[current]; duplicate {
			t.Fatal("encoded Step.next chain is cyclic")
		}
		seen[current] = struct{}{}
		next, err := sleepObjectField(current, sleepStepDescriptor.Name, "next")
		if err != nil {
			t.Fatal(err)
		}
		current, _, err = sleepStepReference(next)
		if err != nil {
			t.Fatal(err)
		}
	}
	return false
}

func sleepBlockStepWithDescriptor(t *testing.T, block *javaser.Object, name string) *javaser.Object {
	t.Helper()
	first, err := sleepObjectField(block, sleepBlockDescriptor.Name, "first")
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := sleepStepReference(first)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[*javaser.Object]struct{})
	for current != nil {
		if _, duplicate := seen[current]; duplicate {
			t.Fatal("encoded Step.next chain is cyclic")
		}
		seen[current] = struct{}{}
		if current.Descriptor != nil && current.Descriptor.Name == name {
			return current
		}
		next, err := sleepObjectField(current, sleepStepDescriptor.Name, "next")
		if err != nil {
			t.Fatal(err)
		}
		current, _, err = sleepStepReference(next)
		if err != nil {
			t.Fatal(err)
		}
	}
	return nil
}

func TestSleepClosureCanonicalFixtures(t *testing.T) {
	for _, name := range []string{"oopack", "readobj", "inline", "localstack", "odd", "callcc", "writeobj", "ser_closure"} {
		name := name
		t.Run(name, func(t *testing.T) {
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
				t.Fatal(err)
			}
			var output bytes.Buffer
			runtime, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.Execute(context.Background(), program); err != nil {
				t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
			}
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestOfficialSleepJavaConsumesOPFORPhaseTwoClosures(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for Java-consumer verification")
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

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("producer.sl", `
$callback = lambda({ return $captured . ':' . $1; }, $captured => 'seven');
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	closure := script.Get("$callback")
	scalarStream, err := encodeSleepScalarStream(closure)
	if err != nil {
		t.Fatal(err)
	}
	rawStream, err := encodeSleepRawStream(closure)
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	scalarPath := filepath.Join(temporary, "opfor-closure-scalar.ser")
	rawPath := filepath.Join(temporary, "opfor-closure-raw.ser")
	if err := os.WriteFile(scalarPath, scalarStream, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawPath, rawStream, 0o600); err != nil {
		t.Fatal(err)
	}
	consumer := filepath.Join("testdata", "serialization", "consume_phase2.sl")
	command := osexec.Command(java, "-jar", jar, consumer, scalarPath, rawPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep consumer: %v\n%s", err, output)
	}
	const want = "seven:tail\nseven\nseven:again\nseven:raw\n"
	if string(output) != want {
		t.Fatalf("official Sleep consumer output mismatch\nwant:\n%s\ngot:\n%s", want, output)
	}
}

func TestOfficialSleepPhaseThreeProducerAndConsumerExecution(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for phase-three Java interoperability")
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
	officialInlinePath := filepath.Join(temporary, "official-inline.ser")
	officialOddPath := filepath.Join(temporary, "official-odd.ser")
	officialWriteobjPath := filepath.Join(temporary, "official-writeobj.ser")
	producerCommand := osexec.Command(
		java, "-jar", jar,
		filepath.Join("testdata", "serialization", "produce_phase3.sl"),
		officialInlinePath, officialOddPath, officialWriteobjPath,
	)
	producerOutput, err := producerCommand.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep producer: %v\n%s", err, producerOutput)
	}
	if got, want := string(producerOutput), "outer before\ninline before\n"; got != want {
		t.Fatalf("official Sleep producer output mismatch\nwant:\n%sgot:\n%s", want, got)
	}

	var goConsumerOutput bytes.Buffer
	consumerRuntime, err := New(WithStdout(&goConsumerOutput), WithStderr(&goConsumerOutput))
	if err != nil {
		t.Fatal(err)
	}
	ownerProgram, err := CompileString("phase3-owner.sl", `
inline phase3_inline { println("unused"); }
return 1;
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := consumerRuntime.Load(context.Background(), ownerProgram)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Set("$number", Int(7)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumerRuntime.Close(context.Background()) })
	for _, fixture := range []struct {
		path        string
		invocations int
		wantResult  string
	}{
		{path: officialInlinePath, invocations: 1},
		{path: officialWriteobjPath, invocations: 2, wantResult: "5040"},
		{path: officialOddPath, invocations: 1},
	} {
		path := fixture.path
		stream, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		value, consumed, err := decodeSleepScalarStreamForScript(bytes.NewReader(stream), owner)
		if err != nil {
			t.Fatal(err)
		}
		if consumed != int64(len(stream)) {
			t.Fatalf("%s consumed = %d, want %d", filepath.Base(path), consumed, len(stream))
		}
		callable, ok := value.Function()
		if !ok {
			t.Fatalf("%s decoded kind = %s, want function", filepath.Base(path), value.Kind())
		}
		var result Value
		for invocation := 0; invocation < fixture.invocations; invocation++ {
			result, err = callable.Invoke(context.Background())
			if err != nil {
				t.Fatalf("invoke %s #%d: %v", filepath.Base(path), invocation+1, err)
			}
		}
		if fixture.wantResult != "" && result.String() != fixture.wantResult {
			t.Fatalf("invoke %s result = %q, want %q", filepath.Base(path), result.String(), fixture.wantResult)
		}
	}
	if got, want := goConsumerOutput.String(), "inline after\nouter after\ntest passed!\n"; got != want {
		t.Fatalf("OPFOR consumer output mismatch\nwant:\n%sgot:\n%s", want, got)
	}

	var goProducerOutput bytes.Buffer
	producerRuntime, err := New(WithStdout(&goProducerOutput), WithStderr(&goProducerOutput))
	if err != nil {
		t.Fatal(err)
	}
	goProducerProgram, err := CompileString("phase3-producer.sl", `
inline phase3_inline
{
   println("inline before");
   yield "first";
   println("inline after");
}

sub phase3_outer
{
   println("outer before");
   phase3_inline();
   println("outer after");
}
$local = {
   local('$state');
   $state = "outer";
   pushl();
   local('$state');
   $state = "inner";
   yield "first";
   popl();
   return $state;
};
$inline = lambda(&phase3_outer);
$odd = {
   local('$handle');
   $handle = [SleepUtils getIOHandle: $null, [System out]];
   println($handle, "test passed!");
   closef($handle);
};
$number = 7;
$writeobj = {
   this('$rv $fact');
   $fact = { return iff($1 == 0, 1, $1 * [$this: $1 - 1]); };
   yield;
   $rv = [$fact: $number];
   yield;
   return $rv;
};
sub phase3_capture
{
   $saved = $1;
   return "parked";
}
sub phase3_source {
   local('$state');
   $state = "retained";
   callcc &phase3_capture;
   return $state . $1;
}
phase3_source();
[$local];
[$inline];
[$writeobj];
`)
	if err != nil {
		t.Fatal(err)
	}
	goProducer, err := producerRuntime.Load(context.Background(), goProducerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producerRuntime.Close(context.Background()) })
	if got, want := goProducerOutput.String(), "outer before\ninline before\n"; got != want {
		t.Fatalf("OPFOR producer suspension output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
	if _, ok := goProducer.Get("$saved").Function(); !ok {
		t.Fatalf("OPFOR producer did not retain a callable continuation: %s", goProducer.Get("$saved").String())
	}
	goPaths := make([]string, 0, 5)
	for _, fixture := range []struct {
		name     string
		variable string
	}{
		{name: "local", variable: "$local"},
		{name: "inline", variable: "$inline"},
		{name: "odd", variable: "$odd"},
		{name: "callcc", variable: "$saved"},
		{name: "writeobj", variable: "$writeobj"},
	} {
		stream, err := encodeSleepScalarStream(goProducer.Get(fixture.variable))
		if err != nil {
			t.Fatalf("encode %s: %v", fixture.name, err)
		}
		path := filepath.Join(temporary, "opfor-"+fixture.name+".ser")
		if err := os.WriteFile(path, stream, 0o600); err != nil {
			t.Fatal(err)
		}
		goPaths = append(goPaths, path)
	}
	consumerArguments := []string{"-jar", jar, filepath.Join("testdata", "serialization", "consume_phase3.sl")}
	consumerArguments = append(consumerArguments, goPaths...)
	consumerCommand := osexec.Command(java, consumerArguments...)
	consumerOutput, err := consumerCommand.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep phase-three consumer: %v\n%s", err, consumerOutput)
	}
	const wantJavaConsumer = "outer\ninline after\nouter after\nretainedtail\n5040\ntest passed!\n"
	if string(consumerOutput) != wantJavaConsumer {
		t.Fatalf("official Sleep phase-three consumer output mismatch\nwant:\n%sgot:\n%s", wantJavaConsumer, consumerOutput)
	}
}

func TestOfficialSleepSuspendedForeachProducerAndConsumerFailure(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for foreach interoperability")
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
	officialPath := filepath.Join(temporary, "official-foreach.ser")
	producerCommand := osexec.Command(
		java, "-jar", jar,
		filepath.Join("testdata", "serialization", "produce_foreach.sl"),
		officialPath,
	)
	if output, err := producerCommand.CombinedOutput(); err != nil {
		t.Fatalf("official Sleep foreach producer: %v\n%s", err, output)
	} else if len(output) != 0 {
		t.Fatalf("official Sleep foreach producer output = %q, want empty", output)
	}

	var goConsumerOutput bytes.Buffer
	consumerRuntime, err := New(WithStdout(&goConsumerOutput), WithStderr(&goConsumerOutput))
	if err != nil {
		t.Fatal(err)
	}
	ownerProgram, err := CompileString("foreach-owner.sl", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := consumerRuntime.Load(context.Background(), ownerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumerRuntime.Close(context.Background()) })
	officialStream, err := os.ReadFile(officialPath)
	if err != nil {
		t.Fatal(err)
	}
	value, consumed, err := decodeSleepScalarStreamForScript(bytes.NewReader(officialStream), owner)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != int64(len(officialStream)) {
		t.Fatalf("official foreach consumed = %d, want %d", consumed, len(officialStream))
	}
	callable, ok := value.Function()
	if !ok {
		t.Fatalf("official foreach decoded kind = %s, want function", value.Kind())
	}
	if _, err := callable.Invoke(context.Background()); err != nil {
		t.Fatal(err)
	}
	const goWarnings = "Warning: null value error at produce_foreach.sl:8\n" +
		"Warning: internal error - class java.util.EmptyStackException at produce_foreach.sl:8\n"
	if got := goConsumerOutput.String(); got != goWarnings {
		t.Fatalf("OPFOR official-foreach warning mismatch\nwant:\n%sgot:\n%s", goWarnings, got)
	}

	producerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	producerProgram, err := CompileString("opfor-foreach-producer.sl", `
$callback = {
   local('$item');
   foreach $item (@("a", "b", "c")) { yield $item; }
};
[$callback];
`)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := producerRuntime.Load(context.Background(), producerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producerRuntime.Close(context.Background()) })
	opforStream, err := encodeSleepScalarStream(producer.Get("$callback"))
	if err != nil {
		t.Fatal(err)
	}
	opforPath := filepath.Join(temporary, "opfor-foreach.ser")
	if err := os.WriteFile(opforPath, opforStream, 0o600); err != nil {
		t.Fatal(err)
	}
	consumerCommand := osexec.Command(
		java, "-jar", jar,
		filepath.Join("testdata", "serialization", "consume_foreach.sl"),
		opforPath,
	)
	javaOutput, err := consumerCommand.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep foreach consumer: %v\n%s", err, javaOutput)
	}
	const javaWarnings = "before\n" +
		"Warning: null value error at opfor-foreach-producer.sl:4\n" +
		"Warning: internal error - class java.util.EmptyStackException at opfor-foreach-producer.sl:4\n" +
		"after\n"
	if got := string(javaOutput); got != javaWarnings {
		t.Fatalf("official Sleep OPFOR-foreach warning mismatch\nwant:\n%sgot:\n%s", javaWarnings, got)
	}
}
