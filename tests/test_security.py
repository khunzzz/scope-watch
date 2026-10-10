import logging

import requests

from scopewatch import logs
from scopewatch.config import Settings, read_secret
from scopewatch.notify import Telegram, TelegramError

TOKEN = "1234567890:AAH-abcdefghijklmnopqrstuvwxyz012345"


def test_token_from_docker_secret_file(tmp_path):
    f = tmp_path / "tg_bot_token"
    f.write_text(TOKEN + "\n")
    env = {"TG_BOT_TOKEN_FILE": str(f), "TG_BOT_TOKEN": "ignored"}
    assert read_secret(env, "TG_BOT_TOKEN") == TOKEN
    assert read_secret({"TG_BOT_TOKEN": TOKEN}, "TG_BOT_TOKEN") == TOKEN


def test_settings_repr_hides_token(tmp_path):
    s = Settings(tg_token=TOKEN, tg_chat_id="1", data_dir=tmp_path)
    assert TOKEN not in repr(s)


def test_log_filter_redacts_messages_and_tracebacks():
    log = logging.getLogger("scope-watch.test")
    rec = []

    class Keep(logging.Handler):
        def emit(self, r):
            rec.append(self.format(r))

    keep = Keep()
    keep.addFilter(logs.RedactSecrets([TOKEN]))
    log.addHandler(keep)
    try:
        log.error("url https://api.telegram.org/bot%s/sendMessage", TOKEN)
        try:
            raise ValueError(f"boom /bot{TOKEN}/x")
        except ValueError:
            log.exception("упало")
        log.error("чужой токен 9876543210:BBBbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
    finally:
        log.removeHandler(keep)
    out = "\n".join(rec)
    assert TOKEN not in out and "9876543210:" not in out and "***" in out


def test_telegram_network_error_has_no_token(monkeypatch):
    def boom(*a, **k):
        raise requests.ConnectionError(f"Max retries exceeded with url: /bot{TOKEN}/sendMessage")

    monkeypatch.setattr(requests, "post", boom)
    try:
        Telegram(TOKEN, "1").send("hi")
    except TelegramError as e:
        assert TOKEN not in str(e)
    else:
        raise AssertionError("ожидалась TelegramError")
