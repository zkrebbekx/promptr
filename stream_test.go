package promptr_test

import (
	"context"
	"testing"

	"github.com/zkrebbekx/promptr"
	"github.com/zkrebbekx/promptr/providers/fake"

	. "github.com/smartystreets/goconvey/convey"
)

type caption struct {
	Text string `json:"text"`
}

// completeOnly is a Provider that does NOT implement StreamProvider, to exercise
// ExtractStream's non-streaming fallback path.
type completeOnly struct{ reply string }

func (c completeOnly) Complete(_ context.Context, _ []promptr.Message) (string, error) {
	return c.reply, nil
}

func drain[T any](ch <-chan promptr.Partial[T]) promptr.Partial[T] {
	var last promptr.Partial[T]
	for p := range ch {
		last = p
	}
	return last
}

func TestExtractStream(t *testing.T) {
	Convey("Given a streaming provider that emits a JSON object in chunks", t, func() {
		p := fake.New(`{"text": "a sunny meadow"}`)
		p.ChunkSize = 4 // force many partial snapshots

		Convey("When ExtractStream consumes it", func() {
			ch, err := promptr.ExtractStream[caption](context.Background(), p, "describe", promptr.Options{})
			So(err, ShouldBeNil)
			last := drain(ch)

			Convey("Then the final partial is complete and fully parsed", func() {
				So(last.Err, ShouldBeNil)
				So(last.Complete, ShouldBeTrue)
				So(last.Value.Text, ShouldEqual, "a sunny meadow")
			})
		})
	})

	Convey("Given a provider that does NOT stream", t, func() {
		p := completeOnly{reply: `{"text": "fallback works"}`}

		Convey("When ExtractStream falls back to a single Complete call", func() {
			ch, err := promptr.ExtractStream[caption](context.Background(), p, "describe", promptr.Options{})
			So(err, ShouldBeNil)
			last := drain(ch)

			Convey("Then it still yields one complete partial", func() {
				So(last.Complete, ShouldBeTrue)
				So(last.Value.Text, ShouldEqual, "fallback works")
			})
		})
	})
}
