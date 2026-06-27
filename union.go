package promptr

import (
	"context"

	"github.com/zkrebbekx/promptr/coerce"
)

// Union is a resolver over several candidate variant types — the building block
// generated code uses for a function that returns a union. It is an alias for
// coerce.Union so generated code only needs to import promptr.
type Union = coerce.Union

// NewUnion builds a Union resolver from zero values of each variant type.
func NewUnion(samples ...any) *Union { return coerce.NewUnion(samples...) }

// ExtractUnion runs prompt against the provider and classifies the reply into
// one of the union's variants, returned as the sealed interface I. Like Extract
// it re-asks on a parse miss, feeding the error back, up to Options.Attempts.
//
// This is what a generated function with a union return type calls.
func ExtractUnion[I any](ctx context.Context, p Provider, prompt string, opts Options, u *Union) (I, error) {
	var zero I
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
		v, perr := coerce.ResolveInto[I](raw, u)
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
