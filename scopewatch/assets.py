"""Эвристическое извлечение ассетов (домены, wildcard, IP, мобильные приложения) из markdown.

Это именно эвристика: текст скоупа диффается отдельно, так что пропуск тут не теряет событие.
"""

from __future__ import annotations

import ipaddress
import re
from urllib.parse import parse_qs, urlparse

# расширения файлов, которые регэксп домена путает с TLD
_FILE_EXT = {
    "pdf", "png", "jpg", "jpeg", "gif", "svg", "webp", "txt", "html", "htm", "php", "asp", "aspx",
    "jsp", "js", "json", "xml", "yml", "yaml", "md", "zip", "rar", "gz", "tar", "apk", "ipa", "exe",
    "doc", "docx", "xls", "xlsx", "csv", "log", "py", "sh", "env", "bak", "sql", "conf", "ini", "cfg",
    "css", "map", "ts", "go", "rb", "java", "class", "jar", "war", "so", "dll", "git", "lock", "mp4",
}
_GTLD = {
    "com", "net", "org", "info", "biz", "pro", "app", "dev", "cloud", "online", "site", "tech",
    "store", "shop", "space", "digital", "games", "game", "live", "link", "chat", "team", "company",
    "group", "zone", "sport", "market", "media", "news", "blog", "studio", "agency", "xyz", "top",
    "club", "vip", "one", "moscow", "tatar", "host", "global", "world", "life", "network", "systems",
    "services", "solutions", "center", "academy", "school", "education", "travel", "bank", "money",
    "finance", "capital", "fund", "pay", "cash", "credit", "insurance", "auto", "car", "cars",
    "delivery", "express", "health", "clinic", "pharmacy", "work", "jobs", "career", "careers",
    "events", "tickets", "tours", "hotel", "restaurant", "food", "farm", "taxi", "toys", "kids",
    "art", "design", "fun", "social", "email", "mobi", "name", "gov", "edu", "mil", "int", "aero",
    "museum", "coop", "post", "tel", "travel", "rocks", "ninja", "guru", "expert", "partners",
    "plus", "run", "page", "wiki", "website", "tools", "software", "codes", "security", "technology",
    "рф", "рус", "москва", "дети", "сайт", "онлайн", "бел", "укр", "қаз",
}
# справочные сайты, которые часто встречаются в правилах, но целями не являются
_REFERENCE = {
    "owasp.org", "mitre.org", "first.org", "nist.gov", "cve.org", "portswigger.net",
    "hackerone.com", "bugcrowd.com", "standoff365.com", "wikipedia.org", "w3.org",
    "cvss.js", "example.com", "example.ru", "example.org", "test.com", "t.me", "telegram.me",
    "youtube.com", "youtu.be", "habr.com", "consultant.ru", "garant.ru",
}

_LABEL = r"[a-z0-9а-яё](?:[a-z0-9а-яё-]{0,61}[a-z0-9а-яё])?"
_DOMAIN = re.compile(
    rf"(?<![\w@.-])((?:\*\.)?(?:{_LABEL}\.)+(?:[a-z]{{2,24}}|рф|рус|москва|xn--[a-z0-9-]+))(?![\w-])",
    re.I,
)
_URL = re.compile(r"https?://[^\s<>()\[\]\"'`]+", re.I)
_ALT = re.compile(r"([\w*.-]+)\.\(([a-z0-9|-]+)\)", re.I)
_MD_LINK = re.compile(r"\[([^\]\n]*)\]\(([^)\s]*)\)")
_EMAIL = re.compile(r"[\w.+-]+@[\w-]+(?:\.[\w-]+)+")
_IP = re.compile(r"(?<![\d.])((?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}(?:/\d{1,2})?)(?![\d.])")


def _is_reference(host: str) -> bool:
    return any(host == r or host.endswith("." + r) for r in _REFERENCE)


def _app_from_url(u: str) -> str | None:
    p = urlparse(u)
    host = (p.hostname or "").lower()
    if host == "play.google.com":
        app = parse_qs(p.query).get("id", [None])[0]
        return f"android:{app}" if app else None
    if host in ("apps.apple.com", "itunes.apple.com"):
        m = re.search(r"/id(\d+)", p.path)
        return f"ios:id{m.group(1)}" if m else None
    if host in ("rustore.ru", "www.rustore.ru"):
        m = re.search(r"/app/([\w.]+)", p.path)
        return f"rustore:{m.group(1)}" if m else None
    if host == "appgallery.huawei.com":
        m = re.search(r"/app/(C?\d+)", p.path)
        return f"appgallery:{m.group(1)}" if m else None
    return None


def _norm_domain(d: str, exclude: tuple[str, ...]) -> str | None:
    d = d.lower().strip(".")
    tld = d.rsplit(".", 1)[-1]
    # 2-буквенные — ccTLD (кроме расширений файлов), длиннее — только из белого списка:
    # иначе в ассеты лезут window.opener, user6.rules, options.authenticationscheme
    ok_tld = (len(tld) == 2 and tld.isascii()) or tld in _GTLD or tld.startswith("xn--")
    if not ok_tld or tld in _FILE_EXT or len(d) < 4:
        return None
    bare = d.lstrip("*.")
    if _is_reference(bare) or any(bare == e or bare.endswith("." + e) for e in exclude):
        return None
    return d


def extract(md: str, exclude: tuple[str, ...] = ()) -> list[str]:
    """exclude — домены самой платформы (ссылки на доки/правила), их не считаем ассетами."""
    if not md:
        return []
    found: set[str] = set()
    # [текст](url) -> "текст url": иначе «*.[tbank.ru](https://tbank.ru)» теряет wildcard
    text = _MD_LINK.sub(r"\1 \2", md)

    for u in _URL.findall(text):
        u = u.rstrip(".,;:!?*_")
        app = _app_from_url(u)
        if app:
            found.add(app)
            continue
        host = urlparse(u).hostname
        if host:
            d = _norm_domain(host, exclude)
            if d:
                found.add(d)
    # URL уже разобраны — убираем, чтобы пути не давали мусорных «доменов»
    text = _URL.sub(" ", text)
    text = _EMAIL.sub(" ", text)
    # markdown-экранирование: \*.domain.ru
    text = text.replace("\\*", "*").replace("\\.", ".")
    # *.vkontakte.(ru|com) → *.vkontakte.ru *.vkontakte.com
    text = _ALT.sub(lambda m: " ".join(m.group(1) + "." + alt for alt in m.group(2).split("|")), text)

    for ip in _IP.findall(text):
        # приватные/служебные адреса в правилах — это примеры и запреты, не цели
        try:
            net = ipaddress.ip_network(ip, strict=False)
        except ValueError:
            continue
        if net.is_global:
            found.add(ip)
    for d in _DOMAIN.findall(text):
        if _IP.fullmatch(d):
            continue
        nd = _norm_domain(d, exclude)
        if nd:
            found.add(nd)
    return sorted(found)


# --- нормализация для сравнения между платформами ---

# зоны второго уровня, где «регистрируемый» домен — это 3 метки (shop.com.ru → shop.com.ru)
_SECOND_LEVEL = {
    "com.ru", "net.ru", "org.ru", "pp.ru", "msk.ru", "spb.ru", "msk.su", "co.uk", "org.uk",
    "com.ua", "com.kz", "org.kz", "com.by", "co.il", "com.tr", "com.cn", "com.au", "co.jp",
}


def registrable(asset: str) -> str | None:
    """*.api.tbank.ru → tbank.ru; для приложений/IP — None."""
    if ":" in asset or " " in asset or _IP.fullmatch(asset):
        return None
    labels = asset.lstrip("*.").split(".")
    if len(labels) < 2:
        return None
    n = 3 if ".".join(labels[-2:]) in _SECOND_LEVEL and len(labels) >= 3 else 2
    return ".".join(labels[-n:])


def link_tokens(assets: list[str]) -> set[str]:
    """Признаки программы для склейки между платформами: регистрируемые домены + id приложений."""
    out = set()
    for a in assets:
        if a.split(":", 1)[0] in ("android", "ios", "rustore", "appgallery"):
            out.add(a)
        elif (r := registrable(a)):
            out.add(r)
    return out


def covered(asset: str, pool: set[str]) -> bool:
    """Покрыт ли ассет списком pool: точное совпадение или wildcard (*.x.ru покрывает a.x.ru и x.ru)."""
    if asset in pool:
        return True
    bare = asset.lstrip("*.")
    for p in pool:
        if p.startswith("*."):
            base = p[2:]
            if bare == base or bare.endswith("." + base):
                return True
    return False
