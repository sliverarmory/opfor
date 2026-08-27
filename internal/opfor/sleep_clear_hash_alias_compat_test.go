package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepClearHashAliasProbe = `%h = %(a => "x", b => "y");
$alias = %h;
clear(%h);
println("h-size=" . size(%h));
println("alias-size=" . size($alias));
$alias["c"] = "z";
println("h-c=[" . %h["c"] . "]");
println("alias-c=[" . $alias["c"] . "]");
%outer = %(slot => %(a => 1));
$indexed_alias = %outer["slot"];
clear(%outer["slot"]);
println("indexed-size=" . size(%outer["slot"]));
println("indexed-alias-size=" . size($indexed_alias));
@outer = @(%(a => 1));
$array_indexed_alias = @outer[0];
clear(@outer[0]);
println("array-indexed-size=" . size(@outer[0]));
println("array-indexed-alias-size=" . size($array_indexed_alias));
%adjacent_outer = %(slot => %(a => 1));
$adjacent_indexed_alias = %adjacent_outer["slot"];
clear(%adjacent_outer["slot"]());
println("adjacent-indexed-size=" . size(%adjacent_outer["slot"]));
println("adjacent-indexed-alias-size=" . size($adjacent_indexed_alias));
%adjacent_bare = %(a => 1);
$adjacent_bare_alias = %adjacent_bare;
clear(%adjacent_bare());
println("adjacent-bare-size=" . size(%adjacent_bare));
println("adjacent-bare-alias-size=" . size($adjacent_bare_alias));
`

const sleepClearHashAliasOutput = `h-size=0
alias-size=2
h-c=[]
alias-c=[z]
indexed-size=0
indexed-alias-size=1
array-indexed-size=0
array-indexed-alias-size=1
adjacent-indexed-size=0
adjacent-indexed-alias-size=1
adjacent-bare-size=0
adjacent-bare-alias-size=1
`

func TestSleepClearHashReplacesScalarAndPreservesAliases(t *testing.T) {
	if got := runSleepClearHashAliasProbe(t); got != sleepClearHashAliasOutput {
		t.Fatalf("clear hash alias output mismatch\nwant:\n%sgot:\n%s", sleepClearHashAliasOutput, got)
	}
}

func TestSleepClearHashAliasOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "clear-hash-alias.sl")
	if err := os.WriteFile(path, []byte(sleepClearHashAliasProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep clear hash alias probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepClearHashAliasProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep clear hash alias output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepClearHashAliasProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtimeInstance.Close(context.Background()); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	if _, err := runtimeInstance.Eval(context.Background(), "clear-hash-alias.sl", sleepClearHashAliasProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
