package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const portableScriptLoaderPostUnloadMutationProbe = `import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "child", '$runs++; if ($saved is $null) { $saved = lambda({ $savedCounter++; return $savedCounter; }); } sub exported { return 41; } if ($runs == 2) { sub post_unload { return 77; } } return [$saved];', $null];
$environment = [$child getScriptEnvironment];
$first = [$child runScript];
$old = [$environment getFunction: "&exported"];
println("r1=" . $first . "/export=" . [$old]);
[$loader unloadScript: $child];
$same_environment = 0;
if ($environment is [$child getScriptEnvironment]) { $same_environment = 1; }
$same_handle = 0;
if ($old is [$environment getFunction: "&exported"]) { $same_handle = 1; }
println("state=" . [$child isLoaded] . "/" . [$loader isLoaded: "child"] . "/env=" . $same_environment . "/handle=" . $same_handle);
println("old=" . [$old]);
$second = [$child runScript];
$post_unload = [$environment getFunction: "&post_unload"];
$current = [$environment getFunction: "&exported"];
$same_after_rerun = 0;
if ($old is $current) { $same_after_rerun = 1; }
println("r2=" . $second . "/post=" . [$post_unload]);
println("rebound=" . $same_after_rerun . "/old=" . [$old] . "/new=" . [$current] . "/loaded=" . [$child isLoaded]);
`

const portableScriptLoaderPostUnloadMutationOutput = "r1=1/export=41\n" +
	"state=0/0/env=1/handle=1\n" +
	"old=41\n" +
	"r2=2/post=77\n" +
	"rebound=0/old=41/new=41/loaded=0\n"

const portableScriptLoaderForkAfterUnloadProbe = `import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: 'child', 'return fork({ println($source, "ready"); readln($source); sub forked { return 88; } return &forked; });', $null];
$handle = [$child runScript];
$ready = readln($handle);
[$loader unloadScript: $child];
println($handle, 'go');
$callback = wait($handle);
println('ready=' . $ready . '/parent=' . [$child isLoaded] . '/callback=' . [$callback]);
`

const portableScriptLoaderForkAfterUnloadOutput = "ready=ready/parent=0/callback=88\n"

func TestPortableScriptLoaderPostUnloadRerunMutatesSameEnvironment(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "script-loader-post-unload.sl", portableScriptLoaderPostUnloadMutationProbe); err != nil {
		t.Fatalf("OPFOR post-unload mutation probe: %v\n%s", err, output.Bytes())
	}
	if got := output.String(); got != portableScriptLoaderPostUnloadMutationOutput {
		t.Fatalf("post-unload mutation output mismatch\nwant:\n%sgot:\n%s", portableScriptLoaderPostUnloadMutationOutput, got)
	}
}

func TestOfficialSleepPortableScriptLoaderPostUnloadRerunMutation(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	mainPath := filepath.Join(t.TempDir(), "script-loader-post-unload.sl")
	if err := os.WriteFile(mainPath, []byte(portableScriptLoaderPostUnloadMutationProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	reference, err := officialSleepJavaCommand(java, "-Dfile.encoding=UTF-8", "-jar", jar, mainPath).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep post-unload mutation probe: %v\n%s", err, reference)
	}
	if got := string(reference); got != portableScriptLoaderPostUnloadMutationOutput {
		t.Fatalf("official post-unload mutation output mismatch\nwant:\n%sgot:\n%s", portableScriptLoaderPostUnloadMutationOutput, got)
	}
}

func TestPortableScriptLoaderForkOutlivesLogicalUnload(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "script-loader-fork-unload.sl", portableScriptLoaderForkAfterUnloadProbe); err != nil {
		t.Fatalf("OPFOR fork-after-unload probe: %v\n%s", err, output.Bytes())
	}
	if got := output.String(); got != portableScriptLoaderForkAfterUnloadOutput {
		t.Fatalf("fork-after-unload output mismatch\nwant:\n%sgot:\n%s", portableScriptLoaderForkAfterUnloadOutput, got)
	}
}

func TestOfficialSleepPortableScriptLoaderForkOutlivesLogicalUnload(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	mainPath := filepath.Join(t.TempDir(), "script-loader-fork-unload.sl")
	if err := os.WriteFile(mainPath, []byte(portableScriptLoaderForkAfterUnloadProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	reference, err := officialSleepJavaCommand(java, "-Dfile.encoding=UTF-8", "-jar", jar, mainPath).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep fork-after-unload probe: %v\n%s", err, reference)
	}
	if got := string(reference); got != portableScriptLoaderForkAfterUnloadOutput {
		t.Fatalf("official fork-after-unload output mismatch\nwant:\n%sgot:\n%s", portableScriptLoaderForkAfterUnloadOutput, got)
	}
}
