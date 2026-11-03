package assets

import (
	"reflect"
	"testing"
)

func TestExtract(t *testing.T) {
	tests := []struct {
		name, text string
		want       []string
	}{
		{"noise", `Пиши на security@corp.ru, см. https://owasp.org/x, файл report.pdf, window.opener, user6.rules, IP 10.0.0.0/8 и 192.168.1.1, но 81.19.64.0/24 и \*.corp.ru`, []string{"*.corp.ru", "81.19.64.0/24"}},
		{"alternation", "*.vkontakte.(ru|com)", []string{"*.vkontakte.com", "*.vkontakte.ru"}},
		{"stores", "https://apps.apple.com/ru/app/x/id1201155481 https://www.rustore.ru/catalog/app/ru.corp.app", []string{"ios:id1201155481", "rustore:ru.corp.app"}},
		{"inner-wildcards", `SSRF через rentgen\*.smailru.net; videostun*.okcdn.ru, call*-*.vkuser.net; **bold.ru**; rustore.ru/catalog/app/ru.alfabank.oavdo.amc; приложения ru.tinkoff.investing, com.vk.vkvideo; сайт ru.investing.com, youdrive.today`, []string{"android:com.vk.vkvideo", "android:ru.tinkoff.investing", "bold.ru", "call*-*.vkuser.net", "rentgen*.smailru.net", "ru.investing.com", "rustore:ru.alfabank.oavdo.amc", "videostun*.okcdn.ru", "youdrive.today"}},
		{"markdown", "*.[tbank.ru](https://tbank.ru)", []string{"*.tbank.ru", "tbank.ru"}},
		{"ip-urls", "http://81.19.64.1:8080/admin https://8.8.8.8/ http://192.168.1.1/private https://127.0.0.1", []string{"8.8.8.8", "81.19.64.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Extract(tt.text, nil)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %q; want %q", got, tt.want)
			}
		})
	}
}
func TestSplitScope(t *testing.T) {
	text := "# Область действия\n- *.corp.ru\n- app.corp.ru\n\n**Мобильные приложения**\n- ru.corp.app\n\n## Что НЕ искать\n- vds-*.corp.ru\n\n**Сайты партнёров**\n- partner.corp.ru\n\n## Выплаты\nВне скоупа: legacy.corp.ru, old.corp.ru\n- pay.corp.ru"
	in, out := SplitScope(text, nil)
	if !reflect.DeepEqual(in, []string{"*.corp.ru", "android:ru.corp.app", "app.corp.ru", "pay.corp.ru"}) || !reflect.DeepEqual(out, []string{"legacy.corp.ru", "old.corp.ru", "partner.corp.ru", "vds-*.corp.ru"}) {
		t.Fatalf("in=%v out=%v", in, out)
	}
}
func TestMatching(t *testing.T) {
	for a, w := range map[string]string{"*.api.tbank.ru": "tbank.ru", "shop.corp.com.ru": "corp.com.ru", "Kaspersky Plus": "", "1.2.3.4": ""} {
		if got := Registrable(a); got != w {
			t.Errorf("%s: %s != %s", a, got, w)
		}
	}
	pool := map[string]bool{"*.x.ru": true, "y.ru": true}
	for a, w := range map[string]bool{"api.x.ru": true, "x.ru": true, "*.a.x.ru": true, "y.ru": true, "api.y.ru": false, "z.ru": false} {
		if Covered(a, pool) != w {
			t.Error(a)
		}
	}
	if !reflect.DeepEqual(LinkTokens([]string{"a.x.ru", "*.x.ru", "android:ru.x", "1.2.3.4"}), map[string]bool{"x.ru": true, "android:ru.x": true}) {
		t.Fatal("tokens")
	}
}
