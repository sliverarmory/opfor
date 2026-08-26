package opfor

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/sliverarmory/opfor/internal/envspec"
)

func TestBuiltInEnvironmentSpecificationsDriveRuntimeBindings(t *testing.T) {
	tests := []struct {
		keyword   string
		kind      BindingKind
		lifetime  BindingLifetime
		recompose bool
	}{
		{keyword: "sub", kind: BindingSub},
		{keyword: "inline", kind: BindingInline},
		{keyword: "on", kind: BindingEvent},
		{keyword: "when", kind: BindingEvent, lifetime: BindingOnce},
		{keyword: "command", kind: BindingCommand},
		{keyword: "alias", kind: BindingAlias},
		{keyword: "ssh_alias", kind: BindingSSHAlias},
		{keyword: "set", kind: BindingHook},
		{keyword: "hook", kind: BindingHook},
		{keyword: "popup", kind: BindingPopup, recompose: true},
		{keyword: "menu", kind: BindingMenu, recompose: true},
		{keyword: "menubar", kind: BindingMenu, recompose: true},
		{keyword: "item", kind: BindingItem},
		{keyword: "bind", kind: BindingKey},
		{keyword: "filter", kind: BindingKind("filter")},
	}

	var source strings.Builder
	for _, test := range tests {
		name := "spec_" + test.keyword
		source.WriteString(test.keyword)
		source.WriteByte(' ')
		source.WriteString(name)
		source.WriteString(" { return; }\n")
	}
	runtimeInstance, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "environment-specs.cna", source.String()); err != nil {
		t.Fatal(err)
	}

	for _, test := range tests {
		t.Run(test.keyword, func(t *testing.T) {
			if got := bindingKind(test.keyword); got != test.kind {
				t.Fatalf("bindingKind(%q) = %q, want %q", test.keyword, got, test.kind)
			}
			if got := bindingKind(strings.ToUpper(test.keyword)); got != test.kind {
				t.Fatalf("mixed-case bindingKind(%q) = %q, want %q", test.keyword, got, test.kind)
			}
			if !knownBindingEnvironment(test.keyword) || !knownBindingEnvironment(strings.ToUpper(test.keyword)) {
				t.Fatalf("%q was not recognized as a built-in environment", test.keyword)
			}
			if got := isCompositionBinding(test.kind); got != test.recompose {
				t.Fatalf("isCompositionBinding(%q) = %v, want %v", test.kind, got, test.recompose)
			}

			bindings := runtimeInstance.Bindings(test.kind, "spec_"+test.keyword)
			if len(bindings) != 1 {
				t.Fatalf("bindings = %#v, want one", bindings)
			}
			binding := bindings[0]
			if binding.Keyword != test.keyword || binding.Kind != test.kind || binding.Lifetime != test.lifetime ||
				binding.Environment != EnvironmentOrdinary {
				t.Fatalf("binding = %#v", binding)
			}
		})
	}

	if knownBindingEnvironment("report") {
		t.Fatal("report became a registered built-in environment")
	}
	if got := bindingKind("report"); got != BindingKind("report") {
		t.Fatalf("bindingKind(report) = %q, want report", got)
	}
	if isCompositionBinding(BindingKind("Menu")) {
		t.Fatal("programmatic mixed-case Menu unexpectedly gained composition semantics")
	}
}

func TestBuiltInEnvironmentSpecificationsMaterializePortableBridges(t *testing.T) {
	runtimeInstance, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	table := newPortableJavaMap("Hashtable", nil)
	loader := &portableScriptLoader{runtime: runtimeInstance}
	shared := portableSharedEnvironment(table)
	shared.installGlobalBridges(loader, runtimeInstance)
	for _, spec := range envspec.Builtins() {
		value, err := portableScriptEnvironmentTypedEntry(table, String(spec.Keyword), "sleep.interfaces.Environment")
		if err != nil || !portableScriptEnvironmentValueImplements(value, "sleep.interfaces.Environment") {
			t.Errorf("environment bridge %q = (%s, %v), want sleep.interfaces.Environment", spec.Keyword, value.Describe(), err)
		}
	}
}
