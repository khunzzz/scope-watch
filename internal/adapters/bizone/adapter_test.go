package bizone

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/khunzzz/scope-watch/internal/assets"
	"github.com/khunzzz/scope-watch/internal/model"
	"github.com/khunzzz/scope-watch/internal/testutil"
)

func TestPythonParserParity(t *testing.T) {
	var tasks map[string][]taskData
	testutil.Load(t, "bizone_tasks.json", &tasks)
	for _, slug := range []string{"tbank", "kaspersky"} {
		t.Run(slug, func(t *testing.T) {
			var raw programData
			testutil.Load(t, "bizone_"+slug+".json", &raw)
			got, e := parse(raw, []versionData{{Type: "rule", Version: 1}}, tasks[slug])
			if e != nil {
				t.Fatal(e)
			}
			testutil.CheckProgram(t, got, assets.Version)
		})
	}
}

type fakeClient struct {
	fn func(string, url.Values) (any, error)
}

func (c fakeClient) JSON(ctx context.Context, u string, q url.Values, out any) error {
	if e := ctx.Err(); e != nil {
		return e
	}
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
func TestDetailFailurePreservesPrevious(t *testing.T) {
	var raw programData
	testutil.Load(t, "bizone_tbank.json", &raw)
	broken, versionBroken, tasksBroken := false, false, false
	client := fakeClient{fn: func(u string, _ url.Values) (any, error) {
		switch {
		case strings.HasSuffix(u, "/tasks/"):
			if tasksBroken {
				return nil, fmt.Errorf("tasks down")
			}
			return map[string]any{"count": 0, "results": []any{}}, nil
		case strings.HasSuffix(u, "/companies/"):
			return map[string]any{"count": 1, "results": []map[string]any{{"id": 1, "slug": "tbank"}}}, nil
		case strings.HasSuffix(u, "program-versions/"):
			if versionBroken {
				return nil, fmt.Errorf("versions down")
			}
			return []map[string]any{{"type": "rule", "version": 1}}, nil
		default:
			if broken {
				return nil, fmt.Errorf("detail down")
			}
			return raw, nil
		}
	}}
	a := New(client)
	first, e := a.Fetch(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	prev := map[string]model.Program{"tbank": first.Programs[0]}
	prev["tbank"].Meta["missing"] = 1
	snapshot := prev["tbank"].Clone()
	broken = true
	got, e := a.Fetch(context.Background(), prev)
	if e != nil || len(got.Failed) != 1 || len(got.Programs) != 1 || got.Programs[0].MetaNumber("missing") != 0 {
		t.Fatalf("%+v %v", got, e)
	}
	if !reflect.DeepEqual(snapshot, prev["tbank"]) {
		t.Fatal("prev mutated")
	}
	got, e = a.Fetch(context.Background(), nil)
	if e != nil || len(got.Programs) != 0 || len(got.Failed) != 1 {
		t.Fatal("new broken program should wait")
	}
	broken = false
	versionBroken = true
	got, e = a.Fetch(context.Background(), prev)
	if e != nil || !reflect.DeepEqual(got.Programs[0].Versions, prev["tbank"].Versions) {
		t.Fatal("lost versions")
	}
	tasksBroken = true
	if _, e = a.Fetch(context.Background(), prev); e == nil {
		t.Fatal("mandatory tasks failure accepted")
	}
}
func TestPaginationShift(t *testing.T) {
	calls := 0
	c := fakeClient{fn: func(_ string, q url.Values) (any, error) {
		offset, _ := strconv.Atoi(q.Get("offset"))
		ids := []int{1, 2, 3, 4}
		if calls > 0 {
			ids = ids[1:]
		}
		calls++
		res := []map[string]int{}
		for _, id := range ids[min(offset, len(ids)):min(offset+2, len(ids))] {
			res = append(res, map[string]int{"id": id})
		}
		var next any
		if offset+2 < len(ids) {
			next = "more"
		}
		return map[string]any{"count": len(ids), "results": res, "next": next}, nil
	}}
	got, e := paginate[taskData](context.Background(), c, "https://test/tasks", 2)
	if e != nil || len(got) != 3 || got[0].ID != 2 {
		t.Fatalf("%+v %v", got, e)
	}
	c.fn = func(_ string, q url.Values) (any, error) {
		return map[string]any{"count": 2, "results": []map[string]int{{"id": 1}, {"id": 1}}}, nil
	}
	if _, e = paginate[taskData](context.Background(), c, "https://test/tasks", 2); e == nil {
		t.Fatal("duplicates accepted")
	}
}
func TestTaskAssets(t *testing.T) {
	no := false
	for _, tt := range []struct {
		task taskData
		want []string
	}{{taskData{TextTask: "Android App"}, []string{"Android App"}}, {taskData{TextTask: "x", Description: "a.corp.ru", Eligibility: &no}, []string{"a.corp.ru" + model.NoBounty}}} {
		if got := taskAssets(tt.task); !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("got %v want %v", got, tt.want)
		}
	}
}
