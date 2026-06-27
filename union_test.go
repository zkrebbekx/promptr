package promptr_test

import (
	"context"
	"testing"

	"github.com/zkrebbekx/promptr"
	"github.com/zkrebbekx/promptr/providers/fake"

	. "github.com/smartystreets/goconvey/convey"
)

// A sealed union mirroring what codegen emits for `union Action = Search | Escalate`.
type action interface{ isAction() }

type search struct {
	Query string `json:"query"`
}
type escalate struct {
	Reason string `json:"reason"`
}

func (search) isAction()   {}
func (escalate) isAction() {}

func TestExtractUnion(t *testing.T) {
	Convey("Given a union resolver over two variant shapes", t, func() {
		u := promptr.NewUnion(search{}, escalate{})

		Convey("When the model returns an escalate-shaped object", func() {
			p := fake.New(`{"reason": "server on fire"}`)
			got, err := promptr.ExtractUnion[action](context.Background(), p, "classify", promptr.Options{}, u)

			Convey("Then it resolves to the escalate variant", func() {
				So(err, ShouldBeNil)
				e, ok := got.(escalate)
				So(ok, ShouldBeTrue)
				So(e.Reason, ShouldEqual, "server on fire")
			})
		})

		Convey("When the first reply is unparseable, it re-asks and recovers", func() {
			p := fake.New("sorry, I can't help", `{"query": "find docs"}`)
			got, err := promptr.ExtractUnion[action](context.Background(), p, "classify", promptr.Options{Attempts: 2}, u)

			Convey("Then the second attempt resolves to search", func() {
				So(err, ShouldBeNil)
				s, ok := got.(search)
				So(ok, ShouldBeTrue)
				So(s.Query, ShouldEqual, "find docs")
				So(len(p.Calls), ShouldEqual, 2)
			})
		})
	})
}
