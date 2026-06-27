package recorded_test

import (
	"context"
	"testing"

	"github.com/zkrebbekx/promptr"
	"github.com/zkrebbekx/promptr/providers/recorded"

	. "github.com/smartystreets/goconvey/convey"
)

func msg(s string) []promptr.Message { return []promptr.Message{{Role: "user", Content: s}} }

func TestRecorded(t *testing.T) {
	Convey("Given a sequential cassette", t, func() {
		p, err := recorded.New([]byte(`{"interactions":[
			{"reply":"first"},
			{"reply":"second"}
		]}`))
		So(err, ShouldBeNil)

		Convey("When Complete is called repeatedly, replies come back in order", func() {
			a, _ := p.Complete(context.Background(), msg("x"))
			b, _ := p.Complete(context.Background(), msg("y"))
			So(a, ShouldEqual, "first")
			So(b, ShouldEqual, "second")

			Convey("And the exhausted cassette errors", func() {
				_, err := p.Complete(context.Background(), msg("z"))
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, "no remaining interaction")
			})
		})
	})

	Convey("Given a match-routed cassette", t, func() {
		p, err := recorded.New([]byte(`{"interactions":[
			{"match":"escalate","reply":"to oncall","prompt_tokens":11,"reply_tokens":3},
			{"match":"search","reply":"results"}
		]}`))
		So(err, ShouldBeNil)

		Convey("When a request mentions search, the search interaction is chosen regardless of order", func() {
			got, err := p.Complete(context.Background(), msg("please search the docs"))
			So(err, ShouldBeNil)
			So(got, ShouldEqual, "results")
		})

		Convey("When a request mentions escalate, its usage is reported", func() {
			_, err := p.Complete(context.Background(), msg("escalate this now"))
			So(err, ShouldBeNil)
			pt, rt := p.LastUsage()
			So(pt, ShouldEqual, 11)
			So(rt, ShouldEqual, 3)
		})
	})

	Convey("Given empty cassette JSON", t, func() {
		_, err := recorded.New([]byte(`{"interactions":[]}`))
		So(err, ShouldNotBeNil)
	})
}
