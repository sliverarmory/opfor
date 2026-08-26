package aggressor

import (
	"slices"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestCatalogClassifiesCodeTransformProviderWrappers(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"encode", "powershell_compress", "transform_vbs"} {
		entry, ok := Lookup(KindFunction, name)
		if !ok || entry.Support != SupportHostRequired || entry.Boundary != BoundaryNativeWrapper {
			t.Errorf("Lookup(function, %q) = %#v/%v, want host-required native wrapper", name, entry, ok)
		}
		if !slices.Contains(opfor.DefaultFunctionNames(), name) {
			t.Errorf("DefaultFunctionNames does not contain %q", name)
		}
	}
}
