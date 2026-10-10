"""End-to-end прогон раннера на фейковых источниках (без сети)."""

import copy
import types

import pytest

from scopewatch import runner
from scopewatch.config import Settings
from scopewatch.sources import standoff


class Collect:
    def __init__(self):
        self.sent = []

    def send(self, html):
        self.sent.append(html)


@pytest.fixture
def env(tmp_path, monkeypatch, so_raw):
    state = {"programs": [standoff.parse(so_raw["flowwow"])], "fail": False}

    def fetch(client, prev):
        if state["fail"]:
            raise ConnectionError("boom")
        return copy.deepcopy(state["programs"])

    fake = types.SimpleNamespace(NAME="standoff", TITLE="Standoff 365", fetch=fetch)
    monkeypatch.setattr(runner, "SOURCES", {"standoff": fake})
    settings = Settings(tg_token=None, tg_chat_id=None, data_dir=tmp_path, fail_alert_after=2)
    return settings, state, so_raw


def test_seed_then_change_then_quiet(env):
    settings, state, so_raw = env
    out = Collect()
    runner.run(settings, out)
    assert len(out.sent) == 1 and "🌱" in out.sent[0]

    state["programs"].append(standoff.parse(so_raw["anyquery"]))
    out = Collect()
    runner.run(settings, out)
    assert len(out.sent) == 1 and "🆕" in out.sent[0] and "AnyQuery" in out.sent[0]

    out = Collect()
    runner.run(settings, out)
    assert out.sent == []


def test_dry_run_saves_nothing(env):
    settings, state, _ = env
    runner.run(settings, Collect())
    state["programs"][0].active = False
    out = Collect()
    runner.run(settings, out, dry_run=True)
    assert any("🔒" in m for m in out.sent)
    out = Collect()
    runner.run(settings, out)          # изменение не потерялось после dry-run
    assert any("🔒" in m for m in out.sent)


def test_failure_keeps_snapshot_and_alerts_once(env):
    settings, state, _ = env
    runner.run(settings, Collect())
    state["fail"] = True
    sent = []
    for _ in range(3):
        out = Collect()
        runner.run(settings, out)
        sent += out.sent
    assert len([m for m in sent if "⚠️" in m]) == 1
    state["fail"] = False
    out = Collect()
    runner.run(settings, out)
    assert len(out.sent) == 1 and "✅" in out.sent[0]   # и никаких ложных «программа пропала»


def test_truncated_list_is_treated_as_failure(env):
    settings, state, so_raw = env
    state["programs"].append(standoff.parse(so_raw["anyquery"]))
    runner.run(settings, Collect())
    state["programs"] = state["programs"][:1]   # из 2 программ пришла 1 (<70%)
    out = Collect()
    runner.run(settings, out)
    assert out.sent == []
    assert runner.Store(settings.data_dir).load("standoff").keys() == {"flowwow", "anyquery"}
