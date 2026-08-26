package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sliverarmory/opfor"
	"github.com/sliverarmory/opfor/internal/cli"
)

// version remains a variable so release builds may replace it with
// -ldflags "-X main.version=<version>". Local source builds report the
// library's source-release version instead of an ambiguous development label.
var version = opfor.Version

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Execute(ctx, cli.Options{Version: version}, args)
}
