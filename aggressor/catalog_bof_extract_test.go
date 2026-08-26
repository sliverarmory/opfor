package aggressor

import (
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestCatalogBOFExtractContractMatchesPublicRuntimeInventory(t *testing.T) {
	t.Parallel()

	var runtimeContract *opfor.AggressorFunctionContract
	for _, candidate := range opfor.DefaultAggressorFunctionContracts() {
		if candidate.Name == "bof_extract" {
			copy := candidate
			runtimeContract = &copy
			break
		}
	}
	if runtimeContract == nil {
		t.Fatal("public runtime contract inventory does not contain bof_extract")
	}
	if runtimeContract.MinimumArguments != 1 || runtimeContract.MaximumArguments != 2 ||
		runtimeContract.TypedProvider != "AggressorBOFExtractor" ||
		runtimeContract.TypedResult != opfor.AggressorContractResultValue ||
		runtimeContract.ProviderErrors != opfor.AggressorContractProviderErrorsAuthoritative ||
		!runtimeContract.HostFallback || len(runtimeContract.Callbacks) != 0 ||
		len(runtimeContract.ArgumentConstraints) != 0 || runtimeContract.Deprecated {
		t.Fatalf("public bof_extract runtime contract = %#v", *runtimeContract)
	}

	entry, ok := Lookup(KindFunction, "bof_extract")
	if !ok {
		t.Fatal("catalog does not contain bof_extract")
	}
	contract := entry.Contract
	if entry.Support != SupportHostRequired || entry.Boundary != BoundaryNativeWrapper ||
		contract.Audit != ContractAuditRuntimeEnforced ||
		contract.Confidence != ContractConfidenceExecutable || !contract.Arity.Known ||
		contract.Arity.Minimum != 1 || contract.Arity.Maximum != 2 ||
		contract.TypedProvider != "AggressorBOFExtractor" ||
		contract.ReturnShape != ReturnShapeValue ||
		contract.ProviderErrors != string(opfor.AggressorContractProviderErrorsAuthoritative) ||
		!contract.HostFallback || len(contract.Callbacks) != 0 ||
		len(contract.ArgumentConstraints) != 0 || contract.Version.Deprecated {
		t.Fatalf("catalog bof_extract entry = %#v", entry)
	}
}
