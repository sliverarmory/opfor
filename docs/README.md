# OPFOR

OPFOR is a pure-Go, embeddable implementation of the Sleep language and an
Aggressor Script (`.cna`) runtime. The same interpreter is available as the
`github.com/sliverarmory/opfor` Go package and as the `opfor` command-line
program.

OPFOR owns parsing, compilation, evaluation, portable Sleep behavior, script
lifecycle, and guarded callbacks. An embedding application owns the state and
effects outside the interpreter: application objects, virtual source stores,
custom functions, and any Cobalt Strike-compatible services it chooses to
provide.

The project targets Go 1.24 or newer and remains in the `v0.x` alpha series.
Exported APIs may still change when compatibility evidence requires a better
contract.

## `v0.1.0-alpha.2` release gates

Alpha.2 focuses on official Sleep 2.1 compatibility and interpreter
performance. Its checkable additions are:

- one SHA-256-authenticated official-JAR helper for every differential test,
  with `OPFOR_REQUIRE_SLEEP_JAR=1` hard-failure mode and a required alpha-tag
  release job;
- `BenchmarkSleep*` workloads for arithmetic, Sleep/native calls, arrays,
  foreach, strings, regex, literals/closures, runtime lifecycle, and the pinned
  upstream corpus, plus `make bench-sleep` and a required CI smoke run;
- zero-allocation direct `Array.Len`, `Get`, and `Set`, and a root-only linear
  append path which retains sublist invalidation and resource accounting;
- disabled-taint, unmetered-VM, synchronous closure/native lease, and simple
  evaluator fast paths with cancellation, unload, and limited-mode regressions;
- compile-time numeric/string literal and closure-function templates, and a
  concurrency-safe 128-entry per-runtime regex LRU;
- explicit cross-runtime ScriptLoader compilation sharing through
  `NewScriptLoaderCache` and `WithScriptLoaderCache`; and
- official-JAR regressions for saved closure control contexts and non-empty
  zero-width regex matches between UTF-16 surrogate halves.

Against baseline commit
`7b65175040b736c95cbf53cbfe7a468f803a9457`, sequential count-10 measurements
on an Apple M5 Max improved the unmetered arithmetic benchmark by 32.9%, Sleep
function calls by 53.1%, and native calls by 50.5%. Function calls used about
48% fewer bytes and 59% fewer allocations; native calls used about 72% fewer
bytes and 68% fewer allocations. These are local comparison results, not
portable timing promises. Re-run with `BENCHTIME=3s make bench-sleep` before
comparing machines or commits.

To compare OPFOR directly with the official Sleep 2.1 Java interpreter, point
`OPFOR_SLEEP_JAR` at the pinned upstream JAR and run:

```console
make bench-sleep-compare OPFOR_SLEEP_JAR=/path/to/sleep-2.1.jar
```

The comparison runs seven identical Sleep workloads through both interpreters,
checks that both return the expected result, and reports median compilation and
execution nanoseconds per operation. Both interpreters compile and execute
in-process after warmup; Java process startup and helper compilation are outside
the measured intervals. The command verifies the same official-JAR SHA-256 used
by the compatibility suite. Pass
`SLEEP_COMPARE_FLAGS='-samples 30 -warmup 50'` to `make` when collecting longer
measurements; the runner also supports `-execute-iterations` and
`-compile-iterations`.

The checked-in [alpha.2 comparison](benchmarks/opfor-vs-sleep-2.1-alpha.2.md)
records the default run on an Apple M5 Max. It shows OPFOR compiling every
workload faster, while the warmed Java interpreter remains faster for every
execution workload; string concatenation, array append, and regex matching have
the largest measured execution gaps.

OPFOR is not a JVM and `Host` is not a sandbox. Portable file, process, socket,
and console functions perform real local effects. Java-style object syntax is
an importer boundary implemented with `ObjectHost`, with a deliberately small
pure-Go compatibility layer for source-backed behavior.

## Install

Add the library to a Go module:

```console
go get github.com/sliverarmory/opfor
```

Install the CLI:

```console
go install github.com/sliverarmory/opfor/cmd/opfor@latest
```

## Configure a runtime

`New` accepts closed functional options and returns an isolated runtime. The
following example makes the stream policy, debug mask, resource quotas, and
initial script state explicit:

```go
func newRuntime(
	input io.Reader,
	output io.Writer,
	diagnostics io.Writer,
	myClient any,
) (*opfor.Runtime, error) {
	roles := opfor.NewArray(
		opfor.String("operator"),
		opfor.String("analyst"),
	)
	config := opfor.NewHash()
	config.Set("mode", opfor.String("offline"))

	return opfor.New(
		opfor.WithStdin(input),
		opfor.WithStdout(output),
		opfor.WithStderr(diagnostics),
		opfor.WithDebugFlags(1),
		opfor.WithLimits(opfor.Limits{
			MaxInstructionsPerExecution:    1_000_000,
			MaxCollectionEntriesPerRuntime: 250_000,
			MaxOutputBytesPerRuntime:       16 << 20,
			MaxInputBytesPerRuntime:        16 << 20,
			MaxDecompressedBytesPerRuntime: 64 << 20,
			MaxSourceBytesPerRuntime:       16 << 20,
		}),
		opfor.WithInitialGlobals(map[string]opfor.Value{
			"client":  opfor.ObjectValue(myClient),
			"@roles":  opfor.ArrayValue(roles),
			"%config": opfor.HashValue(config),
		}),
	)
}
```

The stream defaults are `os.Stdin`, `os.Stdout`, and `os.Stderr`. Configured
streams are borrowed; OPFOR does not close `WithStdin`'s reader. Debug flag `1`
is already the default, so the example restates it for clarity.

The limits above are an example policy, not defaults. A zero field is
unlimited. Instruction accounting resets for each top-level execution or
callback. Collection, input, output, decompression, and admitted-source counters
are monotonic and shared across the root runtime, forks, and source-backed
`ScriptLoader` children.

An initial-global name without a sigil becomes a scalar (`client` becomes
`$client`); names beginning with `$`, `@`, or `%` retain their sigil. The input
map is copied. Scalar values are copied, while arrays, hashes, functions, and
objects preserve identity. Use `ScriptLifecycleObserver.ScriptLoaded` to create
fresh per-script containers when shared identity is not wanted.
`$__SCRIPT__`, `$__SCRIPT_NAME__`, and `@ARGV` are runtime-owned and cannot be
initial globals.

## Compile and execute

Use runtime-scoped compilation when the runtime has custom environment syntax:

```go
ctx := context.Background()

runtime, err := opfor.New()
if err != nil {
	return err
}
defer runtime.Close(ctx)

program, err := runtime.CompileString("startup.sl", `
	println("hello from OPFOR");
	return 42;
`)
if err != nil {
	return err
}

script, err := runtime.Load(ctx, program)
if err != nil {
	return err
}
defer script.Unload(ctx)

fmt.Println(script.Result().Int32())
```

`Program` values are immutable and reusable. `Runtime.Load` creates an
independently unloadable `Script`; registrations and retained callbacks belong
to that script generation. `Runtime.Eval` is convenient for persistent
incremental evaluation when independent unload boundaries are not needed.

`Runtime.Close` cancels active work, unloads scripts, revokes callbacks, and
releases runtime-owned resources. Always provide a bounded or cancelable
context when host callbacks or borrowed I/O may block.

Standalone `Compile` and `CompileString` are also available. Use
`WithStrictSyntax`, `WithCompatibilityWarnings`, and
`WithCompileEnvironment` when configuring compilation without a runtime.

## Generic extension points

These APIs are independent of any particular Aggressor function family:

| API | Use it for |
| --- | --- |
| `WithFunction` / `RegisterFunction` | Exact-name Go-native functions. An importer function overrides the portable default with the same name. |
| `WithHost` | Fallback handling for otherwise unresolved functions and predicates. |
| `WithObjectHost` | Java-style object construction, methods, properties, and type checks. Returning `UnsupportedError` allows OPFOR's portable fallback to try the operation. |
| `Iterator` / `MutableIterator` | Let importer-owned object values participate in `foreach` and iterator-consuming functions. |
| `WithEnvironment` | Add ordinary, filter, or predicate declaration syntax. Compile through that runtime so the parser sees the environment. |
| `WithBindingObserver` | Observe registration and removal of subroutines, events, aliases, hooks, menus, commands, and key bindings. |
| `WithInitialGlobals` | Install eager importer-owned values before top-level execution. |
| `WithVariableProvider` | Make importer storage authoritative for lazy globals, locals, and closure/fork containers. |
| `WithScriptLifecycleObserver` | Receive paired script load and unload notifications. |
| `WithSourceResolver` | Resolve `include` from files, archives, embedded assets, databases, or virtual modules. |
| `WithSleepClasspath` | Configure the built-in `FileSourceResolver`; it is an alternative to `WithSourceResolver`. |
| `WithLoadableProvider` | Map Sleep `use()` identities to pure-Go, script-local bridges without executing Java bytecode. |
| `WithClock` | Supply deterministic wall-clock behavior to portable date/time functions. |
| `WithIncludeCyclePolicy` | Select safe include-cycle rejection or reference-compatible recursion. |
| `NewScriptLoaderCache` / `WithScriptLoaderCache` | Explicitly share immutable ScriptLoader compilation results across selected runtimes. `setGlobalCache(true)` remains unsupported without this capability. |
| `WithTaintMode`, `WithTaintFunction`, `WithTaintPolicy` | Enable and extend Sleep-compatible data-flow taint behavior. |

### Callback ownership

`NativeFunc`, `Host`, `ObjectHost`, observers, and providers are synchronous
interfaces, but independent script executions may call the same implementation
concurrently. Implementations must synchronize their own state, observe the
supplied context, and not retain that context after returning.

`Invocation.Arg` resolves one argument; `Invocation.Values` returns a detached
top-level slice. Compound values and objects may still share importer-owned
identity. `Argument.Set` mutates only a reference-capable argument.

For work that outlives the current call, retain a guarded capability with
`Invocation.Callback` or `Invocation.RetainCallback`.
Invoke it later with a new caller-owned context. Guarded callbacks are bound to
their originating generation and fail after unload or runtime close. Retaining
raw `*Runtime`, `*Cell`, or shared compound values is trusted in-process access,
not a lifecycle-revoked capability.

## Values

`opfor.String` converts valid UTF-8 text to Java UTF-16 code units.
`opfor.BinaryString` marks byte-oriented input and preserves octets that are not
valid UTF-8. `Value.Bytes` returns the reversible host spelling, and
`Value.IsBinaryString` reports binary provenance.

Arrays and hashes are mutable reference types. Use `Array.Values` or
`Hash.KeyValues` for detached top-level snapshots, and construct a fresh graph
before returning application state that scripts must not mutate.

## Aggressor Script integrations

Aggressor-specific tasking, query, UI, catalog, and retained-callback APIs are
documented separately so the generic embedding path stays readable:

- [Aggressor Script extension index](aggressor-script-extensions.md)
- [Grouped implementation guides](aggressor/README.md)
- [Shared provider and callback contract](aggressor/provider-contract.md)

Importers install only the groups they own. A small offline host, a test double,
and a complete Cobalt-compatible host are different adapters around the same
interpreter; implementing every provider is not required.

## CLI

Run a script directly:

```console
opfor script.cna argument-one argument-two
```

The explicit commands are:

```text
opfor run <script> [args...]   compile and execute a script
opfor check <script>          compile without executing
opfor eval <code>             evaluate one source string
opfor repl                    use a persistent session with an interactive prompt
opfor serve [script] [args...] manage scripts through a JSON-lines adapter
opfor version                 print build version information
```

Use `-` as the script path to read source from standard input. Script arguments
populate `@ARGV`. Runtime flags such as `--debug`, `--taint`,
`--max-instructions`, the other resource quotas, and `--classpath` must precede
the command or direct script path.

When standard input and output are terminals, `opfor repl` displays a colored
`opfor > ` prompt, and REPL evaluation diagnostics written to a terminal are
red. Redirected sessions remain prompt-free, and redirected standard error
stays plain text, so pipelines keep their existing line-oriented output.

The CLI is an offline interpreter. It does not implement an external
Aggressor/Cobalt connection, authentication, session transport, or UI.

## Testing an adapter

The public [`conformance`](../conformance) package checks reusable Host,
ObjectHost, `use()`, lifecycle, callback-revocation, and authoritative-error
contracts with inert reference adapters.

Aggressor adapters should additionally use
`opfor.DefaultAggressorFunctionContracts()` and `aggressor.Catalog()` in tests
so a newly supported or reclassified function cannot silently bypass their
routing policy. The grouped Aggressor guides include provider-specific test
matrices.

## Compatibility and safety

Successful parsing or catalog presence is not a claim that a host-owned effect
is implemented. OPFOR uses pinned Sleep and Aggressor corpora as compatibility
evidence and returns explicit unsupported-operation errors rather than
inventing behavior for an unimplemented boundary.

Resource quotas and taint tracking are compatibility and operational controls,
not a security sandbox. Importers must bound their callbacks, object bridges,
retained values, operating-system effects, and external transports.

## Independence and license

OPFOR is an independent compatibility implementation. Aggressor Script and
Cobalt Strike are used only to describe compatibility; this project is not
affiliated with or endorsed by their authors or maintainers.

OPFOR is licensed under the Apache License 2.0. Vendored fixtures and adapted
third-party code retain their own notices and license texts.
