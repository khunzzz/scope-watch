# scope-watch

Следит за публичными программами **Standoff 365** и **BI.ZONE** и шлёт в Telegram новые программы, новые ассеты в скоупе, изменения выплат и правил, закрытие программ. Если компания есть на обеих площадках, обновления приходят одним сообщением.

## Запуск

```bash
cp .env.example .env              # TG_BOT_TOKEN, TG_CHAT_ID
mkdir -p data && sudo chown 1000:1000 data
docker compose up -d --build
```

Первый прогон только сохраняет базу. Дальше проверка идёт каждые 15 минут.
Чтобы узнать chat_id, напиши боту `/start` и запусти `docker compose run --rm scope-watch scope-watch test-notify`.

## Команды

`docker compose exec scope-watch scope-watch <команда>`:

- `history` — журнал изменений
- `links` — какие программы склеены между площадками
- `export -o /data/companies.json` — общий скоуп по компаниям
- `run --dry-run` — прогон без отправки и сохранения

## Ограничения

- Только публичные программы.
- Если Telegram недоступен, сообщения ждут в базе и уйдут при следующем прогоне (без дублей).
- Скоуп Standoff извлекается из текста. Если ассет не распознался, изменение всё равно придёт диффом текста.

## Разработка

```bash
python3 -m venv .venv && .venv/bin/pip install -e '.[dev]'
.venv/bin/pytest
```
