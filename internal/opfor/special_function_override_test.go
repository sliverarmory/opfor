package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const intrinsicFunctionOverrideProbe = `$old = function("&compile_closure"); setf("&compile_closure", { return "override-compile_closure"; }); println(compile_closure("return 1;")); setf("&compile_closure", $old);
$old = function("&find"); setf("&find", { return "override-find"; }); println(find("abc", "b")); setf("&find", $old);
$old = function("&function"); setf("&function", { return "override-function"; }); println(function("&print")); setf("&function", $old);
$old = function("&getStackTrace"); setf("&getStackTrace", { return "override-getStackTrace"; }); println(getStackTrace()); setf("&getStackTrace", $old);
$old = function("&global"); setf("&global", { return "override-global"; }); println(global("$x")); setf("&global", $old);
$old = function("&inline"); setf("&inline", { return "override-inline"; }); println(inline({ return "body-inline"; })); setf("&inline", $old);
$old = function("&invoke"); setf("&invoke", { return "override-invoke"; }); println(invoke({ return "body-invoke"; })); setf("&invoke", $old);
$old = function("&lambda"); setf("&lambda", { return "override-lambda"; }); println(lambda({ return "body-lambda"; })); setf("&lambda", $old);
$old = function("&let"); setf("&let", { return "override-let"; }); println(let({ return "body-let"; })); setf("&let", $old);
$old = function("&local"); setf("&local", { return "override-local"; }); println(local("$y")); setf("&local", $old);
$old = function("&matched"); setf("&matched", { return "override-matched"; }); println(matched()); setf("&matched", $old);
$old = function("&matches"); setf("&matches", { return "override-matches"; }); println(matches("abc", "b")); setf("&matches", $old);
$old = function("&this"); setf("&this", { return "override-this"; }); println(this("$z")); setf("&this", $old);
setf("&setf", { return "override-setf"; }); println(setf());
`

const intrinsicFunctionOverrideOutput = `override-compile_closure
override-find
override-function
override-getStackTrace
override-global
override-inline
override-invoke
override-lambda
override-let
override-local
override-matched
override-matches
override-this
override-setf
`

func TestSleepIntrinsicFunctionsCanBeOverriddenAndRestoredWithSetf(t *testing.T) {
	got := runIntrinsicFunctionOverrideProbe(t)
	if got != intrinsicFunctionOverrideOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", intrinsicFunctionOverrideOutput, got)
	}
}

func TestSleepIntrinsicFunctionOverridesOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, "intrinsic-overrides.sl")
	if err := os.WriteFile(path, []byte(intrinsicFunctionOverrideProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-jar", jar, path)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep intrinsic override probe: %v\n%s", err, want)
	}
	if got := []byte(runIntrinsicFunctionOverrideProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep intrinsic override output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestImporterFunctionsOverrideEvaluatorIntrinsics(t *testing.T) {
	setups := map[string]func(*Runtime) error{
		"WithFunction": nil,
		"RegisterFunction": func(runtime *Runtime) error {
			for _, name := range []string{"find", "checkError"} {
				if err := runtime.RegisterFunction(name, intrinsicOverrideNative); err != nil {
					return err
				}
			}
			return nil
		},
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			options := []Option{WithStdout(&output), WithStderr(&output)}
			if setup == nil {
				options = append(options,
					WithFunction("find", intrinsicOverrideNative),
					WithFunction("checkError", intrinsicOverrideNative),
				)
			}
			runtimeInstance, err := New(options...)
			if err != nil {
				t.Fatal(err)
			}
			if setup != nil {
				if err := setup(runtimeInstance); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := runtimeInstance.Eval(context.Background(), "intrinsic-importer-override.sl", `println(find()); println(checkError());`); err != nil {
				t.Fatal(err)
			}
			if got, want := output.String(), "override-find\noverride-checkError\n"; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func intrinsicOverrideNative(_ context.Context, invocation Invocation) (Value, error) {
	return String("override-" + invocation.Name), nil
}

func runIntrinsicFunctionOverrideProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "intrinsic-overrides.sl", intrinsicFunctionOverrideProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
