package promptr

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// streamTP is a scripted StreamToolProvider: each turn streams its text in
// fixed-size chunks (so partials accumulate) or hands back tool calls.
type streamTP struct {
	replies []Reply
	seen    [][]Message
	n       int
}

func (p *streamTP) Complete(context.Context, []Message) (string, error) { return "", nil }

func (p *streamTP) StreamTools(ctx context.Context, msgs []Message, _ []ToolDef) (ToolStream, error) {
	p.seen = append(p.seen, msgs)
	i := p.n
	if i >= len(p.replies) {
		i = len(p.replies) - 1
	}
	p.n++
	r := p.replies[i]
	deltas := make(chan string)
	go func() {
		defer close(deltas)
		if len(r.Calls) > 0 {
			return
		}
		const size = 4
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
	return ToolStream{Deltas: deltas, Reply: func() (Reply, error) { return r, nil }}, nil
}

func drain[T any](ch <-chan Partial[T]) []Partial[T] {
	var got []Partial[T]
	for p := range ch {
		got = append(got, p)
	}
	return got
}

func TestRunToolsStreamFinalAnswerStreams(t *testing.T) {
	Convey("Given a provider that calls a tool then streams a final answer", t, func() {
		p := &streamTP{replies: []Reply{
			{Calls: []ToolCall{{ID: "1", Name: "add", Arguments: `{"a":2,"b":3}`}}},
			{Text: `{"total": 5}`},
		}}
		var invoked int

		Convey("When RunToolsStream drives the loop", func() {
			ch, err := RunToolsStream[sum](context.Background(), p, "add 2 and 3", []Tool{addTool(&invoked)}, Options{})
			So(err, ShouldBeNil)
			got := drain(ch)

			Convey("Then the tool ran and a Complete partial carried the typed answer", func() {
				So(invoked, ShouldEqual, 1)
				last := got[len(got)-1]
				So(last.Err, ShouldBeNil)
				So(last.Complete, ShouldBeTrue)
				So(last.Value.Total, ShouldEqual, 5)
			})

			Convey("Then progressive partials arrived before the final one", func() {
				// The 12-byte answer streamed in 4-byte chunks yields at least one
				// pre-complete partial once the prefix first coerces.
				var pre int
				for _, pt := range got {
					if !pt.Complete && pt.Err == nil {
						pre++
					}
				}
				So(pre, ShouldBeGreaterThan, 0)
			})

			Convey("Then the second turn carried the tool result back", func() {
				second := p.seen[1]
				var toolResults int
				for _, m := range second {
					if m.Role == "tool" && m.ToolCallID == "1" {
						toolResults++
					}
				}
				So(toolResults, ShouldEqual, 1)
			})
		})
	})
}

func TestRunToolsStreamFallsBackWithoutStreamProvider(t *testing.T) {
	Convey("Given a tool provider that does not implement StreamToolProvider", t, func() {
		// scriptedTP (tools_test.go) is a ToolProvider but not a StreamToolProvider.
		p := &scriptedTP{replies: []Reply{
			{Calls: []ToolCall{{ID: "1", Name: "add", Arguments: `{"a":1,"b":1}`}}},
			{Text: `{"total": 2}`},
		}}
		var invoked int

		Convey("When RunToolsStream runs", func() {
			ch, err := RunToolsStream[sum](context.Background(), p, "go", []Tool{addTool(&invoked)}, Options{})
			So(err, ShouldBeNil)
			got := drain(ch)

			Convey("Then it falls back to a single final Partial", func() {
				So(got, ShouldHaveLength, 1)
				So(got[0].Complete, ShouldBeTrue)
				So(got[0].Value.Total, ShouldEqual, 2)
				So(invoked, ShouldEqual, 1)
			})
		})
	})
}

func TestRunToolsStreamReportsConvergenceFailure(t *testing.T) {
	Convey("Given a provider that never returns a coercible final answer", t, func() {
		p := &streamTP{replies: []Reply{{Text: "not json at all"}}}

		Convey("When RunToolsStream exhausts its repair budget", func() {
			ch, err := RunToolsStream[sum](context.Background(), p, "go", nil, Options{MaxSteps: 2})
			So(err, ShouldBeNil)
			got := drain(ch)

			Convey("Then the final Partial carries the error", func() {
				last := got[len(got)-1]
				So(last.Err, ShouldNotBeNil)
				So(last.Complete, ShouldBeFalse)
			})
		})
	})
}
