package aggressor

import (
	"strings"
	"testing"
)

func TestCatalogCoreInvokedHooksDescribeRuntimeTrigger(t *testing.T) {
	t.Parallel()

	for name, description := range coreInvokedHookDescriptions {
		entry, ok := Lookup(KindHook, name)
		if !ok {
			t.Errorf("Lookup(hook, %q) did not find official hook", name)
			continue
		}
		if entry.Boundary != BoundaryBindingDispatch || entry.Description != description {
			t.Errorf("Lookup(hook, %q) = boundary %q description %q, want %q/%q",
				name, entry.Boundary, entry.Description, BoundaryBindingDispatch, description)
		}
	}
}

func TestNativeWrapperBoundaryDoesNotPromiseUniversalHostFallback(t *testing.T) {
	t.Parallel()

	text := strings.ToLower(string(BoundaryNativeWrapper))
	if strings.Contains(text, "fallback") {
		t.Fatalf("boundary wire value unexpectedly encodes routing detail: %q", text)
	}

	for _, entry := range Catalog().Entries {
		if entry.Boundary != BoundaryNativeWrapper {
			continue
		}
		if entry.Name == "berror" && strings.Contains(strings.ToLower(entry.Description), "host") {
			t.Fatalf("dedicated transcript wrapper description promises Host routing: %q", entry.Description)
		}
	}
}
