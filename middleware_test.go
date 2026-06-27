package promptr_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zkrebbekx/promptr"

	. "github.com/smartystreets/goconvey/convey"
)

// reporting is a provider that reports exact usage.
type reporting struct {
	reply        string
	prompt, repl int
}

func (r reporting) Complete(context.Context, []promptr.Message) (string, error) {
	return r.reply, nil
}
func (r reporting) LastUsage() (int, int) { return r.prompt, r.repl }

// failing always errors.
type failing struct{}

func (failing) Complete(context.Context, []promptr.Message) (string, error) {
	return "", errors.New("boom")
}

// scriptedTool is a ToolProvider that returns a final-text reply, for exercising
// CollectTools observability.
type scriptedTool struct{ text string }

func (s scriptedTool) Complete(context.Context, []promptr.Message) (string, error) {
	return s.text, nil
}
func (s scriptedTool) CompleteTools(context.Context, []promptr.Message, []promptr.ToolDef) (promptr.Reply, error) {
	return promptr.Reply{Text: s.text}, nil
}

func TestCollectTools(t *testing.T) {
	Convey("Given a Collector wrapping a tool provider", t, func() {
		col := &promptr.Collector{}
		tp := col.CollectTools(scriptedTool{text: `{"total": 5}`})

		Convey("When RunTools drives it to a final answer", func() {
			got, err := promptr.RunTools[struct {
				Total int `json:"total"`
			}](context.Background(), tp, "go", nil, promptr.Options{})
			So(err, ShouldBeNil)
			So(got.Total, ShouldEqual, 5)

			Convey("Then the CompleteTools call was recorded", func() {
				s := col.Stats()
				So(s.Calls, ShouldEqual, 1)
				So(s.ReplyTokens, ShouldBeGreaterThan, 0)
			})
		})
	})
}

func TestCollector(t *testing.T) {
	Convey("Given a Collector wrapping a non-reporting provider", t, func() {
		col := &promptr.Collector{}
		p := promptr.Chain(completeOnly{reply: "hello world"}, col.Collect)

		Convey("When a Complete runs", func() {
			out, err := p.Complete(context.Background(), []promptr.Message{{Role: "user", Content: "hi"}})
			So(err, ShouldBeNil)
			So(out, ShouldEqual, "hello world")

			Convey("Then one call is recorded with estimated tokens", func() {
				s := col.Stats()
				So(s.Calls, ShouldEqual, 1)
				So(s.Errors, ShouldEqual, 0)
				So(s.ReplyTokens, ShouldBeGreaterThan, 0) // "hello world" ~ 3 tokens
				So(col.Calls()[0].Estimated, ShouldBeTrue)
			})
		})
	})

	Convey("Given a Collector wrapping a usage-reporting provider", t, func() {
		col := &promptr.Collector{}
		p := col.Collect(reporting{reply: "ok", prompt: 42, repl: 7})

		Convey("When a Complete runs, the exact reported tokens are recorded", func() {
			_, _ = p.Complete(context.Background(), nil)
			s := col.Stats()
			So(s.PromptTokens, ShouldEqual, 42)
			So(s.ReplyTokens, ShouldEqual, 7)
			So(s.TotalTokens(), ShouldEqual, 49)
			So(col.Calls()[0].Estimated, ShouldBeFalse)
		})
	})

	Convey("Given a Collector over a failing provider", t, func() {
		col := &promptr.Collector{}
		p := col.Collect(failing{})

		Convey("When Complete errors, the error is counted and surfaced", func() {
			_, err := p.Complete(context.Background(), nil)
			So(err, ShouldNotBeNil)
			So(col.Stats().Errors, ShouldEqual, 1)
		})
	})
}
