package bizone

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

func paginate[T any](ctx context.Context, c *http.Client, address string, size int) ([]T, error) {
	for attempt := 0; attempt < 2; attempt++ {
		byID := map[string]json.RawMessage{}
		order := []string{}
		count := -1
		stable := true
		done := false
		for page := 0; page < 1000; page++ {
			var p struct {
				Count   *int
				Next    *string
				Results []json.RawMessage
			}
			if err := getJSON(ctx, c, address, url.Values{"limit": {strconv.Itoa(size)}, "offset": {strconv.Itoa(page * size)}}, &p); err != nil {
				return nil, err
			}
			if p.Count == nil || *p.Count < 0 {
				return nil, fmt.Errorf("missing or invalid count at %s", address)
			}
			if count < 0 {
				count = *p.Count
			} else if count != *p.Count {
				stable = false
			}
			for _, raw := range p.Results {
				var id struct{ ID json.RawMessage }
				if err := json.Unmarshal(raw, &id); err != nil {
					return nil, err
				}
				if len(id.ID) == 0 || string(id.ID) == "null" {
					return nil, fmt.Errorf("missing item id at %s", address)
				}
				key := string(id.ID)
				if _, ok := byID[key]; !ok {
					order = append(order, key)
				}
				byID[key] = raw
			}
			if p.Next == nil || *p.Next == "" || len(p.Results) == 0 {
				done = true
				break
			}
		}
		if !done {
			return nil, fmt.Errorf("pagination limit exceeded at %s", address)
		}
		if stable && count == len(byID) {
			out := make([]T, 0, len(order))
			for _, id := range order {
				var item T
				if err := json.Unmarshal(byID[id], &item); err != nil {
					return nil, fmt.Errorf("decode item at %s: %w", address, err)
				}
				out = append(out, item)
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("list changed during pagination at %s", address)
}

func getJSON(ctx context.Context, client *http.Client, address string, params url.Values, out any) error {
	u, err := url.Parse(address)
	if err != nil {
		return err
	}
	query := u.Query()
	for key, values := range params {
		query[key] = values
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: HTTP %d", u.Redacted(), resp.StatusCode)
	}
	const maxResponseBytes = 8 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("response from %s exceeds %d bytes", u.Redacted(), maxResponseBytes)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", u.Redacted(), err)
	}
	return nil
}
