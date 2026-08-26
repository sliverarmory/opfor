<p align="center">
  <img src=".github/images/logo.png" alt="OPFOR logo" width="220">
</p>

OPFOR is an independent, pure-Go runtime for the Sleep language and Cobalt
Strike Aggressor Script (`.cna`). The same engine is available as an embeddable
Go package and as the offline `opfor` CLI, so applications can host scripts and
operators can evaluate, validate, and run them without a JVM.

OPFOR owns parsing, compilation, execution, portable Sleep built-ins, script
lifecycle, events, hooks, and callbacks. Embedding applications supply
Cobalt-specific state and effects—such as Team Server transport, Beacon
tasking, payload generation, UI, and data stores—through explicit host
interfaces. OPFOR is not a Cobalt Strike client or Team Server, and the CLI
does not connect or authenticate to one.

## Embed OPFOR

Create a runtime and evaluate a source string:

```go
package main

import (
	"context"
	"log"

	"github.com/sliverarmory/opfor"
)

func main() {
	ctx := context.Background()
	runtime, err := opfor.New()
	if err != nil {
		log.Fatal(err)
	}
	defer runtime.Close(ctx)

	if _, err := runtime.Eval(ctx, "hello.sl", `println("hello from OPFOR");`); err != nil {
		log.Fatal(err)
	}
}
```

This prints `hello from OPFOR`. `Eval` compiles and runs source in one call; use
`CompileString` and `Runtime.Execute` when a program will be reused. `println`
writes to standard output by default. Use `WithStdin`, `WithStdout`, and
`WithStderr` to replace the process streams.

## CLI

Install the `opfor` interpreter with Go 1.24 or later:

```sh
go install github.com/sliverarmory/opfor/cmd/opfor@latest
```

From a source checkout, install that checkout with:

```sh
go install ./cmd/opfor
```

Or build it in the repository root:

```sh
make
```

Then evaluate an expression, validate a script, or run it:

```sh
./opfor eval '2 + 2'
./opfor check examples/01-hello.sl
./opfor run examples/01-hello.sl operator
```

See [`examples/`](examples/) for runnable scripts that use only stock Sleep
syntax and built-ins.

Running `./opfor` without arguments prints the complete command help. The CLI
is an offline interpreter; it does not connect or log in to a Cobalt Strike
Team Server.

## Scope

OPFOR implements Sleep and Aggressor Script, not Java. A small pure-Go
compatibility shim covers the Java-shaped string, collection, file, random,
and UUID behavior needed by supported scripts. Other object behavior can be
provided by the embedding application.

Java serialization is optional compatibility support for scripts that
explicitly use it; it is not required for normal embedding, execution, or
callbacks.

See the [detailed compatibility and embedding reference](docs/README.md) for
the provider catalog, lifecycle contracts, limits, conformance kit, and exact
alpha coverage.

## License

Apache License 2.0. See [LICENSE](LICENSE).
