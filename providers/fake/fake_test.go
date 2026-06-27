package fake

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/zkrebbekx/promptr"
)

func TestStreamToolsScript(t *testing.T) {
	Convey("Given a fake scripted with a tool call then a final-text reply", t, func() {
		p := &Provider{ToolReplies: []Reply{
			{Calls: []promptr.ToolCall{{ID: "1", Name: "add", Arguments: `{}`}}},
			{Text: `{"total":5}`},
		}}

		Convey("When the first StreamTools turn is consumed", func() {
			ts, err := p.StreamTools(context.Background(), nil, nil)
			So(err, ShouldBeNil)
			var streamed string
			for tok := range ts.Deltas {
				streamed += tok
			}
			reply, _ := ts.Reply()

			Convey("Then a tool-call turn streams no text but reports its calls", func() {
				So(streamed, ShouldEqual, "")
				So(reply.Calls, ShouldHaveLength, 1)
				So(reply.Calls[0].Name, ShouldEqual, "add")
			})
		})
	})
}

func TestStreamToolsStreamsFinalText(t *testing.T) {
	Convey("Given a fake whose only reply is final text", t, func() {
		p := &Provider{ChunkSize: 3, ToolReplies: []Reply{{Text: `{"total":5}`}}}

		Convey("When StreamTools streams it", func() {
			ts, err := p.StreamTools(context.Background(), nil, nil)
			So(err, ShouldBeNil)
			var got string
			var chunks int
			for tok := range ts.Deltas {
				got += tok
				chunks++
			}
			reply, _ := ts.Reply()

			Convey("Then the text streams in chunks and Reply carries the whole text", func() {
				So(got, ShouldEqual, `{"total":5}`)
				So(chunks, ShouldBeGreaterThan, 1)
				So(reply.Text, ShouldEqual, `{"total":5}`)
				So(reply.Calls, ShouldBeEmpty)
			})
		})
	})
}
