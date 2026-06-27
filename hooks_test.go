package promptr

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// capProvider implements all three capability interfaces so the wrapper's
// capability preservation can be asserted.
type capProvider struct {
	text       string
	streamToks []string
	reply      Reply
}

func (c capProvider) Complete(context.Context, []Message) (string, error) {
	return c.text, nil
}

func (c capProvider) Stream(context.Context, []Message) (<-chan string, error) {
	ch := make(chan string, len(c.streamToks))
	for _, t := range c.streamToks {
		ch <- t
	}
	close(ch)
	return ch, nil
}

func (c capProvider) CompleteTools(context.Context, []Message, []ToolDef) (Reply, error) {
	return c.reply, nil
}

// recordHook captures the kinds and outcomes it observes.
type recordHook struct {
	before []CallKind
	after  []Outcome
}

func (r *recordHook) BeforeCall(_ context.Context, info CallInfo) AfterFunc {
	r.before = append(r.before, info.Kind)
	return func(o Outcome) { r.after = append(r.after, o) }
}

// plainProvider implements only Complete.
type plainProvider struct{ text string }

func (p plainProvider) Complete(context.Context, []Message) (string, error) { return p.text, nil }

func TestWithHooksFiresOnEveryPath(t *testing.T) {
	Convey("Given a fully capable provider wrapped with a recording hook", t, func() {
		p := capProvider{
			text:       "hi",
			streamToks: []string{"Hel", "lo"},
			reply:      Reply{Calls: []ToolCall{{ID: "1", Name: "add"}}},
		}
		rec := &recordHook{}
		wrapped := WithHooks(p, rec)

		Convey("Then it still satisfies StreamProvider and ToolProvider", func() {
			_, isStream := wrapped.(StreamProvider)
			_, isTools := wrapped.(ToolProvider)
			So(isStream, ShouldBeTrue)
			So(isTools, ShouldBeTrue)
		})

		Convey("When Complete runs, the complete path fires before and after", func() {
			out, err := wrapped.Complete(context.Background(), []Message{{Role: "user", Content: "x"}})
			So(err, ShouldBeNil)
			So(out, ShouldEqual, "hi")
			So(rec.before, ShouldResemble, []CallKind{KindComplete})
			So(rec.after, ShouldHaveLength, 1)
			So(rec.after[0].Text, ShouldEqual, "hi")
		})

		Convey("When the stream is drained, the after fires with the full text", func() {
			ch, err := wrapped.(StreamProvider).Stream(context.Background(), nil)
			So(err, ShouldBeNil)
			var got string
			for tok := range ch {
				got += tok
			}
			So(got, ShouldEqual, "Hello")
			So(rec.before, ShouldResemble, []CallKind{KindStream})
			So(rec.after, ShouldHaveLength, 1)
			So(rec.after[0].Text, ShouldEqual, "Hello")
		})

		Convey("When CompleteTools runs, the tools path fires with the calls", func() {
			reply, err := wrapped.(ToolProvider).CompleteTools(context.Background(), nil, nil)
			So(err, ShouldBeNil)
			So(reply.Calls, ShouldHaveLength, 1)
			So(rec.before, ShouldResemble, []CallKind{KindTools})
			So(rec.after[0].Calls, ShouldHaveLength, 1)
		})
	})
}

func TestWithHooksPreservesLackOfCapabilities(t *testing.T) {
	Convey("Given a Complete-only provider wrapped with hooks", t, func() {
		wrapped := WithHooks(plainProvider{text: "ok"}, &recordHook{})

		Convey("Then it does not falsely advertise streaming or tools", func() {
			_, isStream := wrapped.(StreamProvider)
			_, isTools := wrapped.(ToolProvider)
			So(isStream, ShouldBeFalse)
			So(isTools, ShouldBeFalse)
		})
	})

	Convey("Given WithHooks called with no hooks", t, func() {
		p := plainProvider{text: "ok"}
		Convey("Then it returns the provider unchanged", func() {
			So(WithHooks(p), ShouldEqual, p)
		})
	})
}

func TestCollectorHookCapturesAllPaths(t *testing.T) {
	Convey("Given a Collector wired as a hook over a capable provider", t, func() {
		col := &Collector{}
		p := capProvider{text: "hi", streamToks: []string{"a", "b"}, reply: Reply{Text: "done"}}
		wrapped := WithHooks(p, col.Hook())

		Convey("When all three call kinds run", func() {
			_, _ = wrapped.Complete(context.Background(), nil)
			ch, _ := wrapped.(StreamProvider).Stream(context.Background(), nil)
			var streamed string
			for tok := range ch {
				streamed += tok
			}
			So(streamed, ShouldEqual, "ab")
			_, _ = wrapped.(ToolProvider).CompleteTools(context.Background(), nil, nil)

			Convey("Then the collector recorded a call for each", func() {
				So(col.Stats().Calls, ShouldEqual, 3)
			})
		})
	})
}
