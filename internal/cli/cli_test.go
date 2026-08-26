package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sliverarmory/opfor"
)

type fakeRuntime struct {
	execute func(context.Context, *opfor.Program, ...opfor.Value) (opfor.Value, error)
	eval    func(context.Context, string, string, ...opfor.Value) (opfor.Value, error)
	close   func(context.Context) error
}

func (f *fakeRuntime) Execute(ctx context.Context, program *opfor.Program, args ...opfor.Value) (opfor.Value, error) {
	if f.execute == nil {
		return opfor.Null(), nil
	}
	return f.execute(ctx, program, args...)
}

func (f *fakeRuntime) Eval(ctx context.Context, name, code string, args ...opfor.Value) (opfor.Value, error) {
	if f.eval == nil {
		return opfor.Null(), nil
	}
	return f.eval(ctx, name, code, args...)
}

func (f *fakeRuntime) Close(ctx context.Context) error {
	if f.close == nil {
		return nil
	}
	return f.close(ctx)
}

func TestRootAndRunExecuteScriptWithTrailingArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "root convenience", args: []string{"task.cna", "one", "--literal-flag"}},
		{name: "explicit run", args: []string{"run", "task.cna", "one", "--literal-flag"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stdin := strings.NewReader("input")
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			options := normalizeOptions(Options{Stdin: stdin, Stdout: stdout, Stderr: stderr})
			program := &opfor.Program{}
			var compiledSource opfor.Source
			var gotArguments []string
			factoryCalls := 0
			closeCalls := 0
			deps := dependencies{
				readFile: func(path string) ([]byte, error) {
					if path != "task.cna" {
						t.Fatalf("read path = %q", path)
					}
					return []byte(`println("hello");`), nil
				},
				compile: func(source opfor.Source) (*opfor.Program, error) {
					compiledSource = source
					return program, nil
				},
				newRuntime: func(gotIn io.Reader, gotOut, gotErr io.Writer, _ runtimeSettings) (scriptRuntime, error) {
					factoryCalls++
					if gotIn != stdin || gotOut != stdout || gotErr != stderr {
						t.Fatal("runtime factory did not receive injected streams")
					}
					return &fakeRuntime{execute: func(_ context.Context, gotProgram *opfor.Program, args ...opfor.Value) (opfor.Value, error) {
						if gotProgram != program {
							t.Fatal("Execute received a different program")
						}
						for _, argument := range args {
							gotArguments = append(gotArguments, argument.String())
						}
						return opfor.Null(), nil
					}, close: func(context.Context) error {
						closeCalls++
						return nil
					}}, nil
				},
			}

			command := newCommand(options, deps)
			command.SetArgs(append([]string{}, test.args...))
			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("ExecuteContext: %v", err)
			}
			if factoryCalls != 1 {
				t.Fatalf("runtime factory calls = %d, want 1", factoryCalls)
			}
			if closeCalls != 1 {
				t.Fatalf("runtime close calls = %d, want 1", closeCalls)
			}
			if compiledSource.Name != "task.cna" || string(compiledSource.Data) != `println("hello");` {
				t.Fatalf("compiled source = %#v", compiledSource)
			}
			if want := []string{"one", "--literal-flag"}; !reflect.DeepEqual(gotArguments, want) {
				t.Fatalf("script arguments = %q, want %q", gotArguments, want)
			}
		})
	}
}

func TestCLIExecutesSelfContainedOfficialRandomStringExample(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "testdata", "upstream", "aggressor-script-examples", "random_string.cna")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := Execute(context.Background(), Options{
		Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr,
	}, []string{"run", path})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 4 || lines[0] != "-------------------------" ||
		lines[1] != "\x034Print Random AlphaNum" || lines[3] != "-------------------------" {
		t.Fatalf("stdout framing = %q", stdout.String())
	}
	const prefix = "Random 20: "
	if !strings.HasPrefix(lines[2], prefix) {
		t.Fatalf("random output line = %q", lines[2])
	}
	generated := strings.TrimPrefix(lines[2], prefix)
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890"
	if len(generated) != 20 {
		t.Fatalf("random output length = %d, want 20", len(generated))
	}
	for index, character := range generated {
		if !strings.ContainsRune(alphabet, character) {
			t.Fatalf("random output byte %d = %q, outside source alphabet", index, character)
		}
	}
}

func TestCLIChecksEveryOfficialAggressorExample(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "upstream", "aggressor-script-examples", "*.cna"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 18 {
		t.Fatalf("official example count = %d, want 18", len(paths))
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			code := Execute(context.Background(), Options{
				Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr,
			}, []string{"check", path})
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if got, want := stdout.String(), path+": ok\n"; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
			if got := stderr.String(); got != "" {
				t.Fatalf("stderr = %q, want empty", got)
			}
		})
	}
}

func TestDefaultDependenciesExecuteRealProgramAndARGV(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "arguments.cna")
	if err := os.WriteFile(path, []byte("println(@ARGV);"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := executeWithDependencies(
		context.Background(),
		Options{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr},
		[]string{path, "alpha", "--literal-flag"},
		defaultDependencies(),
	)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("status/stderr = %d/%q", status, stderr.String())
	}
	if got, want := stdout.String(), "@('alpha', '--literal-flag')\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRuntimeFlagsConfigureTaintDebugAndInstructionLimit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tainted.cna")
	if err := os.WriteFile(path, []byte("println(debug()); println(iff(-istainted @ARGV[0], 1, 0));"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := executeWithDependencies(
		context.Background(),
		Options{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr},
		[]string{"--taint", "--debug", "3", "--max-instructions", "1000", path, "external", "--taint"},
		defaultDependencies(),
	)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("status/stderr = %d/%q", status, stderr.String())
	}
	if got, want := stdout.String(), "3\n1\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestClasspathFlagConfiguresDefaultSourceResolver(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	libraryDirectory := filepath.Join(root, "sleep-libs")
	if err := os.Mkdir(libraryDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	archiveFile, err := os.Create(filepath.Join(libraryDirectory, "hidden.jar"))
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(archiveFile)
	entry, err := archive.Create("pkg/from-classpath.sl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(`$classpath_value = 'found';`)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "classpath.cna")
	if err := os.WriteFile(path, []byte(`include("hidden.jar", "pkg/from-classpath.sl"); println($classpath_value);`), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := executeWithDependencies(
		context.Background(),
		Options{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr},
		[]string{"--classpath", libraryDirectory, path},
		defaultDependencies(),
	)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("status/stderr = %d/%q", status, stderr.String())
	}
	if got, want := stdout.String(), "found\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestInstructionLimitFailureIsConcise(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "infinite.sl")
	if err := os.WriteFile(path, []byte("while (1) { $x++; }"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	status := executeWithDependencies(
		context.Background(),
		Options{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: stderr},
		[]string{"--max-instructions=25", path},
		defaultDependencies(),
	)
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if got := stderr.String(); !strings.Contains(got, opfor.ErrInstructionLimit.Error()) || strings.Contains(got, "Usage:") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestAggregateResourceLimitFlagsAreEnforced(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		flag       string
		limit      uint64
		wantOutput string
		resource   string
	}{
		{name: "source", source: `return 1;`, flag: "--max-source-bytes", limit: 8, resource: "source bytes"},
		{name: "collections", source: `return @(1, 2);`, flag: "--max-collection-entries", limit: 1, resource: "collection entries"},
		{name: "output", source: `println("hello");`, flag: "--max-output-bytes", limit: 3, wantOutput: "hel", resource: "output bytes"},
		{name: "input", source: `$h = allocate(); writeb($h, "abcd"); closef($h); return readb($h, -1);`, flag: "--max-input-bytes", limit: 3, resource: "input bytes"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "limited.sl")
			if err := os.WriteFile(path, []byte(test.source), 0o600); err != nil {
				t.Fatal(err)
			}
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			status := executeWithDependencies(
				context.Background(),
				Options{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr},
				[]string{fmt.Sprintf("%s=%d", test.flag, test.limit), path},
				defaultDependencies(),
			)
			if status != 1 {
				t.Fatalf("status = %d, want 1 (stderr %q)", status, stderr.String())
			}
			if got := stdout.String(); got != test.wantOutput {
				t.Fatalf("stdout = %q, want %q", got, test.wantOutput)
			}
			if got := stderr.String(); !strings.Contains(got, test.resource+" limit exceeded") || strings.Contains(got, "Usage:") {
				t.Fatalf("stderr = %q", got)
			}
		})
	}
}

func TestCheckCompilesWithoutRuntime(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	options := normalizeOptions(Options{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: &bytes.Buffer{}})
	compileCalls := 0
	deps := dependencies{
		readFile: func(path string) ([]byte, error) {
			return []byte("yield;"), nil
		},
		compile: func(source opfor.Source) (*opfor.Program, error) {
			compileCalls++
			if source.Name != "syntax.sl" || string(source.Data) != "yield;" {
				t.Fatalf("source = %#v", source)
			}
			return &opfor.Program{}, nil
		},
		newRuntime: func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error) {
			t.Fatal("check unexpectedly created a runtime")
			return nil, nil
		},
	}
	command := newCommand(options, deps)
	command.SetArgs([]string{"check", "syntax.sl"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}
	if compileCalls != 1 {
		t.Fatalf("compile calls = %d, want 1", compileCalls)
	}
	if got := stdout.String(); got != "syntax.sl: ok\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestExplicitFileCommandsAcceptExtensionlessPaths(t *testing.T) {
	t.Parallel()

	tests := [][]string{
		{"run", "script", "arg"},
		{"check", "script"},
	}
	for _, args := range tests {
		args := args
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()
			options := normalizeOptions(Options{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
			deps := dependencies{
				readFile: func(path string) ([]byte, error) {
					if path != "script" {
						t.Fatalf("read path = %q", path)
					}
					return []byte("$null;"), nil
				},
				compile: func(opfor.Source) (*opfor.Program, error) { return &opfor.Program{}, nil },
				newRuntime: func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error) {
					return &fakeRuntime{}, nil
				},
			}
			command := newCommand(options, deps)
			command.SetArgs(args)
			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("ExecuteContext: %v", err)
			}
		})
	}
}

func TestDashScriptPathReadsStandardInput(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"-"}, {"run", "-"}, {"check", "-"}} {
		args := args
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			t.Parallel()
			stdin := strings.NewReader(`println("stdin");`)
			options := normalizeOptions(Options{Stdin: stdin, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
			compileCalls := 0
			deps := dependencies{
				readFile: func(string) ([]byte, error) {
					t.Fatal("dash script path unexpectedly read a filesystem path")
					return nil, nil
				},
				compile: func(source opfor.Source) (*opfor.Program, error) {
					compileCalls++
					if source.Name != "STDIN" || string(source.Data) != `println("stdin");` {
						t.Fatalf("source = %#v", source)
					}
					return &opfor.Program{}, nil
				},
				newRuntime: func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error) {
					return &fakeRuntime{}, nil
				},
			}
			command := newCommand(options, deps)
			command.SetArgs(args)
			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("ExecuteContext: %v", err)
			}
			if compileCalls != 1 {
				t.Fatalf("compile calls = %d, want 1", compileCalls)
			}
		})
	}
}

func TestEvalExecutesCodeAndPrintsNonNullResult(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	options := normalizeOptions(Options{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: &bytes.Buffer{}})
	deps := inertDependencies()
	closeCalls := 0
	deps.newRuntime = func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error) {
		return &fakeRuntime{eval: func(_ context.Context, name, code string, args ...opfor.Value) (opfor.Value, error) {
			if name != "<eval>" || code != "2 + 2" || len(args) != 0 {
				t.Fatalf("Eval(%q, %q, %v)", name, code, args)
			}
			return opfor.Int(4), nil
		}, close: func(context.Context) error {
			closeCalls++
			return nil
		}}, nil
	}
	command := newCommand(options, deps)
	command.SetArgs([]string{"eval", "2 + 2"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got := stdout.String(); got != "4\n" {
		t.Fatalf("stdout = %q", got)
	}
	if closeCalls != 1 {
		t.Fatalf("runtime close calls = %d, want 1", closeCalls)
	}
}

func TestEvalHonorsPersistentRuntimeFlags(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"--taint", "--debug", "3", "--max-instructions", "99", "--max-collection-entries", "101", "--max-output-bytes", "102", "--max-input-bytes", "103", "--max-decompressed-bytes", "104", "--max-source-bytes", "105", "--classpath", "lib-one;lib-two", "eval", "1"},
		{"--taint=true", "--debug=3", "--max-instructions=99", "--max-collection-entries=101", "--max-output-bytes=102", "--max-input-bytes=103", "--max-decompressed-bytes=104", "--max-source-bytes=105", "--classpath=lib-one;lib-two", "eval", "1"},
	} {
		args := args
		t.Run(strings.Join(args[:len(args)-2], "/"), func(t *testing.T) {
			t.Parallel()
			options := normalizeOptions(Options{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
			deps := inertDependencies()
			deps.newRuntime = func(_ io.Reader, _ io.Writer, _ io.Writer, settings runtimeSettings) (scriptRuntime, error) {
				if !settings.taint || settings.debugFlags != 3 || settings.maxInstructions != 99 ||
					settings.maxCollectionEntries != 101 || settings.maxOutputBytes != 102 ||
					settings.maxInputBytes != 103 || settings.maxDecompressedBytes != 104 || settings.maxSourceBytes != 105 ||
					settings.sleepClasspath != "lib-one;lib-two" {
					t.Fatalf("runtime settings = %+v", settings)
				}
				return &fakeRuntime{eval: func(_ context.Context, _ string, code string, _ ...opfor.Value) (opfor.Value, error) {
					if code != "1" {
						t.Fatalf("eval code = %q", code)
					}
					return opfor.Null(), nil
				}}, nil
			}
			command := newCommand(options, deps)
			command.SetArgs(args)
			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("eval with persistent flags: %v", err)
			}
		})
	}
}

func TestEvalAcceptsLeadingHyphenSource(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"eval", "-1 + 2"}, {"eval", "--", "-1 + 2"}} {
		args := args
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			t.Parallel()
			options := normalizeOptions(Options{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
			deps := inertDependencies()
			deps.newRuntime = func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error) {
				return &fakeRuntime{eval: func(_ context.Context, _ string, code string, _ ...opfor.Value) (opfor.Value, error) {
					if code != "-1 + 2" {
						t.Fatalf("eval code = %q", code)
					}
					return opfor.Int(1), nil
				}}, nil
			}
			command := newCommand(options, deps)
			command.SetArgs(args)
			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("eval leading hyphen: %v", err)
			}
		})
	}
}

func TestREPLUsesOneRuntimeContinuesAfterErrorsAndSkipsBlankLines(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("1 + 1\n\nbad\n\"done\"\n")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	options := normalizeOptions(Options{Stdin: stdin, Stdout: stdout, Stderr: stderr})
	var names, codes []string
	factoryCalls := 0
	closeCalls := 0
	deps := inertDependencies()
	deps.newRuntime = func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error) {
		factoryCalls++
		return &fakeRuntime{eval: func(_ context.Context, name, code string, _ ...opfor.Value) (opfor.Value, error) {
			names = append(names, name)
			codes = append(codes, code)
			switch code {
			case "1 + 1":
				return opfor.Int(2), nil
			case "bad":
				return opfor.Null(), errors.New("bad expression")
			default:
				return opfor.String("done"), nil
			}
		}, close: func(context.Context) error {
			closeCalls++
			return nil
		}}, nil
	}
	command := newCommand(options, deps)
	command.SetArgs([]string{"repl"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("repl: %v", err)
	}
	if factoryCalls != 1 {
		t.Fatalf("runtime factory calls = %d, want 1", factoryCalls)
	}
	if closeCalls != 1 {
		t.Fatalf("runtime close calls = %d, want 1", closeCalls)
	}
	if want := []string{"<repl:1>", "<repl:3>", "<repl:4>"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %q, want %q", names, want)
	}
	if want := []string{"1 + 1", "bad", `"done"`}; !reflect.DeepEqual(codes, want) {
		t.Fatalf("codes = %q, want %q", codes, want)
	}
	if got := stdout.String(); got != "2\ndone\n" {
		t.Fatalf("stdout = %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "<repl:3>: bad expression") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestREPLCancellationDoesNotWaitForInput(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	options := normalizeOptions(Options{Stdin: reader, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	done := make(chan error, 1)
	go func() {
		done <- runREPL(ctx, &fakeRuntime{}, options)
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runREPL error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runREPL did not return after context cancellation")
	}
}

func TestVersionAndHelpDoNotCreateRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantOutput string
	}{
		{name: "version command", args: []string{"version"}, wantOutput: "opfor v0.1.0\n"},
		{name: "version flag", args: []string{"--version"}, wantOutput: "opfor v0.1.0\n"},
		{name: "root help", args: nil, wantOutput: "Run Sleep and Aggressor Script programs"},
		{name: "eval help", args: []string{"eval", "--help"}, wantOutput: "Compile and execute one source string"},
		{name: "serve help", args: []string{"serve", "--help"}, wantOutput: "newline-delimited JSON requests"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stdout := &bytes.Buffer{}
			options := normalizeOptions(Options{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: &bytes.Buffer{}, Version: " v0.1.0 "})
			deps := inertDependencies()
			deps.newRuntime = func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error) {
				t.Fatal("informational command unexpectedly created a runtime")
				return nil, nil
			}
			command := newCommand(options, deps)
			command.SetArgs(append([]string{}, test.args...))
			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("ExecuteContext: %v", err)
			}
			if got := stdout.String(); !strings.Contains(got, test.wantOutput) {
				t.Fatalf("stdout = %q, want substring %q", got, test.wantOutput)
			}
		})
	}
}

func TestErrorsAreConciseAndDoNotPrintUsage(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	code := executeWithDependencies(context.Background(), Options{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: stderr}, []string{"not-a-script"}, inertDependencies())
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if got := stderr.String(); !strings.Contains(got, `opfor: script "not-a-script" must use`) || strings.Contains(got, "Usage:") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestReadAndCompileErrorsIncludeScriptContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		deps dependencies
		want string
	}{
		{
			name: "read",
			deps: dependencies{
				readFile: func(string) ([]byte, error) { return nil, errors.New("permission denied") },
				compile:  func(opfor.Source) (*opfor.Program, error) { return &opfor.Program{}, nil },
				newRuntime: func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error) {
					return &fakeRuntime{}, nil
				},
			},
			want: "read broken.cna: permission denied",
		},
		{
			name: "compile",
			deps: dependencies{
				readFile: func(string) ([]byte, error) { return []byte("bad"), nil },
				compile:  func(opfor.Source) (*opfor.Program, error) { return nil, errors.New("syntax error") },
				newRuntime: func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error) {
					return &fakeRuntime{}, nil
				},
			},
			want: "compile broken.cna: syntax error",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := normalizeOptions(Options{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
			command := newCommand(options, test.deps)
			command.SetArgs([]string{"run", "broken.cna"})
			err := command.ExecuteContext(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func inertDependencies() dependencies {
	return dependencies{
		readFile: func(string) ([]byte, error) { return nil, errors.New("unexpected read") },
		compile:  func(opfor.Source) (*opfor.Program, error) { return &opfor.Program{}, nil },
		newRuntime: func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error) {
			return &fakeRuntime{}, nil
		},
	}
}
