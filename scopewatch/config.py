"""Настройки из окружения (и опционального .env в корне проекта)."""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent


def load_dotenv(path: Path) -> None:
    if not path.exists():
        return
    for line in path.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, v = line.split("=", 1)
            os.environ.setdefault(k.strip(), v.strip().strip('"').strip("'"))


@dataclass(frozen=True)
class Settings:
    tg_token: str | None
    tg_chat_id: str | None
    data_dir: Path
    request_delay: float = 0.4
    fail_alert_after: int = 3     # после скольких падений подряд слать ⚠️
    min_ratio: float = 0.7        # программ стало меньше 70% от прошлого → ответ битый
    interval: int = 900           # секунд между прогонами в режиме loop
    heartbeat: Path | None = None  # файл, который loop трогает после каждого прогона (healthcheck)

    @property
    def has_telegram(self) -> bool:
        return bool(self.tg_token and self.tg_chat_id)

    @classmethod
    def from_env(cls) -> Settings:
        load_dotenv(PROJECT_ROOT / ".env")
        env = os.environ
        return cls(
            tg_token=env.get("TG_BOT_TOKEN"),
            tg_chat_id=env.get("TG_CHAT_ID"),
            data_dir=Path(env.get("SCOPEWATCH_DATA", PROJECT_ROOT / "data")),
            request_delay=float(env.get("SCOPEWATCH_DELAY", 0.4)),
            fail_alert_after=int(env.get("SCOPEWATCH_FAIL_ALERT", 3)),
            interval=int(env.get("SCOPEWATCH_INTERVAL", 900)),
            heartbeat=Path(env["SCOPEWATCH_HEARTBEAT"]) if env.get("SCOPEWATCH_HEARTBEAT") else None,
        )
