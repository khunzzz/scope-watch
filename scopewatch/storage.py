"""Снапшоты: <data>/<platform>/<slug>.json. Если <data> — git-репо, каждое изменение коммитится."""

from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

from .models import Program


def _fname(slug: str) -> str:
    return re.sub(r"[^\w.-]", "_", slug) + ".json"


class Store:
    def __init__(self, root: Path):
        self.root = root
        self.root.mkdir(parents=True, exist_ok=True)

    def load(self, platform: str) -> dict[str, Program]:
        d = self.root / platform
        out = {}
        if d.is_dir():
            for f in d.glob("*.json"):
                p = Program.from_dict(json.loads(f.read_text(encoding="utf-8")))
                out[p.slug] = p
        return out

    def save(self, platform: str, programs: list[Program]) -> None:
        d = self.root / platform
        d.mkdir(parents=True, exist_ok=True)
        keep = set()
        for p in programs:
            name = _fname(p.slug)
            keep.add(name)
            (d / name).write_text(
                json.dumps(p.to_dict(), ensure_ascii=False, indent=1, sort_keys=True) + "\n",
                encoding="utf-8",
            )
        for f in d.glob("*.json"):
            if f.name not in keep:
                f.unlink()

    # --- health: подряд идущие падения платформ ---
    @property
    def _health_path(self) -> Path:
        return self.root / ".health.json"

    def health(self) -> dict:
        try:
            return json.loads(self._health_path.read_text())
        except (FileNotFoundError, ValueError):
            return {}

    def save_health(self, h: dict) -> None:
        self._health_path.write_text(json.dumps(h, indent=1, sort_keys=True) + "\n")

    def commit(self, message: str) -> None:
        if not (self.root / ".git").exists():
            return
        git = ["git", "-C", str(self.root)]
        subprocess.run([*git, "add", "-A"], check=True, capture_output=True)
        if subprocess.run([*git, "diff", "--cached", "--quiet"]).returncode == 0:
            return
        subprocess.run([*git, "-c", "user.name=scope-watch", "-c", "user.email=scope-watch@localhost",
                        "commit", "-q", "-m", message], check=True, capture_output=True)
