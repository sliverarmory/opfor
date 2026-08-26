package aggressor

import (
	"slices"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestCatalogPayloadFunctionsUseNativeWrappers(t *testing.T) {
	t.Parallel()

	names := []string{
		"-hasbootstraphint", "all_payloads", "artifact", "artifact_general",
		"artifact_sign", "artifact_stager", "payload", "payload_bootstrap_hint",
		"payload_local", "powershell", "shellcode", "stager", "stager_bind_pipe",
		"stager_bind_tcp",
	}
	defaults := opfor.DefaultFunctionNames()
	for _, name := range names {
		entry, ok := Lookup(KindFunction, name)
		if !ok || entry.Support != SupportHostRequired || entry.Boundary != BoundaryNativeWrapper {
			t.Errorf("Lookup(function, %q) = %#v/%v, want host-required native wrapper", name, entry, ok)
		}
		if !slices.Contains(defaults, name) {
			t.Errorf("DefaultFunctionNames does not contain payload function %q", name)
		}
	}

	allPayloads, ok := Lookup(KindFunction, "all_payloads")
	if !ok || allPayloads.Contract.Audit != ContractAuditRuntimeEnforced ||
		!allPayloads.Contract.Arity.Known || allPayloads.Contract.Arity.Minimum != 3 ||
		allPayloads.Contract.Arity.Maximum != 6 || len(allPayloads.Contract.ArgumentConstraints) != 3 {
		t.Fatalf("all_payloads contract = %#v, found=%v", allPayloads.Contract, ok)
	}
	want := []struct {
		position int
		values   []string
	}{
		{position: 3, values: []string{"None", "Direct", "Indirect"}},
		{position: 4, values: []string{"wininet", "winhttp", "$null", ""}},
		{position: 5, values: []string{"dns", "dns_over_https", "$null", ""}},
	}
	for index, constraint := range allPayloads.Contract.ArgumentConstraints {
		if constraint.Position != want[index].position || constraint.Kind != "enum" ||
			!slices.Equal(constraint.Values, want[index].values) {
			t.Errorf("all_payloads constraint %d = %#v, want position %d values %#v",
				index, constraint, want[index].position, want[index].values)
		}
	}
}
