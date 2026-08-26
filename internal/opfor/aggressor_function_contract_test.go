package opfor

import (
	"slices"
	"testing"
)

func TestDefaultAggressorFunctionContractsAreSortedDetachedAndNative(t *testing.T) {
	contracts := DefaultAggressorFunctionContracts()
	if len(contracts) == 0 {
		t.Fatal("DefaultAggressorFunctionContracts returned no contracts")
	}
	native := make(map[string]bool)
	for _, name := range DefaultFunctionNames() {
		native[name] = true
	}
	seen := make(map[string]bool, len(contracts))
	for index, contract := range contracts {
		if index > 0 && contracts[index-1].Name >= contract.Name {
			t.Fatalf("contracts are not strictly sorted at %q", contract.Name)
		}
		if seen[contract.Name] {
			t.Fatalf("duplicate contract %q", contract.Name)
		}
		seen[contract.Name] = true
		if !native[contract.Name] {
			t.Errorf("contract %q is not a default native function", contract.Name)
		}
		if contract.MinimumArguments < 0 || contract.MaximumArguments < contract.MinimumArguments {
			t.Errorf("contract %q has invalid arity %d..%d", contract.Name, contract.MinimumArguments, contract.MaximumArguments)
		}
		if contract.TypedProvider == "" || contract.TypedResult == "" ||
			contract.ProviderErrors != AggressorContractProviderErrorsAuthoritative || !contract.HostFallback {
			t.Errorf("contract %q has incomplete typed-provider policy: %#v", contract.Name, contract)
		}
		for _, callback := range contract.Callbacks {
			if callback.Position < 1 || callback.Position > contract.MaximumArguments {
				t.Errorf("contract %q has invalid callback position %d", contract.Name, callback.Position)
			}
			if !callback.Retained {
				t.Errorf("contract %q callback %d is not marked retained", contract.Name, callback.Position)
			}
		}
		for _, constraint := range contract.ArgumentConstraints {
			if constraint.Position < 1 || constraint.Position > contract.MaximumArguments ||
				constraint.Kind == "" || len(constraint.Values) == 0 {
				t.Errorf("contract %q has invalid argument constraint: %#v", contract.Name, constraint)
			}
		}
	}

	contracts[0].Name = "mutated"
	if len(contracts[0].Callbacks) != 0 {
		contracts[0].Callbacks[0].Position = 99
	}
	for index := range contracts {
		if len(contracts[index].ArgumentConstraints) != 0 {
			contracts[index].ArgumentConstraints[0].Position = 99
			contracts[index].ArgumentConstraints[0].Values[0] = "mutated"
			break
		}
	}
	fresh := DefaultAggressorFunctionContracts()
	if fresh[0].Name == "mutated" || len(fresh[0].Callbacks) != 0 && fresh[0].Callbacks[0].Position == 99 {
		t.Fatal("DefaultAggressorFunctionContracts did not return a detached snapshot")
	}
	for _, contract := range fresh {
		for _, constraint := range contract.ArgumentConstraints {
			if constraint.Position == 99 || slices.Contains(constraint.Values, "mutated") {
				t.Fatal("DefaultAggressorFunctionContracts did not detach argument constraints")
			}
		}
	}
}

func TestDefaultAggressorFunctionContractRepresentativeShapes(t *testing.T) {
	contracts := make(map[string]AggressorFunctionContract)
	for _, contract := range DefaultAggressorFunctionContracts() {
		contracts[contract.Name] = contract
	}
	tests := []struct {
		name             string
		minimum, maximum int
		provider         string
		result           AggressorContractResult
		callback         int
		callbackArity    int
		callbackKnown    bool
		deprecated       bool
		constraints      []AggressorArgumentConstraint
	}{
		{name: "artifact_payload", minimum: 5, maximum: 9, provider: "AggressorArtifactProvider", result: AggressorContractResultValue},
		{name: "artifact_stageless", minimum: 5, maximum: 5, provider: "AggressorArtifactProvider", result: AggressorContractResultNull, callback: 5, callbackArity: 1, callbackKnown: true, deprecated: true},
		{name: "bof_extract", minimum: 1, maximum: 2, provider: "AggressorBOFExtractor", result: AggressorContractResultValue},
		{name: "bipconfig", minimum: 2, maximum: 2, provider: "AggressorBeaconActionProvider", result: AggressorContractResultNull, callback: 2},
		{name: "popup_clear", minimum: 1, maximum: 1, provider: "AggressorClientUIProvider", result: AggressorContractResultNull},
		{name: "-is64", minimum: 1, maximum: 1, provider: "AggressorSessionQueryProvider", result: AggressorContractResultPredicate},
		{name: "prompt_text", minimum: 3, maximum: 3, provider: "AggressorPromptProvider", result: AggressorContractResultNull, callback: 3, callbackArity: 1, callbackKnown: true},
		{name: "drow_listener_smb", minimum: 3, maximum: 3, provider: "AggressorDialogProvider", result: AggressorContractResultNull, deprecated: true},
		{name: "all_payloads", minimum: 3, maximum: 6, provider: "AggressorPayloadProvider", result: AggressorContractResultValue, constraints: aggressorAllPayloadsArgumentConstraints},
	}
	for _, test := range tests {
		contract, ok := contracts[test.name]
		if !ok {
			t.Errorf("missing contract %q", test.name)
			continue
		}
		if contract.MinimumArguments != test.minimum || contract.MaximumArguments != test.maximum ||
			contract.TypedProvider != test.provider || contract.TypedResult != test.result || contract.Deprecated != test.deprecated {
			t.Errorf("contract %q = %#v", test.name, contract)
		}
		if len(contract.ArgumentConstraints) != len(test.constraints) {
			t.Errorf("contract %q constraints = %#v, want %#v", test.name, contract.ArgumentConstraints, test.constraints)
		} else {
			for index, constraint := range contract.ArgumentConstraints {
				want := test.constraints[index]
				if constraint.Position != want.Position || constraint.Kind != want.Kind ||
					!slices.Equal(constraint.Values, want.Values) {
					t.Errorf("contract %q constraint %d = %#v, want %#v", test.name, index, constraint, want)
				}
			}
		}
		if test.callback == 0 {
			if len(contract.Callbacks) != 0 {
				t.Errorf("contract %q callbacks = %#v, want none", test.name, contract.Callbacks)
			}
			continue
		}
		if len(contract.Callbacks) != 1 || contract.Callbacks[0].Position != test.callback ||
			contract.Callbacks[0].ArgumentsKnown != test.callbackKnown ||
			contract.Callbacks[0].Arguments != test.callbackArity {
			t.Errorf("contract %q callbacks = %#v", test.name, contract.Callbacks)
		}
	}
}
