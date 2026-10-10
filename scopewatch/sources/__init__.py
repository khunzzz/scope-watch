"""Источники: у каждого модуля NAME, TITLE и fetch(client, prev) -> Fetched."""

from . import bizone, standoff

SOURCES = {standoff.NAME: standoff, bizone.NAME: bizone}
TITLES = {name: mod.TITLE for name, mod in SOURCES.items()}
