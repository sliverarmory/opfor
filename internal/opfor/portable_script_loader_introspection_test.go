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

func TestPortableScriptEnvironmentTypedGetterMaterialization(t *testing.T) {
	runtime, err := New(
		WithEnvironment("ordinary_bridge", EnvironmentOrdinary),
		WithEnvironment("predicate_bridge", EnvironmentPredicate),
		WithEnvironment("filter_bridge", EnvironmentFilter),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	table := newPortableJavaMap("Hashtable", nil)
	loader := &portableScriptLoader{runtime: runtime}
	shared := portableSharedEnvironment(table)
	shared.installGlobalBridges(loader, runtime)
	environment := &portableScriptEnvironment{table: table}

	tests := []struct {
		method string
		key    string
		class  string
	}{
		{"getFunction", "&println", "sleep.interfaces.Function"},
		{"getFunction", "function", "sleep.interfaces.Function"},
		{"getFunction", "-exists", "sleep.interfaces.Function"},
		{"getPredicate", "+", "sleep.interfaces.Predicate"},
		{"getPredicate", "eq", "sleep.interfaces.Predicate"},
		{"getPredicate", "-eof", "sleep.interfaces.Predicate"},
		{"getPredicate", "-isDir", "sleep.interfaces.Predicate"},
		{"getPredicate", "ismatch", "sleep.interfaces.Predicate"},
		{"getOperator", "+", "sleep.interfaces.Operator"},
		{"getOperator", ".", "sleep.interfaces.Operator"},
		{"getOperator", "=>", "sleep.interfaces.Operator"},
		{"getFunctionEnvironment", "sub", "sleep.interfaces.Environment"},
		{"getFunctionEnvironment", "on", "sleep.interfaces.Environment"},
		{"getFunctionEnvironment", "ordinary_bridge", "sleep.interfaces.Environment"},
		{"getPredicateEnvironment", "predicate_bridge", "sleep.interfaces.PredicateEnvironment"},
		{"getFilterEnvironment", "filter_bridge", "sleep.interfaces.FilterEnvironment"},
	}
	for _, test := range tests {
		t.Run(test.method+"/"+test.key, func(t *testing.T) {
			value, handled, invokeErr := environment.invoke(context.Background(), ObjectInvocation{
				Op: ObjectInvoke, Message: test.method,
				Arguments: []Argument{{Value: String(test.key)}},
			})
			if !handled || invokeErr != nil {
				t.Fatalf("%s(%q) = (handled %v, %v), want a value", test.method, test.key, handled, invokeErr)
			}
			if !portableScriptEnvironmentValueImplements(value, test.class) {
				t.Fatalf("%s(%q) = %s, want %s", test.method, test.key, value.Describe(), test.class)
			}
		})
	}

	for _, identity := range []struct {
		name                 string
		leftKey, leftClass   string
		rightKey, rightClass string
	}{
		{"BasicNumbers predicate/operator", "+", "sleep.interfaces.Predicate", "+", "sleep.interfaces.Operator"},
		{"BasicNumbers function/operator", "&abs", "sleep.interfaces.Function", "+", "sleep.interfaces.Operator"},
		{"BasicStrings function aliases", "&charAt", "sleep.interfaces.Function", "&byteAt", "sleep.interfaces.Function"},
		{"BasicUtilities function/predicate", "&size", "sleep.interfaces.Function", "-istrue", "sleep.interfaces.Predicate"},
		{"BasicUtilities hash aliases", "&hash", "sleep.interfaces.Function", "&%", "sleep.interfaces.Function"},
		{"BasicIO direct functions", "&allocate", "sleep.interfaces.Function", "&checksum", "sleep.interfaces.Function"},
		{"BasicIO exec/direct functions", "__EXEC__", "sleep.interfaces.Function", "&allocate", "sleep.interfaces.Function"},
		{"BasicIO consume aliases", "&consume", "sleep.interfaces.Function", "&skip", "sleep.interfaces.Function"},
		{"BasicIO print aliases", "&println", "sleep.interfaces.Function", "&printf", "sleep.interfaces.Function"},
		{"FileSystemBridge function/predicate", "&deleteFile", "sleep.interfaces.Function", "-exists", "sleep.interfaces.Predicate"},
		{"RegexBridge function/predicate", "&matched", "sleep.interfaces.Function", "ismatch", "sleep.interfaces.Predicate"},
		{"FileSystemBridge listing aliases", "&ls", "sleep.interfaces.Function", "&listRoots", "sleep.interfaces.Function"},
		{"DefaultEnvironment keywords", "sub", "sleep.interfaces.Environment", "inline", "sleep.interfaces.Environment"},
	} {
		left, leftErr := portableScriptEnvironmentTypedEntry(table, String(identity.leftKey), identity.leftClass)
		right, rightErr := portableScriptEnvironmentTypedEntry(table, String(identity.rightKey), identity.rightClass)
		if leftErr != nil || rightErr != nil || !left.IdentityEqual(right) {
			t.Fatalf("%s entries = (%s, %v) / (%s, %v), want shared identity", identity.name, left.Describe(), leftErr, right.Describe(), rightErr)
		}
	}

	shared.putLockedForTest(t, String("eq"), String("not a Predicate"))
	_, handled, err := environment.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "getPredicate",
		Arguments: []Argument{{Value: String("eq")}},
	})
	if !handled || err == nil || err.Error() != "java.lang.ClassCastException" {
		t.Fatalf("wrong-type getPredicate = (handled %v, %v), want java.lang.ClassCastException", handled, err)
	}
	shared.installGlobalBridges(loader, runtime)
	_, _, err = environment.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "getPredicate",
		Arguments: []Argument{{Value: String("eq")}},
	})
	if err == nil || err.Error() != "java.lang.ClassCastException" {
		t.Fatalf("matching (isloaded) marker reinstalled caller-mutated predicate: %v", err)
	}
	foreignLoader := &portableScriptLoader{runtime: runtime}
	shared.installGlobalBridges(foreignLoader, runtime)
	if _, err := portableScriptEnvironmentTypedEntry(table, String("eq"), "sleep.interfaces.Predicate"); err != nil {
		t.Fatalf("foreign loader marker did not reinstall global predicate bridge: %v", err)
	}
	missing, handled, err := environment.invoke(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "getOperator",
		Arguments: []Argument{{Value: String("missing")}},
	})
	if !handled || err != nil || !missing.IsNull() {
		t.Fatalf("missing getOperator = (%s, handled %v, %v), want null", missing.Describe(), handled, err)
	}

	taintRuntime, err := New(WithTaintMode(true))
	if err != nil {
		t.Fatalf("New taint runtime: %v", err)
	}
	t.Cleanup(func() { _ = taintRuntime.Close(context.Background()) })
	taintTable := newPortableJavaMap("Hashtable", nil)
	taintShared := portableSharedEnvironment(taintTable)
	taintShared.installGlobalBridges(&portableScriptLoader{runtime: taintRuntime}, taintRuntime)
	if _, err := portableScriptEnvironmentTypedEntry(taintTable, String("+"), "sleep.interfaces.Predicate"); err == nil || err.Error() != "java.lang.ClassCastException" {
		t.Fatalf("taint-mode numeric Sanitizer predicate cast = %v, want java.lang.ClassCastException", err)
	}
	if _, err := portableScriptEnvironmentTypedEntry(taintTable, String("+"), "sleep.interfaces.Operator"); err != nil {
		t.Fatalf("taint-mode numeric Sanitizer operator cast: %v", err)
	}
	taintFunction, err := portableScriptEnvironmentTypedEntry(taintTable, String("&abs"), "sleep.interfaces.Function")
	if err != nil {
		t.Fatalf("taint-mode numeric Sanitizer function cast: %v", err)
	}
	taintOperator, err := portableScriptEnvironmentTypedEntry(taintTable, String("+"), "sleep.interfaces.Operator")
	if err != nil || !taintFunction.IdentityEqual(taintOperator) {
		t.Fatalf("taint-mode numeric function/operator identity = (%s, %s, %v), want shared Sanitizer", taintFunction.Describe(), taintOperator.Describe(), err)
	}
	if _, err := portableScriptEnvironmentTypedEntry(taintTable, String("=="), "sleep.interfaces.Predicate"); err != nil {
		t.Fatalf("taint-mode BasicNumbers predicate cast: %v", err)
	}
	for _, identity := range []struct {
		left, right string
		same        bool
	}{
		{"&size", "-istrue", true},
		{"&lambda", "&let", true},
		{"&lambda", "&compile_closure", false},
		{"&function", "&setf", false},
		{"&allocate", "&checksum", true},
		{"&writeObject", "&readObject", false},
		{"&consume", "&skip", true},
		{"&println", "&printf", true},
	} {
		left, _ := portableScriptEnvironmentEntry(taintTable, String(identity.left))
		right, _ := portableScriptEnvironmentEntry(taintTable, String(identity.right))
		if got := left.IdentityEqual(right); got != identity.same {
			t.Fatalf("taint-mode identity %s/%s = %v, want %v", identity.left, identity.right, got, identity.same)
		}
	}
}

func (shared *portableScriptSharedEnvironment) putLockedForTest(t *testing.T, key, value Value) {
	t.Helper()
	shared.table.mu.Lock()
	shared.putLocked(key, value)
	shared.table.mu.Unlock()
}

func TestPortableScriptEnvironmentBlocksAndLiveTableMutation(t *testing.T) {
	program, err := CompileString("loader-introspection-runtime.sl", portableScriptLoaderIntrospectionProbe())
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
	const want = "bridges=1/1/1/1/1/1/1/1/1/1/1/1/1/1/1/1/1/7.0\nblocks=23/1/1/31/41\n"
	if got := output.String(); got != want {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestOfficialSleepPortableScriptEnvironmentIntrospection(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for ScriptEnvironment introspection differential verification")
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
		java = "java"
	}

	directory := t.TempDir()
	mainPath := filepath.Join(directory, "loader-introspection.sl")
	source := portableScriptLoaderIntrospectionProbe()
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(java, "-Dfile.encoding=UTF-8", "-jar", jar, mainPath)
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
		t.Fatalf("official ScriptEnvironment introspection mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}

func portableScriptLoaderIntrospectionProbe() string {
	return `import java.util.Hashtable;
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$table = [new Hashtable];
$child = [$loader loadScript: "introspection-child", 'sub ordinary { return 23; } sub collision { return 31; } inline collision { return 37; } inline inlined { return 29; } return ordinary();', $table];
$environment = [$child getScriptEnvironment];
$numbers_predicate = [$environment getPredicate: "+"];
$numbers_operator = [$environment getOperator: "+"];
$numbers_same = 0; if ($numbers_predicate is $numbers_operator) { $numbers_same = 1; }
$strings_predicate = 0; if ([$environment getPredicate: "eq"] isa ^sleep.interfaces.Predicate) { $strings_predicate = 1; }
$strings_operator = 0; if ([$environment getOperator: "."] isa ^sleep.interfaces.Operator) { $strings_operator = 1; }
$filesystem_predicate = 0; if ([$environment getPredicate: "-isDir"] isa ^sleep.interfaces.Predicate) { $filesystem_predicate = 1; }
$regex_predicate = 0; if ([$environment getPredicate: "ismatch"] isa ^sleep.interfaces.Predicate) { $regex_predicate = 1; }
$utility_operator = 0; if ([$environment getOperator: "=>"] isa ^sleep.interfaces.Operator) { $utility_operator = 1; }
$sub_environment = [$environment getFunctionEnvironment: "sub"];
$inline_environment = [$environment getFunctionEnvironment: "inline"];
$environment_same = 0; if ($sub_environment is $inline_environment) { $environment_same = 1; }
$environment_type = 0; if ($sub_environment isa ^sleep.interfaces.Environment) { $environment_type = 1; }
$function_special = 0; if ([$environment getFunction: "function"] isa ^sleep.interfaces.Function) { $function_special = 1; }
$number_function = [$environment getFunction: "&abs"];
$number_function_same = 0; if ($number_function is $numbers_operator) { $number_function_same = 1; }
$filesystem_function = [$environment getFunction: "&deleteFile"];
$filesystem_function_same = 0; if ($filesystem_function is [$environment getPredicate: "-exists"]) { $filesystem_function_same = 1; }
$regex_function = [$environment getFunction: "&matched"];
$regex_function_same = 0; if ($regex_function is [$environment getPredicate: "ismatch"]) { $regex_function_same = 1; }
$string_alias_same = 0; if ([$environment getFunction: "&charAt"] is [$environment getFunction: "&byteAt"]) { $string_alias_same = 1; }
$utility_function_same = 0; if ([$environment getFunction: "&size"] is [$environment getPredicate: "-istrue"]) { $utility_function_same = 1; }
$io_function_same = 0; if ([$environment getFunction: "&allocate"] is [$environment getFunction: "&checksum"]) { $io_function_same = 1; }
$io_alias_same = 0; if ([$environment getFunction: "&println"] is [$environment getFunction: "&printf"]) { $io_alias_same = 1; }
$io_exec_same = 0; if ([$environment getFunction: "__EXEC__"] is [$environment getFunction: "&allocate"]) { $io_exec_same = 1; }
$number_dispatch = [$environment evaluateExpression: 'abs(-7)'];
println("bridges=" . $numbers_same . "/" . $strings_predicate . "/" . $strings_operator . "/" . $filesystem_predicate . "/" . $regex_predicate . "/" . $utility_operator . "/" . $environment_same . "/" . $environment_type . "/" . $function_special . "/" . $number_function_same . "/" . $filesystem_function_same . "/" . $regex_function_same . "/" . $string_alias_same . "/" . $utility_function_same . "/" . $io_function_same . "/" . $io_alias_same . "/" . $io_exec_same . "/" . $number_dispatch);
$ordinary_result = [$child runScript];
$block_type = 0; if ([$environment getBlock: "&inlined"] isa ^sleep.engine.Block) { $block_type = 1; }
$inline_function_absent = 1 - [$table containsKey: "&inlined"];
$function_precedence = [$environment evaluateExpression: 'collision()'];
$replacement = [$loader compileScript: "replacement", 'return 41;'];
[$table put: "^&inlined", $replacement];
$replacement_result = [$environment evaluateExpression: 'inlined()'];
println("blocks=" . $ordinary_result . "/" . $block_type . "/" . $inline_function_absent . "/" . $function_precedence . "/" . $replacement_result);
`
}
