package opfor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const officialSleep21JARSHA256 = "0ddde5e9e8d8d8d334d071b1f887c379f5d0be9b190566f05365997b3e375ff1"

// officialSleepDifferentialTools resolves and authenticates the external tools
// used by tests that compare OPFOR with the official Sleep 2.1 implementation.
//
// These tests remain opt-in for normal, pure-Go development. Release and other
// trusted test environments set OPFOR_REQUIRE_SLEEP_JAR=1 to turn a missing JAR
// or Java runtime into a hard failure instead of a skip.
func officialSleepDifferentialTools(t *testing.T) (jar, java string) {
	t.Helper()

	jar = os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		officialSleepDifferentialUnavailable(t, "OPFOR_SLEEP_JAR is unset; set it to the official Sleep 2.1 JAR")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatalf("read official Sleep JAR %q: %v", jar, err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}

	javaName := os.Getenv("OPFOR_JAVA")
	if javaName == "" {
		javaName = "java"
	}
	java, err = osexec.LookPath(javaName)
	if err != nil {
		officialSleepDifferentialUnavailable(t, "official Sleep JAR supplied but Java %q is unavailable: %v", javaName, err)
	}
	return jar, java
}

func officialSleepJavaCompiler(t *testing.T, java string) string {
	t.Helper()
	javac := filepath.Join(filepath.Dir(java), "javac")
	if _, err := os.Stat(javac); err == nil {
		return javac
	}
	javac, err := osexec.LookPath("javac")
	if err != nil {
		officialSleepDifferentialUnavailable(t, "official Sleep differential requires javac: %v", err)
	}
	return javac
}

func officialSleepJavaCommand(java string, args ...string) *osexec.Cmd {
	return osexec.Command(java, officialSleepJavaArgs(args)...)
}

func officialSleepJavaCommandContext(ctx context.Context, java string, args ...string) *osexec.Cmd {
	return osexec.CommandContext(ctx, java, officialSleepJavaArgs(args)...)
}

func officialSleepJavaArgs(args []string) []string {
	for _, arg := range args {
		if len(arg) >= len("-Dfile.encoding=") && arg[:len("-Dfile.encoding=")] == "-Dfile.encoding=" {
			return args
		}
	}
	result := make([]string, 0, len(args)+1)
	result = append(result, "-Dfile.encoding=UTF-8")
	return append(result, args...)
}

func officialSleepDifferentialUnavailable(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("OPFOR_REQUIRE_SLEEP_JAR") == "1" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func TestOfficialSleepDifferentialToolsStrictMode(t *testing.T) {
	if os.Getenv("OPFOR_OFFICIAL_SLEEP_STRICT_HELPER") == "1" {
		t.Setenv("OPFOR_SLEEP_JAR", "")
		t.Setenv("OPFOR_REQUIRE_SLEEP_JAR", "1")
		officialSleepDifferentialTools(t)
		return
	}

	command := osexec.Command(os.Args[0], "-test.run=^TestOfficialSleepDifferentialToolsStrictMode$")
	command.Env = append(os.Environ(), "OPFOR_OFFICIAL_SLEEP_STRICT_HELPER=1")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("strict missing JAR unexpectedly passed\n%s", output)
	}
	if !strings.Contains(string(output), "OPFOR_SLEEP_JAR is unset") {
		t.Fatalf("strict missing JAR output did not explain the failure\n%s", output)
	}
}

func TestOfficialSleepJavaArgsAlwaysUseUTF8(t *testing.T) {
	if got := officialSleepJavaArgs([]string{"-jar", "sleep.jar"}); len(got) != 3 || got[0] != "-Dfile.encoding=UTF-8" {
		t.Fatalf("default Java args = %#v", got)
	}
	explicit := []string{"--add-opens=java.base/java.util=ALL-UNNAMED", "-Dfile.encoding=UTF-8", "-jar", "sleep.jar"}
	if got := officialSleepJavaArgs(explicit); len(got) != len(explicit) {
		t.Fatalf("explicit UTF-8 Java args gained a duplicate: %#v", got)
	}
}
