package opfor

import (
	"context"
	"testing"
)

func TestRuntimeBindingsWithoutNamePreservesCrossNameRegistrationOrder(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	firstProgram, err := CompileString("binding-order-first.cna", `
on zebra { return "zebra-1"; }
on alpha { return "alpha"; }
on zebra { return "zebra-2"; }
`)
	if err != nil {
		t.Fatal(err)
	}
	secondProgram, err := CompileString("binding-order-second.cna", `
on middle { return "middle"; }
on beta { return "beta"; }
`)
	if err != nil {
		t.Fatal(err)
	}
	first, err := runtimeInstance.Load(context.Background(), firstProgram)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtimeInstance.Load(context.Background(), secondProgram)
	if err != nil {
		t.Fatal(err)
	}

	assertBindingSequence(t, runtimeInstance.Bindings(BindingEvent, ""), []bindingIdentity{
		{script: first.ID(), id: 1, name: "zebra"},
		{script: first.ID(), id: 2, name: "alpha"},
		{script: first.ID(), id: 3, name: "zebra"},
		{script: second.ID(), id: 1, name: "middle"},
		{script: second.ID(), id: 2, name: "beta"},
	})
	assertBindingSequence(t, runtimeInstance.Bindings(BindingEvent, "zebra"), []bindingIdentity{
		{script: first.ID(), id: 1, name: "zebra"},
		{script: first.ID(), id: 3, name: "zebra"},
	})

	// Binding IDs are script-local, so unloading the first script must remove
	// only its registrations from both indexes even though the second script
	// reused IDs 1 and 2 under different names.
	if err := first.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertBindingSequence(t, runtimeInstance.Bindings(BindingEvent, ""), []bindingIdentity{
		{script: second.ID(), id: 1, name: "middle"},
		{script: second.ID(), id: 2, name: "beta"},
	})
	assertBindingSequence(t, runtimeInstance.Bindings(BindingEvent, "middle"), []bindingIdentity{
		{script: second.ID(), id: 1, name: "middle"},
	})
	if got := runtimeInstance.Bindings(BindingEvent, "zebra"); len(got) != 0 {
		t.Fatalf("zebra bindings after first unload = %#v, want none", got)
	}

	if err := second.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := runtimeInstance.Bindings(BindingEvent, ""); len(got) != 0 {
		t.Fatalf("bindings after both unloads = %#v, want none", got)
	}
}

type bindingIdentity struct {
	script ScriptID
	id     uint64
	name   string
}

func assertBindingSequence(t *testing.T, bindings []Binding, want []bindingIdentity) {
	t.Helper()
	if len(bindings) != len(want) {
		t.Fatalf("binding count = %d, want %d: %#v", len(bindings), len(want), bindings)
	}
	for index, binding := range bindings {
		got := bindingIdentity{script: binding.Script, id: binding.ID, name: binding.Name}
		if got != want[index] {
			t.Fatalf("binding[%d] = %#v, want %#v (all bindings: %#v)", index, got, want[index], bindings)
		}
	}
}
