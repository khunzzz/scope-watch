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
	if e != nil || len(got.Programs) != 1 || len(got.Programs[0].Rewards) != 0 {
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
