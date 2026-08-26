package aggressor

import "testing"

func TestCatalogBreakpointIsPortableInterpreterOwned(t *testing.T) {
	entry, ok := Lookup(KindFunction, "brk")
	if !ok {
		t.Fatal("brk is absent from the official function catalog")
	}
	if entry.Support != SupportPortableDefault || entry.Boundary != BoundaryPortableRuntime {
		t.Fatalf("brk catalog classification = support %q boundary %q", entry.Support, entry.Boundary)
	}
	if entry.Description != portableDefaultFunctions["brk"] {
		t.Fatalf("brk description = %q, want %q", entry.Description, portableDefaultFunctions["brk"])
	}
}
