package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const stringEvaluatorUnderflowProbeName = "sleep-string-evaluator-underflow.sl"

const stringEvaluatorUnderflowProbe = `sub strlen0 { println("strlen0=[" . strlen() . "]"); }
strlen0(); println("after-strlen0");
sub left0 { println(left()); }
left0(); println("after-left0");
sub left1 { println(left("abc")); }
left1(); println("after-left1");
sub right0 { println(right()); }
right0(); println("after-right0");
sub right1 { println(right("abc")); }
right1(); println("after-right1");
sub charAt0 { println(charAt()); }
charAt0(); println("after-charAt0");
sub charAt1 { println("charAt1=[" . charAt("abc") . "]"); }
charAt1(); println("after-charAt1");
sub indexOf0 { println(indexOf()); }
indexOf0(); println("after-indexOf0");
sub indexOf1 { println(indexOf("abc")); }
indexOf1(); println("after-indexOf1");
sub split0 { println(split()); }
split0(); println("after-split0");
sub split1 { println(split(",")); }
split1(); println("after-split1");
sub join0 { println(join()); }
join0(); println("after-join0");
sub join1 { println("join1=[" . join(",") . "]"); }
join1(); println("after-join1");
sub not0 { println(not()); }
not0(); println("after-not0");
sub formatDate0 { println(formatDate()); }
formatDate0(); println("after-formatDate0");
sub parseDate0 { println(parseDate()); }
parseDate0(); println("after-parseDate0");
sub parseDate1 { println(parseDate("yyyy")); }
parseDate1(); println("after-parseDate1");
sub find0 { println("find0=[" . find() . "]"); }
find0(); println("after-find0");
sub matches0 { println(matches()); }
matches0(); println("after-matches0");
sub matches1 { println(matches("abc")); }
matches1(); println("after-matches1");
sub local0 { println(local()); }
local0(); println("after-local0");
sub global0 { println(global()); }
global0(); println("after-global0");
sub this0 { println(this()); }
this0(); println("after-this0");
sub compileClosure0 { println(compile_closure()); }
compileClosure0(); println("after-compileClosure0");
sub lambda0 { println(lambda()); }
lambda0(); println("after-lambda0");
sub let0 { println(let()); }
let0(); println("after-let0");
sub invoke0 { println(invoke()); }
invoke0(); println("after-invoke0");
sub inline0 { println(inline()); }
inline0(); println("after-inline0");
sub function0 { println(function()); }
function0(); println("after-function0");
`

const stringEvaluatorUnderflowNormalizedOutput = `Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-strlen0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-left0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-left1
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-right0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-right1
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-charAt0
charAt1=[a]
after-charAt1
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-indexOf0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-indexOf1
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-split0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-split1
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-join0
join1=[]
after-join1
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-not0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-formatDate0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-parseDate0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-parseDate1
find0=[0]
after-find0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-matches0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-matches1
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-local0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-global0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-this0
Warning: internal error - class java.util.EmptyStackException at <source>:<line>
after-compileClosure0
Warning: expected &closure--received: $null at <source>:<line>
after-lambda0
Warning: expected &closure--received: $null at <source>:<line>
after-let0
Warning: expected &closure--received: $null at <source>:<line>
after-invoke0
Warning: expected &closure--received: $null at <source>:<line>
after-inline0
Warning: &function: requested function name must begin with '&' at <source>:<line>
after-function0
`

func TestSleepBridgeUnderflowSemantics(t *testing.T) {
	output := runSleepBridgeUnderflowProbe(t)
	if got := normalizeSleepBridgeUnderflowLocations(output); got != stringEvaluatorUnderflowNormalizedOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", stringEvaluatorUnderflowNormalizedOutput, got)
	}
}

func TestSleepBridgeUnderflowSemanticsOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for bridge-underflow verification")
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
	path := filepath.Join(directory, stringEvaluatorUnderflowProbeName)
	if err := os.WriteFile(path, []byte(stringEvaluatorUnderflowProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep bridge-underflow probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepBridgeUnderflowProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep bridge-underflow output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepBridgeUnderflowProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), stringEvaluatorUnderflowProbeName, stringEvaluatorUnderflowProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func normalizeSleepBridgeUnderflowLocations(output string) string {
	location := " at " + stringEvaluatorUnderflowProbeName + ":"
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
