"""Изменения → HTML-сообщения Telegram. Одно сообщение на компанию: если одна и та же
компания обновилась на обеих платформах в одном прогоне, это придёт одним сообщением."""

from __future__ import annotations

from html import escape

from .assets import covered
from .linker import Linker
from .models import SEVERITIES, Program, ProgramChange
from .sources import TITLES

ASSET_BUDGET = 1200
MESSAGE_MAX = 4000
SEV = {"low": "low", "medium": "medium", "high": "high", "critical": "crit"}


def money(n: int) -> str:
    return f"{n:,}".replace(",", " ")


def rng(r: list[int] | None) -> str:
    if not r:
        return "—"
    lo, hi = r
    return money(hi) if not lo or lo == hi else f"{money(lo)}–{money(hi)}"


def rewards_line(p: Program) -> str:
    parts = [f"{SEV[s]} {rng(p.rewards[s])}" for s in SEVERITIES if p.rewards.get(s)]
    return "💰 " + " · ".join(parts) + " ₽" if parts else ""


def asset_block(items: list[str], budget: int = ASSET_BUDGET) -> str:
    out, used = [], 0
    for i, a in enumerate(items):
        if used + len(a) + 1 > budget:
            out.append(f"…ещё {len(items) - i}")
            break
        out.append(a)
        used += len(a) + 1
    return "<pre>" + escape("\n".join(out)) + "</pre>"


def link(p: Program, text: str | None = None) -> str:
    return f'<a href="{escape(p.url)}">{escape(text or TITLES[p.platform])}</a>'


def _split_known(assets: list[str], peers: list[Program]) -> tuple[list[str], list[str]]:
    """Делит ассеты на действительно новые для компании и уже бывшие в скоупе на другой платформе."""
    pool = {a for p in peers for a in p.assets}
    fresh = [a for a in assets if not covered(a, pool)]
    known = [a for a in assets if covered(a, pool)]
    return fresh, known


def _scope_summary(p: Program, peers: list[Program]) -> list[str]:
    """Выплаты + ассеты (новые для компании отдельно от уже известных) + out of scope."""
    lines = []
    if p.rewards:
        lines.append(rewards_line(p))
    if p.assets:
        fresh, known = _split_known(p.assets, peers)
        src = "" if p.assets_source == "structured" else ", из текста"
        lines.append(f"Ассеты ({len(p.assets)}{src}):")
        lines.append(asset_block(fresh or p.assets))
        if peers and known and fresh:
            lines.append(f"ещё {len(known)} уже были в скоупе на "
                         + ", ".join(TITLES[x.platform] for x in peers))
    else:
        lines.append("Ассеты из текста не извлеклись, смотри описание.")
    if p.out_of_scope:
        lines.append("Out of scope: " + escape(", ".join(p.out_of_scope)[:400]))
    return lines


def _section(ch: ProgramChange, peers: list[Program]) -> str:
    p = ch.program
    head = f"<b>{escape(TITLES[p.platform])}</b> · {link(p, p.name)}"
    lines: list[str] = []

    if ch.kind == "new":
        lines.append("🆕 <b>Новая программа</b>")
        lines += _scope_summary(p, peers)
        return head + "\n" + "\n".join(lines)

    if ch.kind == "removed":
        return head + "\n🗑 Пропала из публичного списка (стала приватной или удалена)."

    reopened = ch.activated is True
    if reopened:
        # за время закрытия скоуп мог измениться целиком — показываем его, как у новой программы
        lines.append("♻️ <b>Снова активна</b>")
        lines += _scope_summary(p, peers)
    elif ch.activated is False:
        lines.append(f"🔒 <b>Закрыта</b> ({escape(p.status)})")
    if ch.renamed_from:
        lines.append(f"✏️ Было название: {escape(ch.renamed_from)}")
    if ch.assets_added and not reopened:
        fresh, known = _split_known(ch.assets_added, peers)
        lines.append(f"➕ <b>Новые ассеты ({len(ch.assets_added)})</b>")
        if fresh:
            lines.append(asset_block(fresh))
        if known:
            where = ", ".join(TITLES[x.platform] for x in peers)
            lines.append(f"уже были в скоупе на {where}:")
            lines.append(asset_block(known, 400))
    if ch.assets_removed:
        lines.append(f"➖ Убраны ассеты ({len(ch.assets_removed)}):")
        lines.append(asset_block(ch.assets_removed, 500))
    if ch.bounty_lost:
        lines.append(f"💸 Больше не платят за ({len(ch.bounty_lost)}):")
        lines.append(asset_block(ch.bounty_lost, 400))
    if ch.bounty_gained:
        lines.append(f"💰 Теперь платят за ({len(ch.bounty_gained)}):")
        lines.append(asset_block(ch.bounty_gained, 400))
    if ch.oos_added:
        lines.append("🚫 Добавлено в out of scope:")
        lines.append(asset_block(ch.oos_added, 400))
    if ch.oos_removed:
        lines.append("✅ Убрано из out of scope:")
        lines.append(asset_block(ch.oos_removed, 400))
    if ch.rewards:
        lines.append("💰 <b>Выплаты</b>")
        for sev, a, b in ch.rewards:
            arrow = "📈" if (b or [0, 0])[1] > (a or [0, 0])[1] else "📉"
            lines.append(f"{arrow} {SEV[sev]}: {rng(a)} → {rng(b)}")
    if ch.new_versions:
        kinds = {"rule": "правила", "price": "выплаты"}
        vers = ", ".join(f"{kinds.get(k, k)} v{n}" for k, _, n in (v.partition(":") for v in ch.new_versions))
        lines.append(f"🗂 Новая версия: {escape(vers)}")
    if ch.text_diff:
        # если уже показали ассеты, дифф текста сокращаем
        budget = 700 if (ch.assets_added or ch.assets_removed) else 1500
        diff = ch.text_diff if len(ch.text_diff) <= budget else ch.text_diff[:budget].rsplit("\n", 1)[0] + "\n…"
        lines.append("📝 Текст скоупа/правил:")
        lines.append(f"<pre>{escape(diff)}</pre>")
    return head + "\n" + "\n".join(lines)


def _icon(changes: list[ProgramChange]) -> str:
    if any(c.kind == "new" for c in changes):
        return "🆕"
    if any(c.assets_added for c in changes):
        return "🎯"
    if any(c.activated is False or c.kind == "removed" for c in changes):
        return "🔒"
    return "🔔"


def render(changes: list[ProgramChange], linker: Linker) -> list[str]:
    groups: dict[str, list[ProgramChange]] = {}
    for ch in changes:
        groups.setdefault(linker.group_key(ch.program), []).append(ch)

    # «уже было на другой платформе» считаем по состоянию ДО этого прогона:
    # если ассет добавили на обеих площадках сразу, он новый для обеих
    before = {c.program.key: c.old for c in changes}

    def peers_before(p: Program) -> list[Program]:
        out = []
        for peer in linker.peers(p):
            state = before[peer.key] if peer.key in before else peer
            if state is not None:
                out.append(state)
        return out

    messages = []
    for chs in groups.values():
        name = chs[0].program.name
        changed = {c.program.key for c in chs}
        sections = [_section(c, peers_before(c.program)) for c in chs]
        # программы этой компании на других платформах, которые в этот раз не менялись
        others = {p.key: p for c in chs for p in linker.peers(c.program) if p.key not in changed}
        footer = ""
        if others:
            footer = "\n\n🔗 Также на: " + ", ".join(f"{escape(p.name)} ({link(p)})" for p in others.values())
        head = f"{_icon(chs)} <b>{escape(name)}</b>\n\n"
        msg = head + "\n\n".join(sections) + footer
        if len(msg) <= MESSAGE_MAX:
            messages.append(msg)
        else:  # не влезло в одно сообщение — по сообщению на платформу
            messages += [head + s for s in sections]
    return messages


def mass_change(platform: str, changes: list[ProgramChange]) -> str:
    kinds = {"new": "🆕 новые", "removed": "🗑 пропали", "updated": "🔔 изменились"}
    lines = [f"⚠️ <b>{escape(TITLES[platform])}</b>: за один прогон изменилось сразу {len(changes)} программ. "
             "Похоже, площадка поменяла формат. Поштучно не рассылаю, детали: <code>scope-watch history</code>."]
    for kind, title in kinds.items():
        names = [c.program.name for c in changes if c.kind == kind]
        if names:
            shown = ", ".join(names[:25]) + (f" …ещё {len(names) - 25}" if len(names) > 25 else "")
            lines.append(f"{title} ({len(names)}): {escape(shown)}")
    return "\n".join(lines)


def seeded_summary(lines: list[str]) -> str:
    return "🌱 <b>scope-watch</b>: база сохранена, по ней уведомлений не будет.\n" + "\n".join(lines)


def health_alert(platform: str, fails: int, error: str) -> str:
    return (f"⚠️ <b>{escape(TITLES[platform])}</b> не отвечает {fails} проверок подряд.\n"
            f"<code>{escape(error[:300])}</code>\nВозможно, поменяли API или вёрстку.")


def health_ok(platform: str) -> str:
    return f"✅ <b>{escape(TITLES[platform])}</b> снова доступен."
