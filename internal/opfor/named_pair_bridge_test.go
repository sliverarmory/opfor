package opfor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const namedPairStockBridgeProbe = `sub function_pair {
    println("function-before");
    function(name => "&find");
    println("function-after");
}
function_pair();
println("function-resume");
sub local_pair {
    println("local-before");
    local(name => "$x");
    println("local-after");
}
local_pair();
println("local-resume");
println("find=" . find("abcabc", "a", start => 3));
println("matches=" . matches("a1a2", "a(.)", first => 1));
`

const namedPairStockBridgeOutput = `function-before
Warning: &function: requested function name must begin with '&' at named-pair-stock-bridge.sl:3
function-resume
local-before
Warning: &local: malformed variable name 'name=' from 'name=' at named-pair-stock-bridge.sl:10
local-resume
find=0
matches=@('1')
`

func TestNamedPairsRemainRawAtStockBridgeBoundaries(t *testing.T) {
	if got := runNamedPairStockBridgeProbe(t); !bytes.Equal(got, []byte(namedPairStockBridgeOutput)) {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", namedPairStockBridgeOutput, got)
	}
}

func TestNamedPairStockBridgesOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, "named-pair-stock-bridge.sl")
	if err := os.WriteFile(path, []byte(namedPairStockBridgeProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep named-pair bridge probe: %v\n%s", err, want)
	}
	if got := runNamedPairStockBridgeProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep named-pair bridge output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestNamedPairImporterArgumentsRetainPublicReferences(t *testing.T) {
	var captured Invocation
	runtimeInstance, err := New(WithFunction("capture_pairs", func(_ context.Context, invocation Invocation) (Value, error) {
		captured = invocation
		if len(invocation.Arguments) != 2 {
			return Null(), fmt.Errorf("capture_pairs received %d arguments", len(invocation.Arguments))
		}
		if !invocation.Arguments[0].Set(String("pair-mutated")) || !invocation.Arguments[1].Set(String("reference-mutated")) {
			return Null(), fmt.Errorf("capture_pairs lost a caller reference")
		}
		return Null(), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Eval(context.Background(), "named-pair-importer.sl", `
$left = "left";
$right = "right";
capture_pairs(label => $left, \$right);
return @($left, $right);
`)
	if err != nil {
		t.Fatal(err)
	}
	values, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	got := values.Values()
	if len(got) != 2 || got[0].String() != "pair-mutated" || got[1].String() != "reference-mutated" {
		t.Fatalf("mutated caller values = %s, want pair-mutated/reference-mutated", result.Describe())
	}
	if len(captured.Arguments) != 2 ||
		captured.Arguments[0].Name != "label" || captured.Arguments[0].Reference == nil ||
		captured.Arguments[1].Name != "$right" || captured.Arguments[1].Reference == nil {
		t.Fatalf("importer arguments lost public Name/Reference fields: %#v", captured.Arguments)
	}
	if captured.Arguments[0].syntax != argumentSyntaxPair || captured.Arguments[1].syntax != argumentSyntaxReference {
		t.Fatalf("private argument origins = %d/%d, want pair/reference", captured.Arguments[0].syntax, captured.Arguments[1].syntax)
	}
}

func runNamedPairStockBridgeProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "named-pair-stock-bridge.sl", namedPairStockBridgeProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
