package opfor

import (
	"bytes"
	"context"
	"regexp"
	"testing"
)

var forkHandleIdentityPattern = regexp.MustCompile(`sleep\.bridges\.io\.IOObject@[[:xdigit:]]+|<io:fork>`)

func normalizeForkHandleIdentity(output string) string {
	return forkHandleIdentityPattern.ReplaceAllString(output, "<fork-handle>")
}

func TestCallCCForkCanonicalNormalizedTrace(t *testing.T) {
	got, want := runCanonicalOutput(t, "callccfork")
	got = []byte(normalizeForkHandleIdentity(string(got)))
	want = []byte(normalizeForkHandleIdentity(string(want)))
	if !bytes.Equal(got, want) {
		t.Fatalf("fork-handle-normalized output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestForkerCanonicalNormalizedTrace(t *testing.T) {
	got, want := runCanonicalOutput(t, "forker")
	got = []byte(normalizeForkHandleIdentity(string(got)))
	want = []byte(normalizeForkHandleIdentity(string(want)))
	if !bytes.Equal(got, want) {
		t.Fatalf("fork-handle-normalized output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestForkLaunchTraceOrdersBetweenChildOutputAndTrace(t *testing.T) {
	const source = `debug(15);
println(wait(fork({
    println("child");
    return "done";
}), 1000));`
	const want = `child
Trace: &fork(&closure[fork-order.sl:3-4]#1) = <io:fork> at fork-order.sl:2
Trace: &println('child') at fork-order.sl:3
Trace: &wait(<io:fork>, 1000) = 'done' at fork-order.sl:2
done
Trace: &println('done') at fork-order.sl:2
`
	if got := executeForkTraceScript(t, "fork-order.sl", source); got != want {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestForkRootCallCCTraceIsEmittedOnce(t *testing.T) {
	const source = `debug(15);
println(wait(fork({
    println("before callcc");
    callcc {
        return "done";
    };
}), 1000));`
	const want = `before callcc
Trace: &fork(&closure[fork-callcc-trace.sl:3-4]#1) = <io:fork> at fork-callcc-trace.sl:2
Trace: &println('before callcc') at fork-callcc-trace.sl:3
Trace: [&closure[fork-callcc-trace.sl:5]#3 CALLCC: &closure[fork-callcc-trace.sl:3-4]#2] = 'done' at fork-callcc-trace.sl:4
Trace: &wait(<io:fork>, 1000) = 'done' at fork-callcc-trace.sl:2
done
Trace: &println('done') at fork-callcc-trace.sl:2
`
	if got := executeForkTraceScript(t, "fork-callcc-trace.sl", source); got != want {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func executeForkTraceScript(t *testing.T, name, source string) string {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Eval(context.Background(), name, source); err != nil {
		t.Fatalf("Eval: %v\noutput:\n%s", err, output.String())
	}
	return output.String()
}
