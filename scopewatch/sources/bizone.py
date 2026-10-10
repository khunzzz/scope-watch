"""BI.ZONE Bug Bounty: публичный DRF API /api/bug-bounty/companies/."""

from __future__ import annotations

from ..assets import VERSION as EXTRACTOR_VERSION, extract, split_scope
from ..http import Client
from ..models import SEVERITIES, Program

NAME = "bizone"
TITLE = "BI.ZONE"
BASE = "https://bugbounty.bi.zone"
API = f"{BASE}/api/bug-bounty"
# bi.zone упоминается в правилах чужих программ как ссылка; в задачах (явный скоуп) не фильтруем
EXCLUDE = ("bi.zone",)
OWN_SLUGS = {"bi-zone-bug-bounty"}
PAGE = 100


def _scope_md(d: dict) -> str:
    parts = [d.get("description") or "", d.get("rules") or ""]
    return "\n\n".join(x.replace("\r\n", "\n").strip() for x in parts if x and x.strip())


def task_assets(t: dict) -> list[str]:
    """Задача = группа скоупа: textTask — название («Critical-ресурсы»), хосты — в description.
    Если хостов нет (продукт, «Android App» без ссылки) — ассетом считается само название."""
    title = " ".join((t.get("textTask") or "").split())
    found = extract(f"{title}\n{t.get('description') or ''}")
    items = found or ([title[:120]] if title else [])
    if t.get("inScope", True) and not t.get("eligibility", True):
        items = [f"{a} (без выплат)" for a in items]
    return items


def _tasks_md(tasks: list[dict]) -> str:
    """Задачи в текст: правка описания задачи тоже попадёт в текстовый дифф."""
    blocks = []
    for t in sorted(tasks, key=lambda t: (not t.get("inScope", True), t.get("id", 0))):
        flag = "" if t.get("inScope", True) else " [out of scope]"
        desc = (t.get("description") or "").replace("\r\n", "\n").strip()
        blocks.append(f"### {(t.get('textTask') or '').strip()}{flag}\n{desc}".rstrip())
    return "## Скоуп (задачи BI.ZONE)\n\n" + "\n\n".join(blocks) if blocks else ""


def parse(d: dict, versions: list[dict] | None = None, tasks: list[dict] | None = None) -> Program:
    live = [t for t in (tasks or []) if not t.get("inArchive")]
    md = "\n\n".join(x for x in (_tasks_md(live), _scope_md(d)) if x)
    if live:
        assets = sorted({a for t in live if t.get("inScope", True) for a in task_assets(t)})
        oos = sorted({a for t in live if not t.get("inScope", True) for a in task_assets(t)})
        source = "structured"
    else:
        exclude = () if d["slug"] in OWN_SLUGS else EXCLUDE
        assets, oos = split_scope(_scope_md(d), exclude)
        source = "text"
    rewards = {}
    for sev in SEVERITIES:
        cap = sev.capitalize()
        lo, hi = int(d.get(f"min{cap}Price") or 0), int(d.get(f"max{cap}Price") or 0)
        if hi:
            rewards[sev] = [lo, hi]
    vers = sorted(
        {f"{v.get('type')}:{v.get('version')}" for v in (versions or [])},
        key=lambda s: (s.split(":")[0], int(s.split(":")[1]) if s.split(":")[1].isdigit() else 0),
    )
    upd = max(filter(None, [d.get("modificationDateRules"), d.get("modificationDatePrice")]), default="")
    return Program(
        platform=NAME,
        slug=d["slug"],
        name=d.get("name") or d["slug"],
        url=f"{BASE}/companies/{d['slug']}",
        active=bool(d.get("isActive", True)),
        status="active" if d.get("isActive", True) else "inactive",
        rewards=rewards,
        assets=assets,
        out_of_scope=oos,
        assets_source=source,
        scope_md=md,
        versions=vers,
        meta={"extractor": EXTRACTOR_VERSION},
        updated_at=upd,
    )


def list_raw(c: Client) -> list[dict]:
    out, offset = [], 0
    while True:
        page = c.json(f"{API}/companies/", params={"limit": PAGE, "offset": offset})
        res = page.get("results") or []
        out.extend(x for x in res if x.get("public", True))
        offset += PAGE
        if not page.get("next") or not res:
            break
    return out


def tasks_by_company(c: Client) -> dict[str, list[dict]]:
    """«Задачи» = структурированный скоуп всех публичных программ, одним списком."""
    out: dict[str, list[dict]] = {}
    offset = 0
    while True:
        page = c.json(f"{API}/tasks/", params={"limit": PAGE, "offset": offset})
        res = page.get("results") or []
        for t in res:
            slug = (t.get("company") or {}).get("slug")
            if slug:
                out.setdefault(slug, []).append(t)
        offset += PAGE
        if not page.get("next") or not res:
            break
    return out


def fetch(c: Client, prev: dict[str, Program]) -> list[Program]:
    # без задач нельзя: иначе у всех программ скоуп «откатится» на текстовую эвристику → ложный дифф
    tasks = tasks_by_company(c)
    result = []
    for item in list_raw(c):
        slug = item["slug"]
        detail = c.json(f"{API}/companies/{slug}/")
        try:
            versions = c.json(f"{API}/companies/{slug}/program-versions/")
        except Exception:
            versions = None
        prog = parse(detail, versions if isinstance(versions, list) else None, tasks.get(slug))
        if versions is None and slug in prev:
            # версии не загрузились — не считаем это изменением
            prog.versions = prev[slug].versions
        result.append(prog)
    return result
