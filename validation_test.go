package promptr

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// scriptedP is a plain Provider that returns canned completions in order,
// recording each conversation so the repair loop can be asserted.
type scriptedP struct {
	replies []string
	n       int
	seen    [][]Message
}

func (s *scriptedP) Complete(_ context.Context, msgs []Message) (string, error) {
	s.seen = append(s.seen, msgs)
	i := s.n
	if i >= len(s.replies) {
		i = len(s.replies) - 1
	}
	s.n++
	return s.replies[i], nil
}

type account struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestExtractValidateDrivesRepair(t *testing.T) {
	Convey("Given Options.Validate that rejects the first reply", t, func() {
		p := &scriptedP{replies: []string{
			`{"name":"al","age":30}`,
			`{"name":"alice","age":30}`,
		}}
		opts := Options{Attempts: 2, Validate: func(v any) error {
			a := v.(account)
			if len(a.Name) < 3 {
				return errors.New("name too short")
			}
			return nil
		}}

		Convey("When Extract runs", func() {
			got, err := Extract[account](context.Background(), p, "go", opts)

			Convey("Then the validation failure re-asks and the second reply wins", func() {
				So(err, ShouldBeNil)
				So(got.Name, ShouldEqual, "alice")
				So(p.seen, ShouldHaveLength, 2)
				last := p.seen[1]
				So(last[len(last)-1].Content, ShouldContainSubstring, "did not satisfy the required constraints")
				So(last[len(last)-1].Content, ShouldContainSubstring, "name too short")
			})
		})
	})

	Convey("Given a Validate that never passes", t, func() {
		p := &scriptedP{replies: []string{`{"name":"x","age":1}`}}
		opts := Options{Attempts: 2, Validate: func(any) error { return errors.New("nope") }}

		Convey("When Extract exhausts its attempts", func() {
			_, err := Extract[account](context.Background(), p, "go", opts)

			Convey("Then it returns the last validation error", func() {
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldEqual, "nope")
			})
		})
	})
}

func TestExtractCheckIsAdvisory(t *testing.T) {
	Convey("Given a soft Check that fails", t, func() {
		p := &scriptedP{replies: []string{`{"name":"alice","age":15}`}}

		Convey("When OnCheck is set", func() {
			var seen error
			opts := Options{
				Check:   func(any) error { return errors.New("too young") },
				OnCheck: func(e error) { seen = e },
			}
			got, err := Extract[account](context.Background(), p, "go", opts)

			Convey("Then the value is still returned and the violation reaches OnCheck", func() {
				So(err, ShouldBeNil)
				So(got.Name, ShouldEqual, "alice")
				So(p.seen, ShouldHaveLength, 1) // no repair re-ask
				So(seen, ShouldNotBeNil)
				So(seen.Error(), ShouldEqual, "too young")
			})
		})

		Convey("When OnCheck is nil", func() {
			called := false
			opts := Options{Check: func(any) error { called = true; return errors.New("x") }}
			_, err := Extract[account](context.Background(), p, "go", opts)

			Convey("Then Check is not even invoked", func() {
				So(err, ShouldBeNil)
				So(called, ShouldBeFalse)
			})
		})
	})
}

func TestOptionHelpers(t *testing.T) {
	Convey("Given the functional Option helpers", t, func() {
		var o Options
		o.apply([]Option{
			WithAttempts(5),
			WithMaxSteps(9),
			WithSystem("be terse"),
			OnCheck(func(error) {}),
		})

		Convey("Then each mutates the matching Options field", func() {
			So(o.Attempts, ShouldEqual, 5)
			So(o.MaxSteps, ShouldEqual, 9)
			So(o.System, ShouldEqual, "be terse")
			So(o.OnCheck, ShouldNotBeNil)
		})
	})
}
