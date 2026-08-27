package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const blockWarningRecoveryProbe = `println("==if==");
if (1) {
    println("if-before");
    include();
    println("if-after");
} else {
    println("if-else");
}
println("if-tail");
println("==else==");
if (0) {
    println("else-then");
} else {
    println("else-before");
    include();
    println("else-after");
}
println("else-tail");
println("==while==");
$i = 0;
while ($i < 2) {
    $i++;
    println("while-" . $i . "-before");
    include();
    println("while-after");
}
println("while-tail=" . $i);
println("==for==");
$j = 0;
for ($i = 0; $i < 2; $j++) {
    $i++;
    println("for-" . $i . "-before");
    include();
    println("for-after");
}
println("for-tail=" . $i . "/" . $j);
println("==foreach==");
foreach $x (@("a", "b")) {
    println("foreach-" . $x . "-before");
    include();
    println("foreach-after");
}
println("foreach-tail");
println("==nested==");
if (1) {
    println("outer-before");
    if (1) {
        println("inner-before");
        include();
        println("inner-after");
    }
    println("outer-after");
}
println("nested-tail");
println("==sub==");
sub bad {
    println("sub-before");
    include();
    println("sub-after");
}
bad();
println("sub-tail");
println("==try==");
try {
    println("try-before");
    include();
    println("try-after");
}
catch $error {
    println("caught=" . $error);
}
println("try-tail");
`

const blockWarningRecoveryProbeOutput = `==if==
if-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:4
if-tail
==else==
else-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:15
else-tail
==while==
while-1-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:24
while-2-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:24
while-tail=2
==for==
for-1-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:33
for-2-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:33
for-tail=2/0
==foreach==
foreach-a-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:40
foreach-b-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:40
foreach-tail
==nested==
outer-before
inner-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:49
outer-after
nested-tail
==sub==
sub-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:58
sub-tail
==try==
try-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-probe.sl:66
try-tail
`

const blockWarningConditionProbe = `sub conditional {
    println("condition-before");
    if (include()) {
        println("then");
    }
    println("condition-after");
}
conditional();
println("caller-tail");
`

const blockWarningConditionProbeOutput = `condition-before
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-condition-probe.sl:3
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-condition-probe.sl:3
caller-tail
`

const blockWarningCatchProbe = `try {
    throw "boom";
}
catch $error {
    println("catch-before=" . $error);
    include();
    println("catch-after");
}
println("catch-tail");
`

const blockWarningCatchProbeOutput = `catch-before=boom
Warning: internal error - class java.util.EmptyStackException at opfor-block-warning-catch-probe.sl:6
catch-tail
`

var blockWarningRecoveryProbes = []struct {
	name   string
	source string
	want   string
}{
	{
		name:   "opfor-block-warning-probe.sl",
		source: blockWarningRecoveryProbe,
		want:   blockWarningRecoveryProbeOutput,
	},
	{
		name:   "opfor-block-warning-condition-probe.sl",
		source: blockWarningConditionProbe,
		want:   blockWarningConditionProbeOutput,
	},
	{
		name:   "opfor-block-warning-catch-probe.sl",
		source: blockWarningCatchProbe,
		want:   blockWarningCatchProbeOutput,
	},
}

func TestSleepBlockWarningRecovery(t *testing.T) {
	for _, probe := range blockWarningRecoveryProbes {
		probe := probe
		t.Run(strings.TrimSuffix(probe.name, ".sl"), func(t *testing.T) {
			got := runBlockWarningRecoveryProbe(t, probe.name, probe.source)
			if !bytes.Equal(got, []byte(probe.want)) {
				t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", probe.want, got)
			}
		})
	}
}

func TestSleepBlockWarningRecoveryOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	for _, probe := range blockWarningRecoveryProbes {
		probe := probe
		t.Run(strings.TrimSuffix(probe.name, ".sl"), func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, probe.name)
			if err := os.WriteFile(path, []byte(probe.source), 0o600); err != nil {
				t.Fatal(err)
			}
			command := officialSleepJavaCommand(java, "-jar", jar, path)
			command.Dir = directory
			want, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("official Sleep block-warning probe: %v\n%s", err, want)
			}

			got := runBlockWarningRecoveryProbe(t, probe.name, probe.source)
			if !bytes.Equal(got, want) {
				t.Fatalf("official Sleep block-warning output mismatch\nwant:\n%sgot:\n%s", want, got)
			}
		})
	}
}

func runBlockWarningRecoveryProbe(t *testing.T, name, source string) []byte {
	t.Helper()

	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), name, source); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
