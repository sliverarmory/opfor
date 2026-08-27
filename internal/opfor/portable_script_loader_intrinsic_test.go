package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const portableScriptLoaderIntrinsicProbe = `import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "x", 'return lambda({ return 7; });', $null];
$value = [$child runScript];
println("invoke=" . invoke($value));
`

const portableScriptLoaderIntrinsicOutput = "invoke=7\n"

func TestPortableScriptLoaderDefaultsDoNotOverrideEvaluatorIntrinsics(t *testing.T) {
	if got := runPortableScriptLoaderIntrinsicProbe(t); !bytes.Equal(got, []byte(portableScriptLoaderIntrinsicOutput)) {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", portableScriptLoaderIntrinsicOutput, got)
	}
}

func TestPortableScriptLoaderIntrinsicOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, "loader-intrinsic.sl")
	if err := os.WriteFile(path, []byte(portableScriptLoaderIntrinsicProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep ScriptLoader intrinsic probe: %v\n%s", err, want)
	}
	if got := runPortableScriptLoaderIntrinsicProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep ScriptLoader intrinsic output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runPortableScriptLoaderIntrinsicProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "loader-intrinsic.sl", portableScriptLoaderIntrinsicProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
