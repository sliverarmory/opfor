package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingAggressorBOFExtractor struct {
	mu       sync.Mutex
	requests []AggressorBOFExtractionRequest
	extract  func(context.Context, AggressorBOFExtractionRequest) ([]byte, error)
}

func (extractor *recordingAggressorBOFExtractor) ExtractAggressorBOF(
	ctx context.Context,
	request AggressorBOFExtractionRequest,
) ([]byte, error) {
	extractor.mu.Lock()
	captured := request
	captured.Data = append([]byte(nil), request.Data...)
	extractor.requests = append(extractor.requests, captured)
	extract := extractor.extract
	extractor.mu.Unlock()
	if extract == nil {
		return nil, nil
	}
	return extract(ctx, request)
}

func (extractor *recordingAggressorBOFExtractor) snapshot() []AggressorBOFExtractionRequest {
	extractor.mu.Lock()
	defer extractor.mu.Unlock()
	result := make([]AggressorBOFExtractionRequest, len(extractor.requests))
	for index, request := range extractor.requests {
		result[index] = request
		result[index].Data = append([]byte(nil), request.Data...)
	}
	return result
}

func TestAggressorBOFExtractionFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorBOFExtractionFunctions()
	if len(functions) != 1 || functions["bof_extract"] == nil {
		t.Fatalf("Aggressor BOF extraction functions = %#v, want bof_extract", functions)
	}
	names := DefaultFunctionNames()
	index := sort.SearchStrings(names, "bof_extract")
	if index == len(names) || names[index] != "bof_extract" {
		t.Fatalf("DefaultFunctionNames does not contain bof_extract: %q", names)
	}
	if AggressorBOFDefaultEntryPoint != "sleep_mask" {
		t.Fatalf("default BOF entry point = %q, want sleep_mask", AggressorBOFDefaultEntryPoint)
	}
}

func TestAggressorBOFExtractorResolvedRequestAndCopiedResult(t *testing.T) {
	t.Parallel()

	dataCell := NewCell(BinaryString([]byte{0x00, 0x80, 0xff, 'A'}))
	entryCell := NewCell(String("custom_entry"))
	provided := []byte{0xde, 0xad, 0x00, 0xff}
	extractor := &recordingAggressorBOFExtractor{
		extract: func(_ context.Context, request AggressorBOFExtractionRequest) ([]byte, error) {
			// The request owns a detached copy, so mutation here must not change the
			// caller's scalar.
			request.Data[0] = 0xee
			return provided, nil
		},
	}
	runtimeInstance, err := New(WithAggressorBOFExtractor(extractor))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	span := Span{Source: "custom-extractor.cna"}
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  17,
		Name:    "bof_extract",
		Arguments: []Argument{
			{Reference: dataCell},
			{Reference: entryCell},
		},
		Span: span,
	}
	result, err := runtimeInstance.aggressorBOFExtract(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.Bytes()
	if !ok || !result.IsBinaryString() || !bytes.Equal(got, provided) {
		t.Fatalf("result = %x/binary=%v, want %x/binary", got, result.IsBinaryString(), provided)
	}
	provided[0] = 0x11
	got, _ = result.Bytes()
	if got[0] != 0xde {
		t.Fatalf("extractor mutated completed result through returned storage: %x", got)
	}

	requests := extractor.snapshot()
	if len(requests) != 1 {
		t.Fatalf("extractor requests = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.Name != "bof_extract" || request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 ||
		request.Script != 17 || request.Span != span || request.EntryPoint != "custom_entry" ||
		!bytes.Equal(request.Data, []byte{0x00, 0x80, 0xff, 'A'}) {
		t.Fatalf("resolved request = %#v", request)
	}
	cellBytes, ok := dataCell.Get().Bytes()
	if !ok || !dataCell.Get().IsBinaryString() || !bytes.Equal(cellBytes, []byte{0x00, 0x80, 0xff, 'A'}) {
		t.Fatalf("source data after request mutation = %x/binary=%v", cellBytes, dataCell.Get().IsBinaryString())
	}
	if entryCell.Get().String() != "custom_entry" {
		t.Fatalf("source entry point = %q, want custom_entry", entryCell.Get().String())
	}
}

func TestAggressorBOFExtractorDefaultEntryAndZeroLengthSuccess(t *testing.T) {
	t.Parallel()

	extractor := &recordingAggressorBOFExtractor{
		extract: func(_ context.Context, request AggressorBOFExtractionRequest) ([]byte, error) {
			switch request.EntryPoint {
			case AggressorBOFDefaultEntryPoint:
				return nil, nil
			case "":
				return []byte("empty-entry"), nil
			default:
				return nil, fmt.Errorf("unexpected entry point %q", request.EntryPoint)
			}
		},
	}
	runtimeInstance, err := New(WithAggressorBOFExtractor(extractor))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Invoke(context.Background(), "bof_extract", BinaryString(nil))
	if err != nil {
		t.Fatal(err)
	}
	data, ok := result.Bytes()
	if !ok || result.Kind() != KindString || result.IsNull() || len(data) != 0 {
		t.Fatalf("zero-length success = %s/%x, want a non-null empty string", result.Describe(), data)
	}
	emptyEntryResult, err := runtimeInstance.Invoke(
		context.Background(), "bof_extract", String("object"), String(""),
	)
	if err != nil || emptyEntryResult.String() != "empty-entry" || !emptyEntryResult.IsBinaryString() {
		t.Fatalf("explicit empty entry result = %s/binary=%v/error:%v",
			emptyEntryResult.Describe(), emptyEntryResult.IsBinaryString(), err)
	}
	requests := extractor.snapshot()
	if len(requests) != 2 || requests[0].EntryPoint != "sleep_mask" || len(requests[0].Data) != 0 ||
		requests[1].EntryPoint != "" || !bytes.Equal(requests[1].Data, []byte("object")) {
		t.Fatalf("default request = %#v", requests)
	}
}

func TestAggressorBOFExtractorValidationAndNilPolicy(t *testing.T) {
	t.Parallel()

	var extractorCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed extraction reached Host")
		})),
		WithAggressorBOFExtractor(AggressorBOFExtractorFunc(func(context.Context, AggressorBOFExtractionRequest) ([]byte, error) {
			extractorCalls.Add(1)
			return []byte("ok"), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, arguments := range [][]Value{
		nil,
		{String("a"), String("b"), String("c")},
	} {
		_, err := runtimeInstance.Invoke(context.Background(), "bof_extract", arguments...)
		if err == nil || !strings.Contains(err.Error(), "expected 1 or 2 arguments") {
			t.Errorf("arity %d error = %v", len(arguments), err)
		}
	}
	for _, test := range []struct {
		arguments []Value
		position  int
	}{
		{arguments: []Value{Int(7)}, position: 1},
		{arguments: []Value{String("object"), Null()}, position: 2},
		{arguments: []Value{String("object"), ArrayValue(NewArray())}, position: 2},
	} {
		_, err := runtimeInstance.Invoke(context.Background(), "bof_extract", test.arguments...)
		want := fmt.Sprintf("argument %d", test.position)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "must be a string") {
			t.Errorf("type validation error = %v, want %s string error", err, want)
		}
	}
	if extractorCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid requests called extractor/Host %d/%d time(s)", extractorCalls.Load(), hostCalls.Load())
	}

	var typedNil *recordingAggressorBOFExtractor
	if _, err := New(WithAggressorBOFExtractor(typedNil)); err == nil {
		t.Fatal("typed-nil Aggressor BOF extractor was accepted")
	}
	var nilFunction AggressorBOFExtractorFunc
	if _, err := New(WithAggressorBOFExtractor(nilFunction)); err == nil {
		t.Fatal("nil Aggressor BOF extractor function was accepted")
	}
	if _, err := nilFunction.ExtractAggressorBOF(context.Background(), AggressorBOFExtractionRequest{}); err == nil {
		t.Fatal("direct nil Aggressor BOF extractor function call succeeded")
	}
}

func TestAggressorBOFExtractorHostFallbackPreservesInvocation(t *testing.T) {
	t.Parallel()

	dataCell := NewCell(BinaryString([]byte{0x00, 0xff, 'O'}))
	entryCell := NewCell(String("go"))
	wantErr := errors.New("Host extraction failed after producing a result")
	wantResult := BinaryString([]byte{0xca, 0xfe})
	var received Invocation
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		received = invocation
		if len(invocation.Arguments) == 2 {
			invocation.Arguments[1].Set(String("mutated-by-host"))
		}
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	span := Span{Source: "host-fallback.cna"}
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  23,
		Name:    "bof_extract",
		Arguments: []Argument{
			{Name: "$data", Reference: dataCell},
			{Name: "$entry", Reference: entryCell},
		},
		Span: span,
	}
	result, err := runtimeInstance.aggressorBOFExtract(context.Background(), invocation)
	if !errors.Is(err, wantErr) || !result.IdentityEqual(wantResult) {
		t.Fatalf("Host fallback = (%s, %v), want exact result/%v", result.Describe(), err, wantErr)
	}
	if received.Runtime != invocation.Runtime || received.Script != invocation.Script || received.Name != invocation.Name || received.Span != invocation.Span ||
		len(received.Arguments) != 2 || received.Arguments[0].Reference != dataCell || received.Arguments[1].Reference != entryCell ||
		received.Arguments[0].Name != "$data" || received.Arguments[1].Name != "$entry" {
		t.Fatalf("Host received changed invocation: %#v", received)
	}
	if entryCell.Get().String() != "mutated-by-host" || hostCalls.Load() != 1 {
		t.Fatalf("Host reference mutation/calls = %q/%d", entryCell.Get().String(), hostCalls.Load())
	}

	// Without the typed extractor, input interpretation remains entirely Host
	// owned. OPFOR applies only the documented wrapper arity before forwarding.
	if _, err := runtimeInstance.Invoke(context.Background(), "bof_extract", Int(9)); !errors.Is(err, wantErr) {
		t.Fatalf("Host-owned non-string call error = %v, want %v", err, wantErr)
	}
	if hostCalls.Load() != 2 || received.Arg(0).Kind() != KindInt {
		t.Fatalf("Host-owned input = %s/calls:%d, want int/two", received.Arg(0).Describe(), hostCalls.Load())
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "bof_extract"); err == nil || hostCalls.Load() != 2 {
		t.Fatalf("invalid arity error/Host calls = %v/%d", err, hostCalls.Load())
	}
}

func TestAggressorBOFExtractorErrorsCancellationAndBoundaryAuthority(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("extractor rejected object")
	var calls atomic.Int32
	var cancelDuring context.CancelFunc
	extractor := AggressorBOFExtractorFunc(func(_ context.Context, request AggressorBOFExtractionRequest) ([]byte, error) {
		calls.Add(1)
		switch request.EntryPoint {
		case "error":
			return []byte("discarded"), wantErr
		case "cancel":
			cancelDuring()
			return []byte("late"), nil
		case "boundary":
			return []byte("discarded"), ErrUnsafeArrayView
		default:
			return request.Data, nil
		}
	})
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorBOFExtractor(extractor),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Invoke(context.Background(), "bof_extract", String("object"), String("error"))
	if !errors.Is(err, wantErr) || !result.IsNull() || calls.Load() != 1 || hostCalls.Load() != 0 {
		t.Fatalf("extractor error = (%s, %v), calls extractor/Host %d/%d",
			result.Describe(), err, calls.Load(), hostCalls.Load())
	}

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtimeInstance.Invoke(preCanceled, "bof_extract", String("object")); !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("pre-canceled call error/calls = %v/%d", err, calls.Load())
	}
	during, cancel := context.WithCancel(context.Background())
	cancelDuring = cancel
	result, err = runtimeInstance.Invoke(during, "bof_extract", String("object"), String("cancel"))
	if !errors.Is(err, context.Canceled) || !result.IsNull() || calls.Load() != 2 {
		t.Fatalf("cancel-during result/error/calls = %s/%v/%d", result.Describe(), err, calls.Load())
	}

	result, err = runtimeInstance.Invoke(context.Background(), "bof_extract", String("object"), String("boundary"))
	if !errors.Is(err, ErrUnsafeArrayView) || !result.IsNull() {
		t.Fatalf("boundary result/error = %s/%v", result.Describe(), err)
	}
	_, err = runtimeInstance.Eval(context.Background(), "bof-boundary.cna", `bof_extract("object", "boundary");`)
	if !errors.Is(err, ErrUnsafeArrayView) {
		t.Fatalf("script boundary error = %v, want ErrUnsafeArrayView", err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("authoritative extractor errors reached Host %d time(s)", hostCalls.Load())
	}
}

func TestAggressorBOFExtractorWithFunctionPrecedence(t *testing.T) {
	for _, overrideFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("override-first=%v", overrideFirst), func(t *testing.T) {
			var extractorCalls atomic.Int32
			var hostCalls atomic.Int32
			hostOption := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
				hostCalls.Add(1)
				return Null(), nil
			}))
			extractorOption := WithAggressorBOFExtractor(AggressorBOFExtractorFunc(func(context.Context, AggressorBOFExtractionRequest) ([]byte, error) {
				extractorCalls.Add(1)
				return []byte("extractor"), nil
			}))
			overrideOption := WithFunction("bof_extract", func(context.Context, Invocation) (Value, error) {
				return String("override"), nil
			})
			options := []Option{hostOption, extractorOption, overrideOption}
			if overrideFirst {
				options = []Option{hostOption, overrideOption, extractorOption}
			}
			runtimeInstance, err := New(options...)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			// Invalid stock-wrapper arity proves lookup selected the importer
			// override before any native validation.
			result, err := runtimeInstance.Invoke(context.Background(), "bof_extract")
			if err != nil || result.String() != "override" || extractorCalls.Load() != 0 || hostCalls.Load() != 0 {
				t.Fatalf("override = (%s, %v), extractor/Host %d/%d",
					result.Describe(), err, extractorCalls.Load(), hostCalls.Load())
			}
		})
	}
}

func TestPortableScriptLoaderInheritsAggressorBOFExtractor(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-bof-extract.cna")
	if err := os.WriteFile(childPath, []byte(`return bof_extract("child-object", "child_entry");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-bof-extract.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
$parent = bof_extract("parent-object");
return @($parent, [$child runScript]);
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}

	extractor := &recordingAggressorBOFExtractor{
		extract: func(_ context.Context, request AggressorBOFExtractionRequest) ([]byte, error) {
			result := append([]byte(request.EntryPoint+":"), request.Data...)
			return result, nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("inherited BOF extraction reached Host")
		})),
		WithAggressorBOFExtractor(extractor),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	values := mustArrayValues(t, result)
	if len(values) != 2 || values[0].String() != "sleep_mask:parent-object" ||
		values[1].String() != "child_entry:child-object" || !values[0].IsBinaryString() || !values[1].IsBinaryString() {
		t.Fatalf("parent/child results = %#v", values)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("inherited extractor requests reached Host %d time(s)", hostCalls.Load())
	}
	requests := extractor.snapshot()
	if len(requests) != 2 || requests[0].RuntimeID != runtimeInstance.ID() || requests[0].RuntimeID == 0 ||
		requests[1].RuntimeID == 0 || requests[1].RuntimeID == requests[0].RuntimeID ||
		requests[0].Script != 1 || requests[1].Script != 1 ||
		requests[0].Span.Source != "parent-bof-extract.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child request provenance = %#v", requests)
	}
}

func TestAggressorBOFExtractorSupportsConcurrentRequests(t *testing.T) {
	t.Parallel()

	const concurrentCalls = 24
	entered := make(chan struct{}, concurrentCalls)
	release := make(chan struct{})
	extractor := &recordingAggressorBOFExtractor{
		extract: func(_ context.Context, request AggressorBOFExtractionRequest) ([]byte, error) {
			entered <- struct{}{}
			<-release
			return append([]byte(request.EntryPoint+":"), request.Data...), nil
		},
	}
	runtimeInstance, err := New(WithAggressorBOFExtractor(extractor))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	var wait sync.WaitGroup
	errorsByCall := make(chan error, concurrentCalls)
	for index := 0; index < concurrentCalls; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			data := fmt.Sprintf("object-%d", index)
			entry := fmt.Sprintf("entry-%d", index)
			result, invokeErr := runtimeInstance.Invoke(
				context.Background(), "bof_extract", String(data), String(entry),
			)
			if invokeErr == nil {
				want := entry + ":" + data
				if result.String() != want || !result.IsBinaryString() {
					invokeErr = fmt.Errorf("result = %s/binary=%v, want %q/binary",
						result.Describe(), result.IsBinaryString(), want)
				}
			}
			errorsByCall <- invokeErr
		}()
	}
	for index := 0; index < concurrentCalls; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d/%d extractor calls entered concurrently", index, concurrentCalls)
		}
	}
	close(release)
	wait.Wait()
	close(errorsByCall)
	for invokeErr := range errorsByCall {
		if invokeErr != nil {
			t.Error(invokeErr)
		}
	}
	if requests := extractor.snapshot(); len(requests) != concurrentCalls {
		t.Fatalf("concurrent requests = %d, want %d", len(requests), concurrentCalls)
	}
}

func TestAggressorBOFExtractorRuntimeCloseCancelsProvider(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	var once sync.Once
	runtimeInstance, err := New(WithAggressorBOFExtractor(AggressorBOFExtractorFunc(func(
		ctx context.Context,
		_ AggressorBOFExtractionRequest,
	) ([]byte, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return nil, ctx.Err()
	})))
	if err != nil {
		t.Fatal(err)
	}
	invokeDone := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.Invoke(context.Background(), "bof_extract", String("object"))
		invokeDone <- invokeErr
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("extractor did not start")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtimeInstance.Close(context.Background()) }()
	select {
	case err := <-invokeDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("provider call after Close error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runtime close did not cancel extractor")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Runtime.Close did not finish")
	}
}
