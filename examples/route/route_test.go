package route

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/zkrebbekx/promptr/providers/fake"
)

func TestRouteEndToEnd(t *testing.T) {
	Convey("Given a model reply shaped like the Escalate variant", t, func() {
		p := fake.New("```json\n{ reason: 'customer is furious', metadata: { tier: 'gold' } }\n```")

		Convey("When Route runs against the fake provider", func() {
			got, err := Route(context.Background(), p, "I want a refund NOW")

			Convey("Then the union resolves to a typed Escalate", func() {
				So(err, ShouldBeNil)
				e, ok := got.(Escalate)
				So(ok, ShouldBeTrue)
				So(e.Reason, ShouldEqual, "customer is furious")
				So(e.Metadata["tier"], ShouldEqual, "gold")
			})

			Convey("Then the prompt showed the model the ONE-of choice and alias", func() {
				user := p.Calls[0][len(p.Calls[0])-1].Content
				So(user, ShouldContainSubstring, "matching exactly ONE of these shapes")
				So(user, ShouldContainSubstring, "max_results")            // @alias surfaced in schema
				So(user, ShouldContainSubstring, "why this needs a human") // @description surfaced
			})
		})
	})

	Convey("Given a Search-shaped reply with an aliased field", t, func() {
		p := fake.New(`{"query": "go generics", "max_results": 5}`)

		Convey("When Route runs", func() {
			got, err := Route(context.Background(), p, "find docs on go generics")

			Convey("Then it resolves to Search and binds the aliased field", func() {
				So(err, ShouldBeNil)
				s, ok := got.(Search)
				So(ok, ShouldBeTrue)
				So(s.Query, ShouldEqual, "go generics")
				So(s.Topk, ShouldEqual, 5) // model's "max_results" → Topk via @alias
			})
		})
	})
}
