package opfor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableFixtureObjectCanonicalCompatibility(t *testing.T) {
	tests := []struct {
		name              string
		fixtureWorkingDir bool
		normalizeIdentity bool
	}{
		{name: "clazz"},
		{name: "objects"},
		{name: "scalar", normalizeIdentity: true},
		{name: "setfield", fixtureWorkingDir: true},
		{name: "setfield3", fixtureWorkingDir: true},
		{name: "testfield", fixtureWorkingDir: true},
		{name: "impfrom", fixtureWorkingDir: true},
		{name: "impfrom2", fixtureWorkingDir: true},
		{name: "impfrom4", fixtureWorkingDir: true},
		{name: "use", fixtureWorkingDir: true},
		{name: "use2", fixtureWorkingDir: true},
		{name: "useerr", fixtureWorkingDir: true},
		{name: "convertds2", fixtureWorkingDir: true},
		{name: "convertds3", fixtureWorkingDir: true, normalizeIdentity: true},
		{name: "convertds4", fixtureWorkingDir: true},
		{name: "multih", fixtureWorkingDir: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", test.name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			wantBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", test.name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(test.name+".sl", programBytes))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			var output bytes.Buffer
			runtime, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if test.fixtureWorkingDir {
				fixtureRoot, err := filepath.Abs(filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := runtime.Invoke(context.Background(), "chdir", String(fixtureRoot)); err != nil {
					t.Fatalf("chdir: %v", err)
				}
			}
			if _, err := runtime.Execute(context.Background(), program); err != nil {
				t.Fatalf("Execute: %v\noutput:\n%s", err, output.String())
			}
			got, want := output.String(), string(wantBytes)
			if test.normalizeIdentity {
				got = normalizePortableJavaIdentity(got)
				want = normalizePortableJavaIdentity(want)
			}
			if got != want {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
			}
		})
	}
}

func TestDynamicClassLiteralResolutionKeepsImportedObjectHostClasses(t *testing.T) {
	var markerInvocation ObjectInvocation
	runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Message == "marker" {
			markerInvocation = invocation
			return String("delegated"), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "imported-class.sl", `
import custom.*;
$class = expr('^Widget');
return @($class, [$class marker], $class isa ^Class, [$class getClass]);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	values := array.Values()
	if got := values[0].String(); got != "class custom.Widget" {
		t.Errorf("class literal = %q", got)
	}
	if got := values[1].String(); got != "delegated" {
		t.Errorf("host result = %q", got)
	}
	if !values[2].Truth() || values[3].String() != "class java.lang.Class" {
		t.Errorf("class type behavior = (%s, %s)", values[2].Describe(), values[3].Describe())
	}
	if markerInvocation.Target.String() != "class custom.Widget" || markerInvocation.Class != "" {
		t.Fatalf("marker invocation = %+v", markerInvocation)
	}
}

func TestUnknownDynamicClassLiteralIsSoftCompileObject(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "unknown-class.sl", `
$result = expr('^missing.Widget');
checkError($problem);
return @($result, [$problem formatErrors], [$problem getClass], $problem);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, _ := result.Array()
	values := array.Values()
	if !values[0].IsNull() {
		t.Errorf("expr result = %s, want null", values[0].Describe())
	}
	if got, want := values[1].String(), "Error: unable to resolve class: missing.Widget at line 0\n       ^missing.Widget\n"; got != want {
		t.Errorf("formatErrors = %q, want %q", got, want)
	}
	if got := values[2].String(); got != "class sleep.error.YourCodeSucksException" {
		t.Errorf("error class = %q", got)
	}
	if got := values[3].String(); got != "YourCodeSucksException: 1 error(s): unable to resolve class: missing.Widget at 0" {
		t.Errorf("error summary = %q", got)
	}
}

func TestPortableFixtureImportDoesNotExecuteOtherJARClasses(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fixtureRoot, err := filepath.Abs(filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(fixtureRoot)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	_, err = runtime.Eval(context.Background(), "unmodeled-import.sl", `import org.hick.tests.FooFunction from: data/test.jar;`)
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Eval error = %v, want UnsupportedError", err)
	}
}

func TestPortableFixtureObjectHostRetainsFirstRefusalAndErrors(t *testing.T) {
	fixtureRoot, err := filepath.Abs(filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("first refusal", func(t *testing.T) {
		sentinel := &struct{ name string }{name: "importer"}
		runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			if invocation.Op == ObjectConstruct && invocation.Class == "org.hick.blah.SqueezeBox" {
				return ObjectValue(sentinel), nil
			}
			return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
		})))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if _, err := runtime.Invoke(context.Background(), "chdir", String(fixtureRoot)); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		result, err := runtime.Eval(context.Background(), "first-refusal.sl", `
import org.hick.blah.SqueezeBox from: data/test.jar;
return [new SqueezeBox];
`)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		object, _ := result.Object()
		if object != sentinel {
			t.Fatalf("constructed object = %#v, want importer sentinel", object)
		}
	})

	t.Run("importer error", func(t *testing.T) {
		sentinel := errors.New("importer constructor failure")
		runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			if invocation.Op == ObjectConstruct && invocation.Class == "org.hick.blah.SqueezeBox" {
				return Null(), sentinel
			}
			return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
		})))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if _, err := runtime.Invoke(context.Background(), "chdir", String(fixtureRoot)); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		_, err = runtime.Eval(context.Background(), "importer-error.sl", `
import org.hick.blah.SqueezeBox from: data/test.jar;
return [new SqueezeBox];
`)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Eval error = %v, want importer sentinel", err)
		}
	})
}

func TestPortableFixtureMethodTrace(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fixtureRoot, err := filepath.Abs(filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(fixtureRoot)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	_, err = runtime.Eval(context.Background(), "fixture-trace.sl", `
import org.hick.blah.SqueezeBox from: data/test.jar;
global('$box');
debug(15);
$box = [new SqueezeBox];
println([$box squeeze]);
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	got := output.String()
	for _, fragment := range []string{
		"Trace: [new org.hick.blah.SqueezeBox] = org.hick.blah.SqueezeBox@",
		" squeeze] = 34 at fixture-trace.sl:",
		"Trace: &println(34) at fixture-trace.sl:",
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("trace missing %q\noutput:\n%s", fragment, got)
		}
	}
}
