package opfor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const sleepFunctionResolutionProbe = `import java.util.Hashtable;
import sleep.runtime.ScriptLoader;
sub resolution_type {
    if ($1 is $null) { return "null"; }
    return "function";
}
sub script_probe { return 1; }
println("missing=" . resolution_type(function("&missing")));
println("script=" . resolution_type(function("&script_probe")));
println("intrinsic=" . resolution_type(function("&find")));
println("runtime=" . resolution_type(function("&println")));
setf("&find", $null);
println("removed-intrinsic=" . resolution_type(function("&find")));
setf("&print", $null);
println("removed-runtime=" . resolution_type(function("&print")));
$loader = [new ScriptLoader];
$table = [new Hashtable];
$producer = [$loader loadScript: "producer", 'sub shared_probe { return "shared"; }', $table];
[$producer runScript];
$consumer = [$loader loadScript: "consumer", '$handle = function("&shared_probe"); if ($handle is $null) { return "null"; } return "function/" . [$handle];', $table];
println("shared=" . [$consumer runScript]);
`

const sleepFunctionResolutionOutput = `missing=null
script=function
intrinsic=function
runtime=function
removed-intrinsic=null
removed-runtime=null
shared=function/shared
`

func TestFunctionReturnsOnlyInstalledSleepFunctions(t *testing.T) {
	if got := runSleepFunctionResolutionProbe(t); !bytes.Equal(got, []byte(sleepFunctionResolutionOutput)) {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepFunctionResolutionOutput, got)
	}
}

func TestFunctionDoesNotTreatHostFallbackAsInstalled(t *testing.T) {
	var output bytes.Buffer
	hostCalls := 0
	runtimeInstance, err := New(
		WithStdout(&output),
		WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
			hostCalls++
			if invocation.Name != "host_only" {
				return Null(), fmt.Errorf("unexpected Host call %q", invocation.Name)
			}
			return String("host"), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtimeInstance.Eval(context.Background(), "function-host-resolution.sl", `
sub resolution_type {
    if ($1 is $null) { return "null"; }
    return "function";
}
println("handle=" . resolution_type(function("&host_only")));
println("direct=" . host_only());
`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "handle=null\ndirect=host\n"; got != want {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
	if hostCalls != 1 {
		t.Fatalf("Host calls = %d, want only the direct call", hostCalls)
	}
}

func TestFunctionResolvesRegisteredRuntimeFunctions(t *testing.T) {
	for _, registration := range []string{"WithFunction", "RegisterFunction"} {
		t.Run(registration, func(t *testing.T) {
			calls := 0
			function := func(_ context.Context, invocation Invocation) (Value, error) {
				calls++
				if invocation.Name != "runtime_probe" || invocation.Arg(0).String() != "argument" {
					return Null(), fmt.Errorf("unexpected invocation %#v", invocation)
				}
				return String("registered"), nil
			}
			options := []Option(nil)
			if registration == "WithFunction" {
				options = append(options, WithFunction("runtime_probe", function))
			}
			runtimeInstance, err := New(options...)
			if err != nil {
				t.Fatal(err)
			}
			if registration == "RegisterFunction" {
				if err := runtimeInstance.RegisterFunction("runtime_probe", function); err != nil {
					t.Fatal(err)
				}
			}
			result, err := runtimeInstance.Eval(context.Background(), "function-runtime-resolution.sl", `
$handle = function("&runtime_probe");
if ($handle is $null) { return "null"; }
return [$handle: "argument"];
`)
			if err != nil {
				t.Fatal(err)
			}
			if got := result.String(); got != "registered" {
				t.Fatalf("result = %q, want registered", got)
			}
			if calls != 1 {
				t.Fatalf("registered function calls = %d, want one", calls)
			}
		})
	}
}

func TestFunctionResolutionOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, "function-resolution.sl")
	if err := os.WriteFile(path, []byte(sleepFunctionResolutionProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep function resolution probe: %v\n%s", err, want)
	}
	if !bytes.Equal(want, []byte(sleepFunctionResolutionOutput)) {
		t.Fatalf("official Sleep function resolution output changed\nwant:\n%sgot:\n%s", sleepFunctionResolutionOutput, want)
	}
	if got := runSleepFunctionResolutionProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep function resolution output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepFunctionResolutionProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "function-resolution.sl", sleepFunctionResolutionProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
