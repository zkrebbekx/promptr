package promptr

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Middleware wraps a Provider to add cross-cutting behaviour — logging, metrics,
// caching, rate limiting — without touching the generated code. A Middleware is
// just a Provider-to-Provider function, so they compose by nesting and a wrapped
// provider is still a plain Provider the runtime can call.
type Middleware func(Provider) Provider

// Chain applies middlewares to p, outermost first: Chain(p, a, b) yields
// a(b(p)), so a sees each call before b does.
func Chain(p Provider, mws ...Middleware) Provider {
	for i := len(mws) - 1; i >= 0; i-- {
		p = mws[i](p)
	}
	return p
}

// providerFunc adapts a function to the Provider interface, so middleware can
// build wrappers inline.
type providerFunc func(ctx context.Context, messages []Message) (string, error)

func (f providerFunc) Complete(ctx context.Context, messages []Message) (string, error) {
	return f(ctx, messages)
}

// Call is one observed Provider.Complete invocation.
type Call struct {
	Start    time.Time
	Duration time.Duration
	// PromptTokens / ReplyTokens are exact when the provider reports usage (see
	// UsageReporter); otherwise they are a chars/4 estimate.
	PromptTokens int
	ReplyTokens  int
	Estimated    bool // true when token counts are estimated, not reported
	Err          error
}

// UsageReporter is an optional interface a Provider may implement to expose the
// exact token counts of its most recent Complete call. The Collector uses it
// when present and falls back to a chars/4 estimate otherwise. It is consulted
// immediately after Complete returns, so an implementation should report the
// usage of that call.
type UsageReporter interface {
	LastUsage() (prompt, reply int)
}

// Collector aggregates per-call latency and token usage across every Complete it
// wraps. It is safe for concurrent use. Wire it as middleware with Collect.
type Collector struct {
	mu    sync.Mutex
	calls []Call
}

// Collect returns a Middleware that records every Complete into c.
func (c *Collector) Collect(p Provider) Provider {
	return providerFunc(func(ctx context.Context, messages []Message) (string, error) {
		start := time.Now()
		reply, err := p.Complete(ctx, messages)
		call := Call{Start: start, Duration: time.Since(start), Err: err}

		if u, ok := p.(UsageReporter); ok {
			call.PromptTokens, call.ReplyTokens = u.LastUsage()
		} else {
			call.PromptTokens = estimateTokens(messages)
			call.ReplyTokens = estimateChars(len(reply))
			call.Estimated = true
		}

		c.mu.Lock()
		c.calls = append(c.calls, call)
		c.mu.Unlock()
		return reply, err
	})
}

// CollectTools wraps a ToolProvider so each CompleteTools call is recorded into
// c on the same latency/usage path as Collect. The returned Provider also
// implements ToolProvider, so it can be passed straight to RunTools while
// staying observable.
func (c *Collector) CollectTools(p ToolProvider) Provider {
	return &toolCollector{c: c, inner: p}
}

type toolCollector struct {
	c     *Collector
	inner ToolProvider
}

// Complete delegates to the wrapped provider's Complete when it has one, so the
// collector is usable as a plain Provider too.
func (t *toolCollector) Complete(ctx context.Context, messages []Message) (string, error) {
	if p, ok := t.inner.(Provider); ok {
		return p.Complete(ctx, messages)
	}
	return "", fmt.Errorf("promptr: wrapped tool provider does not implement Complete")
}

func (t *toolCollector) CompleteTools(ctx context.Context, messages []Message, tools []ToolDef) (Reply, error) {
	start := time.Now()
	reply, err := t.inner.CompleteTools(ctx, messages, tools)
	call := Call{Start: start, Duration: time.Since(start), Err: err}

	if u, ok := t.inner.(UsageReporter); ok {
		call.PromptTokens, call.ReplyTokens = u.LastUsage()
	} else {
		call.PromptTokens = estimateTokens(messages)
		call.ReplyTokens = estimateChars(len(reply.Text))
		call.Estimated = true
	}

	t.c.mu.Lock()
	t.c.calls = append(t.c.calls, call)
	t.c.mu.Unlock()
	return reply, err
}

// Calls returns a snapshot copy of every recorded call.
func (c *Collector) Calls() []Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Call, len(c.calls))
	copy(out, c.calls)
	return out
}

// Stats is a rollup of a Collector's recorded calls.
type Stats struct {
	Calls        int
	Errors       int
	PromptTokens int
	ReplyTokens  int
	TotalLatency time.Duration
}

// TotalTokens is the sum of prompt and reply tokens across all calls.
func (s Stats) TotalTokens() int { return s.PromptTokens + s.ReplyTokens }

// AvgLatency is the mean Complete duration, or zero when no calls were recorded.
func (s Stats) AvgLatency() time.Duration {
	if s.Calls == 0 {
		return 0
	}
	return s.TotalLatency / time.Duration(s.Calls)
}

// Stats rolls up the recorded calls into totals.
func (c *Collector) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	var s Stats
	for _, call := range c.calls {
		s.Calls++
		if call.Err != nil {
			s.Errors++
		}
		s.PromptTokens += call.PromptTokens
		s.ReplyTokens += call.ReplyTokens
		s.TotalLatency += call.Duration
	}
	return s
}

// Reset clears all recorded calls.
func (c *Collector) Reset() {
	c.mu.Lock()
	c.calls = nil
	c.mu.Unlock()
}

func estimateTokens(messages []Message) int {
	chars := 0
	for _, m := range messages {
		chars += len(m.Content)
		for _, p := range m.Parts {
			chars += len(p.Text)
		}
	}
	return estimateChars(chars)
}

// estimateChars approximates tokens as roughly chars/4, the common rule of thumb
// for English text. It rounds up so any non-empty text counts as at least one.
func estimateChars(chars int) int {
	if chars == 0 {
		return 0
	}
	return (chars + 3) / 4
}
