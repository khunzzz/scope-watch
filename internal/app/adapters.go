package app

import (
	"net/http"

	"github.com/khunzzz/scope-watch/internal/adapters/bizone"
	"github.com/khunzzz/scope-watch/internal/adapters/standoff"
)

// newAdapters is the only composition point that needs changing to add a platform.
func newAdapters(client *http.Client) []Adapter {
	return []Adapter{standoff.New(client), bizone.New(client)}
}
