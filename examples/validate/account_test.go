package validate

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/zkrebbekx/promptr"
	"github.com/zkrebbekx/promptr/providers/fake"
)

func TestExtractAccountHardAndSoftRules(t *testing.T) {
	Convey("Given a reply that already satisfies every rule", t, func() {
		p := fake.New(`{"email":"a@b.com","username":"alice","age":30,"plan":"PRO","seats":5}`)

		Convey("When ExtractAccount runs", func() {
			got, err := ExtractAccount(context.Background(), p, "make me an account")

			Convey("Then it returns the typed Account in one call", func() {
				So(err, ShouldBeNil)
				So(got.Username, ShouldEqual, "alice")
				So(got.Age, ShouldEqual, 30)
				So(p.Calls, ShouldHaveLength, 1)
			})
		})
	})

	Convey("Given a first reply that violates an @assert rule, then a valid one", t, func() {
		p := fake.New(
			// username too short — fails @assert("min=3,max=20")
			`{"email":"a@b.com","username":"ab","age":30,"plan":"PRO","seats":5}`,
			`{"email":"a@b.com","username":"alice","age":30,"plan":"PRO","seats":5}`,
		)

		Convey("When ExtractAccount runs (Attempts=2)", func() {
			got, err := ExtractAccount(context.Background(), p, "make me an account")

			Convey("Then the @assert violation drove a repair re-ask that recovered", func() {
				So(err, ShouldBeNil)
				So(got.Username, ShouldEqual, "alice")
				So(p.Calls, ShouldHaveLength, 2)
				last := p.Calls[1]
				So(last[len(last)-1].Content, ShouldContainSubstring, "did not satisfy the required constraints")
				So(last[len(last)-1].Content, ShouldContainSubstring, "at least 3")
			})
		})
	})

	Convey("Given a reply that fails only a soft @check rule", t, func() {
		// age 15 passes @assert(gt=0,lt=130) but fails @check(min=18); seats 0
		// fails @check(min=1). Both are advisory.
		p := fake.New(`{"email":"a@b.com","username":"alice","age":15,"plan":"FREE","seats":0}`)

		Convey("When ExtractAccount runs with an OnCheck sink", func() {
			var checkErr error
			got, err := ExtractAccount(context.Background(), p, "make me an account",
				promptr.OnCheck(func(e error) { checkErr = e }))

			Convey("Then the value is still returned and the soft violations reach the sink", func() {
				So(err, ShouldBeNil)
				So(got.Age, ShouldEqual, 15)
				So(p.Calls, ShouldHaveLength, 1) // no repair re-ask for soft checks
				So(checkErr, ShouldNotBeNil)
				So(checkErr.Error(), ShouldContainSubstring, "Age must be at least 18")
				So(checkErr.Error(), ShouldContainSubstring, "Seats must be at least 1")
			})
		})

		Convey("When ExtractAccount runs without an OnCheck sink", func() {
			got, err := ExtractAccount(context.Background(), p, "make me an account")

			Convey("Then the soft violations are silently skipped", func() {
				So(err, ShouldBeNil)
				So(got.Age, ShouldEqual, 15)
			})
		})
	})
}
