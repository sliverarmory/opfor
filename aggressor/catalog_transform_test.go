package aggressor

import (
	"slices"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestCatalogClassifiesPortableTransformAndPowerShellCommand(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"transform", "powershell_command"} {
		entry, ok := Lookup(KindFunction, name)
		if !ok || entry.Support != SupportPortableDefault {
			t.Errorf("Lookup(function, %q) = %#v/%v, want portable-default", name, entry, ok)
		}
		if !slices.Contains(opfor.DefaultFunctionNames(), name) {
			t.Errorf("DefaultFunctionNames does not contain %q", name)
		}
	}

	// Formatting for these related documented surfaces is not complete in the
	// public reference, so this tranche must not overstate their support.
	for _, name := range []string{"transform_vbs", "powershell_compress"} {
		entry, ok := Lookup(KindFunction, name)
		if !ok || entry.Support != SupportHostRequired {
			t.Errorf("Lookup(function, %q) = %#v/%v, want host-required", name, entry, ok)
		}
	}
}
