package opfor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPortableJavaStringBuilderConstructorsAndCapacity(t *testing.T) {
	t.Parallel()

	defaultBuilder := constructPortableStringBuilder(t, "StringBuilder")
	if got := invokePortableStringBuilder(t, defaultBuilder, "length").Int32(); got != 0 {
		t.Fatalf("default length = %d, want 0", got)
	}
	if got := invokePortableStringBuilder(t, defaultBuilder, "capacity").Int32(); got != 16 {
		t.Fatalf("default capacity = %d, want 16", got)
	}

	capacityBuilder := constructPortableStringBuilder(t, "StringBuilder", Int(4))
	if got := invokePortableStringBuilder(t, capacityBuilder, "toString").String(); got != "" {
		t.Fatalf("integer constructor contents = %q, want empty", got)
	}
	if got := invokePortableStringBuilder(t, capacityBuilder, "capacity").Int32(); got != 4 {
		t.Fatalf("integer constructor capacity = %d, want 4", got)
	}

	text := sleepStringValueFromUnits([]uint16{'A', 0xd83d, 0xde00, 0xd800}, nil)
	textBuilder := constructPortableStringBuilder(t, "StringBuffer", text)
	if got := invokePortableStringBuilder(t, textBuilder, "length").Int32(); got != 4 {
		t.Fatalf("UTF-16 constructor length = %d, want 4", got)
	}
	if got := invokePortableStringBuilder(t, textBuilder, "capacity").Int32(); got != 20 {
		t.Fatalf("text constructor capacity = %d, want 20", got)
	}
	assertSleepStringUnits(t, invokePortableStringBuilder(t, textBuilder, "toString"), []uint16{'A', 0xd83d, 0xde00, 0xd800}, []bool{false, false, false, false})

	for _, target := range []string{"java.lang.CharSequence", "java.lang.Appendable", "java.lang.Comparable", "java.io.Serializable", "java.lang.Object"} {
		value, handled, err := textBuilder.invoke(ObjectInvocation{Op: ObjectTypeCheck, Class: target})
		if err != nil || !handled || !value.Truth() {
			t.Errorf("StringBuffer isa %s = (%s, %t, %v), want true", target, value.Describe(), handled, err)
		}
	}

	negative := ObjectInvocation{Op: ObjectConstruct, Class: "StringBuilder", Arguments: []Argument{{Value: Int(-1)}}}
	if value, handled, err := portableJavaStringBuilderConstruct(negative); !handled || !value.IsNull() || err == nil || err.Error() != "java.lang.NegativeArraySizeException: -1" {
		t.Fatalf("negative constructor = (%s, %t, %v)", value.Describe(), handled, err)
	}
}

func TestPortableJavaStringBuilderMutationAndQueries(t *testing.T) {
	t.Parallel()

	builder := constructPortableStringBuilder(t, "StringBuilder", String("abcdef"))
	target := ObjectValue(builder)
	if got := invokePortableStringBuilder(t, builder, "append", String("😀")); !got.IdentityEqual(target) {
		t.Fatalf("append returned %s, want receiver identity", got.Describe())
	}
	invokePortableStringBuilder(t, builder, "insert", Int(3), String("-"))
	invokePortableStringBuilder(t, builder, "delete", Int(1), Int(3))
	invokePortableStringBuilder(t, builder, "deleteCharAt", Int(1))
	invokePortableStringBuilder(t, builder, "replace", Int(1), Int(4), String("XYZ"))
	invokePortableStringBuilder(t, builder, "setCharAt", Int(1), String("q"))

	assertSleepStringUnits(t, invokePortableStringBuilder(t, builder, "toString"), []uint16{'a', 'q', 'Y', 'Z', 0xd83d, 0xde00}, nil)
	if got := invokePortableStringBuilder(t, builder, "charAt", Int(4)); !sleepStringValuesEqual(got, sleepUTF16CharacterValue(0xd83d)) {
		t.Fatalf("charAt high surrogate = %s", got.Describe())
	}
	if got := invokePortableStringBuilder(t, builder, "substring", Int(1), Int(4)).String(); got != "qYZ" {
		t.Fatalf("substring = %q, want qYZ", got)
	}
	if got := invokePortableStringBuilder(t, builder, "subSequence", Int(2)).String(); got != "YZ😀" {
		t.Fatalf("subSequence = %q, want YZ😀", got)
	}
	if got := invokePortableStringBuilder(t, builder, "indexOf", String("YZ")).Int32(); got != 2 {
		t.Fatalf("indexOf = %d, want 2", got)
	}
	if got := invokePortableStringBuilder(t, builder, "lastIndexOf", String("😀")).Int32(); got != 4 {
		t.Fatalf("lastIndexOf = %d, want 4", got)
	}

	invokePortableStringBuilder(t, builder, "reverse")
	assertSleepStringUnits(t, invokePortableStringBuilder(t, builder, "toString"), []uint16{0xd83d, 0xde00, 'Z', 'Y', 'q', 'a'}, nil)
}

func TestPortableJavaStringBuilderSequenceOverloadsAndSelfAppend(t *testing.T) {
	t.Parallel()

	builder := constructPortableStringBuilder(t, "StringBuilder", String("ab"))
	invokePortableStringBuilder(t, builder, "append", ObjectValue(builder))
	invokePortableStringBuilder(t, builder, "append", String("WXYZ"), Int(1), Int(3))

	characters := newPortableJavaArray(
		portableJavaArrayType{name: "char", descriptor: "C", primitive: true},
		[]int{4},
		[]Value{String("p"), String("q"), String("r"), String("s")},
	)
	invokePortableStringBuilder(t, builder, "append", ObjectValue(characters), Int(1), Int(2))
	invokePortableStringBuilder(t, builder, "insert", Int(0), String("123"), Int(1), Int(3))
	invokePortableStringBuilder(t, builder, "insert", Int(2), ObjectValue(characters), Int(0), Int(1))
	invokePortableStringBuilder(t, builder, "append", Null())
	if got, want := invokePortableStringBuilder(t, builder, "toString").String(), "23pababXYqrnull"; got != want {
		t.Fatalf("sequence overloads = %q, want %q", got, want)
	}

	copy := constructPortableStringBuilder(t, "StringBuffer", ObjectValue(builder))
	invokePortableStringBuilder(t, builder, "append", String("!"))
	if got, want := invokePortableStringBuilder(t, copy, "toString").String(), "23pababXYqrnull"; got != want {
		t.Fatalf("CharSequence constructor copy = %q, want %q", got, want)
	}
}

func TestPortableJavaStringBuilderCapacityGrowthAndSetLength(t *testing.T) {
	t.Parallel()

	builder := constructPortableStringBuilder(t, "StringBuilder", Int(2))
	invokePortableStringBuilder(t, builder, "append", String("abc"))
	if got := invokePortableStringBuilder(t, builder, "capacity").Int32(); got != 6 {
		t.Fatalf("overflow growth capacity = %d, want 6", got)
	}
	invokePortableStringBuilder(t, builder, "ensureCapacity", Int(7))
	if got := invokePortableStringBuilder(t, builder, "capacity").Int32(); got != 14 {
		t.Fatalf("ensureCapacity growth = %d, want 14", got)
	}
	invokePortableStringBuilder(t, builder, "ensureCapacity", Int(-1))
	invokePortableStringBuilder(t, builder, "setLength", Int(8))
	assertSleepStringUnits(t, invokePortableStringBuilder(t, builder, "toString"), []uint16{'a', 'b', 'c', 0, 0, 0, 0, 0}, nil)
	invokePortableStringBuilder(t, builder, "setLength", Int(2))
	invokePortableStringBuilder(t, builder, "trimToSize")
	if got := invokePortableStringBuilder(t, builder, "capacity").Int32(); got != 2 {
		t.Fatalf("trimmed capacity = %d, want 2", got)
	}
}

func TestPortableJavaStringBuilderSleepBridgeCoercions(t *testing.T) {
	t.Parallel()

	builder := constructPortableStringBuilder(t, "StringBuilder", String("abc65"))
	invokePortableStringBuilder(t, builder, "replace", Int(0), Int(1), Int(7))
	if got := invokePortableStringBuilder(t, builder, "indexOf", Int(65)).Int32(); got != 3 {
		t.Fatalf("numeric fallback String indexOf = %d, want 3", got)
	}
	// ObjectUtilities considers a numeric scalar a fallback match for char and
	// marshals the first character of its textual spelling.
	invokePortableStringBuilder(t, builder, "setCharAt", Int(0), Int(65))
	if got := invokePortableStringBuilder(t, builder, "toString").String(); got != "6bc65" {
		t.Fatalf("numeric char fallback = %q, want 6bc65", got)
	}

	value, handled, err := builder.invoke(portableStringBuilderInvocation(builder, "setCharAt", Int(0), String("xy")))
	if err != nil || !handled || !value.IsNull() {
		t.Fatalf("multi-character setCharAt = (%s, %t, %v), want soft no-match", value.Describe(), handled, err)
	}
	_, handled, err = builder.invoke(portableStringBuilderInvocation(builder, "setCharAt", Int(0), Null()))
	if !handled || err == nil || !strings.HasPrefix(err.Error(), "java.lang.StringIndexOutOfBoundsException:") {
		t.Fatalf("null setCharAt = (handled %t, %v), want StringIndexOutOfBoundsException", handled, err)
	}
}

func TestPortableJavaStringBuilderPreservesBinaryAndSurrogateUnits(t *testing.T) {
	t.Parallel()

	builder := constructPortableStringBuilder(t, "StringBuilder", BinaryString([]byte{0xc3, 0xa9}))
	invokePortableStringBuilder(t, builder, "append", String("é"))
	assertSleepStringUnits(t, invokePortableStringBuilder(t, builder, "toString"), []uint16{0x00c3, 0x00a9, 0x00e9}, []bool{true, true, false})
	invokePortableStringBuilder(t, builder, "reverse")
	assertSleepStringUnits(t, invokePortableStringBuilder(t, builder, "toString"), []uint16{0x00e9, 0x00a9, 0x00c3}, []bool{false, true, true})

	surrogates := sleepStringValueFromUnits([]uint16{'A', 0xd83d, 0xde00, 'B', 0xd800}, nil)
	surrogateBuilder := constructPortableStringBuilder(t, "StringBuffer", surrogates)
	invokePortableStringBuilder(t, surrogateBuilder, "reverse")
	assertSleepStringUnits(t, invokePortableStringBuilder(t, surrogateBuilder, "toString"), []uint16{0xd800, 'B', 0xd83d, 0xde00, 'A'}, nil)
}

func TestPortableJavaStringBuilderBoundsErrors(t *testing.T) {
	t.Parallel()

	builder := constructPortableStringBuilder(t, "StringBuilder", String("abc"))
	for _, test := range []struct {
		method string
		args   []Value
	}{
		{method: "charAt", args: []Value{Int(3)}},
		{method: "delete", args: []Value{Int(2), Int(1)}},
		{method: "deleteCharAt", args: []Value{Int(-1)}},
		{method: "insert", args: []Value{Int(4), String("x")}},
		{method: "replace", args: []Value{Int(4), Int(5), String("x")}},
		{method: "setCharAt", args: []Value{Int(3), String("x")}},
		{method: "setLength", args: []Value{Int(-1)}},
		{method: "substring", args: []Value{Int(0), Int(4)}},
	} {
		invocation := portableStringBuilderInvocation(builder, test.method, test.args...)
		value, handled, err := builder.invoke(invocation)
		if !handled || !value.IsNull() || err == nil || !strings.HasPrefix(err.Error(), "java.lang.StringIndexOutOfBoundsException:") {
			t.Errorf("%s%v = (%s, %t, %v), want StringIndexOutOfBoundsException", test.method, test.args, value.Describe(), handled, err)
		}
	}

	// Java clamps an oversized end index for delete and replace.
	invokePortableStringBuilder(t, builder, "replace", Int(1), Int(99), String("x"))
	if got := invokePortableStringBuilder(t, builder, "toString").String(); got != "ax" {
		t.Fatalf("clamped replace = %q, want ax", got)
	}
	invokePortableStringBuilder(t, builder, "delete", Int(1), Int(99))
	if got := invokePortableStringBuilder(t, builder, "toString").String(); got != "a" {
		t.Fatalf("clamped delete = %q, want a", got)
	}
}

func TestPortableJavaStringBuilderCharAtReleasesReadLock(t *testing.T) {
	t.Parallel()

	builder := constructPortableStringBuilder(t, "StringBuilder", String("ab"))
	if got := invokePortableStringBuilder(t, builder, "charAt", Int(0)).String(); got != "a" {
		t.Fatalf("charAt = %q, want a", got)
	}

	done := make(chan struct{})
	go func() {
		_, _, _ = builder.invoke(portableStringBuilderInvocation(builder, "setCharAt", Int(0), String("z")))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("setCharAt blocked after charAt; read lock was not released")
	}
	if got := invokePortableStringBuilder(t, builder, "toString").String(); got != "zb" {
		t.Fatalf("post-charAt mutation = %q, want zb", got)
	}
}

func TestPortableJavaStringBufferConcurrentOperations(t *testing.T) {
	buffer := constructPortableStringBuilder(t, "StringBuffer")
	const writers = 12
	const appends = 200

	start := make(chan struct{})
	done := make(chan struct{})
	var writerGroup sync.WaitGroup
	for worker := 0; worker < writers; worker++ {
		writerGroup.Add(1)
		go func() {
			defer writerGroup.Done()
			<-start
			for index := 0; index < appends; index++ {
				value, handled, err := buffer.invoke(portableStringBuilderInvocation(buffer, "append", String("x")))
				if err != nil || !handled || !value.IdentityEqual(ObjectValue(buffer)) {
					t.Errorf("concurrent append = (%s, %t, %v)", value.Describe(), handled, err)
					return
				}
			}
		}()
	}
	var readerGroup sync.WaitGroup
	for reader := 0; reader < 3; reader++ {
		readerGroup.Add(1)
		go func() {
			defer readerGroup.Done()
			<-start
			for {
				select {
				case <-done:
					return
				default:
					_ = buffer.String()
					_, _, _ = buffer.invoke(portableStringBuilderInvocation(buffer, "capacity"))
				}
			}
		}()
	}
	close(start)
	writerGroup.Wait()
	close(done)
	readerGroup.Wait()

	if got, want := buffer.length(), writers*appends; got != want {
		t.Fatalf("concurrent StringBuffer length = %d, want %d", got, want)
	}
}

func TestPortableJavaStringBuilderRuntimeAndObjectHostPrecedence(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "string-builder.sl", `$builder = [new StringBuilder: 2];
[$builder append: "ab"];
[$builder insert: 1, "😀"];
[$builder delete: 0, 1];
return [$builder toString];`)
	if err != nil || value.String() != "😀b" {
		t.Fatalf("runtime StringBuilder = (%s, %v), want 😀b", value.Describe(), err)
	}

	appendCalls := 0
	overrideRuntime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Op == ObjectInvoke && invocation.Message == "append" {
			appendCalls++
			return String("importer"), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = overrideRuntime.Close(context.Background()) })
	value, err = overrideRuntime.Eval(context.Background(), "string-builder-host.sl", `$builder = [new StringBuilder: "a"];
[$builder append: "b"];
return [$builder toString];`)
	if err != nil || value.String() != "a" || appendCalls != 1 {
		t.Fatalf("ObjectHost precedence = (%s, %v, append calls %d), want (a, nil, 1)", value.Describe(), err, appendCalls)
	}
}

func constructPortableStringBuilder(t *testing.T, class string, arguments ...Value) *portableJavaStringBuffer {
	t.Helper()
	invocation := ObjectInvocation{Op: ObjectConstruct, Class: class}
	for _, argument := range arguments {
		invocation.Arguments = append(invocation.Arguments, Argument{Value: argument})
	}
	value, handled, err := portableJavaStringBuilderConstruct(invocation)
	if err != nil || !handled {
		t.Fatalf("new %s(%v) = (%s, %t, %v)", class, arguments, value.Describe(), handled, err)
	}
	object, ok := value.Object()
	if !ok {
		t.Fatalf("new %s(%v) = %s, want object", class, arguments, value.Describe())
	}
	buffer, ok := object.(*portableJavaStringBuffer)
	if !ok || buffer == nil {
		t.Fatalf("new %s(%v) object = %T", class, arguments, object)
	}
	return buffer
}

func invokePortableStringBuilder(t *testing.T, buffer *portableJavaStringBuffer, method string, arguments ...Value) Value {
	t.Helper()
	value, handled, err := buffer.invoke(portableStringBuilderInvocation(buffer, method, arguments...))
	if err != nil || !handled {
		t.Fatalf("%s.%s(%v) = (%s, %t, %v)", buffer.class, method, arguments, value.Describe(), handled, err)
	}
	return value
}

func portableStringBuilderInvocation(buffer *portableJavaStringBuffer, method string, arguments ...Value) ObjectInvocation {
	invocation := ObjectInvocation{Op: ObjectInvoke, Target: ObjectValue(buffer), Message: method}
	for _, argument := range arguments {
		invocation.Arguments = append(invocation.Arguments, Argument{Value: argument})
	}
	return invocation
}

func assertSleepStringUnits(t *testing.T, value Value, wantUnits []uint16, wantRaw []bool) {
	t.Helper()
	gotUnits := sleepStringUnits(value)
	gotRaw := sleepStringRawMask(value)
	if len(wantRaw) == 0 {
		wantRaw = make([]bool, len(wantUnits))
	}
	if !equalUTF16Units(gotUnits, wantUnits) {
		t.Fatalf("UTF-16 units = %x, want %x", gotUnits, wantUnits)
	}
	if len(gotRaw) != len(wantRaw) {
		t.Fatalf("raw mask length = %d, want %d", len(gotRaw), len(wantRaw))
	}
	for index := range gotRaw {
		if gotRaw[index] != wantRaw[index] {
			t.Fatalf("raw mask = %v, want %v", gotRaw, wantRaw)
		}
	}
}

func TestPortableJavaStringBuilderErrorsRemainJavaExceptions(t *testing.T) {
	t.Parallel()
	builder := constructPortableStringBuilder(t, "StringBuilder", String("x"))
	_, _, err := builder.invoke(portableStringBuilderInvocation(builder, "charAt", Int(-1)))
	var exception *portableJavaException
	if !errors.As(newPortableJavaException(err), &exception) || !exception.isA("java.lang.IndexOutOfBoundsException") {
		t.Fatalf("error = %v, want Java IndexOutOfBoundsException", err)
	}
}
