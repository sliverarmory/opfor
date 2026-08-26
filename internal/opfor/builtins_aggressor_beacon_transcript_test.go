package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type recordingAggressorBeaconTranscriptSink struct {
	mu      sync.Mutex
	records []AggressorBeaconTranscriptRecord
	err     error
	publish func(context.Context, AggressorBeaconTranscriptRecord) error
}

func (sink *recordingAggressorBeaconTranscriptSink) PublishAggressorBeaconTranscript(
	ctx context.Context,
	record AggressorBeaconTranscriptRecord,
) error {
	sink.mu.Lock()
	sink.records = append(sink.records, record)
	publish := sink.publish
	err := sink.err
	sink.mu.Unlock()
	if publish != nil {
		return publish(ctx, record)
	}
	return err
}

func (sink *recordingAggressorBeaconTranscriptSink) snapshot() []AggressorBeaconTranscriptRecord {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]AggressorBeaconTranscriptRecord(nil), sink.records...)
}

func TestAggressorBeaconTranscriptFunctionsMapRecordsAndReturnNull(t *testing.T) {
	sink := &recordingAggressorBeaconTranscriptSink{}
	var stdout recordingWriter
	runtimeInstance, err := New(
		WithAggressorBeaconTranscriptSink(sink),
		WithStdout(&stdout),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	tests := []struct {
		name string
		kind AggressorBeaconTranscriptKind
		args []Value
	}{
		{"berror", AggressorBeaconTranscriptError, []Value{String("b-error"), String("error")}},
		{"blog", AggressorBeaconTranscriptLog, []Value{String("b-log"), BinaryString([]byte("log"))}},
		{"blog2", AggressorBeaconTranscriptLog2, []Value{String("b-log2"), String("alternate")}},
		{"binput", AggressorBeaconTranscriptInput, []Value{String("b-input"), String("input")}},
		{"btask", AggressorBeaconTranscriptTask, []Value{String("b-task"), String("task"), String(" T1003, T1059 ")}},
		{"btaskcompleted", AggressorBeaconTranscriptTaskCompleted, []Value{String("b-complete"), Long(0x1_0000_0001)}},
		{"bjoblog", AggressorBeaconTranscriptJobLog, []Value{String("b-job-log"), Int(17), String("job output")}},
		{"bjoberror", AggressorBeaconTranscriptJobError, []Value{String("b-job-error"), String("job-18"), String("job error")}},
	}
	for _, test := range tests {
		result, err := runtimeInstance.Invoke(context.Background(), test.name, test.args...)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if !result.IsNull() {
			t.Errorf("%s result = %s, want $null", test.name, result.Describe())
		}
	}

	records := sink.snapshot()
	if len(records) != len(tests) {
		t.Fatalf("sink records = %d, want %d", len(records), len(tests))
	}
	for index, test := range tests {
		record := records[index]
		if record.Kind != test.kind {
			t.Errorf("record %d kind = %q, want %q", index, record.Kind, test.kind)
		}
		if !record.BeaconID.IdentityEqual(test.args[0]) {
			t.Errorf("record %d BeaconID = %s, want identical %s", index, record.BeaconID.Describe(), test.args[0].Describe())
		}
		if record.RuntimeID != runtimeInstance.ID() {
			t.Errorf("record %d RuntimeID = %d, want %d", index, record.RuntimeID, runtimeInstance.ID())
		}
		if record.Script != 0 || record.Span != (Span{}) {
			t.Errorf("record %d direct-invoke provenance = script %d span %s, want zero", index, record.Script, record.Span)
		}
		switch test.kind {
		case AggressorBeaconTranscriptError,
			AggressorBeaconTranscriptLog,
			AggressorBeaconTranscriptLog2,
			AggressorBeaconTranscriptInput:
			if !record.Text.IdentityEqual(test.args[1]) {
				t.Errorf("record %d Text = %s, want identical %s", index, record.Text.Describe(), test.args[1].Describe())
			}
		case AggressorBeaconTranscriptTask:
			if !record.Text.IdentityEqual(test.args[1]) || !record.HasMITREIDs || record.RawMITREIDs != test.args[2].String() {
				t.Errorf("record %d task fields = %s/%v/%q", index, record.Text.Describe(), record.HasMITREIDs, record.RawMITREIDs)
			}
		case AggressorBeaconTranscriptTaskCompleted:
			if !record.TaskID.IdentityEqual(test.args[1]) {
				t.Errorf("record %d TaskID = %s, want identical %s", index, record.TaskID.Describe(), test.args[1].Describe())
			}
		case AggressorBeaconTranscriptJobLog, AggressorBeaconTranscriptJobError:
			if !record.JobID.IdentityEqual(test.args[1]) || !record.Text.IdentityEqual(test.args[2]) {
				t.Errorf("record %d job fields = %s/%s", index, record.JobID.Describe(), record.Text.Describe())
			}
		}
	}
	if writes := stdout.snapshot(); len(writes) != 0 {
		t.Fatalf("configured sink duplicated records to stdout: %q", writes)
	}
}

func TestAggressorBeaconTranscriptBtaskOptionalMITREPresenceAndRawText(t *testing.T) {
	sink := &recordingAggressorBeaconTranscriptSink{}
	runtimeInstance, err := New(WithAggressorBeaconTranscriptSink(sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, arguments := range [][]Value{
		{String("beacon"), String("two arguments")},
		{String("beacon"), String("empty string"), String("")},
		{String("beacon"), String("null"), Null()},
		{String("beacon"), String("raw"), String(" T1003\t,T1059.001 ")},
	} {
		if _, err := runtimeInstance.Invoke(context.Background(), "btask", arguments...); err != nil {
			t.Fatal(err)
		}
	}
	records := sink.snapshot()
	if len(records) != 4 {
		t.Fatalf("records = %d, want 4", len(records))
	}
	if records[0].HasMITREIDs || records[0].RawMITREIDs != "" {
		t.Errorf("two-argument btask MITRE fields = %v/%q, want false/empty", records[0].HasMITREIDs, records[0].RawMITREIDs)
	}
	for index := 1; index < len(records); index++ {
		if !records[index].HasMITREIDs {
			t.Errorf("record %d lost supplied third-argument presence", index)
		}
	}
	if records[1].RawMITREIDs != "" || records[2].RawMITREIDs != "" {
		t.Errorf("empty/null raw MITRE IDs = %q/%q, want empty/empty", records[1].RawMITREIDs, records[2].RawMITREIDs)
	}
	if got, want := records[3].RawMITREIDs, " T1003\t,T1059.001 "; got != want {
		t.Errorf("raw MITRE IDs = %q, want exact %q", got, want)
	}
}

func TestAggressorBeaconTranscriptFunctionsEnforceExactArity(t *testing.T) {
	sink := &recordingAggressorBeaconTranscriptSink{}
	runtimeInstance, err := New(WithAggressorBeaconTranscriptSink(sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	tests := []struct {
		name   string
		counts []int
	}{
		{"berror", []int{1, 3}},
		{"blog", []int{1, 3}},
		{"blog2", []int{1, 3}},
		{"binput", []int{1, 3}},
		{"btask", []int{1, 4}},
		{"btaskcompleted", []int{1, 3}},
		{"bjoblog", []int{2, 4}},
		{"bjoberror", []int{2, 4}},
	}
	for _, test := range tests {
		for _, count := range test.counts {
			arguments := make([]Value, count)
			for index := range arguments {
				arguments[index] = String(fmt.Sprintf("argument-%d", index))
			}
			result, err := runtimeInstance.Invoke(context.Background(), test.name, arguments...)
			if err == nil || !strings.Contains(err.Error(), "expected exactly") {
				t.Errorf("%s/%d error = %v, want exact-arity error", test.name, count, err)
			}
			if !result.IsNull() {
				t.Errorf("%s/%d result = %s, want $null", test.name, count, result.Describe())
			}
		}
	}
	if records := sink.snapshot(); len(records) != 0 {
		t.Fatalf("invalid calls published %d records", len(records))
	}
}

func TestAggressorBeaconTranscriptSnapshotsReferencesWithoutIDFanout(t *testing.T) {
	sink := &recordingAggressorBeaconTranscriptSink{}
	runtimeInstance, err := New(WithAggressorBeaconTranscriptSink(sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	ids := NewArray(String("beacon-a"), String("beacon-b"))
	idCell := NewCell(ArrayValue(ids))
	textCell := NewCell(BinaryString([]byte{'b', 'e', 'f', 'o', 'r', 'e'}))
	invocation := Invocation{
		Runtime: runtimeInstance,
		Name:    "blog",
		Arguments: []Argument{
			{Reference: idCell},
			{Reference: textCell},
		},
	}
	result, err := runtimeInstance.blog(context.Background(), invocation)
	if err != nil || !result.IsNull() {
		t.Fatalf("blog = (%s, %v), want ($null, nil)", result.Describe(), err)
	}
	idCell.Set(String("replacement"))
	textCell.Set(String("after"))
	records := sink.snapshot()
	if len(records) != 1 {
		t.Fatalf("array Beacon ID published %d records, want exactly one", len(records))
	}
	recordedIDs, ok := records[0].BeaconID.Array()
	if !ok || recordedIDs != ids {
		t.Fatalf("BeaconID = %s, want original array identity", records[0].BeaconID.Describe())
	}
	if got := records[0].Text.String(); got != "before" || !records[0].Text.IsBinaryString() {
		t.Errorf("snapshotted Text = %q/binary=%v, want before/binary", got, records[0].Text.IsBinaryString())
	}
}

func TestAggressorBeaconTranscriptSinkErrorsCancellationAndRuntimeClose(t *testing.T) {
	wantErr := errors.New("sink rejected record")
	sink := &recordingAggressorBeaconTranscriptSink{err: wantErr}
	var stdout recordingWriter
	runtimeInstance, err := New(WithAggressorBeaconTranscriptSink(sink), WithStdout(&stdout))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Invoke(context.Background(), "blog", String("beacon"), String("text"))
	if !errors.Is(err, wantErr) || !result.IsNull() {
		t.Fatalf("sink error call = (%s, %v), want ($null, %v)", result.Describe(), err, wantErr)
	}
	if len(stdout.snapshot()) != 0 {
		t.Fatal("sink error was duplicated to stdout")
	}

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtimeInstance.Invoke(preCanceled, "blog", String("beacon"), String("text")); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled call error = %v, want context.Canceled", err)
	}
	if got := len(sink.snapshot()); got != 1 {
		t.Fatalf("pre-canceled call reached sink; calls = %d, want 1", got)
	}

	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "blog", String("beacon"), String("text")); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("closed Runtime error = %v, want ErrRuntimeClosed", err)
	}
	if got := len(sink.snapshot()); got != 1 {
		t.Fatalf("closed Runtime reached sink; calls = %d, want 1", got)
	}

	var cancelDuringPublish context.CancelFunc
	cancelSink := &recordingAggressorBeaconTranscriptSink{
		publish: func(context.Context, AggressorBeaconTranscriptRecord) error {
			cancelDuringPublish()
			return nil
		},
	}
	cancelRuntime, err := New(WithAggressorBeaconTranscriptSink(cancelSink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cancelRuntime.Close(context.Background()) })
	during, cancelDuring := context.WithCancel(context.Background())
	cancelDuringPublish = cancelDuring
	defer cancelDuring()
	if _, err := cancelRuntime.Invoke(during, "blog", String("beacon"), String("text")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled-during-publish error = %v, want context.Canceled", err)
	}
	if got := len(cancelSink.snapshot()); got != 1 {
		t.Fatalf("canceled-during-publish calls = %d, want 1", got)
	}
}

func TestAggressorBeaconTranscriptSinkBoundaryErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			sink := &recordingAggressorBeaconTranscriptSink{err: boundaryErr}
			runtimeInstance, err := New(WithAggressorBeaconTranscriptSink(sink))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), "blog", String("B"), String("text"))
			if !errors.Is(err, boundaryErr) || !result.IsNull() {
				t.Fatalf("Invoke = (%s, %v), want null/%v", result.Describe(), err, boundaryErr)
			}
			_, err = runtimeInstance.Eval(context.Background(), "transcript-boundary-error.cna", `blog("B", "text");`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("script error = %v, want authoritative %v", err, boundaryErr)
			}
			if got := len(sink.snapshot()); got != 2 {
				t.Fatalf("sink calls = %d, want two", got)
			}
		})
	}
}

func TestAggressorBeaconTranscriptStdoutBoundaryErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			runtimeInstance, err := New(WithStdout(aggressorBeaconTranscriptErrorWriter{err: boundaryErr}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), "blog", String("B"), String("text"))
			if !errors.Is(err, boundaryErr) || !result.IsNull() {
				t.Fatalf("Invoke = (%s, %v), want null/%v", result.Describe(), err, boundaryErr)
			}
			_, err = runtimeInstance.Eval(context.Background(), "transcript-stdout-error.cna", `blog("B", "text");`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("script error = %v, want authoritative %v", err, boundaryErr)
			}
		})
	}
}

func TestAggressorBeaconTranscriptStdoutFormatIsAtomicEscapedAndStable(t *testing.T) {
	var stdout recordingWriter
	runtimeInstance, err := New(WithStdout(&stdout))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  42,
		Name:    "blog",
		Span: Span{
			Source: "fixture\n\x1b.cna",
			Start:  Position{Offset: 3, Line: 2, Column: 4},
			End:    Position{Offset: 9, Line: 2, Column: 10},
		},
		Arguments: []Argument{
			{Value: String("beacon\n\x1b")},
			{Value: BinaryString([]byte{'x', '\n', 0, 0xff})},
		},
	}
	result, err := runtimeInstance.blog(context.Background(), invocation)
	if err != nil || !result.IsNull() {
		t.Fatalf("blog = (%s, %v), want ($null, nil)", result.Describe(), err)
	}
	writes := stdout.snapshot()
	if len(writes) != 1 {
		t.Fatalf("stdout writes = %d, want exactly one", len(writes))
	}
	want := fmt.Sprintf(`opfor.aggressor.beacon_transcript kind="blog" runtime_id=%d script=42 source="fixture\n\x1b.cna" start_offset=3 start_line=2 start_column=4 end_offset=9 end_line=2 end_column=10 beacon_id.kind="string" beacon_id.binary=false beacon_id.tainted=false beacon_id="beacon\n\x1b" text.kind="string" text.binary=true text.tainted=false text="x\n\x00\xff"`, runtimeInstance.ID()) + "\n"
	if got := string(writes[0]); got != want {
		t.Fatalf("stdout record:\n got %q\nwant %q", got, want)
	}
	if bytes.Count(writes[0], []byte{'\n'}) != 1 || bytes.Contains(writes[0][:len(writes[0])-1], []byte{0, 0xff, 0x1b}) {
		t.Fatalf("stdout record contains an unescaped record/control byte: %q", writes[0])
	}
}

func TestAggressorBeaconTranscriptStdoutConcurrentCallsRemainWhole(t *testing.T) {
	var stdout recordingWriter
	runtimeInstance, err := New(WithStdout(&stdout))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	const calls = 64
	var wait sync.WaitGroup
	errorsByCall := make(chan error, calls)
	for index := 0; index < calls; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := runtimeInstance.Invoke(
				context.Background(), "blog",
				String(fmt.Sprintf("beacon-%d", index)),
				String(fmt.Sprintf("line %d\n\x1b[31m", index)),
			)
			errorsByCall <- err
		}()
	}
	wait.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		if err != nil {
			t.Fatal(err)
		}
	}
	writes := stdout.snapshot()
	if len(writes) != calls {
		t.Fatalf("stdout writes = %d, want %d complete records", len(writes), calls)
	}
	for index, write := range writes {
		if !bytes.HasPrefix(write, []byte("opfor.aggressor.beacon_transcript kind=\"blog\"")) || bytes.Count(write, []byte{'\n'}) != 1 || write[len(write)-1] != '\n' {
			t.Errorf("write %d is not one whole escaped record: %q", index, write)
		}
	}
}

type aggressorBeaconTranscriptErrorWriter struct{ err error }

func (writer aggressorBeaconTranscriptErrorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

type aggressorBeaconTranscriptShortWriter struct{}

func (aggressorBeaconTranscriptShortWriter) Write(data []byte) (int, error) {
	return len(data) - 1, nil
}

func TestAggressorBeaconTranscriptStdoutAndFunctionOverrideErrors(t *testing.T) {
	wantErr := errors.New("stdout failed")
	runtimeInstance, err := New(WithStdout(aggressorBeaconTranscriptErrorWriter{err: wantErr}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Invoke(context.Background(), "blog", String("beacon"), String("text"))
	if !errors.Is(err, wantErr) || !result.IsNull() {
		t.Fatalf("stdout failure = (%s, %v), want ($null, %v)", result.Describe(), err, wantErr)
	}
	_ = runtimeInstance.Close(context.Background())

	shortRuntime, err := New(WithStdout(aggressorBeaconTranscriptShortWriter{}))
	if err != nil {
		t.Fatal(err)
	}
	result, err = shortRuntime.Invoke(context.Background(), "blog", String("beacon"), String("text"))
	if !errors.Is(err, io.ErrShortWrite) || !result.IsNull() {
		t.Fatalf("stdout short write = (%s, %v), want ($null, io.ErrShortWrite)", result.Describe(), err)
	}
	_ = shortRuntime.Close(context.Background())

	sink := &recordingAggressorBeaconTranscriptSink{}
	overridden, err := New(
		WithAggressorBeaconTranscriptSink(sink),
		WithFunction("blog", func(context.Context, Invocation) (Value, error) { return Int(73), nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = overridden.Close(context.Background()) })
	value, err := overridden.Invoke(context.Background(), "blog", String("ignored"), String("ignored"))
	if err != nil || value.Int32() != 73 {
		t.Fatalf("override result = (%s, %v), want (73, nil)", value.Describe(), err)
	}
	if got := len(sink.snapshot()); got != 0 {
		t.Fatalf("WithFunction override reached transcript sink %d time(s)", got)
	}
}

func TestAggressorBeaconTranscriptSinkRoutingBypassesHost(t *testing.T) {
	wantHostErr := errors.New("host probe")
	hostCalls := 0
	sink := &recordingAggressorBeaconTranscriptSink{}
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls++
			return Null(), wantHostErr
		})),
		WithAggressorBeaconTranscriptSink(sink),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Invoke(context.Background(), "blog", String("beacon"), String("text"))
	if err != nil || !result.IsNull() {
		t.Fatalf("blog = (%s, %v), want ($null, nil)", result.Describe(), err)
	}
	if hostCalls != 0 {
		t.Fatalf("native transcript wrapper reached Host %d time(s), want zero", hostCalls)
	}
	if got := len(sink.snapshot()); got != 1 {
		t.Fatalf("sink records = %d, want one", got)
	}

	if _, err := runtimeInstance.Invoke(context.Background(), "transcript_host_probe"); !errors.Is(err, wantHostErr) {
		t.Fatalf("unimplemented Host probe error = %v, want %v", err, wantHostErr)
	}
	if hostCalls != 1 {
		t.Fatalf("Host probe calls = %d, want one", hostCalls)
	}
}

func TestAggressorBeaconTranscriptOptionRejectsTypedNil(t *testing.T) {
	var sink *recordingAggressorBeaconTranscriptSink
	if _, err := New(WithAggressorBeaconTranscriptSink(sink)); err == nil || !strings.Contains(err.Error(), "transcript sink is nil") {
		t.Fatalf("typed-nil sink error = %v", err)
	}
	if err := AggressorBeaconTranscriptSinkFunc(nil).PublishAggressorBeaconTranscript(context.Background(), AggressorBeaconTranscriptRecord{}); err == nil {
		t.Fatal("nil AggressorBeaconTranscriptSinkFunc returned no error")
	}
}

func TestPortableScriptLoaderInheritsExplicitAggressorBeaconTranscriptSink(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "transcript-child.cna")
	if err := os.WriteFile(childPath, []byte(`blog("child-beacon", "child-output");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("transcript-parent.sl", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
return [$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingAggressorBeaconTranscriptSink{}
	var parentOutput bytes.Buffer
	runtimeInstance, err := New(
		WithAggressorBeaconTranscriptSink(sink),
		WithStdout(&parentOutput),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil || !result.IsNull() {
		t.Fatalf("child run = (%s, %v), want ($null, nil)", result.Describe(), err)
	}
	records := sink.snapshot()
	if len(records) != 1 {
		t.Fatalf("inherited sink records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Kind != AggressorBeaconTranscriptLog || record.BeaconID.String() != "child-beacon" || record.Text.String() != "child-output" {
		t.Fatalf("child record = %#v", record)
	}
	if record.Script == 0 || record.Span.Source == "" || record.Span.Start.Line == 0 {
		t.Errorf("child provenance = script %d span %s, want populated", record.Script, record.Span)
	}
	if record.RuntimeID == 0 || record.RuntimeID == runtimeInstance.ID() {
		t.Errorf("child RuntimeID = %d, want nonzero identity distinct from parent %d", record.RuntimeID, runtimeInstance.ID())
	}
	if parentOutput.Len() != 0 {
		t.Fatalf("inherited sink duplicated child record to parent stdout: %q", parentOutput.String())
	}
}

func TestPortableScriptLoaderTranscriptRuntimeIDDisambiguatesLocalScriptIDs(t *testing.T) {
	directory := t.TempDir()
	sharedSource := filepath.Join(directory, "shared-transcript-source.cna")
	if err := os.WriteFile(sharedSource, []byte(`blog("child", "same source");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString(filepath.ToSlash(sharedSource), fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$firstLoader = [new ScriptLoader];
$first = [$firstLoader loadScript: %q];
$secondLoader = [new ScriptLoader];
$second = [$secondLoader loadScript: %q];
blog("parent", "same source");
[$first runScript];
[$second runScript];
`, filepath.ToSlash(sharedSource), filepath.ToSlash(sharedSource)))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingAggressorBeaconTranscriptSink{}
	runtimeInstance, err := New(WithAggressorBeaconTranscriptSink(sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	records := sink.snapshot()
	if len(records) != 3 {
		t.Fatalf("records = %d, want parent plus two children", len(records))
	}
	origins := make(map[RuntimeID]struct{}, len(records))
	for index, record := range records {
		if record.RuntimeID == 0 {
			t.Fatalf("record %d has zero RuntimeID", index)
		}
		origins[record.RuntimeID] = struct{}{}
		if record.Script != 1 {
			t.Errorf("record %d Script = %d, want colliding runtime-local ID 1", index, record.Script)
		}
		if got, want := record.Span.Source, filepath.ToSlash(sharedSource); got != want {
			t.Errorf("record %d source = %q, want colliding %q", index, got, want)
		}
	}
	if len(origins) != 3 {
		t.Fatalf("RuntimeIDs = %d, want three distinct origins for colliding Script IDs/sources", len(origins))
	}
	if records[0].RuntimeID != runtimeInstance.ID() {
		t.Errorf("parent record RuntimeID = %d, want parent %d", records[0].RuntimeID, runtimeInstance.ID())
	}
}

func TestPortableScriptLoaderTranscriptStdoutRuntimeIDDisambiguatesSharedConsole(t *testing.T) {
	directory := t.TempDir()
	sharedSource := filepath.Join(directory, "shared-transcript-stdout.cna")
	if err := os.WriteFile(sharedSource, []byte(`blog("child", "same source");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString(filepath.ToSlash(sharedSource), fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$firstLoader = [new ScriptLoader];
$first = [$firstLoader loadScript: %q];
$secondLoader = [new ScriptLoader];
$second = [$secondLoader loadScript: %q];
blog("parent", "same source");
[$first runScript];
[$second runScript];
`, filepath.ToSlash(sharedSource), filepath.ToSlash(sharedSource)))
	if err != nil {
		t.Fatal(err)
	}
	var stdout recordingWriter
	runtimeInstance, err := New(WithStdout(&stdout))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	writes := stdout.snapshot()
	if len(writes) != 3 {
		t.Fatalf("stdout writes = %d, want parent plus two child records", len(writes))
	}
	origins := make(map[uint64]struct{}, len(writes))
	for index, write := range writes {
		line := string(write)
		if !strings.Contains(line, ` script=1 source=`+strconv.Quote(filepath.ToSlash(sharedSource))) {
			t.Errorf("record %d does not retain the colliding script/source provenance: %q", index, line)
		}
		var origin uint64
		found := false
		for _, field := range strings.Fields(line) {
			if !strings.HasPrefix(field, "runtime_id=") {
				continue
			}
			origin, err = strconv.ParseUint(strings.TrimPrefix(field, "runtime_id="), 10, 64)
			if err != nil || origin == 0 {
				t.Fatalf("record %d runtime_id field = %q: %v", index, field, err)
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("record %d has no runtime_id: %q", index, line)
		}
		origins[origin] = struct{}{}
	}
	if len(origins) != 3 {
		t.Fatalf("stdout runtime IDs = %d, want three distinct origins", len(origins))
	}
	if _, exists := origins[uint64(runtimeInstance.ID())]; !exists {
		t.Fatalf("stdout origins do not include parent RuntimeID %d", runtimeInstance.ID())
	}
}

func TestPortableScriptLoaderUnsetAggressorBeaconTranscriptSinkUsesChildConsole(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "transcript-fallback-child.cna")
	if err := os.WriteFile(childPath, []byte(`blog("child-beacon", "child-output");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("transcript-fallback-parent.sl", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
$buffer = allocate();
[sleep.bridges.io.IOObject setConsole: [$child getScriptEnvironment], $buffer];
[$child runScript];
closef($buffer);
return readb($buffer, available($buffer));
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	var parentOutput bytes.Buffer
	runtimeInstance, err := New(WithStdout(&parentOutput))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := result.Bytes()
	if !ok || !result.IsBinaryString() {
		t.Fatalf("child console result = %s/binary=%v, want binary string", result.Describe(), result.IsBinaryString())
	}
	if bytes.Count(output, []byte{'\n'}) != 1 || !bytes.Contains(output, []byte(`kind="blog"`)) ||
		!bytes.Contains(output, []byte(`beacon_id="child-beacon"`)) || !bytes.Contains(output, []byte(`text="child-output"`)) {
		t.Fatalf("child-local fallback = %q", output)
	}
	if !bytes.Contains(output, []byte("source="+strconv.Quote(filepath.ToSlash(childPath)))) {
		t.Errorf("child-local fallback lost source provenance: %q", output)
	}
	if parentOutput.Len() != 0 {
		t.Fatalf("child-local fallback leaked to parent stdout: %q", parentOutput.String())
	}
}

// aggressorBeaconTranscriptCall reconstructs the public Aggressor function
// call represented by a record for corpus fixtures which historically
// recorded these calls through Host.
func aggressorBeaconTranscriptCall(record AggressorBeaconTranscriptRecord) (string, []Value) {
	values := []Value{record.BeaconID}
	switch record.Kind {
	case AggressorBeaconTranscriptError,
		AggressorBeaconTranscriptLog,
		AggressorBeaconTranscriptLog2,
		AggressorBeaconTranscriptInput:
		values = append(values, record.Text)
	case AggressorBeaconTranscriptTask:
		values = append(values, record.Text)
		if record.HasMITREIDs {
			values = append(values, String(record.RawMITREIDs))
		}
	case AggressorBeaconTranscriptTaskCompleted:
		values = append(values, record.TaskID)
	case AggressorBeaconTranscriptJobLog, AggressorBeaconTranscriptJobError:
		values = append(values, record.JobID, record.Text)
	}
	return string(record.Kind), values
}

var _ io.Writer = aggressorBeaconTranscriptErrorWriter{}

var _ io.Writer = aggressorBeaconTranscriptShortWriter{}
