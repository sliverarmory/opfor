package opfor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAggressorCommandFamiliesFullContractAndIsolation(t *testing.T) {
	runtimeInstance, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	program, err := CompileString("command-families.cna", `
$beacon_group = beacon_command_group("bg", "Beacon Group", "Beacon group detail");
$ssh_group = ssh_command_group("sg", "SSH Group", "SSH group detail");
$beacon_register = beacon_command_register("shared", "beacon short", "beacon long", "bg");
$ssh_register = ssh_command_register("shared", "ssh short", "ssh long", "sg");
$ungrouped_register = beacon_command_register("ungrouped", "u short", "u long", "missing");
$late_group = beacon_command_group("missing", "Late", "must not retroactively associate");
$beacon_short = beacon_command_describe("shared");
$beacon_long = beacon_command_detail("shared");
$ssh_short = ssh_command_describe("shared");
$ssh_long = ssh_command_detail("shared");
$beacon_missing = beacon_command_describe("missing");
$ssh_missing = ssh_command_detail("missing");
@beacon_names = beacon_commands();
@ssh_names = ssh_commands();
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"$beacon_group", "$ssh_group", "$beacon_register", "$ssh_register", "$ungrouped_register", "$late_group", "$beacon_missing", "$ssh_missing"} {
		if !script.Get(name).IsNull() {
			t.Errorf("%s = %s, want provisional $null", name, script.Get(name).Describe())
		}
	}
	for name, want := range map[string]string{
		"$beacon_short": "beacon short",
		"$beacon_long":  "beacon long",
		"$ssh_short":    "ssh short",
		"$ssh_long":     "ssh long",
	} {
		if got := script.Get(name).String(); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got, want := commandValueNames(t, script.Get("@beacon_names")), []string{"shared", "ungrouped"}; !reflect.DeepEqual(got, want) {
		t.Errorf("beacon_commands = %q, want %q", got, want)
	}
	if got, want := commandValueNames(t, script.Get("@ssh_names")), []string{"shared"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ssh_commands = %q, want %q", got, want)
	}

	beacon, err := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := beacon.Groups, []AggressorCommandGroup{{ID: "bg", Name: "Beacon Group", Description: "Beacon group detail"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Beacon groups = %#v, want %#v", got, want)
	}
	if got, want := beacon.Commands, []AggressorCommandMetadata{
		{Name: "shared", Description: "beacon short", Detail: "beacon long", GroupID: "bg"},
		{Name: "ungrouped", Description: "u short", Detail: "u long"},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("Beacon commands = %#v, want %#v", got, want)
	}
}

func TestOfficialPortfwdCommandMetadataAndAliasFixture(t *testing.T) {
	const fixture = "testdata/upstream/aggressor-script-examples/portfwd.cna"
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource(fixture, data))
	if err != nil {
		t.Fatal(err)
	}
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	const detail = "Synopsis: portfwd [stop|<host> <port>]\nCreate a port forward <team server>:<port> -> current beacon -> <host>:<port>"
	want := []AggressorCommandMetadata{{Name: "portfwd", Description: "create a port forward", Detail: detail}}
	for _, kind := range []AggressorCommandKind{AggressorCommandBeacon, AggressorCommandSSH} {
		catalog, err := runtimeInstance.SnapshotAggressorCommandCatalog(kind)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(catalog.Commands, want) || len(catalog.Groups) != 0 {
			t.Errorf("%s portfwd catalog = %#v, want %#v", kind, catalog, want)
		}
	}
	if got := runtimeInstance.Bindings(BindingAlias, "portfwd"); len(got) != 1 {
		t.Fatalf("portfwd Beacon aliases = %#v, want one", got)
	}
	if got := runtimeInstance.Bindings(BindingSSHAlias, "portfwd"); len(got) != 1 {
		t.Fatalf("portfwd SSH aliases = %#v, want one", got)
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []AggressorCommandKind{AggressorCommandBeacon, AggressorCommandSSH} {
		catalog, _ := runtimeInstance.SnapshotAggressorCommandCatalog(kind)
		if len(catalog.Commands) != 0 || len(catalog.Groups) != 0 {
			t.Errorf("%s portfwd catalog survived unload: %#v", kind, catalog)
		}
	}
}

func TestSSHAliasFunctionAndEnvironmentFormsShareRegistryAndABI(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("ssh-alias-forms.cna", `
$registration = ssh_alias("function", { return @($0, $1, $2); });
ssh_alias environment { return @($0, $1, $2); }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if !script.Get("$registration").IsNull() {
		t.Fatalf("ssh_alias function result = %s, want $null", script.Get("$registration").Describe())
	}
	for _, name := range []string{"function", "environment"} {
		bindings := runtimeInstance.Bindings(BindingSSHAlias, name)
		if len(bindings) != 1 || bindings[0].Keyword != "ssh_alias" || bindings[0].Name != name {
			t.Fatalf("ssh_alias %q bindings = %#v", name, bindings)
		}
		result, err := runtimeInstance.InvokeConsole(context.Background(), ConsoleInvocation{
			Kind:      BindingSSHAlias,
			Name:      name,
			RawInput:  name + ` "two words"`,
			SessionID: String("ssh-session"),
		})
		if err != nil {
			t.Fatalf("InvokeConsole %q: %v", name, err)
		}
		if got, want := commandValueNames(t, result), []string{name + ` "two words"`, "ssh-session", "two words"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("ssh_alias %q ABI = %q, want %q", name, got, want)
		}
	}
}

func TestAggressorCommandCatalogBaseLayersDefensiveCopyAndUnloadRestore(t *testing.T) {
	base := AggressorCommandCatalog{
		Groups: []AggressorCommandGroup{{ID: "base", Name: "Base", Description: "base group"}},
		Commands: []AggressorCommandMetadata{
			{Name: "alpha", Description: "alpha base", Detail: "alpha detail", GroupID: "base"},
			{Name: "shared", Description: "shared base", Detail: "shared base detail"},
			{Name: "omega", Description: "omega base", Detail: "omega detail"},
		},
	}
	runtimeInstance, err := New(WithAggressorCommandCatalog(AggressorCommandBeacon, base))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	base.Commands[0].Description = "mutated"
	base.Commands = nil
	base.Groups[0].Name = "mutated"

	first := loadAggressorCommandScript(t, runtimeInstance, "first.cna", `
beacon_command_group("dynamic", "Dynamic One", "group one");
beacon_command_register("shared", "shared one", "detail one", "dynamic");
beacon_command_register("new", "new one", "new detail");
`)
	second := loadAggressorCommandScript(t, runtimeInstance, "second.cna", `
beacon_command_group("dynamic", "Dynamic Two", "group two");
beacon_command_register("shared", "shared two", "detail two", "dynamic");
`)

	assertAggressorCommandDescription(t, runtimeInstance, "beacon_command_describe", "shared", "shared two")
	beacon, err := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := commandCatalogNames(beacon), []string{"alpha", "shared", "omega", "new"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("layered order = %q, want %q", got, want)
	}
	if beacon.Commands[0].Description != "alpha base" || beacon.Groups[0].Name != "Base" {
		t.Fatalf("base catalog was not defensively copied: %#v", beacon)
	}
	if got := beacon.Groups[len(beacon.Groups)-1].Name; got != "Dynamic Two" {
		t.Fatalf("winning group = %q, want Dynamic Two", got)
	}
	beacon.Commands[0].Description = "snapshot mutation"
	beacon.Groups[0].Name = "snapshot mutation"
	detached, _ := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if detached.Commands[0].Description != "alpha base" || detached.Groups[0].Name != "Base" {
		t.Fatalf("snapshot was not detached: %#v", detached)
	}

	if err := second.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertAggressorCommandDescription(t, runtimeInstance, "beacon_command_describe", "shared", "shared one")
	beacon, _ = runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if got := beacon.Groups[len(beacon.Groups)-1].Name; got != "Dynamic One" {
		t.Fatalf("restored group = %q, want Dynamic One", got)
	}

	if err := first.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertAggressorCommandDescription(t, runtimeInstance, "beacon_command_describe", "shared", "shared base")
	beacon, _ = runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if got, want := commandCatalogNames(beacon), []string{"alpha", "shared", "omega"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("restored base order = %q, want %q", got, want)
	}
}

func TestAggressorCommandSameOwnerRegistrationsCoalesceAndRestoreLayers(t *testing.T) {
	base := AggressorCommandCatalog{
		Groups: []AggressorCommandGroup{{ID: "g", Name: "Base", Description: "base"}},
		Commands: []AggressorCommandMetadata{{
			Name: "shared", Description: "base", Detail: "base", GroupID: "g",
		}},
	}
	runtimeInstance, err := New(WithAggressorCommandCatalog(AggressorCommandBeacon, base))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	first := loadAggressorCommandScript(t, runtimeInstance, "coalesce-first.cna", `
sub update_help {
    beacon_command_group("g", $1, $1);
    beacon_command_register("shared", $1, $1, "g");
}
`)
	for iteration := 0; iteration < 500; iteration++ {
		label := fmt.Sprintf("first-%03d", iteration)
		if _, err := first.Call(context.Background(), "update_help", String(label)); err != nil {
			t.Fatal(err)
		}
	}
	second := loadAggressorCommandScript(t, runtimeInstance, "coalesce-second.cna", `
beacon_command_group("g", "second", "second");
beacon_command_register("shared", "second", "second", "g");
`)
	if _, err := first.Call(context.Background(), "update_help", String("first-final")); err != nil {
		t.Fatal(err)
	}

	runtimeInstance.aggressorCommands.mu.RLock()
	namespace := runtimeInstance.aggressorCommands.namespaces[AggressorCommandBeacon]
	commandLayers := len(namespace.commands["shared"])
	groupLayers := len(namespace.groups["g"])
	runtimeInstance.aggressorCommands.mu.RUnlock()
	if commandLayers != 3 || groupLayers != 3 {
		t.Fatalf("coalesced layer counts = commands %d, groups %d; want base plus two owners", commandLayers, groupLayers)
	}
	catalog, _ := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if catalog.Commands[0].Description != "first-final" || catalog.Groups[0].Name != "first-final" {
		t.Fatalf("same-owner layer did not move to top: %#v", catalog)
	}

	if err := first.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	catalog, _ = runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if catalog.Commands[0].Description != "second" || catalog.Groups[0].Name != "second" {
		t.Fatalf("cross-owner layer restore = %#v, want second", catalog)
	}
	if err := second.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	catalog, _ = runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if catalog.Commands[0].Description != "base" || catalog.Groups[0].Name != "Base" {
		t.Fatalf("base layer restore = %#v", catalog)
	}
}

func TestAggressorCommandCrossScriptGroupUnloadDoesNotExposeDanglingAssociation(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	groupOwner := loadAggressorCommandScript(t, runtimeInstance, "group-owner.cna",
		`beacon_command_group("transient", "Transient", "temporary");`)
	commandOwner := loadAggressorCommandScript(t, runtimeInstance, "command-owner.cna",
		`beacon_command_register("cross", "cross short", "cross detail", "transient");`)

	catalog, _ := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if len(catalog.Commands) != 1 || catalog.Commands[0].GroupID != "transient" {
		t.Fatalf("associated command = %#v", catalog.Commands)
	}
	if err := groupOwner.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	catalog, _ = runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if len(catalog.Commands) != 1 || catalog.Commands[0].GroupID != "" {
		t.Fatalf("command after group unload = %#v, want no dangling group", catalog.Commands)
	}
	if err := commandOwner.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAggressorCommandGroupsRemainHiddenUntilReferenced(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	groupOwner := loadAggressorCommandScript(t, runtimeInstance, "hidden-group.cna",
		`beacon_command_group("hidden", "Hidden", "not listed alone");`)
	catalog, _ := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if len(catalog.Groups) != 0 {
		t.Fatalf("unreferenced groups = %#v, want hidden", catalog.Groups)
	}
	commandOwner := loadAggressorCommandScript(t, runtimeInstance, "show-group.cna",
		`beacon_command_register("shown", "shown", "shown", "hidden");`)
	catalog, _ = runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if got, want := catalog.Groups, []AggressorCommandGroup{{ID: "hidden", Name: "Hidden", Description: "not listed alone"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("referenced groups = %#v, want %#v", got, want)
	}
	if err := commandOwner.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	catalog, _ = runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if len(catalog.Groups) != 0 {
		t.Fatalf("group after last command unload = %#v, want hidden", catalog.Groups)
	}
	if err := groupOwner.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAggressorCommandRegistrationRollback(t *testing.T) {
	base := AggressorCommandCatalog{Commands: []AggressorCommandMetadata{{
		Name: "shared", Description: "base", Detail: "base detail",
	}}}
	rollbackErr := errors.New("rollback")
	runtimeInstance, err := New(
		WithAggressorCommandCatalog(AggressorCommandBeacon, base),
		WithFunction("fail_load", func(context.Context, Invocation) (Value, error) {
			return Null(), rollbackErr
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("rollback.cna", `
beacon_command_register("shared", "temporary", "temporary detail");
beacon_command_register("leak", "leak", "leak");
fail_load();
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); !errors.Is(err, rollbackErr) {
		t.Fatalf("Load error = %v, want rollback", err)
	}
	assertAggressorCommandDescription(t, runtimeInstance, "beacon_command_describe", "shared", "base")
	catalog, _ := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if got, want := commandCatalogNames(catalog), []string{"shared"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog after rollback = %q, want %q", got, want)
	}
}

func TestAggressorCommandCatalogValidationAndProvisionalExactArities(t *testing.T) {
	invalidCatalogs := []struct {
		name    string
		kind    AggressorCommandKind
		catalog AggressorCommandCatalog
		want    string
	}{
		{name: "kind", kind: "other", want: "invalid Aggressor command kind"},
		{name: "empty-command", kind: AggressorCommandBeacon, catalog: AggressorCommandCatalog{Commands: []AggressorCommandMetadata{{}}}, want: "empty name"},
		{name: "duplicate-command", kind: AggressorCommandBeacon, catalog: AggressorCommandCatalog{Commands: []AggressorCommandMetadata{{Name: "x"}, {Name: "x"}}}, want: "duplicate command"},
		{name: "invalid-group", kind: AggressorCommandBeacon, catalog: AggressorCommandCatalog{Groups: []AggressorCommandGroup{{ID: "bad@id", Name: "Bad"}}}, want: "contains ',' or '@'"},
		{name: "duplicate-group", kind: AggressorCommandBeacon, catalog: AggressorCommandCatalog{Groups: []AggressorCommandGroup{{ID: "g", Name: "G"}, {ID: "g", Name: "G2"}}}, want: "duplicate group"},
		{name: "unknown-group", kind: AggressorCommandBeacon, catalog: AggressorCommandCatalog{Commands: []AggressorCommandMetadata{{Name: "x", GroupID: "missing"}}}, want: "unknown group"},
	}
	for _, test := range invalidCatalogs {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(WithAggressorCommandCatalog(test.kind, test.catalog))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New error = %v, want %q", err, test.want)
			}
		})
	}

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	tests := []struct {
		name   string
		values []Value
		want   string
	}{
		{name: "alias_clear", want: "exactly 1"},
		{name: "alias_clear", values: []Value{String("x"), String("y")}, want: "exactly 1"},
		{name: "beacon_command_describe", want: "exactly 1"},
		{name: "beacon_command_detail", values: []Value{String("x"), String("y")}, want: "exactly 1"},
		{name: "beacon_command_group", values: []Value{String("x"), String("y")}, want: "exactly 3"},
		{name: "beacon_command_register", values: []Value{String("x"), String("y")}, want: "expected 3 or 4"},
		{name: "beacon_command_register", values: []Value{String("x"), String("y"), String("z"), String("g"), String("extra")}, want: "expected 3 or 4"},
		{name: "beacon_commands", values: []Value{String("extra")}, want: "exactly 0"},
		{name: "ssh_command_describe", want: "exactly 1"},
		{name: "ssh_command_detail", values: []Value{String("x"), String("y")}, want: "exactly 1"},
		{name: "ssh_command_group", values: []Value{String("x"), String("y"), String("z"), String("extra")}, want: "exactly 3"},
		{name: "ssh_command_register", values: []Value{String("x")}, want: "expected 3 or 4"},
		{name: "ssh_commands", values: []Value{String("extra")}, want: "exactly 0"},
	}
	for index, test := range tests {
		t.Run(fmt.Sprintf("%02d-%s", index, test.name), func(t *testing.T) {
			_, err := runtimeInstance.Invoke(context.Background(), test.name, test.values...)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Invoke error = %v, want %q", err, test.want)
			}
		})
	}
	for _, function := range []string{"beacon_command_describe", "beacon_command_detail", "ssh_command_describe", "ssh_command_detail"} {
		value, err := runtimeInstance.Invoke(context.Background(), function, String("missing"))
		if err != nil || !value.IsNull() {
			t.Errorf("%s missing = (%s, %v), want $null", function, value.Describe(), err)
		}
	}
}

func TestPortableScriptLoaderInheritsAggressorCommandBaseWithoutParentOverlay(t *testing.T) {
	base := AggressorCommandCatalog{
		Groups: []AggressorCommandGroup{{ID: "child-base", Name: "Child Base", Description: "inherited even while hidden"}},
		Commands: []AggressorCommandMetadata{{
			Name: "base", Description: "base short", Detail: "base detail",
		}},
	}
	runtimeInstance, err := New(
		WithAggressorCommandCatalog(AggressorCommandBeacon, base),
		WithFunction("child_group_id", func(_ context.Context, invocation Invocation) (Value, error) {
			catalog, snapshotErr := invocation.Runtime.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
			if snapshotErr != nil {
				return Null(), snapshotErr
			}
			for _, command := range catalog.Commands {
				if command.Name == invocation.Arg(0).String() {
					return String(command.GroupID), nil
				}
			}
			return Null(), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("loader-command-catalog.sl", `
import sleep.runtime.ScriptLoader;
beacon_command_register("parent", "parent short", "parent detail");
$loader = [new ScriptLoader];
$child = [$loader loadScript: "child", 'beacon_command_register("child", "child short", "child detail", "child-base"); return @(beacon_command_describe("base"), beacon_command_describe("parent"), beacon_command_describe("child"), child_group_id("child"));', $null];
return [$child runScript];
`)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	values := commandValueSlice(t, parent.Result())
	if len(values) != 4 || values[0].String() != "base short" || !values[1].IsNull() || values[2].String() != "child short" || values[3].String() != "child-base" {
		t.Fatalf("child catalog result = %s", parent.Result().Describe())
	}
	parentCatalog, _ := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if got, want := commandCatalogNames(parentCatalog), []string{"base", "parent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parent catalog = %q, want %q", got, want)
	}
}

func TestAliasClearRemovesAllBeaconLayersOnlyAndNotifiesOnce(t *testing.T) {
	observer := &commandBindingObserver{unregistered: make(map[string]int)}
	runtimeInstance, err := New(WithBindingObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	first := loadAggressorCommandScript(t, runtimeInstance, "alias-first.cna", `
alias shared { return "first"; }
ssh_alias shared { return "ssh"; }
beacon_command_register("shared", "help", "detail");
`)
	second := loadAggressorCommandScript(t, runtimeInstance, "alias-second.cna", `alias shared { return "second"; }`)
	aliases := runtimeInstance.Bindings(BindingAlias, "shared")
	if len(aliases) != 2 {
		t.Fatalf("Beacon aliases before clear = %#v, want two", aliases)
	}

	value, err := runtimeInstance.Invoke(context.Background(), "alias_clear", String("shared"))
	if err != nil || !value.IsNull() {
		t.Fatalf("alias_clear = (%s, %v), want $null", value.Describe(), err)
	}
	if got := runtimeInstance.Bindings(BindingAlias, "shared"); len(got) != 0 {
		t.Fatalf("Beacon aliases after clear = %#v", got)
	}
	if got := runtimeInstance.Bindings(BindingSSHAlias, "shared"); len(got) != 1 {
		t.Fatalf("SSH aliases after clear = %#v, want one", got)
	}
	assertAggressorCommandDescription(t, runtimeInstance, "beacon_command_describe", "shared", "help")
	if _, err := runtimeInstance.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingAlias, Name: "shared", RawInput: "shared", SessionID: String("beacon"),
	}); err == nil {
		t.Fatal("cleared Beacon alias remained invokable")
	}
	sshValue, err := runtimeInstance.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingSSHAlias, Name: "shared", RawInput: "shared", SessionID: String("ssh"),
	})
	if err != nil || sshValue.String() != "ssh" {
		t.Fatalf("SSH alias = (%s, %v), want ssh", sshValue.Describe(), err)
	}

	for _, binding := range aliases {
		if got := observer.unregisteredCount(binding); got != 1 {
			t.Errorf("alias %d/%d Unregistered count = %d, want 1", binding.Script, binding.ID, got)
		}
	}
	if err := second.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := first.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, binding := range aliases {
		if got := observer.unregisteredCount(binding); got != 1 {
			t.Errorf("alias %d/%d Unregistered count after unload = %d, want 1", binding.Script, binding.ID, got)
		}
	}
	for key, count := range observer.snapshotUnregistered() {
		if count != 1 {
			t.Errorf("binding %s Unregistered count = %d, want 1", key, count)
		}
	}
}

func TestAliasClearConcurrentUnloadNotifiesExactlyOnce(t *testing.T) {
	observer := &commandBindingObserver{unregistered: make(map[string]int)}
	runtimeInstance, err := New(WithBindingObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for iteration := 0; iteration < 100; iteration++ {
		name := fmt.Sprintf("race-%03d", iteration)
		script := loadAggressorCommandScript(t, runtimeInstance, name+".cna", fmt.Sprintf(`alias %s { return; }`, name))
		binding := script.Bindings()[0]
		start := make(chan struct{})
		var wait sync.WaitGroup
		var unloadErr, clearErr error
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			unloadErr = script.Unload(context.Background())
		}()
		go func() {
			defer wait.Done()
			<-start
			_, clearErr = runtimeInstance.Invoke(context.Background(), "alias_clear", String(name))
		}()
		close(start)
		wait.Wait()
		if unloadErr != nil || clearErr != nil {
			t.Fatalf("iteration %d errors = unload %v, clear %v", iteration, unloadErr, clearErr)
		}
		if got := observer.unregisteredCount(binding); got != 1 {
			t.Fatalf("iteration %d Unregistered count = %d, want 1", iteration, got)
		}
	}
}

func TestAggressorCommandStateConcurrentSnapshotsAndUnload(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("concurrent-command-state.cna", `
sub register_help {
    beacon_command_register($1, $2, $3);
}
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 100; iteration++ {
				name := fmt.Sprintf("w%d-%03d", worker, iteration)
				_, callErr := script.Call(context.Background(), "register_help", String(name), String(name+" short"), String(name+" detail"))
				if callErr != nil && !errors.Is(callErr, ErrScriptUnloaded) && !errors.Is(callErr, context.Canceled) {
					t.Errorf("register_help: %v", callErr)
					return
				}
				if _, snapshotErr := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon); snapshotErr != nil {
					t.Errorf("Snapshot: %v", snapshotErr)
					return
				}
			}
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		_ = script.Unload(context.Background())
	}()
	wait.Wait()
	catalog, _ := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if len(catalog.Commands) != 0 {
		t.Fatalf("script-owned commands survived unload: %d", len(catalog.Commands))
	}
}

func TestAggressorCommandLayersRevokeAtUnloadAdmission(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	runtimeInstance, err := New(WithFunction("block_unload", func(context.Context, Invocation) (Value, error) {
		enteredOnce.Do(func() { close(entered) })
		<-release
		return Null(), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	script := loadAggressorCommandScript(t, runtimeInstance, "blocked-unload.cna", `
beacon_command_register("transient", "transient", "transient");
sub hold_unload { block_unload(); }
`)
	callDone := make(chan error, 1)
	go func() {
		_, callErr := script.Call(context.Background(), "hold_unload")
		callDone <- callErr
	}()
	<-entered

	unloadContext, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	unloadErr := script.Unload(unloadContext)
	cancel()
	if !errors.Is(unloadErr, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("blocked Unload error = %v, want deadline exceeded", unloadErr)
	}
	catalog, _ := runtimeInstance.SnapshotAggressorCommandCatalog(AggressorCommandBeacon)
	if len(catalog.Commands) != 0 {
		close(release)
		t.Fatalf("commands visible after unload admission = %#v", catalog.Commands)
	}
	close(release)
	if callErr := <-callDone; callErr != nil && !errors.Is(callErr, context.Canceled) && !errors.Is(callErr, ErrScriptUnloaded) {
		t.Fatalf("blocked call error = %v", callErr)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func loadAggressorCommandScript(t *testing.T, runtimeInstance *Runtime, name, source string) *Script {
	t.Helper()
	program, err := CompileString(name, source)
	if err != nil {
		t.Fatalf("CompileString(%s): %v", name, err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatalf("Load(%s): %v", name, err)
	}
	return script
}

func assertAggressorCommandDescription(t *testing.T, runtimeInstance *Runtime, function, name, want string) {
	t.Helper()
	value, err := runtimeInstance.Invoke(context.Background(), function, String(name))
	if err != nil {
		t.Fatalf("%s(%q): %v", function, name, err)
	}
	if got := value.String(); got != want {
		t.Fatalf("%s(%q) = %q, want %q", function, name, got, want)
	}
}

func commandCatalogNames(catalog AggressorCommandCatalog) []string {
	names := make([]string, len(catalog.Commands))
	for index, command := range catalog.Commands {
		names[index] = command.Name
	}
	return names
}

func commandValueSlice(t *testing.T, value Value) []Value {
	t.Helper()
	array, ok := value.Array()
	if !ok || array == nil {
		t.Fatalf("value = %s, want array", value.Describe())
	}
	return array.Values()
}

func commandValueNames(t *testing.T, value Value) []string {
	t.Helper()
	values := commandValueSlice(t, value)
	names := make([]string, len(values))
	for index, value := range values {
		names[index] = value.String()
	}
	return names
}

type commandBindingObserver struct {
	mu           sync.Mutex
	unregistered map[string]int
}

func (observer *commandBindingObserver) Registered(context.Context, Binding) error { return nil }

func (observer *commandBindingObserver) Unregistered(_ context.Context, binding Binding) error {
	observer.mu.Lock()
	observer.unregistered[commandBindingKey(binding)]++
	observer.mu.Unlock()
	return nil
}

func (observer *commandBindingObserver) unregisteredCount(binding Binding) int {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return observer.unregistered[commandBindingKey(binding)]
}

func (observer *commandBindingObserver) snapshotUnregistered() map[string]int {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	result := make(map[string]int, len(observer.unregistered))
	for key, count := range observer.unregistered {
		result[key] = count
	}
	return result
}

func commandBindingKey(binding Binding) string {
	return fmt.Sprintf("%d/%d", binding.Script, binding.ID)
}
