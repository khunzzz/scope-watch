package standoff

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/khunzzz/scope-watch/internal/scope"
)

func (a *Adapter) list(ctx context.Context) ([]programData, error) {
	seen := map[string]bool{}
	out := []programData{}
	var total *int
	for n := 1; n <= 50; n++ {
		var p page
		if err := a.nextData(ctx, a.base+"/programs", url.Values{"page": {strconv.Itoa(n)}}, &p); err != nil {
			return nil, fmt.Errorf("list page %d: %w", n, err)
		}
		if p.Programs == nil {
			return nil, fmt.Errorf("missing programs on page %d", n)
		}
		if p.Total != nil {
			if *p.Total < 0 || (total != nil && *p.Total != *total) {
				return nil, fmt.Errorf("inconsistent page count on page %d", n)
			}
			total = p.Total
		}
		if len(p.Programs) == 0 {
			if total != nil && n <= *total {
				return nil, fmt.Errorf("empty page %d of %d", n, *total)
			}
			return out, nil
		}
		if total != nil && *total < n {
			return nil, fmt.Errorf("unexpected programs beyond page count on page %d", n)
		}
		for _, r := range p.Programs {
			if r.Slug == "" {
				return nil, fmt.Errorf("missing slug on page %d", n)
			}
			if seen[r.Slug] {
				return nil, fmt.Errorf("duplicate slug %q on page %d", r.Slug, n)
			}
			seen[r.Slug] = true
			if r.Visibility == "" || r.Visibility == "public" {
				out = append(out, r)
			}
		}
		if total != nil && n == *total {
			return out, nil
		}
	}
	return nil, fmt.Errorf("standoff pagination exceeds 50 pages")
}

func (a *Adapter) loadRewards(ctx context.Context, slug string) (map[string]rewardData, error) {
	var page struct{ Reward json.RawMessage }
	if err := a.nextData(ctx, a.base+"/programs/"+url.PathEscape(slug), nil, &page); err != nil {
		return nil, err
	}
	if len(page.Reward) == 0 {
		return nil, fmt.Errorf("missing reward field for %q", slug)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(page.Reward, &fields); err != nil {
		return nil, fmt.Errorf("rewards for %q: %w", slug, err)
	}
	// An explicit null means no reward table is published, not a failed fetch.
	// Keep nil distinct from an explicitly empty object for the adapter.
	if fields == nil {
		return nil, nil
	}
	result := make(map[string]rewardData)
	for _, severity := range scope.Severities {
		if raw, ok := fields[severity]; ok {
			var reward rewardData
			if err := json.Unmarshal(raw, &reward); err != nil {
				return nil, fmt.Errorf("%s reward for %q: %w", severity, slug, err)
			}
			result[severity] = reward
		}
	}
	return result, nil
}

var nextDataScript = regexp.MustCompile(`(?s)<script\b[^>]*\bid=["']__NEXT_DATA__["'][^>]*>(.*?)</script>`)

func (a *Adapter) nextData(ctx context.Context, u string, q url.Values, out any) error {
	b, e := a.get(ctx, u, q)
	if e != nil {
		return e
	}
	m := nextDataScript.FindSubmatch(b)
	if m == nil {
		return fmt.Errorf("no __NEXT_DATA__ at %s", u)
	}
	var envelope struct {
		Props struct {
			PageProps json.RawMessage `json:"pageProps"`
		} `json:"props"`
	}
	if e = json.Unmarshal(m[1], &envelope); e != nil {
		return fmt.Errorf("decode Next.js envelope: %w", e)
	}
	if len(envelope.Props.PageProps) == 0 {
		return fmt.Errorf("missing pageProps at %s", u)
	}
	return json.Unmarshal(envelope.Props.PageProps, out)
}

func (a *Adapter) get(ctx context.Context, address string, params url.Values) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	for key, values := range params {
		query[key] = values
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: HTTP %d", u.Redacted(), resp.StatusCode)
	}
	const maxResponseBytes = 8 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("response from %s exceeds %d bytes", u.Redacted(), maxResponseBytes)
	}
	return body, nil
}
