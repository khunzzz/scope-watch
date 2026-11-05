# scope-watch

Мониторинг публичных bug bounty программ Standoff 365 и BI.ZONE на Go.
Отправляет в Telegram изменения scope, правил, выплат и статусов программ.
Данные и очередь уведомлений хранятся в SQLite.

## Запуск

Нужны Docker и Docker Compose.

```sh
cp .env.example .env
mkdir -p secrets data
printf '%s' 'ТОКЕН_БОТА' > secrets/tg_bot_token
sudo chown 1000:1000 data secrets/tg_bot_token
sudo chmod 400 secrets/tg_bot_token
```

Укажи список чатов в `.env`: `TG_CHAT_IDS=123456789,-1001234567890`. Затем:

```sh
docker compose up -d --build
```

Чтобы узнать `TG_CHAT_IDS`, напиши боту `/start` и выполни:

```sh
docker compose run --rm scope-watch scope-watch test-notify
```

Первый сбор сохраняет исходный снимок. Следующие проверки — каждые 15 минут.
Сервис читает публичные данные и не сканирует цели.
Отсутствие опубликованной таблицы выплат Standoff (`reward: null`) не считается
ошибкой или нулевой наградой: ранее известные суммы сохраняются, проверка
повторяется при изменении программы или через 6 часов.

## Команды

```sh
docker compose exec scope-watch scope-watch status
docker compose exec scope-watch scope-watch history -n 20
docker compose exec scope-watch scope-watch run --dry-run
docker compose exec scope-watch scope-watch export -o /data/companies.json
```

Интервал: `SCOPEWATCH_INTERVAL` в секундах. Срок истории:
`SCOPEWATCH_HISTORY_DAYS` в днях, по умолчанию `0` — хранить всё.

Для отправки в тему форума Telegram задай необязательную переменную
`TG_MESSAGE_THREAD_ID=4` и ID супергруппы в `TG_CHAT_IDS` (например,
`TG_CHAT_IDS=-1001234567890`). ID темы должен быть положительным целым числом;
явно заданные `0`, отрицательные, пустые и некорректные значения вызывают ошибку
настроек. Если переменная отсутствует, поле `message_thread_id` не отправляется,
и прежнее поведение сохраняется. Одна тема применяется ко всем чатам списка и
ко всем отправкам (`test-notify`, `seed`, `run`, `loop`, повторы очереди).
В поставляемом Docker Compose переменная из `.env` передаётся в контейнер
через `env_file`. При другом способе запуска передай её в окружение процесса.

## Разработка

Версия Go указана в `go.mod`.

```sh
go build -trimpath -o bin/scope-watch ./cmd/scope-watch
go vet ./...
go test -race -count=1 ./...
```

[Полное руководство](docs/guide.md) · [Планы](docs/roadmap.md) · [Правила работы с кодом](AGENTS.md)
