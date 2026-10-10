"""Настройки из окружения (и опционального .env в корне проекта)."""

from __future__ import annotations

import os
from dataclasses import dataclass, field
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


def read_secret(env, name: str) -> str | None:
    """Секрет из файла <NAME>_FILE (Docker secret) или, если файла нет, из переменной <NAME>."""
    path = env.get(f"{name}_FILE")
    if path:
        return Path(path).read_text().strip() or None
    return env.get(name) or None


@dataclass(frozen=True)
class Settings:
    tg_token: str | None = field(repr=False)   # не печатается в repr/логах
    tg_chat_id: str | None
    data_dir: Path
    request_delay: float = 0.4
    interval: int = 900           # секунд между прогонами в режиме loop
    heartbeat: Path | None = None  # файл, который loop трогает после каждого прогона (healthcheck)
    fail_alert_after: int = 3     # после скольких падений площадки подряд слать ⚠️

    # --- пороги защиты от ложных срабатываний (все в одном месте) ---
    # ответ площадки битый, если программ стало < min_ratio от прошлого И пропало > min_drop штук
    min_ratio: float = 0.7
    min_drop: int = 5
    # не загрузились детали у > max_fail_share программ → площадка упала целиком;
    # меньше — точечный сбой: у таких программ остаётся прошлый снапшот
    max_fail_share: float = 0.2
    # сколько прогонов подряд программы нет в списке, прежде чем сказать «пропала»
    missing_runs: int = 2
    # массовое изменение (> max(mass_min, mass_ratio × программ площадки)) → одно сводное сообщение
    mass_min: int = 10
    mass_ratio: float = 0.3

    @property
    def has_telegram(self) -> bool:
        return bool(self.tg_token and self.tg_chat_id)

    @classmethod
    def from_env(cls) -> Settings:
        load_dotenv(PROJECT_ROOT / ".env")
        env = os.environ
        return cls(
            tg_token=read_secret(env, "TG_BOT_TOKEN"),
            tg_chat_id=env.get("TG_CHAT_ID"),
            data_dir=Path(env.get("SCOPEWATCH_DATA", PROJECT_ROOT / "data")),
            request_delay=float(env.get("SCOPEWATCH_DELAY", 0.4)),
            fail_alert_after=int(env.get("SCOPEWATCH_FAIL_ALERT", 3)),
            interval=int(env.get("SCOPEWATCH_INTERVAL", 900)),
            heartbeat=Path(env["SCOPEWATCH_HEARTBEAT"]) if env.get("SCOPEWATCH_HEARTBEAT") else None,
        )
