// Package promptr is the runtime that generated .promptr code calls into: a
// minimal, provider-agnostic Provider interface plus the parse-with-repair loop
// that turns a model's loose reply into a typed Go value via the coerce kernel.
//
// The compiler (cmd/promptr) turns each `function` in a .promptr file into a Go
// function whose body is a single call to Extract — so the generated code stays
// thin and readable, and all the retry/coercion logic lives here, tested once.
package promptr

import (
	"context"

	"github.com/zkrebbekx/promptr/coerce"
)

// Message is one turn in a chat-style exchange. Roles follow the usual
// "system" / "user" / "assistant" convention; a Provider maps them to whatever
// its backend expects.
type Message struct {
	Role    string
	Content string
}

// Provider is the single seam between promptr and a language model. Implement it
// with net/http against any chat API — the core imports no vendor SDK. A
// deterministic fake lives in providers/fake for tests and the playground.
type Provider interface {
	Complete(ctx context.Context, messages []Message) (string, error)
}

// Options tunes an Extract call.
type Options struct {
	// Attempts is the maximum number of model calls (default 2): the first
	// try plus repair re-asks when the reply will not coerce into T.
	Attempts int
	// System, when non-empty, is prepended as a system message.
	System string
}

func (o Options) attempts() int {
	if o.Attempts <= 0 {
		return 2
	}
	return o.Attempts
}

// Extract runs prompt against the provider and coerces the reply into T. If the
// reply will not coerce, it re-asks — appending the unparseable reply and the
// parse error so the model can correct itself — up to Options.Attempts times.
//
// This is the function every generated .promptr function calls.
func Extract[T any](ctx context.Context, p Provider, prompt string, opts Options) (T, error) {
	var zero T
	msgs := make([]Message, 0, 4)
	if opts.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: opts.System})
	}
	msgs = append(msgs, Message{Role: "user", Content: prompt})

	var lastErr error
	for i := 0; i < opts.attempts(); i++ {
		raw, err := p.Complete(ctx, msgs)
		if err != nil {
			return zero, err
		}
		v, perr := coerce.Into[T](raw)
		if perr == nil {
			return v, nil
		}
		lastErr = perr
		msgs = append(msgs,
			Message{Role: "assistant", Content: raw},
			Message{Role: "user", Content: "That reply could not be parsed (" + perr.Error() + "). Reply again with only the valid value, no commentary."},
		)
	}
	return zero, lastErr
}
