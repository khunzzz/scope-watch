"""Модели: нормализованная программа и структурное изменение программы."""

from __future__ import annotations

from dataclasses import asdict, dataclass, field
from typing import Literal

SEVERITIES = ("low", "medium", "high", "critical")


@dataclass
class Program:
    platform: str
    slug: str
    name: str
    url: str
    active: bool
    status: str = ""
    # severity -> [min, max]; пустой dict = выплаты неизвестны (не загружались)
    rewards: dict[str, list[int]] = field(default_factory=dict)
    assets: list[str] = field(default_factory=list)
    # явно помеченное платформой как out of scope (сейчас только BI.ZONE)
    out_of_scope: list[str] = field(default_factory=list)
    # откуда ассеты: "structured" (API платформы) или "text" (эвристика по markdown)
    assets_source: str = "text"
    scope_md: str = ""
    # BI.ZONE: ["rule:3", "price:2", ...]
    versions: list[str] = field(default_factory=list)
    updated_at: str = ""
    # служебное, в дифф не идёт (время загрузки выплат и т.п.)
    meta: dict = field(default_factory=dict)

    @property
    def key(self) -> str:
        return f"{self.platform}/{self.slug}"

    @property
    def max_reward(self) -> int:
        return max((r[1] for r in self.rewards.values() if r), default=0)

    def to_dict(self) -> dict:
        return asdict(self)

    @classmethod
    def from_dict(cls, d: dict) -> Program:
        known = cls.__dataclass_fields__
        return cls(**{k: v for k, v in d.items() if k in known})


RewardDelta = tuple[str, list[int] | None, list[int] | None]  # (severity, было, стало)


@dataclass
class ProgramChange:
    kind: Literal["new", "updated", "removed"]
    program: Program                    # актуальная версия (для removed — последняя известная)
    old: Program | None = None
    activated: bool | None = None       # True — переоткрыта, False — закрыта
    renamed_from: str | None = None
    assets_added: list[str] = field(default_factory=list)
    assets_removed: list[str] = field(default_factory=list)
    oos_added: list[str] = field(default_factory=list)
    oos_removed: list[str] = field(default_factory=list)
    rewards: list[RewardDelta] = field(default_factory=list)
    new_versions: list[str] = field(default_factory=list)
    text_diff: str = ""

    def is_empty(self) -> bool:
        if self.kind != "updated":
            return False
        return not (self.activated is not None or self.renamed_from or self.assets_added
                    or self.assets_removed or self.oos_added or self.oos_removed
                    or self.rewards or self.new_versions or self.text_diff)
