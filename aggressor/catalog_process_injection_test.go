package aggressor

import (
	"slices"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestCatalogClassifiesProcessInjectionNativeWrappers(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"pi_explicit_get", "pi_explicit_info", "pi_explicit_set",
		"pi_spawn_get", "pi_spawn_info", "pi_spawn_set",
		"pi_user_explicit_clear", "pi_user_explicit_get", "pi_user_explicit_get_map",
		"pi_user_explicit_get_names", "pi_user_explicit_set", "pi_user_spawn_clear",
		"pi_user_spawn_get", "pi_user_spawn_get_map", "pi_user_spawn_get_names",
		"pi_user_spawn_set",
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
