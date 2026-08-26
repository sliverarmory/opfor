package opfor

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestSleepImportDiagnosticsMatchCanonicalGolden(t *testing.T) {
	t.Run("imperror compile resolution", func(t *testing.T) {
		source, golden := readSleepImportFixture(t, "imperror")
		assertImportParses(t, "imperror.sl", source)

		_, err := Compile(NewSource("imperror.sl", source))
		compileError := requireImportCompileError(t, err, diagnosticImportedClassNotFound, messageImportedClassNotFound, 1)
		if got := renderSleepImportDiagnostics(source, compileError.Diagnostics); got != string(golden) {
			t.Fatalf("rendered diagnostic mismatch\nwant:\n%s\ngot:\n%s", golden, got)
		}
	})

	t.Run("impfrom3 load resolution", func(t *testing.T) {
		source, golden := readSleepImportFixture(t, "impfrom3")
		assertImportParses(t, "impfrom3.sl", source)

		program, err := Compile(NewSource("impfrom3.sl", source))
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
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
		_, err = runtime.Execute(context.Background(), program)
		compileError := requireImportCompileError(t, err, diagnosticImportArchiveNotFound, messageImportArchiveNotFound, 5)
		if got := renderSleepImportDiagnostics(source, compileError.Diagnostics); got != string(golden) {
			t.Fatalf("rendered diagnostic mismatch\nwant:\n%s\ngot:\n%s", golden, got)
		}
	})
}

func TestStaticImportValidationUsesReservedSleepCatalog(t *testing.T) {
	if got, want := len(sleep21ClassCatalog), 116; got != want {
		t.Fatalf("Sleep 2.1 class catalog size = %d, want %d", got, want)
	}
	for _, test := range []struct {
		name string
		code string
	}{
		{name: "runtime class", code: `import sleep.runtime.ScriptLoader;`},
		{name: "error class", code: `import sleep.error.YourCodeSucksException;`},
		{name: "sleep wildcard", code: `import sleep.runtime.*;`},
		{name: "java host namespace", code: `import java.awt.Frame;`},
		{name: "custom host namespace", code: `import application.Widget;`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if _, err := CompileString("accepted-import.sl", test.code); err != nil {
				t.Fatalf("Compile: %v", err)
			}
		})
	}

	for _, test := range []struct {
		name string
		code string
		line int
	}{
		{name: "top level", code: `import sleep.runtime.NoSuchClass;`, line: 1},
		{name: "nested block", code: "if (1) {\n  import sleep.runtime.NoSuchClass;\n}", line: 2},
		{name: "nested closure", code: "sub invalid {\n  import sleep.runtime.NoSuchClass;\n}", line: 2},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileString("missing-sleep-class.sl", test.code)
			requireImportCompileError(t, err, diagnosticImportedClassNotFound, messageImportedClassNotFound, test.line)
		})
	}
}

func TestExplicitCustomImportRemainsObjectHostDelegated(t *testing.T) {
	var got ObjectInvocation
	runtime, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Op == ObjectConstruct {
			got = invocation
			return String("delegated"), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := runtime.Eval(context.Background(), "custom-import.sl", `
import application.Widget;
return [new Widget];
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if result.String() != "delegated" || got.Class != "application.Widget" {
		t.Fatalf("result = %s, invocation = %+v", result.Describe(), got)
	}
}

func TestImportFromArchiveResolutionPolicy(t *testing.T) {
	t.Run("declined missing archive is deterministic", func(t *testing.T) {
		calls := 0
		runtime := newImportPolicyRuntime(t, t.TempDir(),
			WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
				calls++
				return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name, Span: invocation.Span}
			})),
		)
		_, err := runtime.Eval(context.Background(), "missing-archive.sl", `import application.Widget from: missing.jar;`)
		requireImportCompileError(t, err, diagnosticImportArchiveNotFound, messageImportArchiveNotFound, 1)
		if calls != 1 {
			t.Fatalf("host calls = %d, want 1", calls)
		}
	})

	t.Run("resolution failure is outside script catch", func(t *testing.T) {
		runtime := newImportPolicyRuntime(t, t.TempDir())
		_, err := runtime.Eval(context.Background(), "uncatchable-import.sl", `
try {
    import application.Widget from: missing.jar;
}
catch $error {
    return "caught";
}
`)
		requireImportCompileError(t, err, diagnosticImportArchiveNotFound, messageImportArchiveNotFound, 3)
	})

	t.Run("missing class entry is deterministic", func(t *testing.T) {
		directory := t.TempDir()
		writeImportArchive(t, filepath.Join(directory, "classes.jar"))
		runtime := newImportPolicyRuntime(t, directory)
		_, err := runtime.Eval(context.Background(), "missing-entry.sl", `import application.Widget from: classes.jar;`)
		requireImportCompileError(t, err, diagnosticImportedClassNotFound, messageImportedClassNotFound, 1)
	})

	t.Run("class entry is not JVM execution", func(t *testing.T) {
		directory := t.TempDir()
		writeImportArchive(t, filepath.Join(directory, "classes.jar"), "application/Widget.class")
		runtime := newImportPolicyRuntime(t, directory)
		_, err := runtime.Eval(context.Background(), "unmodeled-entry.sl", `import application.Widget from: classes.jar;`)
		var unsupported *UnsupportedError
		if !errors.As(err, &unsupported) {
			t.Fatalf("Eval error = %v, want UnsupportedError", err)
		}
	})

	t.Run("host retains first refusal", func(t *testing.T) {
		directory := t.TempDir()
		calls := 0
		runtime := newImportPolicyRuntime(t, directory,
			WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
				if invocation.Name != "import" {
					return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name, Span: invocation.Span}
				}
				calls++
				return Null(), nil
			})),
		)
		result, err := runtime.Eval(context.Background(), "host-import.sl", `
import application.Widget from: classes.jar;
return 7;
`)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		if result.Int64() != 7 || calls != 1 {
			t.Fatalf("result = %s, host calls = %d", result.Describe(), calls)
		}
	})

	t.Run("entry backed object host", func(t *testing.T) {
		directory := t.TempDir()
		writeImportArchive(t, filepath.Join(directory, "classes.jar"), "application/Widget.class")
		var got ObjectInvocation
		runtime := newImportPolicyRuntime(t, directory,
			WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
				if invocation.Op == ObjectConstruct {
					got = invocation
					return String("delegated"), nil
				}
				return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
			})),
		)
		result, err := runtime.Eval(context.Background(), "object-host-import.sl", `
import application.Widget from: classes.jar;
return [new Widget];
`)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		if result.String() != "delegated" || got.Class != "application.Widget" {
			t.Fatalf("result = %s, invocation = %+v", result.Describe(), got)
		}
	})

	t.Run("custom resolver leaves virtual archive to host", func(t *testing.T) {
		resolverCalls := 0
		hostCalls := 0
		runtime, err := New(
			WithSourceResolver(SourceResolverFunc(func(_ context.Context, _ SourceRequest) (Source, error) {
				resolverCalls++
				return Source{}, errors.New("unexpected include resolution")
			})),
			WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
				if invocation.Name != "import" {
					return Null(), &UnsupportedError{Operation: "host function", Name: invocation.Name, Span: invocation.Span}
				}
				hostCalls++
				return Null(), nil
			})),
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if _, err := runtime.Eval(context.Background(), "virtual-import.sl", `import application.Widget from: virtual.jar;`); err != nil {
			t.Fatalf("Eval: %v", err)
		}
		if resolverCalls != 0 || hostCalls != 1 {
			t.Fatalf("resolver calls = %d, host calls = %d", resolverCalls, hostCalls)
		}
	})
}

func readSleepImportFixture(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	root := filepath.Join("testdata", "upstream", "sleep-2.1")
	source, err := os.ReadFile(filepath.Join(root, "programs", name+".sl"))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join(root, "golden", name+".sl"))
	if err != nil {
		t.Fatal(err)
	}
	return source, golden
}

func assertImportParses(t *testing.T, name string, source []byte) {
	t.Helper()
	parsed := parser.Parse(lexer.NewSource(name, source))
	if parsed.HasErrors() {
		t.Fatalf("parser diagnostics = %v, want acceptance", parsed.Diagnostics)
	}
}

func requireImportCompileError(t *testing.T, err error, code, message string, line int) *CompileError {
	t.Helper()
	var compileError *CompileError
	if !errors.As(err, &compileError) || len(compileError.Diagnostics) != 1 {
		t.Fatalf("error = %v, want one CompileError diagnostic", err)
	}
	diagnostic := compileError.Diagnostics[0]
	if diagnostic.Severity != SeverityError || diagnostic.Code != code || diagnostic.Message != message || diagnostic.Span.Start.Line != line {
		t.Fatalf("diagnostic = %+v, want code %s, message %q, line %d", diagnostic, code, message, line)
	}
	return compileError
}

func renderSleepImportDiagnostics(source []byte, diagnostics []Diagnostic) string {
	var output strings.Builder
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(&output, "Error: %s at line %d\n", diagnostic.Message, diagnostic.Span.Start.Line)
		start, end := diagnostic.Span.Start.Offset, diagnostic.Span.End.Offset
		if start < 0 || end < start || end > len(source) {
			continue
		}
		fmt.Fprintf(&output, "       %s\n", strings.TrimSpace(string(source[start:end])))
	}
	return output.String()
}

func newImportPolicyRuntime(t *testing.T, directory string, options ...Option) *Runtime {
	t.Helper()
	runtime, err := New(options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "chdir", String(directory)); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	return runtime
}

func writeImportArchive(t *testing.T, filename string, entries ...string) {
	t.Helper()
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for _, entry := range entries {
		writer, err := archive.Create(entry)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte("not executable bytecode")); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
