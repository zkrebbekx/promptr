package coerce_test

import (
	"testing"

	"github.com/zkrebbekx/promptr/coerce"

	. "github.com/smartystreets/goconvey/convey"
)

// A sumx-style sealed union of agent actions.
type Action interface{ isAction() }

type Search struct {
	Query string `json:"query"`
}
type Reply struct {
	Message string `json:"message"`
}
type Escalate struct {
	Team   string `json:"team"`
	Reason string `json:"reason"`
}

func (Search) isAction()   {}
func (Reply) isAction()    {}
func (Escalate) isAction() {}

func TestUnionBestFit(t *testing.T) {
	Convey("Given a union of three action shapes", t, func() {
		u := coerce.NewUnion(Search{}, Reply{}, Escalate{})

		Convey("When output uniquely matches one shape's fields", func() {
			s, errS := coerce.ResolveInto[Action](`{"query": "weather in NYC"}`, u)
			e, errE := coerce.ResolveInto[Action](`{"team": "sre", "reason": "db down"}`, u)

			Convey("Then the best-fitting variant is selected and typed", func() {
				So(errS, ShouldBeNil)
				search, ok := s.(Search)
				So(ok, ShouldBeTrue)
				So(search.Query, ShouldEqual, "weather in NYC")

				So(errE, ShouldBeNil)
				esc, ok := e.(Escalate)
				So(ok, ShouldBeTrue)
				So(esc.Team, ShouldEqual, "sre")
				So(esc.Reason, ShouldEqual, "db down")
			})
		})

		Convey("When an explicit discriminator is present", func() {
			r, err := coerce.ResolveInto[Action](`{"type": "reply", "message": "on it"}`, u)

			Convey("Then the named variant wins regardless of shape overlap", func() {
				So(err, ShouldBeNil)
				reply, ok := r.(Reply)
				So(ok, ShouldBeTrue)
				So(reply.Message, ShouldEqual, "on it")
			})
		})

		Convey("When the output is wrapped in prose and a fence", func() {
			raw := "I'll search for that:\n```json\n{\"query\": \"go generics\"}\n```"
			s, err := coerce.ResolveInto[Action](raw, u)

			Convey("Then extraction and resolution still work together", func() {
				So(err, ShouldBeNil)
				_, ok := s.(Search)
				So(ok, ShouldBeTrue)
			})
		})
	})
}
