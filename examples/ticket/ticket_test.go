package ticket

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/zkrebbekx/promptr/providers/fake"
)

func TestExtractTicketEndToEnd(t *testing.T) {
	Convey("Given a model reply wrapped in prose and a fenced, loose object", t, func() {
		reply := "Sure! Here is the ticket:\n```json\n" +
			"{ title: 'Server is down', severity: 'critical priority', tags: ['outage','prod',], due_days: \"1\" }\n" +
			"```\nHope that helps!"
		p := fake.New(reply)

		Convey("When ExtractTicket runs against the fake provider", func() {
			got, err := ExtractTicket(context.Background(), p, "my server is down!!")

			Convey("Then the loose reply coerces into a typed Ticket", func() {
				So(err, ShouldBeNil)
				So(got.Title, ShouldEqual, "Server is down")
				So(got.Severity, ShouldEqual, SeverityCRITICAL) // fuzzy enum match
				So(got.Tags, ShouldResemble, []string{"outage", "prod"})
				So(got.DueDays, ShouldNotBeNil)
				So(*got.DueDays, ShouldEqual, 1) // "1" coerced to *int
			})

			Convey("Then the generated prompt carried the input and schema", func() {
				So(p.Calls, ShouldHaveLength, 1)
				user := p.Calls[0][len(p.Calls[0])-1].Content
				So(user, ShouldContainSubstring, "my server is down!!")
				So(user, ShouldContainSubstring, "one of [LOW, HIGH, CRITICAL]")
			})
		})
	})

	Convey("Given a first reply that cannot parse and a valid second reply", t, func() {
		p := fake.New(
			"I'm not sure I can do that.",
			`{"title":"Disk full","severity":"HIGH","tags":["storage"]}`,
		)

		Convey("When ExtractTicket runs (Attempts=2)", func() {
			got, err := ExtractTicket(context.Background(), p, "disk is full")

			Convey("Then the repair retry recovers a typed Ticket", func() {
				So(err, ShouldBeNil)
				So(got.Title, ShouldEqual, "Disk full")
				So(got.Severity, ShouldEqual, SeverityHIGH)
				So(got.DueDays, ShouldBeNil) // optional, absent
			})

			Convey("Then it took two model calls and re-asked with the parse error", func() {
				So(p.Calls, ShouldHaveLength, 2)
				lastConvo := p.Calls[1]
				So(len(lastConvo), ShouldBeGreaterThan, 1)
				So(lastConvo[len(lastConvo)-1].Content, ShouldContainSubstring, "could not be parsed")
			})
		})
	})
}
