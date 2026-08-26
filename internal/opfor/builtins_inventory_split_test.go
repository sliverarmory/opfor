package opfor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestDefaultFunctionInventorySnapshot(t *testing.T) {
	names := DefaultFunctionNames()
	digest := sha256.Sum256([]byte(strings.Join(names, "\x00")))
	if got, want := len(names), 602; got != want {
		t.Fatalf("default native function count = %d, want %d\nnames=%q", got, want, names)
	}
	if got, want := fmt.Sprintf("%x", digest), "d38c1641eb3074fa62e4424c359418cf254bf9d0e4499eee9e521343ed4cefec"; got != want {
		t.Fatalf("default native function inventory SHA-256 = %s, want %s\nnames=%q", got, want, names)
	}
}

func TestSleepAndAggressorFunctionInventoriesAreDisjointAndComplete(t *testing.T) {
	runtimeInstance := &Runtime{clock: systemClock{}}
	ioFunctions := ioFunctionsForState(runtimeInstance, &ioBuiltinState{runtime: runtimeInstance})
	sleep := runtimeInstance.sleepFunctions(ioFunctions)
	aggressor := runtimeInstance.aggressorFunctions()

	for name := range sleep {
		if _, duplicate := aggressor[name]; duplicate {
			t.Errorf("native function %q appears in both Sleep and Aggressor inventories", name)
		}
	}
	for name := range evidenceGatedExtensionFunctionNames {
		if _, present := sleep[name]; present {
			t.Errorf("Sleep inventory unexpectedly installs evidence-gated function %q", name)
		}
		if _, present := aggressor[name]; present {
			t.Errorf("Aggressor inventory unexpectedly installs evidence-gated function %q", name)
		}
	}

	wantRuntimeSleep := []string{"checkError", "debug", "exit", "profile", "use", "watch"}
	if got := sortedFunctionNames(runtimeInstance.sleepRuntimeFunctions()); !slices.Equal(got, wantRuntimeSleep) {
		t.Fatalf("Sleep runtime functions = %q, want %q", got, wantRuntimeSleep)
	}
	for _, name := range wantRuntimeSleep {
		if _, present := sleep[name]; !present {
			t.Errorf("Sleep inventory is missing runtime function %q", name)
		}
		if _, present := aggressor[name]; present {
			t.Errorf("Aggressor inventory unexpectedly claims Sleep runtime function %q", name)
		}
	}

	wantAggressorTranches := map[string][]string{
		"binary":   {"base64_decode", "base64_encode"},
		"sequence": {"range"},
		"time":     {"dstamp", "tstamp"},
	}
	gotAggressorTranches := map[string]map[string]NativeFunc{
		"binary":   aggressorBinaryFunctions(),
		"sequence": aggressorSequenceFunctions(),
		"time":     runtimeInstance.aggressorTimeFunctions(),
	}
	for tranche, want := range wantAggressorTranches {
		got := sortedFunctionNames(gotAggressorTranches[tranche])
		if !slices.Equal(got, want) {
			t.Errorf("Aggressor %s functions = %q, want %q", tranche, got, want)
		}
		for _, name := range want {
			if _, present := aggressor[name]; !present {
				t.Errorf("Aggressor inventory is missing %s function %q", tranche, name)
			}
			if _, present := sleep[name]; present {
				t.Errorf("Sleep inventory unexpectedly claims Aggressor %s function %q", tranche, name)
			}
		}
	}

	core := runtimeInstance.coreFunctions(ioFunctions)
	wantCore := make(map[string]struct{}, len(sleep)+len(aggressor))
	for name := range sleep {
		wantCore[name] = struct{}{}
	}
	for name := range aggressor {
		wantCore[name] = struct{}{}
	}
	if got, want := sortedFunctionNames(core), sortedInventoryNames(wantCore); !slices.Equal(got, want) {
		t.Fatalf("combined core inventory diverged from the Sleep/Aggressor union\n got=%q\nwant=%q", got, want)
	}
}

func TestFunctionInventoryCompositionPreservesIntentionalResolution(t *testing.T) {
	runtimeInstance := &Runtime{clock: systemClock{}}
	ioFunctions := ioFunctionsForState(runtimeInstance, &ioBuiltinState{runtime: runtimeInstance})
	sleep := runtimeInstance.sleepFunctions(ioFunctions)
	if got, want := nativeFunctionPointer(sleep["reverse"]), nativeFunctionPointer(builtinSequenceReverse); got != want {
		t.Fatalf("Sleep reverse implementation pointer = %#x, want sequence implementation %#x", got, want)
	}
	if got, displaced := nativeFunctionPointer(sleep["reverse"]), nativeFunctionPointer(builtinSleepReverse); got == displaced {
		t.Fatalf("Sleep reverse unexpectedly resolved to the displaced string/number implementation %#x", got)
	}

	aggressor := runtimeInstance.aggressorFunctions()
	if got, want := nativeFunctionPointer(aggressor["beacon_inline_execute"]), nativeFunctionPointer(runtimeInstance.beaconInlineExecute); got != want {
		t.Fatalf("beacon_inline_execute implementation pointer = %#x, want specialized wrapper %#x", got, want)
	}
	if got, generic := nativeFunctionPointer(aggressor["beacon_inline_execute"]), nativeFunctionPointer(runtimeInstance.aggressorBeaconExecution); got == generic {
		t.Fatalf("beacon_inline_execute unexpectedly resolved to generic wrapper %#x", got)
	}
}

func TestInstalledStockAndPublicFunctionsShareStatefulInventory(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	runtimeInstance.mu.RLock()
	stockSrand := runtimeInstance.stockFunctions["srand"]
	stockRand := runtimeInstance.stockFunctions["rand"]
	publicSrand := runtimeInstance.functions["srand"]
	publicRand := runtimeInstance.functions["rand"]
	runtimeInstance.mu.RUnlock()
	for name, function := range map[string]NativeFunc{
		"stock srand":  stockSrand,
		"stock rand":   stockRand,
		"public srand": publicSrand,
		"public rand":  publicRand,
	} {
		if function == nil {
			t.Fatalf("%s is nil", name)
		}
	}

	const scriptID = ScriptID(0x7ffffffe)
	const seed = int64(0x12345678)
	if _, err := stockSrand(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  scriptID,
		Name:    "srand",
		Arguments: []Argument{
			{Value: Long(seed)},
		},
	}); err != nil {
		t.Fatalf("stock srand: %v", err)
	}
	wantRandom := newSleepJavaRandom(seed)
	wantFirst, _ := wantRandom.nextInt(1_000_000)
	wantSecond, _ := wantRandom.nextInt(1_000_000)

	first, err := publicRand(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  scriptID,
		Name:    "rand",
		Arguments: []Argument{
			{Value: Int(1_000_000)},
		},
	})
	if err != nil || first.Int32() != wantFirst {
		t.Fatalf("public rand after stock srand = (%s, %v), want %d", first.Describe(), err, wantFirst)
	}
	second, err := stockRand(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  scriptID,
		Name:    "rand",
		Arguments: []Argument{
			{Value: Int(1_000_000)},
		},
	})
	if err != nil || second.Int32() != wantSecond {
		t.Fatalf("stock rand after public rand = (%s, %v), want %d", second.Describe(), err, wantSecond)
	}

	if _, err := publicSrand(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  scriptID,
		Name:    "srand",
		Arguments: []Argument{
			{Value: Long(seed + 1)},
		},
	}); err != nil {
		t.Fatalf("public srand: %v", err)
	}
	wantReset := newSleepJavaRandom(seed + 1)
	wantAfterReset, _ := wantReset.nextInt(1_000_000)
	afterReset, err := stockRand(context.Background(), Invocation{
		Runtime: runtimeInstance,
		Script:  scriptID,
		Name:    "rand",
		Arguments: []Argument{
			{Value: Int(1_000_000)},
		},
	})
	if err != nil || afterReset.Int32() != wantAfterReset {
		t.Fatalf("stock rand after public srand = (%s, %v), want %d", afterReset.Describe(), err, wantAfterReset)
	}
}

func sortedFunctionNames(functions map[string]NativeFunc) []string {
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedInventoryNames(inventory map[string]struct{}) []string {
	names := make([]string, 0, len(inventory))
	for name := range inventory {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// binaryFunctions and sequenceFunctions preserve the focused tranche-level
// unit-test helpers without reintroducing mixed production inventories.
func (*Runtime) binaryFunctions() map[string]NativeFunc {
	functions := sleepBinaryFunctions()
	mergeDisjointFunctionInventory(functions, aggressorBinaryFunctions())
	return functions
}

func (r *Runtime) sequenceFunctions() map[string]NativeFunc {
	functions := r.sleepSequenceFunctions()
	mergeDisjointFunctionInventory(functions, aggressorSequenceFunctions())
	return functions
}
