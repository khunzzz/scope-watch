import json
from pathlib import Path

import pytest

from scopewatch.sources import bizone, standoff

FX = Path(__file__).parent / "fixtures"


def load(name):
    return json.loads((FX / name).read_text(encoding="utf-8"))


@pytest.fixture
def so_raw():
    """Сырые программы Standoff (как в списке /programs) по slug."""
    out = {p["slug"]: p for p in load("standoff_list.json")}
    out["tbank"] = load("standoff_tbank.json")
    return out


@pytest.fixture
def bz_tasks():
    return load("bizone_tasks.json")


@pytest.fixture
def so_tbank(so_raw):
    return standoff.parse(so_raw["tbank"])


@pytest.fixture
def bz_tbank(bz_tasks):
    return bizone.parse(load("bizone_tbank.json"), [{"type": "rule", "version": 1}], bz_tasks["tbank"])
