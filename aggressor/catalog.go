// Package aggressor provides the documented Aggressor Script compatibility
// catalog and importer adapters for OPFOR's public host interfaces. Catalog
// support describes routing availability, not end-to-end Cobalt behavior or
// complete Sleep standard-library fidelity.
package aggressor

import (
	"sort"
	"strings"
	"sync"

	"github.com/sliverarmory/opfor"
)

const (
	// CatalogSchemaVersion identifies the JSON-compatible catalog schema.
	CatalogSchemaVersion = 3
	// OfficialSnapshotDate is the research date of the embedded official names.
	OfficialSnapshotDate = "2026-08-23"
	// OfficialFunctionCount is the number of unique function/predicate headings
	// in the researched official documentation snapshot.
	OfficialFunctionCount = 436
	// OfficialEventCount is the number of unique event headings, including *.
	OfficialEventCount = 56
	// OfficialHookCount is the number of unique hook headings.
	OfficialHookCount = 80
	// OfficialPopupHookCount is the number of names in the separately
	// documented popup-hook table.
	OfficialPopupHookCount = 18
	// OfficialNamesSHA256 covers the canonical kind-tab-name-newline inventory
	// in function, event, hook, and popup-hook order. It was computed from the
	// independently extracted documentation snapshot.
	OfficialNamesSHA256 = "85264cd141f4c28a9bf2b629fbc768123c28583f798b99dbff976b76a96a75e8"
)

const (
	officialFunctionsURL      = "https://hstechdocs.helpsystems.com/manuals/cobaltstrike/current/userguide/content/topics_aggressor-scripts/as-resources_functions.htm"
	officialEventsURL         = "https://hstechdocs.helpsystems.com/manuals/cobaltstrike/current/userguide/content/topics_aggressor-scripts/as-resources_events.htm"
	officialHooksURL          = "https://hstechdocs.helpsystems.com/manuals/cobaltstrike/current/userguide/content/topics_aggressor-scripts/as-resources_hooks.htm"
	officialPopupHooksURL     = "https://hstechdocs.helpsystems.com/manuals/cobaltstrike/current/userguide/content/topics_aggressor-scripts/as-resources_popup-hooks.htm"
	officialSSHSessionsURL    = "https://hstechdocs.helpsystems.com/manuals/cobaltstrike/current/userguide/content/topics_aggressor-scripts/as_ssh-sessions.htm"
	officialSSHSessionsSHA256 = "df657c6c4d3662e41a448265067ce1781440e26e696d9db0b66e0a3f915da9ff"
	officialExamplesRoot      = "testdata/upstream/aggressor-script-examples/"
)

// EntryKind identifies an Aggressor compatibility surface.
type EntryKind string

const (
	// KindFunction identifies a function or predicate entry.
	KindFunction EntryKind = "function"
	// KindEvent identifies an event entry.
	KindEvent EntryKind = "event"
	// KindHook identifies a set-hook entry.
	KindHook EntryKind = "hook"
	// KindPopupHook identifies a popup-hook entry.
	KindPopupHook EntryKind = "popup-hook"
)

// Match identifies how a documented name matches a concrete ABI name.
type Match string

const (
	// MatchExact requires a case-sensitive exact name.
	MatchExact Match = "exact"
	// MatchPrefix identifies a documented family of names sharing a prefix.
	MatchPrefix Match = "prefix"
)

// Support classifies how an entry is provided by OPFOR.
type Support string

const (
	// SupportPortableDefault means the base pure-Go runtime implements the entry.
	SupportPortableDefault Support = "portable-default"
	// SupportHostRequired means OPFOR provides an adapter/dispatch boundary and
	// the embedding application must supply the Cobalt-specific behavior.
	SupportHostRequired Support = "host-required"
	// SupportUnsupported means no compatible implementation or adapter boundary
	// currently exists. The evidence-backed catalog has no such entries today;
	// the value is reserved so omissions are represented explicitly as coverage
	// grows rather than being mislabeled host-required.
	SupportUnsupported Support = "unsupported"
)

// Boundary identifies the OPFOR routing surface behind a catalog entry. It is
// intentionally independent from Support: a host-required entry may have a
// validated native wrapper or may route directly to the generic Host, while
// events and hooks are registered and invoked through OPFOR's binding
// dispatcher.
type Boundary string

const (
	// BoundaryPortableRuntime means OPFOR completes the operation in its
	// portable pure-Go runtime without an importer callback.
	BoundaryPortableRuntime Boundary = "portable-runtime"
	// BoundaryNativeWrapper means OPFOR owns a name-specific native wrapper with
	// the validation and typed/fallback routing documented by that wrapper.
	BoundaryNativeWrapper Boundary = "native-wrapper"
	// BoundaryGenericHost means OPFOR has no function-specific native wrapper;
	// the invocation is routed directly to the importer's generic Host.
	BoundaryGenericHost Boundary = "generic-host"
	// BoundaryBindingDispatch means OPFOR owns callback registration, lifetime,
	// and dispatch while the importer supplies the external event, hook, or UI
	// trigger and any Cobalt-owned context.
	BoundaryBindingDispatch Boundary = "binding-dispatch"
)

// Evidence identifies the source used to include or classify an entry.
type Evidence struct {
	Source    string `json:"source"`
	Reference string `json:"reference"`
	SHA256    string `json:"sha256,omitempty"`
}

// ContractAudit identifies how much of an entry's callable contract has been
// independently described. Name-only entries are routing inventory, not a
// behavioral-compatibility claim.
type ContractAudit string

const (
	// ContractAuditNameOnly means OPFOR knows and routes the name, but one or
	// more callable-contract fields still lack an executable/source audit.
	ContractAuditNameOnly ContractAudit = "name-only"
	// ContractAuditRuntimeEnforced means the metadata is derived from the same
	// native wrapper specifications that enforce the typed-provider boundary.
	ContractAuditRuntimeEnforced ContractAudit = "runtime-enforced"
)

// ContractConfidence describes what the contract metadata itself proves.
type ContractConfidence string

const (
	// ContractConfidenceInventory means only name and routing presence are
	// evidenced.
	ContractConfidenceInventory ContractConfidence = "inventory"
	// ContractConfidenceExecutable means the shape is exercised by OPFOR's
	// native wrapper and conformance tests.
	ContractConfidenceExecutable ContractConfidence = "executable"
)

// ReturnShape describes the value visible to script code on successful typed
// provider completion.
type ReturnShape string

const (
	ReturnShapeUnknown   ReturnShape = "unknown"
	ReturnShapeValue     ReturnShape = "value"
	ReturnShapeNull      ReturnShape = "null"
	ReturnShapePredicate ReturnShape = "predicate"
)

// SoftErrorPolicy describes documented non-fatal error behavior. Unknown is
// explicit: an error-returning importer API is not evidence of Cobalt's
// script-visible soft-error behavior.
type SoftErrorPolicy string

const (
	SoftErrorPolicyUnknown SoftErrorPolicy = "unknown"
)

// ArityContract is an inclusive positional argument range. Minimum and
// Maximum are meaningful only when Known is true.
type ArityContract struct {
	Known   bool `json:"known"`
	Minimum int  `json:"minimum"`
	Maximum int  `json:"maximum"`
}

// CallbackContract describes a callback-shaped source argument. Position is
// one-based. Arguments is meaningful only when ArgumentsKnown is true.
type CallbackContract struct {
	Position       int  `json:"position"`
	Required       bool `json:"required"`
	Nullable       bool `json:"nullable"`
	Retained       bool `json:"retained"`
	ArgumentsKnown bool `json:"arguments_known"`
	Arguments      int  `json:"arguments"`
}

// ArgumentConstraint reserves a machine-readable slot for source-audited
// enum, range, and type constraints. An empty list means they are not yet
// audited; it must not be interpreted as accepting every value. For enum
// constraints, "$null" denotes the null Value and an empty Values element
// denotes a non-null blank string.
type ArgumentConstraint struct {
	Position int      `json:"position"`
	Kind     string   `json:"kind"`
	Values   []string `json:"values"`
	Minimum  string   `json:"minimum"`
	Maximum  string   `json:"maximum"`
}

// VersionContract records documented lifecycle information. Empty version
// strings are unknown, not assertions that the entry has always existed.
type VersionContract struct {
	Introduced string `json:"introduced"`
	Deprecated bool   `json:"deprecated"`
	Removed    string `json:"removed"`
}

// Contract separates name/routing coverage from callable compatibility. The
// v3 schema intentionally emits unknown and empty fields so downstream tools
// cannot mistake an omitted audit for a permissive contract.
type Contract struct {
	Audit               ContractAudit        `json:"audit"`
	Arity               ArityContract        `json:"arity"`
	Callbacks           []CallbackContract   `json:"callbacks"`
	ArgumentConstraints []ArgumentConstraint `json:"argument_constraints"`
	ReturnShape         ReturnShape          `json:"return_shape"`
	SoftErrorPolicy     SoftErrorPolicy      `json:"soft_error_policy"`
	Version             VersionContract      `json:"version"`
	Confidence          ContractConfidence   `json:"confidence"`
	TypedProvider       string               `json:"typed_provider"`
	ProviderErrors      string               `json:"provider_errors"`
	HostFallback        bool                 `json:"host_fallback"`
}

// SourceSnapshot records a documentation input used to extract official
// names. Page hashes make the evidence boundary reproducible even if the live
// documentation changes.
type SourceSnapshot struct {
	Kind   EntryKind `json:"kind"`
	URL    string    `json:"url"`
	SHA256 string    `json:"sha256"`
	Count  int       `json:"count"`
}

// Entry is one machine-readable compatibility claim.
type Entry struct {
	Name        string     `json:"name"`
	Kind        EntryKind  `json:"kind"`
	Match       Match      `json:"match"`
	MatchValue  string     `json:"match_value,omitempty"`
	Support     Support    `json:"support"`
	Boundary    Boundary   `json:"boundary"`
	Description string     `json:"description,omitempty"`
	Evidence    []Evidence `json:"evidence,omitempty"`
	Contract    Contract   `json:"contract"`
}

// Matches reports whether a concrete case-sensitive ABI name is represented
// by this entry.
func (entry Entry) Matches(name string) bool {
	switch entry.Match {
	case MatchPrefix:
		return len(name) > len(entry.MatchValue) && strings.HasPrefix(name, entry.MatchValue)
	case MatchExact, "":
		return entry.Name == name
	default:
		return false
	}
}

// Counts summarizes the embedded official documentation inventory. Additional
// sample-only compatibility entries are not included in these counts.
type Counts struct {
	Functions  int `json:"functions"`
	Events     int `json:"events"`
	Hooks      int `json:"hooks"`
	PopupHooks int `json:"popup_hooks"`
}

// CatalogSnapshot is a deterministic, JSON-compatible catalog snapshot.
type CatalogSnapshot struct {
	SchemaVersion    int              `json:"schema_version"`
	SnapshotDate     string           `json:"snapshot_date"`
	NamesSHA256      string           `json:"names_sha256"`
	EvidenceBoundary string           `json:"evidence_boundary"`
	OfficialCounts   Counts           `json:"official_counts"`
	Sources          []SourceSnapshot `json:"sources"`
	Entries          []Entry          `json:"entries"`
}

var (
	catalogOnce  sync.Once
	catalogValue CatalogSnapshot
)

// Catalog returns an independent copy of the compatibility catalog. Entries
// are sorted first by kind and then by name.
func Catalog() CatalogSnapshot {
	return cloneCatalog(*internalCatalog())
}

// Lookup returns the catalog entry matching a concrete case-sensitive
// Aggressor name. Most entries match exactly; documented name families such as
// custom_event_<event name> use their machine-readable prefix rule.
func Lookup(kind EntryKind, name string) (Entry, bool) {
	catalog := internalCatalog()
	index := sort.Search(len(catalog.Entries), func(index int) bool {
		entry := catalog.Entries[index]
		if entry.Kind != kind {
			return entry.Kind >= kind
		}
		return entry.Name >= name
	})
	if index < len(catalog.Entries) && catalog.Entries[index].Kind == kind && catalog.Entries[index].Name == name {
		return cloneEntry(catalog.Entries[index]), true
	}
	for _, entry := range catalog.Entries {
		if entry.Kind == kind && entry.Match == MatchPrefix && entry.Matches(name) {
			return cloneEntry(entry), true
		}
	}
	return Entry{}, false
}

// Filter returns entries matching kind and support. An empty kind or support
// acts as a wildcard.
func Filter(kind EntryKind, support Support) []Entry {
	catalog := internalCatalog()
	entries := make([]Entry, 0)
	for _, entry := range catalog.Entries {
		if kind != "" && entry.Kind != kind {
			continue
		}
		if support != "" && entry.Support != support {
			continue
		}
		entries = append(entries, cloneEntry(entry))
	}
	return entries
}

// FilterByBoundary returns entries matching kind and routing boundary. An
// empty kind or boundary acts as a wildcard. It complements Filter without
// changing that function's existing support-based API.
func FilterByBoundary(kind EntryKind, boundary Boundary) []Entry {
	catalog := internalCatalog()
	entries := make([]Entry, 0)
	for _, entry := range catalog.Entries {
		if kind != "" && entry.Kind != kind {
			continue
		}
		if boundary != "" && entry.Boundary != boundary {
			continue
		}
		entries = append(entries, cloneEntry(entry))
	}
	return entries
}

func internalCatalog() *CatalogSnapshot {
	catalogOnce.Do(func() {
		catalogValue = buildCatalog()
	})
	return &catalogValue
}

type catalogKey struct {
	kind EntryKind
	name string
}

func buildCatalog() CatalogSnapshot {
	entries := make(map[catalogKey]Entry, len(officialFunctionNames)+len(officialEventNames)+len(officialHookNames)+len(officialPopupHookNames))
	addOfficial := func(kind EntryKind, names []string, source, digest string) {
		for _, name := range names {
			description := officialDescription(kind)
			if kind == KindHook {
				if specific, ok := coreInvokedHookDescriptions[name]; ok {
					description = specific
				}
			}
			entry := Entry{
				Name:        name,
				Kind:        kind,
				Match:       MatchExact,
				Support:     SupportHostRequired,
				Boundary:    defaultBoundary(kind),
				Description: description,
				Evidence: []Evidence{{
					Source:    "official-documentation",
					Reference: source,
					SHA256:    digest,
				}},
			}
			if kind == KindEvent && name == "custom_event_<event name>" {
				entry.Match = MatchPrefix
				entry.MatchValue = "custom_event_"
			}
			entries[catalogKey{kind: kind, name: name}] = entry
		}
	}
	addOfficial(KindFunction, officialFunctionNames, officialFunctionsURL, officialFunctionsSHA256)
	addOfficial(KindEvent, officialEventNames, officialEventsURL, officialEventsSHA256)
	addOfficial(KindHook, officialHookNames, officialHooksURL, officialHooksSHA256)
	addOfficial(KindPopupHook, officialPopupHookNames, officialPopupHooksURL, officialPopupHooksSHA256)
	// The current function page retains headings for several functions it also
	// explicitly labels REMOVED. Preserve those names and their official-source
	// evidence without describing them as current callable contracts.
	for name, description := range removedOfficialFunctionDescriptions {
		key := catalogKey{kind: KindFunction, name: name}
		entry, exists := entries[key]
		if !exists {
			panic("removed Aggressor function is absent from the official catalog: " + name)
		}
		entry.Description = description
		entry.Contract.Version.Removed = removedOfficialFunctionVersions[name]
		entries[key] = entry
	}
	// The ssh_alias function form is permitted by the official SSH sessions
	// guide, but has no heading in the independently hashed function inventory.
	// Keep that distinction explicit without changing the official heading count
	// or digest.
	entries[catalogKey{kind: KindFunction, name: "ssh_alias"}] = Entry{
		Name:        "ssh_alias",
		Kind:        KindFunction,
		Match:       MatchExact,
		Support:     SupportHostRequired,
		Boundary:    BoundaryGenericHost,
		Description: "Officially guide-documented SSH alias registration function.",
		Evidence: []Evidence{{
			Source:    "official-documentation-guide",
			Reference: officialSSHSessionsURL,
			SHA256:    officialSSHSessionsSHA256,
		}},
	}

	portableFunctions := make(map[string]string, len(portableDefaultFunctions))
	for name, description := range portableDefaultFunctions {
		portableFunctions[name] = description
	}
	for _, name := range opfor.DefaultFunctionNames() {
		if _, hostRequired := hostRequiredRuntimeFunctions[name]; hostRequired {
			continue
		}
		if _, described := portableFunctions[name]; !described {
			portableFunctions[name] = "Portable pure-Go Sleep runtime function."
		}
	}
	for name, description := range portableFunctions {
		if _, hostRequired := hostRequiredRuntimeFunctions[name]; hostRequired {
			continue
		}
		key := catalogKey{kind: KindFunction, name: name}
		entry, exists := entries[key]
		if !exists {
			entry = Entry{Name: name, Kind: KindFunction, Match: MatchExact}
		}
		entry.Support = SupportPortableDefault
		entry.Boundary = BoundaryPortableRuntime
		entry.Description = description
		entry.Evidence = appendUniqueEvidence(entry.Evidence, Evidence{
			Source:    "opfor-runtime",
			Reference: "portable Sleep/runtime implementation",
		})
		entries[key] = entry
	}
	// DefaultFunctionNames includes native wrappers as well as functions that
	// complete entirely in the portable runtime. Apply host-required overrides
	// after automatic discovery so wrappers are not mistaken for portable support.
	for name, description := range hostRequiredRuntimeFunctions {
		key := catalogKey{kind: KindFunction, name: name}
		entry, exists := entries[key]
		if !exists {
			entry = Entry{Name: name, Kind: KindFunction, Match: MatchExact}
		}
		entry.Support = SupportHostRequired
		entry.Boundary = BoundaryNativeWrapper
		entry.Description = description
		entry.Evidence = appendUniqueEvidence(entry.Evidence, Evidence{
			Source:    "opfor-runtime",
			Reference: "native importer-boundary wrapper",
		})
		entries[key] = entry
	}
	// Enrich only the function-specific typed-provider wrappers whose arity,
	// callback, and result behavior is enforced by the runtime. Other native and
	// generic Host routes remain explicitly name-only until separately audited.
	for _, runtimeContract := range opfor.DefaultAggressorFunctionContracts() {
		key := catalogKey{kind: KindFunction, name: runtimeContract.Name}
		entry, exists := entries[key]
		if !exists {
			panic("native Aggressor function contract is absent from the catalog: " + runtimeContract.Name)
		}
		if entry.Boundary != BoundaryNativeWrapper {
			panic("native Aggressor function contract is not classified as a native wrapper: " + runtimeContract.Name)
		}
		entry.Contract = catalogContractFromRuntime(runtimeContract)
		entries[key] = entry
	}

	for key, files := range officialExampleEvidence {
		entry, exists := entries[key]
		if !exists {
			entry = Entry{
				Name:        key.name,
				Kind:        key.kind,
				Match:       MatchExact,
				Support:     SupportHostRequired,
				Boundary:    defaultBoundary(key.kind),
				Description: "Dependency observed in the vendored official Aggressor Script examples.",
			}
		}
		for _, file := range files {
			entry.Evidence = appendUniqueEvidence(entry.Evidence, Evidence{
				Source:    "official-example-corpus",
				Reference: officialExamplesRoot + file,
			})
		}
		entries[key] = entry
	}

	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Boundary == "" {
			entry.Boundary = defaultBoundary(entry.Kind)
		}
		entry.Contract = normalizeContract(entry.Contract)
		sort.Slice(entry.Evidence, func(i, j int) bool {
			if entry.Evidence[i].Source != entry.Evidence[j].Source {
				return entry.Evidence[i].Source < entry.Evidence[j].Source
			}
			return entry.Evidence[i].Reference < entry.Evidence[j].Reference
		})
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Name < result[j].Name
	})

	return CatalogSnapshot{
		SchemaVersion: CatalogSchemaVersion,
		SnapshotDate:  OfficialSnapshotDate,
		NamesSHA256:   OfficialNamesSHA256,
		EvidenceBoundary: strings.Join([]string{
			"Official names come from the documentation snapshot pinned by the catalog source URLs, hashes, and snapshot date.",
			"Guide-documented functions without reference-page headings are cataloged separately and do not alter the official heading counts or digest.",
			"External per-script evidence is limited to the 18 commit-pinned Cobalt-Strike/aggressor_script_examples files in testdata/corpus.json.",
			"Other behavior coverage uses OPFOR-authored inline snippets and is not presented as external corpus evidence.",
			"A host-required entry means a stable OPFOR callback/dispatch boundary exists; it does not claim a Cobalt client or Team Server is present.",
			"The boundary field distinguishes portable execution, name-specific native wrappers, generic Host routing, and callback binding/dispatch without changing the support classification.",
			"Contract audit=name-only is routing inventory, not behavioral compatibility; runtime-enforced contracts are derived from the native wrapper specifications and still do not prove the external Cobalt effect.",
		}, " "),
		OfficialCounts: Counts{
			Functions:  len(officialFunctionNames),
			Events:     len(officialEventNames),
			Hooks:      len(officialHookNames),
			PopupHooks: len(officialPopupHookNames),
		},
		Sources: []SourceSnapshot{
			{Kind: KindFunction, URL: officialFunctionsURL, SHA256: officialFunctionsSHA256, Count: len(officialFunctionNames)},
			{Kind: KindEvent, URL: officialEventsURL, SHA256: officialEventsSHA256, Count: len(officialEventNames)},
			{Kind: KindHook, URL: officialHooksURL, SHA256: officialHooksSHA256, Count: len(officialHookNames)},
			{Kind: KindPopupHook, URL: officialPopupHooksURL, SHA256: officialPopupHooksSHA256, Count: len(officialPopupHookNames)},
		},
		Entries: result,
	}
}

func defaultBoundary(kind EntryKind) Boundary {
	switch kind {
	case KindEvent, KindHook, KindPopupHook:
		return BoundaryBindingDispatch
	default:
		return BoundaryGenericHost
	}
}

func officialDescription(kind EntryKind) string {
	switch kind {
	case KindEvent:
		return "Officially documented Aggressor event; the embedding host must emit it."
	case KindHook:
		return "Officially documented Aggressor hook; the embedding host must invoke it."
	case KindPopupHook:
		return "Officially documented Aggressor popup hook; the embedding host must supply the UI context."
	default:
		return "Officially documented Aggressor function or predicate; Cobalt-specific behavior is supplied by the embedding host."
	}
}

var coreInvokedHookDescriptions = map[string]string{
	"BEACON_INLINE_EXECUTE": "Official Aggressor hook invoked by OPFOR's beacon_inline_execute wrapper before any execution provider or Host fallback.",
	"POWERSHELL_COMMAND":    "Official Aggressor hook invoked by OPFOR's portable powershell_command implementation before its default formatting.",
	"POWERSHELL_COMPRESS":   "Official Aggressor hook invoked by OPFOR's powershell_compress wrapper before any code-transform provider or Host fallback.",
}

var removedOfficialFunctionDescriptions = map[string]string{
	"bbypassuac":               "Removed legacy Beacon function (Cobalt Strike 4.0); retained only as a generic Host compatibility name with no native wrapper.",
	"bpsexec_psh":              "Removed legacy Beacon function (Cobalt Strike 4.0); retained only as a generic Host compatibility name with no native wrapper.",
	"brunasadmin":              "Removed legacy Beacon function (Cobalt Strike 4.0); retained only as a generic Host compatibility name with no native wrapper.",
	"bstage":                   "Removed legacy Beacon function (Cobalt Strike 4.0); retained only as a generic Host compatibility name with no native wrapper.",
	"bwdigest":                 "Removed legacy Beacon function (Cobalt Strike 4.0); retained only as a generic Host compatibility name with no native wrapper.",
	"bwinrm":                   "Removed legacy Beacon function (Cobalt Strike 4.0); retained only as a generic Host compatibility name with no native wrapper.",
	"bwmi":                     "Removed legacy Beacon function (Cobalt Strike 4.0); retained only as a generic Host compatibility name with no native wrapper.",
	"openBypassUACDialog":      "Removed legacy Aggressor client-UI function (Cobalt Strike 4.1); retained only as a generic Host compatibility name with no native wrapper.",
	"openWindowsDropperDialog": "Removed legacy Aggressor client-UI function (Cobalt Strike 4.0); retained only as a generic Host compatibility name with no native wrapper.",
}

var removedOfficialFunctionVersions = map[string]string{
	"bbypassuac":               "4.0",
	"bpsexec_psh":              "4.0",
	"brunasadmin":              "4.0",
	"bstage":                   "4.0",
	"bwdigest":                 "4.0",
	"bwinrm":                   "4.0",
	"bwmi":                     "4.0",
	"openBypassUACDialog":      "4.1",
	"openWindowsDropperDialog": "4.0",
}

func catalogContractFromRuntime(source opfor.AggressorFunctionContract) Contract {
	callbacks := make([]CallbackContract, len(source.Callbacks))
	for index, callback := range source.Callbacks {
		callbacks[index] = CallbackContract{
			Position: callback.Position, Required: callback.Required,
			Nullable: callback.Nullable, Retained: callback.Retained,
			ArgumentsKnown: callback.ArgumentsKnown, Arguments: callback.Arguments,
		}
	}
	constraints := make([]ArgumentConstraint, len(source.ArgumentConstraints))
	for index, constraint := range source.ArgumentConstraints {
		constraints[index] = ArgumentConstraint{
			Position: constraint.Position,
			Kind:     constraint.Kind,
			Values:   append([]string(nil), constraint.Values...),
		}
	}
	result := ReturnShape(source.TypedResult)
	switch result {
	case ReturnShapeValue, ReturnShapeNull, ReturnShapePredicate:
	default:
		panic("unknown native Aggressor result shape: " + source.Name)
	}
	return normalizeContract(Contract{
		Audit: ContractAuditRuntimeEnforced,
		Arity: ArityContract{
			Known: true, Minimum: source.MinimumArguments, Maximum: source.MaximumArguments,
		},
		Callbacks:           callbacks,
		ArgumentConstraints: constraints,
		ReturnShape:         result,
		Version:             VersionContract{Deprecated: source.Deprecated},
		Confidence:          ContractConfidenceExecutable,
		TypedProvider:       source.TypedProvider,
		ProviderErrors:      string(source.ProviderErrors),
		HostFallback:        source.HostFallback,
	})
}

func normalizeContract(contract Contract) Contract {
	if contract.Audit == "" {
		contract.Audit = ContractAuditNameOnly
	}
	if contract.ReturnShape == "" {
		contract.ReturnShape = ReturnShapeUnknown
	}
	if contract.SoftErrorPolicy == "" {
		contract.SoftErrorPolicy = SoftErrorPolicyUnknown
	}
	if contract.Confidence == "" {
		contract.Confidence = ContractConfidenceInventory
	}
	if contract.Callbacks == nil {
		contract.Callbacks = []CallbackContract{}
	}
	if contract.ArgumentConstraints == nil {
		contract.ArgumentConstraints = []ArgumentConstraint{}
	}
	return contract
}

func appendUniqueEvidence(evidence []Evidence, candidate Evidence) []Evidence {
	for _, existing := range evidence {
		if existing == candidate {
			return evidence
		}
	}
	return append(evidence, candidate)
}

func cloneCatalog(source CatalogSnapshot) CatalogSnapshot {
	clone := source
	clone.Sources = append([]SourceSnapshot(nil), source.Sources...)
	clone.Entries = make([]Entry, len(source.Entries))
	for index, entry := range source.Entries {
		clone.Entries[index] = cloneEntry(entry)
	}
	return clone
}

func cloneEntry(entry Entry) Entry {
	entry.Evidence = append([]Evidence(nil), entry.Evidence...)
	callbacks := entry.Contract.Callbacks
	entry.Contract.Callbacks = make([]CallbackContract, len(entry.Contract.Callbacks))
	copy(entry.Contract.Callbacks, callbacks)
	constraints := entry.Contract.ArgumentConstraints
	entry.Contract.ArgumentConstraints = make([]ArgumentConstraint, len(constraints))
	for index, constraint := range constraints {
		entry.Contract.ArgumentConstraints[index] = constraint
		entry.Contract.ArgumentConstraints[index].Values = append([]string(nil), constraint.Values...)
	}
	return entry
}

var portableDefaultFunctions = map[string]string{
	"alias":                              "Portable script-owned Beacon alias registration using the documented function form.",
	"alias_clear":                        "Portable removal of every active script-owned Beacon alias layer for an exact name.",
	"bind":                               "Portable layered keyboard-shortcut registration using the documented function form and declaration binding registry.",
	"beacon_command_describe":            "Portable lookup of a Beacon command's short help description.",
	"beacon_command_detail":              "Portable lookup of a Beacon command's detailed help text.",
	"beacon_command_group":               "Portable script-owned Beacon help-group registration.",
	"beacon_command_register":            "Portable script-owned Beacon command-help registration.",
	"beacon_commands":                    "Portable deterministic enumeration of registered Beacon command-help names.",
	"beacon_elevator_describe":           "Portable lookup of a Beacon elevator's description.",
	"beacon_elevator_register":           "Portable script-owned Beacon elevator metadata and callback registration.",
	"beacon_elevators":                   "Portable deterministic enumeration of registered Beacon elevator names.",
	"beacon_exploit_describe":            "Portable lookup of a Beacon local exploit's description.",
	"beacon_exploit_register":            "Portable script-owned Beacon local-exploit metadata and callback registration.",
	"beacon_exploits":                    "Portable deterministic enumeration of registered Beacon local-exploit names.",
	"beacon_remote_exec_method_describe": "Portable lookup of a Beacon remote-exec method's description.",
	"beacon_remote_exec_method_register": "Portable script-owned Beacon remote-exec-method metadata and callback registration.",
	"beacon_remote_exec_methods":         "Portable deterministic enumeration of registered Beacon remote-exec method names.",
	"beacon_remote_exploit_arch":         "Portable lookup of a Beacon remote exploit's registered architecture.",
	"beacon_remote_exploit_describe":     "Portable lookup of a Beacon remote exploit's description.",
	"beacon_remote_exploit_register":     "Portable script-owned Beacon remote-exploit metadata and callback registration.",
	"beacon_remote_exploits":             "Portable deterministic enumeration of registered Beacon remote-exploit names.",
	"bof_pack":                           "Portable Aggressor BOF argument-buffer encoder with a UTF-8 default and configurable target encoding.",
	"brk":                                "Portable interpreter-owned Aggressor breakpoint snapshot with a synchronous optional presentation provider and headless console default.",
	"closef":                             "Portable Sleep I/O handle close operation.",
	"dispatch_event":                     "Portable Aggressor callback scheduling through the runtime event dispatcher.",
	"dstamp":                             "Portable Aggressor date/time formatting with seconds in the runtime clock's location.",
	"fireAlias":                          "Portable runtime-local invocation of a registered Beacon alias.",
	"format_size":                        "Portable Aggressor human-readable binary-size formatter.",
	"formatDate":                         "Portable Sleep date formatting using the runtime clock boundary.",
	"getAggressorClientType":             "Portable Aggressor client-type query for OPFOR's headless runtime.",
	"global":                             "Portable Sleep scope declaration handled by the evaluator.",
	"gunzip":                             "Portable Aggressor GZIP decompression over binary strings.",
	"gzip":                               "Portable Aggressor GZIP compression over binary strings.",
	"iff":                                "Portable lazy conditional function handled by the evaluator.",
	"int":                                "Portable Sleep integer conversion.",
	"insert_menu":                        "Portable runtime-local composition of every exact popup-hook layer into the current menu tree.",
	"iprange":                            "Portable Aggressor bounded IPv4 range expansion.",
	"keys":                               "Portable Sleep hash key enumeration.",
	"lambda":                             "Portable Sleep closure capture handled by the evaluator.",
	"local":                              "Portable Sleep scope declaration handled by the evaluator.",
	"matched":                            "Portable Sleep regular-expression capture lookup.",
	"on":                                 "Portable script-owned event registration using the documented function form.",
	"openf":                              "Portable Sleep filesystem handle operation.",
	"parseDate":                          "Portable Sleep date parsing operation.",
	"pe_mask":                            "Portable raw PE byte-range XOR over a fresh byte-string copy.",
	"pe_mask_string":                     "Portable raw PE NUL-terminated string XOR over a fresh byte-string copy.",
	"pe_set_compile_time_with_long":      "Portable PE32/PE32+ COFF compile-time mutation from Unix milliseconds over a fresh byte-string copy.",
	"pe_set_long":                        "Portable raw PE little-endian DWORD mutation over a fresh byte-string copy.",
	"pe_set_short":                       "Portable raw PE little-endian WORD mutation over a fresh byte-string copy.",
	"pe_set_string":                      "Portable raw PE string mutation without a terminator over a fresh byte-string copy.",
	"pe_set_stringz":                     "Portable raw PE string mutation with a NUL terminator over a fresh byte-string copy.",
	"pe_stomp":                           "Portable raw PE zeroing through the first NUL-terminated string over a fresh byte-string copy.",
	"pe_update_checksum":                 "Portable PE32/PE32+ full-file image checksum recomputation over a fresh byte-string copy.",
	"powershell_command":                 "Portable PowerShell one-liner formatting with the documented POWERSHELL_COMMAND hook and UTF-16LE Base64 default.",
	"println":                            "Portable console or handle output operation.",
	"rand":                               "Portable Sleep pseudorandom number operation.",
	"range":                              "Portable deterministic sequence construction.",
	"readb":                              "Portable Sleep binary read operation.",
	"readln":                             "Portable Sleep line read operation.",
	"script_resource":                    "Portable local resource path resolution relative to the loaded script.",
	"size":                               "Portable Sleep collection size operation.",
	"split":                              "Portable Sleep string split operation.",
	"ssh_alias":                          "Portable script-owned SSH alias registration using the guide-documented function form.",
	"ssh_command_describe":               "Portable lookup of an SSH command's short help description.",
	"ssh_command_detail":                 "Portable lookup of an SSH command's detailed help text.",
	"ssh_command_group":                  "Portable script-owned SSH help-group registration.",
	"ssh_command_register":               "Portable script-owned SSH command-help registration.",
	"ssh_commands":                       "Portable deterministic enumeration of registered SSH command-help names.",
	"str_chunk":                          "Portable Aggressor UTF-16 code-unit string chunking.",
	"str_decode":                         "Portable Aggressor byte-string decoding with a finite charset registry.",
	"str_encode":                         "Portable Aggressor text encoding with a finite charset registry.",
	"str_xor":                            "Portable Aggressor repeating-key byte-string XOR.",
	"strlen":                             "Portable Sleep Java UTF-16 code-unit length operation.",
	"strrep":                             "Portable Sleep string replacement operation.",
	"substr":                             "Portable Sleep substring operation.",
	"this":                               "Portable persistent closure scope declaration handled by the evaluator.",
	"ticks":                              "Portable runtime-clock timestamp operation.",
	"tstamp":                             "Portable Aggressor date/time formatting without seconds in the runtime clock's location.",
	"transform":                          "Portable Aggressor hex, powershell-base64, and Veil transforms with documented provisional byte/case policy; format-underspecified selectors reject explicitly.",
	"unbind":                             "Portable removal of every active keyboard-shortcut layer for an exact shortcut.",
	"when":                               "Portable one-shot script-owned event registration through function or declaration form.",
}

func buildHostRequiredRuntimeFunctions() map[string]string {
	functions := map[string]string{
		"-is64":                      "Native Beacon/session x64 predicate wrapper; a query provider or embedding host must supply Cobalt session state.",
		"-isactive":                  "Native Beacon/session active predicate wrapper; a query provider or embedding host must supply Cobalt session state.",
		"-isadmin":                   "Native Beacon/session administrator predicate wrapper; a query provider or embedding host must supply Cobalt session state.",
		"-isbeacon":                  "Native Beacon session-type predicate wrapper; a query provider or embedding host must supply Cobalt session state.",
		"-isssh":                     "Native SSH session-type predicate wrapper; a query provider or embedding host must supply Cobalt session state.",
		"action":                     "Native Cobalt shared-event action wrapper; a configured client-service provider or embedding Host must publish the client effect.",
		"addTab":                     "Native Aggressor client-tab wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"addVisualization":           "Native Aggressor visualization-registration wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"add_to_clipboard":           "Native Aggressor clipboard wrapper; a configured client-UI provider or embedding Host must supply client clipboard behavior.",
		"applications":               "Native Cobalt application-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"artifact_payload":           "Native synchronous stageless-artifact generation wrapper; a configured artifact provider or embedding Host must supply Cobalt payload generation.",
		"artifact_stageless":         "Native deprecated callback-based stageless-artifact generation wrapper; a configured artifact provider or embedding Host must supply Cobalt payload generation.",
		"archives":                   "Native Cobalt archive-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"barch":                      "Native Beacon architecture query wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"bbrowser":                   "Native Aggressor Beacon-browser component wrapper; a configured client-UI provider or embedding Host must supply the opaque UI component.",
		"bdata":                      "Native Beacon metadata dictionary query wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"beacon_data":                "Native Beacon metadata dictionary query wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"beacon_execute_job":         "Native Beacon command-job execution wrapper; a configured execution provider or embedding Host must perform Beacon tasking.",
		"beacon_execute_postex_job":  "Native Beacon postex-job execution wrapper; a configured execution provider or embedding Host must perform Beacon tasking.",
		"beacon_ids":                 "Native Beacon ID enumeration wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"beacon_info":                "Native Beacon metadata field query wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"beacon_inline_execute":      "Native Beacon inline-BOF execution wrapper; a configured execution provider or embedding Host must perform Beacon tasking after OPFOR applies the hook.",
		"beacon_inline_execute_pe":   "Native Beacon inline-PE execution wrapper; a configured execution provider or embedding Host must perform Beacon tasking.",
		"beacons":                    "Native Beacon metadata enumeration wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"bcd":                        "Native Beacon change-directory action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bcp":                        "Native Beacon file-copy action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bdrives":                    "Native Beacon drive-listing action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"berror":                     "Native Beacon error-transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"belevate":                   "Native local-exploit dispatch wrapper; a registered script callback owns tasking, otherwise the embedding host must handle it.",
		"belevate_command":           "Native elevator dispatch wrapper; a registered script callback owns tasking, otherwise the embedding host must handle it.",
		"bdllinject":                 "Native Beacon reflective-DLL injection action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bdllspawn":                  "Native Beacon reflective-DLL spawn action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bdownload":                  "Native Beacon file-download action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bexecute":                   "Native Beacon process-execution action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bexecute_assembly":          "Native Beacon execute-assembly action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bhashdump":                  "Native Beacon hashdump action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"binput":                     "Native Beacon input-transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"binfo":                      "Native Beacon metadata field query wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"binline_execute":            "Native Beacon inline-object execution action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bjoberror":                  "Native Beacon job-error transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"bjoblog":                    "Native Beacon job-output transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"blog":                       "Native Beacon output-transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"blog2":                      "Native Beacon alternate-output transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"bjump":                      "Native remote-exploit dispatch wrapper; a registered script callback owns tasking, otherwise the embedding host must handle it.",
		"bls":                        "Native Beacon file-listing action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bmkdir":                     "Native Beacon directory-creation action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bmimikatz":                  "Native Beacon Mimikatz action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bmimikatz_small":            "Native Beacon small-Mimikatz action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bmv":                        "Native Beacon file-move action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bnet":                       "Native Beacon network-enumeration action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bof_extract":                "Typed importer boundary for Cobalt-owned extracted-BOF generation; defaults the entry point to sleep_mask and preserves Host fallback when no extractor is configured.",
		"bportscan":                  "Native Beacon port-scan action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bpowershell":                "Native Beacon PowerShell action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bpowershell_import_clear":   "Native Beacon PowerShell-import clear action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bpowerpick":                 "Native Beacon PowerPick action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bps":                        "Native Beacon process-listing action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bpsinject":                  "Native Beacon injected-PowerShell action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bpwd":                       "Native Beacon working-directory action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bread_pipe":                 "Native Beacon named-pipe read action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bremote_exec":               "Native remote-exec-method dispatch wrapper; a registered script callback owns tasking, otherwise the embedding host must handle it.",
		"brm":                        "Native Beacon file-removal action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bshell":                     "Native Beacon shell-command action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"btask":                      "Native Beacon task-description transcript adapter; an importer sink is required for Cobalt reporting and attribution; it does not task a Beacon.",
		"btaskcompleted":             "Native explicit Beacon task-completion transcript adapter; an importer sink is required for Cobalt reporting and attribution.",
		"btimestomp":                 "Native Beacon timestomp action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"bupload":                    "Native Beacon file-upload action wrapper; a configured action provider or embedding Host must read local content and perform Beacon tasking.",
		"bupload_raw":                "Native Beacon raw-content upload action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
		"call":                       "Native Team Server RPC dispatch wrapper; a configured RPC provider or embedding Host must issue the request.",
		"closeClient":                "Native Aggressor client-close wrapper; a configured client-service provider or embedding Host must manage the client lifecycle.",
		"colorMenu":                  "Native Aggressor color-menu component wrapper; a configured client-UI provider or embedding Host must supply the opaque UI component.",
		"credential_add":             "Native Cobalt credential mutation wrapper; a configured data-store provider or embedding Host must update client-owned records.",
		"credentials":                "Native Cobalt credential-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"custom_event":               "Native Cobalt custom-event wrapper; a configured client-service provider or embedding Host must publish the Team Server effect.",
		"custom_event_private":       "Native Cobalt private custom-event wrapper; a configured client-service provider or embedding Host must publish the client effect.",
		"data_keys":                  "Native Cobalt data-model key-enumeration wrapper; a configured provider or embedding Host must supply client-owned model state.",
		"data_query":                 "Native heterogeneous Cobalt data-model query wrapper; a configured provider or embedding Host must supply key-specific model data.",
		"dbutton_action":             "Native Aggressor dialog action-button wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"dbutton_help":               "Native Aggressor dialog Help-button wrapper; a configured dialog provider or embedding Host must supply browser/UI behavior.",
		"dialog":                     "Native Aggressor dialog-construction wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"dialog_description":         "Native Aggressor dialog-description wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"dialog_show":                "Native Aggressor dialog-presentation wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"drow_beacon":                "Native Aggressor Beacon-selection dialog-row wrapper; a configured dialog provider or embedding Host must supply client UI/state.",
		"drow_checkbox":              "Native Aggressor checkbox dialog-row wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"drow_combobox":              "Native Aggressor combobox dialog-row wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"drow_exploits":              "Native Aggressor exploit-selection dialog-row wrapper; a configured dialog provider or embedding Host must supply client UI/state.",
		"drow_file":                  "Native Aggressor file-chooser dialog-row wrapper; a configured dialog provider or embedding Host must supply UI/filesystem behavior.",
		"drow_interface":             "Native Aggressor VPN-interface dialog-row wrapper; a configured dialog provider or embedding Host must supply client UI/state.",
		"drow_krbtgt":                "Native Aggressor krbtgt-selection dialog-row wrapper; a configured dialog provider or embedding Host must supply client UI/state.",
		"drow_listener":              "Native Aggressor listener dialog-row wrapper limited to listeners with stagers; a configured dialog provider or embedding Host must supply client UI/state.",
		"drow_listener_smb":          "Native deprecated Aggressor listener-row wrapper equivalent to drow_listener_stage; a configured dialog provider or embedding Host must supply client UI/state.",
		"drow_listener_stage":        "Native Aggressor dialog-row wrapper for all Beacon and Foreign listener payloads; a configured dialog provider or embedding Host must supply client UI/state.",
		"drow_mailserver":            "Native Aggressor mail-server dialog-row wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"drow_proxyserver":           "Native deprecated Aggressor proxy-server dialog-row wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"drow_site":                  "Native Aggressor site/URL dialog-row wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"drow_text":                  "Native Aggressor text dialog-row wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"drow_text_big":              "Native Aggressor multiline-text dialog-row wrapper; a configured dialog provider or embedding Host must supply UI behavior.",
		"downloads":                  "Native Cobalt download-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"elog":                       "Native Cobalt event-log wrapper; a configured client-service provider or embedding Host must publish the client effect.",
		"encode":                     "Native Cobalt position-independent-code encoder wrapper; a configured code-transform provider or embedding Host must supply the exact encoder output.",
		"file_browser":               "Native Aggressor file-browser window wrapper; a configured client-UI provider or embedding Host must present the UI.",
		"getAggressorClient":         "Native Aggressor client-object query wrapper; a configured client-service provider or embedding Host must supply the opaque client object.",
		"get_postex_kit_callback_id": "Native Postex Kit callback-ID query wrapper; a configured execution provider or embedding Host must supply the Cobalt message type.",
		"get_cs_version":             "Native Cobalt version-query wrapper; a configured client-service provider or embedding Host must supply the client version.",
		"highlight":                  "Native Cobalt data-model highlight wrapper; a configured data-store provider or embedding Host must update client-owned presentation state.",
		"host_delete":                "Native Cobalt host-record deletion wrapper; a configured data-store provider or embedding Host must update client-owned records.",
		"host_info":                  "Native Cobalt host-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"host_update":                "Native Cobalt host-record mutation wrapper; a configured data-store provider or embedding Host must update client-owned records.",
		"hosts":                      "Native Cobalt host-record enumeration wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"insert_color_menu":          "Native Aggressor color-menu insertion wrapper; a configured client-UI provider or embedding Host must mutate the active menu tree.",
		"insert_component":           "Native Aggressor component-insertion wrapper; a configured client-UI provider or embedding Host must mutate the active menu tree.",
		"keystrokes":                 "Native Cobalt keystroke-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"killdate":                   "Native Team Server kill-date query wrapper; a configured profile provider or embedding Host must supply the active Team Server value.",
		"localip":                    "Native Team Server local-IP query wrapper; a configured site provider or embedding Host must supply the Team Server address.",
		"menubar":                    "Native Aggressor top-level menubar registration wrapper with a retained popup composer; a configured client-UI provider or embedding Host must supply UI behavior.",
		"mynick":                     "Native Cobalt client-nickname query wrapper; a configured client-service provider or embedding Host must supply client identity.",
		"nextTab":                    "Native Aggressor next-tab wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"popup_clear":                "Native effect-only Aggressor popup-clear wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"pgraph":                     "Native Aggressor pivot-graph component wrapper; a configured client-UI provider or embedding Host must supply the opaque UI component.",
		"pivots":                     "Native Cobalt pivot-model query wrapper; a configured data-model provider or embedding Host must supply client-owned model state.",
		"pref_get":                   "Native synchronous scalar preference-query wrapper; a configured preference provider or embedding Host must supply Cobalt preference state.",
		"pref_get_list":              "Native synchronous list preference-query wrapper preserving returned array identity; a configured preference provider or embedding Host must supply Cobalt preference state.",
		"pref_set":                   "Native synchronous scalar preference-mutation wrapper; a configured preference provider or embedding Host must update Cobalt preference state.",
		"pref_set_list":              "Native synchronous array preference-mutation wrapper preserving input array identity; a configured preference provider or embedding Host must update Cobalt preference state.",
		"previousTab":                "Native Aggressor previous-tab wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"privmsg":                    "Native Cobalt private-message wrapper; a configured client-service provider or embedding Host must publish the chat effect.",
		"powershell":                 "Native deprecated Cobalt PowerShell bootstrap wrapper; a configured payload provider or embedding Host must generate the listener-specific one-liner.",
		"powershell_compress":        "Native Cobalt PowerShell compression wrapper; the newest active POWERSHELL_COMPRESS hook wins, otherwise a configured code-transform provider or embedding Host must supply the exact wrapped script.",
		"process_browser":            "Native Aggressor process-browser window wrapper; a configured client-UI provider or embedding Host must present the UI.",
		"prompt_confirm":             "Native Aggressor Yes/No prompt wrapper; a configured prompt provider or embedding Host must supply UI behavior.",
		"prompt_directory_open":      "Native Aggressor directory-selection prompt wrapper; a configured prompt provider or embedding Host must supply UI/filesystem behavior.",
		"prompt_file_open":           "Native Aggressor file-open prompt wrapper; a configured prompt provider or embedding Host must supply UI/filesystem behavior.",
		"prompt_file_save":           "Native Aggressor file-save prompt wrapper; a configured prompt provider or embedding Host must supply UI/filesystem behavior.",
		"prompt_text":                "Native Aggressor text prompt wrapper; a configured prompt provider or embedding Host must supply UI behavior.",
		"removeTab":                  "Native Aggressor tab-removal wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"resetData":                  "Native Cobalt data-store reset wrapper; a configured data-store provider or embedding Host must update client-owned records.",
		"redactobject":               "Native Cobalt post-exploitation-object redaction wrapper; a configured data-store provider or embedding Host must perform the client data-model/UI effect.",
		"say":                        "Native Cobalt public-chat wrapper; a configured client-service provider or embedding Host must publish the chat effect.",
		"sbrowser":                   "Native Aggressor session-browser component wrapper; a configured client-UI provider or embedding Host must supply the opaque UI component.",
		"screenshots":                "Native Cobalt screenshot-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"separator":                  "Native Aggressor popup-menu separator wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"services":                   "Native Cobalt service-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"setup_strings":              "Native Malleable C2 string-application wrapper; a configured profile provider or embedding Host must apply the active profile to the Beacon payload.",
		"setup_transformations":      "Native Malleable C2 transformation wrapper; a configured profile provider or embedding Host must apply the active profile for the requested Beacon architecture.",
		"showVisualization":          "Native Aggressor visualization-selection wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"show_error":                 "Native Aggressor error-message wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"show_message":               "Native Aggressor message wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"show_popup":                 "Native Aggressor popup-presentation wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"site_host":                  "Native Team Server site-hosting wrapper; a configured site provider or embedding Host must create or replace hosted content.",
		"site_kill":                  "Native Team Server site-removal wrapper; a configured site provider or embedding Host must remove hosted content.",
		"sites":                      "Native Team Server hosted-site enumeration wrapper; a configured site provider or embedding Host must supply the hosted-site inventory.",
		"sync_download":              "Native Cobalt synchronized-download wrapper; a configured client-service provider or embedding Host must perform the client effect and optional callback.",
		"targets":                    "Native Cobalt target-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"tbrowser":                   "Native Aggressor target-browser component wrapper; a configured client-UI provider or embedding Host must supply the opaque UI component.",
		"transform_vbs":              "Native Cobalt VBS shellcode-transform wrapper; a configured code-transform provider or embedding Host must supply the exact VBS expression.",
		"tokenToEmail":               "Native Cobalt token-to-email query wrapper; a configured data-store provider or embedding Host must supply client-owned identity state.",
		"url_open":                   "Native Aggressor URL-open wrapper; a configured client-UI provider or embedding Host must supply browser/UI behavior.",
		"users":                      "Native Cobalt connected-user query wrapper; a configured client-service provider or embedding Host must supply Team Server user state.",
		"vpn_interface_info":         "Native Covert VPN interface-metadata wrapper; a configured VPN provider or embedding Host must supply Team Server interface state.",
		"vpn_interfaces":             "Native Covert VPN interface-inventory wrapper; a configured VPN provider or embedding Host must supply Team Server interface state.",
		"vpn_tap_create":             "Native Covert VPN interface-creation wrapper; a configured VPN provider or embedding Host must perform the Team Server effect.",
		"vpn_tap_delete":             "Native Covert VPN interface-deletion wrapper; a configured VPN provider or embedding Host must perform the Team Server effect.",
	}

	groups := []struct {
		names       []string
		description string
	}{
		{
			names: []string{
				"bargue_add", "bargue_list", "bargue_remove", "bbeacon_config",
				"bbeacon_gate", "bbeacon_interpreter", "bbeacon_interpreter_lint", "bblockdlls", "bbrowserpivot",
				"bbrowserpivot_stop", "bcancel", "bcheckin", "bclear", "bclipboard",
				"bconnect", "bcovertvpn", "bdata_store_list", "bdata_store_load",
				"bdata_store_unload", "bdcsync", "bdesktop", "bdllload", "beacon_console_watermark",
				"beacon_console_watermark_reset", "beacon_job_hide_output", "beacon_job_name", "beacon_link",
				"beacon_remove", "beacon_stage_pipe", "beacon_stage_tcp", "bexit", "bgetprivs", "bgetsystem",
				"bgetuid", "binject", "binline_execute_pe", "bipconfig", "bjob_send_data",
				"bjobkill", "bjobs", "bkerberos_ccache_use", "bkerberos_ticket_purge",
				"bkerberos_ticket_use", "bkeylogger", "bkill", "blink", "bloginuser",
				"blogonpasswords", "bmode", "bnote", "bpassthehash", "bpause",
				"bpowershell_import", "bppid", "bprintscreen", "bpsexec", "bpsexec_command",
				"breg_queryv", "brev2self", "brportfwd", "brportfwd_local",
				"brportfwd_stop", "brun", "brunas", "brunu", "bscreenshot", "bscreenwatch",
				"bsetenv", "bshinject", "bshspawn", "bsleep", "bsleepu", "bsocks",
				"bsocks_stop", "bspawn", "bspawnas", "bspawnto", "bspawnu", "bspunnel",
				"bspunnel_local", "bssh", "bssh_key", "bsteal_token", "bsudo",
				"bsyscall_method", "btoken_store_remove", "btoken_store_remove_all",
				"btoken_store_show", "btoken_store_steal", "btoken_store_steal_and_use",
				"btoken_store_use", "bunlink",
			},
			description: "Native documented Beacon action wrapper; a configured action provider or embedding Host must perform the Cobalt session effect.",
		},
		{
			names:       []string{"beacon_host_imported_script", "beacon_host_script"},
			description: "Native Beacon hosted-script wrapper; a configured execution provider or embedding Host must perform Cobalt hosting and return the generated invocation script.",
		},
		{
			names: []string{
				"listener_create", "listener_create_ext", "listener_delete", "listener_describe",
				"listener_info", "listener_pivot_create", "listener_restart", "listeners",
				"listeners_local", "listeners_stageless",
			},
			description: "Native listener-state wrapper; a configured listener provider or embedding Host must supply the connected Cobalt listener behavior.",
		},
		{
			names: []string{
				"-hasbootstraphint", "all_payloads", "artifact", "artifact_general",
				"artifact_sign", "artifact_stager", "payload", "payload_bootstrap_hint",
				"payload_local", "shellcode", "stager", "stager_bind_pipe", "stager_bind_tcp",
			},
			description: "Native payload-generation wrapper; a configured payload provider or embedding Host must supply Cobalt listener-aware generation and signing behavior.",
		},
		{
			names: []string{
				"payloadstore_add", "payloadstore_fetch", "payloadstore_list",
				"payloadstore_metadata", "payloadstore_remove",
			},
			description: "Native payload-store wrapper; a configured payload-store provider or embedding Host must supply Cobalt payload-store state.",
		},
		{
			names: []string{
				"pi_explicit_get", "pi_explicit_info", "pi_explicit_set",
				"pi_spawn_get", "pi_spawn_info", "pi_spawn_set",
				"pi_user_explicit_clear", "pi_user_explicit_get", "pi_user_explicit_get_map",
				"pi_user_explicit_get_names", "pi_user_explicit_set", "pi_user_spawn_clear",
				"pi_user_spawn_get", "pi_user_spawn_get_map", "pi_user_spawn_get_names",
				"pi_user_spawn_set",
			},
			description: "Native process-injection configuration wrapper; a configured process-injection provider or embedding Host must supply Cobalt's built-in and user-defined injection selections and inventories.",
		},
		{
			names: []string{
				"pe_insert_rich_header", "pe_mask_section", "pe_patch_code",
				"pe_remove_rich_header", "pe_set_compile_time_with_string",
				"pe_set_export_name", "pe_set_value_at", "pedump",
			},
			description: "Native Cobalt-owned PE inspection/transformation wrapper; a configured PE provider or embedding Host must supply the documented result without OPFOR inferring the algorithm. pe_set_export_name accepts the one- or two-argument evidence union from the conflicting current table and examples.",
		},
		{
			names: []string{
				"openAboutDialog", "openApplicationManager", "openAutoRunDialog", "openBeaconBrowser",
				"openBeaconConsole", "openBrowserPivotSetup", "openCloneSiteDialog", "openConnectDialog",
				"openCovertVPNSetup", "openCredentialManager", "openDefaultShortcutsDialog",
				"openDownloadBrowser", "openElevateDialog", "openEventLog", "openFileBrowser",
				"openGoldenTicketDialog", "openHTMLApplicationDialog", "openHostFileDialog",
				"openInterfaceManager", "openJavaSignedAppletDialog", "openJavaSmartAppletDialog",
				"openJobBrowser", "openJobConsole", "openJumpDialog", "openKeystrokeBrowser",
				"openListenerManager", "openMakeTokenDialog", "openMalleableProfileDialog",
				"openNewCredentialDialog", "openOfficeMacroDialog", "openOneLinerDialog",
				"openOrActivate", "openPayloadGeneratorDialog", "openPayloadGeneratorStageDialog",
				"openPayloadHelper", "openPayloadStoreManager", "openPivotListenerSetup",
				"openPortScanner", "openPortScannerLocal", "openPowerShellWebDialog",
				"openPreferencesDialog", "openProcessBrowser", "openSOCKSBrowser", "openSOCKSSetup",
				"openScreenshotBrowser", "openScriptConsole", "openScriptManager", "openScriptedWebDialog",
				"openServiceBrowser", "openSiteManager", "openSpawnAsDialog", "openSpawnDialog",
				"openSpearPhishDialog", "openSystemInformationDialog", "openSystemProfilerDialog",
				"openTargetBrowser", "openUserDefinedBrowser", "openWebLog", "openWindowsExecutableDialog",
				"openWindowsExecutableStageAllDialog", "openWindowsExecutableStageDialog",
			},
			description: "Native Cobalt client-window command wrapper; a configured client-UI provider or embedding Host must construct or present the UI.",
		},
	}
	for _, group := range groups {
		for _, name := range group.names {
			if _, exists := functions[name]; exists {
				panic("duplicate host-required Aggressor function: " + name)
			}
			functions[name] = group.description
		}
	}
	return functions
}

var hostRequiredRuntimeFunctions = buildHostRequiredRuntimeFunctions()

var officialExampleEvidence = map[catalogKey][]string{
	{KindFunction, "addTab"}:                  {"mouse.cna"},
	{KindFunction, "artifact_stageless"}:      {"stagelesspython.cna", "stagelessweb.cna"},
	{KindFunction, "base64_encode"}:           {"stagelesspython.cna"},
	{KindFunction, "bcd"}:                     {"bot.cna"},
	{KindFunction, "bdownload"}:               {"safedelete.cna"},
	{KindFunction, "beacon_command_register"}: {"portfwd.cna"},
	{KindFunction, "berror"}:                  {"search.cna"},
	{KindFunction, "bexecute"}:                {"safedelete.cna"},
	{KindFunction, "binput"}:                  {"initial.cna"},
	{KindFunction, "blog"}:                    {"getenv.cna", "getexplorer.cna", "getpidany.cna", "oneliner.cna", "search.cna"},
	{KindFunction, "bls"}:                     {"bot.cna"},
	{KindFunction, "bpowershell"}:             {"initial.cna"},
	{KindFunction, "bps"}:                     {"getexplorer.cna", "getpidany.cna"},
	{KindFunction, "bpwd"}:                    {"bot.cna"},
	{KindFunction, "brm"}:                     {"safedelete.cna"},
	{KindFunction, "bshell"}:                  {"bot.cna", "getenv.cna", "initial.cna"},
	{KindFunction, "btask"}:                   {"portfwd.cna", "search.cna"},
	{KindFunction, "call"}:                    {"oneliner.cna", "portfwd.cna"},
	{KindFunction, "cast"}:                    {"oneliner.cna"},
	{KindFunction, "closef"}:                  {"mkimport.cna", "oneliner.cna"},
	{KindFunction, "credential_add"}:          {"mkimport.cna"},
	{KindFunction, "data_keys"}:               {"data_models.cna"},
	{KindFunction, "data_query"}:              {"data_models.cna", "search.cna"},
	{KindFunction, "dbutton_action"}:          {"stagelesspython.cna", "stagelessweb.cna"},
	{KindFunction, "dialog"}:                  {"stagelesspython.cna", "stagelessweb.cna"},
	{KindFunction, "dialog_description"}:      {"stagelesspython.cna", "stagelessweb.cna"},
	{KindFunction, "dialog_show"}:             {"stagelesspython.cna", "stagelessweb.cna"},
	{KindFunction, "drow_checkbox"}:           {"stagelessweb.cna"},
	{KindFunction, "drow_listener_stage"}:     {"stagelesspython.cna", "stagelessweb.cna"},
	{KindFunction, "drow_text"}:               {"stagelesspython.cna", "stagelessweb.cna"},
	{KindFunction, "dstamp"}:                  {"search.cna", "tokenToEmail.cna"},
	{KindFunction, "fire_event"}:              {"checkit.cna"},
	{KindFunction, "fork"}:                    {"mouse.cna"},
	{KindFunction, "formatDate"}:              {"bot.cna"},
	{KindFunction, "getAggressorClient"}:      {"callany.cna"},
	{KindFunction, "global"}:                  {"checkit.cna", "getenv.cna", "initial.cna"},
	{KindFunction, "iff"}:                     {"safedelete.cna", "stagelessweb.cna"},
	{KindFunction, "int"}:                     {"portfwd.cna"},
	{KindFunction, "keys"}:                    {"data_models.cna"},
	{KindFunction, "lambda"}:                  {"bot.cna", "getexplorer.cna", "getpidany.cna", "initial.cna", "mouse.cna", "safedelete.cna"},
	{KindFunction, "local"}:                   {"callany.cna", "checkit.cna", "data_models.cna", "getenv.cna", "getexplorer.cna", "getpidany.cna", "initial.cna", "mkimport.cna", "mouse.cna", "oneliner.cna", "random_string.cna", "safedelete.cna", "search.cna", "stagelesspython.cna", "stagelessweb.cna", "tokenToEmail.cna"},
	{KindFunction, "localip"}:                 {"stagelesspython.cna", "stagelessweb.cna"},
	{KindFunction, "matched"}:                 {"getenv.cna", "mkimport.cna"},
	{KindFunction, "openf"}:                   {"mkimport.cna", "oneliner.cna"},
	{KindFunction, "println"}:                 {"bot.cna", "checkit.cna", "data_models.cna", "mkimport.cna", "random_string.cna"},
	{KindFunction, "popup_clear"}:             {"safedelete.cna"},
	{KindFunction, "prompt_confirm"}:          {"safedelete.cna"},
	{KindFunction, "prompt_text"}:             {"safedelete.cna", "stagelesspython.cna", "stagelessweb.cna"},
	{KindFunction, "rand"}:                    {"random_string.cna"},
	{KindFunction, "readb"}:                   {"oneliner.cna"},
	{KindFunction, "readln"}:                  {"mkimport.cna"},
	{KindFunction, "separator"}:               {"safedelete.cna"},
	{KindFunction, "show_popup"}:              {"mouse.cna"},
	{KindFunction, "site_host"}:               {"stagelesspython.cna", "stagelessweb.cna"},
	{KindFunction, "size"}:                    {"tokenToEmail.cna"},
	{KindFunction, "split"}:                   {"getenv.cna", "getexplorer.cna", "getpidany.cna"},
	{KindFunction, "ssh_command_register"}:    {"portfwd.cna"},
	{KindFunction, "strlen"}:                  {"random_string.cna", "search.cna"},
	{KindFunction, "strrep"}:                  {"mkimport.cna"},
	{KindFunction, "substr"}:                  {"callany.cna", "random_string.cna", "search.cna"},
	{KindFunction, "this"}:                    {"callany.cna"},
	{KindFunction, "tokenToEmail"}:            {"tokenToEmail.cna"},
	{KindFunction, "when"}:                    {"bot.cna"},
	{KindEvent, "beacon_checkin"}:             {"checkit.cna", "initial.cna"},
	{KindEvent, "beacon_initial"}:             {"getenv.cna", "initial.cna"},
	{KindEvent, "beacon_output"}:              {"getenv.cna", "bot.cna"},
	{KindEvent, "beacon_output_alt"}:          {"bot.cna"},
	{KindEvent, "beacon_revisited"}:           {"checkit.cna"},
	{KindHook, "PROFILER_HIT"}:                {"tokenToEmail.cna"},
	{KindHook, "WEB_HIT"}:                     {"tokenToEmail.cna"},
	{KindPopupHook, "attacks"}:                {"stagelesspython.cna", "stagelessweb.cna"},
	{KindPopupHook, "filebrowser"}:            {"safedelete.cna"},
}
