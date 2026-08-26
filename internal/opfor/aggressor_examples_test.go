package opfor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestOfficialAggressorExamplesCompileAndLoadWithRecordingAdapters(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "upstream", "aggressor-script-examples", "*.cna"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) != 18 {
		t.Fatalf("official example count = %d, want 18", len(paths))
	}

	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(filepath.Base(path), data))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}

			recorder := &aggressorLoadRecorder{}
			var diagnostics bytes.Buffer
			options := []Option{
				WithHost(HostFunc(recorder.call)),
				WithAggressorClientServiceProvider(recorder),
				WithObjectHost(ObjectHostFunc(recorder.object)),
				WithBindingObserver(recorder),
				WithStdout(io.Discard),
				WithStderr(&diagnostics),
				WithInstructionLimit(250_000),
			}
			// Prevent a future corpus update from accidentally turning this
			// structural load test into a local filesystem or process action.
			for _, name := range []string{
				"__EXEC__", "exec", "openf", "ls", "lof", "mkdir",
				"deleteFile", "move", "rename", "copyFile",
			} {
				name := name
				options = append(options, WithFunction(name, func(context.Context, Invocation) (Value, error) {
					return Null(), fmt.Errorf("test blocked external function %s", name)
				}))
			}
			runtime, err := New(options...)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			script, err := runtime.Load(ctx, program)
			cancel()
			if err != nil {
				t.Fatalf("load with recording adapters: %v", err)
			}
			if script == nil || script.ID() == 0 {
				t.Fatalf("loaded script = %#v", script)
			}
			if recorder.registrationCount() == 0 && recorder.callCount() == 0 && recorder.objectCount() == 0 {
				t.Fatal("example loaded without any observable registration or adapter call")
			}
			if got := diagnostics.String(); got != "" {
				t.Fatalf("load diagnostics = %q, want none", got)
			}
		})
	}
}

type aggressorLoadRecorder struct {
	mu            sync.Mutex
	calls         []string
	objects       []ObjectOperation
	registrations []Binding
}

func (recorder *aggressorLoadRecorder) call(_ context.Context, invocation Invocation) (Value, error) {
	if _, clientService := aggressorClientServiceSpecs[invocation.Name]; clientService {
		return Null(), fmt.Errorf("typed Aggressor client service %q bypassed configured provider", invocation.Name)
	}
	recorder.mu.Lock()
	recorder.calls = append(recorder.calls, invocation.Name)
	recorder.mu.Unlock()

	// The recording adapter supplies only the structural values needed for
	// deterministic top-level loading. It deliberately performs no Cobalt
	// effect and never invokes callbacks handed to it.
	switch invocation.Name {
	case "data_keys":
		return ArrayValue(NewArray()), nil
	case "data_query":
		model := NewHash()
		if invocation.Arg(0).String() == "metadata" {
			model.Set("c2profile", ObjectValue(&aggressorLoadObject{class: "Profile"}))
		}
		return HashValue(model), nil
	default:
		return Null(), nil
	}
}

func (recorder *aggressorLoadRecorder) HandleAggressorClientService(
	_ context.Context,
	request AggressorClientServiceRequest,
) (Value, error) {
	recorder.mu.Lock()
	recorder.calls = append(recorder.calls, request.Name)
	recorder.mu.Unlock()
	switch request.Operation {
	case AggressorClientServiceGetAggressorClient:
		return ObjectValue(&aggressorLoadObject{class: "AggressorClient"}), nil
	case AggressorClientServiceGetCSVersion:
		return String("test-version"), nil
	case AggressorClientServiceMyNick:
		return String("operator"), nil
	case AggressorClientServiceUsers:
		return ArrayValue(NewArray()), nil
	default:
		return Null(), nil
	}
}

func (recorder *aggressorLoadRecorder) object(_ context.Context, invocation ObjectInvocation) (Value, error) {
	recorder.mu.Lock()
	recorder.objects = append(recorder.objects, invocation.Op)
	recorder.mu.Unlock()
	if invocation.Op == ObjectConstruct {
		return ObjectValue(&aggressorLoadObject{class: invocation.Class}), nil
	}
	return Null(), nil
}

func (recorder *aggressorLoadRecorder) Registered(_ context.Context, binding Binding) error {
	recorder.mu.Lock()
	recorder.registrations = append(recorder.registrations, binding)
	recorder.mu.Unlock()
	return nil
}

func (recorder *aggressorLoadRecorder) Unregistered(_ context.Context, _ Binding) error {
	return nil
}

func (recorder *aggressorLoadRecorder) callCount() int {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return len(recorder.calls)
}

func (recorder *aggressorLoadRecorder) objectCount() int {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return len(recorder.objects)
}

func (recorder *aggressorLoadRecorder) registrationCount() int {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return len(recorder.registrations)
}

type aggressorLoadObject struct {
	class string
}
