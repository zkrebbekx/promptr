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
			So(msgs, ShouldContain, `function "F" uses unknown tool or sub-agent "Missing"`)
			So(msgs, ShouldContain, `tool Bad refers to unknown type "Ghost"`)
		})
	})

	Convey("Given a function delegating to another function as a sub-agent", t, func() {
		f, err := Parse(`
class Research { summary string }
class Brief { topic string }
client C { provider "fake" model "x" }
function ResearchTopic(topic: string) -> Research {
  client C
  description "research it"
  prompt #"{{ topic }}"#
}
function WriteBrief(req: string) -> Brief {
  client C
  tools [ResearchTopic]
  prompt #"{{ req }}"#
}`)
		So(err, ShouldBeNil)
		Convey("Then Validate reports no diagnostics", func() {
			So(Validate(f), ShouldBeEmpty)
		})
	})

	Convey("Given a function delegating to itself", t, func() {
		f, _ := Parse(`
class B { x string }
client C { provider "fake" model "x" }
function Loop(req: string) -> B {
  client C
  tools [Loop]
  prompt #"{{ req }}"#
}`)
		Convey("Then self-delegation is flagged", func() {
			So(diagMsgs(f), ShouldContain, `function "Loop" cannot delegate to itself as a sub-agent`)
		})
	})

	Convey("Given a sub-agent that streams", t, func() {
		f, _ := Parse(`
class R { x string }
class B { y string }
client C { provider "fake" model "x" }
function Sub(t: string) -> stream R { client C prompt #"{{ t }}"# }
function Top(req: string) -> B { client C tools [Sub] prompt #"{{ req }}"# }`)
		Convey("Then the streaming sub-agent is rejected", func() {
			So(diagMsgs(f), ShouldContain, `function "Top" cannot use streaming function "Sub" as a sub-agent`)
		})
	})

	Convey("Given a sub-agent that itself needs tool handlers", t, func() {
		f, _ := Parse(`
class W { city string }
class R { x string }
class B { y string }
client C { provider "fake" model "x" }
tool GetW(city: string) -> W { description "d" }
function Sub(t: string) -> R { client C tools [GetW] prompt #"{{ t }}"# }
function Top(req: string) -> B { client C tools [Sub] prompt #"{{ req }}"# }`)
		Convey("Then the handler-needing sub-agent is rejected", func() {
			So(diagMsgs(f), ShouldContain, `function "Top" cannot use sub-agent "Sub": it requires tool handlers (calls tool "GetW")`)
		})
	})

	Convey("Given a sub-agent taking a binary part parameter", t, func() {
		f, _ := Parse(`
class R { x string }
class B { y string }
client C { provider "fake" model "x" }
function Sub(photo: image) -> R { client C prompt #"go {{ ctx.output_schema }}"# }
function Top(req: string) -> B { client C tools [Sub] prompt #"{{ req }}"# }`)
		Convey("Then the part-param sub-agent is rejected", func() {
			So(diagMsgs(f), ShouldContain, `function "Top" cannot use sub-agent "Sub": it takes a binary part parameter`)
		})
	})

	Convey("Given a delegation cycle between two functions", t, func() {
		f, _ := Parse(`
class A { x string }
class B { y string }
client C { provider "fake" model "x" }
function One(req: string) -> A { client C tools [Two] prompt #"{{ req }}"# }
function Two(req: string) -> B { client C tools [One] prompt #"{{ req }}"# }`)
		Convey("Then the cycle is flagged", func() {
			msgs := diagMsgs(f)
			hasCycle := false
			for _, m := range msgs {
				if m == `sub-agent delegation cycle through function "One"` || m == `sub-agent delegation cycle through function "Two"` {
					hasCycle = true
				}
			}
			So(hasCycle, ShouldBeTrue)
		})
	})

	Convey("Given a test whose expect matches the returned class", t, func() {
		f, err := Parse(`enum Sev { LOW HIGH }
class Ticket { title string sev Sev open bool votes int }
client C { provider "fake" model "x" }
function ExtractTicket(text: string) -> Ticket { client C prompt #"{{ text }}"# }
test ok {
  function ExtractTicket
  args { text "hi" }
  expect { title "X" sev HIGH open true votes 2 }
}`)
		So(err, ShouldBeNil)
		Convey("Then Validate reports no diagnostics", func() {
			So(Validate(f), ShouldBeEmpty)
		})
	})

	Convey("Given an expect with an unknown field, a type mismatch and a bad enum member", t, func() {
		f, _ := Parse(`enum Sev { LOW HIGH }
class Ticket { title string sev Sev votes int }
client C { provider "fake" model "x" }
function ExtractTicket(text: string) -> Ticket { client C prompt #"{{ text }}"# }
test bad {
  function ExtractTicket
  args { text "hi" }
  expect { ghost "x" votes "three" sev MEDIUM }
}`)
		msgs := diagMsgs(f)
		Convey("Then each problem is flagged", func() {
			So(msgs, ShouldContain, `test "bad" expects field "ghost", not a field of "Ticket"`)
			So(msgs, ShouldContain, `test "bad" field "votes" expects a int, got "three"`)
			So(msgs, ShouldContain, `test "bad" field "sev" expects a Sev member, got "MEDIUM"`)
		})
	})

	Convey("Given an expect on a function that does not return a class", t, func() {
		f, _ := Parse(`client C { provider "fake" model "x" }
function Greet(name: string) -> string { client C prompt #"{{ name }}"# }
test t { function Greet args { name "Zac" } expect { x "y" } }`)
		Convey("Then it is flagged as not assertable", func() {
			So(diagMsgs(f), ShouldContain, `test "t" has expect, but "Greet" does not return a class`)
		})
	})

	Convey("Given a test targeting a streaming function", t, func() {
		f, _ := Parse(`class S { headline string }
client C { provider "fake" model "x" }
function Sum(text: string) -> stream S { client C prompt #"{{ text }}"# }
test t { function Sum args { text "hi" } }`)
		Convey("Then the streaming target is rejected", func() {
			So(diagMsgs(f), ShouldContain, `test "t" cannot target streaming function "Sum"`)
		})
	})
}
