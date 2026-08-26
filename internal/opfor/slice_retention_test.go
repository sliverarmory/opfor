package opfor

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sliverarmory/opfor/internal/bytecode"
)

func TestActiveBindingRemovalClearsBackingArrayTails(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	firstProgram, err := CompileString("binding-retention-first.cna", `alias shared { return "first"; }`)
	if err != nil {
		t.Fatal(err)
	}
	first, err := runtimeInstance.Load(context.Background(), firstProgram)
	if err != nil {
		t.Fatal(err)
	}
	secondProgram, err := CompileString("binding-retention-second.cna", `alias shared { return "second"; }`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtimeInstance.Load(context.Background(), secondProgram)
	if err != nil {
		t.Fatal(err)
	}

	bindings := second.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("second script binding count = %d, want 1", len(bindings))
	}
	if !second.removeBindingIfPresent(bindings[0]) {
		t.Fatal("active binding removal reported the binding absent")
	}

	second.mu.RLock()
	scriptBindings := second.bindings
	second.mu.RUnlock()
	assertBindingBackingTailCleared(t, "Script.bindings", scriptBindings)

	runtimeInstance.mu.RLock()
	byName := runtimeInstance.bindings[BindingAlias]["shared"]
	ordered := runtimeInstance.bindingOrder[BindingAlias]
	runtimeInstance.mu.RUnlock()
	if len(byName) != 1 || byName[0].Script != first.ID() {
		t.Fatalf("runtime name index after removal = %#v, want first script binding", byName)
	}
	if len(ordered) != 1 || ordered[0].Script != first.ID() {
		t.Fatalf("runtime order index after removal = %#v, want first script binding", ordered)
	}
	assertBindingBackingTailCleared(t, "Runtime.bindings", byName)
	assertBindingBackingTailCleared(t, "Runtime.bindingOrder", ordered)
}

func TestPopupDescendantRemovalClearsScriptBindingTail(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("popup-retention.cna", `
popup root {
    menu "Tools" {
        item "Action" { return; }
    }
    item "Direct" { return; }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingPopup, "root"); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingMenu, "Tools"); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.InvokeBinding(context.Background(), BindingPopup, "root"); err != nil {
		t.Fatal(err)
	}

	script.mu.RLock()
	bindings := script.bindings
	script.mu.RUnlock()
	if len(bindings) != 3 {
		t.Fatalf("active popup binding count = %d, want root, menu, and direct item", len(bindings))
	}
	assertBindingBackingTailCleared(t, "popup Script.bindings", bindings)
}

func TestRemoveWarningWatcherClearsBackingArrayTail(t *testing.T) {
	instance := &portableScriptInstance{}
	kept := &sliceRetentionCallable{name: "kept"}
	removed := &sliceRetentionCallable{name: "removed"}
	for _, watcher := range []Callable{kept, removed} {
		_, handled, err := instance.invoke(context.Background(), ObjectInvocation{
			Op:        ObjectInvoke,
			Message:   "addWarningWatcher",
			Arguments: []Argument{{Value: FunctionValue(watcher)}},
		})
		if err != nil || !handled {
			t.Fatalf("addWarningWatcher = handled %v, err %v", handled, err)
		}
	}
	_, handled, err := instance.invoke(context.Background(), ObjectInvocation{
		Op:        ObjectInvoke,
		Message:   "removeWarningWatcher",
		Arguments: []Argument{{Value: FunctionValue(removed)}},
	})
	if err != nil || !handled {
		t.Fatalf("removeWarningWatcher = handled %v, err %v", handled, err)
	}

	instance.mu.Lock()
	watchers := instance.watchers
	instance.mu.Unlock()
	if len(watchers) != 1 || watchers[0] != kept {
		t.Fatalf("watchers after removal = %#v, want only kept watcher", watchers)
	}
	if len(watchers) == cap(watchers) {
		t.Fatal("watcher test setup has no hidden backing-array tail")
	}
	for index, watcher := range watchers[len(watchers):cap(watchers)] {
		if watcher != nil {
			t.Fatalf("watcher backing tail[%d] = %#v, want nil", index, watcher)
		}
	}
}

func TestArrayCacheRefreshClearsBackingArrayTail(t *testing.T) {
	array := NewArray(String("kept"), FunctionValue(&sliceRetentionCallable{name: "removed"}))
	if err := removeArrayAt(array, 1); err != nil {
		t.Fatal(err)
	}

	storage, window := array.arrayStorage()
	storage.mu.RLock()
	cached := window.cached
	storage.mu.RUnlock()
	if len(cached) != 1 || cached[0].Get().String() != "kept" {
		t.Fatalf("array cache after removal = %#v, want only kept cell", cached)
	}
	if len(cached) == cap(cached) {
		t.Fatal("array cache test setup has no hidden backing-array tail")
	}
	for index, cell := range cached[len(cached):cap(cached)] {
		if cell != nil {
			t.Fatalf("array cache backing tail[%d] = %p, want nil", index, cell)
		}
	}
}

func TestFiberLocalPopClearsBackingArrayTail(t *testing.T) {
	root := newRootScope()
	middle := root.nextLocal()
	middle.local("$retained").Set(FunctionValue(&sliceRetentionCallable{name: "removed"}))
	current := middle.nextLocal()
	fiber := &fiber{scope: current, locals: []*scope{root, middle}}

	if !fiber.popLocal(nil) || !fiber.popLocal(nil) {
		t.Fatal("popLocal rejected a populated local stack")
	}
	if fiber.scope != root || len(fiber.locals) != 0 {
		t.Fatalf("fiber after local pops = scope %p, locals %d; want root %p and no locals", fiber.scope, len(fiber.locals), root)
	}
	if cap(fiber.locals) == 0 {
		t.Fatal("local stack test setup has no hidden backing-array tail")
	}
	for index, local := range fiber.locals[:cap(fiber.locals)] {
		if local != nil {
			t.Fatalf("local stack backing tail[%d] = %p, want nil", index, local)
		}
	}
}

func TestFiberIteratorRemovalClearsBackingArrayTails(t *testing.T) {
	kept := &sliceIterator{source: String("kept")}
	removed := &closureIterator{
		source:  FunctionValue(&sliceRetentionCallable{name: "removed"}),
		closure: &sliceRetentionCallable{name: "removed"},
	}

	t.Run("destroy", func(t *testing.T) {
		fiber := &fiber{iterators: []valueIterator{kept, removed}}
		if _, _, _, err := fiber.step(context.Background(), bytecode.Instruction{Op: bytecode.OpIterDestroy}); err != nil {
			t.Fatal(err)
		}
		assertIteratorBackingTailCleared(t, fiber.iterators, kept)
	})

	t.Run("catch", func(t *testing.T) {
		secondRemoved := &arrayIterator{source: FunctionValue(&sliceRetentionCallable{name: "second-removed"})}
		fiber := &fiber{
			iterators: []valueIterator{kept, removed, secondRemoved},
			tries:     []tryFrame{{handler: 7, depth: 1}},
		}
		if !fiber.catch(errors.New("caught")) {
			t.Fatal("catch rejected a populated try stack")
		}
		assertIteratorBackingTailCleared(t, fiber.iterators, kept)
	})
}

func assertBindingBackingTailCleared(t *testing.T, name string, bindings []Binding) {
	t.Helper()
	if len(bindings) == cap(bindings) {
		t.Fatalf("%s test setup has no hidden backing-array tail", name)
	}
	for index, binding := range bindings[len(bindings):cap(bindings)] {
		if !reflect.DeepEqual(binding, Binding{}) {
			t.Fatalf("%s backing tail[%d] = %#v, want zero Binding", name, index, binding)
		}
	}
}

func assertIteratorBackingTailCleared(t *testing.T, iterators []valueIterator, kept valueIterator) {
	t.Helper()
	if len(iterators) != 1 || iterators[0] != kept {
		t.Fatalf("iterator stack after removal = %#v, want only kept iterator", iterators)
	}
	if len(iterators) == cap(iterators) {
		t.Fatal("iterator test setup has no hidden backing-array tail")
	}
	for index, iterator := range iterators[len(iterators):cap(iterators)] {
		if iterator != nil {
			t.Fatalf("iterator backing tail[%d] = %#v, want nil", index, iterator)
		}
	}
}

type sliceRetentionCallable struct {
	name string
}

func (callable *sliceRetentionCallable) Invoke(context.Context, ...Value) (Value, error) {
	return String(callable.name), nil
}
