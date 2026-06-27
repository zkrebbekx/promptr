package dsl

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func diagMsgs(f *File) []string {
	var out []string
	for _, d := range Validate(f) {
		out = append(out, d.Msg)
	}
	return out
}

func TestValidate(t *testing.T) {
	Convey("Given a well-formed file", t, func() {
		f, err := Parse(`
class Search { q string }
class Escalate { reason string }
union Action = Search | Escalate
client C { provider "fake" model "x" }
function Route(msg: string) -> Action {
  client C
  prompt #"{{ msg }} {{ ctx.output_schema }}"#
}
test Routes { function Route args { msg "help" } }`)
		So(err, ShouldBeNil)

		Convey("Then Validate reports no diagnostics", func() {
			So(Validate(f), ShouldBeEmpty)
		})
	})

	Convey("Given a union over an undeclared class", t, func() {
		f, _ := Parse("class A { x int }\nunion U = A | Ghost")
		Convey("Then the missing variant is flagged", func() {
			So(diagMsgs(f), ShouldContain, `union "U" variant "Ghost" is not a declared class`)
		})
	})

	Convey("Given a function using an unknown client and unknown return type", t, func() {
		f, _ := Parse(`function F(x: string) -> Mystery { client Nope prompt #"{{x}}"# }`)
		msgs := diagMsgs(f)
		Convey("Then both are flagged", func() {
			So(msgs, ShouldContain, `function "F" uses unknown client "Nope"`)
			So(msgs, ShouldContain, `F refers to unknown type "Mystery"`)
		})
	})

	Convey("Given a test with a bad arg and a missing required arg", t, func() {
		f, _ := Parse(`
client C { provider "fake" model "x" }
function Greet(name: string, lang: string) -> string { client C prompt #"hi"# }
test T { function Greet args { name "Zac" wrong "x" } }`)
		msgs := diagMsgs(f)
		Convey("Then the extra and missing args are flagged", func() {
			So(msgs, ShouldContain, `test "T" sets arg "wrong", not a parameter of "Greet"`)
			So(msgs, ShouldContain, `test "T" is missing arg "lang" required by "Greet"`)
		})
	})

	Convey("Given a duplicate declaration", t, func() {
		f, _ := Parse("class Dup { x int }\nenum Dup { A B }")
		Convey("Then it is flagged", func() {
			So(diagMsgs(f), ShouldContain, `duplicate declaration "Dup"`)
		})
	})

	Convey("Given a well-formed tool-using function", t, func() {
		f, err := Parse(`
class W { city string }
client C { provider "fake" model "x" }
tool GetW(city: string) -> W { description "look up" }
function Plan(goal: string) -> string {
  client C
  tools [GetW]
  prompt #"{{ goal }}"#
}`)
		So(err, ShouldBeNil)
		Convey("Then Validate reports no diagnostics", func() {
			So(Validate(f), ShouldBeEmpty)
		})
	})

	Convey("Given a function referencing an unknown tool and a tool with a bad type", t, func() {
		f, _ := Parse(`
client C { provider "fake" model "x" }
tool Bad(x: Ghost) -> string { description "d" }
function F(g: string) -> string {
  client C
  tools [Missing]
  prompt #"{{ g }}"#
}`)
		msgs := diagMsgs(f)
		Convey("Then the unknown tool and unresolved tool type are flagged", func() {
			So(msgs, ShouldContain, `function "F" uses unknown tool "Missing"`)
			So(msgs, ShouldContain, `tool Bad refers to unknown type "Ghost"`)
		})
	})
}
