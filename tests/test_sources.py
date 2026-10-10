from conftest import load

from scopewatch.sources import bizone, standoff


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
