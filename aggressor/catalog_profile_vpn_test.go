package aggressor

import (
	"slices"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestCatalogClassifiesProfileAndVPNNativeWrappers(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"killdate",
		"setup_strings",
		"setup_transformations",
		"vpn_interface_info",
		"vpn_interfaces",
		"vpn_tap_create",
		"vpn_tap_delete",
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
