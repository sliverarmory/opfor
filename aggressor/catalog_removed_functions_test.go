package aggressor

import (
	"slices"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestCatalogRemovedOfficialFunctionsRemainGenericHostOnly(t *testing.T) {
	t.Parallel()

	wantNames := []string{
		"bbypassuac",
		"bpsexec_psh",
		"brunasadmin",
		"bstage",
		"bwdigest",
		"bwinrm",
		"bwmi",
		"openBypassUACDialog",
		"openWindowsDropperDialog",
	}
	gotNames := make([]string, 0, len(removedOfficialFunctionDescriptions))
	for name := range removedOfficialFunctionDescriptions {
		gotNames = append(gotNames, name)
	}
	slices.Sort(gotNames)
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("removed official functions = %q, want %q", gotNames, wantNames)
	}

	defaults := opfor.DefaultFunctionNames()
	for _, name := range wantNames {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(function, %q) did not find removed official heading", name)
			continue
		}
		if entry.Support != SupportHostRequired || entry.Boundary != BoundaryGenericHost {
			t.Errorf("Lookup(function, %q) = %q/%q, want host-required/generic-host", name, entry.Support, entry.Boundary)
		}
		if entry.Description != removedOfficialFunctionDescriptions[name] {
			t.Errorf("Lookup(function, %q).Description = %q, want %q", name, entry.Description, removedOfficialFunctionDescriptions[name])
		}
		if slices.Contains(defaults, name) {
			t.Errorf("DefaultFunctionNames unexpectedly contains removed function %q", name)
		}
		if _, installed := hostRequiredRuntimeFunctions[name]; installed {
			t.Errorf("hostRequiredRuntimeFunctions unexpectedly claims a native wrapper for removed function %q", name)
		}
		if hasEvidence(entry, "opfor-runtime", "native importer-boundary wrapper") {
			t.Errorf("removed function %q claims native-wrapper evidence: %#v", name, entry.Evidence)
		}
		if !hasEvidence(entry, "official-documentation", officialFunctionsURL) {
			t.Errorf("removed function %q lost official heading evidence: %#v", name, entry.Evidence)
		}
	}
}
