package promptr

import (
	"context"
	"fmt"
	"sync"
)

// ToolDef describes a tool offered to the model: its name, a one-line
// description, and a human-readable schema of its argument object (built the
// same way output schemas are, so the model sees one consistent style). A
// ToolProvider marshals these into whatever its backend's tool/function-calling
// API expects.
type ToolDef struct {
	Name        string
	Description string
	Params      string // schema description of the JSON argument object
}

// ToolCall is the model's request to invoke a tool: an opaque ID the provider
// uses to correlate the result, the tool Name, and the raw JSON Arguments. The
// agent loop decodes Arguments tolerantly via the coerce kernel.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Reply is one turn from a ToolProvider: either a final answer in Text, or one
// or more tool Calls the model wants run before it can answer. When Calls is
// non-empty the loop dispatches them and asks again; otherwise Text is final.
type Reply struct {
	Text  string
	Calls []ToolCall
}

// ToolProvider is an optional capability a Provider may implement to support
// tool/function-calling. CompleteTools sends the conversation along with the
// available tool definitions and returns either the model's final text or the
// tool calls it wants executed. A Provider that does not implement this cannot
// run tool functions — RunTools returns a clear error.
type ToolProvider interface {
	CompleteTools(ctx context.Context, messages []Message, tools []ToolDef) (Reply, error)
}

// Tool binds a ToolDef to its Go implementation. Invoke receives the model's raw
// JSON arguments and returns the result as JSON to feed back into the
// conversation. Generated code builds these by wrapping a typed handler (decode
// args → call handler → marshal result); you can also construct them by hand.
type Tool struct {
	Def    ToolDef
	Invoke func(ctx context.Context, argsJSON string) (resultJSON string, err error)
}

// RunTools runs prompt against a tool-capable provider, executing the Go tools
// the model asks for and feeding their results back, until the model returns a
// final answer that coerces into T. The loop is bounded by Options.MaxSteps
// (default 8).
//
// An unknown tool name or a handler error is fed back to the model as a tool
// result so it can recover, rather than aborting the run. If the model's final
// answer will not coerce into T, the parse error is fed back for one repair
// re-ask (within the step budget), mirroring Extract.
//
// This is what a generated tool-using function calls. The provider must
// implement ToolProvider; otherwise RunTools returns an error.
func RunTools[T any](ctx context.Context, p Provider, prompt string, tools []Tool, opts Options) (T, error) {
	var zero T
	tp, ok := p.(ToolProvider)
	if !ok {
		return zero, fmt.Errorf("promptr: provider %T does not support tool calls", p)
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

	steps := opts.maxSteps()
	var lastErr error
	for step := 0; step < steps; step++ {
		reply, err := tp.CompleteTools(ctx, msgs, defs)
		if err != nil {
			return zero, err
		}

		if len(reply.Calls) == 0 {
			v, repair, ferr := finalize[T](reply.Text, opts)
			if ferr == nil {
				return v, nil
			}
			lastErr = ferr
			msgs = append(msgs,
				Message{Role: "assistant", Content: reply.Text},
				Message{Role: "user", Content: repair},
			)
			continue
		}

		// Echo the assistant's tool-call turn, then append each tool's result so
		// the model can use it on the next turn. Results keep request order even
		// when dispatched concurrently.
		msgs = append(msgs, Message{Role: "assistant", ToolCalls: reply.Calls})
		results := runCalls(ctx, byName, reply.Calls, opts.ParallelTools)
		for i, call := range reply.Calls {
			msgs = append(msgs, Message{Role: "tool", ToolCallID: call.ID, Content: results[i]})
		}
	}

	if lastErr != nil {
		return zero, lastErr
	}
	return zero, fmt.Errorf("promptr: tool loop did not converge within %d steps", steps)
}

// runCalls executes the tool calls of a single model turn and returns their
// results in request order. When parallel is set and there is more than one call,
// each runs in its own goroutine — the calls of one turn are independent, so a
// model that requests several at once gets them dispatched concurrently. Each
// goroutine writes only its own slot, so no result is shared between them.
func runCalls(ctx context.Context, byName map[string]Tool, calls []ToolCall, parallel bool) []string {
	results := make([]string, len(calls))
	if !parallel || len(calls) <= 1 {
		for i, call := range calls {
			results[i] = dispatch(ctx, byName, call)
		}
		return results
	}
	var wg sync.WaitGroup
	wg.Add(len(calls))
	for i, call := range calls {
		go func(i int, call ToolCall) {
			defer wg.Done()
			results[i] = dispatch(ctx, byName, call)
		}(i, call)
	}
	wg.Wait()
	return results
}

// dispatch runs one tool call, returning the result JSON, or an error message as
// a string that is fed back to the model so it can recover (an unknown tool or a
// handler failure does not abort the whole run).
func dispatch(ctx context.Context, byName map[string]Tool, call ToolCall) string {
	t, ok := byName[call.Name]
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", call.Name)
	}
	out, err := t.Invoke(ctx, call.Arguments)
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}
