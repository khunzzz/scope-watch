"""Эвристическое извлечение ассетов (домены, wildcard, IP, мобильные приложения) из markdown.

Это именно эвристика: текст скоупа диффается отдельно, так что пропуск тут не теряет событие.
"""

from __future__ import annotations

import ipaddress
import re
from urllib.parse import parse_qs, urlparse

from .tlds import TLDS

# Поднимать при любом изменении логики извлечения: снапшоты со старой версией
# не сравниваются по ассетам (иначе улучшение экстрактора = поток ложных «новых ассетов»).
VERSION = 2

# расширения файлов, которые регэксп домена путает с TLD
_FILE_EXT = {
    "pdf", "png", "jpg", "jpeg", "gif", "svg", "webp", "txt", "html", "htm", "php", "asp", "aspx",
    "jsp", "js", "json", "xml", "yml", "yaml", "md", "zip", "rar", "gz", "tar", "apk", "ipa", "exe",
    "doc", "docx", "xls", "xlsx", "csv", "log", "py", "sh", "env", "bak", "sql", "conf", "ini", "cfg",
    "css", "map", "ts", "go", "rb", "java", "class", "jar", "war", "so", "dll", "git", "lock", "mp4",
}
# справочные сайты, которые часто встречаются в правилах, но целями не являются
_REFERENCE = {
    "owasp.org", "mitre.org", "first.org", "nist.gov", "cve.org", "portswigger.net",
    "hackerone.com", "bugcrowd.com", "wikipedia.org", "w3.org",
    "cvss.js", "example.com", "example.ru", "example.org", "test.com", "t.me", "telegram.me",
    "youtube.com", "youtu.be", "habr.com", "consultant.ru", "garant.ru",
}

# «*» внутри метки — шаблоны вида videostun*.okcdn.ru, call*-*.vkuser.net, vds-*.timeweb.ru
_LABEL = r"[a-z0-9а-яё*](?:[a-z0-9а-яё*-]{0,61}[a-z0-9а-яё*])?"
_DOMAIN = re.compile(
    rf"(?<![\w@.-])((?:\*\.)?(?:{_LABEL}\.)+(?:[a-zа-яё]{{2,24}}|xn--[a-z0-9-]+))(?![\w-])",
    re.I,
)
_URL = re.compile(r"https?://[^\s<>()\[\]\"'`]+", re.I)
# ссылки на сторы без схемы: rustore.ru/catalog/app/ru.x, play.google.com/store/apps/details?id=ru.x
_STORE_NOSCHEME = re.compile(
    r"(?<![/\w.])((?:www\.)?(?:rustore\.ru/catalog/app/|play\.google\.com/store/apps/details\?id=|"
    r"apps\.apple\.com/\S*?/id\d|appgallery\.huawei\.com/app/)[^\s<>()\[\]\"'`]*)", re.I)
_ALT = re.compile(r"([\w*.-]+)\.\(([a-z0-9|-]+)\)", re.I)
_MD_LINK = re.compile(r"\[([^\]\n]*)\]\(([^)\s]*)\)")
# ru.tinkoff.investing, com.vk.vkvideo — id Android-приложения без ссылки на стор
_PKG = re.compile(r"(?<![\w.@/-])((?:com|ru|org|net|io|app|me|tech|kz|by|uz)(?:\.[a-z][a-z0-9_]*){1,6})(?![\w-])", re.I)
_PKG_TAIL = {"android", "app", "mobile", "ios", "client"}
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
    d = re.sub(r"^\*+\.", "*.", d)            # **.x.ru → *.x.ru
    d = re.sub(r"^\*+(?=[^.*])", "", d)       # **x.ru (markdown) → x.ru
    tld = d.rsplit(".", 1)[-1]
    # зона должна быть настоящей (список IANA): отсекает window.opener, boot.ini, т.д, user6.rules
    if tld not in TLDS or tld in _FILE_EXT or len(d) < 4:
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
    text = _STORE_NOSCHEME.sub(r"https://\1", text)
    # жирный markdown: **a.ru** → a.ru (экранированное \* — это wildcard, его не трогаем)
    text = re.sub(r"(?<!\\)\*\*", " ", text)

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

    def pkg(m: re.Match) -> str:
        tok = m.group(1).lower()
        last = tok.rsplit(".", 1)[-1]
        # ru.investing.com — это домен (языковой поддомен), ru.tinkoff.sme — приложение
        if last not in TLDS or last in _PKG_TAIL or "_" in tok:
            found.add(f"android:{tok}")
            return " "
        return m.group(0)

    text = _PKG.sub(pkg, text)

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


# --- разбивка текста на «в скоупе» / «вне скоупа» ---

_OOS = re.compile(
    r"не\s*(?:нужно\s*|надо\s*|следует\s*)?(?:иска|тестир|атак|проверя|исследова)"
    r"|вне\s+(?:скоуп|област|рам|программ)|out[\s-]*of[\s-]*scope|not\s+in\s+scope"
    r"|не\s+вход[яи]т|не\s+относ[яи]тся|исключени|исключа[ею]тся|не\s+рассматрива|не\s+принима"
    r"|не\s+являются\s+цел|запрещ|excluded|exclusion|non[\s-]*qualifying|ineligible",
    re.I,
)
_HEADING = re.compile(r"^\s{0,3}(?:(#{1,6})\s*(.+?)\s*#*\s*$|\*\*(.+?)\*\*\s*:?\s*$|__(.+?)__\s*:?\s*$)")
_LABEL_LINE = re.compile(r"^\s*[-*]?\s*([^:]{3,80}):\s*(.*)$")
_BOLD_LEVEL = 7  # жирная строка-заголовок — самый нижний уровень


def split_scope(md: str, exclude: tuple[str, ...] = ()) -> tuple[list[str], list[str]]:
    """Ассеты текста, разложенные по разделам: под заголовками «НЕ искать», «Out of scope»,
    «Исключения»… — вне скоупа, остальное — в скоупе. Подзаголовок без ключевых слов наследует
    раздел родителя. Строка «Вне скоупа: a.ru, b.ru» уходит вне скоупа, не меняя раздел.
    Если ассет есть в обоих, считаем его в скоупе."""
    inside, outside = [], []
    stack: list[tuple[int, bool]] = []  # (уровень заголовка, вне скоупа?)
    for line in (md or "").splitlines():
        h = _HEADING.match(line)
        if h:
            hashes, title = h.group(1), next(g for g in h.groups()[1:] if g)
            level = len(hashes) if hashes else _BOLD_LEVEL
            while stack and stack[-1][0] >= level:
                stack.pop()
            parent_oos = stack[-1][1] if stack else False
            stack.append((level, bool(_OOS.search(title)) or parent_oos))
            continue
        lab = _LABEL_LINE.match(line)
        if lab and _OOS.search(lab.group(1)):
            outside.append(lab.group(2))
            continue
        (outside if stack and stack[-1][1] else inside).append(line)
    ins = extract("\n".join(inside), exclude)
    out = [a for a in extract("\n".join(outside), exclude) if a not in set(ins)]
    return ins, out


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
