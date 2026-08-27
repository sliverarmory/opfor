package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sleepNumericWarningProbeName = "sleep-numeric-warning.sl"

const sleepNumericWarningProbe = `sub parse_bad {
    println("parse-bad-before");
    parseNumber("xyz", 10);
    println("parse-bad-tail");
}
parse_bad();
println("parse-bad-resume");
sub parse_radix {
    println("parse-radix-before");
    parseNumber("1", 1);
    println("parse-radix-tail");
}
parse_radix();
println("parse-radix-resume");
sub parse_empty {
    println("parse-empty-before");
    parseNumber("", 10);
    println("parse-empty-tail");
}
parse_empty();
println("parse-empty-resume");
println("parse-arabic=" . parseNumber("١٢٣", 10));
println("parse-fullwidth=" . parseNumber("ＦＦ", 16));
println("format-arabic=" . formatNumber("١٢٣", 10, 16));
sub parse_binary_bad {
    println("parse-binary-bad-before");
    parseNumber("2", 2);
    println("parse-binary-bad-tail");
}
parse_binary_bad();
println("parse-binary-bad-resume");
sub parse_arabic_binary_bad {
    println("parse-arabic-binary-bad-before");
    parseNumber("٢", 2);
    println("parse-arabic-binary-bad-tail");
}
parse_arabic_binary_bad();
println("parse-arabic-binary-bad-resume");
sub parse_embedded_sign {
    println("parse-embedded-sign-before");
    parseNumber("1-0", 10);
    println("parse-embedded-sign-tail");
}
parse_embedded_sign();
println("parse-embedded-sign-resume");
println("format-high=" . formatNumber("15", 37));
println("format-low=" . formatNumber("15", 1));
println("format-three=" . formatNumber("ff", 16, 2));
println("format-three-high=" . formatNumber("ff", 16, 37));
println("format-four=" . formatNumber("15", 10, 2, 8));
sub format_four_bad {
    println("format-four-bad-before");
    formatNumber("ff", 16, 2, 8);
    println("format-four-bad-tail");
}
format_four_bad();
println("format-four-bad-resume");
sub rand_zero {
    println("rand-zero-before");
    rand(0);
    println("rand-zero-tail");
}
rand_zero();
println("rand-zero-resume");
sub rand_negative {
    println("rand-negative-before");
    rand(-1);
    println("rand-negative-tail");
}
rand_negative();
println("rand-negative-resume");
sub rand_empty {
    println("rand-empty-before");
    rand(@());
    println("rand-empty-tail");
}
rand_empty();
println("rand-empty-resume");
println("done");
`

const sleepNumericWarningNormalizedOutput = `parse-bad-before
Warning: For input string: "xyz" at <source>:<line>
parse-bad-resume
parse-radix-before
Warning: Radix out of range at <source>:<line>
parse-radix-resume
parse-empty-before
Warning: Zero length BigInteger at <source>:<line>
parse-empty-resume
parse-arabic=123
parse-fullwidth=255
format-arabic=7b
parse-binary-bad-before
Warning: For input string: "2" under radix 2 at <source>:<line>
parse-binary-bad-resume
parse-arabic-binary-bad-before
Warning: For input string: "٢" under radix 2 at <source>:<line>
parse-arabic-binary-bad-resume
parse-embedded-sign-before
Warning: Illegal embedded sign character at <source>:<line>
parse-embedded-sign-resume
format-high=15
format-low=15
format-three=11111111
format-three-high=255
format-four=15
format-four-bad-before
Warning: For input string: "ff" at <source>:<line>
format-four-bad-resume
rand-zero-before
Warning: bound must be positive at <source>:<line>
rand-zero-resume
rand-negative-before
Warning: bound must be positive at <source>:<line>
rand-negative-resume
rand-empty-before
Warning: bound must be positive at <source>:<line>
rand-empty-resume
done
`

func TestSleepNumericBridgeWarningCompatibility(t *testing.T) {
	output := runSleepNumericWarningProbe(t)
	if got := normalizeSleepNumericWarningLocations(output); got != sleepNumericWarningNormalizedOutput {
		t.Fatalf("numeric bridge output mismatch\nwant:\n%sgot:\n%s", sleepNumericWarningNormalizedOutput, got)
	}
}

func TestSleepNumericBridgeWarningOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, sleepNumericWarningProbeName)
	if err := os.WriteFile(path, []byte(sleepNumericWarningProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep numeric bridge probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepNumericWarningProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep numeric bridge output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepNumericWarningProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepNumericWarningProbeName, sleepNumericWarningProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func normalizeSleepNumericWarningLocations(output string) string {
	location := " at " + sleepNumericWarningProbeName + ":"
	lines := strings.SplitAfter(output, "\n")
	for index, line := range lines {
		if !strings.HasPrefix(line, "Warning: ") {
			continue
		}
		position := strings.LastIndex(line, location)
		if position < 0 {
			continue
		}
		newline := ""
		if strings.HasSuffix(line, "\n") {
			newline = "\n"
		}
		lines[index] = line[:position] + " at <source>:<line>" + newline
	}
	return strings.Join(lines, "")
}
