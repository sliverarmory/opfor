package opfor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const sleepPostfixMutationProbeName = "sleep-postfix-mutation-probe.sl"

const sleepPostfixMutationProbe = `$value = 1;
println("inc=" . $value++);
println("after-inc=" . $value);
println("dec=" . $value--);
println("after-dec=" . $value);
$value = 10;
@values = @(20);
%values = %(key => 30);
println("space=[" . eval('$value ++;') . "],value=" . $value);
println("group=[" . eval('($value)++;') . "],value=" . $value);
println("array=[" . eval('@values[0]++;') . "],value=" . @values[0]);
println("hash=[" . eval('%values["key"]--;') . "],value=" . %values["key"]);
println("tail");
`

const sleepPostfixMutationProbeOutput = `inc=2
after-inc=2
dec=1
after-dec=1
space=[],value=10
group=[],value=10
array=[],value=20
hash=[],value=30
tail
`

func TestSleepPostfixMutationCompatibility(t *testing.T) {
	if got := runSleepPostfixMutationProbe(t); got != sleepPostfixMutationProbeOutput {
		t.Fatalf("postfix-mutation output mismatch\nwant:\n%sgot:\n%s", sleepPostfixMutationProbeOutput, got)
	}
}

func TestSleepInvalidPostfixMutationCompileDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "whitespace-increment", source: `$value ++;`},
		{name: "whitespace-decrement", source: `$value --;`},
		{name: "grouped-increment", source: `($value)++;`},
		{name: "grouped-decrement", source: `($value)--;`},
		{name: "array-index", source: `@values[0]++;`},
		{name: "hash-index", source: `%values["key"]--;`},
		{name: "scalar-index", source: `$values["key"]++;`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileString(test.name+".sl", test.source)
			var compileError *CompileError
			if !errors.As(err, &compileError) {
				t.Fatalf("CompileString(%q) error = %v, want *CompileError", test.source, err)
			}
			if got, want := len(compileError.Diagnostics), 1; got != want {
				t.Fatalf("CompileString(%q) diagnostics = %+v, want %d", test.source, compileError.Diagnostics, want)
			}
			diagnostic := compileError.Diagnostics[0]
			if diagnostic.Severity != SeverityError || diagnostic.Code != "PAR005" ||
				diagnostic.Message != "Syntax error" || diagnostic.Span.Start.Offset != 0 ||
				diagnostic.Span.End.Offset != len(test.source)-1 {
				t.Fatalf("CompileString(%q) diagnostic = %+v, want one full-expression PAR005 Syntax error", test.source, diagnostic)
			}
		})
	}
}

func TestSleepPrefixMutationRemainsInvalid(t *testing.T) {
	for _, source := range []string{`++$value;`, `--$value;`} {
		if _, err := CompileString("prefix-mutation.sl", source); err == nil {
			t.Errorf("CompileString(%q) unexpectedly accepted prefix mutation", source)
		}
	}
}

func TestSleepPostfixMutationOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	path := filepath.Join(t.TempDir(), sleepPostfixMutationProbeName)
	if err := os.WriteFile(path, []byte(sleepPostfixMutationProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep postfix-mutation probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepPostfixMutationProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep postfix-mutation output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepPostfixMutationProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepPostfixMutationProbeName, sleepPostfixMutationProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
