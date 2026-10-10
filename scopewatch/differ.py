"""Сравнение снапшотов одной платформы → структурные изменения (без форматирования)."""

from __future__ import annotations

import difflib

from .models import NO_BOUNTY, SEVERITIES, Program, ProgramChange

DIFF_BUDGET = 1500  # символов текстового диффа скоупа
LINE_MAX = 220


def text_diff(old: str, new: str, budget: int = DIFF_BUDGET) -> str:
    lines = []
    for i, ln in enumerate(difflib.unified_diff(old.splitlines(), new.splitlines(), lineterm="", n=0)):
        # первые две строки — заголовки ---/+++; по префиксу их не отличить от удалённой
        # строки «-- x» или добавленной «++ x», поэтому пропускаем по позиции
        if i < 2 or ln.startswith("@@") or not ln[1:].strip():
            continue
        lines.append(ln if len(ln) <= LINE_MAX else ln[:LINE_MAX] + "…")
    out, used = [], 0
    for i, ln in enumerate(lines):
        if used + len(ln) + 1 > budget:
            out.append(f"…ещё {len(lines) - i} строк")
            break
        out.append(ln)
        used += len(ln) + 1
    return "\n".join(out)


def compare(old: Program, new: Program) -> ProgramChange:
    ch = ProgramChange(kind="updated", program=new, old=old)

    if old.active != new.active:
        ch.activated = new.active
    if old.name != new.name:
        ch.renamed_from = old.name

    # сменился источник ассетов (текст ↔ API) или версия экстрактора — списки несравнимы,
    # был бы ложный дифф; реальные правки всё равно видны в диффе текста
    same_basis = (old.assets_source == new.assets_source
                  and old.meta.get("extractor") == new.meta.get("extractor"))
    if same_basis:
        added, removed = set(new.assets) - set(old.assets), set(old.assets) - set(new.assets)
        # «a.ru» → «a.ru (без выплат)» и обратно — это смена выплат по ассету, а не новый ассет
        ch.bounty_lost = sorted(a for a in removed if a + NO_BOUNTY in added)
        ch.bounty_gained = sorted(a[: -len(NO_BOUNTY)] for a in removed
                                  if a.endswith(NO_BOUNTY) and a[: -len(NO_BOUNTY)] in added)
        added -= {a + NO_BOUNTY for a in ch.bounty_lost} | set(ch.bounty_gained)
        removed -= set(ch.bounty_lost) | {a + NO_BOUNTY for a in ch.bounty_gained}
        ch.assets_added, ch.assets_removed = sorted(added), sorted(removed)
        ch.oos_added = sorted(set(new.out_of_scope) - set(old.out_of_scope))
        ch.oos_removed = sorted(set(old.out_of_scope) - set(new.out_of_scope))

    # выплаты сравниваем, только если обе стороны реально загружены
    if old.rewards and new.rewards and old.rewards != new.rewards:
        ch.rewards = [(s, old.rewards.get(s), new.rewards.get(s))
                      for s in SEVERITIES if old.rewards.get(s) != new.rewards.get(s)]

    if old.versions:
        ch.new_versions = [v for v in new.versions if v not in old.versions]

    if old.scope_md != new.scope_md:
        ch.text_diff = text_diff(old.scope_md, new.scope_md)
    return ch


def carry_missing(old: dict[str, Program], new: list[Program], runs: int) -> list[Program]:
    """Пагинация может «потерять» программу на один прогон (список сдвинулся между страницами).
    Такую программу переносим из прошлого снапшота со счётчиком пропусков; «пропала» — только
    когда её нет runs прогонов подряд (Settings.missing_runs)."""
    present = {p.slug for p in new}
    carried = []
    for slug, o in old.items():
        if slug in present:
            continue
        misses = o.meta.get("missing", 0) + 1
        if misses < runs:
            c = Program.from_dict(o.to_dict())
            c.meta["missing"] = misses
            carried.append(c)
    return new + carried


def diff_platform(old: dict[str, Program], new: list[Program]) -> list[ProgramChange]:
    changes = []
    seen = set()
    for p in new:
        seen.add(p.slug)
        o = old.get(p.slug)
        if o is None:
            if p.active:
                changes.append(ProgramChange(kind="new", program=p))
            continue
        ch = compare(o, p)
        if not ch.is_empty():
            changes.append(ch)
    for slug, o in old.items():
        if slug not in seen and o.active:
            changes.append(ProgramChange(kind="removed", program=o, old=o))
    return changes
