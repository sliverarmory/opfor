package opfor

import (
	"encoding/json"
	"os"
	"path"
	"sort"
	"strings"
	"testing"
)

type sleepOracleCategory string

const (
	sleepOracleExecution       sleepOracleCategory = "execution"
	sleepOracleSanitizedOutput sleepOracleCategory = "sanitized-output"
	sleepOracleInertFixture    sleepOracleCategory = "inert-fixture"
	sleepOracleDiagnostic      sleepOracleCategory = "diagnostic"
)

// These programs execute byte-exactly under a mode or hermetic command
// fixture that is intentionally kept out of the central raw-output matrix.
var sleepModeSpecificExecutionOracleNames = []string{
	"backtickin",
	"taint1", "taint2", "taint3", "taint4", "taint5",
	"taint6", "taint8", "taint9", "taint10",
}

// These programs compare canonical output only after removing a
// process-, checkout-, or platform-specific identity/path fragment.
var sleepSanitizedOutputOracleNames = []string{
	"callccfork", "cast", "chdir", "convertds3", "debugce", "forker",
	"incit2", "incit3", "mapasc", "scalar", "taint7", "trace", "warn2", "wrong",
}

// btest reads a pinned JAR as opaque bytes. Its Java bytecode is never loaded.
var sleepInertFixtureOracleNames = []string{"btest"}

var sleepDiagnosticOracleNames = []string{
	"argerr", "bareword", "concaterrs",
	"errors1", "errors2", "errors3", "errors4", "errors5",
	"hoeserror", "imperror", "impfrom3", "keyvalueerr",
	"noterm", "noterm2", "sillysyntax",
}

func TestSleepOracleRegistry(t *testing.T) {
	manifestNames := readManifestSleepProgramNames(t)
	executionNames := make([]string, 0, len(sleepGoldenExecutionOracles)+len(sleepModeSpecificExecutionOracleNames))
	for _, oracle := range sleepGoldenExecutionOracles {
		executionNames = append(executionNames, oracle.name)
	}
	executionNames = append(executionNames, sleepModeSpecificExecutionOracleNames...)

	categories := []struct {
		name      sleepOracleCategory
		programs  []string
		wantCount int
	}{
		{name: sleepOracleExecution, programs: executionNames, wantCount: 312},
		{name: sleepOracleSanitizedOutput, programs: sleepSanitizedOutputOracleNames, wantCount: 14},
		{name: sleepOracleInertFixture, programs: sleepInertFixtureOracleNames, wantCount: 1},
		{name: sleepOracleDiagnostic, programs: sleepDiagnosticOracleNames, wantCount: 15},
	}

	owners := make(map[string]sleepOracleCategory, 342)
	for _, category := range categories {
		if got := len(category.programs); got != category.wantCount {
			t.Errorf("%s oracle count = %d, want %d", category.name, got, category.wantCount)
		}
		for _, name := range category.programs {
			if previous, duplicate := owners[name]; duplicate {
				t.Errorf("duplicate Sleep oracle %q in %s and %s", name, previous, category.name)
				continue
			}
			owners[name] = category.name
			if _, manifested := manifestNames[name]; !manifested {
				t.Errorf("unmanifested Sleep oracle %q in %s", name, category.name)
			}
		}
	}

	missing := make([]string, 0)
	for name := range manifestNames {
		if _, registered := owners[name]; !registered {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) != 0 {
		t.Errorf("manifested Sleep programs missing an oracle category: %s", strings.Join(missing, ", "))
	}
	if got, want := len(owners), len(manifestNames); got != want {
		t.Errorf("unique Sleep oracle count = %d, manifest program count = %d", got, want)
	}
}

func readManifestSleepProgramNames(t *testing.T) map[string]string {
	t.Helper()

	data, err := os.ReadFile(corpusManifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", corpusManifestPath, err)
	}
	var manifest corpusManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode %s: %v", corpusManifestPath, err)
	}

	const corpusID = "sleep-2.1-reference"
	var source *corpusSource
	for index := range manifest.Corpora {
		if manifest.Corpora[index].ID != corpusID {
			continue
		}
		if source != nil {
			t.Fatalf("duplicate manifest corpus %q", corpusID)
		}
		source = &manifest.Corpora[index]
	}
	if source == nil {
		t.Fatalf("manifest corpus %q is missing", corpusID)
	}

	names := make(map[string]string, 342)
	programCount := 0
	for _, file := range source.Files {
		if file.Role != "program" {
			continue
		}
		programCount++
		if path.Ext(file.Path) != ".sl" {
			t.Errorf("manifested Sleep program %q does not have a .sl extension", file.Path)
			continue
		}
		name := strings.TrimSuffix(path.Base(file.Path), ".sl")
		if previous, duplicate := names[name]; duplicate {
			t.Errorf("duplicate manifested Sleep program name %q in %q and %q", name, previous, file.Path)
			continue
		}
		names[name] = file.Path
	}
	if got, want := programCount, 342; got != want {
		t.Errorf("manifest Sleep program file count = %d, want %d", got, want)
	}
	if got, want := len(names), 342; got != want {
		t.Errorf("unique manifest Sleep program name count = %d, want %d", got, want)
	}
	return names
}
