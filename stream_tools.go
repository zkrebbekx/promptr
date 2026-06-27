package promptr

import (
	"context"
	"fmt"
	"strings"

	"github.com/zkrebbekx/promptr/coerce"
)

// ToolStream is one streamed tool-enabled turn. Deltas carries the assistant's
// text as it generates (closed when the turn ends); Reply, valid only once
// Deltas has fully drained, returns the turn's outcome — the complete text and
// any tool calls the model requested. The contract is "drain Deltas, then call
// Reply": a provider may block sending on Deltas until the consumer reads, so a
// caller that stops reading early must cancel ctx to release it.
type ToolStream struct {
	Deltas <-chan string
	Reply  func() (Reply, error)
}

// StreamToolProvider is an optional capability combining StreamProvider and
// ToolProvider: a tool-enabled turn whose text streams token-by-token while the
// model is still free to request tool calls instead of answering. A Provider
// that implements it lets RunToolsStream surface partial answers during the
// final turn of an agent loop. One that does not is still usable — RunToolsStream
// falls back to a single blocking RunTools call.
type StreamToolProvider interface {
	StreamTools(ctx context.Context, messages []Message, tools []ToolDef) (ToolStream, error)
}

// RunToolsStream drives the same bounded agent loop as RunTools, but streams the
// model's text: it emits a progressively coerced Partial[T] after each token of
// the turn that is currently generating, and a final Complete partial once the
// model answers and the reply coerces into T. Intermediate tool-calling turns
// dispatch their tools (honoring Options.ParallelTools) and feed the results
// back, exactly like RunTools.
//
// The provider must implement StreamToolProvider to stream; otherwise this falls
// back to a blocking RunTools call delivered as a single final Partial, so a
// generated `-> stream T` tool-using function works against any tool provider.
// The returned channel closes when the loop ends (a final answer, an error, or
// the step budget); a non-nil terminal error rides on the last Partial's Err.
func RunToolsStream[T any](ctx context.Context, p Provider, prompt string, tools []Tool, opts Options) (<-chan Partial[T], error) {
	sp, ok := p.(StreamToolProvider)
	if !ok {
		out := make(chan Partial[T], 1)
		v, err := RunTools[T](ctx, p, prompt, tools, opts)
		out <- Partial[T]{Value: v, Complete: err == nil, Err: err}
		close(out)
		return out, nil
	}

	defs := make([]ToolDef, len(tools))
	byName := make(map[string]Tool, len(tools))
	for i, t := range tools {
		defs[i] = t.Def
		byName[t.Def.Name] = t
	}

	msgs := make([]Message, 0, 8)
	if opts.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: opts.System})
	}
	msgs = append(msgs, opts.userMessage(prompt))

	out := make(chan Partial[T])
	go func() {
		defer close(out)
		steps := opts.maxSteps()
		var lastErr error
		for step := 0; step < steps; step++ {
			ts, err := sp.StreamTools(ctx, msgs, defs)
			if err != nil {
				emit(ctx, out, Partial[T]{Err: err})
				return
			}

			// Surface partials as the turn's text accumulates. A turn that ends in
			// tool calls usually emits little or no text, so it rarely coerces; the
			// final-answer turn is the one whose partials matter.
			var acc strings.Builder
			for tok := range ts.Deltas {
				acc.WriteString(tok)
				if v, perr := coerce.Into[T](acc.String()); perr == nil {
					if !emit(ctx, out, Partial[T]{Value: v, Complete: false}) {
						return
					}
				}
			}

			reply, err := ts.Reply()
			if err != nil {
				emit(ctx, out, Partial[T]{Err: err})
				return
			}

			if len(reply.Calls) == 0 {
				v, repair, ferr := finalize[T](reply.Text, opts)
				if ferr == nil {
					emit(ctx, out, Partial[T]{Value: v, Complete: true})
					return
				}
				lastErr = ferr
				msgs = append(msgs,
					Message{Role: "assistant", Content: reply.Text},
					Message{Role: "user", Content: repair},
				)
				continue
			}

			msgs = append(msgs, Message{Role: "assistant", ToolCalls: reply.Calls})
			results := runCalls(ctx, byName, reply.Calls, opts.ParallelTools)
			for i, call := range reply.Calls {
				msgs = append(msgs, Message{Role: "tool", ToolCallID: call.ID, Content: results[i]})
			}
		}

		if lastErr == nil {
			lastErr = fmt.Errorf("promptr: tool loop did not converge within %d steps", steps)
		}
		emit(ctx, out, Partial[T]{Err: lastErr})
	}()
	return out, nil
}

// emit sends p on out unless ctx is done; it reports whether the send succeeded.
func emit[T any](ctx context.Context, out chan<- Partial[T], p Partial[T]) bool {
	select {
	case out <- p:
		return true
	case <-ctx.Done():
		return false
	}
}
