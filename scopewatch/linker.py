"""Склейка программ одной компании между платформами (Standoff ↔ BI.ZONE).

Признаки программы: регистрируемые домены ассетов и id мобильных приложений,
плюс нормализованный slug (vkplay_vk ↔ vk-play, gov-smev ↔ fgis-smev).
Вес общего признака = 1 / (в скольких программах он встречается): mail.ru из
правил всех VK-программ почти ничего не весит, а tbank.ru — весит.
Связь ставится только между разными платформами и только при взаимном выборе
(A — лучший кандидат для B и наоборот), чтобы не собирать цепочки через общий домен.
"""

from __future__ import annotations

import re
from collections import Counter
from dataclasses import dataclass

from .assets import link_tokens
from .models import Program

THRESHOLD = 0.6
SLUG_MATCH_BONUS = 1.0
_SLUG_NOISE = {"vk", "gov", "fgis", "gis", "bug", "bounty", "bb", "new", "ru", "program"}


def slug_key(slug: str) -> str:
    parts = [p for p in re.split(r"[^a-z0-9]+", slug.lower()) if p and p not in _SLUG_NOISE]
    key = "".join(parts)
    # vkplay_vk ↔ vk-play: префикс «vk» слитно с названием
    return key[2:] if key.startswith("vk") and len(key) > 4 else key


@dataclass(frozen=True)
class Link:
    a: str      # Program.key
    b: str
    score: float
    shared: tuple[str, ...]


class Linker:
    def __init__(self, programs: list[Program]):
        self.by_key = {p.key: p for p in programs}
        self.tokens = {p.key: link_tokens(p.assets) for p in programs}
        df = Counter(t for toks in self.tokens.values() for t in toks)
        self.weight = {t: 1.0 / n for t, n in df.items()}
        self.links = self._build(programs)
        self._peers: dict[str, list[str]] = {}
        for ln in self.links:
            self._peers.setdefault(ln.a, []).append(ln.b)
            self._peers.setdefault(ln.b, []).append(ln.a)

    def score(self, a: Program, b: Program) -> tuple[float, tuple[str, ...]]:
        shared = self.tokens[a.key] & self.tokens[b.key]
        s = sum(self.weight[t] for t in shared)
        ka, kb = slug_key(a.slug), slug_key(b.slug)
        if ka and ka == kb:
            s += SLUG_MATCH_BONUS
        return s, tuple(sorted(shared, key=lambda t: -self.weight[t]))

    def _build(self, programs: list[Program]) -> list[Link]:
        platforms = sorted({p.platform for p in programs})
        links: list[Link] = []
        for i, pa in enumerate(platforms):
            for pb in platforms[i + 1:]:
                A = [p for p in programs if p.platform == pa]
                B = [p for p in programs if p.platform == pb]
                scores = {(a.key, b.key): self.score(a, b) for a in A for b in B}
                best_a = {a.key: max(B, key=lambda b: scores[(a.key, b.key)][0]).key for a in A} if B else {}
                best_b = {b.key: max(A, key=lambda a: scores[(a.key, b.key)][0]).key for b in B} if A else {}
                for ak, bk in best_a.items():
                    s, shared = scores[(ak, bk)]
                    if s >= THRESHOLD and best_b.get(bk) == ak:
                        links.append(Link(ak, bk, round(s, 3), shared))
        return links

    def peers(self, p: Program) -> list[Program]:
        """Та же компания на других платформах."""
        return [self.by_key[k] for k in self._peers.get(p.key, [])]

    def group_key(self, p: Program) -> str:
        return min([p.key, *self._peers.get(p.key, [])])
