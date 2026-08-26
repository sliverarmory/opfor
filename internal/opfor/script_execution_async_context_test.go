package opfor

import (
	"context"
	"errors"
	"testing"
	"time"
)

type asynchronousExecutionImporterContextKey struct{}

func TestDetachAsynchronousExecutionContextMasksPrivateState(t *testing.T) {
	deadlineContext, cancelDeadline := context.WithDeadline(
		context.Background(),
		time.Now().Add(time.Hour),
	)
	defer cancelDeadline()
	importerContext, cancelImporter := context.WithCancelCause(deadlineContext)
	importerValue := &struct{ name string }{name: "importer value"}
	meter := &executionMeter{limit: 73}

	ctx := context.WithValue(importerContext, asynchronousExecutionImporterContextKey{}, importerValue)
	ctx = context.WithValue(ctx, executionMeterKey{}, meter)
	privateValues := []struct {
		name  string
		key   any
		value any
	}{
		{name: "fiber", key: currentFiberContextKey{}, value: &fiber{}},
		{name: "include", key: includeChainContextKey{}, value: []includeChainEntry{{}}},
		{name: "binding", key: bindingInvocationContextKey{}, value: &BindingInvocation{}},
		{name: "loadable", key: loadableResolutionContextKey{}, value: &loadableResolutionToken{}},
		{name: "native", key: nativeDispatchStateContextKey{}, value: &nativeDispatchState{}},
		{name: "run", key: portableScriptInstanceRunContextKey{}, value: &portableScriptInstanceRunToken{}},
		{name: "script execution", key: scriptExecutionContextKey{}, value: &scriptExecutionToken{}},
		{name: "runtime execution", key: runtimeExecutionContextKey{}, value: &runtimeExecutionToken{}},
		{name: "script unload", key: scriptUnloadContextKey{}, value: &scriptUnloadToken{}},
		{name: "runtime close", key: runtimeCloseContextKey{}, value: &runtimeCloseToken{}},
		{name: "UI ancestry", key: aggressorUICallbackAncestryContextKey{}, value: &aggressorUICallbackAncestry{}},
		{name: "generation cleanup", key: scriptGenerationCleanupContextKey{}, value: &scriptGenerationCleanupToken{}},
	}
	for _, private := range privateValues {
		ctx = context.WithValue(ctx, private.key, private.value)
		if got := ctx.Value(private.key); got == nil {
			t.Fatalf("source context %s value is nil", private.name)
		}
	}

	detached := detachAsynchronousExecutionContext(ctx)
	if got := detached.Value(asynchronousExecutionImporterContextKey{}); got != importerValue {
		t.Fatalf("importer value = %#v, want exact retained value %#v", got, importerValue)
	}
	if got := detached.Value(executionMeterKey{}); got != meter {
		t.Fatalf("execution meter = %p, want exact retained meter %p", got, meter)
	}
	for _, private := range privateValues {
		if got := detached.Value(private.key); got != nil {
			t.Errorf("detached context retained private %s value %#v", private.name, got)
		}
	}
	wantDeadline, wantDeadlineOK := importerContext.Deadline()
	gotDeadline, gotDeadlineOK := detached.Deadline()
	if gotDeadlineOK != wantDeadlineOK || !gotDeadline.Equal(wantDeadline) {
		t.Fatalf("detached deadline = (%v, %v), want (%v, %v)", gotDeadline, gotDeadlineOK, wantDeadline, wantDeadlineOK)
	}
	if err := detached.Err(); err != nil {
		t.Fatalf("detached context before importer cancellation = %v", err)
	}

	importerCanceled := errors.New("importer canceled")
	cancelImporter(importerCanceled)
	select {
	case <-detached.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("detached context did not observe importer cancellation")
	}
	if !errors.Is(detached.Err(), context.Canceled) {
		t.Fatalf("detached error = %v, want context.Canceled", detached.Err())
	}
	if !errors.Is(context.Cause(detached), importerCanceled) {
		t.Fatalf("detached cancellation cause = %v, want %v", context.Cause(detached), importerCanceled)
	}
}
