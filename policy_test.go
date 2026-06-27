package promptr_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/zkrebbekx/promptr"
)

// flaky fails its first failN calls, then returns reply.
type flaky struct {
	failN int
	calls int
	reply string
}

func (f *flaky) Complete(context.Context, []promptr.Message) (string, error) {
	f.calls++
	if f.calls <= f.failN {
		return "", errors.New("boom")
	}
	return f.reply, nil
}

// always errors every call.
type always struct{ calls int }

func (a *always) Complete(context.Context, []promptr.Message) (string, error) {
	a.calls++
	return "", errors.New("down")
}

func TestRetry(t *testing.T) {
	Convey("Given a provider that fails twice then succeeds", t, func() {
		p := &flaky{failN: 2, reply: "ok"}
		r := promptr.Retry(p, 3, 0)

		Convey("When called, Retry keeps trying up to the attempt budget", func() {
			out, err := r.Complete(context.Background(), nil)
			So(err, ShouldBeNil)
			So(out, ShouldEqual, "ok")
			So(p.calls, ShouldEqual, 3)
		})
	})

	Convey("Given a provider that always fails", t, func() {
		p := &always{}
		r := promptr.Retry(p, 2, 0)
		Convey("When called, it exhausts attempts and returns the last error", func() {
			_, err := r.Complete(context.Background(), nil)
			So(err, ShouldNotBeNil)
			So(p.calls, ShouldEqual, 2)
		})
	})
}

func TestFallback(t *testing.T) {
	Convey("Given a failing primary and a healthy backup", t, func() {
		primary := &always{}
		backup := &flaky{failN: 0, reply: "from-backup"}
		fb := promptr.Fallback(primary, backup)

		Convey("When called, it falls over to the backup and returns its reply", func() {
			out, err := fb.Complete(context.Background(), nil)
			So(err, ShouldBeNil)
			So(out, ShouldEqual, "from-backup")
			So(primary.calls, ShouldEqual, 1)
			So(backup.calls, ShouldEqual, 1)
		})
	})

	Convey("Given all providers failing", t, func() {
		fb := promptr.Fallback(&always{}, &always{})
		Convey("When called, it returns the last error", func() {
			_, err := fb.Complete(context.Background(), nil)
			So(err, ShouldNotBeNil)
		})
	})
}

func TestRoundRobin(t *testing.T) {
	Convey("Given three providers under round-robin", t, func() {
		a := &flaky{reply: "a"}
		b := &flaky{reply: "b"}
		c := &flaky{reply: "c"}
		rr := promptr.RoundRobin(a, b, c)

		Convey("When called repeatedly, calls rotate across providers", func() {
			var got []string
			for i := 0; i < 4; i++ {
				out, err := rr.Complete(context.Background(), nil)
				So(err, ShouldBeNil)
				got = append(got, out)
			}
			So(got, ShouldResemble, []string{"a", "b", "c", "a"})
		})
	})
}

func TestRegistry(t *testing.T) {
	Convey("Given a registry with one wired client", t, func() {
		reg := promptr.Registry{"main": &flaky{reply: "hi"}}

		Convey("When Get resolves a known name, it returns that provider", func() {
			out, err := reg.Get("main").Complete(context.Background(), nil)
			So(err, ShouldBeNil)
			So(out, ShouldEqual, "hi")
		})

		Convey("When Get resolves an unknown name, the call fails with a clear error", func() {
			_, err := reg.Get("missing").Complete(context.Background(), nil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, `client "missing"`)
		})
	})
}
