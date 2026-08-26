package opfor

import (
	"context"
	"io"
	"strings"
	"testing"
)

const maximumRuntimeLifecycleFuzzOperations = 256

func FuzzRuntimeLifecycle(f *testing.F) {
	program, err := CompileString("<lifecycle-fuzz>", `
sub echo { return $1; }
on fuzz { return $1; }
return "loaded";
`)
	if err != nil {
		f.Fatalf("compile lifecycle fuzz program: %v", err)
	}
	for _, seed := range [][]byte{
		{0, 1, 2, 3, 4, 5},
		{0, 4, 0, 1, 6, 7, 0, 2},
		{5, 0, 1, 2, 3, 4, 6, 7},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > maximumRuntimeLifecycleFuzzOperations {
			t.Skip()
		}
		ctx := context.Background()
		runtimeInstance := newLifecycleFuzzRuntime(t)
		runtimes := []*Runtime{runtimeInstance}
		var scripts []*Script
		var current *Script
		closed := false

		for _, operation := range operations {
			switch operation % 8 {
			case 0:
				if !closed && (current == nil || !current.Active()) {
					loaded, loadErr := runtimeInstance.Load(ctx, program, Int(int32(operation)))
					if loadErr == nil {
						current = loaded
						scripts = append(scripts, loaded)
						if loaded.ID() == 0 || !loaded.Active() {
							t.Fatalf("successful load returned invalid script: id %d active %v", loaded.ID(), loaded.Active())
						}
					}
				}
			case 1:
				if current != nil {
					_, _ = current.Call(ctx, "echo", Int(int32(operation)))
				}
			case 2:
				_, _ = runtimeInstance.DispatchEvent(ctx, "fuzz", Int(int32(operation)))
			case 3:
				if current != nil {
					_ = current.Set("$fuzz", Int(int32(operation)))
					_ = current.Get("$fuzz")
				}
			case 4:
				if current != nil {
					_ = current.Unload(ctx)
				}
			case 5:
				_ = runtimeInstance.Close(ctx)
				closed = true
			case 6:
				_, _ = runtimeInstance.Invoke(ctx, "println", String("fuzz"))
			case 7:
				if closed {
					runtimeInstance = newLifecycleFuzzRuntime(t)
					runtimes = append(runtimes, runtimeInstance)
					current = nil
					closed = false
				}
			}
		}

		for _, runtimeInstance := range runtimes {
			if closeErr := runtimeInstance.Close(context.Background()); closeErr != nil {
				t.Fatalf("terminal runtime close: %v", closeErr)
			}
		}
		for _, script := range scripts {
			if script.Active() {
				t.Fatalf("script %d remained active after terminal runtime close", script.ID())
			}
		}
	})
}

func newLifecycleFuzzRuntime(t *testing.T) *Runtime {
	t.Helper()
	runtimeInstance, err := New(
		WithStdin(strings.NewReader("")),
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithLimits(Limits{
			MaxInstructionsPerExecution:    4_096,
			MaxCollectionEntriesPerRuntime: 4_096,
			MaxOutputBytesPerRuntime:       64 << 10,
			MaxInputBytesPerRuntime:        64 << 10,
			MaxDecompressedBytesPerRuntime: 64 << 10,
			MaxSourceBytesPerRuntime:       64 << 10,
		}),
	)
	if err != nil {
		t.Fatalf("create lifecycle fuzz runtime: %v", err)
	}
	return runtimeInstance
}
