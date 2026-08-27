package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const intrinsicBridgeIdentityProbe = `setf("&afind", function("&find"));
setf("&amatches", function("&matches"));
setf("&amatched", function("&matched"));
println("find=" . afind("abc", "b"));
println("matches=" . join(",", amatches("a1b22", "([0-9]+)")));
if ("abc" ismatch "a(b)c") { println("matched=" . join(",", amatched())); }
$x = "G";
setf("&local", function("&global"));
sub p { local(chr(36) . "x"); $x = "L"; println("inner=" . $x); }
p();
println("outer=" . $x);
$base = { return $a . "/" . $b; };
setf("&bind", function("&lambda"));
$bound = bind($base, $a => "A", $b => "B");
println("bound=" . [$bound]);
if ($bound is $base) { println("identity=same"); } else { println("identity=different"); }
compile_closure("(");
$error = "before";
setf("&checkError", function("&getStackTrace"));
checkError($error);
println("error=" . $error);
println("tail");
sub zero_setf { setf(); println("zero-setf-tail"); }
zero_setf();
println("zero-setf-resume");
`

const intrinsicBridgeIdentityOutput = `find=1
matches=1,22
matched=b
inner=L
outer=G
bound=A/B
identity=same
error=YourCodeSucksException: 1 error(s): Mismatched Parentheses - missing close paren at 0
tail
zero-setf-tail
zero-setf-resume
`

func TestIntrinsicFunctionHandlesPreserveBridgeIdentity(t *testing.T) {
	if got := runIntrinsicBridgeIdentityProbe(t); !bytes.Equal(got, []byte(intrinsicBridgeIdentityOutput)) {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", intrinsicBridgeIdentityOutput, got)
	}
}

func TestIntrinsicFunctionBridgeIdentityOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, "intrinsic-bridge-identity.sl")
	if err := os.WriteFile(path, []byte(intrinsicBridgeIdentityProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep intrinsic bridge identity probe: %v\n%s", err, want)
	}
	if got := runIntrinsicBridgeIdentityProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep intrinsic bridge identity output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runIntrinsicBridgeIdentityProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "intrinsic-bridge-identity.sl", intrinsicBridgeIdentityProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
