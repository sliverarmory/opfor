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

const sleepStockBridgeCalledKeyProbe = `setf("&znum", function("&abs"));
println("num-unknown=[" . znum(-5) . "]");
setf("&sin", function("&abs"));
println("num-cross=" . sin(1.5707963267948966));
setf("&concat", function("&size"));
println("utilities-cross=" . join(",", concat(@(1), @(2))));
setf("&size", function("&getStackTrace"));
println("utilities-intrinsic-cross=" . size(@(1), @(2)));
setf("&zutil", function("&size"));
println("utilities-unknown=[" . zutil(@(1, 2)) . "]");
setf("&zhash", function("&ohash"));
println("hash-default=" . typeOf(zhash(a => 1)));
$mapper = { if ($1 eq "drop") { return $null; } return $1; };
setf("&zmap", function("&map"));
println("map-default=" . join(",", zmap($mapper, @("drop", "x"))));
setf("&zcast", function("&casti"));
$casted = zcast("AB", "c");
println("cast-default=" . [$casted getClass]);
setf("&zeval", function("&eval"));
println("eval-default=" . zeval("2 + 3"));
setf("&zchar", function("&charAt"));
println("char-default=" . zchar("A", 0));
setf("&zsub", function("&mid"));
println("substring-default=" . zsub("abcd", 1, 2));
setf("&zindex", function("&lindexOf"));
println("index-default=" . zindex("ababa", "ba", 0));
setf("&zsort", function("&sorta"));
println("sort-default=" . join(",", zsort(@("b", "a"))));
setf("&zsizeof", function("&sizeof"));
println("io-unknown=[" . zsizeof("C") . "]");
setf("&sizeof", function("&allocate"));
println("io-cross=" . sizeof("C"));
setf("&zfs", function("&cwd"));
println("filesystem-unknown=[" . zfs() . "]");
`

const sleepStockBridgeCalledKeyOutput = `num-unknown=[]
num-cross=1.0
utilities-cross=1,2
utilities-intrinsic-cross=1
utilities-unknown=[]
hash-default=class sleep.engine.types.HashContainer
map-default=x
cast-default=class [C
eval-default=5
char-default=65
substring-default=b
index-default=1
sort-default=b,a
io-unknown=[]
io-cross=1
filesystem-unknown=[]
`

func TestStockSleepFunctionHandlesDispatchTheCalledKey(t *testing.T) {
	if got := runStockBridgeCalledKeyProbe(t); got != sleepStockBridgeCalledKeyOutput {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepStockBridgeCalledKeyOutput, got)
	}
}

func TestStockSleepBridgeHandleWinsOverCalledKeyImporterOverride(t *testing.T) {
	var output bytes.Buffer
	importerCalls := 0
	runtimeInstance, err := New(
		WithStdout(&output),
		WithStderr(&output),
		WithFunction("sin", func(context.Context, Invocation) (Value, error) {
			importerCalls++
			return Double(999), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtimeInstance.Eval(context.Background(), "stock-bridge-importer-collision.sl", `
setf("&sin", function("&abs"));
println(sin(1.5707963267948966));
`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "1.0\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if importerCalls != 0 {
		t.Fatalf("importer calls = %d, want 0", importerCalls)
	}
}

func TestStockSleepBridgeAliasRetainsOriginTaintWrapper(t *testing.T) {
	var output bytes.Buffer
	runtimeInstance, err := New(WithTaintMode(true), WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtimeInstance.Eval(context.Background(), "stock-bridge-taint.sl", `
setf("&sin", function("&abs"));
$result = sin(taint(1.5707963267948966));
println(iff(-istainted $result, "tainted", "clean"));
`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "clean\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestStockSleepBridgeFamilyInventory(t *testing.T) {
	tests := []struct {
		name       string
		members    []string
		family     sleepBuiltinFunctionFamily
		bridge     string
		unknown    sleepBuiltinDispatch
		dispatchTo string
	}{
		{
			"BasicNumbers", []string{
				"abs", "acos", "asin", "atan", "atan2", "ceil", "cos", "log", "round",
				"sin", "sqrt", "tan", "radians", "degrees", "exp", "floor", "sum",
				"double", "int", "uint", "long", "parseNumber", "formatNumber", "rand", "srand", "not",
			},
			sleepBuiltinFamilyNumbers, "BasicNumbers", sleepBuiltinDispatchNull, "",
		},
		{
			"BasicUtilities", []string{
				"concat", "keys", "size", "push", "pop", "add", "flatten", "clear", "splice",
				"subarray", "sublist", "setRemovalPolicy", "setMissPolicy", "untaint", "taint",
				"putAll", "addAll", "removeAll", "retainAll", "pushl", "popl", "search", "reduce",
				"values", "remove", "setField", "typeOf", "newInstance", "scalar", "exit", "watch",
				"debug", "warn", "profile", "getStackTrace", "checkError", "invoke", "inline",
			},
			sleepBuiltinFamilyUtilities, "BasicUtilities", sleepBuiltinDispatchNull, "",
		},
		{"BasicUtilities_array", []string{"array", "@"}, sleepBuiltinFamilyArray, "BasicUtilities.array", sleepBuiltinDispatchNative, "array"},
		{"BasicUtilities_hash", []string{"hash", "ohash", "ohasha", "%"}, sleepBuiltinFamilyHash, "BasicUtilities.hash", sleepBuiltinDispatchNative, "hash"},
		{"BasicUtilities_map", []string{"map", "filter"}, sleepBuiltinFamilyMap, "BasicUtilities.map", sleepBuiltinDispatchNative, "filter"},
		{"BasicUtilities_cast", []string{"cast", "casti"}, sleepBuiltinFamilyCast, "BasicUtilities.cast", sleepBuiltinDispatchNative, "cast"},
		{"BasicUtilities_use", []string{"use", "include"}, sleepBuiltinFamilyUse, "BasicUtilities.use", sleepBuiltinDispatchNative, "include"},
		{"BasicUtilities_eval", []string{"eval", "expr"}, sleepBuiltinFamilyEval, "BasicUtilities.eval", sleepBuiltinDispatchNative, "expr"},
		{"BasicUtilities_sync", []string{"semaphore", "acquire", "release"}, sleepBuiltinFamilySync, "BasicUtilities.sync", sleepBuiltinDispatchNull, ""},
		{"BasicStrings_charAt", []string{"charAt", "byteAt"}, sleepBuiltinFamilyStringCharAt, "BasicStrings.charAt", sleepBuiltinDispatchNative, "byteAt"},
		{"BasicStrings_substr", []string{"substr", "mid"}, sleepBuiltinFamilyStringSubstring, "BasicStrings.substr", sleepBuiltinDispatchNative, "substr"},
		{"BasicStrings_indexOf", []string{"indexOf", "lindexOf"}, sleepBuiltinFamilyStringIndexOf, "BasicStrings.indexOf", sleepBuiltinDispatchNative, "indexOf"},
		{"BasicStrings_sorters", []string{"sorta", "sortn", "sortd"}, sleepBuiltinFamilyStringSorters, "BasicStrings.sorters", sleepBuiltinDispatchSortIdentity, ""},
		{"BasicStrings_left", []string{"left"}, sleepBuiltinFamilyStringLeft, "BasicStrings.left", sleepBuiltinDispatchNative, "left"},
		{"BasicStrings_right", []string{"right"}, sleepBuiltinFamilyStringRight, "BasicStrings.right", sleepBuiltinDispatchNative, "right"},
		{
			"BasicIO", []string{
				"allocate", "readc", "readObject", "writeObject", "readAsObject", "writeAsObject",
				"sizeof", "wait", "setEncoding", "checksum", "digest",
			},
			sleepBuiltinFamilyIO, "BasicIO", sleepBuiltinDispatchNull, "",
		},
		{"BasicIO_socket", []string{"connect", "listen"}, sleepBuiltinFamilySocket, "BasicIO.socket", sleepBuiltinDispatchNative, "connect"},
		{"BasicIO_consume", []string{"consume", "skip"}, sleepBuiltinFamilyConsume, "BasicIO.consume", sleepBuiltinDispatchNative, "consume"},
		{"BasicIO_println", []string{"println", "printf"}, sleepBuiltinFamilyPrintln, "BasicIO.println", sleepBuiltinDispatchNative, "println"},
		{
			"FileSystemBridge", []string{
				"createNewFile", "deleteFile", "chdir", "cwd", "getCurrentDirectory", "mkdir",
				"rename", "setLastModified", "setReadOnly",
			},
			sleepBuiltinFamilyFileSystem, "FileSystemBridge", sleepBuiltinDispatchNull, "",
		},
		{"FileSystemBridge_listFiles", []string{"ls", "listRoots"}, sleepBuiltinFamilyFileListing, "FileSystemBridge.listFiles", sleepBuiltinDispatchNative, "ls"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, member := range test.members {
				family, ok := sleepBuiltinFamilyForFunction(member)
				if !ok || family != test.family {
					t.Errorf("%s family = %d/%t, want %d/true", member, family, ok, test.family)
				}
			}
			if got := sleepBuiltinFamilyBridgeName(test.family); got != test.bridge {
				t.Fatalf("bridge = %q, want %q", got, test.bridge)
			}
			dispatchName, dispatch := sleepBuiltinFamilyDispatch(test.family, "definitely_unknown_alias")
			if dispatch != test.unknown || dispatchName != test.dispatchTo {
				t.Fatalf("unknown dispatch = %q/%d, want %q/%d", dispatchName, dispatch, test.dispatchTo, test.unknown)
			}
		})
	}
}

func TestStockSleepFunctionCalledKeyOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "stock-bridge-called-key.sl")
	if err := os.WriteFile(path, []byte(sleepStockBridgeCalledKeyProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep stock bridge probe: %v\n%s", err, want)
	}
	if got := []byte(runStockBridgeCalledKeyProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep stock bridge output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func officialSleepDifferentialTools(t *testing.T) (string, string) {
	t.Helper()
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for stock bridge verification")
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
	return jar, java
}

func runStockBridgeCalledKeyProbe(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "stock-bridge-called-key.sl", sleepStockBridgeCalledKeyProbe); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
