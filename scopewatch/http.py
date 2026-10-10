"""HTTP-сессия: ретраи на 5xx, таймауты, вежливая пауза между запросами."""

from __future__ import annotations

import json
import re
import time

import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

UA = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"
TIMEOUT = 30
DELAY = 0.4

_NEXT_DATA = re.compile(r'<script id="__NEXT_DATA__"[^>]*>(.*?)</script>', re.S)


class Client:
    def __init__(self, delay: float = DELAY):
        self.delay = delay
        self._last = 0.0
        self.s = requests.Session()
        self.s.headers.update({
            "User-Agent": UA,
            "Accept-Language": "ru-RU,ru;q=0.9,en;q=0.8",
            "X-Bug-Bounty": "sayfu",
        })
        retry = Retry(total=3, backoff_factor=2, status_forcelist=(429, 500, 502, 503, 504),
                      allowed_methods=("GET",), respect_retry_after_header=True)
        self.s.mount("https://", HTTPAdapter(max_retries=retry))

    def get(self, url: str, **kw) -> requests.Response:
        wait = self.delay - (time.monotonic() - self._last)
        if wait > 0:
            time.sleep(wait)
        try:
            r = self.s.get(url, timeout=TIMEOUT, **kw)
        finally:
            self._last = time.monotonic()
        r.raise_for_status()
        return r

    def json(self, url: str, **kw):
        return self.get(url, **kw).json()

    def next_data(self, url: str, **kw) -> dict:
        """pageProps из Next.js-страницы."""
        m = _NEXT_DATA.search(self.get(url, **kw).text)
        if not m:
            raise ValueError(f"нет __NEXT_DATA__ на {url}")
        return json.loads(m.group(1))["props"]["pageProps"]
