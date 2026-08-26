package opfor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const corpusManifestPath = "testdata/corpus.json"

type corpusManifest struct {
	SchemaVersion int            `json:"schema_version"`
	Description   string         `json:"description"`
	Corpora       []corpusSource `json:"corpora"`
}

type corpusSource struct {
	ID                      string             `json:"id"`
	Name                    string             `json:"name"`
	Upstream                corpusUpstream     `json:"upstream"`
	License                 corpusLicense      `json:"license"`
	Modes                   []string           `json:"modes"`
	Requirements            []string           `json:"requirements"`
	ConditionalRequirements []string           `json:"conditional_requirements"`
	Collections             []corpusCollection `json:"collections"`
	Files                   []corpusFile       `json:"files"`
}

type corpusUpstream struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	SourceURL  string `json:"source_url"`
}

type corpusLicense struct {
	SPDX   string `json:"spdx"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type corpusCollection struct {
	Role      string `json:"role"`
	Root      string `json:"root"`
	Glob      string `json:"glob"`
	Recursive bool   `json:"recursive,omitempty"`
	Count     int    `json:"count"`
}

type corpusFile struct {
	Path   string `json:"path"`
	Role   string `json:"role"`
	SHA256 string `json:"sha256"`
}

type expectedCorpus struct {
	Name          string
	Repository    string
	Commit        string
	SourceURL     string
	SPDX          string
	LicensePath   string
	LicenseHash   string
	Modes         []string
	Requirements  []string
	Conditional   []string
	Collections   map[string]expectedCollection
	InventoryHash string
}

type expectedCollection struct {
	Root      string
	Glob      string
	Recursive bool
	Count     int
}

var expectedCorpora = map[string]expectedCorpus{
	"sleep-2.1-reference": {
		Name:         "Canonical Sleep 2.1 regression programs and golden output",
		Repository:   "https://github.com/Cobalt-Strike/sleep",
		Commit:       "60ac3ff9dacc3e7b5a6c58be201c5830afbda398",
		SourceURL:    "https://github.com/Cobalt-Strike/sleep/tree/60ac3ff9dacc3e7b5a6c58be201c5830afbda398/tests",
		SPDX:         "BSD-3-Clause",
		LicensePath:  "third_party_licenses/Cobalt-Strike-sleep-BSD-3-Clause.txt",
		LicenseHash:  "9f3d9cd86bd76547e36b7c1eb576a176feb3e07801eae8823cd93219068be765",
		Modes:        []string{"parse", "execute-golden"},
		Requirements: []string{"sleep-2.1-language"},
		Conditional: []string{
			"filesystem", "java-interop", "network", "process-execution", "serialization",
			"threads-and-coroutines",
		},
		Collections: map[string]expectedCollection{
			"program": {
				Root:  "testdata/upstream/sleep-2.1/programs",
				Glob:  "*.sl",
				Count: 342,
			},
			"golden": {
				Root:  "testdata/upstream/sleep-2.1/golden",
				Glob:  "*.sl",
				Count: 342,
			},
			"fixture-data": {
				Root:  "testdata/upstream/sleep-2.1/fixtures/data",
				Glob:  "*",
				Count: 4,
			},
			"fixture-data2": {
				Root:  "testdata/upstream/sleep-2.1/fixtures/data2",
				Glob:  "*",
				Count: 2,
			},
			"fixture-data3": {
				Root:  "testdata/upstream/sleep-2.1/fixtures/data3",
				Glob:  "*",
				Count: 3,
			},
		},
		InventoryHash: "38d91f2e878671b92fda120dd923e8b81f91362f65f9e808613d947afaf39881",
	},
	"cobalt-strike-aggressor-script-examples": {
		Name:         "Official Cobalt Strike Aggressor Script examples",
		Repository:   "https://github.com/Cobalt-Strike/aggressor_script_examples",
		Commit:       "36d7514dbec82d53d23f25fe7f9e18f4af613be8",
		SourceURL:    "https://github.com/Cobalt-Strike/aggressor_script_examples/tree/36d7514dbec82d53d23f25fe7f9e18f4af613be8",
		SPDX:         "Apache-2.0",
		LicensePath:  "third_party_licenses/Cobalt-Strike-aggressor_script_examples-Apache-2.0.txt",
		LicenseHash:  "c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4",
		Modes:        []string{"parse", "load-with-recording-host", "mock-execute"},
		Requirements: []string{"aggressor-environment-bridges", "cobalt-strike-host-api-mocks"},
		Conditional: []string{
			"event-dispatch", "filesystem", "gui-bridge", "java-object-bridge", "threads-and-coroutines",
		},
		Collections: map[string]expectedCollection{
			"program": {
				Root:  "testdata/upstream/aggressor-script-examples",
				Glob:  "*.cna",
				Count: 18,
			},
		},
		InventoryHash: "7281018a609ce16c06b0e80a5584cf94cd3b6fd83ef6e3bfd119354f32ecca3b",
	},
}

func TestCorpusProvenance(t *testing.T) {
	data, err := os.ReadFile(corpusManifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", corpusManifestPath, err)
	}

	var manifest corpusManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode %s: %v", corpusManifestPath, err)
	}
	if manifest.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.Description) == "" {
		t.Fatal("manifest description is empty")
	}
	if got, want := len(manifest.Corpora), len(expectedCorpora); got != want {
		t.Fatalf("corpus count = %d, want %d", got, want)
	}

	seenCorpora := make(map[string]struct{}, len(manifest.Corpora))
	seenFiles := make(map[string]string)
	totalFiles := 0
	for _, source := range manifest.Corpora {
		expected, ok := expectedCorpora[source.ID]
		if !ok {
			t.Errorf("unexpected corpus %q", source.ID)
			continue
		}
		if _, duplicate := seenCorpora[source.ID]; duplicate {
			t.Errorf("duplicate corpus %q", source.ID)
			continue
		}
		seenCorpora[source.ID] = struct{}{}
		t.Run(source.ID, func(t *testing.T) {
			verifyCorpusSource(t, source, expected, seenFiles)
		})
		totalFiles += len(source.Files)
	}
	for id := range expectedCorpora {
		if _, ok := seenCorpora[id]; !ok {
			t.Errorf("required corpus %q is missing", id)
		}
	}
	if totalFiles != 711 {
		t.Errorf("total vendored corpus file count = %d, want 711", totalFiles)
	}
}

// TestCNACorpusWorktreeBoundary makes the user's corpus policy executable:
// every worktree .cna file must be one of the 18 manifest-pinned files from
// Cobalt-Strike/aggressor_script_examples. OPFOR-authored conformance snippets
// remain inline in Go tests and use synthetic source names rather than adding
// another external script corpus.
func TestCNACorpusWorktreeBoundary(t *testing.T) {
	data, err := os.ReadFile(corpusManifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", corpusManifestPath, err)
	}
	var manifest corpusManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode %s: %v", corpusManifestPath, err)
	}

	const approvedID = "cobalt-strike-aggressor-script-examples"
	const approvedRoot = "testdata/upstream/aggressor-script-examples/"
	approved := make(map[string]struct{})
	for _, source := range manifest.Corpora {
		if source.ID != approvedID {
			continue
		}
		for _, file := range source.Files {
			if file.Role != "program" || path.Ext(file.Path) != ".cna" {
				continue
			}
			if !strings.HasPrefix(file.Path, approvedRoot) {
				t.Fatalf("approved manifest contains out-of-root .cna path %q", file.Path)
			}
			approved[file.Path] = struct{}{}
		}
	}
	if got, want := len(approved), 18; got != want {
		t.Fatalf("approved manifest .cna count = %d, want %d", got, want)
	}

	discovered := make(map[string]struct{})
	err = filepath.WalkDir(".", func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (name == ".git" || strings.HasPrefix(filepath.ToSlash(name), ".git/")) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(name), ".cna") {
			return nil
		}
		repositoryPath := strings.TrimPrefix(filepath.ToSlash(name), "./")
		discovered[repositoryPath] = struct{}{}
		if !strings.HasPrefix(repositoryPath, approvedRoot) {
			t.Errorf("worktree .cna path %q is outside the approved corpus root", repositoryPath)
			return nil
		}
		if _, ok := approved[repositoryPath]; !ok {
			t.Errorf("worktree .cna path %q is absent from the approved manifest", repositoryPath)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("discover worktree .cna files: %v", err)
	}
	for name := range approved {
		if _, ok := discovered[name]; !ok {
			t.Errorf("approved manifest .cna path %q is missing from the worktree", name)
		}
	}
}

func verifyCorpusSource(t *testing.T, source corpusSource, expected expectedCorpus, globallySeen map[string]string) {
	t.Helper()

	if source.Name != expected.Name {
		t.Errorf("name = %q, want %q", source.Name, expected.Name)
	}
	if source.Upstream.Repository != expected.Repository {
		t.Errorf("repository = %q, want %q", source.Upstream.Repository, expected.Repository)
	}
	if source.Upstream.Commit != expected.Commit {
		t.Errorf("commit = %q, want %q", source.Upstream.Commit, expected.Commit)
	}
	if source.Upstream.SourceURL != expected.SourceURL {
		t.Errorf("source_url = %q, want %q", source.Upstream.SourceURL, expected.SourceURL)
	}
	if source.License.SPDX != expected.SPDX {
		t.Errorf("license SPDX = %q, want %q", source.License.SPDX, expected.SPDX)
	}
	if source.License.Path != expected.LicensePath {
		t.Errorf("license path = %q, want %q", source.License.Path, expected.LicensePath)
	}
	if source.License.SHA256 != expected.LicenseHash {
		t.Errorf("license hash = %q, want %q", source.License.SHA256, expected.LicenseHash)
	}
	if !reflect.DeepEqual(source.Modes, expected.Modes) {
		t.Errorf("modes = %q, want %q", source.Modes, expected.Modes)
	}
	if !reflect.DeepEqual(source.Requirements, expected.Requirements) {
		t.Errorf("requirements = %q, want %q", source.Requirements, expected.Requirements)
	}
	if !reflect.DeepEqual(source.ConditionalRequirements, expected.Conditional) {
		t.Errorf("conditional_requirements = %q, want %q", source.ConditionalRequirements, expected.Conditional)
	}
	verifyRegularFileHash(t, source.License.Path, source.License.SHA256)

	collections := make(map[string]corpusCollection, len(source.Collections))
	expectedFileCount := 0
	for _, item := range source.Collections {
		want, ok := expected.Collections[item.Role]
		if !ok {
			t.Errorf("unexpected collection role %q", item.Role)
			continue
		}
		if _, duplicate := collections[item.Role]; duplicate {
			t.Errorf("duplicate collection role %q", item.Role)
			continue
		}
		collections[item.Role] = item
		if item.Root != want.Root || item.Glob != want.Glob || item.Recursive != want.Recursive || item.Count != want.Count {
			t.Errorf("collection %q = (%q, %q, recursive=%t, %d), want (%q, %q, recursive=%t, %d)",
				item.Role, item.Root, item.Glob, item.Recursive, item.Count,
				want.Root, want.Glob, want.Recursive, want.Count)
		}
		expectedFileCount += want.Count
	}
	if len(collections) != len(expected.Collections) {
		t.Errorf("collection count = %d, want %d", len(collections), len(expected.Collections))
	}
	if len(source.Files) != expectedFileCount {
		t.Errorf("file manifest count = %d, want %d", len(source.Files), expectedFileCount)
	}

	inventory := append([]corpusFile(nil), source.Files...)
	sort.Slice(inventory, func(i, j int) bool { return inventory[i].Path < inventory[j].Path })
	hasher := sha256.New()
	for _, item := range inventory {
		fmt.Fprintf(hasher, "%s\t%s\t%s\n", item.Role, item.Path, item.SHA256)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != expected.InventoryHash {
		t.Errorf("inventory hash = %s, want %s", got, expected.InventoryHash)
	}

	listedByCollection := make(map[string]map[string]struct{}, len(collections))
	for role := range collections {
		listedByCollection[role] = make(map[string]struct{})
	}
	for _, item := range source.Files {
		if owner, duplicate := globallySeen[item.Path]; duplicate {
			t.Errorf("path %q is also owned by corpus %q", item.Path, owner)
			continue
		}
		globallySeen[item.Path] = source.ID

		if err := validateManifestPath(item.Path); err != nil {
			t.Errorf("invalid path %q: %v", item.Path, err)
			continue
		}
		collection, ok := collections[item.Role]
		if !ok {
			t.Errorf("file %q has unknown role %q", item.Path, item.Role)
			continue
		}
		if collection.Recursive {
			if !strings.HasPrefix(item.Path, collection.Root+"/") {
				t.Errorf("file %q is not beneath recursive collection %q", item.Path, collection.Root)
				continue
			}
		} else {
			if path.Dir(item.Path) != collection.Root {
				t.Errorf("file %q is not a direct child of %q", item.Path, collection.Root)
				continue
			}
		}
		matched, err := path.Match(collection.Glob, path.Base(item.Path))
		if err != nil {
			t.Fatalf("invalid collection glob %q: %v", collection.Glob, err)
		}
		if !matched {
			t.Errorf("file %q does not match %q", item.Path, collection.Glob)
			continue
		}
		if _, duplicate := listedByCollection[item.Role][item.Path]; duplicate {
			t.Errorf("file %q appears more than once", item.Path)
			continue
		}
		listedByCollection[item.Role][item.Path] = struct{}{}
		verifyRegularFileHash(t, item.Path, item.SHA256)
	}

	for role, item := range collections {
		actual := readCollection(t, item)
		listed := listedByCollection[role]
		if len(actual) != item.Count {
			t.Errorf("collection %q on-disk count = %d, want %d", role, len(actual), item.Count)
		}
		if len(listed) != item.Count {
			t.Errorf("collection %q listed count = %d, want %d", role, len(listed), item.Count)
		}
		for name := range actual {
			if _, ok := listed[name]; !ok {
				t.Errorf("unmanifested file %q", name)
			}
		}
		for name := range listed {
			if _, ok := actual[name]; !ok {
				t.Errorf("manifest path %q is absent from collection", name)
			}
		}
	}

	switch source.ID {
	case "sleep-2.1-reference":
		programs := collectionBaseNames(listedByCollection["program"])
		golden := collectionBaseNames(listedByCollection["golden"])
		if !reflect.DeepEqual(programs, golden) {
			t.Error("Sleep program and golden filename sets differ")
		}
	case "cobalt-strike-aggressor-script-examples":
		want := []string{
			"bot.cna", "callany.cna", "checkit.cna", "data_models.cna", "getenv.cna",
			"getexplorer.cna", "getpidany.cna", "initial.cna", "mkimport.cna", "mouse.cna",
			"oneliner.cna", "portfwd.cna", "random_string.cna", "safedelete.cna", "search.cna",
			"stagelesspython.cna", "stagelessweb.cna", "tokenToEmail.cna",
		}
		got := collectionBaseNames(listedByCollection["program"])
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Aggressor example names = %q, want %q", got, want)
		}
	}
}

func validateManifestPath(name string) error {
	if name == "" {
		return fmt.Errorf("empty path")
	}
	if path.IsAbs(name) || path.Clean(name) != name {
		return fmt.Errorf("path must be clean and relative")
	}
	if name == ".." || strings.HasPrefix(name, "../") {
		return fmt.Errorf("path escapes the repository")
	}
	if strings.ContainsRune(name, '\\') {
		return fmt.Errorf("path must use forward slashes")
	}
	return nil
}

func verifyRegularFileHash(t *testing.T, name, wantHash string) {
	t.Helper()
	if err := validateManifestPath(name); err != nil {
		t.Errorf("invalid path %q: %v", name, err)
		return
	}
	decoded, err := hex.DecodeString(wantHash)
	if err != nil || len(decoded) != sha256.Size || wantHash != strings.ToLower(wantHash) {
		t.Errorf("invalid SHA-256 for %q: %q", name, wantHash)
		return
	}
	info, err := os.Lstat(filepath.FromSlash(name))
	if err != nil {
		t.Errorf("stat %q: %v", name, err)
		return
	}
	if !info.Mode().IsRegular() {
		t.Errorf("%q mode = %s, want a regular file", name, info.Mode())
		return
	}
	data, err := os.ReadFile(filepath.FromSlash(name))
	if err != nil {
		t.Errorf("read %q: %v", name, err)
		return
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != wantHash {
		t.Errorf("SHA-256(%q) = %s, want %s", name, got, wantHash)
	}
}

func readCollection(t *testing.T, item corpusCollection) map[string]struct{} {
	t.Helper()
	if item.Recursive {
		return readRecursiveCollection(t, item)
	}
	entries, err := os.ReadDir(filepath.FromSlash(item.Root))
	if err != nil {
		t.Fatalf("read collection %q: %v", item.Root, err)
	}
	result := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := path.Join(item.Root, entry.Name())
		if entry.IsDir() {
			t.Errorf("unexpected directory %q", name)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			t.Errorf("stat %q: %v", name, err)
			continue
		}
		if !info.Mode().IsRegular() {
			t.Errorf("unexpected non-regular file %q", name)
			continue
		}
		matched, err := path.Match(item.Glob, entry.Name())
		if err != nil {
			t.Fatalf("invalid collection glob %q: %v", item.Glob, err)
		}
		if !matched {
			t.Errorf("unexpected file %q does not match %q", name, item.Glob)
			continue
		}
		result[name] = struct{}{}
	}
	return result
}

func readRecursiveCollection(t *testing.T, item corpusCollection) map[string]struct{} {
	t.Helper()
	root := filepath.FromSlash(item.Root)
	result := make(map[string]struct{}, item.Count)
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == root || entry.IsDir() {
			return nil
		}
		repositoryPath := filepath.ToSlash(name)
		info, err := entry.Info()
		if err != nil {
			t.Errorf("stat %q: %v", repositoryPath, err)
			return nil
		}
		if !info.Mode().IsRegular() {
			t.Errorf("unexpected non-regular file %q", repositoryPath)
			return nil
		}
		matched, err := path.Match(item.Glob, path.Base(repositoryPath))
		if err != nil {
			return fmt.Errorf("invalid collection glob %q: %w", item.Glob, err)
		}
		if !matched {
			t.Errorf("unexpected file %q does not match %q", repositoryPath, item.Glob)
			return nil
		}
		result[repositoryPath] = struct{}{}
		return nil
	})
	if err != nil {
		t.Fatalf("read recursive collection %q: %v", item.Root, err)
	}
	return result
}

func collectionBaseNames(files map[string]struct{}) []string {
	result := make([]string, 0, len(files))
	for name := range files {
		result = append(result, path.Base(name))
	}
	sort.Strings(result)
	return result
}
