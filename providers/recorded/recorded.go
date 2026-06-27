// Package recorded is a promptr.Provider that replays hand-authored JSON
// cassettes instead of calling a real model — a VCR for deterministic tests and
// offline demos. Cassettes are written by hand (never captured from a live
// third-party API), so a suite stays reproducible and dependency-free.
//
// A cassette is a JSON document of ordered interactions:
//
//	{
//	  "interactions": [
//	    {"reply": "{\"answer\": 42}"},
//	    {"match": "escalate", "reply": "{\"team\": \"oncall\"}"}
//	  ]
//	}
//
// Each Complete consumes the next usable interaction: the first unconsumed one
// whose "match" substring appears in the request (an empty match always
// applies). This makes a cassette sequential by default but order-independent
// when you give interactions distinguishing match strings.
package recorded

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/zkrebbekx/promptr"
)

// Interaction is one scripted request/response pair in a cassette.
type Interaction struct {
	// Match, when non-empty, requires this substring to appear in the request
	// text before the interaction is used.
	Match string `json:"match,omitempty"`
	Reply string `json:"reply"`
	// PromptTokens / ReplyTokens, when set, are surfaced via UsageReporter.
	PromptTokens int `json:"prompt_tokens,omitempty"`
	ReplyTokens  int `json:"reply_tokens,omitempty"`
}

type cassette struct {
	Interactions []Interaction `json:"interactions"`
}

// Provider replays a cassette. It is safe for concurrent use; interactions are
// consumed under a lock so each is returned at most once.
type Provider struct {
	mu           sync.Mutex
	interactions []Interaction
	consumed     []bool
	lastPrompt   int
	lastReply    int
}

// New builds a Provider from cassette JSON.
func New(data []byte) (*Provider, error) {
	var c cassette
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("recorded: parse cassette: %w", err)
	}
	if len(c.Interactions) == 0 {
		return nil, fmt.Errorf("recorded: cassette has no interactions")
	}
	return &Provider{
		interactions: c.Interactions,
		consumed:     make([]bool, len(c.Interactions)),
	}, nil
}

// Load builds a Provider from a cassette file.
func Load(path string) (*Provider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("recorded: %w", err)
	}
	return New(data)
}

// NewReader builds a Provider from a cassette stream.
func NewReader(r io.Reader) (*Provider, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("recorded: %w", err)
	}
	return New(data)
}

// Complete returns the reply of the next usable interaction for the request.
func (p *Provider) Complete(_ context.Context, messages []promptr.Message) (string, error) {
	req := requestText(messages)

	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.interactions {
		if p.consumed[i] {
			continue
		}
		in := p.interactions[i]
		if in.Match != "" && !strings.Contains(req, in.Match) {
			continue
		}
		p.consumed[i] = true
		p.lastPrompt = in.PromptTokens
		p.lastReply = in.ReplyTokens
		return in.Reply, nil
	}
	return "", fmt.Errorf("recorded: no remaining interaction matches request")
}

// LastUsage reports the token counts of the last replayed interaction (zero when
// the cassette did not specify them). It satisfies promptr.UsageReporter.
func (p *Provider) LastUsage() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastPrompt, p.lastReply
}

func requestText(messages []promptr.Message) string {
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(m.Content)
		sb.WriteByte('\n')
		for _, part := range m.Parts {
			sb.WriteString(part.Text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// static assertions: Provider satisfies the runtime contracts.
var (
	_ promptr.Provider      = (*Provider)(nil)
	_ promptr.UsageReporter = (*Provider)(nil)
)
