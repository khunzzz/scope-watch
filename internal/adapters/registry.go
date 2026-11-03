// Package adapters wires the supported platform implementations.
package adapters

import (
	"github.com/khunzzz/scope-watch/internal/adapters/bizone"
	"github.com/khunzzz/scope-watch/internal/adapters/standoff"
	"github.com/khunzzz/scope-watch/internal/httpclient"
	"github.com/khunzzz/scope-watch/internal/runner"
)

// New is the only composition point that needs changing to add a platform.
func New(client *httpclient.Client) []runner.Adapter {
	return []runner.Adapter{standoff.New(client), bizone.New(client)}
}
