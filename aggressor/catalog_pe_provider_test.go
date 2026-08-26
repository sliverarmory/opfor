package aggressor

import (
	"slices"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestCatalogClassifiesDocumentedPEAndRedactionProviderWrappers(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"pe_insert_rich_header",
		"pe_mask_section",
		"pe_patch_code",
		"pe_remove_rich_header",
		"pe_set_compile_time_with_string",
		"pe_set_export_name",
		"pe_set_value_at",
		"pedump",
		"redactobject",
	} {
		entry, ok := Lookup(KindFunction, name)
		if !ok || entry.Support != SupportHostRequired || entry.Boundary != BoundaryNativeWrapper {
			t.Errorf("Lookup(function, %q) = %#v/%v, want host-required native wrapper", name, entry, ok)
		}
		if !slices.Contains(opfor.DefaultFunctionNames(), name) {
			t.Errorf("DefaultFunctionNames does not contain %q", name)
		}
	}

}
