package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestForkWaitRepeatsResultAndTimeoutIsSoft(t *testing.T) {
	program, err := CompileString("fork-wait.sl", `
$handle = fork({ sleep(50); return 73; });
$early = wait($handle, 1);
checkError($problem);
$first = wait($handle);
$second = wait($handle);
return @($early, $problem, $first, $second);
`)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if len(values) != 4 || !values[0].IsNull() || !strings.Contains(values[1].String(), "wait on object timed out") ||
		values[2].Int32() != 73 || values[3].Int32() != 73 {
		t.Fatalf("wait values = %s", result.Describe())
	}
}

func TestForkDiscardsTargetCapturesButSharesInjectedValueBacking(t *testing.T) {
	program, err := CompileString("fork-values.sl", `
$captured = 41;
@shared = @(1);
$target = {
    $seen = $captured;
    push(@value, 2);
    @value = @(9);
    return $seen;
};
$without = wait(fork($target));
$with = wait(fork($target, $captured => 41, @value => @shared));
return @($without, $with, @shared);
`)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	values, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	items := values.Values()
	if len(items) != 3 || !items[0].IsNull() || items[1].Int32() != 41 {
		t.Fatalf("fork capture results = %s", result.Describe())
	}
	shared, ok := items[2].Array()
	if !ok || len(shared.Values()) != 2 || shared.Values()[1].Int32() != 2 {
		t.Fatalf("shared backing = %s, want @(1, 2)", items[2].Describe())
	}
}

func TestForkPreservesCallCCWithinChild(t *testing.T) {
	program, err := CompileString("fork-callcc.sl", `
sub worker {
    callcc { return "parked result"; };
    return "unreachable";
}
return wait(fork(&worker), 1000);
`)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if result.String() != "parked result" {
		t.Fatalf("forked callcc result = %s", result.Describe())
	}
}

func TestForkChildDetachesParentTraceFiber(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "callccfork.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("callccfork.sl", programBytes))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := runtime.Execute(ctx, program); err != nil {
		t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
	}
	waitTrace := ""
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.Contains(line, "Trace: &wait(") {
			waitTrace = line
			break
		}
	}
	if waitTrace == "" || !strings.Contains(waitTrace, " = 'pHEAR'") || strings.Contains(waitTrace, "-goto-") {
		t.Fatalf("wait trace inherited child callcc state: %q\noutput:\n%s", waitTrace, output.String())
	}
}

func TestForkUnloadCancelsBlockedPipeAndJoinsChild(t *testing.T) {
	program, err := CompileString("fork-unload.sl", `
$handle = fork({ readb($source, -1); return 1; });
return $handle;
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, closeRuntime := range []bool{false, true} {
		name := "script-unload"
		if closeRuntime {
			name = "runtime-close"
		}
		t.Run(name, func(t *testing.T) {
			runtime, err := New()
			if err != nil {
				t.Fatal(err)
			}
			script, err := runtime.Load(context.Background(), program)
			if err != nil {
				t.Fatal(err)
			}
			if scripts := runtime.Scripts(); len(scripts) != 1 || scripts[0] != script {
				t.Fatalf("Runtime.Scripts exposed fork children: %#v", scripts)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if closeRuntime {
				err = runtime.Close(ctx)
			} else {
				err = script.Unload(ctx)
			}
			if err != nil {
				t.Fatalf("close: %v", err)
			}
			if scripts := runtime.Scripts(); len(scripts) != 0 {
				t.Fatalf("runtime retained %d script instances after unload", len(scripts))
			}
			runtime.mu.RLock()
			internalCount := len(runtime.scripts)
			runtime.mu.RUnlock()
			if internalCount != 0 {
				t.Fatalf("runtime retained %d internal fork instances after unload", internalCount)
			}
		})
	}
}
