package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepTransliterationWarningProbeName = "sleep-tr-warning-probe.sl"

const sleepTransliterationWarningProbe = `sub leading_range {
    println(tr("input", '-a', '', ''));
    println("unreachable-leading");
}
leading_range();
println("after-leading");

sub trailing_range {
    println(tr("input", 'a-', '', ''));
    println("unreachable-trailing");
}
trailing_range();
println("after-trailing");

sub bad_escape {
    println(tr("input", '\q', '', ''));
    println("unreachable-bad-escape");
}
bad_escape();
println("after-bad-escape");

sub trailing_escape {
    println(tr("input", 'a\\', '', ''));
    println("unreachable-trailing-escape");
}
trailing_escape();
println("after-trailing-escape");

sub mapper_range {
    println(tr("input", 'a', '-', ''));
    println("unreachable-mapper-range");
}
mapper_range();
println("after-mapper-range");
`

const sleepTransliterationWarningOutput = `Warning: Dangling range operator '-' near index 1
-a
 ^ at sleep-tr-warning-probe.sl:2
after-leading
Warning: Dangling range operator '-' near index 1
a-
 ^ at sleep-tr-warning-probe.sl:9
after-trailing
Warning: unrecognized escaped meta-character 'q' near index 1
\q
 ^ at sleep-tr-warning-probe.sl:16
after-bad-escape
Warning: attempting to escape end of pattern string near index 1
a\
 ^ at sleep-tr-warning-probe.sl:23
after-trailing-escape
Warning: Dangling range operator '-' near index 0
-
^ at sleep-tr-warning-probe.sl:30
after-mapper-range
`

func TestSleepTransliterationPatternSyntaxWarnings(t *testing.T) {
	if got := runSleepTransliterationWarningProbe(t); got != sleepTransliterationWarningOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepTransliterationWarningOutput, got)
	}
}

func TestSleepTransliterationPatternSyntaxWarningsOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, sleepTransliterationWarningProbeName)
	if err := os.WriteFile(path, []byte(sleepTransliterationWarningProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep transliteration warning probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepTransliterationWarningProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepTransliterationWarningProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), sleepTransliterationWarningProbeName, sleepTransliterationWarningProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
