package opfor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"testing"
)

func TestSystemPropertiesPortableSnapshot(t *testing.T) {
	t.Setenv("OPFOR_SYSTEM_PROPERTIES_SECRET", "must-not-be-enumerated")

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	function := runtime.functions["systemProperties"]
	if function == nil {
		t.Fatal("systemProperties is not registered")
	}

	value, err := function(context.Background(), Invocation{
		Name:      "systemProperties",
		Runtime:   runtime,
		Arguments: []Argument{{Value: String("ignored")}},
	})
	if err != nil {
		t.Fatalf("systemProperties: %v", err)
	}
	properties, ok := value.Hash()
	if !ok {
		t.Fatalf("systemProperties = %s, want hash", value.Describe())
	}

	lineSeparator := "\n"
	if goruntime.GOOS == "windows" {
		lineSeparator = "\r\n"
	}
	want := map[string]string{
		"file.separator": string(os.PathSeparator),
		"path.separator": string(os.PathListSeparator),
		"line.separator": lineSeparator,
		"os.name":        goruntime.GOOS,
		"os.arch":        goruntime.GOARCH,
		"java.io.tmpdir": os.TempDir(),
		"user.dir":       runtime.defaultFileResolver.BaseDirectory(),
	}
	if home, homeErr := os.UserHomeDir(); homeErr == nil && home != "" {
		want["user.home"] = home
	}
	userNameVariable := "USER"
	if goruntime.GOOS == "windows" {
		userNameVariable = "USERNAME"
	}
	if userName := os.Getenv(userNameVariable); userName != "" {
		want["user.name"] = userName
	}

	for key, wantValue := range want {
		got, exists := properties.Get(key)
		if !exists || got.Kind() != KindString || got.String() != wantValue {
			t.Errorf("systemProperties[%q] = (%s, %v), want string %q", key, got.Describe(), exists, wantValue)
		}
	}
	for _, key := range properties.Keys() {
		got, exists := properties.Get(key)
		if !exists || got.Kind() != KindString {
			t.Errorf("systemProperties[%q] = (%s, %v), want string value", key, got.Describe(), exists)
		}
	}
	for _, absent := range []string{"java.version", "java.vendor", "java.vm.name", "OPFOR_SYSTEM_PROPERTIES_SECRET"} {
		if got, exists := properties.Get(absent); exists || !got.IsNull() {
			t.Errorf("systemProperties[%q] = (%s, %v), want absent/$null", absent, got.Describe(), exists)
		}
	}
	secondValue, err := function(context.Background(), Invocation{Name: "systemProperties", Runtime: runtime})
	if err != nil {
		t.Fatalf("second systemProperties: %v", err)
	}
	second, ok := secondValue.Hash()
	if !ok {
		t.Fatalf("second systemProperties = %s, want hash", secondValue.Describe())
	}
	if !reflect.DeepEqual(properties.Keys(), second.Keys()) {
		t.Fatalf("property traversal is not deterministic: first %q, second %q", properties.Keys(), second.Keys())
	}
	properties.Set("os.name", String("changed snapshot"))
	assertSystemProperty(t, properties, "os.name", goruntime.GOOS)
	if got, exists := second.Get("os.name"); !exists || got.String() != goruntime.GOOS {
		t.Fatalf("second snapshot os.name = (%s, %v), want %q", got.Describe(), exists, goruntime.GOOS)
	}

	keysBeforeMiss := properties.Keys()
	missing, err := properties.HashAt(context.Background(), "java.version")
	if err != nil || !missing.IsNull() {
		t.Fatalf("missing property lookup = (%s, %v), want $null", missing.Describe(), err)
	}
	if !reflect.DeepEqual(properties.Keys(), keysBeforeMiss) {
		t.Fatalf("missing lookup inserted a key: before %q, after %q", keysBeforeMiss, properties.Keys())
	}
}

func TestReadOnlyHashMatchesSleepMapWrapperMutationSemantics(t *testing.T) {
	hash := NewReadOnlyHash(map[string]Value{
		"alpha": String("one"),
		"beta":  String("two"),
	})

	existing := hash.Ensure("alpha")
	existing.Set(String("changed"))
	assertSystemProperty(t, hash, "alpha", "one")

	missing := hash.Ensure("missing")
	missing.Set(String("added"))
	if got, exists := hash.Get("missing"); exists || !got.IsNull() {
		t.Fatalf("assigned missing key = (%s, %v), want absent/$null", got.Describe(), exists)
	}
	if err := hash.SetContext(context.Background(), "beta", String("changed")); err != nil {
		t.Fatalf("SetContext on read-only hash: %v", err)
	}
	assertSystemProperty(t, hash, "beta", "two")
	if hash.Delete("alpha") {
		t.Fatal("Delete reported success on a read-only hash")
	}
	if err := removeHashValues(hash, []Value{String("alpha")}); !errors.Is(err, ErrReadOnlyHash) {
		t.Fatalf("removeHashValues error = %v, want ErrReadOnlyHash", err)
	}
}

func TestSystemPropertiesStructuralRemovalIsSleepWarning(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	const source = `%properties = systemProperties();
$before = size(%properties);
%properties["os.name"] = "changed";
%properties["missing"] = "added";
println(%properties["os.name"]);
println("missing=[" . %properties["missing"] . "] size-stable=" . (size(%properties) == $before));
remove(%properties, "os.name");
println("unreachable");
`
	if _, err := runtime.Eval(context.Background(), "read-only-system-properties.sl", source); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	want := goruntime.GOOS + "\nmissing=[] size-stable=1\nWarning: hash is read-only at read-only-system-properties.sl:7\n"
	if got := output.String(); got != want {
		t.Fatalf("read-only systemProperties output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSystemPropertiesForeachRemovalOnlyMutatesIterationSnapshot(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	const source = `%properties = systemProperties();
$before = size(%properties);
$seen = 0;
foreach $key => $value (%properties)
{
   $seen++;
   remove();
}
return @($before, $seen, size(%properties));
`
	value, err := runtime.Eval(context.Background(), "read-only-system-properties-foreach.sl", source)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("foreach result = %s, want array", value.Describe())
	}
	values := array.Values()
	if len(values) != 3 || values[0].Int32() == 0 || values[1].Int32() != values[0].Int32() || values[2].Int32() != values[0].Int32() {
		t.Fatalf("foreach counts = %v, want equal nonzero before/seen/after", values)
	}
}

func TestSystemPropertiesUserDirectoryTracksRuntimeChdir(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	function := runtime.functions["systemProperties"]
	invoke := func() *Hash {
		t.Helper()
		value, invokeErr := function(context.Background(), Invocation{Name: "systemProperties", Runtime: runtime})
		if invokeErr != nil {
			t.Fatalf("systemProperties: %v", invokeErr)
		}
		hash, ok := value.Hash()
		if !ok {
			t.Fatalf("systemProperties = %s, want hash", value.Describe())
		}
		return hash
	}

	before := invoke()
	initialDirectory := runtime.defaultFileResolver.BaseDirectory()
	target := t.TempDir()
	if _, err := runtime.functions["chdir"](context.Background(), Invocation{
		Name:      "chdir",
		Runtime:   runtime,
		Arguments: []Argument{{Value: String(target)}},
	}); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	after := invoke()

	assertSystemProperty(t, before, "user.dir", initialDirectory)
	assertSystemProperty(t, after, "user.dir", filepath.Clean(target))
	// The returned hash is a detached snapshot, so later runtime changes do not
	// rewrite a value already handed to the script.
	assertSystemProperty(t, before, "user.dir", initialDirectory)
}

func TestSystemPropertiesOmitsUserDirectoryForCustomSourceResolver(t *testing.T) {
	resolver := SourceResolverFunc(func(context.Context, SourceRequest) (Source, error) {
		return NewSource("unused", nil), nil
	})
	runtime, err := New(WithSourceResolver(resolver))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.functions["systemProperties"](context.Background(), Invocation{
		Name:    "systemProperties",
		Runtime: runtime,
	})
	if err != nil {
		t.Fatalf("systemProperties: %v", err)
	}
	properties, ok := value.Hash()
	if !ok {
		t.Fatalf("systemProperties = %s, want hash", value.Describe())
	}
	if got, exists := properties.Get("user.dir"); exists || !got.IsNull() {
		t.Fatalf("custom-resolver user.dir = (%s, %v), want absent/$null", got.Describe(), exists)
	}
}

func TestSystemPropertiesWithFunctionOverride(t *testing.T) {
	override := NewHash()
	override.Set("scope", String("redacted"))
	runtime, err := New(WithFunction("systemProperties", func(context.Context, Invocation) (Value, error) {
		return HashValue(override), nil
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "system-properties-override.sl", `return systemProperties();`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	properties, ok := value.Hash()
	if !ok {
		t.Fatalf("override = %s, want hash", value.Describe())
	}
	assertSystemProperty(t, properties, "scope", "redacted")
	if _, exists := properties.Get("os.name"); exists {
		t.Fatal("portable systemProperties replaced the importer override")
	}
}

// TestSystemPropertiesOfficialJARDifferential checks the source-grounded
// common surface. The reference value is a live read-only MapWrapper while
// OPFOR returns a detached read-only hash, and JVM/Go platform spellings
// differ, so this oracle intentionally compares only hash shape,
// missing/string value kinds, separators, and the initial working directory.
func TestSystemPropertiesOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	const source = `%properties = systemProperties("ignored");
if (-ishash %properties) { println("hash"); } else { println("not-hash"); }
$before = size(%properties);
println(typeOf(%properties["opfor.definitely.missing.property"]));
println(typeOf(%properties["file.separator"]));
println("file=[" . %properties["file.separator"] . "]");
println("path=[" . %properties["path.separator"] . "]");
println("line-size=" . strlen(%properties["line.separator"]));
println("user-dir=[" . %properties["user.dir"] . "]");
%properties["file.separator"] = "changed";
%properties["opfor.definitely.missing.property"] = "added";
println("after=[" . %properties["file.separator"] . "]/missing=[" . %properties["opfor.definitely.missing.property"] . "]");
if (size(%properties) == $before) { println("size-stable"); } else { println("size-changed"); }
$seen = 0;
foreach $key => $value (%properties) { $seen++; remove(); }
if ($seen == size(%properties)) { println("foreach-stable"); } else { println("foreach-changed"); }
`
	var goOutput bytes.Buffer
	runtime, err := New(WithStdout(&goOutput), WithStderr(&goOutput))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Eval(context.Background(), "system-properties-probe.sl", source); err != nil {
		t.Fatalf("pure-Go systemProperties probe: %v\n%s", err, goOutput.String())
	}

	command := officialSleepJavaCommand(java, "-jar", jar, "-e", source)
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep systemProperties probe: %v\n%s", err, javaOutput)
	}
	if !bytes.Equal(goOutput.Bytes(), javaOutput) {
		t.Fatalf("official Sleep systemProperties mismatch\nwant:\n%s\ngot:\n%s", javaOutput, goOutput.Bytes())
	}
}

func assertSystemProperty(t *testing.T, properties *Hash, key, want string) {
	t.Helper()
	got, exists := properties.Get(key)
	if !exists || got.Kind() != KindString || got.String() != want {
		t.Fatalf("systemProperties[%q] = (%s, %v), want string %q", key, got.Describe(), exists, want)
	}
}
