// Package app wires the CLI, collection pipeline and process lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/khunzzz/scope-watch/internal/notify/telegram"
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func (e *exitError) Unwrap() error { return e.err }

func invalid(err error) error { return &exitError{code: 2, err: err} }

type application struct {
	settings    Settings
	client      *http.Client
	runner      *Runner
	log         *slog.Logger
	verbose     bool
	out, stderr io.Writer
}

// Execute returns a process exit code; signal handling belongs to main.
func Execute(ctx context.Context, args []string, out, stderr io.Writer) int {
	app := &application{out: out, stderr: stderr}
	root := newCommand(app)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if app.client != nil {
		app.client.CloseIdleConnections()
	}
	if err == nil || errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return 0
	}
	fmt.Fprintln(stderr, telegram.Redact(err.Error(), app.settings.Token))
	var exit *exitError
	if errors.As(err, &exit) {
		return exit.code
	}
	// Cobra errors occur before command execution; runtime failures are wrapped below.
	return 2
}

func newCommand(app *application) *cobra.Command {
	root := &cobra.Command{
		Use:           "scope-watch",
		Short:         "Мониторинг публичных bug bounty программ",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE:          func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	root.SetOut(app.out)
	root.SetErr(app.stderr)
	root.PersistentFlags().BoolVarP(&app.verbose, "verbose", "v", false, "подробные логи")
	root.AddCommand(
		newRunCommand(app),
		newLoopCommand(app),
		newSeedCommand(app),
		newStatusCommand(app),
		newHistoryCommand(app),
		newLinksCommand(app),
		newExportCommand(app),
		newNotifyCommand(app),
	)
	root.AddCommand(&cobra.Command{
		Use:   "healthcheck",
		Short: "Проверить heartbeat контейнера",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := healthcheck(); err != nil {
				return &exitError{code: 1, err: err}
			}
			return nil
		},
	})
	return root
}

func (app *application) prepare() error {
	settings, err := FromEnv()
	if err != nil {
		return invalid(err)
	}
	app.settings = settings
	level := slog.LevelInfo
	if app.verbose {
		level = slog.LevelDebug
	}
	writer := telegram.RedactingWriter{Writer: app.stderr, Token: settings.Token}
	app.log = slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level}))
	app.client = newPlatformHTTPClient(settings.RequestDelay)
	app.runner, err = newRunner(settings, newAdapters(app.client), app.log)
	if err != nil {
		return invalid(err)
	}
	return nil
}

func (app *application) command(use, short string, run func(*cobra.Command) error) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Short:   short,
		Args:    cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error { return app.prepare() },
		RunE: func(cmd *cobra.Command, args []string) error {
			err := run(cmd)
			if err == nil {
				return nil
			}
			var exit *exitError
			if errors.As(err, &exit) {
				return err
			}
			return &exitError{code: 1, err: err}
		},
	}
}

func (app *application) validatePlatforms(names []string) error {
	for _, name := range names {
		if !slices.Contains(app.runner.Names(), name) {
			return invalid(fmt.Errorf("unknown platform %q (available: %s)", name, strings.Join(app.runner.Names(), ", ")))
		}
	}
	return nil
}

func (app *application) notifier(dry bool) Notifier {
	if !dry && app.settings.HasTelegram() {
		return telegram.NewClient(app.settings.Token, app.settings.ChatID)
	}
	return telegram.Stdout{Writer: app.out}
}
