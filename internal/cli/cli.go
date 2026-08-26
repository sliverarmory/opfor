// Package cli implements the opfor command-line interface.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sliverarmory/opfor"
	"github.com/spf13/cobra"
)

const (
	defaultVersion = opfor.Version
	maxREPLLine    = 8 << 20
	closeTimeout   = 5 * time.Second
)

// Options configures command input, output, and version reporting. Nil streams
// use the corresponding process standard stream.
type Options struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
}

type scriptRuntime interface {
	Execute(context.Context, *opfor.Program, ...opfor.Value) (opfor.Value, error)
	Eval(context.Context, string, string, ...opfor.Value) (opfor.Value, error)
	Close(context.Context) error
}

type dependencies struct {
	readFile        func(string) ([]byte, error)
	readFileBounded func(string, uint64) ([]byte, error)
	compile         func(opfor.Source) (*opfor.Program, error)
	newRuntime      func(io.Reader, io.Writer, io.Writer, runtimeSettings) (scriptRuntime, error)
}

type runtimeSettings struct {
	taint                bool
	maxInstructions      uint64
	maxCollectionEntries uint64
	maxOutputBytes       uint64
	maxInputBytes        uint64
	maxDecompressedBytes uint64
	maxSourceBytes       uint64
	debugFlags           int32
	sleepClasspath       string
}

func defaultDependencies() dependencies {
	return dependencies{
		readFile:        os.ReadFile,
		readFileBounded: readFileWithSourceLimit,
		compile: func(source opfor.Source) (*opfor.Program, error) {
			return opfor.Compile(source)
		},
		newRuntime: func(stdin io.Reader, stdout, stderr io.Writer, settings runtimeSettings) (scriptRuntime, error) {
			options := []opfor.Option{
				opfor.WithStdin(stdin),
				opfor.WithStdout(stdout),
				opfor.WithStderr(stderr),
				opfor.WithTaintMode(settings.taint),
				opfor.WithDebugFlags(settings.debugFlags),
				opfor.WithSleepClasspath(settings.sleepClasspath),
				opfor.WithLimits(opfor.Limits{
					MaxInstructionsPerExecution:    settings.maxInstructions,
					MaxCollectionEntriesPerRuntime: settings.maxCollectionEntries,
					MaxOutputBytesPerRuntime:       settings.maxOutputBytes,
					MaxInputBytesPerRuntime:        settings.maxInputBytes,
					MaxDecompressedBytesPerRuntime: settings.maxDecompressedBytes,
					MaxSourceBytesPerRuntime:       settings.maxSourceBytes,
				}),
			}
			return opfor.New(options...)
		},
	}
}

// New constructs the opfor Cobra command tree.
func New(options Options) *cobra.Command {
	options = normalizeOptions(options)
	return newCommand(options, defaultDependencies())
}

// Execute runs the command tree with args, writes a concise error on failure,
// and returns a process exit status.
func Execute(ctx context.Context, options Options, args []string) int {
	return executeWithDependencies(ctx, options, args, defaultDependencies())
}

func executeWithDependencies(ctx context.Context, options Options, args []string, dependencies dependencies) int {
	options = normalizeOptions(options)
	command := newCommand(options, dependencies)
	command.SetArgs(append([]string{}, args...))
	if ctx == nil {
		ctx = context.Background()
	}
	if err := command.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(options.Stderr, "opfor: %v\n", err)
		return 1
	}
	return 0
}

func normalizeOptions(options Options) Options {
	if options.Stdin == nil {
		options.Stdin = os.Stdin
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	options.Version = strings.TrimSpace(options.Version)
	if options.Version == "" {
		options.Version = defaultVersion
	}
	return options
}

func newCommand(options Options, dependencies dependencies) *cobra.Command {
	settings := &runtimeSettings{debugFlags: 1}
	command := &cobra.Command{
		Use:   "opfor [script.cna|script.sl|script.sleep] [args...]",
		Short: "Run Sleep and Aggressor Script programs locally",
		Long: "Run Sleep and Aggressor Script programs locally.\n\n" +
			"opfor is an offline interpreter; it does not connect or log in to a Cobalt Strike Team Server.",
		SilenceErrors:    true,
		SilenceUsage:     true,
		TraverseChildren: true,
		Version:          options.Version,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			if err := requireScriptArguments(nil, args); err != nil {
				return err
			}
			return validateScriptPath(args[0])
		},
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return command.Help()
			}
			return runScript(command.Context(), options, dependencies, *settings, args)
		},
	}
	command.SetIn(options.Stdin)
	command.SetOut(options.Stdout)
	command.SetErr(options.Stderr)
	command.SetVersionTemplate("opfor {{.Version}}\n")
	command.CompletionOptions.DisableDefaultCmd = true
	command.Flags().SetInterspersed(false)
	command.PersistentFlags().BoolVar(&settings.taint, "taint", false, "enable Sleep-compatible taint tracking")
	command.PersistentFlags().Uint64Var(&settings.maxInstructions, "max-instructions", 0, "maximum VM instructions per execution (0 disables the limit)")
	command.PersistentFlags().Uint64Var(&settings.maxCollectionEntries, "max-collection-entries", 0, "maximum collection entries admitted per runtime family (0 disables the limit)")
	command.PersistentFlags().Uint64Var(&settings.maxOutputBytes, "max-output-bytes", 0, "maximum output bytes written per runtime family (0 disables the limit)")
	command.PersistentFlags().Uint64Var(&settings.maxInputBytes, "max-input-bytes", 0, "maximum input bytes materialized per runtime family (0 disables the limit)")
	command.PersistentFlags().Uint64Var(&settings.maxDecompressedBytes, "max-decompressed-bytes", 0, "maximum decompressed bytes produced per runtime family (0 disables the limit)")
	command.PersistentFlags().Uint64Var(&settings.maxSourceBytes, "max-source-bytes", 0, "maximum source bytes admitted per runtime family (0 disables the limit)")
	command.PersistentFlags().Int32Var(&settings.debugFlags, "debug", 1, "initial Sleep debug bitmask")
	command.PersistentFlags().StringVar(&settings.sleepClasspath, "classpath", "", "Sleep source/container search path for include and import")

	command.AddCommand(
		newRunCommand(options, dependencies, settings),
		newServeCommand(options, dependencies, settings),
		newCheckCommand(options, dependencies, settings),
		newEvalCommand(options, dependencies, settings),
		newREPLCommand(options, dependencies, settings),
		newVersionCommand(options.Version),
	)
	return command
}

func newRunCommand(options Options, dependencies dependencies, settings *runtimeSettings) *cobra.Command {
	command := &cobra.Command{
		Use:   "run <script> [args...]",
		Short: "Compile and run a script",
		Args:  requireScriptArguments,
		RunE: func(command *cobra.Command, args []string) error {
			return runScript(command.Context(), options, dependencies, *settings, args)
		},
	}
	command.Flags().SetInterspersed(false)
	return command
}

func newCheckCommand(options Options, dependencies dependencies, settings *runtimeSettings) *cobra.Command {
	return &cobra.Command{
		Use:   "check <script>",
		Short: "Compile a script without running it",
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(command, args); err != nil {
				return err
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if _, err := compileFile(dependencies, args[0], options.Stdin, settings.maxSourceBytes); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(options.Stdout, "%s: ok\n", args[0]); err != nil {
				return fmt.Errorf("write check result: %w", err)
			}
			return nil
		},
	}
}

func newEvalCommand(options Options, dependencies dependencies, settings *runtimeSettings) *cobra.Command {
	return &cobra.Command{
		Use:                "eval <code>",
		Short:              "Compile and execute one source string",
		DisableFlagParsing: true,
		Args: func(command *cobra.Command, args []string) error {
			if len(args) == 1 && isHelpArgument(args[0]) {
				return nil
			}
			if len(args) == 2 && args[0] == "--" {
				return nil
			}
			return cobra.ExactArgs(1)(command, args)
		},
		RunE: func(command *cobra.Command, args []string) (resultErr error) {
			if len(args) == 1 && isHelpArgument(args[0]) {
				return command.Help()
			}
			code := args[0]
			if len(args) == 2 {
				code = args[1]
			}
			runtime, err := dependencies.newRuntime(options.Stdin, options.Stdout, options.Stderr, *settings)
			if err != nil {
				return fmt.Errorf("create runtime: %w", err)
			}
			defer func() {
				resultErr = errors.Join(resultErr, closeRuntime(runtime))
			}()
			value, err := runtime.Eval(command.Context(), "<eval>", code)
			if err != nil {
				return fmt.Errorf("eval: %w", err)
			}
			if err := writeResult(options.Stdout, value); err != nil {
				return fmt.Errorf("write eval result: %w", err)
			}
			return nil
		},
	}
}

func newREPLCommand(options Options, dependencies dependencies, settings *runtimeSettings) *cobra.Command {
	return &cobra.Command{
		Use:   "repl",
		Short: "Read and execute source lines from standard input",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) (resultErr error) {
			runtime, err := dependencies.newRuntime(options.Stdin, options.Stdout, options.Stderr, *settings)
			if err != nil {
				return fmt.Errorf("create runtime: %w", err)
			}
			defer func() {
				resultErr = errors.Join(resultErr, closeRuntime(runtime))
			}()
			return runREPL(command.Context(), runtime, options)
		},
	}
}

func newVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the OPFOR version",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(command.OutOrStdout(), "opfor %s\n", version)
			return err
		},
	}
}

func requireScriptArguments(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errors.New("requires a script path")
	}
	return nil
}

func validateScriptPath(path string) error {
	if path == "-" {
		return nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cna", ".sl", ".sleep":
		return nil
	default:
		return fmt.Errorf("script %q must use a .cna, .sl, or .sleep extension", path)
	}
}

func runScript(ctx context.Context, options Options, dependencies dependencies, settings runtimeSettings, args []string) (resultErr error) {
	program, err := compileFile(dependencies, args[0], options.Stdin, settings.maxSourceBytes)
	if err != nil {
		return err
	}
	runtime, err := dependencies.newRuntime(options.Stdin, options.Stdout, options.Stderr, settings)
	if err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, closeRuntime(runtime))
	}()
	values := make([]opfor.Value, len(args)-1)
	for index, argument := range args[1:] {
		values[index] = opfor.String(argument)
	}
	if _, err := runtime.Execute(ctx, program, values...); err != nil {
		return fmt.Errorf("execute %s: %w", args[0], err)
	}
	return nil
}

func compileFile(dependencies dependencies, path string, stdin io.Reader, maxSourceBytes ...uint64) (*opfor.Program, error) {
	name := path
	var data []byte
	var err error
	var sourceLimit uint64
	if len(maxSourceBytes) != 0 {
		sourceLimit = maxSourceBytes[0]
	}
	if path == "-" {
		name = "STDIN"
		data, err = readSourceWithLimit(stdin, sourceLimit)
		if err != nil {
			return nil, fmt.Errorf("read standard input: %w", err)
		}
	} else {
		if sourceLimit != 0 && dependencies.readFileBounded != nil {
			data, err = dependencies.readFileBounded(path, sourceLimit)
		} else {
			data, err = dependencies.readFile(path)
			if err == nil && sourceLimit != 0 && uint64(len(data)) > sourceLimit {
				err = &opfor.LimitError{Resource: "source bytes", Limit: sourceLimit}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	}
	program, err := dependencies.compile(opfor.NewSource(name, data))
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", path, err)
	}
	return program, nil
}

func readFileWithSourceLimit(path string, limit uint64) ([]byte, error) {
	if limit == 0 || limit >= uint64(math.MaxInt64) {
		return os.ReadFile(path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr == nil && info.Mode().IsRegular() && info.Size() > int64(limit) {
		return nil, &opfor.LimitError{Resource: "source bytes", Limit: limit}
	}
	return readSourceWithLimit(file, limit)
}

func readSourceWithLimit(reader io.Reader, limit uint64) ([]byte, error) {
	if limit == 0 || limit >= uint64(math.MaxInt64) {
		return io.ReadAll(reader)
	}
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) > limit {
		return nil, &opfor.LimitError{Resource: "source bytes", Limit: limit}
	}
	return data, nil
}

func runREPL(ctx context.Context, runtime scriptRuntime, options Options) error {
	type inputLine struct {
		number int
		code   string
		err    error
	}
	lines := make(chan inputLine)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(options.Stdin)
		scanner.Buffer(make([]byte, 64*1024), maxREPLLine)
		for number := 1; scanner.Scan(); number++ {
			line := inputLine{number: number, code: scanner.Text()}
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case lines <- inputLine{err: err}:
			case <-ctx.Done():
			}
		}
	}()

	for {
		var line inputLine
		var ok bool
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok = <-lines:
			if !ok {
				return ctx.Err()
			}
		}
		if line.err != nil {
			return fmt.Errorf("read repl input: %w", line.err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		code := line.code
		if strings.TrimSpace(code) == "" {
			continue
		}
		name := fmt.Sprintf("<repl:%d>", line.number)
		value, err := runtime.Eval(ctx, name, code)
		if err != nil {
			if _, writeErr := fmt.Fprintf(options.Stderr, "%s: %v\n", name, err); writeErr != nil {
				return fmt.Errorf("write repl error: %w", writeErr)
			}
			continue
		}
		if err := writeResult(options.Stdout, value); err != nil {
			return fmt.Errorf("write repl result: %w", err)
		}
	}
}

func isHelpArgument(argument string) bool {
	return argument == "-h" || argument == "--help"
}

func closeRuntime(runtime scriptRuntime) error {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	if err := runtime.Close(ctx); err != nil {
		return fmt.Errorf("close runtime: %w", err)
	}
	return nil
}

func writeResult(writer io.Writer, value opfor.Value) error {
	if value.IsNull() {
		return nil
	}
	_, err := fmt.Fprintln(writer, value.String())
	return err
}
