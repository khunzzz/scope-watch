package standoff

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/khunzzz/scope-watch/internal/assets"
	"github.com/khunzzz/scope-watch/internal/model"
)

const Name = "standoff"
const Title = "Standoff 365"
const baseURL = "https://bugbounty.standoff365.com"

func parse(raw programData) (model.Program, error) {
	if raw.Slug == "" {
		return model.Program{}, fmt.Errorf("standoff program has no slug")
	}
	parts := []string{}
	for _, s := range []string{raw.Description, raw.SpecialRules} {
		if s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")); s != "" {
			parts = append(parts, s)
		}
	}
	md := strings.Join(parts, "\n\n")
	exclude := []string{"standoff365.com"}
	if raw.Slug == "standoff-365" {
		exclude = nil
	}
	inside, outside := assets.SplitScope(md, exclude)
	name := raw.Name
	if name == "" {
		name = raw.Slug
	}
	status := raw.Status
	if raw.Finished {
		status = "finished"
	}
	return model.Program{
		Platform:     Name,
		Slug:         raw.Slug,
		Name:         name,
		URL:          baseURL + "/programs/" + url.PathEscape(raw.Slug),
		Active:       raw.Status == "published" && !raw.Finished && (raw.ArchivedAt == nil || *raw.ArchivedAt == ""),
		Status:       status,
		Assets:       inside,
		OutOfScope:   outside,
		AssetsSource: "text",
		ScopeMD:      md,
		UpdatedAt:    raw.UpdatedAt,
		Meta:         map[string]any{"extractor": assets.Version},
		Versions:     []string{},
	}, nil
}

func rewards(raw map[string]rewardData) map[string][]int {
	out := map[string][]int{}
	for _, s := range model.Severities {
		r := raw[s]
		if r.MaxReward != 0 {
			out[s] = []int{r.MinReward, r.MaxReward}
		}
	}
	return out
}
