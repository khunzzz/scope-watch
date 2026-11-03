package standoff

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/khunzzz/scope-watch/internal/assets"
	"github.com/khunzzz/scope-watch/internal/model"
	"github.com/khunzzz/scope-watch/internal/testutil"
)

func raws(t *testing.T) map[string]programData {
	var ps []programData
	testutil.Load(t, "standoff_list.json", &ps)
	out := map[string]programData{}
	for _, p := range ps {
		out[p.Slug] = p
	}
	var p programData
	testutil.Load(t, "standoff_tbank.json", &p)
	out[p.Slug] = p
	return out
}
func TestPythonParserParity(t *testing.T) {
	for slug, raw := range raws(t) {
		t.Run(slug, func(t *testing.T) {
			got, err := parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			testutil.CheckProgram(t, got, assets.Version)
		})
	}
}

type fakeClient struct {
	fn    func(string, url.Values) (any, error)
	calls []string
}

func (c *fakeClient) NextData(ctx context.Context, u string, q url.Values, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.calls = append(c.calls, u)
	v, e := c.fn(u, q)
	if e != nil {
		return e
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func TestRewardsCacheAndFailure(t *testing.T) {
	raw := raws(t)["flowwow"]
	broken := false
	c := &fakeClient{fn: func(u string, q url.Values) (any, error) {
		if strings.HasSuffix(u, "/programs") {
			return map[string]any{"programs": []programData{raw}, "total": 1}, nil
		}
		if broken {
			return nil, fmt.Errorf("down")
		}
		return map[string]any{"reward": map[string]rewardData{"critical": {50000, 70000}}}, nil
	}}
	a := New(c)
	a.now = func() time.Time { return time.Unix(1000000, 0) }
	first, e := a.Fetch(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	prev := map[string]model.Program{raw.Slug: first.Programs[0]}
	c.calls = nil
	got, e := a.Fetch(context.Background(), prev)
	if e != nil || len(c.calls) != 1 || got.Programs[0].Rewards["critical"][1] != 70000 {
		t.Fatalf("cached: %+v %v %v", got, e, c.calls)
	}
	prev[raw.Slug].Meta["rewards_at"] = 0
	snapshot := prev[raw.Slug].Clone()
	broken = true
	got, e = a.Fetch(context.Background(), prev)
	if e != nil || len(got.Failed) != 1 || got.Programs[0].MetaNumber("rewards_at") != 0 {
		t.Fatalf("failure %+v %v", got, e)
	}
	if !reflect.DeepEqual(snapshot, prev[raw.Slug]) {
		t.Fatal("mutated previous snapshot")
	}
	got, e = a.Fetch(context.Background(), nil)
	if e != nil || len(got.Programs) != 1 || got.Programs[0].Rewards != nil {
		t.Fatal("new program without rewards must survive", e)
	}
}
func TestListIntegrity(t *testing.T) {
	raw := raws(t)["flowwow"]
	c := &fakeClient{fn: func(_ string, q url.Values) (any, error) {
		ps := []programData{raw}
		if q.Get("page") == "2" {
			ps = nil
		}
		return map[string]any{"programs": ps, "total": 3}, nil
	}}
	if _, e := New(c).Fetch(context.Background(), nil); e == nil {
		t.Fatal("empty middle page accepted")
	}
	c.fn = func(_ string, _ url.Values) (any, error) {
		return map[string]any{"programs": []programData{raw}, "total": 51}, nil
	}
	if _, e := New(c).Fetch(context.Background(), nil); e == nil {
		t.Fatal("truncated pagination accepted")
	}
}

func TestRejectIncompleteList(t *testing.T) {
	first := programData{Slug: "first", Visibility: "public"}
	second := programData{Slug: "second", Visibility: "public"}
	for name, pages := range map[string][]any{
		"repeated page": {
			map[string]any{"programs": []programData{first}, "total": 2},
			map[string]any{"programs": []programData{first}, "total": 2},
		},
		"changed count": {
			map[string]any{"programs": []programData{first}, "total": 3},
			map[string]any{"programs": []programData{second}, "total": 2},
		},
		"missing list": {
			map[string]any{"programs": []programData{first}},
			map[string]any{},
		},
		"invalid count": {
			map[string]any{"programs": []programData{first}, "total": -1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			i := 0
			c := &fakeClient{fn: func(string, url.Values) (any, error) {
				if i >= len(pages) {
					t.Fatal("requested an unexpected page")
				}
				p := pages[i]
				i++
				return p, nil
			}}
			if _, err := New(c).list(context.Background()); err == nil {
				t.Fatal("incomplete list accepted")
			}
		})
	}
}

func TestMissingRewardPreservesCache(t *testing.T) {
	raw := raws(t)["flowwow"]
	old, err := parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	old.Rewards = map[string][]int{"critical": {50000, 70000}}
	old.Meta["rewards_at"] = 1
	c := &fakeClient{fn: func(u string, _ url.Values) (any, error) {
		if strings.HasSuffix(u, "/programs") {
			return map[string]any{"programs": []programData{raw}, "total": 1}, nil
		}
		return map[string]any{}, nil
	}}
	a := New(c)
	a.now = func() time.Time { return time.Unix(1000000, 0) }
	got, err := a.Fetch(context.Background(), map[string]model.Program{raw.Slug: old})
	if err != nil || len(got.Programs) != 1 || len(got.Failed) != 1 {
		t.Fatalf("fetch: %+v, %v", got, err)
	}
	if !reflect.DeepEqual(got.Programs[0].Rewards, old.Rewards) || got.Programs[0].MetaNumber("rewards_at") != 1 {
		t.Fatalf("missing reward replaced cache: %+v", got.Programs[0])
	}
	// Null has no confirmed API meaning and must not erase cached payouts.
	c.fn = func(string, url.Values) (any, error) {
		return map[string]any{"reward": nil}, nil
	}
	if _, err := a.loadRewards(context.Background(), raw.Slug); err == nil {
		t.Fatal("null reward accepted as known payouts")
	}
}

func TestRewardKnowledgeAndCache(t *testing.T) {
	raw := raws(t)["flowwow"]
	old, err := parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if old.Rewards != nil {
		t.Fatal("list-only program claims known payouts")
	}
	now := time.Unix(1000000, 0)
	old.Meta["rewards_at"] = float64(now.Unix())
	var reward any
	c := &fakeClient{fn: func(u string, _ url.Values) (any, error) {
		if strings.HasSuffix(u, "/programs") {
			return map[string]any{"programs": []programData{raw}, "total": 1}, nil
		}
		return map[string]any{"reward": reward}, nil
	}}
	a := New(c)
	a.now = func() time.Time { return now }
	got, err := a.Fetch(context.Background(), map[string]model.Program{raw.Slug: old})
	if err != nil || len(got.Failed) != 1 || got.Programs[0].Rewards != nil || len(c.calls) != 2 {
		t.Fatalf("unknown payouts not retried/preserved: %+v, %v", got, err)
	}
	reward = map[string]rewardData{"critical": {0, 0}}
	got, err = a.Fetch(context.Background(), map[string]model.Program{raw.Slug: old})
	if err != nil || len(got.Failed) != 0 || got.Programs[0].Rewards == nil || len(got.Programs[0].Rewards) != 0 {
		t.Fatalf("zero payouts not marked known: %+v, %v", got, err)
	}
	c.calls = nil
	got, err = a.Fetch(context.Background(), map[string]model.Program{raw.Slug: got.Programs[0]})
	if err != nil || len(c.calls) != 1 || got.Programs[0].Rewards == nil {
		t.Fatalf("known empty payouts not cached: %+v, %v", got, err)
	}
}

func TestRewardsWithMetadata(t *testing.T) {
	var page any
	testutil.Load(t, "standoff_flowwow_page.json", &page)
	c := &fakeClient{fn: func(string, url.Values) (any, error) { return page, nil }}
	got, err := New(c).loadRewards(context.Background(), "flowwow")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got["critical"].MinReward != 50000 || got["critical"].MaxReward != 70000 {
		t.Fatalf("missing severity rewards: %+v", got)
	}
}
func TestSpecialRulesAndFinished(t *testing.T) {
	raw := raws(t)["flowwow"]
	raw.Description = ""
	raw.SpecialRules = "Скоуп: *.special.ru"
	raw.Finished = true
	p, e := parse(raw)
	if e != nil || p.Active || !reflect.DeepEqual(p.Assets, []string{"*.special.ru"}) {
		t.Fatalf("%+v %v", p, e)
	}
}
