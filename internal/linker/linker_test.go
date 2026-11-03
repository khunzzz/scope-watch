package linker

import (
	"github.com/khunzzz/scope-watch/internal/model"
	"github.com/khunzzz/scope-watch/internal/testutil"
	"testing"
)

func TestSlugKey(t *testing.T) {
	for _, pair := range [][2]string{{"vkplay_vk", "vk-play"}, {"vkontakte_vk", "vkontakte"}, {"gov-smev", "fgis-smev"}} {
		if SlugKey(pair[0]) != SlugKey(pair[1]) {
			t.Fatal(pair)
		}
	}
}
func TestMutualMatching(t *testing.T) {
	p := testutil.Programs(t)
	a, b := p["standoff/tbank"], p["bizone/tbank"]
	lk := New([]model.Program{a, b})
	if len(lk.Links) != 1 || len(lk.Peers(a)) != 1 || lk.GroupKey(a) != lk.GroupKey(b) {
		t.Fatal(lk.Links)
	}
	b.Platform = a.Platform
	lk = New([]model.Program{a, b})
	if len(lk.Links) != 0 {
		t.Fatal("same-platform link")
	}
}
