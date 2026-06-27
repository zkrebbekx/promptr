package codegen

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/zkrebbekx/promptr/dsl"
)

const testSchema = `enum Severity { LOW HIGH CRITICAL }

class Ticket {
  title    string
  severity Severity
  open     bool
  votes    int
}

client Default {
  provider "fake"
  model    "scripted"
}

function ExtractTicket(text: string) -> Ticket {
  client Default
  prompt #"Extract {{ text }} {{ ctx.output_schema }}"#
}

test outage {
  function ExtractTicket
  args { text "server down!" }
  expect {
    title    "Server is down"
    severity CRITICAL
    open     true
    votes    3
  }
}`

func TestGenerateTests(t *testing.T) {
	Convey("Given a schema with a test block carrying typed expectations", t, func() {
		f, err := dsl.Parse(testSchema)
		So(err, ShouldBeNil)

		out, gerr := GenerateTests("livetest", f)
		So(gerr, ShouldBeNil)
		src := string(out)

		Convey("Then it emits a runnable Go test function", func() {
			So(src, ShouldContainSubstring, "func TestOutage(t *testing.T) {")
			So(src, ShouldContainSubstring, "package livetest")
			So(src, ShouldContainSubstring, "\"testing\"")
		})

		Convey("Then it injects an overridable PromptrProvider that Skips when nil", func() {
			So(src, ShouldContainSubstring, "var PromptrProvider promptr.Provider")
			So(src, ShouldContainSubstring, "if PromptrProvider == nil {")
			So(src, ShouldContainSubstring, "t.Skip(")
		})

		Convey("Then it calls the target function with the arg literal", func() {
			So(src, ShouldContainSubstring, `ExtractTicket(context.Background(), PromptrProvider, "server down!")`)
		})

		Convey("Then it asserts each expected field with a typed literal", func() {
			So(src, ShouldContainSubstring, `if got.Title != "Server is down" {`)
			So(src, ShouldContainSubstring, "if got.Severity != SeverityCRITICAL {")
			So(src, ShouldContainSubstring, "if got.Open != true {")
			So(src, ShouldContainSubstring, "if got.Votes != 3 {")
		})
	})
}

func TestGenerateTestsNoTestable(t *testing.T) {
	Convey("Given a schema whose only test targets a streaming function", t, func() {
		f, err := dsl.Parse(`class S { headline string }
client D { provider "fake" model "x" }
function Summarize(text: string) -> stream S {
  client D
  prompt #"{{ text }}"#
}
test t1 { function Summarize args { text "hi" } }`)
		So(err, ShouldBeNil)

		Convey("When generating tests", func() {
			out, gerr := GenerateTests("app", f)
			Convey("Then nothing is emitted (streaming funcs aren't directly testable)", func() {
				So(gerr, ShouldBeNil)
				So(out, ShouldBeNil)
			})
		})
	})
}

func TestGenerateTestsSmokeOnly(t *testing.T) {
	Convey("Given a test block with args but no expect", t, func() {
		f, err := dsl.Parse(`class Ticket { title string }
client D { provider "fake" model "x" }
function ExtractTicket(text: string) -> Ticket {
  client D
  prompt #"{{ text }}"#
}
test smoke { function ExtractTicket args { text "hi" } }`)
		So(err, ShouldBeNil)

		out, gerr := GenerateTests("app", f)
		So(gerr, ShouldBeNil)
		src := string(out)

		Convey("Then it emits a call that only asserts no error", func() {
			So(src, ShouldContainSubstring, "func TestSmoke(t *testing.T) {")
			So(src, ShouldContainSubstring, "_ = got")
			So(src, ShouldNotContainSubstring, "got.Title !=")
		})
	})
}
