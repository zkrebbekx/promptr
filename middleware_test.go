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
