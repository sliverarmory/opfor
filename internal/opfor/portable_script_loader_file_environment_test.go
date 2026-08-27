package opfor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestPortableScriptLoaderFileOverloadsAndPrivateEnvironment(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "file-child.sl")
	if err := os.WriteFile(childPath, []byte(`return 7;`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := portableScriptLoaderFileEnvironmentProbe(filepath.ToSlash(childPath))
	program, err := CompileString("loader-file-environment.sl", source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	var resolverCalls atomic.Int32
	runtime, err := New(
		WithStdout(&output),
		WithStderr(&output),
		WithSourceResolver(SourceResolverFunc(func(context.Context, SourceRequest) (Source, error) {
			resolverCalls.Add(1)
			return Source{}, fmt.Errorf("File overload unexpectedly reached SourceResolver")
		})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\n%s", err, output.String())
	}
	want := "file=1/1/1/1/1/1/2/1\nprivate=1/1/1/1\nrun=1/1/1/3\n"
	if got := output.String(); got != want {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
	if got := resolverCalls.Load(); got != 0 {
		t.Fatalf("File overload resolver calls = %d, want 0", got)
	}
}

func TestOfficialSleepPortableScriptLoaderFileOverloadsAndPrivateEnvironment(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	childPath := filepath.Join(directory, "file-child.sl")
	mainPath := filepath.Join(directory, "loader-file-environment.sl")
	if err := os.WriteFile(childPath, []byte(`return 7;`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := portableScriptLoaderFileEnvironmentProbe(filepath.ToSlash(childPath))
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	command := officialSleepJavaCommand(java, "-Dfile.encoding=UTF-8", "-jar", jar, mainPath)
	reference, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep: %v\n%s", err, reference)
	}
	program, err := CompileString(mainPath, source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official ScriptLoader File/environment mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}

func portableScriptLoaderFileEnvironmentProbe(childPath string) string {
	return fmt.Sprintf(`import java.io.File;
import java.util.Hashtable;
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$file = [new File: %q];
$block = [$loader compileScript: $file];
$block_match = 0; if ([$block getSource] eq [$file getAbsolutePath]) { $block_match = 1; }
$shared_table = [new Hashtable];
$shared = [$loader loadScript: $file, $shared_table];
$plain = [$loader loadScript: $file];
$name_match = 0; if ([$shared getName] eq [$file getAbsolutePath]) { $name_match = 1; }
$shared_run = [$shared runScript];
$shared_run_match = 0; if ($shared_run == 7) { $shared_run_match = 1; }
$plain_run = [$plain runScript];
$plain_run_match = 0; if ($plain_run == 7) { $plain_run_match = 1; }
println("file=" . $block_match . "/" . $name_match . "/" . $shared_run_match . "/" . $plain_run_match . "/" . [$shared_table containsKey: "(isloaded)"] . "/" . [$shared_table containsKey: "&println"] . "/" . [[$loader getScripts] size] . "/" . [[$loader getScriptsByKey] size]);
$private = [$loader loadScript: "private", 'sub private_fn { return 9; } return 1;', $null];
$private_environment = [$private getScriptEnvironment];
$private_table = [$private_environment getEnvironment];
$private_function = 0; if ([$private_environment getFunction: "&println"] isa ^sleep.interfaces.Function) { $private_function = 1; }
$private_table_type = 0; if ($private_table isa ^java.util.Hashtable) { $private_table_type = 1; }
println("private=" . [$private_table containsKey: "(isloaded)"] . "/" . [$private_table containsKey: "&println"] . "/" . $private_function . "/" . $private_table_type);
$private_result = [$private runScript];
$sub_function = 0; if ([$private_environment getFunction: "&private_fn"] isa ^sleep.interfaces.Function) { $sub_function = 1; }
[$private_table put: "&private_fn", [$private_table get: "&strlen"]];
$rerouted = [$private_environment evaluateExpression: 'private_fn("abc")'];
println("run=" . $private_result . "/" . [$private_table containsKey: "&private_fn"] . "/" . $sub_function . "/" . $rerouted);
`, childPath)
}
