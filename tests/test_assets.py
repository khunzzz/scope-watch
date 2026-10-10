from scopewatch.assets import covered, extract, link_tokens, registrable
from scopewatch.sources import standoff


def test_wildcards_inside_markdown_links(so_tbank):
    for d in ("*.tbank.ru", "*.tinkoff.ru", "*.tcsbank.ru", "*.tinkoffinsurance.ru"):
        assert d in so_tbank.assets


def test_hosts_and_store_apps(so_raw):
    a = standoff.parse(so_raw["flowwow"]).assets
    assert "flowwow.com" in a and "android:com.flowwow" in a
    assert not any("standoff365" in x for x in a)


def test_noise_filtered():
    md = ("Пиши на security@corp.ru, см. https://owasp.org/x, файл report.pdf, "
          "window.opener, user6.rules, IP 10.0.0.0/8 и 192.168.1.1, но 81.19.64.0/24 и \\*.corp.ru")
    assert extract(md) == ["*.corp.ru", "81.19.64.0/24"]


def test_alternation_expanded():
    assert extract("*.vkontakte.(ru|com)") == ["*.vkontakte.com", "*.vkontakte.ru"]


def test_store_links():
    md = "https://apps.apple.com/ru/app/x/id1201155481 https://www.rustore.ru/catalog/app/ru.corp.app"
    assert extract(md) == ["ios:id1201155481", "rustore:ru.corp.app"]


def test_registrable_and_tokens():
    assert registrable("*.api.tbank.ru") == "tbank.ru"
    assert registrable("shop.corp.com.ru") == "corp.com.ru"
    assert registrable("Kaspersky Plus") is None
    assert link_tokens(["a.x.ru", "*.x.ru", "android:ru.x", "1.2.3.4"]) == {"x.ru", "android:ru.x"}


def test_covered():
    pool = {"*.x.ru", "y.ru"}
    assert covered("api.x.ru", pool) and covered("x.ru", pool) and covered("*.a.x.ru", pool)
    assert covered("y.ru", pool) and not covered("api.y.ru", pool) and not covered("z.ru", pool)
