package opfor

import (
	"context"
	"io"
	"strconv"
	"sync/atomic"
	"testing"
)

type recordingRuntimeExtensionProfile struct {
	clones atomic.Int32
}

func (profile *recordingRuntimeExtensionProfile) cloneForScriptLoader(parent *Runtime) Option {
	profile.clones.Add(1)
	parentID := parent.ID()
	return WithFunction("extension_profile_parent_runtime_id", func(context.Context, Invocation) (Value, error) {
		return String(strconv.FormatUint(uint64(parentID), 10)), nil
	})
}

func TestScriptLoaderPreservesActiveRuntimeExtensionProfiles(t *testing.T) {
	profile := &recordingRuntimeExtensionProfile{}
	parent, err := New(withRuntimeExtensionProfiles([]runtimeExtensionProfile{profile}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Close(context.Background()) })

	child := newProfileTestChildRuntime(t, parent)
	assertProfileTestParentRuntimeID(t, child, parent.ID())
	if len(child.extensionProfiles) != 1 || child.extensionProfiles[0] != profile {
		t.Fatalf("child extension profiles = %#v, want exact parent profile", child.extensionProfiles)
	}

	grandchild := newProfileTestChildRuntime(t, child)
	assertProfileTestParentRuntimeID(t, grandchild, child.ID())
	if len(grandchild.extensionProfiles) != 1 || grandchild.extensionProfiles[0] != profile {
		t.Fatalf("grandchild extension profiles = %#v, want exact parent profile", grandchild.extensionProfiles)
	}
	if got := profile.clones.Load(); got != 2 {
		t.Fatalf("profile clone calls = %d, want child plus grandchild", got)
	}
}

func newProfileTestChildRuntime(t *testing.T, parent *Runtime) *Runtime {
	t.Helper()
	instance := &portableScriptInstance{
		loader: &portableScriptLoader{runtime: parent},
		debug:  1,
	}
	child, err := instance.newChildRuntime(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Close(context.Background()) })
	return child
}

func assertProfileTestParentRuntimeID(t *testing.T, runtime *Runtime, want RuntimeID) {
	t.Helper()
	value, err := runtime.Invoke(context.Background(), "extension_profile_parent_runtime_id")
	if err != nil || value.String() != strconv.FormatUint(uint64(want), 10) {
		t.Fatalf("profile marker = (%s, %v), want parent RuntimeID %d", value.Describe(), err, want)
	}
}
