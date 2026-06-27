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
	// Parts, when non-empty, carries a multimodal message (text + images/etc).
	// Providers that support multimodal input map Parts to their content array;
	// otherwise Content is used. Text-only callers leave Parts nil.
	Parts []Part
	// ToolCalls, on an "assistant" turn, are the tool invocations the model
	// requested (see ToolProvider). ToolCallID, on a "tool" turn, correlates a
	// tool result back to the call that produced it. Both are zero for ordinary
	// text turns, so non-tool callers are unaffected.
	ToolCalls  []ToolCall
	ToolCallID string
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
	// UserParts, when non-empty, makes the user turn multimodal: the rendered
	// prompt becomes the leading text Part, followed by these (images, files…).
	UserParts []Part
	// MaxSteps bounds the tool-calling agent loop (default 8): the most
	// model⇄tool round-trips RunTools will take before giving up. Ignored by the
	// non-tool Extract paths.
	MaxSteps int
	// Validate, when set, runs after a reply coerces into T. A non-nil error is
	// treated like a parse failure: its message is shown to the model and the call
	// retried, so a generated @assert constraint drives the model to self-correct.
	// It receives the value boxed as any (a struct or pointer to one).
	Validate func(v any) error
	// Check, when set, runs after Validate passes. Its violations are advisory:
	// the value is still returned and any error is handed to OnCheck. This backs
	// soft @check constraints. Check only runs when OnCheck is also set.
	Check func(v any) error
	// OnCheck receives the non-fatal result of Check. A nil OnCheck disables the
	// Check pass entirely.
	OnCheck func(err error)
	// ParallelTools, when set, dispatches the tool calls of a single model turn
	// concurrently instead of one after another (results still feed back in
	// request order). The calls within one turn are independent, so this cuts
	// latency when the model asks for several at once. Off by default, since tool
	// handlers that share mutable state must be goroutine-safe to opt in. Ignored
	// by the non-tool Extract paths.
	ParallelTools bool
}

// Option tunes the Options a generated function uses, applied after its built-in
// defaults. Generated functions take a trailing `...Option`, so callers can set
// retry budgets, a system preamble or a soft-check sink without the signature
// changing. The zero set leaves the defaults untouched.
type Option func(*Options)

// WithAttempts caps the number of model calls (the first try plus repair re-asks).
func WithAttempts(n int) Option { return func(o *Options) { o.Attempts = n } }

// WithMaxSteps bounds the tool-calling agent loop's model⇄tool round-trips.
func WithMaxSteps(n int) Option { return func(o *Options) { o.MaxSteps = n } }

// WithSystem prepends a system message to the exchange.
func WithSystem(s string) Option { return func(o *Options) { o.System = s } }

// OnCheck installs a sink for soft @check violations. Without it, generated
// @check constraints are evaluated into a no-op and effectively skipped; with it,
// each non-fatal violation is delivered here while the value is still returned.
func OnCheck(fn func(err error)) Option { return func(o *Options) { o.OnCheck = fn } }

// ParallelTools enables concurrent dispatch of the tool calls within a single
// model turn (see Options.ParallelTools). Opt in only when the tool handlers are
// goroutine-safe.
func ParallelTools() Option { return func(o *Options) { o.ParallelTools = true } }

// apply runs each Option against o.
func (o *Options) apply(opts []Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// userMessage builds the user turn for prompt, attaching any multimodal parts.
func (o Options) userMessage(prompt string) Message {
	if len(o.UserParts) == 0 {
		return Message{Role: "user", Content: prompt}
	}
	parts := make([]Part, 0, len(o.UserParts)+1)
	parts = append(parts, TextPart(prompt))
	parts = append(parts, o.UserParts...)
	return Message{Role: "user", Content: prompt, Parts: parts}
}

func (o Options) attempts() int {
	if o.Attempts <= 0 {
		return 2
	}
	return o.Attempts
}

func (o Options) maxSteps() int {
	if o.MaxSteps <= 0 {
		return 8
	}
	return o.MaxSteps
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
	msgs = append(msgs, opts.userMessage(prompt))

	var lastErr error
	for i := 0; i < opts.attempts(); i++ {
		raw, err := p.Complete(ctx, msgs)
		if err != nil {
			return zero, err
		}
		v, repair, ferr := finalize[T](raw, opts)
		if ferr == nil {
			return v, nil
		}
		lastErr = ferr
		msgs = append(msgs,
			Message{Role: "assistant", Content: raw},
			Message{Role: "user", Content: repair},
		)
	}
	return zero, lastErr
}

// finalize coerces raw into T and applies any Validate/Check from opts. On
// success it returns the value and a nil error. On a parse failure or a failed
// @assert (Validate) it returns a repair message — to feed back to the model for
// another attempt — alongside the underlying error. The advisory Check pass runs
// only after Validate succeeds and only when OnCheck is set; its violations are
// reported to OnCheck and never block the value.
func finalize[T any](raw string, opts Options) (v T, repair string, err error) {
	v, perr := coerce.Into[T](raw)
	if perr != nil {
		return v, "That reply could not be parsed (" + perr.Error() + "). Reply again with only the valid value, no commentary.", perr
	}
	if opts.Validate != nil {
		if verr := opts.Validate(v); verr != nil {
			return v, "That reply did not satisfy the required constraints (" + verr.Error() + "). Reply again with only a valid value, no commentary.", verr
		}
	}
	if opts.Check != nil && opts.OnCheck != nil {
		if cerr := opts.Check(v); cerr != nil {
			opts.OnCheck(cerr)
		}
	}
	return v, "", nil
}
