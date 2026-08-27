package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sleepBasicIOCalledKeyProbeName = "sleep-basicio-called-key.sl"

// This probe is intentionally network-free. Each socket call carries an
// invalid fourth callback argument, so BasicIO.SocketFuncs fails at
// BridgeUtilities.getFunction before SocketHandler.start can open a socket.
const sleepBasicIOCalledKeyProbe = `sub socket_connect_cross {
    println("socket-connect-cross-before");
    setf("&connect", function("&listen"));
    connect("127.0.0.1", 1, 0, $null);
    println("socket-connect-cross-tail");
}
socket_connect_cross();
println("socket-connect-cross-resume");
sub socket_listen_cross {
    println("socket-listen-cross-before");
    setf("&listen", function("&connect"));
    $peer = "unset";
    listen(1, 0, $peer, $null);
    println("socket-listen-cross-tail");
}
socket_listen_cross();
println("socket-listen-cross-resume");
sub socket_unknown_alias {
    println("socket-unknown-before");
    setf("&zsocket", function("&listen"));
    zsocket("127.0.0.1", 1, 0, $null);
    println("socket-unknown-tail");
}
socket_unknown_alias();
println("socket-unknown-resume");

$consume_cross = allocate();
writeb($consume_cross, "abcdef");
closef($consume_cross);
setf("&skip", function("&consume"));
println("consume-cross=" . skip($consume_cross, 2, 1));
println("consume-cross-tail=" . readb($consume_cross, -1));

$skip_cross = allocate();
writeb($skip_cross, "abcdef");
closef($skip_cross);
setf("&consume", function("&skip"));
println("skip-cross=" . consume($skip_cross, 1, 1));
println("skip-cross-tail=" . readb($skip_cross, -1));

$consume_unknown = allocate();
writeb($consume_unknown, "abcdef");
closef($consume_unknown);
setf("&zconsume", function("&skip"));
println("consume-unknown=" . zconsume($consume_unknown, 3, 2));
println("consume-unknown-tail=" . readb($consume_unknown, -1));

setf("&printf", function("&println"));
printf("println-to-printf");
setf("&println", function("&printf"));
println("printf-to-println");
setf("&zprintln", function("&printf"));
zprintln("println-unknown");
`

const sleepBasicIOCalledKeyNormalizedOutput = `socket-connect-cross-before
Warning: expected &closure--received: $null at <source>:<line>
socket-connect-cross-resume
socket-listen-cross-before
Warning: expected &closure--received: $null at <source>:<line>
socket-listen-cross-resume
socket-unknown-before
Warning: expected &closure--received: $null at <source>:<line>
socket-unknown-resume
consume-cross=2
consume-cross-tail=cdef
skip-cross=1
skip-cross-tail=bcdef
consume-unknown=3
consume-unknown-tail=def
println-to-printf
printf-to-println
println-unknown
`

func TestSleepBasicIOSharedBridgeCalledKeyCompatibility(t *testing.T) {
	output, hostCalls := runSleepBasicIOCalledKeyProbe(t, nil)
	if hostCalls != 0 {
		t.Fatalf("Host calls = %d, want 0", hostCalls)
	}
	if got := normalizeSleepBasicIOCalledKeyWarnings(output); got != sleepBasicIOCalledKeyNormalizedOutput {
		t.Fatalf("BasicIO called-key output mismatch\nwant:\n%sgot:\n%s", sleepBasicIOCalledKeyNormalizedOutput, got)
	}
}

func TestSleepBasicIOSharedBridgeAliasesDoNotFallBackToHost(t *testing.T) {
	var hostNames []string
	host := HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostNames = append(hostNames, invocation.Name)
		return String("host-fallback"), nil
	})
	output, hostCalls := runSleepBasicIOCalledKeyProbe(t, host)
	if hostCalls != 0 || len(hostNames) != 0 {
		t.Fatalf("Host received BasicIO aliases: count=%d names=%q", hostCalls, hostNames)
	}
	if got := normalizeSleepBasicIOCalledKeyWarnings(output); got != sleepBasicIOCalledKeyNormalizedOutput {
		t.Fatalf("BasicIO called-key output with Host mismatch\nwant:\n%sgot:\n%s", sleepBasicIOCalledKeyNormalizedOutput, got)
	}
}

func TestSleepBasicIOSharedBridgeCalledKeyOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepBasicIOCalledKeyProbeName)
	if err := os.WriteFile(path, []byte(sleepBasicIOCalledKeyProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep BasicIO called-key probe: %v\n%s", err, want)
	}
	got, hostCalls := runSleepBasicIOCalledKeyProbe(t, nil)
	if hostCalls != 0 {
		t.Fatalf("Host calls = %d, want 0", hostCalls)
	}
	if !bytes.Equal([]byte(got), want) {
		t.Fatalf("official Sleep BasicIO called-key output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepBasicIOCalledKeyProbe(t *testing.T, host Host) (string, int) {
	t.Helper()
	var output bytes.Buffer
	hostCalls := 0
	options := []Option{WithStdout(&output), WithStderr(&output)}
	if host != nil {
		options = append(options, WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
			hostCalls++
			return host.Call(ctx, invocation)
		})))
	}
	runtimeInstance, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepBasicIOCalledKeyProbeName, sleepBasicIOCalledKeyProbe); err != nil {
		t.Fatal(err)
	}
	return output.String(), hostCalls
}

func normalizeSleepBasicIOCalledKeyWarnings(output string) string {
	location := " at " + sleepBasicIOCalledKeyProbeName + ":"
	lines := strings.SplitAfter(output, "\n")
	for index, line := range lines {
		if !strings.HasPrefix(line, "Warning: ") {
			continue
		}
		position := strings.LastIndex(line, location)
		if position < 0 {
			continue
		}
		newline := ""
		if strings.HasSuffix(line, "\n") {
			newline = "\n"
		}
		lines[index] = line[:position] + " at <source>:<line>" + newline
	}
	return strings.Join(lines, "")
}
