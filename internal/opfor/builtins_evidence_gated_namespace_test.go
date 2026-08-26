package opfor

import (
	"context"
	"slices"
	"testing"
)

var evidenceGatedExtensionNamesForTest = []string{
	"-d", "-e", "-f", "copyFile", "dirname", "move", "pwd",
	"contains", "containsAll", "grep", "isEmpty", "mapValues", "unshift",
	"zip", "lastIndexOf", "trim", "item", "menu",
}

var evidenceGatedPredicateNamesForTest = []string{"le", "ge", "notin", "-isnull"}

func TestEvidenceGatedExtensionsRemainHostFunctions(t *testing.T) {
	defaults := DefaultFunctionNames()
	for _, name := range evidenceGatedExtensionNamesForTest {
		if slices.Contains(defaults, name) {
			t.Errorf("DefaultFunctionNames unexpectedly contains evidence-gated extension %q", name)
		}
	}
	for _, name := range []string{"getFileParent", "rename", "-exists", "-isDir", "-isFile"} {
		if !slices.Contains(defaults, name) {
			t.Errorf("DefaultFunctionNames lost documented alias %q", name)
		}
	}

	var calls []string
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls = append(calls, invocation.Name)
		if len(invocation.Arguments) != 1 || invocation.Arg(0).String() != invocation.Name {
			t.Errorf("Host %q arguments = %#v", invocation.Name, invocation.Arguments)
		}
		return String("host-" + invocation.Name), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, name := range evidenceGatedExtensionNamesForTest {
		if slices.Contains(runtimeInstance.FunctionNames(), name) {
			t.Errorf("new Runtime unexpectedly registered evidence-gated extension %q", name)
		}
		value, invokeErr := runtimeInstance.Invoke(context.Background(), name, String(name))
		if invokeErr != nil || value.String() != "host-"+name {
			t.Errorf("Invoke(%q) Host fallback = (%s, %v)", name, value.Describe(), invokeErr)
		}
	}
	if !slices.Equal(calls, evidenceGatedExtensionNamesForTest) {
		t.Fatalf("Host calls = %q, want %q", calls, evidenceGatedExtensionNamesForTest)
	}
}

func TestEvidenceGatedFilePredicatesReachHostByExactName(t *testing.T) {
	type hostCall struct {
		name     string
		argument string
	}
	var calls []hostCall
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls = append(calls, hostCall{name: invocation.Name, argument: invocation.Arg(0).String()})
		return Bool(invocation.Name != "-f"), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Eval(context.Background(), "host-file-predicates.sl", `
$exists = 0;
$file = 0;
$directory = 0;
if (-e "exists") { $exists = 1; }
if (-f "file") { $file = 1; }
if (-d "directory") { $directory = 1; }
return @($exists, $file, $directory);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := result.Array()
	if !ok || array.Len() != 3 {
		t.Fatalf("predicate result = %s, want three values", result.Describe())
	}
	wantValues := []bool{true, false, true}
	for index, want := range wantValues {
		value, present := array.Get(index)
		if !present || value.Truth() != want {
			t.Errorf("predicate result %d = (%s, %v), want %v", index, value.Describe(), present, want)
		}
	}
	wantCalls := []hostCall{{"-e", "exists"}, {"-f", "file"}, {"-d", "directory"}}
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("predicate Host calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestEvidenceGatedPredicatesReachHostByExactName(t *testing.T) {
	type hostCall struct {
		name      string
		arguments []string
	}
	var calls []hostCall
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		arguments := make([]string, len(invocation.Arguments))
		for index, argument := range invocation.Arguments {
			arguments[index] = argument.Resolve().String()
		}
		calls = append(calls, hostCall{name: invocation.Name, arguments: arguments})
		return Bool(true), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Eval(context.Background(), "host-predicates.sl", `
$le = 0; if ("le-left" le "le-right") { $le = 1; }
$ge = 0; if ("ge-left" ge "ge-right") { $ge = 1; }
$notin = 0; if ("notin-left" notin "notin-right") { $notin = 1; }
$isnull = 0; if (-isnull "isnull-value") { $isnull = 1; }
return @($le, $ge, $notin, $isnull);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := result.Array()
	if !ok || array.Len() != len(evidenceGatedPredicateNamesForTest) {
		t.Fatalf("predicate result = %s, want %d values", result.Describe(), len(evidenceGatedPredicateNamesForTest))
	}
	for index, name := range evidenceGatedPredicateNamesForTest {
		value, present := array.Get(index)
		if !present || !value.Truth() {
			t.Errorf("predicate %q result = (%s, %v), want true", name, value.Describe(), present)
		}
	}

	wantCalls := []hostCall{
		{name: "le", arguments: []string{"le-left", "le-right"}},
		{name: "ge", arguments: []string{"ge-left", "ge-right"}},
		{name: "notin", arguments: []string{"notin-left", "notin-right"}},
		{name: "-isnull", arguments: []string{"isnull-value"}},
	}
	if !slices.EqualFunc(calls, wantCalls, func(left, right hostCall) bool {
		return left.name == right.name && slices.Equal(left.arguments, right.arguments)
	}) {
		t.Fatalf("predicate Host calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestItemAndMenuDeclarationsRemainStockEnvironments(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	script, err := runtimeInstance.Load(context.Background(), mustCompileMenuBindingTest(t, "declaration-menu.cna", `
popup declaration_root {
	menu "Tools" {
		item "Run" { return "clicked"; }
	}
}
`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })

	if _, err := runtimeInstance.DispatchPopupHook(context.Background(), "declaration_root"); err != nil {
		t.Fatal(err)
	}
	menu := onlyBinding(t, runtimeInstance.Bindings(BindingMenu, "Tools"))
	if _, err := runtimeInstance.InvokeBindingByID(context.Background(), menu.Script, menu.ID); err != nil {
		t.Fatal(err)
	}
	item := onlyBinding(t, runtimeInstance.Bindings(BindingItem, "Run"))
	value, err := runtimeInstance.InvokeBindingByID(context.Background(), item.Script, item.ID)
	if err != nil || value.String() != "clicked" {
		t.Fatalf("declaration item callback = (%s, %v), want clicked", value.Describe(), err)
	}
}
