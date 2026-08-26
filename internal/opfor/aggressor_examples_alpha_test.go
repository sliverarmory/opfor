package opfor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// officialAggressorAlphaBehaviorRunners makes the alpha execution promise a
// machine-checked one-to-one mapping instead of a documentation-only claim.
// Each runner executes representative behavior from the named, byte-pinned
// source through inert importer adapters.
var officialAggressorAlphaBehaviorRunners = map[string]func(*testing.T){
	"bot.cna":             TestOfficialBotExampleRunsDelayedCoroutineCallbacks,
	"callany.cna":         TestOfficialCallAnyAliasReceivesUnparsedRawTail,
	"checkit.cna":         TestOfficialCheckitExampleFiresRegisteredEvent,
	"data_models.cna":     TestOfficialDataModelsExampleQueriesHostModelsAndProfile,
	"getenv.cna":          TestOfficialGetenvExampleTracksEnvironmentPerBeacon,
	"getexplorer.cna":     runOfficialGetExplorerExample,
	"getpidany.cna":       runOfficialGetPIDAnyExample,
	"initial.cna":         TestOfficialInitialExampleAdvancesPerBeaconCoroutine,
	"mkimport.cna":        TestOfficialMkImportExampleParsesInertCredentialFile,
	"mouse.cna":           TestOfficialMouseExampleRetainsForkedMouseListener,
	"oneliner.cna":        TestOfficialOnelinerExampleBuildsAndTasksInertPayload,
	"portfwd.cna":         TestOfficialPortForwardAliasesReceiveSessionAndParsedArguments,
	"random_string.cna":   TestOfficialRandomStringExampleUsesDeterministicRandOverride,
	"safedelete.cna":      TestOfficialSafeDeleteExampleRunsNestedPopupCallbacks,
	"search.cna":          TestOfficialSearchExampleQueriesBeaconLogAndPreservesFormattingBytes,
	"stagelesspython.cna": TestOfficialStagelessPythonExampleRunsSequentialArtifactCallbacks,
	"stagelessweb.cna":    TestOfficialStagelessWebExampleRunsArtifactCallback,
	"tokenToEmail.cna":    TestOfficialTokenToEmailWebHitHookFormatsResponseAndHandlerBranches,
}

func TestOfficialAggressorAlphaBehaviorCoverage(t *testing.T) {
	data, err := os.ReadFile(corpusManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest corpusManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode %s: %v", corpusManifestPath, err)
	}

	var names []string
	for _, corpus := range manifest.Corpora {
		if corpus.ID != "cobalt-strike-aggressor-script-examples" {
			continue
		}
		for _, file := range corpus.Files {
			if file.Role == "program" && filepath.Ext(file.Path) == ".cna" {
				names = append(names, filepath.Base(file.Path))
			}
		}
	}
	if len(names) != len(officialAggressorAlphaBehaviorRunners) {
		t.Fatalf("manifest sources = %d, behavior runners = %d", len(names), len(officialAggressorAlphaBehaviorRunners))
	}

	manifestNames := make(map[string]struct{}, len(names))
	for _, name := range names {
		manifestNames[name] = struct{}{}
		if _, ok := officialAggressorAlphaBehaviorRunners[name]; !ok {
			t.Errorf("official source %q has no alpha behavior runner", name)
		}
	}
	for name := range officialAggressorAlphaBehaviorRunners {
		if _, ok := manifestNames[name]; !ok {
			t.Errorf("alpha behavior runner %q has no official source", name)
		}
	}
	if t.Failed() {
		return
	}

	sort.Strings(names)
	for _, name := range names {
		name := name
		t.Run(name, officialAggressorAlphaBehaviorRunners[name])
	}
}
