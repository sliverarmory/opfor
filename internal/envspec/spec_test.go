package envspec

import (
	"reflect"
	"testing"
)

func TestBuiltinsExactCompatibilityMatrix(t *testing.T) {
	want := []Spec{
		{Keyword: "sub", LexicalKeyword: true, Form: Ordinary, Binding: BindingSub, Closure: ClosureRoot, Lifetime: Persistent},
		{Keyword: "inline", LexicalKeyword: true, Form: Ordinary, Binding: BindingInline, Closure: ClosureInline, Lifetime: Persistent},
		{Keyword: "on", LexicalKeyword: true, Form: Ordinary, Binding: BindingEvent, Closure: ClosureCurrent, Lifetime: Persistent},
		{Keyword: "when", LexicalKeyword: true, Form: Ordinary, Binding: BindingEvent, Closure: ClosureCurrent, Lifetime: Once},
		{Keyword: "command", LexicalKeyword: true, Form: Ordinary, Binding: BindingCommand, Closure: ClosureCurrent, Lifetime: Persistent},
		{Keyword: "alias", LexicalKeyword: true, Form: Ordinary, Binding: BindingAlias, Closure: ClosureCurrent, Lifetime: Persistent},
		{Keyword: "ssh_alias", LexicalKeyword: true, Form: Ordinary, Binding: BindingSSHAlias, Closure: ClosureCurrent, Lifetime: Persistent},
		{Keyword: "set", LexicalKeyword: true, Form: Ordinary, Binding: BindingHook, Closure: ClosureCurrent, Lifetime: Persistent},
		{Keyword: "hook", LexicalKeyword: true, Form: Ordinary, Binding: BindingHook, Closure: ClosureCurrent, Lifetime: Persistent},
		{Keyword: "popup", LexicalKeyword: true, Form: Ordinary, Binding: BindingPopup, Closure: ClosureCurrent, Lifetime: Persistent, RecomposeDescendants: true},
		{Keyword: "menu", LexicalKeyword: true, Form: Ordinary, Binding: BindingMenu, Closure: ClosureCurrent, Lifetime: Persistent, RecomposeDescendants: true},
		{Keyword: "menubar", LexicalKeyword: true, Form: Ordinary, Binding: BindingMenu, Closure: ClosureCurrent, Lifetime: Persistent, RecomposeDescendants: true},
		{Keyword: "item", LexicalKeyword: true, Form: Ordinary, Binding: BindingItem, Closure: ClosureCurrent, Lifetime: Persistent},
		{Keyword: "bind", LexicalKeyword: false, Form: Ordinary, Binding: BindingKey, Closure: ClosureCurrent, Lifetime: Persistent},
		{Keyword: "filter", LexicalKeyword: true, Form: Ordinary, Binding: BindingFilter, Closure: ClosureCurrent, Lifetime: Persistent},
	}
	got := Builtins()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("built-in environment specifications = %#v, want %#v", got, want)
	}

	seen := make(map[string]struct{}, len(got))
	for _, spec := range got {
		if _, duplicate := seen[spec.Keyword]; duplicate {
			t.Fatalf("duplicate environment keyword %q", spec.Keyword)
		}
		seen[spec.Keyword] = struct{}{}
		if lookedUp, ok := Lookup(spec.Keyword); !ok || lookedUp != spec {
			t.Fatalf("Lookup(%q) = (%#v, %v), want %#v", spec.Keyword, lookedUp, ok, spec)
		}
	}
	if _, ok := Lookup("report"); ok {
		t.Fatal("report became a built-in environment; it must remain a generic host extension")
	}
	if _, ok := Lookup("On"); ok {
		t.Fatal("Lookup normalized mixed case; callers must retain their existing normalization rules")
	}
	if spec, ok := LookupFold("SUB"); !ok || spec.Keyword != "sub" {
		t.Fatalf("LookupFold(SUB) = (%#v, %v), want sub", spec, ok)
	}
	if spec, ok := LookupFold("ſub"); !ok || spec.Keyword != "sub" {
		t.Fatalf("LookupFold(ſub) = (%#v, %v), want Unicode-folded sub", spec, ok)
	}

	got[0].Keyword = "mutated"
	if spec, ok := Lookup("sub"); !ok || spec.Keyword != "sub" {
		t.Fatalf("mutating Builtins result changed the table: (%#v, %v)", spec, ok)
	}
}

func TestRecomposingBindingsAreDerivedFromSpecifications(t *testing.T) {
	for binding, want := range map[string]bool{
		BindingPopup: true,
		BindingMenu:  true,
		BindingItem:  false,
		"Menu":       false,
		"report":     false,
	} {
		if got := RecomposesDescendants(binding); got != want {
			t.Errorf("RecomposesDescendants(%q) = %v, want %v", binding, got, want)
		}
	}
}
