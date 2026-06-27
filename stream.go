package promptr

import (
	"context"

	"github.com/zkrebbekx/promptr/coerce"
)

// StreamProvider is an optional capability a Provider may implement to deliver a
// reply incrementally. Stream returns a channel of text chunks (e.g. server-sent
// token deltas); the channel closes when the model is done. A Provider that does
// not implement this is still usable for streaming extraction — ExtractStream
// falls back to a single Complete call.
type StreamProvider interface {
	Stream(ctx context.Context, messages []Message) (<-chan string, error)
}

// Partial carries a best-effort value parsed from an as-yet-incomplete stream.
// Complete flips to true once the payload parses cleanly. It mirrors
// coerce.Partial so generated code only imports promptr.
type Partial[T any] struct {
	Value    T
	Complete bool
	Err      error
}

// ExtractStream runs prompt against the provider and emits a progressively
// completed T after each chunk, coercing the accumulated text with the tolerant
// kernel so callers can render partial state. If the provider implements
// StreamProvider the reply streams token-by-token; otherwise a single Complete
// call yields one final Partial. The returned channel closes when the model is
// done.
//
// This is what a generated `-> stream T` function calls.
func ExtractStream[T any](ctx context.Context, p Provider, prompt string, opts Options) (<-chan Partial[T], error) {
	msgs := make([]Message, 0, 2)
	if opts.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: opts.System})
	}
	msgs = append(msgs, opts.userMessage(prompt))

	sp, ok := p.(StreamProvider)
	if !ok {
		raw, err := p.Complete(ctx, msgs)
		if err != nil {
			return nil, err
		}
		out := make(chan Partial[T], 1)
		v, perr := coerce.Into[T](raw)
		out <- Partial[T]{Value: v, Complete: perr == nil, Err: perr}
		close(out)
		return out, nil
	}

	chunks, err := sp.Stream(ctx, msgs)
	if err != nil {
		return nil, err
	}
	coerced := coerce.Stream[T](chunks)
	out := make(chan Partial[T])
	go func() {
		defer close(out)
		for cp := range coerced {
			select {
			case out <- Partial[T]{Value: cp.Value, Complete: cp.Complete, Err: cp.Err}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
