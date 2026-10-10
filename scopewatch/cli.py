"""CLI.

  scope-watch loop                                прогон каждые SCOPEWATCH_INTERVAL секунд (Docker)
  scope-watch run [--dry-run] [--only standoff]   один прогон (для cron / ручной проверки)
  scope-watch seed                                перезаписать базу без уведомлений
  scope-watch test-notify                         проверить бота / узнать chat_id
  scope-watch history [-n 20] [--program tbank]   журнал изменений из базы
  scope-watch links                               какие программы склеены между платформами
  scope-watch export [-o companies.json]          сводный скоуп по компаниям (для рекона)
"""

from __future__ import annotations

import argparse
import json
import logging
import random
import signal
import sys
import time

from . import logs
from .config import Settings
from .linker import Linker
from .notify import Stdout, Telegram
from .runner import run
from .sources import SOURCES, TITLES
from .storage import Store


def _notifier(s: Settings, dry_run: bool):
    if dry_run or not s.has_telegram:
        return Stdout()
    return Telegram(s.tg_token, s.tg_chat_id)


def _linker(s: Settings) -> Linker:
    store = Store(s.data_dir)
    try:
        return Linker([p for name in SOURCES for p in store.load(name).values() if p.active])
    finally:
        store.close()


def cmd_run(s: Settings, a) -> int:
    if not a.dry_run and not s.has_telegram:
        logging.error("нужны TG_BOT_TOKEN и TG_CHAT_ID (env или .env); для проверки — --dry-run")
        return 2
    res = run(s, _notifier(s, a.dry_run), platforms=a.only, dry_run=a.dry_run)
    return 1 if res.unsent else 0


def cmd_loop(s: Settings, a) -> int:
    if not s.has_telegram:
        logging.error("нужны TG_BOT_TOKEN и TG_CHAT_ID")
        return 2
    probe = s.data_dir / ".write-test"
    try:
        probe.touch()
        probe.unlink()
    except OSError as e:
        logging.error("нет записи в %s (%s). Для Docker: sudo chown -R 1000:1000 ./data", s.data_dir, e)
        return 2
    stop = False

    def on_term(*_):
        # docker stop: даём текущему прогону дописать снапшоты, потом выходим
        nonlocal stop
        stop = True
        logging.info("получен сигнал остановки, выйду после текущего прогона")

    signal.signal(signal.SIGTERM, on_term)
    signal.signal(signal.SIGINT, on_term)
    notifier = _notifier(s, False)
    logging.info("loop: интервал %d с, данные в %s", s.interval, s.data_dir)
    while not stop:
        started = time.monotonic()
        try:
            res = run(s, notifier)
            # Telegram недоступен (сообщения копятся в очереди) — пусть healthcheck это покажет
            if s.heartbeat and not res.unsent:
                s.heartbeat.touch()
        except Exception:  # noqa: BLE001 — один упавший прогон не должен ронять контейнер
            logging.exception("прогон упал, попробую в следующий раз")
        # небольшой джиттер, чтобы не ходить на площадки ровно по расписанию
        sleep_for = max(30.0, s.interval - (time.monotonic() - started)) + random.uniform(0, 30)
        deadline = time.monotonic() + sleep_for
        while not stop and time.monotonic() < deadline:
            time.sleep(1)
    return 0


def cmd_seed(s: Settings, a) -> int:
    run(s, _notifier(s, False), platforms=a.only, seed=True)
    return 0


def cmd_test_notify(s: Settings, a) -> int:
    if not s.tg_token:
        print("нет TG_BOT_TOKEN")
        return 2
    tg = Telegram(s.tg_token, s.tg_chat_id or "")
    if not s.tg_chat_id:
        print("TG_CHAT_ID не задан. Чаты, которые писали боту:", tg.chat_ids_hint())
        return 2
    tg.send("✅ <b>scope-watch</b> на связи.\n<pre>*.example.ru</pre>")
    print("отправлено")
    return 0


def cmd_history(s: Settings, a) -> int:
    store = Store(s.data_dir)
    rows = store.history(a.n, a.program)
    store.close()
    for r in reversed(rows):
        when = time.strftime("%Y-%m-%d %H:%M", time.localtime(r["ts"]))
        what = []
        if r["kind"] != "updated":
            what.append(r["kind"])
        if r["activated"] is not None:
            what.append("reopened" if r["activated"] else "closed")
        if r["assets_added"]:
            what.append("+" + ", +".join(r["assets_added"][:5]) + (" …" if len(r["assets_added"]) > 5 else ""))
        if r["assets_removed"]:
            what.append(f"-{len(r['assets_removed'])} ассетов")
        if r.get("bounty_lost"):
            what.append(f"без выплат: {len(r['bounty_lost'])}")
        if r.get("bounty_gained"):
            what.append(f"с выплатами: {len(r['bounty_gained'])}")
        if r["rewards"]:
            what.append("выплаты")
        if r["new_versions"]:
            what.append("версия " + ",".join(r["new_versions"]))
        if r["text_diff"]:
            what.append("текст")
        print(f"{when}  {TITLES[r['platform']]:<12} {r['name'][:28]:<28} {'; '.join(what)}")
    if not rows:
        print("изменений пока не было")
    return 0


def cmd_links(s: Settings, a) -> int:
    lk = _linker(s)
    for ln in sorted(lk.links, key=lambda x: -x.score):
        pa, pb = lk.by_key[ln.a], lk.by_key[ln.b]
        print(f"{ln.score:5.2f}  {pa.key:<36} {pb.key:<44} {', '.join(ln.shared[:3])}")
    print(f"\nсклеено пар: {len(lk.links)}")
    return 0


def cmd_export(s: Settings, a) -> int:
    lk = _linker(s)
    groups: dict[str, list] = {}
    for p in lk.by_key.values():
        groups.setdefault(lk.group_key(p), []).append(p)
    out = []
    for progs in groups.values():
        progs.sort(key=lambda p: p.platform)
        assets = sorted({x for p in progs for x in p.assets})
        out.append({
            "company": progs[0].name,
            "programs": [{"platform": TITLES[p.platform], "slug": p.slug, "url": p.url,
                          "max_reward": p.max_reward} for p in progs],
            "max_reward": max(p.max_reward for p in progs),
            "assets": assets,
            "out_of_scope": sorted({x for p in progs for x in p.out_of_scope}),
        })
    out.sort(key=lambda c: -c["max_reward"])
    text = json.dumps(out, ensure_ascii=False, indent=1)
    if a.output:
        with open(a.output, "w", encoding="utf-8") as f:
            f.write(text + "\n")
        print(f"{len(out)} компаний → {a.output}")
    else:
        print(text)
    return 0


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="scope-watch", description="Мониторинг скоупов Standoff 365 и BI.ZONE")
    ap.add_argument("-v", "--verbose", action="store_true")
    sub = ap.add_subparsers(dest="cmd", required=True)

    sub.add_parser("loop", help="бесконечный цикл прогонов (для Docker)").set_defaults(func=cmd_loop)

    p = sub.add_parser("run", help="один прогон с уведомлениями")
    p.add_argument("--dry-run", action="store_true", help="печатать сообщения, ничего не сохранять")
    p.add_argument("--only", action="append", choices=list(SOURCES))
    p.set_defaults(func=cmd_run)

    p = sub.add_parser("seed", help="перезаписать базу без уведомлений")
    p.add_argument("--only", action="append", choices=list(SOURCES))
    p.set_defaults(func=cmd_seed)

    sub.add_parser("test-notify", help="тестовое сообщение в Telegram").set_defaults(func=cmd_test_notify)
    p = sub.add_parser("history", help="журнал изменений")
    p.add_argument("-n", type=int, default=20)
    p.add_argument("--program", help="slug программы")
    p.set_defaults(func=cmd_history)

    sub.add_parser("links", help="склейка программ между платформами").set_defaults(func=cmd_links)

    p = sub.add_parser("export", help="сводный скоуп по компаниям")
    p.add_argument("-o", "--output")
    p.set_defaults(func=cmd_export)

    a = ap.parse_args(argv)
    settings = Settings.from_env()
    logs.setup(a.verbose, [settings.tg_token])
    return a.func(settings, a)


if __name__ == "__main__":
    sys.exit(main())
