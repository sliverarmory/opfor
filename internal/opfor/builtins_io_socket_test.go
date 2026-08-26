package opfor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	osexec "os/exec"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const officialSleep21JARSHA256 = "0ddde5e9e8d8d8d334d071b1f887c379f5d0be9b190566f05365997b3e375ff1"

func TestSocketTerminalCloseUnblocksWriterBeforeTakingHandleLock(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	task, err := (&ioBuiltinState{runtime: runtimeInstance}).newSleepSocketTask(
		context.Background(), Invocation{Runtime: runtimeInstance}, sleepSocketConnect, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	local, peer := net.Pipe()
	defer peer.Close()
	if !task.adopt(local) || !task.attach(local) {
		t.Fatal("failed to attach in-memory socket transport")
	}

	writeResult := make(chan error, 1)
	go func() {
		_, writeErr := task.handle.Write([]byte("blocked"))
		writeResult <- writeErr
	}()
	deadline := time.After(2 * time.Second)
	for {
		if !task.handle.writeMu.TryLock() {
			break
		}
		task.handle.writeMu.Unlock()
		goruntime.Gosched()
		select {
		case err := <-writeResult:
			t.Fatalf("net.Pipe write completed before terminal close: %v", err)
		case <-deadline:
			t.Fatal("socket writer did not enter the handle write section")
		default:
		}
	}

	closeResult := make(chan struct{})
	go func() {
		task.cancelAndClose()
		close(closeResult)
	}()
	select {
	case <-closeResult:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal socket close deadlocked behind a blocked writer")
	}
	select {
	case err := <-writeResult:
		if err == nil {
			t.Fatal("blocked socket write unexpectedly succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked socket writer did not wake after transport close")
	}
	task.complete()
}

// TestSleepBasicIOSocketArgumentContract covers BasicIO.java lines 508-565,
// SocketObject.SocketHandler's defaults, and BridgeUtilities' extraction of
// named KeyValuePair arguments. Named arguments do not consume positional
// slots, unknown names are ignored, names are case-sensitive, and the
// leftmost source duplicate wins after Sleep's right-to-left evaluation.
func TestSleepBasicIOSocketArgumentContract(t *testing.T) {
	leftAddress := NewCell(String("127.0.0.1"))
	call := parseSleepSocketCall(Invocation{Arguments: []Argument{
		{Value: String("host")},
		{Name: "linger", Value: Int(11)},
		{Name: "linger", Value: Int(22)},
		{Name: "Linger", Value: Int(33)},
		{Name: "laddr", Reference: leftAddress},
		{Name: "unknown", Value: String("ignored")},
		{Value: Int(4444)},
	}})
	if got, want := len(call.positional), 2; got != want {
		t.Fatalf("positional count = %d, want %d", got, want)
	}
	if got := call.positional[0].Resolve().String(); got != "host" {
		t.Fatalf("first positional = %q, want host", got)
	}
	if got := call.positional[1].Resolve().Int32(); got != 4444 {
		t.Fatalf("second positional = %d, want 4444", got)
	}
	if got := call.options.linger; got != 11 {
		t.Fatalf("duplicate linger = %d, want leftmost 11", got)
	}
	if !call.options.laddrSet || call.options.laddr != "127.0.0.1" {
		t.Fatalf("laddr = %q, %t", call.options.laddr, call.options.laddrSet)
	}

	defaults := parseSleepSocketCall(Invocation{})
	if defaults.options.linger != 5 || defaults.options.laddrSet || defaults.options.lport != 0 || defaults.options.backlog != 0 {
		t.Fatalf("socket option defaults = %#v", defaults.options)
	}
	for _, test := range []struct {
		value Value
		want  int32
	}{
		{value: String("010"), want: 10},
		{value: String("0x10"), want: 0},
		{value: String("1.5"), want: 0},
		{value: Double(1.9), want: 1},
		{value: Double(1e40), want: 1<<31 - 1},
		{value: Long(1<<32 + 7), want: 7},
		{value: ObjectValue(socketNumericObject("010")), want: 8},
		{value: ObjectValue(socketNumericObject("#10")), want: 16},
		{value: ObjectValue(socketNumericObject("0b10")), want: 0},
		{value: ObjectValue(socketNumericObject("0o10")), want: 0},
		{value: ObjectValue(socketNumericObject("1_0")), want: 0},
		{value: ObjectValue(socketNumericObject("#-1")), want: 0},
		{value: ObjectValue(socketNumericObject("0x+1")), want: 0},
		{value: ObjectValue(socketNumericObject("0-1")), want: 0},
	} {
		if got := sleepSocketInt32(test.value); got != test.want {
			t.Errorf("socket int conversion of %s = %d, want %d", test.value.Describe(), got, test.want)
		}
	}
}

func TestSleepBasicIOSocketCallbackRequiresFourthPositionalArgument(t *testing.T) {
	runtime, state := newSocketTestRuntime(t)
	var calls atomic.Int32
	third := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
		calls.Add(1)
		return Null(), nil
	}))
	if _, err := state.connect(context.Background(), socketInvocation(runtime, 0, "connect",
		Argument{Value: String("127.0.0.1")}, Argument{Value: Int(1)}, Argument{Value: Int(1)}, Argument{Value: String("not a closure")})); err == nil || !strings.Contains(err.Error(), "expected &closure") {
		t.Fatalf("invalid fourth callback error = %v", err)
	}

	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	acceptDone := make(chan error, 1)
	go func() {
		conn, acceptErr := server.Accept()
		if conn != nil {
			_ = conn.Close()
		}
		acceptDone <- acceptErr
	}()
	connected, err := state.connect(context.Background(), socketInvocation(runtime, 0, "connect",
		Argument{Value: String("127.0.0.1")},
		Argument{Value: Int(int32(server.Addr().(*net.TCPAddr).Port))},
		Argument{Value: third}))
	if err != nil {
		t.Fatal(err)
	}
	if err := <-acceptDone; err != nil {
		t.Fatal(err)
	}
	_ = server.Close()
	if calls.Load() != 0 {
		t.Fatalf("connect(host, port, closure) invoked callback %d times", calls.Load())
	}
	_ = mustSocketHandle(t, connected).close()

	listenResult := make(chan Value, 1)
	listenError := make(chan error, 1)
	go func() {
		handle, listenErr := state.listen(context.Background(), socketInvocation(runtime, 0, "listen",
			Argument{Value: Int(0)}, Argument{Value: Int(2_000)}, Argument{Value: third}))
		listenResult <- handle
		listenError <- listenErr
	}()
	listener := socketListenerForPort(t, runtime.socketState, 0)
	client, err := net.DialTimeout("tcp", listener.listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var accepted Value
	select {
	case accepted = <-listenResult:
	case <-time.After(2 * time.Second):
		t.Fatal("listen(port, timeout, closure) did not return synchronously after accept")
	}
	if err := <-listenError; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("listen(port, timeout, closure) invoked callback %d times", calls.Load())
	}
	_ = mustSocketHandle(t, accepted).close()
	runtime.socketState.release(0)
}

func TestSleepBasicIOConnectIsDuplexAndTracksEOF(t *testing.T) {
	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	serverDone := make(chan error, 1)
	go func() {
		conn, acceptErr := server.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer conn.Close()
		line, readErr := bufio.NewReader(conn).ReadString('\n')
		if readErr != nil {
			serverDone <- readErr
			return
		}
		if line != "ping\n" {
			serverDone <- fmt.Errorf("server read %q", line)
			return
		}
		if _, writeErr := io.WriteString(conn, "pong\n"); writeErr != nil {
			serverDone <- writeErr
			return
		}
		tcp, ok := conn.(*net.TCPConn)
		if !ok {
			serverDone <- fmt.Errorf("accepted connection is %T, want *net.TCPConn", conn)
			return
		}
		if writeErr := tcp.CloseWrite(); writeErr != nil {
			serverDone <- writeErr
			return
		}
		_, readErr = conn.Read(make([]byte, 1))
		if !errors.Is(readErr, io.EOF) {
			serverDone <- fmt.Errorf("peer read after client readln EOF = %v, want EOF", readErr)
			return
		}
		serverDone <- nil
	}()

	runtime, _ := newSocketTestRuntime(t)
	functions := runtime.ioFunctions()
	port := int32(server.Addr().(*net.TCPAddr).Port)
	handle, err := runtime.Invoke(context.Background(), "connect", Null(), Int(port), Int(2_000))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "available", handle); got.Kind() != KindInt || got.Int32() != 0 {
		t.Fatalf("available(open socket) = %s, want integer 0", got.Describe())
	}
	assertIOEOF(t, runtime, functions, handle, false)
	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, String("ping\n"))
	if got := mustCallIOBuiltin(t, runtime, functions, "readln", handle).String(); got != "pong" {
		t.Fatalf("readln = %q, want pong", got)
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readln", handle); !got.IsNull() {
		t.Fatalf("readln at socket EOF = %s, want null", got.Describe())
	}
	assertIOEOF(t, runtime, functions, handle, true)
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestSleepBasicIOReadlnEOFFullyClosesDuplexAndConsole(t *testing.T) {
	left, right := net.Pipe()
	handle := newIOHandle("readln-duplex", left, left, true, true, false)
	runtime, err := New(WithStdout(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	functions := runtime.ioFunctions()
	peerDone := make(chan error, 1)
	go func() {
		_, writeErr := io.WriteString(right, "tail")
		closeErr := right.Close()
		peerDone <- errors.Join(writeErr, closeErr)
	}()
	if got := mustCallIOBuiltin(t, runtime, functions, "readln", ObjectValue(handle)).String(); got != "tail" {
		t.Fatalf("partial line at EOF = %q, want tail", got)
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
	handle.mu.Lock()
	reader, writer := handle.reader, handle.writer
	handle.mu.Unlock()
	if reader != nil || writer != nil {
		t.Fatalf("readln EOF left duplex pipelines open: reader=%v writer=%v", reader != nil, writer != nil)
	}

	var output bytes.Buffer
	consoleRuntime, err := New(WithStdin(strings.NewReader("console-tail")), WithStdout(&output))
	if err != nil {
		t.Fatal(err)
	}
	consoleFunctions := consoleRuntime.ioFunctions()
	console := mustCallIOBuiltin(t, consoleRuntime, consoleFunctions, "getConsole")
	if _, err := consoleRuntime.Invoke(context.Background(), "print", String("before|")); err != nil {
		t.Fatal(err)
	}
	if got := mustCallIOBuiltin(t, consoleRuntime, consoleFunctions, "readln").String(); got != "console-tail" {
		t.Fatalf("console partial line at EOF = %q", got)
	}
	if _, err := consoleRuntime.Invoke(context.Background(), "print", String("implicit|")); err != nil {
		t.Fatal(err)
	}
	if _, err := consoleRuntime.Invoke(context.Background(), "print", console, String("explicit|")); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "before|" {
		t.Fatalf("console output after readln EOF = %q, want before|", got)
	}
}

func TestSleepBasicIOListenSetsPeerAndReusesTimedOutListener(t *testing.T) {
	runtime, state := newSocketTestRuntime(t)
	owner := newSocketTestOwner(t, runtime)
	functions := runtime.ioFunctions()

	peer := NewCell(String("unchanged"))
	first, err := state.listen(context.Background(), socketInvocation(runtime, owner.ID(), "listen",
		Argument{Value: Int(0)}, Argument{Value: Int(10)}, Argument{Reference: peer}))
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}
	if peer.Get().String() != "unchanged" {
		t.Fatalf("timed-out listen changed peer to %q", peer.Get().String())
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "available", first); !got.IsNull() {
		t.Fatalf("available(timed-out listen) = %s, want null", got.Describe())
	}
	problem := socketCheckError(t, runtime, owner)
	if !strings.Contains(problem.String(), "java.net.SocketTimeoutException: Accept timed out") {
		t.Fatalf("timed-out listen checkError = %s", problem.Describe())
	}

	listener := socketListenerForPort(t, runtime.socketState, 0)
	actualPort := int32(listener.listener.Addr().(*net.TCPAddr).Port)
	callbackDone := make(chan Value, 1)
	callback := FunctionValue(ioTestCallable(func(_ context.Context, values ...Value) (Value, error) {
		callbackDone <- values[0]
		return String("discarded"), nil
	}))
	second, err := state.listen(context.Background(), socketInvocation(runtime, owner.ID(), "listen",
		Argument{Value: Int(0)}, Argument{Value: Int(2_000)}, Argument{Reference: peer}, Argument{Value: callback},
		Argument{Name: "laddr", Value: String("unresolvable.invalid")},
		Argument{Name: "backlog", Value: Int(1)}))
	if err != nil {
		t.Fatalf("second listen: %v", err)
	}
	if reused := socketListenerForPort(t, runtime.socketState, 0); reused != listener {
		t.Fatal("listen did not reuse the requested-port listener after timeout")
	}

	client, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(actualPort))), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	select {
	case callbackHandle := <-callbackDone:
		if !callbackHandle.IdentityEqual(second) {
			t.Fatalf("callback handle = %s, want returned handle", callbackHandle.Describe())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listen callback did not run")
	}
	if got := peer.Get().String(); got != "127.0.0.1" {
		t.Fatalf("peer = %q, want 127.0.0.1", got)
	}
	assertIOEOF(t, runtime, functions, second, false)
	if _, err := io.WriteString(client, "from-client\n"); err != nil {
		t.Fatal(err)
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readln", second).String(); got != "from-client" {
		t.Fatalf("accepted socket readln = %q, want from-client", got)
	}
	mustCallIOBuiltin(t, runtime, functions, "writeb", second, String("from-listener\n"))
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got, err := bufio.NewReader(client).ReadString('\n'); err != nil || got != "from-listener\n" {
		t.Fatalf("accepted socket client read = %q, %v", got, err)
	}
	if value, waitErr := callIOBuiltinForScript(context.Background(), runtime, functions, owner.ID(), "wait", second, Int(-1)); waitErr != nil || !value.IsNull() {
		t.Fatalf("completed wait(-1) = %s, %v; want null, nil", value.Describe(), waitErr)
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", second)
	mustCallIOBuiltin(t, runtime, functions, "closef", Int(0))
}

func TestSleepBasicIOListenAllowsConcurrentCachedAccepts(t *testing.T) {
	runtime, state := newSocketTestRuntime(t)
	firstOwner := newSocketTestOwner(t, runtime)
	secondOwner := newSocketTestOwner(t, runtime)
	functions := runtime.ioFunctions()
	firstCallback := make(chan struct{}, 1)
	secondCallback := make(chan struct{}, 1)

	first, err := state.listen(context.Background(), socketInvocation(runtime, firstOwner.ID(), "listen",
		Argument{Value: Int(0)}, Argument{Value: Int(2_000)}, Argument{Value: Null()},
		Argument{Value: FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
			firstCallback <- struct{}{}
			return Null(), nil
		}))}, Argument{Name: "laddr", Value: String("127.0.0.1")}))
	if err != nil {
		t.Fatal(err)
	}
	_ = socketListenerForPort(t, runtime.socketState, 0)
	second, err := state.listen(context.Background(), socketInvocation(runtime, secondOwner.ID(), "listen",
		Argument{Value: Int(0)}, Argument{Value: Int(25)}, Argument{Value: Null()},
		Argument{Value: FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
			secondCallback <- struct{}{}
			return Null(), nil
		}))}))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-secondCallback:
	case <-time.After(time.Second):
		t.Fatal("short concurrent accept remained serialized behind long accept")
	}
	if _, err := callIOBuiltinForScript(context.Background(), runtime, functions, secondOwner.ID(), "wait", second); err != nil {
		t.Fatal(err)
	}
	if problem := socketCheckError(t, runtime, secondOwner); problem.String() != "java.net.SocketTimeoutException: Accept timed out" {
		t.Fatalf("short concurrent accept checkError = %s", problem.Describe())
	}
	select {
	case <-firstCallback:
		t.Fatal("long concurrent accept completed with the short accept")
	default:
	}

	mustCallIOBuiltin(t, runtime, functions, "closef", Int(0))
	if _, err := callIOBuiltinForScript(context.Background(), runtime, functions, firstOwner.ID(), "wait", first); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstCallback:
	default:
		t.Fatal("listener release did not wake the remaining concurrent accept")
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", first)
	mustCallIOBuiltin(t, runtime, functions, "closef", second)
}

func TestSleepBasicIOSocketCallbackWaitAndRelease(t *testing.T) {
	runtime, state := newSocketTestRuntime(t)
	owner := newSocketTestOwner(t, runtime)
	functions := runtime.ioFunctions()

	entered := make(chan Value, 1)
	releaseCallback := make(chan struct{})
	var calls atomic.Int32
	callback := FunctionValue(ioTestCallable(func(_ context.Context, values ...Value) (Value, error) {
		calls.Add(1)
		entered <- values[0]
		<-releaseCallback
		return Int(99), nil
	}))
	handle, err := state.listen(context.Background(), socketInvocation(runtime, owner.ID(), "listen",
		Argument{Value: Int(0)}, Argument{Value: Int(0)}, Argument{Value: Null()}, Argument{Value: callback},
		Argument{Name: "laddr", Value: String("127.0.0.1")}))
	if err != nil {
		t.Fatal(err)
	}
	listener := socketListenerForPort(t, runtime.socketState, 0)
	client, err := net.DialTimeout("tcp4", listener.listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	select {
	case callbackHandle := <-entered:
		if !callbackHandle.IdentityEqual(handle) {
			t.Fatalf("callback argument = %s, want returned handle", callbackHandle.Describe())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback did not start")
	}

	if value, waitErr := callIOBuiltinForScript(context.Background(), runtime, functions, owner.ID(), "wait", handle, Int(1)); waitErr != nil || !value.IsNull() {
		t.Fatalf("timed wait = %s, %v", value.Describe(), waitErr)
	}
	problem := socketCheckError(t, runtime, owner)
	if !strings.Contains(problem.String(), "java.io.IOException: wait on object timed out") {
		t.Fatalf("wait checkError = %s", problem.Describe())
	}
	longWaitContext, cancelLongWait := context.WithTimeout(context.Background(), 10*time.Millisecond)
	_, longWaitErr := callIOBuiltinForScript(longWaitContext, runtime, functions, owner.ID(), "wait", handle, Long(1<<63-1))
	cancelLongWait()
	if !errors.Is(longWaitErr, context.DeadlineExceeded) {
		t.Fatalf("wait(Long.MAX_VALUE) error = %v, want context deadline", longWaitErr)
	}
	if problem := socketCheckError(t, runtime, owner); !problem.IsNull() {
		t.Fatalf("wait(Long.MAX_VALUE) soft error = %s, want null", problem.Describe())
	}
	close(releaseCallback)
	if value, waitErr := callIOBuiltinForScript(context.Background(), runtime, functions, owner.ID(), "wait", handle); waitErr != nil || !value.IsNull() {
		t.Fatalf("wait = %s, %v; want null, nil", value.Describe(), waitErr)
	}
	if calls.Load() != 1 {
		t.Fatalf("callback calls = %d, want 1", calls.Load())
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", Int(0))
}

func TestSleepBasicIOSocketCallbackSetsMessageVariable(t *testing.T) {
	runtime, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(context.Background(), "socket-callback-message.sl", `
global('$callback_zero');
$socket = connect('127.0.0.1', -1, 10, { $callback_zero = $0; });
wait($socket);
return $callback_zero;
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != "&callback" {
		t.Fatalf("socket callback $0 = %q, want &callback", got)
	}
}

func TestSleepBasicIOSocketCallbackResolvesNamedSub(t *testing.T) {
	runtime, state := newSocketTestRuntime(t)
	value, err := runtime.Eval(context.Background(), "socket-named-callback.sl", `
global('$named_callback');
sub named_socket_callback { $named_callback = $0 . ':' . iff(-eof $1, 'eof', 'open'); }
$socket = connect('127.0.0.1', -1, 10, '&named_socket_callback');
wait($socket);
return $named_callback;
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != "&callback:eof" {
		t.Fatalf("named socket callback = %q, want &callback:eof", got)
	}

	program, err := CompileString("socket-bare-callback.sl", `
sub bare_named_socket_callback { return 1; }
return 1;
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.connect(context.Background(), socketInvocation(runtime, owner.ID(), "connect",
		Argument{Value: String("127.0.0.1")}, Argument{Value: Int(-1)}, Argument{Value: Int(10)},
		Argument{Value: String("bare_named_socket_callback")})); err == nil || !strings.Contains(err.Error(), "expected &closure") {
		t.Fatalf("bare named callback error = %v, want expected &closure", err)
	}
}

func TestSleepBasicIOSocketSoftErrorsAndListenerRelease(t *testing.T) {
	runtime, state := newSocketTestRuntime(t)
	owner := newSocketTestOwner(t, runtime)
	functions := runtime.ioFunctions()

	reserved, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refusedPort := int32(reserved.Addr().(*net.TCPAddr).Port)
	_ = reserved.Close()
	failed, err := state.connect(context.Background(), socketInvocation(runtime, owner.ID(), "connect",
		Argument{Value: String("127.0.0.1")}, Argument{Value: Int(refusedPort)}, Argument{Value: Int(250)}))
	if err != nil {
		t.Fatalf("refused connect returned bridge error: %v", err)
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "available", failed); !got.IsNull() {
		t.Fatalf("available(refused connect) = %s, want null", got.Describe())
	}
	assertIOEOF(t, runtime, functions, failed, true)
	if problem := socketCheckError(t, runtime, owner); !strings.Contains(problem.String(), "java.net.ConnectException: Connection refused") {
		t.Fatalf("refused connect checkError = %s", problem.Describe())
	}
	if problem := socketCheckError(t, runtime, owner); !problem.IsNull() {
		t.Fatalf("second checkError = %s, want null", problem.Describe())
	}
	failedHandle := mustSocketHandle(t, failed)
	mustCallIOBuiltin(t, runtime, functions, "closef", failed)
	if task := runtime.socketState.lookup(failedHandle); task != nil {
		t.Fatal("closef retained a completed failed socket task")
	}
	owner.mu.Lock()
	for task := range owner.socketTasks {
		if task.handle == failedHandle {
			owner.mu.Unlock()
			t.Fatal("closef retained a completed failed socket task on its owner")
		}
	}
	owner.mu.Unlock()
	if _, err := state.connect(context.Background(), socketInvocation(runtime, owner.ID(), "connect",
		Argument{Value: String("[::1")}, Argument{Value: Int(1)}, Argument{Value: Int(25)})); err != nil {
		t.Fatalf("bad-host connect returned bridge error: %v", err)
	}
	if problem := socketCheckError(t, runtime, owner); !strings.Contains(problem.String(), "java.net.UnknownHostException: [::1") {
		t.Fatalf("bad-host connect checkError = %s", problem.Describe())
	}
	if _, err := state.connect(context.Background(), socketInvocation(runtime, owner.ID(), "connect",
		Argument{Value: String("127.0.0.1")}, Argument{Value: Int(-1)})); err != nil {
		t.Fatalf("bad-port connect returned bridge error: %v", err)
	}
	if problem := socketCheckError(t, runtime, owner); !strings.Contains(problem.String(), "java.lang.IllegalArgumentException: port out of range:-1") {
		t.Fatalf("bad-port connect checkError = %s", problem.Describe())
	}
	if _, err := state.listen(context.Background(), socketInvocation(runtime, owner.ID(), "listen",
		Argument{Value: Int(-1)}, Argument{Value: Int(1)}, Argument{Value: Null()},
		Argument{Name: "laddr", Value: String("[::1")},
		Argument{Name: "laddr", Value: String("127.0.0.1")})); err != nil {
		t.Fatalf("bad-address/bad-port listen returned bridge error: %v", err)
	}
	if problem := socketCheckError(t, runtime, owner); problem.String() != "java.net.UnknownHostException: [::1: invalid IPv6 address literal" {
		t.Fatalf("bad-address/bad-port listen checkError = %s", problem.Describe())
	}
	if _, err := state.listen(context.Background(), socketInvocation(runtime, owner.ID(), "listen",
		Argument{Value: Int(-1)})); err != nil {
		t.Fatalf("bad-port listen returned bridge error: %v", err)
	}
	if problem := socketCheckError(t, runtime, owner); !strings.Contains(problem.String(), "java.lang.IllegalArgumentException: Port value out of range: -1") {
		t.Fatalf("bad-port listen checkError = %s", problem.Describe())
	}
	if _, err := state.listen(context.Background(), socketInvocation(runtime, owner.ID(), "listen",
		Argument{Value: Int(0)}, Argument{Value: Int(-1)}, Argument{Value: Null()},
		Argument{Name: "laddr", Value: String("127.0.0.1")})); err != nil {
		t.Fatalf("negative-timeout listen returned bridge error: %v", err)
	}
	if problem := socketCheckError(t, runtime, owner); !strings.Contains(problem.String(), "java.lang.IllegalArgumentException: timeout < 0") {
		t.Fatalf("negative-timeout listen checkError = %s", problem.Describe())
	}
	// ServerSocket construction happens before setSoTimeout, so the listener is
	// cached even though the first call rejects its negative timeout.
	_ = socketListenerForPort(t, runtime.socketState, 0)
	mustCallIOBuiltin(t, runtime, functions, "closef", Int(0))

	callbackDone := make(chan struct{}, 1)
	callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
		callbackDone <- struct{}{}
		return Null(), nil
	}))
	handle, err := state.listen(context.Background(), socketInvocation(runtime, owner.ID(), "listen",
		Argument{Value: Int(0)}, Argument{Value: Int(0)}, Argument{Value: Null()}, Argument{Value: callback},
		Argument{Name: "laddr", Value: String("127.0.0.1")}))
	if err != nil {
		t.Fatal(err)
	}
	_ = socketListenerForPort(t, runtime.socketState, 0)
	mustCallIOBuiltin(t, runtime, functions, "closef", Int(0))
	if _, waitErr := callIOBuiltinForScript(context.Background(), runtime, functions, owner.ID(), "wait", handle); waitErr != nil {
		t.Fatal(waitErr)
	}
	select {
	case <-callbackDone:
	default:
		t.Fatal("listener-release failure did not invoke callback")
	}
	if problem := socketCheckError(t, runtime, owner); !strings.Contains(problem.String(), "java.net.SocketException: Socket closed") {
		t.Fatalf("released listener checkError = %s", problem.Describe())
	}
	// Releasing a missing listener is intentionally a repeatable no-op.
	mustCallIOBuiltin(t, runtime, functions, "closef", Int(0))
}

func TestSleepBasicIOSocketNegativeLingerRunsAfterConnectAndAccept(t *testing.T) {
	t.Run("connect", func(t *testing.T) {
		server, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer server.Close()
		closed := make(chan error, 1)
		go func() {
			conn, acceptErr := server.Accept()
			if acceptErr != nil {
				closed <- acceptErr
				return
			}
			defer conn.Close()
			_, readErr := conn.Read(make([]byte, 1))
			closed <- readErr
		}()

		runtime, state := newSocketTestRuntime(t)
		owner := newSocketTestOwner(t, runtime)
		functions := runtime.ioFunctions()
		handle, err := state.connect(context.Background(), socketInvocation(runtime, owner.ID(), "connect",
			Argument{Value: String("127.0.0.1")},
			Argument{Value: Int(int32(server.Addr().(*net.TCPAddr).Port))},
			Argument{Value: Int(2_000)}, Argument{Name: "linger", Value: Int(-1)}))
		if err != nil {
			t.Fatal(err)
		}
		if problem := socketCheckError(t, runtime, owner); problem.String() != "java.lang.IllegalArgumentException: invalid value for SO_LINGER" {
			t.Fatalf("negative connect linger checkError = %s", problem.Describe())
		}
		if got := mustCallIOBuiltin(t, runtime, functions, "available", handle); !got.IsNull() {
			t.Fatalf("negative-linger connect available = %s, want null", got.Describe())
		}
		mustCallIOBuiltin(t, runtime, functions, "closef", handle)
		select {
		case err := <-closed:
			if !errors.Is(err, io.EOF) {
				t.Fatalf("server close observation = %v, want EOF", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("closef did not close the adopted pre-stream connection")
		}
	})

	t.Run("listen", func(t *testing.T) {
		runtime, state := newSocketTestRuntime(t)
		owner := newSocketTestOwner(t, runtime)
		functions := runtime.ioFunctions()
		peer := NewCell(String("unchanged"))
		callbackDone := make(chan struct{}, 1)
		callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
			callbackDone <- struct{}{}
			return Null(), nil
		}))
		handle, err := state.listen(context.Background(), socketInvocation(runtime, owner.ID(), "listen",
			Argument{Value: Int(0)}, Argument{Value: Int(2_000)}, Argument{Reference: peer}, Argument{Value: callback},
			Argument{Name: "laddr", Value: String("127.0.0.1")}, Argument{Name: "linger", Value: Int(-1)}))
		if err != nil {
			t.Fatal(err)
		}
		listener := socketListenerForPort(t, runtime.socketState, 0)
		client, err := net.DialTimeout("tcp4", listener.listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if _, err := callIOBuiltinForScript(context.Background(), runtime, functions, owner.ID(), "wait", handle); err != nil {
			t.Fatal(err)
		}
		select {
		case <-callbackDone:
		default:
			t.Fatal("negative-linger accept did not invoke callback")
		}
		if problem := socketCheckError(t, runtime, owner); problem.String() != "java.lang.IllegalArgumentException: invalid value for SO_LINGER" {
			t.Fatalf("negative listen linger checkError = %s", problem.Describe())
		}
		if peer.Get().String() != "unchanged" {
			t.Fatalf("negative-linger accept changed peer to %q", peer.Get().String())
		}
		mustCallIOBuiltin(t, runtime, functions, "closef", handle)
		mustCallIOBuiltin(t, runtime, functions, "closef", Int(0))
	})
}

func TestSleepBasicIOConnectIPv6Loopback(t *testing.T) {
	server, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		conn, acceptErr := server.Accept()
		if acceptErr == nil {
			_, acceptErr = io.WriteString(conn, "v6\n")
			_ = conn.Close()
		}
		done <- acceptErr
	}()
	runtime, _ := newSocketTestRuntime(t)
	handle, err := runtime.Invoke(context.Background(), "connect",
		String("::1"), Int(int32(server.Addr().(*net.TCPAddr).Port)), Int(2_000))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCallIOBuiltin(t, runtime, runtime.ioFunctions(), "readln", handle).String(); got != "v6" {
		t.Fatalf("IPv6 readln = %q, want v6", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSleepBasicIOSocketScriptUnloadCancelsWorker(t *testing.T) {
	runtime, state := newSocketTestRuntime(t)
	owner := newSocketTestOwner(t, runtime)
	var calls atomic.Int32
	callback := FunctionValue(ioTestCallable(func(context.Context, ...Value) (Value, error) {
		calls.Add(1)
		return Null(), nil
	}))
	handle, err := state.listen(context.Background(), socketInvocation(runtime, owner.ID(), "listen",
		Argument{Value: Int(0)}, Argument{Value: Int(0)}, Argument{Value: Null()}, Argument{Value: callback},
		Argument{Name: "laddr", Value: String("127.0.0.1")}))
	if err != nil {
		t.Fatal(err)
	}
	listener := socketListenerForPort(t, runtime.socketState, 0)
	task := runtime.socketState.lookup(mustSocketHandle(t, handle))
	if task == nil {
		t.Fatal("missing pending socket task")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := owner.Unload(ctx); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	select {
	case <-task.done:
	default:
		t.Fatal("Unload returned before the socket worker completed")
	}
	if calls.Load() != 0 {
		t.Fatalf("callback calls after owner unload = %d, want 0", calls.Load())
	}
	if got := runtime.socketState.lookup(mustSocketHandle(t, handle)); got != nil {
		t.Fatal("unloaded socket task remains runtime-owned")
	}
	// The listener cache is Runtime-owned rather than Script-owned, preserving
	// reuse across loaded scripts until closef(port) or Runtime.Close.
	if cached := socketListenerForPort(t, runtime.socketState, 0); cached != listener {
		t.Fatal("script unload unexpectedly released the runtime listener cache")
	}
}

func TestSleepBasicIOSocketRuntimeCloseCancelsUnownedListen(t *testing.T) {
	runtime, state := newSocketTestRuntime(t)
	result := make(chan error, 1)
	go func() {
		_, listenErr := state.listen(context.Background(), socketInvocation(runtime, 0, "listen",
			Argument{Value: Int(0)}, Argument{Value: Int(0)}, Argument{Value: Null()}))
		result <- listenErr
	}()
	listener := socketListenerForPort(t, runtime.socketState, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(ctx); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("canceled listen: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("Runtime.Close did not release blocking listen: %v", ctx.Err())
	}
	if _, err := listener.listener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("listener after Runtime.Close error = %v, want net.ErrClosed", err)
	}
}

func TestSleepBasicIOSocketImporterOverrideWins(t *testing.T) {
	runtime, err := New(WithFunction("connect", func(context.Context, Invocation) (Value, error) {
		return String("importer-connect"), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Invoke(context.Background(), "connect", String("127.0.0.1"), Int(1))
	if err != nil || value.String() != "importer-connect" {
		t.Fatalf("overridden connect = %s, %v", value.Describe(), err)
	}
}

// TestSleepBasicIOSocketOfficialJARDifferential is opt-in because the BSD JAR
// is supplied separately. Both sides only connect to a test-owned IPv4
// loopback echo listener; ordinary CI remains pure Go and network-independent.
func TestSleepBasicIOSocketOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for socket differential verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	port := reserveSocketTestPort(t)
	goOutput := runPureGoSocketProbe(t, port)
	javaServer := startSocketProbeServer(t, port)
	command := osexec.Command(java, "-jar", jar, "-e", sleepSocketProbeSource(port))
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep socket probe: %v\n%s", err, javaOutput)
	}
	if serverErr := <-javaServer; serverErr != nil {
		t.Fatal(serverErr)
	}
	if !bytes.Equal(goOutput, javaOutput) {
		t.Fatalf("official Sleep socket output mismatch\nwant:\n%sgot:\n%s", javaOutput, goOutput)
	}

	goErrorOutput := runPureGoSocketErrorProbe(t)
	command = osexec.Command(java, "-jar", jar, "-e", sleepSocketErrorProbeSource)
	javaErrorOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep socket error-order probe: %v\n%s", err, javaErrorOutput)
	}
	if !bytes.Equal(goErrorOutput, javaErrorOutput) {
		t.Fatalf("official Sleep socket error-order mismatch\nwant:\n%sgot:\n%s", javaErrorOutput, goErrorOutput)
	}
}

func newSocketTestRuntime(t *testing.T) (*Runtime, *ioBuiltinState) {
	t.Helper()
	runtime, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	state := &ioBuiltinState{runtime: runtime, console: runtime.console, cwd: t.TempDir()}
	t.Cleanup(func() {
		tasks := runtime.socketState.shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for _, task := range tasks {
			_ = task.join(ctx)
		}
	})
	return runtime, state
}

func newSocketTestOwner(t *testing.T, runtime *Runtime) *Script {
	t.Helper()
	program, err := CompileString("socket-owner.sl", "return 1;\n")
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	return script
}

func socketInvocation(runtime *Runtime, script ScriptID, name string, arguments ...Argument) Invocation {
	return Invocation{Runtime: runtime, Script: script, Name: name, Arguments: arguments}
}

func socketCheckError(t *testing.T, runtime *Runtime, script *Script) Value {
	t.Helper()
	value, err := runtime.checkError(context.Background(), socketInvocation(runtime, script.ID(), "checkError"))
	if err != nil {
		t.Fatalf("checkError: %v", err)
	}
	return value
}

func mustSocketHandle(t *testing.T, value Value) *sleepIOHandle {
	t.Helper()
	handle, ok := ioHandleValue(value)
	if !ok {
		t.Fatalf("value %s is not an I/O handle", value.Describe())
	}
	return handle
}

func socketListenerForPort(t *testing.T, state *sleepSocketState, port int32) *sleepSocketListener {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state.mu.Lock()
		listener := state.listeners[port]
		state.mu.Unlock()
		if listener != nil {
			return listener
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("listener for requested port %d was not created", port)
	return nil
}

func reserveSocketTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func startSocketProbeServer(t *testing.T, port int) <-chan error {
	t.Helper()
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		defer listener.Close()
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer conn.Close()
		line, readErr := bufio.NewReader(conn).ReadString('\n')
		if readErr == nil && line != "ping\n" {
			readErr = fmt.Errorf("probe server read %q", line)
		}
		if readErr == nil {
			_, readErr = io.WriteString(conn, "pong\n")
		}
		done <- readErr
	}()
	return done
}

func runPureGoSocketProbe(t *testing.T, port int) []byte {
	t.Helper()
	server := startSocketProbeServer(t, port)
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Eval(context.Background(), "socket-probe.sl", sleepSocketProbeSource(port)); err != nil {
		t.Fatalf("pure-Go socket probe: %v\n%s", err, output.String())
	}
	if serverErr := <-server; serverErr != nil {
		t.Fatal(serverErr)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func runPureGoSocketErrorProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Eval(context.Background(), "socket-error-probe.sl", sleepSocketErrorProbeSource); err != nil {
		t.Fatalf("pure-Go socket error-order probe: %v\n%s", err, output.String())
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func sleepSocketProbeSource(port int) string {
	return "$h = connect('127.0.0.1', " + strconv.Itoa(port) + ", 2000);\n" +
		"if (-eof $h) { println('open:0'); } else { println('open:1'); }\n" +
		"println('available:' . available($h));\n" +
		"println($h, 'ping');\n" +
		"println('reply:' . readln($h));\n" +
		"closef($h);\n"
}

const sleepSocketErrorProbeSource = "$h = listen(-1, 1, $peer, laddr => '[::1', laddr => '127.0.0.1');\n" +
	"checkError($problem);\n" +
	"println($problem);\n"

type socketNumericObject string

func (value socketNumericObject) String() string { return string(value) }
