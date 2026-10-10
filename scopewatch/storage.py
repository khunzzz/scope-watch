"""SQLite: текущее состояние программ, журнал изменений, здоровье площадок, очередь сообщений.
Один файл <data>/scope-watch.db."""

from __future__ import annotations

import json
import sqlite3
import time
from dataclasses import asdict
from pathlib import Path

from .models import Program, ProgramChange

DB_NAME = "scope-watch.db"

SCHEMA = """
CREATE TABLE IF NOT EXISTS programs (
    platform TEXT NOT NULL,
    slug     TEXT NOT NULL,
    data     TEXT NOT NULL,          -- Program в JSON
    PRIMARY KEY (platform, slug)
);
CREATE TABLE IF NOT EXISTS changes (
    id       INTEGER PRIMARY KEY,
    ts       REAL NOT NULL,
    platform TEXT NOT NULL,
    slug     TEXT NOT NULL,
    kind     TEXT NOT NULL,          -- new / updated / removed
    data     TEXT NOT NULL           -- ProgramChange в JSON (без полных снапшотов)
);
CREATE INDEX IF NOT EXISTS changes_program ON changes (platform, slug, ts);
CREATE TABLE IF NOT EXISTS health (
    platform   TEXT PRIMARY KEY,
    fails      INTEGER NOT NULL DEFAULT 0,
    alerted    INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS outbox (
    id   INTEGER PRIMARY KEY,
    ts   REAL NOT NULL,
    html TEXT NOT NULL               -- готовое сообщение, ждёт отправки
);
"""


class Store:
    def __init__(self, data_dir: Path):
        data_dir.mkdir(parents=True, exist_ok=True)
        self.path = data_dir / DB_NAME
        self.db = sqlite3.connect(self.path)
        self.db.executescript(SCHEMA)

    def close(self) -> None:
        self.db.close()

    # --- программы ---

    def load(self, platform: str) -> dict[str, Program]:
        rows = self.db.execute("SELECT data FROM programs WHERE platform = ?", (platform,))
        return {p.slug: p for p in (Program.from_dict(json.loads(d)) for (d,) in rows)}

    def commit_run(self, snapshots: dict[str, list[Program]], health: dict,
                   changes: list[ProgramChange], messages: list[str]) -> None:
        """Итог прогона одной транзакцией: снапшоты площадок, здоровье, журнал и очередь сообщений.
        Если процесс убьют посередине, не будет ни сохранённого состояния без сообщений
        (= потерянные события), ни сообщений без состояния (= дубли)."""
        with self.db:
            for platform, programs in snapshots.items():
                self._save(platform, programs)
            self._save_health(health)
            self._log_changes(changes)
            now = time.time()
            self.db.executemany("INSERT INTO outbox (ts, html) VALUES (?, ?)", [(now, m) for m in messages])

    def _save(self, platform: str, programs: list[Program]) -> None:
        """Полная замена состояния площадки (транзакция — в commit_run)."""
        self.db.execute("DELETE FROM programs WHERE platform = ?", (platform,))
        self.db.executemany(
            "INSERT INTO programs (platform, slug, data) VALUES (?, ?, ?)",
            [(platform, p.slug, json.dumps(p.to_dict(), ensure_ascii=False)) for p in programs],
        )

    # --- очередь сообщений ---

    def outbox(self) -> list[tuple[int, str]]:
        return list(self.db.execute("SELECT id, html FROM outbox ORDER BY id"))

    def outbox_done(self, msg_id: int) -> None:
        with self.db:
            self.db.execute("DELETE FROM outbox WHERE id = ?", (msg_id,))

    # --- журнал изменений ---

    def _log_changes(self, changes: list[ProgramChange]) -> None:
        now = time.time()
        rows = []
        for ch in changes:
            d = asdict(ch)
            d.pop("old")
            p = d.pop("program")
            d["name"], d["url"] = p["name"], p["url"]
            rows.append((now, ch.program.platform, ch.program.slug, ch.kind, json.dumps(d, ensure_ascii=False)))
        self.db.executemany("INSERT INTO changes (ts, platform, slug, kind, data) VALUES (?, ?, ?, ?, ?)", rows)

    def history(self, limit: int = 20, slug: str | None = None) -> list[dict]:
        q = "SELECT ts, platform, slug, kind, data FROM changes"
        args: tuple = ()
        if slug:
            q, args = q + " WHERE slug = ?", (slug,)
        rows = self.db.execute(q + " ORDER BY id DESC LIMIT ?", (*args, limit))
        return [{"ts": ts, "platform": pl, "slug": sl, "kind": k, **json.loads(d)} for ts, pl, sl, k, d in rows]

    # --- здоровье площадок ---

    def health(self) -> dict:
        rows = self.db.execute("SELECT platform, fails, alerted, last_error FROM health")
        return {pl: {"fails": f, "alerted": bool(a), "last_error": e} for pl, f, a, e in rows}

    def _save_health(self, h: dict) -> None:
        self.db.executemany(
            "INSERT OR REPLACE INTO health (platform, fails, alerted, last_error) VALUES (?, ?, ?, ?)",
            [(pl, v["fails"], int(v["alerted"]), v.get("last_error", "")) for pl, v in h.items()],
        )
