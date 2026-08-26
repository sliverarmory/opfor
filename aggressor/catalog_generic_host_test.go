package aggressor

import (
	"reflect"
	"slices"
	"testing"
)

func TestCatalogOfficialGenericHostBoundaryIsIntentional(t *testing.T) {
	t.Parallel()

	want := []string{
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
	got := make([]string, 0, len(want))
	for _, name := range officialFunctionNames {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Fatalf("Lookup(function, %q) did not find official function", name)
		}
		if entry.Boundary == BoundaryGenericHost {
			got = append(got, name)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("official generic-Host functions = %q, want intentional debt set %q", got, want)
	}
}
