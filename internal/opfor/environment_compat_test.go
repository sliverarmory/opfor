package opfor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
)

const interpolatedEnvironmentNameProbe = `$suffix = "name";
sub "dynamic_$suffix" { return "ok:" . $1; }
println(dynamic_name("value"));
`

func TestOrdinaryEnvironmentEvaluatesInterpolatedNameAtRegistration(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	program, err := runtime.CompileString("interpolated-environment.sl", interpolatedEnvironmentNameProbe)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "ok:value\n" {
		t.Fatalf("output = %q, want %q", got, "ok:value\n")
	}
	bindings := runtime.Bindings(BindingSub, "dynamic_name")
	if len(bindings) != 1 {
		t.Fatalf("dynamic_name binding count = %d, want 1", len(bindings))
	}
	binding := bindings[0]
	if binding.Keyword != "sub" || binding.Environment != EnvironmentOrdinary {
		t.Fatalf("binding kind = %q/%d, want sub/ordinary", binding.Keyword, binding.Environment)
	}
	if len(binding.Selectors) != 1 || binding.Selectors[0].Raw != `"dynamic_$suffix"` ||
		!binding.Selectors[0].Evaluated || binding.Selectors[0].Value.String() != "dynamic_name" {
		t.Fatalf("binding selectors = %#v", binding.Selectors)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestImporterFilterAndPredicateEnvironmentABI(t *testing.T) {
	runtime, err := New(
		WithEnvironment("capture_filter", EnvironmentFilter),
		WithEnvironment("capture_predicate", EnvironmentPredicate),
	)
	if err != nil {
		t.Fatal(err)
	}
	program, err := runtime.CompileString("importer-environments.sl", `
$threshold = 2;
capture_filter alert "score  > $threshold" { return $1; }
capture_predicate ($threshold == 2 && $1 eq "ok") { return $1; }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	filters := runtime.Bindings(BindingKind("capture_filter"), "alert")
	if len(filters) != 1 {
		t.Fatalf("filter binding count = %d, want 1", len(filters))
	}
	filter := filters[0]
	if filter.Environment != EnvironmentFilter || filter.Filter != `"score  > $threshold"` {
		t.Fatalf("filter metadata = environment %d filter %q", filter.Environment, filter.Filter)
	}
	if got := []string{filter.Selectors[0].Raw, filter.Selectors[1].Raw}; !reflect.DeepEqual(got, []string{"alert", `"score  > $threshold"`}) {
		t.Fatalf("filter selector order = %q", got)
	}
	if filter.Selectors[0].Evaluated || filter.Selectors[1].Evaluated {
		t.Fatalf("filter selectors unexpectedly evaluated: %#v", filter.Selectors)
	}
	filteredValue, err := runtime.InvokeBinding(context.Background(), BindingKind("capture_filter"), "alert", String("filtered"))
	if err != nil || filteredValue.String() != "filtered" {
		t.Fatalf("filter callback = %s, %v; want filtered", filteredValue.Describe(), err)
	}

	predicates := runtime.Bindings(BindingKind("capture_predicate"), "")
	if len(predicates) != 1 {
		t.Fatalf("predicate binding count = %d, want 1", len(predicates))
	}
	predicate := predicates[0]
	if predicate.Environment != EnvironmentPredicate || predicate.Name != "" || predicate.Predicate == nil {
		t.Fatalf("predicate metadata = %#v", predicate)
	}
	if len(predicate.Selectors) != 1 || predicate.Selectors[0].Evaluated || predicate.Selectors[0].Raw != `($threshold == 2 && $1 eq "ok")` {
		t.Fatalf("predicate selectors = %#v", predicate.Selectors)
	}
	matched, err := predicate.Predicate.Evaluate(context.Background(), String("ok"))
	if err != nil || !matched {
		t.Fatalf("predicate(ok) = %v, %v; want true", matched, err)
	}
	matched, err = predicate.Predicate.Evaluate(context.Background(), String("no"))
	if err != nil || matched {
		t.Fatalf("predicate(no) = %v, %v; want false", matched, err)
	}
	predicateValue, err := runtime.InvokeBinding(context.Background(), BindingKind("capture_predicate"), "", String("predicate"))
	if err != nil || predicateValue.String() != "predicate" {
		t.Fatalf("predicate callback = %s, %v; want predicate", predicateValue.Describe(), err)
	}
	if err := script.Set("$threshold", Int(3)); err != nil {
		t.Fatal(err)
	}
	matched, err = predicate.Predicate.Evaluate(context.Background(), String("ok"))
	if err != nil || matched {
		t.Fatalf("predicate after global mutation = %v, %v; want false", matched, err)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := predicate.Predicate.Evaluate(context.Background(), String("ok")); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("predicate after unload error = %v, want ErrScriptUnloaded", err)
	}
}

func TestEnvironmentRegistrationPropagatesToPortableScriptLoaderChild(t *testing.T) {
	parent, err := New(WithEnvironment("guard", EnvironmentPredicate))
	if err != nil {
		t.Fatal(err)
	}
	instance := &portableScriptInstance{
		loader: &portableScriptLoader{runtime: parent},
		debug:  1,
	}
	child, err := instance.newChildRuntime(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close(context.Background())
	program, err := child.CompileString("child-environment.sl", `guard ($1 eq "ok") { return; }`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := child.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	bindings := child.Bindings(BindingKind("guard"), "")
	if len(bindings) != 1 || bindings[0].Environment != EnvironmentPredicate || bindings[0].Predicate == nil {
		t.Fatalf("child predicate bindings = %#v", bindings)
	}
}

func TestUninstalledFilterAndPredicateEnvironmentWarnings(t *testing.T) {
	tests := []struct {
		name   string
		kind   EnvironmentKind
		source string
	}{
		{name: "filtered", kind: EnvironmentFilter, source: `custom target "raw" { return; }`},
		{name: "predicate", kind: EnvironmentPredicate, source: `custom (1 == 1) { return; }`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, err := CompileString(test.name+".sl", test.source, WithCompileEnvironment("custom", test.kind))
			if err != nil {
				t.Fatal(err)
			}
			var warnings bytes.Buffer
			runtime, err := New(WithStderr(&warnings))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.Execute(context.Background(), program); err != nil {
				t.Fatal(err)
			}
			want := "Warning: Attempting to bind code to non-existent predicate environment: custom at " + test.name + ".sl:1\n"
			if got := warnings.String(); got != want {
				t.Fatalf("warning = %q, want %q", got, want)
			}
		})
	}
}

func TestRepeatedPopupCompositionReplacesDescendantsAndCarriesParentContext(t *testing.T) {
	observer := &environmentBindingObserver{}
	runtime, err := New(WithBindingObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	program, err := runtime.CompileString("popup-context.cna", `
popup root {
    menu "Tools" {
        item "Action" { return $1; }
    }
    item "Direct" { return $1; }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.InvokeBinding(context.Background(), BindingPopup, "root", String("first")); err != nil {
		t.Fatal(err)
	}
	firstMenu := onlyBinding(t, runtime.Bindings(BindingMenu, "Tools"))
	firstDirect := onlyBinding(t, runtime.Bindings(BindingItem, "Direct"))
	assertBindingParent(t, firstMenu.Parent, BindingPopup, "root", []string{"first"})
	assertBindingParent(t, firstDirect.Parent, BindingPopup, "root", []string{"first"})

	if _, err := runtime.InvokeBinding(context.Background(), BindingMenu, "Tools", String("menu-first")); err != nil {
		t.Fatal(err)
	}
	firstAction := onlyBinding(t, runtime.Bindings(BindingItem, "Action"))
	assertBindingParent(t, firstAction.Parent, BindingMenu, "Tools", []string{"menu-first"})
	assertBindingParent(t, firstAction.Parent.Parent, BindingPopup, "root", []string{"first"})

	if _, err := runtime.InvokeBinding(context.Background(), BindingPopup, "root", String("second")); err != nil {
		t.Fatal(err)
	}
	secondMenu := onlyBinding(t, runtime.Bindings(BindingMenu, "Tools"))
	secondDirect := onlyBinding(t, runtime.Bindings(BindingItem, "Direct"))
	if secondMenu.ID == firstMenu.ID || secondDirect.ID == firstDirect.ID {
		t.Fatalf("second composition reused retired binding IDs: menu %d/%d direct %d/%d", firstMenu.ID, secondMenu.ID, firstDirect.ID, secondDirect.ID)
	}
	if got := len(runtime.Bindings(BindingItem, "Action")); got != 0 {
		t.Fatalf("old nested Action count = %d, want 0 before new menu composition", got)
	}
	assertBindingParent(t, secondMenu.Parent, BindingPopup, "root", []string{"second"})
	assertBindingParent(t, secondDirect.Parent, BindingPopup, "root", []string{"second"})
	value, err := runtime.InvokeBinding(context.Background(), BindingItem, "Direct")
	if err != nil || value.String() != "second" {
		t.Fatalf("second Direct result = %s, %v; want second", value.Describe(), err)
	}
	if _, err := runtime.InvokeBinding(context.Background(), BindingMenu, "Tools", String("menu-second")); err != nil {
		t.Fatal(err)
	}
	secondAction := onlyBinding(t, runtime.Bindings(BindingItem, "Action"))
	assertBindingParent(t, secondAction.Parent, BindingMenu, "Tools", []string{"menu-second"})
	assertBindingParent(t, secondAction.Parent.Parent, BindingPopup, "root", []string{"second"})

	if observer.unregistered < 3 {
		t.Fatalf("unregistered descendant count = %d, want at least 3", observer.unregistered)
	}
}

func TestInterpolatedEnvironmentNameOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	want, err := officialSleepJavaCommand(java, "-jar", jar, "-e", interpolatedEnvironmentNameProbe).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep probe: %v\n%s", err, want)
	}
	var got bytes.Buffer
	runtime, err := New(WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Eval(context.Background(), "interpolated-environment.sl", interpolatedEnvironmentNameProbe); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("environment differential mismatch\nofficial:\n%s\nopfor:\n%s", want, got.Bytes())
	}
}

type environmentBindingObserver struct {
	unregistered int
}

func (*environmentBindingObserver) Registered(context.Context, Binding) error { return nil }
func (observer *environmentBindingObserver) Unregistered(context.Context, Binding) error {
	observer.unregistered++
	return nil
}

func onlyBinding(t *testing.T, bindings []Binding) Binding {
	t.Helper()
	if len(bindings) != 1 {
		t.Fatalf("binding count = %d, want 1", len(bindings))
	}
	return bindings[0]
}

func assertBindingParent(t *testing.T, parent *BindingInvocation, kind BindingKind, name string, arguments []string) {
	t.Helper()
	if parent == nil || parent.Kind != kind || parent.Name != name {
		t.Fatalf("parent = %#v, want %s %q", parent, kind, name)
	}
	got := make([]string, len(parent.Arguments))
	for index, argument := range parent.Arguments {
		got[index] = argument.String()
	}
	if !reflect.DeepEqual(got, arguments) {
		t.Fatalf("parent arguments = %q, want %q", got, arguments)
	}
}
