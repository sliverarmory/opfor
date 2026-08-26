package aggressor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/sliverarmory/opfor"
	"github.com/sliverarmory/opfor/internal/lexer"
)

func TestCatalogOfficialInventoryAndClassifications(t *testing.T) {
	t.Parallel()

	catalog := Catalog()
	if got, want := CatalogSchemaVersion, 3; got != want {
		t.Fatalf("CatalogSchemaVersion = %d, want %d for the explicit-contract schema", got, want)
	}
	if catalog.SchemaVersion != CatalogSchemaVersion || catalog.SnapshotDate != OfficialSnapshotDate {
		t.Fatalf("catalog metadata = schema %d snapshot %q", catalog.SchemaVersion, catalog.SnapshotDate)
	}
	if catalog.NamesSHA256 != OfficialNamesSHA256 || len(catalog.Sources) != 4 {
		t.Fatalf("catalog source integrity metadata = %q/%d", catalog.NamesSHA256, len(catalog.Sources))
	}
	if got, want := catalog.OfficialCounts, (Counts{Functions: 436, Events: 56, Hooks: 80, PopupHooks: 18}); got != want {
		t.Fatalf("official counts = %#v, want %#v", got, want)
	}
	if len(officialFunctionNames) != OfficialFunctionCount || len(officialEventNames) != OfficialEventCount || len(officialHookNames) != OfficialHookCount || len(officialPopupHookNames) != OfficialPopupHookCount {
		t.Fatalf("embedded inventory lengths = %d/%d/%d/%d", len(officialFunctionNames), len(officialEventNames), len(officialHookNames), len(officialPopupHookNames))
	}
	if catalog.EvidenceBoundary == "" {
		t.Fatal("catalog evidence boundary is empty")
	}

	seen := make(map[catalogKey]struct{}, len(catalog.Entries))
	for index, entry := range catalog.Entries {
		key := catalogKey{entry.Kind, entry.Name}
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate catalog entry: %#v", key)
		}
		seen[key] = struct{}{}
		if entry.Name == "" || entry.Description == "" || len(entry.Evidence) == 0 {
			t.Fatalf("incomplete entry: %#v", entry)
		}
		if entry.Contract.Audit == "" || entry.Contract.ReturnShape == "" ||
			entry.Contract.SoftErrorPolicy == "" || entry.Contract.Confidence == "" ||
			entry.Contract.Callbacks == nil || entry.Contract.ArgumentConstraints == nil {
			t.Fatalf("entry %s/%s has an implicit contract field: %#v", entry.Kind, entry.Name, entry.Contract)
		}
		switch entry.Match {
		case MatchExact:
			if entry.MatchValue != "" {
				t.Fatalf("exact entry %s/%s has match value %q", entry.Kind, entry.Name, entry.MatchValue)
			}
		case MatchPrefix:
			if entry.MatchValue == "" {
				t.Fatalf("prefix entry %s/%s has no match value", entry.Kind, entry.Name)
			}
		default:
			t.Fatalf("entry %s/%s has invalid match %q", entry.Kind, entry.Name, entry.Match)
		}
		switch entry.Support {
		case SupportPortableDefault, SupportHostRequired, SupportUnsupported:
		default:
			t.Fatalf("entry %s/%s has invalid support %q", entry.Kind, entry.Name, entry.Support)
		}
		switch entry.Boundary {
		case BoundaryPortableRuntime, BoundaryNativeWrapper, BoundaryGenericHost, BoundaryBindingDispatch:
		default:
			t.Fatalf("entry %s/%s has invalid boundary %q", entry.Kind, entry.Name, entry.Boundary)
		}
		if index != 0 {
			previous := catalog.Entries[index-1]
			if previous.Kind > entry.Kind || (previous.Kind == entry.Kind && previous.Name > entry.Name) {
				t.Fatalf("catalog is not sorted at %s/%s", entry.Kind, entry.Name)
			}
		}
	}

	tests := []struct {
		kind    EntryKind
		name    string
		support Support
	}{
		{KindFunction, "range", SupportPortableDefault},
		{KindFunction, "println", SupportPortableDefault},
		{KindFunction, "ticks", SupportPortableDefault},
		{KindFunction, "formatDate", SupportPortableDefault},
		{KindFunction, "parseDate", SupportPortableDefault},
		{KindFunction, "pack", SupportPortableDefault},
		{KindFunction, "concat", SupportPortableDefault},
		{KindFunction, "mid", SupportPortableDefault},
		{KindFunction, "uint", SupportPortableDefault},
		{KindFunction, "base64_decode", SupportPortableDefault},
		{KindFunction, "base64_encode", SupportPortableDefault},
		{KindFunction, "iprange", SupportPortableDefault},
		{KindFunction, "format_size", SupportPortableDefault},
		{KindFunction, "gzip", SupportPortableDefault},
		{KindFunction, "gunzip", SupportPortableDefault},
		{KindFunction, "str_chunk", SupportPortableDefault},
		{KindFunction, "str_encode", SupportPortableDefault},
		{KindFunction, "str_decode", SupportPortableDefault},
		{KindFunction, "str_xor", SupportPortableDefault},
		{KindFunction, "script_resource", SupportPortableDefault},
		{KindFunction, "on", SupportPortableDefault},
		{KindFunction, "alias", SupportPortableDefault},
		{KindFunction, "fireAlias", SupportPortableDefault},
		{KindFunction, "fireEvent", SupportPortableDefault},
		{KindFunction, "bshell", SupportHostRequired},
		{KindFunction, "fire_event", SupportPortableDefault},
		{KindEvent, "beacon_output", SupportHostRequired},
		{KindEvent, "beacon_revisited", SupportHostRequired},
		{KindHook, "WEB_HIT", SupportHostRequired},
		{KindHook, "RESOURCE_GENERATOR_VBS", SupportHostRequired},
		{KindPopupHook, "filebrowser", SupportHostRequired},
	}
	for _, test := range tests {
		entry, ok := Lookup(test.kind, test.name)
		if !ok {
			t.Errorf("Lookup(%q, %q) did not find entry", test.kind, test.name)
			continue
		}
		if entry.Support != test.support {
			t.Errorf("Lookup(%q, %q).Support = %q, want %q", test.kind, test.name, entry.Support, test.support)
		}
	}
	if _, ok := Lookup(KindFunction, "not_documented_or_observed"); ok {
		t.Fatal("Lookup unexpectedly invented an entry")
	}
	if len(Filter(KindFunction, SupportPortableDefault)) == 0 {
		t.Fatal("catalog filtering returned unexpected counts")
	}
	for _, name := range officialHookNames {
		entry, ok := Lookup(KindHook, name)
		if !ok || entry.Support != SupportHostRequired {
			t.Fatalf("official hook %q is not classified host-required", name)
		}
	}
	if got := len(Filter(KindPopupHook, SupportHostRequired)); got != OfficialPopupHookCount {
		t.Fatalf("host-required popup hook count = %d, want %d", got, OfficialPopupHookCount)
	}
	if _, err := json.Marshal(catalog); err != nil {
		t.Fatalf("json.Marshal(Catalog): %v", err)
	}
}

func TestCatalogNativeFunctionContractsMatchRuntimeInventory(t *testing.T) {
	t.Parallel()

	runtimeContracts := opfor.DefaultAggressorFunctionContracts()
	if len(runtimeContracts) == 0 {
		t.Fatal("runtime native Aggressor contract inventory is empty")
	}
	for _, source := range runtimeContracts {
		entry, ok := Lookup(KindFunction, source.Name)
		if !ok {
			t.Errorf("runtime contract %q has no catalog entry", source.Name)
			continue
		}
		contract := entry.Contract
		if entry.Boundary != BoundaryNativeWrapper || contract.Audit != ContractAuditRuntimeEnforced ||
			contract.Confidence != ContractConfidenceExecutable || !contract.Arity.Known ||
			contract.Arity.Minimum != source.MinimumArguments || contract.Arity.Maximum != source.MaximumArguments ||
			contract.TypedProvider != source.TypedProvider || string(contract.ReturnShape) != string(source.TypedResult) ||
			contract.ProviderErrors != string(source.ProviderErrors) || contract.HostFallback != source.HostFallback ||
			contract.Version.Deprecated != source.Deprecated {
			t.Errorf("catalog contract %q does not match runtime: catalog=%#v runtime=%#v", source.Name, contract, source)
		}
		if len(contract.Callbacks) != len(source.Callbacks) {
			t.Errorf("catalog contract %q callback count = %d, want %d", source.Name, len(contract.Callbacks), len(source.Callbacks))
			continue
		}
		for index, callback := range contract.Callbacks {
			want := source.Callbacks[index]
			if callback.Position != want.Position || callback.Required != want.Required ||
				callback.Nullable != want.Nullable || callback.Retained != want.Retained ||
				callback.ArgumentsKnown != want.ArgumentsKnown || callback.Arguments != want.Arguments {
				t.Errorf("catalog contract %q callback %d = %#v, want %#v", source.Name, index, callback, want)
			}
		}
		if len(contract.ArgumentConstraints) != len(source.ArgumentConstraints) {
			t.Errorf("catalog contract %q constraint count = %d, want %d", source.Name, len(contract.ArgumentConstraints), len(source.ArgumentConstraints))
			continue
		}
		for index, constraint := range contract.ArgumentConstraints {
			want := source.ArgumentConstraints[index]
			if constraint.Position != want.Position || constraint.Kind != want.Kind ||
				!slices.Equal(constraint.Values, want.Values) {
				t.Errorf("catalog contract %q constraint %d = %#v, want %#v", source.Name, index, constraint, want)
			}
		}
	}

	nameOnly, ok := Lookup(KindFunction, "bbypassuac")
	if !ok || nameOnly.Contract.Audit != ContractAuditNameOnly || nameOnly.Contract.Arity.Known ||
		nameOnly.Contract.ReturnShape != ReturnShapeUnknown || nameOnly.Contract.SoftErrorPolicy != SoftErrorPolicyUnknown ||
		nameOnly.Contract.Version.Removed != "4.0" {
		t.Fatalf("removed name-only contract = %#v, found=%v", nameOnly.Contract, ok)
	}
	event, ok := Lookup(KindEvent, "beacon_output")
	if !ok || event.Contract.Audit != ContractAuditNameOnly || event.Contract.Arity.Known ||
		event.Contract.Confidence != ContractConfidenceInventory {
		t.Fatalf("event name-only contract = %#v, found=%v", event.Contract, ok)
	}
}

func TestCatalogOfficialRoutingBoundaryClassifications(t *testing.T) {
	t.Parallel()

	functionCounts := map[Boundary]int{}
	for _, name := range officialFunctionNames {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Fatalf("Lookup(%q, %q) did not find official function", KindFunction, name)
		}
		functionCounts[entry.Boundary]++
	}
	wantFunctionCounts := map[Boundary]int{
		BoundaryPortableRuntime: 60,
		BoundaryNativeWrapper:   367,
		BoundaryGenericHost:     9,
	}
	for boundary, want := range wantFunctionCounts {
		if got := functionCounts[boundary]; got != want {
			t.Errorf("official function boundary %q count = %d, want %d", boundary, got, want)
		}
	}
	if got := len(functionCounts); got != len(wantFunctionCounts) {
		t.Fatalf("official functions use %d boundaries (%#v), want only %#v", got, functionCounts, wantFunctionCounts)
	}

	for _, inventory := range []struct {
		kind  EntryKind
		names []string
	}{
		{KindEvent, officialEventNames},
		{KindHook, officialHookNames},
		{KindPopupHook, officialPopupHookNames},
	} {
		for _, name := range inventory.names {
			entry, ok := Lookup(inventory.kind, name)
			if !ok {
				t.Fatalf("Lookup(%q, %q) did not find official binding", inventory.kind, name)
			}
			if entry.Boundary != BoundaryBindingDispatch {
				t.Errorf("Lookup(%q, %q).Boundary = %q, want %q", inventory.kind, name, entry.Boundary, BoundaryBindingDispatch)
			}
		}
	}

	tests := []struct {
		kind     EntryKind
		name     string
		boundary Boundary
	}{
		{KindFunction, "range", BoundaryPortableRuntime},
		{KindFunction, "bind", BoundaryPortableRuntime},
		{KindFunction, "bshell", BoundaryNativeWrapper},
		{KindFunction, "bof_extract", BoundaryNativeWrapper},
		{KindFunction, "pref_get", BoundaryNativeWrapper},
		{KindFunction, "listener_create_ext", BoundaryNativeWrapper},
		{KindFunction, "openPayloadHelper", BoundaryNativeWrapper},
		{KindFunction, "pi_user_spawn_get_map", BoundaryNativeWrapper},
		{KindFunction, "vpn_interfaces", BoundaryNativeWrapper},
		{KindEvent, "beacon_output", BoundaryBindingDispatch},
		{KindHook, "WEB_HIT", BoundaryBindingDispatch},
		{KindPopupHook, "filebrowser", BoundaryBindingDispatch},
	}
	for _, test := range tests {
		entry, ok := Lookup(test.kind, test.name)
		if !ok {
			t.Errorf("Lookup(%q, %q) did not find entry", test.kind, test.name)
			continue
		}
		if entry.Boundary != test.boundary {
			t.Errorf("Lookup(%q, %q).Boundary = %q, want %q", test.kind, test.name, entry.Boundary, test.boundary)
		}
	}

	for boundary, want := range wantFunctionCounts {
		if got := len(FilterByBoundary(KindFunction, boundary)); got < want {
			t.Errorf("FilterByBoundary(%q, %q) returned %d entries, fewer than %d official functions", KindFunction, boundary, got, want)
		}
	}
	if got, minimum := len(FilterByBoundary(KindEvent, BoundaryBindingDispatch)), OfficialEventCount; got < minimum {
		t.Errorf("binding-dispatch event count = %d, want at least the %d official events", got, minimum)
	}
}

func TestCatalogMatchesDocumentedCustomEventFamily(t *testing.T) {
	t.Parallel()

	entry, ok := Lookup(KindEvent, "custom_event_my-topic")
	if !ok {
		t.Fatal("documented concrete custom event did not match catalog pattern")
	}
	if entry.Name != "custom_event_<event name>" || entry.Match != MatchPrefix || entry.MatchValue != "custom_event_" {
		t.Fatalf("custom event entry = %#v", entry)
	}
	if entry.Matches("custom_event_") || entry.Matches("different_my-topic") {
		t.Fatal("custom event prefix accepted an invalid concrete name")
	}
}

func TestCatalogBuiltInTrancheClassifications(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		support     Support
		description string
	}{
		{"bof_pack", SupportPortableDefault, "Portable Aggressor BOF argument-buffer encoder with a UTF-8 default and configurable target encoding."},
		{"dispatch_event", SupportPortableDefault, "Portable Aggressor callback scheduling through the runtime event dispatcher."},
		{"getAggressorClientType", SupportPortableDefault, "Portable Aggressor client-type query for OPFOR's headless runtime."},
		{"beacon_inline_execute", SupportHostRequired, "Native Beacon inline-BOF execution wrapper; a configured execution provider or embedding Host must perform Beacon tasking after OPFOR applies the hook."},
	}
	for _, test := range tests {
		entry, ok := Lookup(KindFunction, test.name)
		if !ok {
			t.Errorf("Lookup(%q) did not find built-in", test.name)
			continue
		}
		if entry.Support != test.support || entry.Description != test.description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				test.name, entry.Support, entry.Description, test.support, test.description)
		}
	}

	runtimeInstance, err := opfor.New()
	if err != nil {
		t.Fatalf("opfor.New: %v", err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	names := runtimeInstance.FunctionNames()
	if defaults := opfor.DefaultFunctionNames(); !slices.Equal(defaults, names) {
		t.Fatalf("opfor.DefaultFunctionNames() diverged from a default Runtime:\n got %q\nwant %q", defaults, names)
	}
	index := sort.SearchStrings(names, "beacon_inline_execute")
	if index == len(names) || names[index] != "beacon_inline_execute" {
		t.Fatal("beacon_inline_execute is not registered in the native runtime; host-required override was not exercised")
	}
}

func TestCatalogBeaconExecutionProviderFamily(t *testing.T) {
	t.Parallel()

	wantDescriptions := map[string]string{
		"beacon_host_imported_script": "Native Beacon hosted-script wrapper; a configured execution provider or embedding Host must perform Cobalt hosting and return the generated invocation script.",
		"beacon_host_script":          "Native Beacon hosted-script wrapper; a configured execution provider or embedding Host must perform Cobalt hosting and return the generated invocation script.",
		"beacon_execute_job":          "Native Beacon command-job execution wrapper; a configured execution provider or embedding Host must perform Beacon tasking.",
		"beacon_execute_postex_job":   "Native Beacon postex-job execution wrapper; a configured execution provider or embedding Host must perform Beacon tasking.",
		"beacon_inline_execute":       "Native Beacon inline-BOF execution wrapper; a configured execution provider or embedding Host must perform Beacon tasking after OPFOR applies the hook.",
		"beacon_inline_execute_pe":    "Native Beacon inline-PE execution wrapper; a configured execution provider or embedding Host must perform Beacon tasking.",
		"get_postex_kit_callback_id":  "Native Postex Kit callback-ID query wrapper; a configured execution provider or embedding Host must supply the Cobalt message type.",
	}
	names := opfor.DefaultFunctionNames()
	for name, description := range wantDescriptions {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find execution wrapper", name)
			continue
		}
		if entry.Support != SupportHostRequired || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportHostRequired, description)
		}
		if !hasEvidence(entry, "opfor-runtime", "native importer-boundary wrapper") {
			t.Errorf("Lookup(%q) evidence = %#v, want native importer boundary", name, entry.Evidence)
		}
		index := sort.SearchStrings(names, name)
		if index == len(names) || names[index] != name {
			t.Errorf("%q is absent from opfor.DefaultFunctionNames", name)
		}
	}

}

func TestCatalogHostRequiredNativeEvidenceMatchesImporterBoundary(t *testing.T) {
	t.Parallel()

	defaults := opfor.DefaultFunctionNames()
	for name := range hostRequiredRuntimeFunctions {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find host-required native wrapper", name)
			continue
		}
		if entry.Support != SupportHostRequired {
			t.Errorf("Lookup(%q).Support = %q, want %q", name, entry.Support, SupportHostRequired)
		}
		if entry.Boundary != BoundaryNativeWrapper {
			t.Errorf("Lookup(%q).Boundary = %q, want %q", name, entry.Boundary, BoundaryNativeWrapper)
		}
		if hasEvidence(entry, "opfor-runtime", "portable Sleep/runtime implementation") {
			t.Errorf("Lookup(%q) retains contradictory portable-runtime evidence: %#v", name, entry.Evidence)
		}
		if !hasEvidence(entry, "opfor-runtime", "native importer-boundary wrapper") {
			t.Errorf("Lookup(%q) evidence = %#v, want native importer boundary", name, entry.Evidence)
		}
		if !slices.Contains(defaults, name) {
			t.Errorf("Lookup(%q) claims a native-wrapper boundary but DefaultFunctionNames does not contain it", name)
		}
	}
}

func TestCatalogAggressorTimestampClassifications(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"dstamp": "Portable Aggressor date/time formatting with seconds in the runtime clock's location.",
		"tstamp": "Portable Aggressor date/time formatting without seconds in the runtime clock's location.",
	}
	defaults := opfor.DefaultFunctionNames()
	for name, description := range tests {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find documented timestamp helper", name)
			continue
		}
		if entry.Support != SupportPortableDefault || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportPortableDefault, description)
		}
		if !hasEvidence(entry, "official-documentation", officialFunctionsURL) {
			t.Errorf("Lookup(%q) is missing official function-reference evidence: %#v", name, entry.Evidence)
		}
		if !hasEvidence(entry, "opfor-runtime", "portable Sleep/runtime implementation") {
			t.Errorf("Lookup(%q) is missing portable-runtime evidence: %#v", name, entry.Evidence)
		}
		if !slices.Contains(defaults, name) {
			t.Errorf("DefaultFunctionNames missing portable timestamp helper %q", name)
		}
	}

	dstamp, _ := Lookup(KindFunction, "dstamp")
	for _, file := range []string{"search.cna", "tokenToEmail.cna"} {
		if !hasEvidence(dstamp, "official-example-corpus", officialExamplesRoot+file) {
			t.Errorf("Lookup(\"dstamp\") is missing %s evidence: %#v", file, dstamp.Evidence)
		}
	}
}

func TestCatalogAggressorCommandHelpAndAliasClearClassifications(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"alias_clear",
		"beacon_command_describe",
		"beacon_command_detail",
		"beacon_command_group",
		"beacon_command_register",
		"beacon_commands",
		"ssh_alias",
		"ssh_command_describe",
		"ssh_command_detail",
		"ssh_command_group",
		"ssh_command_register",
		"ssh_commands",
	} {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find command-help tranche function", name)
			continue
		}
		if entry.Support != SupportPortableDefault {
			t.Errorf("Lookup(%q).Support = %q, want %q", name, entry.Support, SupportPortableDefault)
		}
	}

	if slices.Contains(officialFunctionNames, "ssh_alias") {
		t.Fatal("ssh_alias unexpectedly entered the official function-heading inventory")
	}
	sshAlias, ok := Lookup(KindFunction, "ssh_alias")
	if !ok {
		t.Fatal("Lookup(ssh_alias) did not find guide-documented function")
	}
	guideEvidence := false
	for _, evidence := range sshAlias.Evidence {
		if evidence.Source == "official-documentation-guide" && evidence.Reference == officialSSHSessionsURL && evidence.SHA256 == officialSSHSessionsSHA256 {
			guideEvidence = true
		}
	}
	if !guideEvidence {
		t.Fatalf("ssh_alias evidence = %#v, want official SSH sessions guide", sshAlias.Evidence)
	}
}

func TestCatalogAggressorWhenClassificationAndEvidence(t *testing.T) {
	t.Parallel()

	if slices.Contains(officialFunctionNames, "when") {
		t.Fatal("when unexpectedly entered the current official function-heading inventory")
	}
	const description = "Portable one-shot script-owned event registration through function or declaration form."
	entry, ok := Lookup(KindFunction, "when")
	if !ok {
		t.Fatal("Lookup(\"when\") did not find one-shot event registration")
	}
	if entry.Support != SupportPortableDefault || entry.Description != description {
		t.Errorf("Lookup(\"when\") = support %q description %q, want %q/%q",
			entry.Support, entry.Description, SupportPortableDefault, description)
	}
	if !hasEvidence(entry, "opfor-runtime", "portable Sleep/runtime implementation") {
		t.Errorf("Lookup(\"when\") is missing portable-runtime evidence: %#v", entry.Evidence)
	}
	if !hasEvidence(entry, "official-example-corpus", officialExamplesRoot+"bot.cna") {
		t.Errorf("Lookup(\"when\") is missing bot.cna evidence: %#v", entry.Evidence)
	}
	if defaults := opfor.DefaultFunctionNames(); !slices.Contains(defaults, "when") {
		t.Error("DefaultFunctionNames missing portable one-shot event function \"when\"")
	}
}

func TestCatalogAggressorBeaconTechniqueRegistryClassifications(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
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
	}
	for name, description := range tests {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find Beacon technique registry function", name)
			continue
		}
		if entry.Support != SupportPortableDefault || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportPortableDefault, description)
		}
	}
}

func TestCatalogAggressorBeaconTechniqueActionClassifications(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"belevate":         "Native local-exploit dispatch wrapper; a registered script callback owns tasking, otherwise the embedding host must handle it.",
		"belevate_command": "Native elevator dispatch wrapper; a registered script callback owns tasking, otherwise the embedding host must handle it.",
		"bjump":            "Native remote-exploit dispatch wrapper; a registered script callback owns tasking, otherwise the embedding host must handle it.",
		"bremote_exec":     "Native remote-exec-method dispatch wrapper; a registered script callback owns tasking, otherwise the embedding host must handle it.",
	}
	defaults := opfor.DefaultFunctionNames()
	for name, description := range tests {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find Beacon technique action", name)
			continue
		}
		if entry.Support != SupportHostRequired || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportHostRequired, description)
		}
		index := sort.SearchStrings(defaults, name)
		if index == len(defaults) || defaults[index] != name {
			t.Errorf("DefaultFunctionNames missing native dispatch wrapper %q", name)
		}
	}
}

var catalogAggressorBeaconActionDescriptions = map[string]string{
	"bcd":                      "Native Beacon change-directory action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bcp":                      "Native Beacon file-copy action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bdrives":                  "Native Beacon drive-listing action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bdllinject":               "Native Beacon reflective-DLL injection action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bdllspawn":                "Native Beacon reflective-DLL spawn action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bdownload":                "Native Beacon file-download action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bexecute":                 "Native Beacon process-execution action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bexecute_assembly":        "Native Beacon execute-assembly action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bhashdump":                "Native Beacon hashdump action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"binline_execute":          "Native Beacon inline-object execution action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bls":                      "Native Beacon file-listing action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bmkdir":                   "Native Beacon directory-creation action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bmimikatz":                "Native Beacon Mimikatz action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bmimikatz_small":          "Native Beacon small-Mimikatz action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bmv":                      "Native Beacon file-move action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bnet":                     "Native Beacon network-enumeration action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bportscan":                "Native Beacon port-scan action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bpowershell":              "Native Beacon PowerShell action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bpowershell_import_clear": "Native Beacon PowerShell-import clear action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bpowerpick":               "Native Beacon PowerPick action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bps":                      "Native Beacon process-listing action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bpsinject":                "Native Beacon injected-PowerShell action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bpwd":                     "Native Beacon working-directory action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bread_pipe":               "Native Beacon named-pipe read action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"brm":                      "Native Beacon file-removal action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bshell":                   "Native Beacon shell-command action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"btimestomp":               "Native Beacon timestomp action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
	"bupload":                  "Native Beacon file-upload action wrapper; a configured action provider or embedding Host must read local content and perform Beacon tasking.",
	"bupload_raw":              "Native Beacon raw-content upload action wrapper; a configured action provider or embedding Host must perform Beacon tasking.",
}

func TestCatalogAggressorBeaconActionClassifications(t *testing.T) {
	t.Parallel()

	if got := len(catalogAggressorBeaconActionDescriptions); got != 29 {
		t.Fatalf("Beacon action catalog description count = %d, want 29", got)
	}
	defaults := opfor.DefaultFunctionNames()
	for name, description := range catalogAggressorBeaconActionDescriptions {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find Beacon action wrapper", name)
			continue
		}
		if entry.Support != SupportHostRequired || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportHostRequired, description)
		}
		if !hasEvidence(entry, "opfor-runtime", "native importer-boundary wrapper") {
			t.Errorf("Lookup(%q) is missing native importer-boundary evidence: %#v", name, entry.Evidence)
		}
		index := sort.SearchStrings(defaults, name)
		if index == len(defaults) || defaults[index] != name {
			t.Errorf("DefaultFunctionNames missing Beacon action wrapper %q", name)
		}
	}
}

func TestCatalogAggressorBeaconTranscriptClassifications(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"berror":         "Native Beacon error-transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"binput":         "Native Beacon input-transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"bjoberror":      "Native Beacon job-error transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"bjoblog":        "Native Beacon job-output transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"blog":           "Native Beacon output-transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"blog2":          "Native Beacon alternate-output transcript adapter; an importer sink is required for Cobalt transcript persistence and attribution.",
		"btask":          "Native Beacon task-description transcript adapter; an importer sink is required for Cobalt reporting and attribution; it does not task a Beacon.",
		"btaskcompleted": "Native explicit Beacon task-completion transcript adapter; an importer sink is required for Cobalt reporting and attribution.",
	}
	defaults := opfor.DefaultFunctionNames()
	for name, description := range tests {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find Beacon transcript adapter", name)
			continue
		}
		if entry.Support != SupportHostRequired || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportHostRequired, description)
		}
		index := sort.SearchStrings(defaults, name)
		if index == len(defaults) || defaults[index] != name {
			t.Errorf("DefaultFunctionNames missing native transcript adapter %q", name)
		}
	}
}

func TestCatalogAggressorSessionQueryClassifications(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"-is64":       "Native Beacon/session x64 predicate wrapper; a query provider or embedding host must supply Cobalt session state.",
		"-isactive":   "Native Beacon/session active predicate wrapper; a query provider or embedding host must supply Cobalt session state.",
		"-isadmin":    "Native Beacon/session administrator predicate wrapper; a query provider or embedding host must supply Cobalt session state.",
		"-isbeacon":   "Native Beacon session-type predicate wrapper; a query provider or embedding host must supply Cobalt session state.",
		"-isssh":      "Native SSH session-type predicate wrapper; a query provider or embedding host must supply Cobalt session state.",
		"barch":       "Native Beacon architecture query wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"bdata":       "Native Beacon metadata dictionary query wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"beacon_data": "Native Beacon metadata dictionary query wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"beacon_ids":  "Native Beacon ID enumeration wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"beacon_info": "Native Beacon metadata field query wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"beacons":     "Native Beacon metadata enumeration wrapper; a query provider or embedding host must supply Cobalt session metadata.",
		"binfo":       "Native Beacon metadata field query wrapper; a query provider or embedding host must supply Cobalt session metadata.",
	}
	defaults := opfor.DefaultFunctionNames()
	for name, description := range tests {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find session query wrapper", name)
			continue
		}
		if entry.Support != SupportHostRequired || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportHostRequired, description)
		}
		if !hasEvidence(entry, "opfor-runtime", "native importer-boundary wrapper") {
			t.Errorf("Lookup(%q) is missing native importer-boundary evidence: %#v", name, entry.Evidence)
		}
		index := sort.SearchStrings(defaults, name)
		if index == len(defaults) || defaults[index] != name {
			t.Errorf("DefaultFunctionNames missing session query wrapper %q", name)
		}
	}
}

func TestCatalogAggressorDataModelQueryClassifications(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"data_keys":  "Native Cobalt data-model key-enumeration wrapper; a configured provider or embedding Host must supply client-owned model state.",
		"data_query": "Native heterogeneous Cobalt data-model query wrapper; a configured provider or embedding Host must supply key-specific model data.",
	}
	defaults := opfor.DefaultFunctionNames()
	for name, description := range tests {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find data-model query wrapper", name)
			continue
		}
		if entry.Support != SupportHostRequired || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportHostRequired, description)
		}
		if !hasEvidence(entry, "opfor-runtime", "native importer-boundary wrapper") {
			t.Errorf("Lookup(%q) is missing native importer-boundary evidence: %#v", name, entry.Evidence)
		}
		index := sort.SearchStrings(defaults, name)
		if index == len(defaults) || defaults[index] != name {
			t.Errorf("DefaultFunctionNames missing data-model query wrapper %q", name)
		}
	}
}

func TestCatalogAggressorDataStoreClassifications(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"applications":   "Native Cobalt application-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"archives":       "Native Cobalt archive-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"credential_add": "Native Cobalt credential mutation wrapper; a configured data-store provider or embedding Host must update client-owned records.",
		"credentials":    "Native Cobalt credential-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"downloads":      "Native Cobalt download-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"highlight":      "Native Cobalt data-model highlight wrapper; a configured data-store provider or embedding Host must update client-owned presentation state.",
		"host_delete":    "Native Cobalt host-record deletion wrapper; a configured data-store provider or embedding Host must update client-owned records.",
		"host_info":      "Native Cobalt host-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"host_update":    "Native Cobalt host-record mutation wrapper; a configured data-store provider or embedding Host must update client-owned records.",
		"hosts":          "Native Cobalt host-record enumeration wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"keystrokes":     "Native Cobalt keystroke-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"resetData":      "Native Cobalt data-store reset wrapper; a configured data-store provider or embedding Host must update client-owned records.",
		"screenshots":    "Native Cobalt screenshot-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"services":       "Native Cobalt service-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"targets":        "Native Cobalt target-record query wrapper; a configured data-store provider or embedding Host must supply client-owned records.",
		"tokenToEmail":   "Native Cobalt token-to-email query wrapper; a configured data-store provider or embedding Host must supply client-owned identity state.",
	}
	assertCatalogImporterBoundaryFunctions(t, tests)
}

func TestCatalogAggressorClientServiceClassifications(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"action":               "Native Cobalt shared-event action wrapper; a configured client-service provider or embedding Host must publish the client effect.",
		"closeClient":          "Native Aggressor client-close wrapper; a configured client-service provider or embedding Host must manage the client lifecycle.",
		"custom_event":         "Native Cobalt custom-event wrapper; a configured client-service provider or embedding Host must publish the Team Server effect.",
		"custom_event_private": "Native Cobalt private custom-event wrapper; a configured client-service provider or embedding Host must publish the client effect.",
		"elog":                 "Native Cobalt event-log wrapper; a configured client-service provider or embedding Host must publish the client effect.",
		"getAggressorClient":   "Native Aggressor client-object query wrapper; a configured client-service provider or embedding Host must supply the opaque client object.",
		"get_cs_version":       "Native Cobalt version-query wrapper; a configured client-service provider or embedding Host must supply the client version.",
		"mynick":               "Native Cobalt client-nickname query wrapper; a configured client-service provider or embedding Host must supply client identity.",
		"privmsg":              "Native Cobalt private-message wrapper; a configured client-service provider or embedding Host must publish the chat effect.",
		"say":                  "Native Cobalt public-chat wrapper; a configured client-service provider or embedding Host must publish the chat effect.",
		"users":                "Native Cobalt connected-user query wrapper; a configured client-service provider or embedding Host must supply Team Server user state.",
	}
	assertCatalogImporterBoundaryFunctions(t, tests)

	client, ok := Lookup(KindFunction, "getAggressorClient")
	if !ok {
		t.Fatal("Lookup(getAggressorClient) did not find client-service wrapper")
	}
	if !hasEvidence(client, "official-example-corpus", officialExamplesRoot+"callany.cna") {
		t.Errorf("getAggressorClient is missing general-example evidence: %#v", client.Evidence)
	}
}

var catalogAggressorArtifactSiteDescriptions = map[string]string{
	"artifact_payload":   "Native synchronous stageless-artifact generation wrapper; a configured artifact provider or embedding Host must supply Cobalt payload generation.",
	"artifact_stageless": "Native deprecated callback-based stageless-artifact generation wrapper; a configured artifact provider or embedding Host must supply Cobalt payload generation.",
	"localip":            "Native Team Server local-IP query wrapper; a configured site provider or embedding Host must supply the Team Server address.",
	"site_host":          "Native Team Server site-hosting wrapper; a configured site provider or embedding Host must create or replace hosted content.",
	"site_kill":          "Native Team Server site-removal wrapper; a configured site provider or embedding Host must remove hosted content.",
	"sites":              "Native Team Server hosted-site enumeration wrapper; a configured site provider or embedding Host must supply the hosted-site inventory.",
}

func TestCatalogAggressorArtifactAndSiteClassifications(t *testing.T) {
	t.Parallel()

	if got := len(catalogAggressorArtifactSiteDescriptions); got != 6 {
		t.Fatalf("artifact/site classification count = %d, want 6", got)
	}
	defaults := opfor.DefaultFunctionNames()
	for name, description := range catalogAggressorArtifactSiteDescriptions {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find artifact/site wrapper", name)
			continue
		}
		if entry.Support != SupportHostRequired || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportHostRequired, description)
		}
		if !hasEvidence(entry, "opfor-runtime", "native importer-boundary wrapper") {
			t.Errorf("Lookup(%q) is missing native importer-boundary evidence: %#v", name, entry.Evidence)
		}
		index := sort.SearchStrings(defaults, name)
		if index == len(defaults) || defaults[index] != name {
			t.Errorf("DefaultFunctionNames missing artifact/site wrapper %q", name)
		}
	}
}

func TestCatalogAggressorTeamServerRPCClassification(t *testing.T) {
	t.Parallel()

	const description = "Native Team Server RPC dispatch wrapper; a configured RPC provider or embedding Host must issue the request."
	entry, ok := Lookup(KindFunction, "call")
	if !ok {
		t.Fatal("Lookup(\"call\") did not find Team Server RPC wrapper")
	}
	if entry.Support != SupportHostRequired || entry.Description != description {
		t.Errorf("Lookup(\"call\") = support %q description %q, want %q/%q",
			entry.Support, entry.Description, SupportHostRequired, description)
	}
	if !hasEvidence(entry, "opfor-runtime", "native importer-boundary wrapper") {
		t.Errorf("Lookup(\"call\") is missing native importer-boundary evidence: %#v", entry.Evidence)
	}
	if defaults := opfor.DefaultFunctionNames(); !slices.Contains(defaults, "call") {
		t.Error("DefaultFunctionNames missing Team Server RPC wrapper \"call\"")
	}
}

func TestCatalogAggressorUIProviderClassifications(t *testing.T) {
	t.Parallel()

	names := []string{
		"dbutton_action", "dbutton_help",
		"dialog", "dialog_description", "dialog_show",
		"drow_beacon", "drow_checkbox", "drow_combobox", "drow_exploits",
		"drow_file", "drow_interface", "drow_krbtgt", "drow_listener",
		"drow_listener_smb", "drow_listener_stage", "drow_mailserver",
		"drow_proxyserver", "drow_site", "drow_text", "drow_text_big",
		"prompt_confirm", "prompt_directory_open", "prompt_file_open",
		"prompt_file_save", "prompt_text",
	}
	defaults := opfor.DefaultFunctionNames()
	for _, name := range names {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find Aggressor UI wrapper", name)
			continue
		}
		if entry.Support != SupportHostRequired {
			t.Errorf("Lookup(%q) support = %q, want %q", name, entry.Support, SupportHostRequired)
		}
		if !hasEvidence(entry, "opfor-runtime", "native importer-boundary wrapper") {
			t.Errorf("Lookup(%q) is missing native importer-boundary evidence: %#v", name, entry.Evidence)
		}
		index := sort.SearchStrings(defaults, name)
		if index == len(defaults) || defaults[index] != name {
			t.Errorf("DefaultFunctionNames missing Aggressor UI wrapper %q", name)
		}
	}
}

func TestCatalogAggressorClientUIClassifications(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"addTab":            "Native Aggressor client-tab wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"addVisualization":  "Native Aggressor visualization-registration wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"add_to_clipboard":  "Native Aggressor clipboard wrapper; a configured client-UI provider or embedding Host must supply client clipboard behavior.",
		"nextTab":           "Native Aggressor next-tab wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"popup_clear":       "Native effect-only Aggressor popup-clear wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"previousTab":       "Native Aggressor previous-tab wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"removeTab":         "Native Aggressor tab-removal wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"separator":         "Native Aggressor popup-menu separator wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"showVisualization": "Native Aggressor visualization-selection wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"show_error":        "Native Aggressor error-message wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"show_message":      "Native Aggressor message wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"show_popup":        "Native Aggressor popup-presentation wrapper; a configured client-UI provider or embedding Host must supply UI behavior.",
		"url_open":          "Native Aggressor URL-open wrapper; a configured client-UI provider or embedding Host must supply browser/UI behavior.",
	}
	assertCatalogImporterBoundaryFunctions(t, tests)
}

func assertCatalogImporterBoundaryFunctions(t *testing.T, tests map[string]string) {
	t.Helper()
	defaults := opfor.DefaultFunctionNames()
	for name, description := range tests {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find importer-boundary wrapper", name)
			continue
		}
		if entry.Support != SupportHostRequired || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportHostRequired, description)
		}
		if !hasEvidence(entry, "opfor-runtime", "native importer-boundary wrapper") {
			t.Errorf("Lookup(%q) is missing native importer-boundary evidence: %#v", name, entry.Evidence)
		}
		index := sort.SearchStrings(defaults, name)
		if index == len(defaults) || defaults[index] != name {
			t.Errorf("DefaultFunctionNames missing importer-boundary wrapper %q", name)
		}
	}
}

func TestCatalogAggressorPEMutatorClassifications(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"pe_mask":                       "Portable raw PE byte-range XOR over a fresh byte-string copy.",
		"pe_mask_string":                "Portable raw PE NUL-terminated string XOR over a fresh byte-string copy.",
		"pe_set_compile_time_with_long": "Portable PE32/PE32+ COFF compile-time mutation from Unix milliseconds over a fresh byte-string copy.",
		"pe_set_long":                   "Portable raw PE little-endian DWORD mutation over a fresh byte-string copy.",
		"pe_set_short":                  "Portable raw PE little-endian WORD mutation over a fresh byte-string copy.",
		"pe_set_string":                 "Portable raw PE string mutation without a terminator over a fresh byte-string copy.",
		"pe_set_stringz":                "Portable raw PE string mutation with a NUL terminator over a fresh byte-string copy.",
		"pe_stomp":                      "Portable raw PE zeroing through the first NUL-terminated string over a fresh byte-string copy.",
		"pe_update_checksum":            "Portable PE32/PE32+ full-file image checksum recomputation over a fresh byte-string copy.",
	}
	defaults := opfor.DefaultFunctionNames()
	for name, description := range tests {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find portable PE helper", name)
			continue
		}
		if entry.Support != SupportPortableDefault || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportPortableDefault, description)
		}
		index := sort.SearchStrings(defaults, name)
		if index == len(defaults) || defaults[index] != name {
			t.Errorf("DefaultFunctionNames missing portable PE helper %q", name)
		}
	}
}

const catalogUnavailableWorkingDirectoryChild = "OPFOR_TEST_CATALOG_UNAVAILABLE_WORKING_DIRECTORY_CHILD"

func TestCatalogSurvivesUnavailableWorkingDirectoryWithoutPoisoning(t *testing.T) {
	if os.Getenv(catalogUnavailableWorkingDirectoryChild) != "1" {
		command := exec.Command(
			os.Args[0],
			"-test.run=^TestCatalogSurvivesUnavailableWorkingDirectoryWithoutPoisoning$",
		)
		command.Env = append(os.Environ(), catalogUnavailableWorkingDirectoryChild+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("catalog unavailable-working-directory subprocess: %v\n%s", err, output)
		}
		return
	}

	if goruntime.GOOS == "windows" {
		t.Skip("Windows does not permit removing the process working directory")
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd before deleted-directory probe: %v", err)
	}
	directory, err := os.MkdirTemp("", "opfor-catalog-deleted-cwd-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	removed := false
	defer func() {
		_ = os.Chdir(original)
		if !removed {
			_ = os.RemoveAll(directory)
		}
	}()
	if err := os.Chdir(directory); err != nil {
		t.Fatalf("Chdir(%s): %v", directory, err)
	}
	if err := os.Remove(directory); err != nil {
		t.Skipf("platform does not permit removing its current directory: %v", err)
	}
	removed = true
	_, unavailableErr := os.Getwd()
	if goruntime.GOOS == "linux" && unavailableErr == nil {
		t.Fatal("Linux unexpectedly resolved a removed working directory")
	}

	first, firstPanic := catalogCallWithoutPanic()
	if err := os.Chdir(original); err != nil {
		t.Fatalf("restore working directory: %v", err)
	}
	second, secondPanic := catalogCallWithoutPanic()
	if firstPanic != nil || secondPanic != nil {
		t.Fatalf("Catalog panics = first %v, second %v", firstPanic, secondPanic)
	}
	for call, snapshot := range map[string]CatalogSnapshot{"first": first, "second": second} {
		if snapshot.SchemaVersion != CatalogSchemaVersion || len(snapshot.Entries) == 0 {
			t.Errorf("%s Catalog after unavailable cwd = schema %d with %d entries", call, snapshot.SchemaVersion, len(snapshot.Entries))
		}
	}
}

func catalogCallWithoutPanic() (snapshot CatalogSnapshot, panicValue any) {
	defer func() { panicValue = recover() }()
	snapshot = Catalog()
	return snapshot, nil
}

func TestCatalogReturnsIndependentCopies(t *testing.T) {
	t.Parallel()

	first := Catalog()
	if len(first.Entries) == 0 || len(first.Entries[0].Evidence) == 0 {
		t.Fatal("catalog unexpectedly empty")
	}
	first.Entries[0].Name = "mutated"
	first.Entries[0].Evidence[0].Reference = "mutated"
	first.Sources[0].URL = "mutated"
	for index := range first.Entries {
		if first.Entries[index].Name == "artifact_stageless" {
			if len(first.Entries[index].Contract.Callbacks) == 0 {
				t.Fatal("artifact_stageless contract unexpectedly lacks a callback")
			}
			first.Entries[index].Contract.Callbacks[0].Position = 99
		}
		if first.Entries[index].Name == "all_payloads" {
			if len(first.Entries[index].Contract.ArgumentConstraints) == 0 ||
				len(first.Entries[index].Contract.ArgumentConstraints[0].Values) == 0 {
				t.Fatal("all_payloads contract unexpectedly lacks constraints")
			}
			first.Entries[index].Contract.ArgumentConstraints[0].Position = 99
			first.Entries[index].Contract.ArgumentConstraints[0].Values[0] = "mutated"
		}
	}
	second := Catalog()
	if second.Entries[0].Name == "mutated" || second.Entries[0].Evidence[0].Reference == "mutated" || second.Sources[0].URL == "mutated" {
		t.Fatal("Catalog returned shared mutable data")
	}
	artifact, ok := Lookup(KindFunction, "artifact_stageless")
	if !ok || len(artifact.Contract.Callbacks) != 1 || artifact.Contract.Callbacks[0].Position != 5 {
		t.Fatalf("Catalog returned shared callback metadata: %#v, found=%v", artifact.Contract.Callbacks, ok)
	}
	allPayloads, ok := Lookup(KindFunction, "all_payloads")
	if !ok || len(allPayloads.Contract.ArgumentConstraints) != 3 ||
		allPayloads.Contract.ArgumentConstraints[0].Position != 3 ||
		allPayloads.Contract.ArgumentConstraints[0].Values[0] != "None" {
		t.Fatalf("Catalog returned shared constraint metadata: %#v, found=%v", allPayloads.Contract.ArgumentConstraints, ok)
	}
}

func TestOfficialInventoryCanonicalDigest(t *testing.T) {
	t.Parallel()

	var canonical strings.Builder
	for _, inventory := range []struct {
		kind   EntryKind
		values []string
	}{
		{KindFunction, officialFunctionNames},
		{KindEvent, officialEventNames},
		{KindHook, officialHookNames},
		{KindPopupHook, officialPopupHookNames},
	} {
		for _, name := range inventory.values {
			_, _ = fmt.Fprintf(&canonical, "%s\t%s\n", inventory.kind, name)
		}
	}
	sum := sha256.Sum256([]byte(canonical.String()))
	if got := fmt.Sprintf("%x", sum); got != OfficialNamesSHA256 {
		t.Fatalf("official name digest = %s, want %s", got, OfficialNamesSHA256)
	}

	wantSources := map[EntryKind]struct {
		digest string
		count  int
	}{
		KindFunction:  {officialFunctionsSHA256, OfficialFunctionCount},
		KindEvent:     {officialEventsSHA256, OfficialEventCount},
		KindHook:      {officialHooksSHA256, OfficialHookCount},
		KindPopupHook: {officialPopupHooksSHA256, OfficialPopupHookCount},
	}
	for _, source := range Catalog().Sources {
		want, ok := wantSources[source.Kind]
		if !ok || source.URL == "" || len(source.SHA256) != 64 || source.SHA256 != want.digest || source.Count != want.count {
			t.Fatalf("source snapshot = %#v", source)
		}
		delete(wantSources, source.Kind)
	}
	if len(wantSources) != 0 {
		t.Fatalf("missing source snapshots: %#v", wantSources)
	}
}

func TestOfficialExampleCallEventAndHookDependenciesAreCataloged(t *testing.T) {
	t.Parallel()

	paths := officialExamplePaths(t)
	observed := make(map[catalogKey]map[string]struct{})
	record := func(key catalogKey, file string) {
		if observed[key] == nil {
			observed[key] = make(map[string]struct{})
		}
		observed[key][file] = struct{}{}
	}
	ignoredCalls := map[string]struct{}{
		"for": {}, "foreach": {}, "if": {}, "while": {},
	}
	for _, path := range paths {
		file := filepath.Base(path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		result := lexer.Lex(lexer.NewSource(file, data))
		if len(result.Diagnostics) != 0 {
			t.Fatalf("Lex(%s): %v", path, result.Diagnostics)
		}

		declared := make(map[string]struct{})
		for index := 0; index+1 < len(result.Tokens); index++ {
			if result.Tokens[index].Is(lexer.Keyword, "sub") && result.Tokens[index+1].Kind == lexer.Identifier {
				declared[result.Tokens[index+1].Lexeme] = struct{}{}
			}
		}
		for index := 0; index+1 < len(result.Tokens); index++ {
			token := result.Tokens[index]
			if (token.Kind != lexer.Identifier && token.Kind != lexer.Keyword) || result.Tokens[index+1].Kind != lexer.LeftParen {
				continue
			}
			name := token.Lexeme
			if _, ignored := ignoredCalls[name]; ignored {
				continue
			}
			if _, local := declared[name]; local {
				continue
			}
			record(catalogKey{KindFunction, name}, file)
		}

		for index := 0; index+1 < len(result.Tokens); index++ {
			token := result.Tokens[index]
			switch {
			case token.Is(lexer.Keyword, "on"):
				name := selectorName(result.Tokens[index+1])
				if name != "" {
					record(catalogKey{KindEvent, name}, file)
				}
			case token.Is(lexer.Keyword, "set"):
				name := selectorName(result.Tokens[index+1])
				if name != "" {
					record(catalogKey{KindHook, name}, file)
				}
			case token.Is(lexer.Keyword, "popup"):
				name := selectorName(result.Tokens[index+1])
				if entry, ok := Lookup(KindPopupHook, name); ok && entry.Name == name {
					record(catalogKey{KindPopupHook, name}, file)
				}
			case token.Is(lexer.Keyword, "when") && index+2 < len(result.Tokens) && result.Tokens[index+1].Kind == lexer.LeftParen:
				name := selectorName(result.Tokens[index+2])
				if name != "" {
					record(catalogKey{KindEvent, name}, file)
				}
			case token.Is(lexer.Identifier, "fire_event") && index+2 < len(result.Tokens) && result.Tokens[index+1].Kind == lexer.LeftParen:
				name := selectorName(result.Tokens[index+2])
				if name != "" {
					record(catalogKey{KindEvent, name}, file)
				}
			}
		}
	}

	for key, files := range observed {
		entry, ok := Lookup(key.kind, key.name)
		if !ok {
			t.Errorf("official examples use uncataloged %s %q", key.kind, key.name)
			continue
		}
		for file := range files {
			if !hasEvidence(entry, "official-example-corpus", officialExamplesRoot+file) {
				t.Errorf("%s/%s is missing corpus evidence for %s", key.kind, key.name, file)
			}
		}
	}
	for key, files := range officialExampleEvidence {
		for _, file := range files {
			if _, ok := observed[key][file]; !ok {
				t.Errorf("catalog cites %s for unobserved %s/%s dependency", file, key.kind, key.name)
			}
		}
	}
}

func TestOfficialBeaconActionCorpusEvidence(t *testing.T) {
	t.Parallel()

	observed, calls := observedBeaconActionCalls(t, officialExamplePaths(t), officialExamplesRoot)
	if calls != 15 || len(observed) != 9 {
		t.Errorf("approved corpus Beacon action calls/names = %d/%d, want 15/9", calls, len(observed))
	}
	for key, files := range observed {
		entry, ok := Lookup(key.kind, key.name)
		if !ok {
			t.Errorf("approved corpus uses uncataloged %s/%s", key.kind, key.name)
			continue
		}
		for file := range files {
			if !hasEvidence(entry, "official-example-corpus", officialExamplesRoot+file) {
				t.Errorf("%s/%s is missing approved corpus evidence for %s", key.kind, key.name, file)
			}
		}
	}
	for key, files := range officialExampleEvidence {
		if _, action := catalogAggressorBeaconActionDescriptions[key.name]; !action || key.kind != KindFunction {
			continue
		}
		for _, file := range files {
			if _, ok := observed[key][file]; !ok {
				t.Errorf("catalog cites %s for unobserved %s/%s action", file, key.kind, key.name)
			}
		}
	}
}

func TestOfficialArtifactAndSiteCorpusEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		paths           []string
		root            string
		source          string
		wantCallSites   int
		wantNames       int
		wantFiles       int
		wantEvidence    int
		wantNameByCalls map[string]int
	}{
		{
			name:          "general examples",
			paths:         officialExamplePaths(t),
			root:          officialExamplesRoot,
			source:        "official-example-corpus",
			wantCallSites: 7,
			wantNames:     3,
			wantFiles:     2,
			wantEvidence:  6,
			wantNameByCalls: map[string]int{
				"artifact_stageless": 3,
				"localip":            2,
				"site_host":          2,
			},
		},
	}
	for _, test := range tests {
		observed, callsByName := observedArtifactSiteCalls(t, test.paths, test.root)
		callSites := 0
		files := make(map[string]struct{})
		for key, observedFiles := range observed {
			for file := range observedFiles {
				files[file] = struct{}{}
			}
			callSites += callsByName[key.name]
		}
		if callSites != test.wantCallSites || len(observed) != test.wantNames || len(files) != test.wantFiles {
			t.Errorf("%s artifact/site call sites/names/files = %d/%d/%d, want %d/%d/%d",
				test.name, callSites, len(observed), len(files), test.wantCallSites, test.wantNames, test.wantFiles)
		}
		for name, wantCalls := range test.wantNameByCalls {
			if got := callsByName[name]; got != wantCalls {
				t.Errorf("%s %s call sites = %d, want %d", test.name, name, got, wantCalls)
			}
		}
		if len(callsByName) != len(test.wantNameByCalls) {
			t.Errorf("%s artifact/site lexical call names = %#v, want %#v", test.name, callsByName, test.wantNameByCalls)
		}

		catalogEvidence := 0
		for name := range catalogAggressorArtifactSiteDescriptions {
			key := catalogKey{KindFunction, name}
			entry, ok := Lookup(key.kind, key.name)
			if !ok {
				t.Errorf("%s uses uncataloged %s/%s", test.name, key.kind, key.name)
				continue
			}
			for _, evidence := range entry.Evidence {
				if evidence.Source != test.source || !strings.HasPrefix(evidence.Reference, test.root) {
					continue
				}
				catalogEvidence++
				file := strings.TrimPrefix(evidence.Reference, test.root)
				if _, ok := observed[key][file]; !ok {
					t.Errorf("%s catalog cites %s for unobserved %s/%s call", test.name, file, key.kind, key.name)
				}
			}
		}
		if catalogEvidence != test.wantEvidence {
			t.Errorf("%s artifact/site per-file evidence count = %d, want %d", test.name, catalogEvidence, test.wantEvidence)
		}
		for key, observedFiles := range observed {
			entry, _ := Lookup(key.kind, key.name)
			for file := range observedFiles {
				if !hasEvidence(entry, test.source, test.root+file) {
					t.Errorf("%s/%s is missing %s evidence for %s", key.kind, key.name, test.source, file)
				}
			}
		}
	}
}

func TestOfficialTeamServerRPCCorpusEvidence(t *testing.T) {
	t.Parallel()

	const name = "call"
	root := filepath.Join("..", filepath.FromSlash(officialExamplesRoot))
	observedFiles := make(map[string]struct{})
	callSites := 0
	for _, path := range officialExamplePaths(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		result := lexer.Lex(lexer.NewSource(filepath.Base(path), data))
		if len(result.Diagnostics) != 0 {
			t.Fatalf("Lex(%s): %v", path, result.Diagnostics)
		}
		file, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("Rel(%s, %s): %v", root, path, err)
		}
		file = filepath.ToSlash(file)
		for index := 0; index+1 < len(result.Tokens); index++ {
			if result.Tokens[index].Kind == lexer.Identifier &&
				result.Tokens[index].Lexeme == name &&
				result.Tokens[index+1].Kind == lexer.LeftParen {
				callSites++
				observedFiles[file] = struct{}{}
			}
		}
	}
	if callSites != 3 || len(observedFiles) != 2 {
		t.Fatalf("official Team Server RPC call sites/files = %d/%d, want 3/2", callSites, len(observedFiles))
	}

	entry, ok := Lookup(KindFunction, name)
	if !ok {
		t.Fatal("official corpus uses uncataloged function/call")
	}
	evidenceFiles := make(map[string]struct{})
	for _, evidence := range entry.Evidence {
		if evidence.Source != "official-example-corpus" || !strings.HasPrefix(evidence.Reference, officialExamplesRoot) {
			continue
		}
		file := strings.TrimPrefix(evidence.Reference, officialExamplesRoot)
		evidenceFiles[file] = struct{}{}
		if _, observed := observedFiles[file]; !observed {
			t.Errorf("catalog cites %s for an unobserved function/call", file)
		}
	}
	if len(evidenceFiles) != 2 {
		t.Errorf("Team Server RPC per-file evidence count = %d, want 2", len(evidenceFiles))
	}
	for file := range observedFiles {
		if _, present := evidenceFiles[file]; !present {
			t.Errorf("function/call is missing official-example-corpus evidence for %s", file)
		}
	}
}

func observedArtifactSiteCalls(t *testing.T, paths []string, evidenceRoot string) (map[catalogKey]map[string]struct{}, map[string]int) {
	t.Helper()

	root := filepath.Join("..", filepath.FromSlash(evidenceRoot))
	observed := make(map[catalogKey]map[string]struct{})
	callsByName := make(map[string]int)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		result := lexer.Lex(lexer.NewSource(filepath.Base(path), data))
		if len(result.Diagnostics) != 0 {
			t.Fatalf("Lex(%s): %v", path, result.Diagnostics)
		}
		file, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("Rel(%s, %s): %v", root, path, err)
		}
		file = filepath.ToSlash(file)
		for index := 0; index+1 < len(result.Tokens); index++ {
			token := result.Tokens[index]
			if token.Kind != lexer.Identifier || result.Tokens[index+1].Kind != lexer.LeftParen {
				continue
			}
			if _, active := catalogAggressorArtifactSiteDescriptions[token.Lexeme]; !active {
				continue
			}
			key := catalogKey{KindFunction, token.Lexeme}
			if observed[key] == nil {
				observed[key] = make(map[string]struct{})
			}
			observed[key][file] = struct{}{}
			callsByName[token.Lexeme]++
		}
	}
	return observed, callsByName
}

func observedBeaconActionCalls(t *testing.T, paths []string, evidenceRoot string) (map[catalogKey]map[string]struct{}, int) {
	t.Helper()

	root := filepath.Join("..", filepath.FromSlash(evidenceRoot))
	observed := make(map[catalogKey]map[string]struct{})
	calls := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		result := lexer.Lex(lexer.NewSource(filepath.Base(path), data))
		if len(result.Diagnostics) != 0 {
			t.Fatalf("Lex(%s): %v", path, result.Diagnostics)
		}
		file, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("Rel(%s, %s): %v", root, path, err)
		}
		file = filepath.ToSlash(file)
		for index := 0; index+1 < len(result.Tokens); index++ {
			token := result.Tokens[index]
			if token.Kind != lexer.Identifier || result.Tokens[index+1].Kind != lexer.LeftParen {
				continue
			}
			if _, action := catalogAggressorBeaconActionDescriptions[token.Lexeme]; !action {
				continue
			}
			key := catalogKey{KindFunction, token.Lexeme}
			if observed[key] == nil {
				observed[key] = make(map[string]struct{})
			}
			observed[key][file] = struct{}{}
			calls++
		}
	}
	return observed, calls
}

func TestCatalogAggressorMenuAndKeyboardFunctionClassifications(t *testing.T) {
	t.Parallel()

	portable := map[string]string{
		"bind":        "Portable layered keyboard-shortcut registration using the documented function form and declaration binding registry.",
		"insert_menu": "Portable runtime-local composition of every exact popup-hook layer into the current menu tree.",
		"unbind":      "Portable removal of every active keyboard-shortcut layer for an exact shortcut.",
	}
	defaults := opfor.DefaultFunctionNames()
	for name, description := range portable {
		entry, ok := Lookup(KindFunction, name)
		if !ok {
			t.Errorf("Lookup(%q) did not find menu/key function", name)
			continue
		}
		if entry.Support != SupportPortableDefault || entry.Description != description {
			t.Errorf("Lookup(%q) = support %q description %q, want %q/%q",
				name, entry.Support, entry.Description, SupportPortableDefault, description)
		}
		if !hasEvidence(entry, "official-documentation", officialFunctionsURL) ||
			!hasEvidence(entry, "opfor-runtime", "portable Sleep/runtime implementation") {
			t.Errorf("Lookup(%q) evidence = %#v", name, entry.Evidence)
		}
		if !slices.Contains(defaults, name) {
			t.Errorf("DefaultFunctionNames missing %q", name)
		}
	}

	const menubarDescription = "Native Aggressor top-level menubar registration wrapper with a retained popup composer; a configured client-UI provider or embedding Host must supply UI behavior."
	menubar, ok := Lookup(KindFunction, "menubar")
	if !ok {
		t.Fatal("Lookup(\"menubar\") did not find client-UI wrapper")
	}
	if menubar.Support != SupportHostRequired || menubar.Description != menubarDescription {
		t.Errorf("Lookup(\"menubar\") = support %q description %q", menubar.Support, menubar.Description)
	}
	if !hasEvidence(menubar, "official-documentation", officialFunctionsURL) ||
		!hasEvidence(menubar, "opfor-runtime", "native importer-boundary wrapper") {
		t.Errorf("menubar runtime/documentation evidence = %#v", menubar.Evidence)
	}
	if !slices.Contains(defaults, "menubar") {
		t.Error("DefaultFunctionNames missing menubar native wrapper")
	}
}

func officialExamplePaths(t *testing.T) []string {
	t.Helper()
	return officialCorpusCNAPaths(t, "cobalt-strike-aggressor-script-examples", "program", 18)
}

func officialCorpusCNAPaths(t *testing.T, corpusID, role string, wantCount int) []string {
	t.Helper()

	manifestData, err := os.ReadFile(filepath.Join("..", "testdata", "corpus.json"))
	if err != nil {
		t.Fatalf("ReadFile(corpus.json): %v", err)
	}
	var manifest struct {
		Corpora []struct {
			ID    string `json:"id"`
			Files []struct {
				Path   string `json:"path"`
				Role   string `json:"role"`
				SHA256 string `json:"sha256"`
			} `json:"files"`
		} `json:"corpora"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("Unmarshal(corpus.json): %v", err)
	}
	var paths []string
	for _, corpus := range manifest.Corpora {
		if corpus.ID != corpusID {
			continue
		}
		for _, file := range corpus.Files {
			if file.Role != role || filepath.Ext(file.Path) != ".cna" {
				continue
			}
			path := filepath.Join("..", filepath.FromSlash(file.Path))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", path, err)
			}
			digest := sha256.Sum256(data)
			if got := fmt.Sprintf("%x", digest); got != file.SHA256 {
				t.Fatalf("%s digest = %s, want %s", path, got, file.SHA256)
			}
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) != wantCount {
		t.Fatalf("manifest %s .cna count = %d, want %d", corpusID, len(paths), wantCount)
	}
	return paths
}

func hasEvidence(entry Entry, source, reference string) bool {
	for _, evidence := range entry.Evidence {
		if evidence.Source == source && evidence.Reference == reference {
			return true
		}
	}
	return false
}

func selectorName(token lexer.Token) string {
	switch token.Kind {
	case lexer.Identifier, lexer.Keyword, lexer.SingleString, lexer.DoubleString:
		if token.Text != "" {
			return token.Text
		}
		return strings.Trim(token.Lexeme, "\"'")
	default:
		return ""
	}
}

func TestEmbeddedOfficialNamesAreUniqueAndSorted(t *testing.T) {
	t.Parallel()

	for name, values := range map[string][]string{
		"functions":   officialFunctionNames,
		"events":      officialEventNames,
		"hooks":       officialHookNames,
		"popup hooks": officialPopupHookNames,
	} {
		if !sort.StringsAreSorted(values) {
			t.Errorf("official %s names are not sorted", name)
		}
		for index := 1; index < len(values); index++ {
			if values[index] == values[index-1] {
				t.Errorf("official %s name %q is duplicated", name, values[index])
			}
		}
	}
}
