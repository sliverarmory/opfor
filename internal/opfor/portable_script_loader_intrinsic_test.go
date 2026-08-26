package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
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
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for ScriptLoader intrinsic verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	directory := t.TempDir()
	path := filepath.Join(directory, "loader-intrinsic.sl")
	if err := os.WriteFile(path, []byte(portableScriptLoaderIntrinsicProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
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
