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
	// ToolReplies scripts CompleteTools: one Reply per call, in order, so an
	// agent loop (model → tool → model → final) can be driven deterministically.
	ToolReplies []Reply
	tn          int
}

// Reply is one scripted turn for CompleteTools: either tool Calls the model
// "requests", or a final Text answer. Script a slice of these on a Provider to
// exercise RunTools without a network.
type Reply struct {
	Text  string
	Calls []promptr.ToolCall
}

// static assertions: Provider satisfies all four runtime contracts.
var (
	_ promptr.Provider           = (*Provider)(nil)
	_ promptr.StreamProvider     = (*Provider)(nil)
	_ promptr.ToolProvider       = (*Provider)(nil)
	_ promptr.StreamToolProvider = (*Provider)(nil)
)

// New builds a Provider scripted with the given replies.
func New(replies ...string) *Provider { return &Provider{Replies: replies} }

// Complete returns the next scripted reply.
func (p *Provider) Complete(_ context.Context, msgs []promptr.Message) (string, error) {
	p.Calls = append(p.Calls, msgs)
	return p.nextReply()
}

// CompleteTools returns the next scripted Reply. After the last entry it
// keeps returning that final one. Every message slice is recorded in Calls.
func (p *Provider) CompleteTools(_ context.Context, msgs []promptr.Message, _ []promptr.ToolDef) (promptr.Reply, error) {
	p.Calls = append(p.Calls, msgs)
	if len(p.ToolReplies) == 0 {
		return promptr.Reply{}, fmt.Errorf("fake: no scripted tool replies")
	}
	i := p.tn
	if i >= len(p.ToolReplies) {
		i = len(p.ToolReplies) - 1
	}
	p.tn++
	r := p.ToolReplies[i]
	return promptr.Reply{Text: r.Text, Calls: r.Calls}, nil
}

// StreamTools scripts a streamed tool-enabled turn from the same ToolReplies
// slice CompleteTools walks. A final-text Reply streams its Text in chunks (so
// the partial-coerce path is exercised); a tool-call Reply streams nothing and
// hands back its Calls. It implements promptr.StreamToolProvider.
func (p *Provider) StreamTools(ctx context.Context, msgs []promptr.Message, _ []promptr.ToolDef) (promptr.ToolStream, error) {
	p.Calls = append(p.Calls, msgs)
	if len(p.ToolReplies) == 0 {
		return promptr.ToolStream{}, fmt.Errorf("fake: no scripted tool replies")
	}
	i := p.tn
	if i >= len(p.ToolReplies) {
		i = len(p.ToolReplies) - 1
	}
	p.tn++
	r := p.ToolReplies[i]

	size := p.ChunkSize
	if size <= 0 {
		size = defaultChunkSize
	}
	deltas := make(chan string)
	go func() {
		defer close(deltas)
		if len(r.Calls) > 0 {
			return // a tool-call turn carries no streamed text
		}
		for j := 0; j < len(r.Text); j += size {
			end := j + size
			if end > len(r.Text) {
				end = len(r.Text)
			}
			select {
			case deltas <- r.Text[j:end]:
			case <-ctx.Done():
				return
			}
		}
	}()
	return promptr.ToolStream{
		Deltas: deltas,
		Reply:  func() (promptr.Reply, error) { return promptr.Reply{Text: r.Text, Calls: r.Calls}, nil },
	}, nil
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
