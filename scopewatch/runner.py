"""Один прогон: собрать → сравнить → склеить компании → отрендерить → отправить → сохранить."""

from __future__ import annotations

import logging
from dataclasses import dataclass, field

from . import render
from .config import Settings
from .differ import diff_platform
from .http import Client
from .linker import Linker
from .models import Program, ProgramChange
from .notify import Notifier
from .sources import SOURCES, TITLES
from .storage import Store

log = logging.getLogger("scope-watch")


@dataclass
class Collected:
    prev: dict[str, Program]
    new: list[Program] | None = None   # None — платформа упала, данные не трогаем
    error: str = ""

    @property
    def current(self) -> list[Program]:
        return self.new if self.new is not None else list(self.prev.values())


@dataclass
class RunResult:
    messages: list[str] = field(default_factory=list)
    changes: list[ProgramChange] = field(default_factory=list)
    seeded: list[str] = field(default_factory=list)


def collect(settings: Settings, store: Store, platforms: list[str]) -> dict[str, Collected]:
    client = Client(delay=settings.request_delay)
    out = {}
    for name in platforms:
        c = Collected(prev=store.load(name))
        try:
            new = SOURCES[name].fetch(client, c.prev)
            if not new:
                raise RuntimeError("пустой список программ")
            if c.prev and len(new) < settings.min_ratio * len(c.prev):
                raise RuntimeError(f"подозрительно мало программ: {len(new)} против {len(c.prev)}")
            c.new = new
            log.info("%s: %d программ", name, len(new))
        except Exception as e:  # noqa: BLE001 — любая ошибка источника = платформа недоступна
            c.error = f"{type(e).__name__}: {e}"
            log.error("%s: %s", name, c.error)
        out[name] = c
    return out


def update_health(settings: Settings, health: dict, collected: dict[str, Collected]) -> list[str]:
    """Обновляет счётчики падений на месте, возвращает сообщения ⚠️/✅."""
    msgs = []
    for name, c in collected.items():
        h = health.setdefault(name, {"fails": 0, "alerted": False})
        if c.new is None:
            h["fails"] += 1
            h["last_error"] = c.error[:300]
            if h["fails"] >= settings.fail_alert_after and not h["alerted"]:
                msgs.append(render.health_alert(name, h["fails"], c.error))
                h["alerted"] = True
        else:
            if h["alerted"]:
                msgs.append(render.health_ok(name))
            h.update(fails=0, alerted=False, last_error="")
    return msgs


def run(settings: Settings, notifier: Notifier, *, platforms: list[str] | None = None,
        seed: bool = False, dry_run: bool = False) -> RunResult:
    store = Store(settings.data_dir)
    platforms = platforms or list(SOURCES)
    collected = collect(settings, store, platforms)

    # склейка по актуальному состоянию всех платформ (включая не опрошенные в этот раз)
    everything = [p for name in SOURCES
                  for p in (collected[name].current if name in collected else store.load(name).values())]
    linker = Linker([p for p in everything if p.active])

    result = RunResult()
    for name, c in collected.items():
        if c.new is None:
            continue
        if seed or not c.prev:
            result.seeded.append(f"{TITLES[name]}: {len(c.new)} программ ({sum(p.active for p in c.new)} активных)")
            continue
        result.changes += diff_platform(c.prev, c.new)

    result.messages = render.render(result.changes, linker)
    if result.seeded:
        result.messages.append(render.seeded_summary(result.seeded))
    health = store.health()
    if not dry_run:
        result.messages = update_health(settings, health, collected) + result.messages

    # сначала отправка, потом сохранение: если Telegram упал, события придут в следующий прогон
    for m in result.messages:
        notifier.send(m)

    if not dry_run:
        for name, c in collected.items():
            if c.new is not None:
                store.save(name, c.new)
        store.save_health(health)
        summary = ", ".join(f"{ch.program.key}" for ch in result.changes[:20])
        store.commit(f"{len(result.changes)} changes: {summary}" if result.changes
                     else ("seed" if result.seeded else "refresh"))
    log.info("готово: изменений %d, сообщений %d", len(result.changes), len(result.messages))
    return result
