package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/khunzzz/scope-watch/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
