package opfor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestInputLimitIsFatalAndPreservesBinaryProvenance(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New(WithLimits(Limits{MaxInputBytesPerRuntime: 3}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	functions := runtimeInstance.ioFunctions()
	handle := ObjectValue(newIOHandle("input-limit", bytes.NewReader([]byte{0x00, 0x80, 0xff, 0x41}), nil, true, false, false).
		withRuntimeOutputAccount(runtimeInstance.resources))

	_, err = callIOBuiltin(context.Background(), runtimeInstance, functions, "readb", handle, Int(-1))
	assertInputLimitError(t, err, 3)
	if got := runtimeInstance.resources.used(resourceInputBytes); got != 0 {
		t.Fatalf("failed oversized admission charged %d input bytes, want 0", got)
	}

	exactRuntime, err := New(WithLimits(Limits{MaxInputBytesPerRuntime: 4}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exactRuntime.Close(context.Background()) })
	exactHandle := ObjectValue(newIOHandle("input-exact", bytes.NewReader([]byte{0x00, 0x80, 0xff, 0x41}), nil, true, false, false).
		withRuntimeOutputAccount(exactRuntime.resources))
	value, err := callIOBuiltin(context.Background(), exactRuntime, exactRuntime.ioFunctions(), "readb", exactHandle, Int(-1))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := value.Bytes()
	if !ok || !bytes.Equal(got, []byte{0x00, 0x80, 0xff, 0x41}) || !value.IsBinaryString() {
		t.Fatalf("readb = %x/binary=%v, want exact raw-byte provenance", got, value.IsBinaryString())
	}
	if used := exactRuntime.resources.used(resourceInputBytes); used != 4 {
		t.Fatalf("exact admission charged %d input bytes, want 4", used)
	}
}

func TestInputLimitChargesTextReadAheadAndReplay(t *testing.T) {
	t.Parallel()

	textRuntime, err := New(WithLimits(Limits{MaxInputBytesPerRuntime: 4}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textRuntime.Close(context.Background()) })
	textHandle := newIOHandle("utf16-input", bytes.NewReader([]byte{0x00, 'A', 0x00, '\n'}), nil, true, false, false).
		withRuntimeOutputAccount(textRuntime.resources)
	if err := textHandle.setTextEncoding("UTF-16BE"); err != nil {
		t.Fatal(err)
	}
	line, present, err := textHandle.readLineContext(context.Background())
	if err != nil || !present || line.String() != "A" || line.IsBinaryString() {
		t.Fatalf("UTF-16 line = %s/present=%v/binary=%v/error=%v", line.Describe(), present, line.IsBinaryString(), err)
	}
	if used := textRuntime.resources.used(resourceInputBytes); used != 4 {
		t.Fatalf("text read-ahead charged %d bytes, want the 4 raw input bytes", used)
	}

	replayRuntime, err := New(WithLimits(Limits{MaxInputBytesPerRuntime: 3}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replayRuntime.Close(context.Background()) })
	replay := newIOHandle("replay-input", bytes.NewReader([]byte("ab")), nil, true, false, false).
		withRuntimeOutputAccount(replayRuntime.resources)
	if err := replay.markInput(2); err != nil {
		t.Fatal(err)
	}
	if value, err := replay.readBytesContext(context.Background(), 2); err != nil || string(value) != "ab" {
		t.Fatalf("initial read = %q, %v", value, err)
	}
	if err := replay.resetInput(); err != nil {
		t.Fatal(err)
	}
	if _, err := replay.readBytesContext(context.Background(), 2); err == nil {
		t.Fatal("replayed bytes bypassed the monotonic input limit")
	} else {
		assertInputLimitError(t, err, 3)
	}
}

func TestInputLimitEscapesSleepIOSoftErrorBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		limit  uint64
		invoke func(context.Context, *Runtime, map[string]NativeFunc, Value) error
	}{
		{
			name:  "readc",
			limit: 1,
			invoke: func(ctx context.Context, runtimeInstance *Runtime, functions map[string]NativeFunc, handle Value) error {
				_, err := callIOBuiltin(ctx, runtimeInstance, functions, "readc", handle)
				return err
			},
		},
		{
			name:  "consume",
			limit: 1,
			invoke: func(ctx context.Context, runtimeInstance *Runtime, functions map[string]NativeFunc, handle Value) error {
				_, err := callIOBuiltin(ctx, runtimeInstance, functions, "consume", handle, Int(2), Int(1))
				return err
			},
		},
		{
			name:  "available delimiter",
			limit: 1,
			invoke: func(ctx context.Context, runtimeInstance *Runtime, functions map[string]NativeFunc, handle Value) error {
				_, err := callIOBuiltin(ctx, runtimeInstance, functions, "available", handle, String("z"))
				return err
			},
		},
		{
			name:  "bread",
			limit: 1,
			invoke: func(ctx context.Context, runtimeInstance *Runtime, functions map[string]NativeFunc, handle Value) error {
				_, err := callIOBuiltin(ctx, runtimeInstance, functions, "bread", handle, String("C2"))
				return err
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runtimeInstance, err := New(WithLimits(Limits{MaxInputBytesPerRuntime: test.limit}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			handle := ObjectValue(newIOHandle(test.name, bytes.NewReader([]byte("ab")), nil, true, false, false).
				withRuntimeOutputAccount(runtimeInstance.resources))
			err = test.invoke(context.Background(), runtimeInstance, runtimeInstance.ioFunctions(), handle)
			assertInputLimitError(t, err, test.limit)
		})
	}
}

func TestInputLimitTerminatesScriptExecution(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New(WithLimits(Limits{MaxInputBytesPerRuntime: 1}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("input-limit.sl", `$h = allocate(); writeb($h, "ab"); closef($h); consume($h, 2, 1); return "unreachable";`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtimeInstance.Execute(context.Background(), program)
	assertInputLimitError(t, err, 1)
}

func TestInputLimitCapsRequestedTemporaryIOBuffers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		invoke func(context.Context, *Runtime, map[string]NativeFunc, Value) error
	}{
		{
			name: "readb",
			invoke: func(ctx context.Context, runtimeInstance *Runtime, functions map[string]NativeFunc, handle Value) error {
				_, err := callIOBuiltin(ctx, runtimeInstance, functions, "readb", handle, Int(1<<31-1))
				return err
			},
		},
		{
			name: "consume",
			invoke: func(ctx context.Context, runtimeInstance *Runtime, functions map[string]NativeFunc, handle Value) error {
				_, err := callIOBuiltin(ctx, runtimeInstance, functions, "consume", handle, Int(1<<31-1), Int(1<<31-1))
				return err
			},
		},
		{
			name: "available look-ahead",
			invoke: func(ctx context.Context, runtimeInstance *Runtime, functions map[string]NativeFunc, handle Value) error {
				_, err := callIOBuiltin(ctx, runtimeInstance, functions, "available", handle, String("needle"))
				return err
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runtimeInstance, err := New(WithLimits(Limits{MaxInputBytesPerRuntime: 1}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			reader := &sizedEOFReader{length: 1 << 30}
			handle := ObjectValue(newIOHandle(test.name, reader, nil, true, false, false).
				withRuntimeOutputAccount(runtimeInstance.resources))
			if err := test.invoke(context.Background(), runtimeInstance, runtimeInstance.ioFunctions(), handle); err != nil {
				t.Fatalf("invoke: %v", err)
			}
			if reader.maximumRequest > sleepIOReadBufferSize {
				t.Fatalf("largest underlying read request = %d, want <= %d", reader.maximumRequest, sleepIOReadBufferSize)
			}
		})
	}
}

func TestCanceledOwnedReadClosesTransport(t *testing.T) {
	t.Parallel()

	reader := newBlockingReadCloser()
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	handle := ObjectValue(newIOHandle("blocking", reader, nil, true, false, false).
		withRuntimeOutputAccount(runtimeInstance.resources))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, readErr := callIOBuiltin(ctx, runtimeInstance, runtimeInstance.ioFunctions(), "readb", handle, Int(1))
		result <- readErr
	}()

	select {
	case <-reader.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("read did not reach the owned transport")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled read error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled read remained blocked")
	}
	select {
	case <-reader.closed:
	default:
		t.Fatal("canceled owned read did not close its transport")
	}
}

func TestInputLimitAbortsTransportBeforeWaitingForBlockedDuplexWrite(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New(WithLimits(Limits{MaxInputBytesPerRuntime: 1}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	local, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	transport := &writeSignalingConn{Conn: local, started: make(chan struct{})}
	handle := newIOHandle("blocked-duplex", transport, transport, true, true, false).
		withRuntimeOutputAccount(runtimeInstance.resources)

	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := handle.Write([]byte("blocked until the peer reads or the transport closes"))
		writeDone <- writeErr
	}()
	select {
	case <-transport.started:
	case <-time.After(2 * time.Second):
		t.Fatal("duplex write did not enter the transport")
	}

	peerWriteDone := make(chan error, 1)
	go func() {
		_, writeErr := peer.Write([]byte("ab"))
		peerWriteDone <- writeErr
	}()
	readDone := make(chan error, 1)
	go func() {
		_, readErr := handle.readBytesContext(context.Background(), 2)
		readDone <- readErr
	}()

	select {
	case readErr := <-readDone:
		assertInputLimitError(t, readErr, 1)
	case <-time.After(2 * time.Second):
		t.Fatal("quota-failing read deadlocked behind the blocked duplex writer")
	}
	select {
	case writeErr := <-writeDone:
		if writeErr == nil {
			t.Fatal("blocked duplex write unexpectedly succeeded without a peer read")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal input abort did not wake the blocked duplex writer")
	}
	select {
	case <-peerWriteDone:
	case <-time.After(2 * time.Second):
		t.Fatal("peer write remained blocked after the terminal input abort")
	}
}

func TestInputLimitDoesNotCloseOrFlushBorrowedPersistentConsole(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New(WithLimits(Limits{MaxInputBytesPerRuntime: 1}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	input := &closeTrackingReader{Reader: bytes.NewReader([]byte("ab"))}
	output := &closeFlushTrackingWriter{}
	handle := newIOHandle("borrowed-console", input, output, false, false, true).
		withRuntimeOutputAccount(runtimeInstance.resources)
	_, err = handle.readBytesContext(context.Background(), 2)
	assertInputLimitError(t, err, 1)
	if input.closeCalls != 0 || output.closeCalls != 0 || output.flushCalls != 0 {
		t.Fatalf("borrowed console close/flush calls = input:%d output:%d flush:%d, want all zero",
			input.closeCalls, output.closeCalls, output.flushCalls)
	}
	handle.mu.Lock()
	readerOpen, writerOpen := handle.reader != nil, handle.writer != nil
	handle.mu.Unlock()
	if !readerOpen || !writerOpen {
		t.Fatalf("borrowed console logical streams changed: reader=%v writer=%v", readerOpen, writerOpen)
	}
}

func TestCanceledProcessReadDestroysAndReapsChild(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a helper process")
	}

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	functions := runtimeInstance.ioFunctions()
	value, err := callIOBuiltin(context.Background(), runtimeInstance, functions, "exec", processHelperCommand("block"))
	if err != nil {
		t.Fatal(err)
	}
	handle, ok := ioHandleValue(value)
	if !ok {
		t.Fatalf("exec = %s, want process handle", value.Describe())
	}
	process := handle.getProcess()
	if process == nil {
		t.Fatal("process handle has no managed process")
	}
	if line, present, readErr := handle.readLineContext(context.Background()); readErr != nil || !present || line.String() != "ready" {
		t.Fatalf("process readiness = %s/present=%v/error=%v", line.Describe(), present, readErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, readErr := callIOBuiltin(ctx, runtimeInstance, functions, "readb", value, Int(1))
		result <- readErr
	}()
	time.AfterFunc(25*time.Millisecond, cancel)
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled process read error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled process read remained blocked")
	}
	joinContext, cancelJoin := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelJoin()
	if err := process.join(joinContext); err != nil {
		t.Fatalf("canceled process was not reaped: %v", err)
	}
}

type blockingReadCloser struct {
	entered   chan struct{}
	closed    chan struct{}
	enterOnce sync.Once
	closeOnce sync.Once
}

type sizedEOFReader struct {
	length         int
	maximumRequest int
}

type writeSignalingConn struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (connection *writeSignalingConn) Write(data []byte) (int, error) {
	connection.once.Do(func() { close(connection.started) })
	return connection.Conn.Write(data)
}

type closeTrackingReader struct {
	*bytes.Reader
	closeCalls int
}

func (reader *closeTrackingReader) Close() error {
	reader.closeCalls++
	return nil
}

type closeFlushTrackingWriter struct {
	closeCalls int
	flushCalls int
}

func (*closeFlushTrackingWriter) Write(data []byte) (int, error) { return len(data), nil }

func (writer *closeFlushTrackingWriter) Close() error {
	writer.closeCalls++
	return nil
}

func (writer *closeFlushTrackingWriter) Flush() error {
	writer.flushCalls++
	return nil
}

func (reader *sizedEOFReader) Len() int { return reader.length }

func (reader *sizedEOFReader) Read(data []byte) (int, error) {
	if len(data) > reader.maximumRequest {
		reader.maximumRequest = len(data)
	}
	return 0, io.EOF
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{entered: make(chan struct{}), closed: make(chan struct{})}
}

func (reader *blockingReadCloser) Read(_ []byte) (int, error) {
	reader.enterOnce.Do(func() { close(reader.entered) })
	<-reader.closed
	return 0, io.ErrClosedPipe
}

func (reader *blockingReadCloser) Close() error {
	reader.closeOnce.Do(func() { close(reader.closed) })
	return nil
}

func assertInputLimitError(t *testing.T, err error, limit uint64) {
	t.Helper()
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("error = %v, want ErrResourceLimit", err)
	}
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || limitErr.Resource != LimitResourceInputBytes || limitErr.Limit != limit {
		t.Fatalf("limit error = %#v, want input bytes/%d", limitErr, limit)
	}
}
