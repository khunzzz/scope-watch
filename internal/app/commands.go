package app

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/khunzzz/scope-watch/internal/notify/telegram"
)

func newRunCommand(app *application) *cobra.Command {
	var only []string
	var dry bool
	cmd := app.command("run", "Выполнить один сбор", func(cmd *cobra.Command) error {
		if err := app.validatePlatforms(only); err != nil {
			return err
		}
		if !dry && !app.settings.HasTelegram() {
			return invalid(fmt.Errorf("нужны TG_BOT_TOKEN и TG_CHAT_ID; для проверки — --dry-run"))
		}
		result, err := app.runner.Run(cmd.Context(), app.notifier(dry), Options{Platforms: only, DryRun: dry})
		if err != nil {
			return err
		}
		if result.Unsent > 0 || len(result.FailedPlatforms) > 0 {
			return fmt.Errorf("сбор завершён с ошибками: площадок %d, сообщений в очереди %d", len(result.FailedPlatforms), result.Unsent)
		}
		return nil
	})
	cmd.Flags().StringArrayVar(&only, "only", nil, "площадка (можно повторять)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "напечатать сообщения без отправки и сохранения")
	return cmd
}

func newLoopCommand(app *application) *cobra.Command {
	var only []string
	cmd := app.command("loop", "Периодически собирать изменения", func(cmd *cobra.Command) error {
		if err := app.validatePlatforms(only); err != nil {
			return err
		}
		if !app.settings.HasTelegram() {
			return invalid(fmt.Errorf("нужны TG_BOT_TOKEN и TG_CHAT_ID"))
		}
		return loop(cmd.Context(), app.settings, app.runner, app.notifier(false), only, app.log)
	})
	cmd.Flags().StringArrayVar(&only, "only", nil, "площадка (можно повторять)")
	return cmd
}

func newSeedCommand(app *application) *cobra.Command {
	var only []string
	cmd := app.command("seed", "Сохранить исходный снимок без событий об изменениях", func(cmd *cobra.Command) error {
		if err := app.validatePlatforms(only); err != nil {
			return err
		}
		if !app.settings.HasTelegram() {
			return invalid(fmt.Errorf("нужны TG_BOT_TOKEN и TG_CHAT_ID; для проверки — run --dry-run"))
		}
		result, err := app.runner.Run(cmd.Context(), app.notifier(false), Options{Platforms: only, Seed: true})
		if err != nil {
			return err
		}
		if result.Unsent > 0 || len(result.FailedPlatforms) > 0 {
			return fmt.Errorf("исходный снимок сохранён не полностью")
		}
		return nil
	})
	cmd.Flags().StringArrayVar(&only, "only", nil, "площадка (можно повторять)")
	return cmd
}

func newStatusCommand(app *application) *cobra.Command {
	var asJSON bool
	cmd := app.command("status", "Показать свежесть данных, сбои и очередь", func(cmd *cobra.Command) error {
		return showStatus(cmd.Context(), app.settings, app.runner.Names(), app.out, asJSON)
	})
	cmd.Flags().BoolVar(&asJSON, "json", false, "вывести JSON")
	return cmd
}

func newHistoryCommand(app *application) *cobra.Command {
	var limit int
	var program string
	cmd := app.command("history", "Показать журнал изменений", func(cmd *cobra.Command) error {
		if limit < 1 {
			return invalid(fmt.Errorf("-n must be positive"))
		}
		return inspect(cmd.Context(), app.settings, app.runner, "history", limit, program, "", app.out)
	})
	cmd.Flags().IntVarP(&limit, "number", "n", 20, "число изменений")
	cmd.Flags().StringVar(&program, "program", "", "slug программы")
	return cmd
}

func newLinksCommand(app *application) *cobra.Command {
	return app.command("links", "Показать связанные программы площадок", func(cmd *cobra.Command) error {
		return inspect(cmd.Context(), app.settings, app.runner, "links", 0, "", "", app.out)
	})
}

func newExportCommand(app *application) *cobra.Command {
	var output string
	cmd := app.command("export", "Выгрузить scope компаний в JSON", func(cmd *cobra.Command) error {
		return inspect(cmd.Context(), app.settings, app.runner, "export", 0, "", output, app.out)
	})
	cmd.Flags().StringVarP(&output, "output", "o", "", "выходной файл (по умолчанию stdout)")
	return cmd
}

func newNotifyCommand(app *application) *cobra.Command {
	return app.command("test-notify", "Проверить Telegram или узнать chat_id", func(cmd *cobra.Command) error {
		if app.settings.Token == "" {
			return invalid(fmt.Errorf("нет TG_BOT_TOKEN"))
		}
		tg := telegram.NewClient(app.settings.Token, app.settings.ChatID)
		if app.settings.ChatID == "" {
			hint, err := tg.ChatIDs(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintln(app.out, "TG_CHAT_ID не задан. Чаты, которые писали боту:", hint)
			return invalid(fmt.Errorf("задай TG_CHAT_ID"))
		}
		if err := tg.Send(cmd.Context(), "✅ <b>scope-watch</b> на связи.\n<pre>*.example.ru</pre>"); err != nil {
			return err
		}
		_, err := fmt.Fprintln(app.out, "отправлено")
		return err
	})
}
