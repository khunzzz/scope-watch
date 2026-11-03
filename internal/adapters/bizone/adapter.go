// Package bizone adapts the public BI.ZONE API to program snapshots.
package bizone

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/khunzzz/scope-watch/internal/scope"
)

type Adapter struct {
	client   *http.Client
	base     string
	pageSize int
}

func New(client *http.Client) *Adapter {
	return &Adapter{client: client, base: baseURL + "/api/bug-bounty", pageSize: 100}
}

func (*Adapter) Name() string { return Name }

func (*Adapter) Title() string { return Title }

func (a *Adapter) Fetch(ctx context.Context, prev map[string]scope.Program) (scope.Fetched, error) {
	tasks, err := paginate[taskData](ctx, a.client, a.base+"/tasks/", a.pageSize)
	if err != nil {
		return scope.Fetched{}, fmt.Errorf("tasks: %w", err)
	}
	byCompany := map[string][]taskData{}
	for _, t := range tasks {
		if t.Company != nil && t.Company.Slug != "" {
			byCompany[t.Company.Slug] = append(byCompany[t.Company.Slug], t)
		}
	}
	all, err := paginate[programData](ctx, a.client, a.base+"/companies/", a.pageSize)
	if err != nil {
		return scope.Fetched{}, fmt.Errorf("companies: %w", err)
	}
	out := scope.Fetched{Programs: []scope.Program{}, Failed: []string{}}
	for _, item := range all {
		if err := ctx.Err(); err != nil {
			return scope.Fetched{}, err
		}
		if !defaultTrue(item.Public) {
			continue
		}
		out.Total++
		slug := item.Slug
		if slug == "" {
			return scope.Fetched{}, fmt.Errorf("company has no slug")
		}
		address := a.base + "/companies/" + url.PathEscape(slug) + "/"
		var detail programData
		if err := getJSON(ctx, a.client, address, nil, &detail); err != nil {
			if ctx.Err() != nil {
				return scope.Fetched{}, ctx.Err()
			}
			out.Failed = append(out.Failed, slug)
			if old, ok := prev[slug]; ok {
				kept := old.Clone()
				delete(kept.Meta, "missing")
				out.Programs = append(out.Programs, kept)
			}
			continue
		}
		if detail.Slug != slug {
			return scope.Fetched{}, fmt.Errorf("company slug mismatch: requested %s, got %s", slug, detail.Slug)
		}
		var versions []versionData
		versionErr := getJSON(ctx, a.client, address+"program-versions/", nil, &versions)
		if ctx.Err() != nil {
			return scope.Fetched{}, ctx.Err()
		}
		if versionErr != nil {
			// Unmarshal can populate a slice before returning a type error.
			versions = nil
		}
		p, err := parse(detail, versions, byCompany[slug])
		if err != nil {
			return scope.Fetched{}, err
		}
		if versionErr != nil || versions == nil {
			if old, ok := prev[slug]; ok {
				p.Versions = append([]string{}, old.Versions...)
			}
		}
		out.Programs = append(out.Programs, p)
	}
	return out, nil
}
