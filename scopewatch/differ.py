"""Сравнение снапшотов одной платформы → структурные изменения (без форматирования)."""

from __future__ import annotations

import difflib

from .models import SEVERITIES, Program, ProgramChange

DIFF_BUDGET = 1500  # символов текстового диффа скоупа
LINE_MAX = 220


def text_diff(old: str, new: str, budget: int = DIFF_BUDGET) -> str:
    lines = []
    for ln in difflib.unified_diff(old.splitlines(), new.splitlines(), lineterm="", n=0):
        if ln.startswith(("---", "+++", "@@")) or not ln[1:].strip():
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
        ch.assets_added = sorted(set(new.assets) - set(old.assets))
        ch.assets_removed = sorted(set(old.assets) - set(new.assets))
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


MISSING_RUNS = 2  # сколько прогонов подряд программы нет в списке, прежде чем сказать «пропала»


def carry_missing(old: dict[str, Program], new: list[Program]) -> list[Program]:
    """Пагинация может «потерять» программу на один прогон (список сдвинулся между страницами).
    Такую программу переносим из прошлого снапшота со счётчиком пропусков; «пропала» — только
    когда её нет MISSING_RUNS прогонов подряд."""
    present = {p.slug for p in new}
    carried = []
    for slug, o in old.items():
        if slug in present:
            continue
        misses = o.meta.get("missing", 0) + 1
        if misses < MISSING_RUNS:
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
