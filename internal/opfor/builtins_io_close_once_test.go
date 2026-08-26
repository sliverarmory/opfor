package opfor

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCanceledOwnedReadClosesUnderlyingTransportOnce(t *testing.T) {
	t.Parallel()

	transport := newCloseCountingBlockingReader()
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	handle := newIOHandle("close-once-reader", transport, nil, true, false, false).
		withRuntimeOutputAccount(runtimeInstance.resources)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, readErr := callIOBuiltin(
			ctx,
			runtimeInstance,
			runtimeInstance.ioFunctions(),
			"readb",
			ObjectValue(handle),
			Int(1),
		)
		result <- readErr
	}()

	select {
	case <-transport.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("read did not reach the owned transport")
	}

	// cancelReadOnContext, abortRead, and ordinary lifecycle close all converge
	// here. Exercise them concurrently: the first Close must still wake Read
	// promptly, while every later call must stop at the handle's coordinator.
	cancel()
	var closes sync.WaitGroup
	for index := 0; index < 16; index++ {
		closes.Add(1)
		go func(index int) {
			defer closes.Done()
			if index%2 == 0 {
				handle.abortOwnedTransport()
				return
			}
			_ = handle.close()
		}(index)
	}

	select {
	case readErr := <-result:
		if !errors.Is(readErr, context.Canceled) {
			t.Fatalf("canceled read error = %v, want context.Canceled", readErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled read remained blocked")
	}
	closes.Wait()
	if calls := transport.closeCalls.Load(); calls != 1 {
		t.Fatalf("underlying Close calls = %d, want exactly 1", calls)
	}
}

func TestOwnedDuplexHandleSharesCloseCoordinator(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("underlying close failure")
	transport := &closeCountingDuplex{closeErr: wantErr}
	handle := newIOHandle("close-once-duplex", transport, transport, true, true, false)
	readCloser, writeCloser := handle.readCloser, handle.writeClose
	if !sameIOCloser(readCloser, writeCloser) {
		t.Fatal("owned duplex sides did not share one close coordinator")
	}

	var closes sync.WaitGroup
	results := make(chan error, 32)
	for index := 0; index < 32; index++ {
		closes.Add(1)
		go func(index int) {
			defer closes.Done()
			if index%2 == 0 {
				results <- readCloser.Close()
				return
			}
			results <- writeCloser.Close()
		}(index)
	}
	closes.Add(1)
	go func() {
		defer closes.Done()
		handle.abortOwnedTransport()
	}()
	closes.Wait()
	close(results)

	for closeErr := range results {
		if closeErr != wantErr {
			t.Fatalf("repeated coordinated Close error = %v, want exact cached %v", closeErr, wantErr)
		}
	}
	closeErr := handle.close()
	if !errors.Is(closeErr, wantErr) || closeErr.Error() != wantErr.Error() {
		t.Fatalf("shared duplex logical close error = %q, want exactly %q", closeErr, wantErr)
	}
	if calls := transport.closeCalls.Load(); calls != 1 {
		t.Fatalf("duplex underlying Close calls = %d, want exactly 1", calls)
	}
}

func TestSocketAttachSharesOwnedCloseCoordinator(t *testing.T) {
	t.Parallel()

	local, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	connection := &closeCountingConn{Conn: local}
	ctx, cancel := context.WithCancel(context.Background())
	task := &sleepSocketTask{
		handle: newIOHandle("socket-close-once", nil, nil, true, true, false),
		ctx:    ctx,
		cancel: cancel,
	}
	if !task.adopt(connection) {
		t.Fatal("socket task rejected its connection")
	}
	if !task.attach(connection) {
		t.Fatal("socket task rejected its adopted connection")
	}

	readCloser, writeCloser := task.handle.readCloser, task.handle.writeClose
	if !sameIOCloser(readCloser, writeCloser) {
		t.Fatal("attached socket sides did not share one close coordinator")
	}
	if _, ok := readCloser.(*sleepOnceCloser); !ok {
		t.Fatalf("attached socket closer = %T, want *sleepOnceCloser", readCloser)
	}
	if readCloser != task.connCloser {
		t.Fatal("attached socket did not retain its adoption-time coordinator")
	}

	var closes sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < 32; index++ {
		closes.Add(1)
		go func(index int) {
			defer closes.Done()
			<-start
			switch index % 3 {
			case 0:
				task.closeHandle()
			case 1:
				task.cancelAndClose()
			default:
				_ = task.handle.close()
			}
		}(index)
	}
	close(start)
	closes.Wait()
	if err := task.handle.close(); err != nil {
		t.Fatalf("close attached socket handle: %v", err)
	}
	if calls := connection.closeCalls.Load(); calls != 1 {
		t.Fatalf("socket connection Close calls = %d, want exactly 1", calls)
	}
}

func TestSocketAdoptionCoordinatesPreAttachLifecycleClose(t *testing.T) {
	t.Parallel()

	local, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	connection := &closeCountingConn{Conn: local}
	ctx, cancel := context.WithCancel(context.Background())
	task := &sleepSocketTask{
		handle: newIOHandle("socket-pre-attach-close-once", nil, nil, true, true, false),
		ctx:    ctx,
		cancel: cancel,
	}
	if !task.adopt(connection) {
		t.Fatal("socket task rejected its connection")
	}
	if task.connCloser == nil {
		t.Fatal("socket adoption did not install a task-owned close coordinator")
	}

	var closes sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < 32; index++ {
		closes.Add(1)
		go func(index int) {
			defer closes.Done()
			<-start
			if index%2 == 0 {
				task.closeHandle()
				return
			}
			task.cancelAndClose()
		}(index)
	}
	close(start)
	closes.Wait()

	if calls := connection.closeCalls.Load(); calls != 1 {
		t.Fatalf("pre-attach socket connection Close calls = %d, want exactly 1", calls)
	}
	task.mu.Lock()
	conn, attached := task.conn, task.attached
	task.mu.Unlock()
	if conn != nil || attached {
		t.Fatalf("pre-attach socket remained live: conn=%v attached=%v", conn != nil, attached)
	}
}

type closeCountingBlockingReader struct {
	entered chan struct{}
	closed  chan struct{}

	enterOnce  sync.Once
	closedOnce sync.Once
	closeCalls atomic.Int32
}

func newCloseCountingBlockingReader() *closeCountingBlockingReader {
	return &closeCountingBlockingReader{
		entered: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (reader *closeCountingBlockingReader) Read([]byte) (int, error) {
	reader.enterOnce.Do(func() { close(reader.entered) })
	<-reader.closed
	return 0, io.ErrClosedPipe
}

func (reader *closeCountingBlockingReader) Close() error {
	reader.closeCalls.Add(1)
	reader.closedOnce.Do(func() { close(reader.closed) })
	return nil
}

type closeCountingDuplex struct {
	closeCalls atomic.Int32
	closeErr   error
}

type closeCountingConn struct {
	net.Conn
	closeCalls atomic.Int32
}

func (connection *closeCountingConn) Close() error {
	connection.closeCalls.Add(1)
	return connection.Conn.Close()
}

func (*closeCountingDuplex) Read([]byte) (int, error) { return 0, io.EOF }

func (*closeCountingDuplex) Write(data []byte) (int, error) { return len(data), nil }

func (transport *closeCountingDuplex) Close() error {
	transport.closeCalls.Add(1)
	return transport.closeErr
}
