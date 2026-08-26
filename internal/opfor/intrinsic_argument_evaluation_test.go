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

const intrinsicArgumentEvaluationProbe = `$order = "";
sub mark { $order .= $1; return $2; }
find(mark("A", "abc"), mark("B", "b"), mark("C", 0));
println("find-order=" . $order);
$order = "";
if ("abc" ismatch "a(b)c") { matched(mark("A", 1), mark("B", 2)); }
println("matched-order=" . $order);
$order = "";
getStackTrace(mark("A", 1), mark("B", 2));
println("stack-order=" . $order);
$order = "";
$f = function(mark("A", "&find"), mark("B", "ignored"));
println("function-order=" . $order);
$order = "";
$base = { return $x . "/" . $y; };
$bound = let(mark("A", $base), $x => mark("B", "X"), $y => mark("C", "Y"));
println("let-order=" . $order . "/" . [$bound]);
$order = "";
$g = "G";
sub local_probe { local(mark("A", chr(36) . "l"), mark("B", chr(36) . "g")); $l = "L"; $g = "X"; println("inner=" . $l . "/" . $g); }
local_probe();
println("local-order=" . $order);
println("outer-g=" . $g);
$order = "";
compile_closure("(");
$error = "before";
checkError($error, mark("B", 1));
println("check-order=" . $order);
if ($error ne "before") { println("changed=yes"); } else { println("changed=no"); }
compile_closure("(");
println("literal=" . checkError("before"));
println("tail");
`

const intrinsicArgumentEvaluationOutput = `find-order=CBA
matched-order=BA
stack-order=BA
function-order=BA
let-order=CBA/X/Y
inner=L/X
local-order=BA
outer-g=X
check-order=B
changed=yes
literal=YourCodeSucksException: 1 error(s): Mismatched Parentheses - missing close paren at 0
tail
`

func TestIntrinsicArgumentsEvaluateOnceInSleepOrder(t *testing.T) {
	if got := runIntrinsicArgumentEvaluationProbe(t); !bytes.Equal(got, []byte(intrinsicArgumentEvaluationOutput)) {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", intrinsicArgumentEvaluationOutput, got)
	}
}

func TestIntrinsicArgumentEvaluationOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for intrinsic argument evaluation verification")
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
	path := filepath.Join(directory, "intrinsic-argument-evaluation.sl")
	if err := os.WriteFile(path, []byte(intrinsicArgumentEvaluationProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep intrinsic argument evaluation probe: %v\n%s", err, want)
	}
	if got := runIntrinsicArgumentEvaluationProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep intrinsic argument evaluation mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runIntrinsicArgumentEvaluationProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "intrinsic-argument-evaluation.sl", intrinsicArgumentEvaluationProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
