package standoff

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/khunzzz/scope-watch/internal/scope"
)

func TestLoadRewardsPublishedAndInvalidFields(t *testing.T) {
	for _, tc := range []struct {
		name, page       string
		wantErr, wantNil bool
	}{
		{"unpublished", `{"reward":null}`, false, true},
		{"empty", `{"reward":{}}`, false, false},
		{"published", `{"reward":{"critical":{"minReward":10,"maxReward":20}}}`, false, false},
		{"missing", `{}`, true, true},
		{"array", `{"reward":[]}`, true, true},
		{"string", `{"reward":"unknown"}`, true, true},
		{"bad amount", `{"reward":{"critical":{"maxReward":"oops"}}}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `<script id="__NEXT_DATA__">{"props":{"pageProps":%s}}</script>`, tc.page)
			}))
			defer server.Close()
			a := New(server.Client())
			a.base = server.URL
			got, err := a.loadRewards(context.Background(), "test")
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && (got == nil) != tc.wantNil {
				t.Fatalf("reward = %#v, wantNil = %v", got, tc.wantNil)
			}
		})
	}
}

func TestUnpublishedRewardsCacheAndRefresh(t *testing.T) {
	calls := 0
	published := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := `{"programs":[{"slug":"test","status":"published","updatedAt":"unchanged"}],"total":1}`
		if r.URL.Path != "/programs" {
			calls++
			page = `{"reward":null}`
			if published {
				page = `{"reward":{"critical":{"minReward":10,"maxReward":20}}}`
			}
		}
		fmt.Fprintf(w, `<script id="__NEXT_DATA__">{"props":{"pageProps":%s}}</script>`, page)
	}))
	defer server.Close()
	a := New(server.Client())
	a.base = server.URL
	now := time.Unix(1800000000, 0)
	a.now = func() time.Time { return now }
	prev := map[string]scope.Program{}
	for i := 0; i < 3; i++ {
		if i == 2 {
			published = true
			now = now.Add(rewardTTL + time.Second)
		}
		got, err := a.Fetch(context.Background(), prev)
		if err != nil || len(got.Failed) != 0 || len(got.Programs) != 1 {
			t.Fatalf("fetch %d: %+v, %v", i, got, err)
		}
		p := got.Programs[0]
		if p.Rewards == nil || p.MetaNumber("rewards_pending") != 0 {
			t.Fatalf("unpublished result left pending: %+v", p)
		}
		if i < 2 && p.MetaNumber("rewards_unpublished") != 1 {
			t.Fatalf("fetch %d lost unpublished marker: %+v", i, p.Meta)
		}
		if i == 1 && calls != 1 {
			t.Fatalf("unpublished reward bypassed cache: %d calls", calls)
		}
		if i == 2 && (calls != 2 || p.MetaNumber("rewards_unpublished") != 0 || len(p.Rewards["critical"]) != 2 || p.Rewards["critical"][1] != 20) {
			t.Fatalf("TTL did not refresh unpublished rewards: %+v, calls=%d", p, calls)
		}
		prev[p.Slug] = p
	}
}
