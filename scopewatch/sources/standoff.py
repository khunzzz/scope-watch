"""Standoff 365 Bug Bounty.

Список: Next.js-страницы /programs?page=N (в каждой программе уже есть description/specialRules).
Выплаты: pageProps.reward со страницы /programs/<slug>. Это тяжёлая страница (~140 KB),
поэтому грузим её только для новых/изменённых программ и раз в REWARD_TTL.
"""

from __future__ import annotations

import logging
import time

from ..assets import VERSION as EXTRACTOR_VERSION, split_scope
from ..http import Client
from ..models import SEVERITIES, Fetched, Program

NAME = "standoff"
TITLE = "Standoff 365"
BASE = "https://bugbounty.standoff365.com"
# ссылки на доки/VPN площадки в правилах чужих программ — не ассеты;
# но у программы самой площадки это и есть скоуп
EXCLUDE = ("standoff365.com",)
OWN_SLUGS = {"standoff-365"}
REWARD_TTL = 6 * 3600
MAX_PAGES = 50

log = logging.getLogger("scope-watch")


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
    assets, oos = split_scope(md, () if p["slug"] in OWN_SLUGS else EXCLUDE)
    return Program(
        platform=NAME,
        slug=p["slug"],
        name=p.get("name") or p["slug"],
        url=f"{BASE}/programs/{p['slug']}",
        active=status == "published" and not p.get("finished") and not p.get("archivedAt"),
        status="finished" if p.get("finished") else status,
        assets=assets,
        out_of_scope=oos,
        scope_md=md,
        meta={"extractor": EXTRACTOR_VERSION},
        updated_at=p.get("updatedAt") or "",
    )


def list_raw(c: Client) -> list[dict]:
    seen, out = set(), []
    total_pages = None   # pageProps.total — число страниц (не программ)
    for page in range(1, MAX_PAGES + 1):
        pp = c.next_data(f"{BASE}/programs", params={"page": page})
        progs = pp.get("programs") or []
        if isinstance(pp.get("total"), int) and total_pages is None:
            total_pages = pp["total"]
        if not progs:
            if total_pages and page <= total_pages:
                # пустая страница посреди списка — сбой, а не конец: иначе хвост списка «пропадёт»
                raise ValueError(f"пустая страница {page} из {total_pages}")
            break
        for p in progs:
            if p.get("visibility", "public") == "public" and p["slug"] not in seen:
                seen.add(p["slug"])
                out.append(p)
        if total_pages and page >= total_pages:
            break
    return out


def fetch(c: Client, prev: dict[str, Program]) -> Fetched:
    now = time.time()
    items = list_raw(c)
    result, failed = [], []
    for raw in items:
        prog = parse(raw)
        old = prev.get(prog.slug)
        stale = (
            old is None
            or old.updated_at != prog.updated_at
            or now - old.meta.get("rewards_at", 0) > REWARD_TTL
        )
        loaded = False
        if stale and prog.active:
            try:
                pp = c.next_data(prog.url)
                prog.rewards = _rewards(pp.get("reward"))
                prog.meta["rewards_at"] = now
                loaded = True
            except Exception as e:  # noqa: BLE001 — сбой одной страницы не должен ронять площадку
                log.warning("standoff/%s: выплаты не загрузились (%s: %s)", prog.slug, type(e).__name__, e)
                failed.append(prog.slug)
        if not loaded and old is not None:
            # прошлые выплаты и время их загрузки: при сбое повторим в следующий прогон
            prog.rewards = old.rewards
            prog.meta["rewards_at"] = old.meta.get("rewards_at", 0)
        # скоуп Standoff целиком в списке, поэтому новая программа без выплат всё равно
        # объявляется (иначе вечный сбой её страницы = навсегда пропущенная программа);
        # выплаты подтянутся в следующий прогон: rewards_at не выставлен
        result.append(prog)
    return Fetched(result, total=len(items), failed=failed)
