"""BI.ZONE Bug Bounty: публичный DRF API /api/bug-bounty/companies/."""

from __future__ import annotations

import logging

from ..assets import VERSION as EXTRACTOR_VERSION, extract, split_scope
from ..http import Client
from ..models import NO_BOUNTY, SEVERITIES, Fetched, Program

NAME = "bizone"
TITLE = "BI.ZONE"
BASE = "https://bugbounty.bi.zone"
API = f"{BASE}/api/bug-bounty"
# bi.zone упоминается в правилах чужих программ как ссылка; в задачах (явный скоуп) не фильтруем
EXCLUDE = ("bi.zone",)
OWN_SLUGS = {"bi-zone-bug-bounty"}
PAGE = 100

log = logging.getLogger("scope-watch")


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
        items = [a + NO_BOUNTY for a in items]
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


def _paginate(c: Client, url: str) -> list[dict]:
    """Все страницы DRF (limit/offset) с проверкой целостности. Offset-пагинация на живом
    списке теряет элементы: удалили/заархивировали задачу между страницами — следующая
    страница сдвигается, и одна задача пропадает (= ложное «ассет убран», потом «добавлен»).
    Поэтому count не должен меняться за проход, а уникальных id должно быть ровно count.
    Не сошлось — ещё одна попытка, потом ошибка (площадка «упала» на этот прогон)."""
    for _ in range(2):
        by_id: dict = {}
        counts = set()
        offset = 0
        while True:
            page = c.json(url, params={"limit": PAGE, "offset": offset})
            res = page.get("results") or []
            counts.add(page.get("count"))
            by_id.update((x["id"], x) for x in res)
            offset += PAGE
            if not page.get("next") or not res:
                break
        if counts == {len(by_id)}:
            return list(by_id.values())
    raise RuntimeError(f"{url}: список менялся во время пагинации (count {counts}, получено {len(by_id)})")


def list_raw(c: Client) -> list[dict]:
    return [x for x in _paginate(c, f"{API}/companies/") if x.get("public", True)]


def tasks_by_company(c: Client) -> dict[str, list[dict]]:
    """«Задачи» = структурированный скоуп всех публичных программ, одним списком."""
    out: dict[str, list[dict]] = {}
    for t in _paginate(c, f"{API}/tasks/"):
        slug = (t.get("company") or {}).get("slug")
        if slug:
            out.setdefault(slug, []).append(t)
    return out


def fetch(c: Client, prev: dict[str, Program]) -> Fetched:
    # без задач нельзя: иначе у всех программ скоуп «откатится» на текстовую эвристику → ложный дифф
    tasks = tasks_by_company(c)
    items = list_raw(c)
    result, failed = [], []
    for item in items:
        slug = item["slug"]
        try:
            detail = c.json(f"{API}/companies/{slug}/")
        except Exception as e:  # noqa: BLE001 — сбой одной программы не должен ронять площадку
            log.warning("bizone/%s: детали не загрузились (%s: %s)", slug, type(e).__name__, e)
            failed.append(slug)
            if slug in prev:
                # оставляем прошлый снапшот: ни ложного «пропала», ни ложного диффа
                kept = Program.from_dict(prev[slug].to_dict())
                kept.meta.pop("missing", None)   # в списке она есть
                result.append(kept)
            continue  # новая — объявим в следующий прогон, когда детали загрузятся
        try:
            versions = c.json(f"{API}/companies/{slug}/program-versions/")
        except Exception:
            versions = None
        prog = parse(detail, versions if isinstance(versions, list) else None, tasks.get(slug))
        if versions is None and slug in prev:
            # версии не загрузились — не считаем это изменением
            prog.versions = prev[slug].versions
        result.append(prog)
    return Fetched(result, total=len(items), failed=failed)
