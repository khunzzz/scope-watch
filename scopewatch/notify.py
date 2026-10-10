"""Отправка в Telegram через Bot API (без фреймворков)."""

from __future__ import annotations

import time
from typing import Protocol

import requests

LIMIT = 4096


class Notifier(Protocol):
    def send(self, html: str) -> None: ...


class Telegram:
    def __init__(self, token: str, chat_id: str):
        self.url = f"https://api.telegram.org/bot{token}"
        self.chat_id = chat_id

    def send(self, html: str) -> None:
        if len(html) > LIMIT:
            # диффы режутся бюджетами заранее; сюда попадаем только в патологических случаях
            html = html[: LIMIT - 40].rsplit("\n", 1)[0] + "\n…(обрезано)"
            if html.count("<pre>") > html.count("</pre>"):
                html += "</pre>"
        for attempt in range(5):
            r = requests.post(f"{self.url}/sendMessage", timeout=30, json={
                "chat_id": self.chat_id,
                "text": html,
                "parse_mode": "HTML",
                "link_preview_options": {"is_disabled": True},
            })
            if r.status_code == 429:
                time.sleep(int(r.json().get("parameters", {}).get("retry_after", 5)) + 1)
                continue
            if r.status_code == 400 and "can't parse entities" in r.text:
                # сломанная разметка — шлём как plain text, чтобы не потерять событие
                r = requests.post(f"{self.url}/sendMessage", timeout=30,
                                  json={"chat_id": self.chat_id, "text": html})
            r.raise_for_status()
            time.sleep(1)  # не больше ~1 сообщения в секунду в один чат
            return
        raise RuntimeError("Telegram: слишком много 429")

    def chat_ids_hint(self) -> str:
        r = requests.get(f"{self.url}/getUpdates", timeout=30).json()
        chats = {u["message"]["chat"]["id"]: u["message"]["chat"].get("username")
                 for u in r.get("result", []) if "message" in u}
        return ", ".join(f"{cid} (@{un})" for cid, un in chats.items()) or "нет апдейтов — напиши боту /start"


class Stdout:
    """--dry-run: печать вместо отправки."""

    def send(self, html: str) -> None:
        print("─" * 60)
        print(html)
