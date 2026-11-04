package app

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/khunzzz/scope-watch/internal/storage"
)

func newNotificationsCommand(app *application) *cobra.Command {
	var limit int
	cmd := app.command("notifications", "Показать ошибки доставки в JSON", func(cmd *cobra.Command) (err error) {
		if limit < 1 || limit > 1000 {
			return invalid(fmt.Errorf("-n must be between 1 and 1000"))
		}
		s, err := storage.Open(cmd.Context(), app.settings.DataDir)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := s.Close(); err == nil {
				err = closeErr
			}
		}()
		failures, err := s.DeliveryFailures(cmd.Context(), uint64(limit))
		if err != nil {
			return err
		}
		return json.NewEncoder(app.out).Encode(failures)
	})
	cmd.Flags().IntVarP(&limit, "number", "n", 100, "число сообщений (до 1000)")
	var id int64
	retry := app.command("retry", "Вернуть сообщение из карантина в очередь", func(cmd *cobra.Command) (err error) {
		if id < 1 {
			return invalid(fmt.Errorf("--id must be positive"))
		}
		lock, err := acquireRunLock(app.settings.DataDir)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := lock.Close(); err == nil {
				err = closeErr
			}
		}()
		s, err := storage.Open(cmd.Context(), app.settings.DataDir)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := s.Close(); err == nil {
				err = closeErr
			}
		}()
		if err = s.RetryNotification(cmd.Context(), id); err != nil {
			return err
		}
		_, err = fmt.Fprintf(app.out, "Сообщение %d будет повторено при следующем сборе.\n", id)
		return err
	})
	retry.Flags().Int64Var(&id, "id", 0, "ID сообщения из notifications")
	cmd.AddCommand(retry)
	return cmd
}
