# scope-watch

Следит за публичными bug bounty программами **Standoff 365** и **BI.ZONE** и пишет в Telegram, когда:

- 🆕 появилась новая программа;
- 🎯 в скоупе появились новые ассеты (домены, wildcard, IP, мобильные приложения). Новые идут блоком `<pre>`, чтобы сразу скопировать в subfinder/httpx;
- 💰 изменились выплаты;
- 📝 поменялся текст скоупа или правил (короткий дифф);
- 🔒/♻️/🗑 программа закрылась, переоткрылась или пропала из публичного списка;
- 🗂 на BI.ZONE вышла новая версия правил или выплат.

## Дубли между платформами

Многие компании есть на обеих площадках (VK, Т-Банк, Ozon, Госуслуги, Timeweb, RuStore…). `linker.py` склеивает такие программы по общим регистрируемым доменам и id приложений. У каждого признака вес 1/частота, поэтому `mail.ru` из правил всех VK-программ почти ничего не весит. Дополнительно учитывается нормализованный slug, связь ставится только при взаимном выборе. Это даёт:

- одно сообщение на компанию, даже если она обновилась на обеих платформах;
- новые ассеты делятся на «действительно новые» и «уже были в скоупе на другой платформе»;
- ссылку «🔗 Также на …»;
- `scope-watch export`: сводный скоуп по компаниям без дублей.

Посмотреть, что склеилось: `scope-watch links`.

## Откуда данные (всё публично, без логина)

| | Список | Скоуп | Выплаты |
|---|---|---|---|
| Standoff 365 | `__NEXT_DATA__` страниц `/programs?page=N` (нужен `Accept-Language: ru`, иначе приходит машинный перевод) | markdown `description` + `specialRules`, домены извлекаются эвристикой | `pageProps.reward` со страницы программы; кэш на 6 ч или до смены `updatedAt` |
| BI.ZONE | `GET /api/bug-bounty/companies/` | `GET /api/bug-bounty/tasks/`: структурированный скоуп, хосты лежат в описании задачи | поля `min/max*Price` |

## Структура

```
scopewatch/
  cli.py         команды: run / seed / test-notify / links / export
  config.py      Settings из env / .env
  runner.py      прогон: collect → diff → link → render → send → save
  sources/       по модулю на платформу: fetch(client, prev) -> list[Program]
  models.py      Program, ProgramChange
  assets.py      извлечение и нормализация ассетов, registrable(), covered()
  differ.py      снапшоты → структурные изменения
  linker.py      склейка компаний между платформами
  render.py      изменения → HTML для Telegram (группировка по компании)
  notify.py      Telegram Bot API / stdout
  storage.py     снапшоты JSON + git-коммиты + health
deploy/          systemd service/timer, install.sh
tests/           pytest на реальных фикстурах, без сети
```

Новая платформа — это новый модуль в `sources/` с `NAME`, `TITLE`, `fetch()`, плюс одна строка в `sources/__init__.py`.

## Надёжность

- Первый прогон (пустая база) только сохраняет состояние и шлёт 🌱, сотни «новых программ» не будет.
- Если платформа упала или вернула меньше 70% программ от прошлого раза, её снапшоты не трогаются и ложных «пропала» нет. После 3 падений подряд приходит ⚠️, после восстановления ✅.
- Сначала отправка, потом сохранение: если Telegram недоступен, события придут в следующий прогон.
- База лежит в git (`/var/lib/scope-watch`), полная история скоупов: `git log -p bizone/tbank.json`.
- Запросы: пауза 0.4 с, ретраи на 5xx/429, заголовок `X-Bug-Bounty: sayfu`.

## Локально

```bash
python3 -m venv .venv && .venv/bin/pip install -e '.[dev]'
.venv/bin/pytest
cp .env.example .env            # вписать токен и chat_id
.venv/bin/scope-watch seed      # без токена печатает в консоль
.venv/bin/scope-watch run --dry-run
.venv/bin/scope-watch test-notify
```

## VPS

1. Бот: @BotFather → `/newbot` → токен. Напиши своему боту `/start`.
2. chat_id: `TG_BOT_TOKEN=... scope-watch test-notify` без `TG_CHAT_ID` покажет id чатов, которые писали боту.
3. На VPS (Debian/Ubuntu, хватит 1 vCPU / 512 MB). Сначала проверь, что площадки открываются с его IP:
   ```bash
   curl -sI https://bugbounty.bi.zone/api/bug-bounty/companies/ | head -1
   curl -sI https://bugbounty.standoff365.com/programs | head -1
   git clone <repo> scope-watch && cd scope-watch
   sudo TG_BOT_TOKEN=... TG_CHAT_ID=... ./deploy/install.sh
   ```
4. Проверка: `systemctl list-timers scope-watch.timer`, `journalctl -u scope-watch -f`.

Обновление кода: `git pull && sudo ./deploy/install.sh`.
