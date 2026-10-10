"""Логирование с вырезанием секретов: токен бота не должен попасть в docker logs ни из
сообщения, ни из трейсбека (исключения requests содержат URL /bot<token>/…)."""

from __future__ import annotations

import logging
import re

# формат токена Telegram-бота: <id>:<35 символов>
_BOT_TOKEN = re.compile(r"\b\d{6,12}:[A-Za-z0-9_-]{30,}\b")
MASK = "***"


class RedactSecrets(logging.Filter):
    def __init__(self, secrets: list[str | None]):
        super().__init__()
        self.secrets = [s for s in secrets if s]

    def redact(self, text: str) -> str:
        for s in self.secrets:
            text = text.replace(s, MASK)
        return _BOT_TOKEN.sub(MASK, text)

    def filter(self, record: logging.LogRecord) -> bool:
        record.msg, record.args = self.redact(record.getMessage()), ()
        if record.exc_info and not record.exc_text:
            record.exc_text = logging.Formatter().formatException(record.exc_info)
        if record.exc_text:
            record.exc_text = self.redact(record.exc_text)
        return True


def setup(verbose: bool, secrets: list[str | None]) -> None:
    logging.basicConfig(level=logging.DEBUG if verbose else logging.INFO,
                        format="%(asctime)s %(levelname)s %(message)s")
    logging.getLogger("urllib3").setLevel(logging.WARNING)
    # фильтр на обработчике, а не на логгере: так он видит записи всех дочерних логгеров
    for h in logging.getLogger().handlers:
        h.addFilter(RedactSecrets(secrets))
