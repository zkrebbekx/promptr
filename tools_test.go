package promptr

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// scriptedTP is a ToolProvider that returns canned Replies in order, recording
// the messages it was shown so the loop's bookkeeping can be asserted.
type scriptedTP struct {
	replies []Reply
	n       int
	seen    [][]Message
}

func (s *scriptedTP) Complete(_ context.Context, _ []Message) (string, error) { return "", nil }

func (s *scriptedTP) CompleteTools(_ context.Context, msgs []Message, _ []ToolDef) (Reply, error) {
	s.seen = append(s.seen, msgs)
	i := s.n
	if i >= len(s.replies) {
		i = len(s.replies) - 1
	}
	s.n++
	return s.replies[i], nil
}

type sum struct {
	Total int `json:"total"`
}

func addTool(invoked *int) Tool {
	return Tool{
		Def: ToolDef{Name: "add"},
		Invoke: func(_ context.Context, _ string) (string, error) {
			*invoked++
			return `{"result": 5}`, nil
		},
	}
}

func TestRunToolsHappyPath(t *testing.T) {
	Convey("Given a provider that calls a tool then answers", t, func() {
		p := &scriptedTP{replies: []Reply{
			{Calls: []ToolCall{{ID: "1", Name: "add", Arguments: `{"a":2,"b":3}`}}},
			{Text: `{"total": 5}`},
		}}
		var invoked int

		Convey("When RunTools drives the loop", func() {
			got, err := RunTools[sum](context.Background(), p, "add 2 and 3", []Tool{addTool(&invoked)}, Options{})
			So(err, ShouldBeNil)

			Convey("Then the tool ran and the typed answer came back", func() {
				So(invoked, ShouldEqual, 1)
				So(got.Total, ShouldEqual, 5)
			})

			Convey("Then the second turn carried the assistant call and the tool result", func() {
				second := p.seen[1]
				var assistantCalls, toolResults int
				for _, m := range second {
					if m.Role == "assistant" && len(m.ToolCalls) > 0 {
						assistantCalls++
					}
					if m.Role == "tool" && m.ToolCallID == "1" {
						toolResults++
					}
				}
				So(assistantCalls, ShouldEqual, 1)
				So(toolResults, ShouldEqual, 1)
			})
		})
	})
}

func TestRunToolsUnknownToolRecovers(t *testing.T) {
	Convey("Given the model asks for a tool that was not provided", t, func() {
		p := &scriptedTP{replies: []Reply{
			{Calls: []ToolCall{{ID: "1", Name: "missing", Arguments: `{}`}}},
			{Text: `{"total": 0}`},
		}}

		Convey("When RunTools dispatches it", func() {
			got, err := RunTools[sum](context.Background(), p, "go", []Tool{addTool(new(int))}, Options{})
			So(err, ShouldBeNil)

			Convey("Then an error result is fed back rather than aborting", func() {
				So(got.Total, ShouldEqual, 0)
				result := p.seen[1][len(p.seen[1])-1]
				So(result.Role, ShouldEqual, "tool")
				So(result.Content, ShouldContainSubstring, "unknown tool")
			})
		})
	})
}

func TestRunToolsHandlerErrorRecovers(t *testing.T) {
	Convey("Given a tool whose handler returns an error", t, func() {
		p := &scriptedTP{replies: []Reply{
			{Calls: []ToolCall{{ID: "1", Name: "boom", Arguments: `{}`}}},
			{Text: `{"total": 1}`},
		}}
		failing := Tool{
			Def:    ToolDef{Name: "boom"},
			Invoke: func(_ context.Context, _ string) (string, error) { return "", errors.New("kaboom") },
		}

		Convey("When RunTools dispatches it", func() {
			got, err := RunTools[sum](context.Background(), p, "go", []Tool{failing}, Options{})
			So(err, ShouldBeNil)

			Convey("Then the handler error is fed back to the model", func() {
				So(got.Total, ShouldEqual, 1)
				result := p.seen[1][len(p.seen[1])-1]
				So(result.Content, ShouldContainSubstring, "kaboom")
			})
		})
	})
}

func TestRunToolsBudgetExhausted(t *testing.T) {
	Convey("Given a provider that always asks for a tool", t, func() {
		p := &scriptedTP{replies: []Reply{
			{Calls: []ToolCall{{ID: "1", Name: "add", Arguments: `{}`}}},
		}}

		Convey("When RunTools runs with a small MaxSteps", func() {
			_, err := RunTools[sum](context.Background(), p, "loop", []Tool{addTool(new(int))}, Options{MaxSteps: 3})

			Convey("Then it gives up with a convergence error", func() {
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, "did not converge")
			})
		})
	})
}

func TestRunToolsRequiresToolProvider(t *testing.T) {
	Convey("Given a provider that does not implement ToolProvider", t, func() {
		p := ProviderFunc(func(_ context.Context, _ []Message) (string, error) { return "", nil })

		Convey("When RunTools is called", func() {
			_, err := RunTools[sum](context.Background(), p, "go", nil, Options{})

			Convey("Then it returns a clear error", func() {
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, "does not support tool calls")
			})
		})
	})
}
