"""Standoff 365 Bug Bounty.

Список: Next.js-страницы /programs?page=N (в каждой программе уже есть description/specialRules).
Выплаты: pageProps.reward со страницы /programs/<slug>. Это тяжёлая страница (~140 KB),
поэтому грузим её только для новых/изменённых программ и раз в REWARD_TTL.
"""

from __future__ import annotations

import time

from ..assets import extract
from ..http import Client
from ..models import SEVERITIES, Program

NAME = "standoff"
TITLE = "Standoff 365"
BASE = "https://bugbounty.standoff365.com"
EXCLUDE = ("standoff365.com",)
REWARD_TTL = 6 * 3600
MAX_PAGES = 50


def _scope_md(p: dict) -> str:
    parts = [p.get("description") or "", p.get("specialRules") or ""]
    return "\n\n".join(x.replace("\r\n", "\n").strip() for x in parts if x and x.strip())


def _rewards(reward: dict | None) -> dict[str, list[int]]:
    if not reward:
        return {}
    out = {}
    for sev in SEVERITIES:
        r = reward.get(sev) or {}
        lo, hi = int(r.get("minReward") or 0), int(r.get("maxReward") or 0)
        if hi:
            out[sev] = [lo, hi]
    return out


def parse(p: dict) -> Program:
    md = _scope_md(p)
    status = p.get("status") or ""
    return Program(
        platform=NAME,
        slug=p["slug"],
        name=p.get("name") or p["slug"],
        url=f"{BASE}/programs/{p['slug']}",
        active=status == "published" and not p.get("finished") and not p.get("archivedAt"),
        status="finished" if p.get("finished") else status,
        assets=extract(md, EXCLUDE),
        scope_md=md,
        updated_at=p.get("updatedAt") or "",
    )


def list_raw(c: Client) -> list[dict]:
    seen, out = set(), []
    for page in range(1, MAX_PAGES + 1):
        pp = c.next_data(f"{BASE}/programs", params={"page": page})
        progs = pp.get("programs") or []
        if not progs:
            break
        for p in progs:
            if p.get("visibility", "public") == "public" and p["slug"] not in seen:
                seen.add(p["slug"])
                out.append(p)
        total_pages = pp.get("total")
        if isinstance(total_pages, int) and page >= total_pages:
            break
    return out


def fetch(c: Client, prev: dict[str, Program]) -> list[Program]:
    now = time.time()
    result = []
    for raw in list_raw(c):
        prog = parse(raw)
        old = prev.get(prog.slug)
        stale = (
            old is None
            or old.updated_at != prog.updated_at
            or now - old.meta.get("rewards_at", 0) > REWARD_TTL
        )
        if stale and prog.active:
            pp = c.next_data(prog.url)
            prog.rewards = _rewards(pp.get("reward"))
            prog.meta["rewards_at"] = now
        elif old is not None:
            prog.rewards = old.rewards
            prog.meta["rewards_at"] = old.meta.get("rewards_at", 0)
        result.append(prog)
    return result
