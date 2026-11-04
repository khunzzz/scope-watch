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

## Команды

```sh
docker compose exec scope-watch scope-watch status
docker compose exec scope-watch scope-watch history -n 20
docker compose exec scope-watch scope-watch run --dry-run
docker compose exec scope-watch scope-watch export -o /data/companies.json
```

Интервал: `SCOPEWATCH_INTERVAL` в секундах. Срок истории:
`SCOPEWATCH_HISTORY_DAYS` в днях, по умолчанию `0` — хранить всё.

## Разработка

Версия Go указана в `go.mod`.

```sh
go build -trimpath -o bin/scope-watch ./cmd/scope-watch
go vet ./...
go test -race -count=1 ./...
```

[Полное руководство](docs/guide.md) · [Планы](docs/roadmap.md) · [Правила работы с кодом](AGENTS.md)
