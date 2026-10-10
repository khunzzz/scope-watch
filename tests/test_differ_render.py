import copy

from scopewatch.differ import diff_platform
from scopewatch.linker import Linker, slug_key
from scopewatch.render import MESSAGE_MAX, render
from scopewatch.sources import standoff


def with_desc(raw, extra):
    raw = copy.deepcopy(raw)
    raw["description"] = (raw.get("description") or "") + "\n\n" + extra
    return raw


def test_no_changes(so_raw):
    p = standoff.parse(so_raw["flowwow"])
    assert diff_platform({p.slug: p}, [copy.deepcopy(p)]) == []


def test_asset_and_reward_change(so_raw):
    old = standoff.parse(so_raw["flowwow"])
    old.rewards = {"critical": [50000, 70000]}
    new = standoff.parse(with_desc(so_raw["flowwow"], "- *.newzone.flowwow.com"))
    new.rewards = {"critical": [50000, 150000]}
    [ch] = diff_platform({old.slug: old}, [new])
    assert ch.assets_added == ["*.newzone.flowwow.com"]
    assert ch.rewards == [("critical", [50000, 70000], [50000, 150000])]
    [msg] = render([ch], Linker([new]))
    assert "*.newzone.flowwow.com" in msg and "70 000 → 50 000–150 000" in msg and "📈" in msg


def test_unloaded_rewards_are_not_a_change(so_raw):
    old = standoff.parse(so_raw["flowwow"])
    old.rewards = {"critical": [1, 2]}
    new = copy.deepcopy(old)
    new.rewards = {}
    assert diff_platform({old.slug: old}, [new]) == []


def test_asset_source_switch_is_not_a_change(so_raw):
    old = standoff.parse(so_raw["flowwow"])
    new = copy.deepcopy(old)
    new.assets, new.assets_source = ["something.else.ru"], "structured"
    assert diff_platform({old.slug: old}, [new]) == []


def test_new_closed_removed(so_raw):
    a, b = standoff.parse(so_raw["flowwow"]), standoff.parse(so_raw["anyquery"])
    closed = copy.deepcopy(a)
    closed.active = False
    kinds = sorted((c.kind, c.activated) for c in diff_platform({a.slug: a}, [closed, b]))
    assert kinds == [("new", None), ("updated", False)]
    [ch] = diff_platform({a.slug: a, b.slug: b}, [b])
    assert ch.kind == "removed"


def test_slug_key():
    assert slug_key("vkplay_vk") == slug_key("vk-play") == "play"
    assert slug_key("vkontakte_vk") == slug_key("vkontakte")
    assert slug_key("gov-smev") == slug_key("fgis-smev") == "smev"


def test_same_company_linked_and_merged(so_tbank, bz_tbank):
    lk = Linker([so_tbank, bz_tbank])
    assert [p.key for p in lk.peers(so_tbank)] == [bz_tbank.key]

    # одна и та же правка на обеих платформах в одном прогоне → одно сообщение
    so_new, bz_new = copy.deepcopy(so_tbank), copy.deepcopy(bz_tbank)
    so_new.assets = sorted(so_new.assets + ["*.brandnew-tbank.ru"])
    bz_new.assets = sorted(bz_new.assets + ["*.brandnew-tbank.ru"])
    changes = diff_platform({so_tbank.slug: so_tbank}, [so_new]) + diff_platform({bz_tbank.slug: bz_tbank}, [bz_new])
    msgs = render(changes, Linker([so_new, bz_new]))
    assert len(msgs) == 1
    assert "Standoff 365" in msgs[0] and "BI.ZONE" in msgs[0]
    # добавлен на обеих сразу — нигде не «уже был»
    assert "уже были в скоупе" not in msgs[0]


def test_asset_already_on_other_platform(so_tbank, bz_tbank):
    # на Standoff добавили хост, который на BI.ZONE уже покрыт *.tinkoff.ru
    so_new = copy.deepcopy(so_tbank)
    so_new.assets = sorted(so_new.assets + ["fresh.tinkoff.ru", "totally-new.ru"])
    changes = diff_platform({so_tbank.slug: so_tbank}, [so_new])
    [msg] = render(changes, Linker([so_new, bz_tbank]))
    assert "уже были в скоупе на BI.ZONE" in msg
    fresh_part, known_part = msg.split("уже были в скоупе")
    assert "totally-new.ru" in fresh_part and "fresh.tinkoff.ru" in known_part
    assert "🔗 Также на" in msg


def test_unrelated_programs_not_linked(so_raw, bz_tbank):
    assert Linker([standoff.parse(so_raw["flowwow"]), bz_tbank]).links == []


def test_huge_change_fits_telegram(so_raw):
    old = standoff.parse(so_raw["flowwow"])
    lines = "\n".join(f"- *.zone{i}.example-target.ru строка {i}" for i in range(500))
    new = standoff.parse(dict(copy.deepcopy(so_raw["flowwow"]), description=lines))
    msgs = render(diff_platform({old.slug: old}, [new]), Linker([new]))
    assert all(len(m) < 4096 for m in msgs) and MESSAGE_MAX < 4096


def test_truncation_never_cuts_html_entity(so_raw):
    # обрезка после escape резала «&amp;» до «&am» → Telegram не парсит разметку
    from scopewatch.models import ProgramChange
    p = standoff.parse(so_raw["flowwow"])
    p.out_of_scope = ["x" * 396 + " & <b>"]
    [msg] = render([ProgramChange(kind="new", program=p)], Linker([p]))
    line = next(ln for ln in msg.splitlines() if ln.startswith("Out of scope"))
    assert line.endswith("x &amp; &lt;") and "<b>" not in line
