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
	Replies   []string
	Calls     [][]promptr.Message
	ChunkSize int // bytes per streamed chunk (default 8); see Stream
	n         int
}

// static assertions: Provider satisfies both runtime contracts.
var (
	_ promptr.Provider       = (*Provider)(nil)
	_ promptr.StreamProvider = (*Provider)(nil)
)

// New builds a Provider scripted with the given replies.
func New(replies ...string) *Provider { return &Provider{Replies: replies} }

// Complete returns the next scripted reply.
func (p *Provider) Complete(_ context.Context, msgs []promptr.Message) (string, error) {
	p.Calls = append(p.Calls, msgs)
	return p.nextReply()
}

func (p *Provider) nextReply() (string, error) {
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

// ChunkSize is how many bytes each streamed chunk carries (default 8). It lets
// tests exercise the partial-parse path of coerce.Stream.
const defaultChunkSize = 8

// Stream emits the next scripted reply as a sequence of chunks, so streaming
// extraction can be exercised without a network. It implements
// promptr.StreamProvider.
func (p *Provider) Stream(ctx context.Context, msgs []promptr.Message) (<-chan string, error) {
	p.Calls = append(p.Calls, msgs)
	reply, err := p.nextReply()
	if err != nil {
		return nil, err
	}
	size := p.ChunkSize
	if size <= 0 {
		size = defaultChunkSize
	}
	out := make(chan string)
	go func() {
		defer close(out)
		for i := 0; i < len(reply); i += size {
			end := i + size
			if end > len(reply) {
				end = len(reply)
			}
			select {
			case out <- reply[i:end]:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
