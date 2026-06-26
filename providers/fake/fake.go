// Package fake is a deterministic promptr.Provider for tests, examples and the
// playground — no network, no API key. It returns scripted replies in order, so
// you can exercise generated functions (including the parse-repair retry path)
// against known model output.
package fake

import (
	"context"
	"fmt"

	"github.com/zkrebbekx/promptr"
)

// Provider replies with Replies in order, one per Complete call. After the last
// scripted reply it keeps returning that final reply. Calls records every
// message slice it was given, for assertions.
type Provider struct {
	Replies []string
	Calls   [][]promptr.Message
	n       int
}

// New builds a Provider scripted with the given replies.
func New(replies ...string) *Provider { return &Provider{Replies: replies} }

// Complete returns the next scripted reply.
func (p *Provider) Complete(_ context.Context, msgs []promptr.Message) (string, error) {
	p.Calls = append(p.Calls, msgs)
	if len(p.Replies) == 0 {
		return "", fmt.Errorf("fake: no scripted replies")
	}
	i := p.n
	if i >= len(p.Replies) {
		i = len(p.Replies) - 1
	}
	p.n++
	return p.Replies[i], nil
}
