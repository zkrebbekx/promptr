package promptr

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// CallKind identifies which provider capability a Hook is observing.
type CallKind string

// The call kinds a Hook can observe, one per provider capability.
const (
	KindComplete CallKind = "complete" // Provider.Complete
	KindStream   CallKind = "stream"   // StreamProvider.Stream
	KindTools    CallKind = "tools"    // ToolProvider.CompleteTools
)

// CallInfo describes a provider call as a Hook sees it begin.
type CallInfo struct {
	Kind     CallKind
	Messages []Message
	Tools    []ToolDef // set only when Kind is KindTools
	Start    time.Time
}

// Outcome describes how a provider call ended. A Hook's AfterFunc receives it.
type Outcome struct {
	// Text is the final text reply (KindComplete/KindStream, or KindTools when
	// the model answered without requesting a tool). For a stream it is the full
	// accumulated text once the channel closes.
	Text string
	// Calls is the tool calls the model requested (KindTools only).
	Calls []ToolCall
	// PromptTokens/ReplyTokens are exact when the provider implements
	// UsageReporter; otherwise they are a chars/4 estimate and Estimated is true.
	PromptTokens int
	ReplyTokens  int
	Estimated    bool
	Duration     time.Duration
	Err          error
}

// Hook observes provider calls for cross-cutting concerns — logging, metrics,
// tracing — without having to implement a Provider. BeforeCall fires just before
// a call; the AfterFunc it returns (which may be nil) fires when the call returns
// or, for a stream, when the channel closes. Register hooks with WithHooks, which
// preserves the wrapped provider's streaming and tool-calling capabilities — the
// problem a plain Provider-to-Provider Middleware cannot solve.
type Hook interface {
	BeforeCall(ctx context.Context, info CallInfo) AfterFunc
}

// AfterFunc observes a call's Outcome. A nil AfterFunc is fine — it is skipped.
type AfterFunc func(Outcome)

// WithHooks wraps p so every Complete, Stream and CompleteTools call fires the
// given hooks, and returns a provider that still satisfies StreamProvider and
// ToolProvider exactly when p does. This is the capability-preserving seam for
// observability: unlike Chain (which only wraps Complete), WithHooks keeps a
// streaming/tool provider usable for streaming and tool calls. With no hooks it
// returns p unchanged.
func WithHooks(p Provider, hooks ...Hook) Provider {
	if len(hooks) == 0 {
		return p
	}
	sp, _ := p.(StreamProvider)
	tp, _ := p.(ToolProvider)
	base := &hookProvider{inner: p, sp: sp, tp: tp, hooks: hooks}
	switch {
	case sp != nil && tp != nil:
		return hpStreamTool{base}
	case sp != nil:
		return hpStream{base}
	case tp != nil:
		return hpTool{base}
	default:
		return base
	}
}

// hookProvider holds the inner provider plus its detected capabilities (sp/tp are
// nil when unsupported). The exported Complete and the helper stream/completeTools
// methods do the hook firing; the hpXxx wrapper types below expose Stream and
// CompleteTools only for the combinations the inner provider actually supports,
// so type assertions on the result stay truthful.
type hookProvider struct {
	inner Provider
	sp    StreamProvider
	tp    ToolProvider
	hooks []Hook
}

func (h *hookProvider) before(ctx context.Context, info CallInfo) []AfterFunc {
	afters := make([]AfterFunc, 0, len(h.hooks))
	for _, hk := range h.hooks {
		afters = append(afters, hk.BeforeCall(ctx, info))
	}
	return afters
}

func fireAfter(afters []AfterFunc, o Outcome) {
	for _, a := range afters {
		if a != nil {
			a(o)
		}
	}
}

// fillUsage records exact token counts when the inner provider reports them, else
// a chars/4 estimate, matching Collector's accounting.
func (h *hookProvider) fillUsage(o *Outcome, msgs []Message, reply string) {
	if u, ok := h.inner.(UsageReporter); ok {
		o.PromptTokens, o.ReplyTokens = u.LastUsage()
	} else {
		o.PromptTokens = estimateTokens(msgs)
		o.ReplyTokens = estimateChars(len(reply))
		o.Estimated = true
	}
}

func (h *hookProvider) Complete(ctx context.Context, msgs []Message) (string, error) {
	start := time.Now()
	afters := h.before(ctx, CallInfo{Kind: KindComplete, Messages: msgs, Start: start})
	reply, err := h.inner.Complete(ctx, msgs)
	o := Outcome{Text: reply, Err: err, Duration: time.Since(start)}
	h.fillUsage(&o, msgs, reply)
	fireAfter(afters, o)
	return reply, err
}

func (h *hookProvider) stream(ctx context.Context, msgs []Message) (<-chan string, error) {
	start := time.Now()
	afters := h.before(ctx, CallInfo{Kind: KindStream, Messages: msgs, Start: start})
	ch, err := h.sp.Stream(ctx, msgs)
	if err != nil {
		fireAfter(afters, Outcome{Err: err, Duration: time.Since(start)})
		return nil, err
	}
	out := make(chan string)
	go func() {
		defer close(out)
		var sb strings.Builder
		finish := func(err error) {
			o := Outcome{Text: sb.String(), Err: err, Duration: time.Since(start)}
			h.fillUsage(&o, msgs, sb.String())
			fireAfter(afters, o)
		}
		for tok := range ch {
			sb.WriteString(tok)
			select {
			case out <- tok:
			case <-ctx.Done():
				finish(ctx.Err())
				return
			}
		}
		finish(nil)
	}()
	return out, nil
}

func (h *hookProvider) completeTools(ctx context.Context, msgs []Message, tools []ToolDef) (Reply, error) {
	start := time.Now()
	afters := h.before(ctx, CallInfo{Kind: KindTools, Messages: msgs, Tools: tools, Start: start})
	reply, err := h.tp.CompleteTools(ctx, msgs, tools)
	o := Outcome{Text: reply.Text, Calls: reply.Calls, Err: err, Duration: time.Since(start)}
	h.fillUsage(&o, msgs, reply.Text)
	fireAfter(afters, o)
	return reply, err
}

// Capability-preserving wrappers: each embeds *hookProvider (for Complete) and
// exposes exactly the extra interfaces the inner provider supports.
type hpStream struct{ *hookProvider }

func (h hpStream) Stream(ctx context.Context, msgs []Message) (<-chan string, error) {
	return h.stream(ctx, msgs)
}

type hpTool struct{ *hookProvider }

func (h hpTool) CompleteTools(ctx context.Context, msgs []Message, tools []ToolDef) (Reply, error) {
	return h.completeTools(ctx, msgs, tools)
}

type hpStreamTool struct{ *hookProvider }

func (h hpStreamTool) Stream(ctx context.Context, msgs []Message) (<-chan string, error) {
	return h.stream(ctx, msgs)
}

func (h hpStreamTool) CompleteTools(ctx context.Context, msgs []Message, tools []ToolDef) (Reply, error) {
	return h.completeTools(ctx, msgs, tools)
}

// Hook adapts a Collector to the Hook seam so it records every Complete, Stream
// and CompleteTools call when wired via WithHooks — capturing the streaming and
// tool paths that the older Collect/CollectTools wrappers miss.
func (c *Collector) Hook() Hook { return collectorHook{c} }

type collectorHook struct{ c *Collector }

func (ch collectorHook) BeforeCall(_ context.Context, info CallInfo) AfterFunc {
	return func(o Outcome) {
		ch.c.mu.Lock()
		ch.c.calls = append(ch.c.calls, Call{
			Start:        info.Start,
			Duration:     o.Duration,
			PromptTokens: o.PromptTokens,
			ReplyTokens:  o.ReplyTokens,
			Estimated:    o.Estimated,
			Err:          o.Err,
		})
		ch.c.mu.Unlock()
	}
}

// LogHook returns a Hook that logs each call to logger (slog.Default when nil):
// a debug line before the call and an info/error line after, with kind, latency
// and token counts. It is the zero-dependency reference Hook; an OpenTelemetry
// span hook is a handful of lines following the same shape.
func LogHook(logger *slog.Logger) Hook {
	if logger == nil {
		logger = slog.Default()
	}
	return slogHook{logger}
}

type slogHook struct{ logger *slog.Logger }

func (s slogHook) BeforeCall(ctx context.Context, info CallInfo) AfterFunc {
	s.logger.LogAttrs(ctx, slog.LevelDebug, "promptr call start",
		slog.String("kind", string(info.Kind)),
		slog.Int("messages", len(info.Messages)),
		slog.Int("tools", len(info.Tools)),
	)
	return func(o Outcome) {
		lvl := slog.LevelInfo
		if o.Err != nil {
			lvl = slog.LevelError
		}
		s.logger.LogAttrs(ctx, lvl, "promptr call done",
			slog.String("kind", string(info.Kind)),
			slog.Duration("duration", o.Duration),
			slog.Int("prompt_tokens", o.PromptTokens),
			slog.Int("reply_tokens", o.ReplyTokens),
			slog.Bool("estimated", o.Estimated),
			slog.Int("tool_calls", len(o.Calls)),
			slog.Any("err", o.Err),
		)
	}
}
