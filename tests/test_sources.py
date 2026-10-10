import copy

import pytest
import requests
from conftest import load

from scopewatch.sources import bizone, standoff


class FakeClient:
    """Отдаёт ответы по URL; URL из broken — HTTP-ошибка."""

    def __init__(self, routes, broken=()):
        self.routes, self.broken, self.calls = routes, set(broken), []

    def _get(self, url, params=None):
        self.calls.append(url)
        if url in self.broken:
            raise requests.HTTPError(f"500 for {url}")
        r = self.routes[url]
        return r(params) if callable(r) else copy.deepcopy(r)

    json = next_data = _get


def bizone_routes(details):
    tasks = load("bizone_tasks.json")
    routes = {
        f"{bizone.API}/tasks/": {"count": sum(map(len, tasks.values())), "next": None,
                                 "results": [t for ts in tasks.values() for t in ts]},
        f"{bizone.API}/companies/": {"count": len(details), "next": None,
                                     "results": [{"id": d["slug"], "slug": d["slug"], "public": True} for d in details]},
    }
    for d in details:
        routes[f"{bizone.API}/companies/{d['slug']}/"] = d
        routes[f"{bizone.API}/companies/{d['slug']}/program-versions/"] = [{"type": "rule", "version": 1}]
    return routes


def test_standoff_rewards():
    r = standoff._rewards(load("standoff_flowwow_page.json")["reward"])
    assert r["critical"] == [50000, 70000] and r["low"] == [5000, 10000]


def test_standoff_scope_includes_special_rules(so_raw):
    raw = dict(so_raw["flowwow"], description="", specialRules="Скоуп: *.special.ru")
    assert standoff.parse(raw).assets == ["*.special.ru"]


def test_standoff_finished_is_inactive(so_raw):
    assert not standoff.parse(dict(so_raw["flowwow"], finished=True)).active


def test_bizone_hosts_from_task_descriptions(bz_tbank):
    assert bz_tbank.assets_source == "structured"
    assert "*.tinkoff.ru" in bz_tbank.assets
    assert bz_tbank.out_of_scope  # у T-Bank есть out-of-scope задачи
    assert "## Скоуп (задачи BI.ZONE)" in bz_tbank.scope_md


def test_bizone_task_without_hosts_keeps_title():
    t = {"textTask": "Android App", "description": "", "inScope": True, "eligibility": True}
    assert bizone.task_assets(t) == ["Android App"]
    t = {"textTask": "x", "description": "a.corp.ru", "inScope": True, "eligibility": False}
    assert bizone.task_assets(t) == ["a.corp.ru (без выплат)"]


def test_bizone_without_tasks_falls_back_to_text():
    p = bizone.parse(load("bizone_kaspersky.json"), [{"type": "rule", "version": 1}, {"type": "price", "version": 2}])
    assert p.assets_source == "text" and "www.kaspersky.ru" in p.assets
    assert p.versions == ["price:2", "rule:1"]
    assert p.rewards["critical"][1] > 0


def test_bizone_one_broken_program_does_not_break_platform():
    kasp, tbank = load("bizone_kaspersky.json"), load("bizone_tbank.json")
    routes = bizone_routes([kasp, tbank])
    prev = {p.slug: p for p in bizone.fetch(FakeClient(routes), {}).programs}
    prev["kaspersky"].meta["missing"] = 1           # прошлый прогон её не было в списке

    got = bizone.fetch(FakeClient(routes, broken=[f"{bizone.API}/companies/kaspersky/"]), prev)
    assert got.failed == ["kaspersky"] and got.total == 2
    by = {p.slug: p for p in got.programs}
    # сломанная — ровно прошлый снапшот (но уже не «пропавшая»), соседняя — обновлена
    assert by["kaspersky"].scope_md == prev["kaspersky"].scope_md
    assert "missing" not in by["kaspersky"].meta
    assert by["tbank"].assets == prev["tbank"].assets

    # новая программа со сломанными деталями пропускается до следующего прогона
    got = bizone.fetch(FakeClient(routes, broken=[f"{bizone.API}/companies/kaspersky/"]), {})
    assert [p.slug for p in got.programs] == ["tbank"] and got.failed == ["kaspersky"]


def test_standoff_broken_reward_page_keeps_old_rewards(so_raw):
    raw = [so_raw["flowwow"], so_raw["anyquery"]]
    page = load("standoff_flowwow_page.json")
    routes = {
        f"{standoff.BASE}/programs": lambda params: {"programs": copy.deepcopy(raw) if params["page"] == 1 else [],
                                                     "total": 1},
        f"{standoff.BASE}/programs/flowwow": page,
        f"{standoff.BASE}/programs/anyquery": page,
    }
    first = standoff.fetch(FakeClient(routes), {})
    assert not first.failed
    prev = {p.slug: p for p in first.programs}
    prev["flowwow"].meta["rewards_at"] = 0           # выплаты протухли → страницу надо перечитать
    got = standoff.fetch(FakeClient(routes, broken=[f"{standoff.BASE}/programs/flowwow"]), prev)
    assert got.failed == ["flowwow"]
    by = {p.slug: p for p in got.programs}
    assert by["flowwow"].rewards == prev["flowwow"].rewards and by["flowwow"].meta["rewards_at"] == 0
    # новая программа со сломанной страницей выплат всё равно в списке (скоуп есть в самом списке)
    got = standoff.fetch(FakeClient(routes, broken=[f"{standoff.BASE}/programs/anyquery"]), {})
    by = {p.slug: p for p in got.programs}
    assert set(by) == {"flowwow", "anyquery"} and by["anyquery"].rewards == {}
    assert "rewards_at" not in by["anyquery"].meta


def test_bizone_pagination_shift_is_detected(monkeypatch):
    tasks = [t for ts in load("bizone_tasks.json").values() for t in ts]
    monkeypatch.setattr(bizone, "PAGE", 10)
    state = {"pages": 0}

    def page(params):
        # после первой страницы одну задачу заархивировали: список сдвинулся
        cur = tasks if state["pages"] == 0 else tasks[1:]
        state["pages"] += 1
        off = params["offset"]
        return {"count": len(cur), "next": "more" if off + 10 < len(cur) else None,
                "results": copy.deepcopy(cur[off:off + 10])}

    got = bizone.tasks_by_company(FakeClient({f"{bizone.API}/tasks/": page}))
    # первая попытка отброшена, вторая прошла по стабильному списку: ничего не потеряно
    assert sorted(t["id"] for ts in got.values() for t in ts) == sorted(t["id"] for t in tasks[1:])

    def dup(params):   # нестабильный порядок: на двух страницах одна и та же задача
        off = params["offset"]
        res = tasks[off:off + 10] if off else tasks[:9] + [tasks[0]]
        return {"count": 20, "next": "more" if off == 0 else None, "results": copy.deepcopy(res)}

    with pytest.raises(RuntimeError, match="пагинации"):
        bizone.tasks_by_company(FakeClient({f"{bizone.API}/tasks/": dup}))


def test_standoff_empty_page_mid_list_is_error(so_raw):
    def pages(params):
        progs = {1: [so_raw["flowwow"]], 2: [], 3: [so_raw["anyquery"]]}.get(params["page"], [])
        return {"programs": copy.deepcopy(progs), "total": 3}

    with pytest.raises(ValueError, match="пустая страница 2 из 3"):
        standoff.list_raw(FakeClient({f"{standoff.BASE}/programs": pages}))
    # обычный конец списка: страница после последней пустая — не ошибка
    ok = lambda params: {"programs": [so_raw["flowwow"]] if params["page"] == 1 else [], "total": 1}  # noqa: E731
    assert [p["slug"] for p in standoff.list_raw(FakeClient({f"{standoff.BASE}/programs": ok}))] == ["flowwow"]
