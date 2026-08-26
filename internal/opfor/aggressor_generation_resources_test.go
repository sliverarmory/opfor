package opfor

import (
	"context"
	"reflect"
	"testing"
)

type generationTechniqueCallable string

func (callable generationTechniqueCallable) Invoke(context.Context, ...Value) (Value, error) {
	return String(string(callable)), nil
}

func TestAggressorCommandGenerationRemovalPreservesLaterGeneration(t *testing.T) {
	state := newAggressorCommandState(map[AggressorCommandKind]AggressorCommandCatalog{
		AggressorCommandBeacon: {
			Groups: []AggressorCommandGroup{{ID: "shared", Name: "base group"}},
			Commands: []AggressorCommandMetadata{{
				Name: "shared", Description: "base command", GroupID: "shared",
			}},
		},
	})
	const owner ScriptID = 7
	oldGeneration := &scriptGeneration{}
	newGeneration := &scriptGeneration{}

	state.registerGroup(AggressorCommandBeacon, owner, oldGeneration,
		AggressorCommandGroup{ID: "shared", Name: "old group"})
	state.registerCommand(AggressorCommandBeacon, owner, oldGeneration,
		AggressorCommandMetadata{Name: "shared", Description: "old command", GroupID: "shared"})
	// A repeated registration coalesces only within its exact generation.
	state.registerCommand(AggressorCommandBeacon, owner, oldGeneration,
		AggressorCommandMetadata{Name: "shared", Description: "old replacement", GroupID: "shared"})
	state.registerGroup(AggressorCommandBeacon, owner, newGeneration,
		AggressorCommandGroup{ID: "shared", Name: "new group"})
	state.registerCommand(AggressorCommandBeacon, owner, newGeneration,
		AggressorCommandMetadata{Name: "shared", Description: "new command", GroupID: "shared"})

	state.mu.RLock()
	namespace := state.namespaces[AggressorCommandBeacon]
	commandLayers := len(namespace.commands["shared"])
	groupLayers := len(namespace.groups["shared"])
	state.mu.RUnlock()
	if commandLayers != 3 || groupLayers != 3 {
		t.Fatalf("layers before retirement = commands %d, groups %d; want base + two generations", commandLayers, groupLayers)
	}

	state.removeGeneration(owner, oldGeneration)
	snapshot := state.snapshot(AggressorCommandBeacon)
	if got, want := snapshot.Commands, []AggressorCommandMetadata{{
		Name: "shared", Description: "new command", GroupID: "shared",
	}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commands after old retirement = %#v, want %#v", got, want)
	}
	if got, want := snapshot.Groups, []AggressorCommandGroup{{ID: "shared", Name: "new group"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("groups after old retirement = %#v, want %#v", got, want)
	}

	state.removeGeneration(owner, newGeneration)
	snapshot = state.snapshot(AggressorCommandBeacon)
	if got := snapshot.Commands[0].Description; got != "base command" {
		t.Fatalf("command after all script generations retire = %q, want base command", got)
	}
	if got := snapshot.Groups[0].Name; got != "base group" {
		t.Fatalf("group after all script generations retire = %q, want base group", got)
	}
}

func TestAggressorBeaconTechniqueGenerationRemovalPreservesLaterCallback(t *testing.T) {
	state := newAggressorBeaconTechniqueState(map[AggressorBeaconTechniqueKind]AggressorBeaconTechniqueCatalog{
		AggressorBeaconTechniqueElevator: {Techniques: []AggressorBeaconTechniqueMetadata{{
			Name: "shared", Description: "base",
		}}},
	})
	const owner ScriptID = 11
	oldGeneration := &scriptGeneration{}
	newGeneration := &scriptGeneration{}
	state.register(AggressorBeaconTechniqueElevator, owner, oldGeneration,
		AggressorBeaconTechniqueMetadata{Name: "shared", Description: "old"},
		generationTechniqueCallable("old"))
	state.register(AggressorBeaconTechniqueElevator, owner, oldGeneration,
		AggressorBeaconTechniqueMetadata{Name: "shared", Description: "old replacement"},
		generationTechniqueCallable("old replacement"))
	state.register(AggressorBeaconTechniqueElevator, owner, newGeneration,
		AggressorBeaconTechniqueMetadata{Name: "shared", Description: "new"},
		generationTechniqueCallable("new"))

	state.mu.RLock()
	layers := len(state.namespaces[AggressorBeaconTechniqueElevator].techniques["shared"])
	state.mu.RUnlock()
	if layers != 3 {
		t.Fatalf("layers before retirement = %d, want base + two generations", layers)
	}

	state.removeGeneration(owner, oldGeneration)
	metadata, exists := state.describe(AggressorBeaconTechniqueElevator, "shared")
	if !exists || metadata.Description != "new" {
		t.Fatalf("metadata after old retirement = (%#v, %v), want new", metadata, exists)
	}
	callback, exists := state.callback(AggressorBeaconTechniqueElevator, "shared")
	if !exists || callback == nil {
		t.Fatal("new callback disappeared with old generation")
	}
	value, err := callback.Invoke(context.Background())
	if err != nil || value.String() != "new" {
		t.Fatalf("callback after old retirement = (%q, %v), want new", value.String(), err)
	}

	state.removeGeneration(owner, newGeneration)
	metadata, exists = state.describe(AggressorBeaconTechniqueElevator, "shared")
	if !exists || metadata.Description != "base" {
		t.Fatalf("metadata after all script generations retire = (%#v, %v), want base", metadata, exists)
	}
	if callback, exists = state.callback(AggressorBeaconTechniqueElevator, "shared"); !exists || callback != nil {
		t.Fatalf("base callback after retirement = (%#v, %v), want nil metadata-only entry", callback, exists)
	}
}

func TestTakeAggressorUIResourcesForGenerationPreservesLaterGeneration(t *testing.T) {
	owner := &Script{aggressorUIResources: make(map[aggressorUIResource]struct{})}
	oldGeneration := &scriptGeneration{}
	newGeneration := &scriptGeneration{}
	oldDialog := &aggressorDialog{
		owner: owner, generation: oldGeneration, state: aggressorUIBuilding, done: make(chan struct{}),
	}
	newPrompt := &aggressorPrompt{
		owner: owner, generation: newGeneration, state: aggressorUIPresenting, done: make(chan struct{}),
	}
	owner.aggressorUIResources[oldDialog] = struct{}{}
	owner.aggressorUIResources[newPrompt] = struct{}{}

	owner.mu.Lock()
	resources := takeAggressorUIResourcesForGenerationLocked(owner, oldGeneration)
	owner.mu.Unlock()
	if len(resources) != 1 || resources[0] != oldDialog {
		t.Fatalf("taken resources = %#v, want only old dialog", resources)
	}
	if _, exists := owner.aggressorUIResources[newPrompt]; !exists || len(owner.aggressorUIResources) != 1 {
		t.Fatalf("later-generation resource was removed: %#v", owner.aggressorUIResources)
	}

	revokeAggressorUIResources(resources)
	if oldDialog.state != aggressorUIRevoked || oldDialog.owner != nil {
		t.Fatalf("old dialog after revoke = state %v, owner %p", oldDialog.state, oldDialog.owner)
	}
	if newPrompt.state != aggressorUIPresenting || newPrompt.owner != owner {
		t.Fatalf("later prompt changed = state %v, owner %p", newPrompt.state, newPrompt.owner)
	}
}

func TestAggressorPopupComposerRetainsInvocationGenerationAndRawValues(t *testing.T) {
	generation := &scriptGeneration{}
	array := NewArray(String("one"))
	raw := ArrayValue(array)
	arguments := []Value{raw}
	composer := newAggressorPopupComposer(nil, Invocation{
		Script:     19,
		generation: generation,
	}, nil, arguments, nil)
	arguments[0] = String("replaced")

	if composer.creator != 19 || composer.generation != generation {
		t.Fatalf("composer owner = (%d, %p), want (19, %p)", composer.creator, composer.generation, generation)
	}
	captured, ok := composer.arguments[0].Array()
	if !ok || captured != array {
		t.Fatalf("captured argument = %#v, want original shared array %p", composer.arguments[0], array)
	}
}
