package dsl

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestFormatCanonicalLayout(t *testing.T) {
	Convey("Given a messily-spaced class with attributes", t, func() {
		src := `class   Account {
  email string    @assert("required")
  username   string @assert("min=3")
  plan Plan
}`
		Convey("When formatted", func() {
			out, err := Format(src)
			So(err, ShouldBeNil)

			Convey("Then names and attributed types are column-aligned", func() {
				So(out, ShouldContainSubstring, "  email    string @assert(\"required\")\n")
				So(out, ShouldContainSubstring, "  username string @assert(\"min=3\")\n")
				So(out, ShouldContainSubstring, "  plan     Plan\n")
			})

			Convey("Then the result ends with a single trailing newline", func() {
				So(strings.HasSuffix(out, "}\n"), ShouldBeTrue)
				So(strings.HasSuffix(out, "}\n\n"), ShouldBeFalse)
			})
		})
	})
}

func TestFormatIsIdempotent(t *testing.T) {
	Convey("Given a file exercising every declaration kind", t, func() {
		src := `// header
enum Sev { LOW HIGH }
class T {
  title string
  sev   Sev
}
union U = A | B
client Default {
  provider "fake"
  model "scripted"
}
tool Look(q: string) -> T {
  description "look it up"
}
function F(text: string) -> stream T {
  client Default
  tools [Look]
  prompt #"
    do it {{ text }}
  "#
}`
		Convey("When formatted twice", func() {
			once, err := Format(src)
			So(err, ShouldBeNil)
			twice, err := Format(once)
			So(err, ShouldBeNil)

			Convey("Then the second pass changes nothing", func() {
				So(twice, ShouldEqual, once)
			})

			Convey("Then declaration order is preserved", func() {
				So(indexOf(once, "enum Sev"), ShouldBeLessThan, indexOf(once, "class T"))
				So(indexOf(once, "class T"), ShouldBeLessThan, indexOf(once, "union U"))
				So(indexOf(once, "tool Look"), ShouldBeLessThan, indexOf(once, "function F"))
			})
		})
	})
}

func TestFormatPreservesFuncDescriptionAndSubAgent(t *testing.T) {
	Convey("Given an orchestrator delegating to a sub-agent function", t, func() {
		src := `class R {
  summary string
}
class B {
  topic string
}
client C {
  provider "fake"
  model    "scripted"
}
function ResearchTopic(topic: string) -> R {
  client C
  description "Research a topic."
  prompt #"go {{ topic }}"#
}
function WriteBrief(req: string) -> B {
  client C
  tools [ResearchTopic]
  prompt #"go {{ req }}"#
}`
		Convey("When formatted", func() {
			out, err := Format(src)
			So(err, ShouldBeNil)

			Convey("Then the function description and tool delegation survive", func() {
				So(out, ShouldContainSubstring, `description "Research a topic."`)
				So(out, ShouldContainSubstring, "tools [ResearchTopic]")
			})

			Convey("Then it is idempotent", func() {
				twice, err := Format(out)
				So(err, ShouldBeNil)
				So(twice, ShouldEqual, out)
			})
		})
	})
}

func TestFormatPreservesComments(t *testing.T) {
	Convey("Given leading and trailing comments", t, func() {
		src := `// a class
class T {
  // the title
  title string
  sev   Sev // the severity
}`
		Convey("When formatted", func() {
			out, err := Format(src)
			So(err, ShouldBeNil)

			Convey("Then a leading comment hugs its declaration", func() {
				So(out, ShouldContainSubstring, "// a class\nclass T {")
			})

			Convey("Then a field's leading comment is indented above it", func() {
				So(out, ShouldContainSubstring, "  // the title\n  title string")
			})

			Convey("Then a trailing comment stays on its line", func() {
				So(out, ShouldContainSubstring, "sev   Sev // the severity")
			})
		})
	})
}

func TestFormatDoesNotTreatCommentInRawStringAsComment(t *testing.T) {
	Convey("Given a prompt body containing what looks like a comment", t, func() {
		src := `client D { provider "fake" model "x" }
function F(text: string) -> T {
  client D
  prompt #"
    // this is prompt text, not a comment
    {{ text }}
  "#
}`
		Convey("When formatted", func() {
			out, err := Format(src)
			So(err, ShouldBeNil)

			Convey("Then the // line survives inside the prompt body verbatim", func() {
				So(out, ShouldContainSubstring, "// this is prompt text, not a comment")
				// And it was not hoisted out as a standalone comment above anything.
				So(strings.Count(out, "// this is prompt text"), ShouldEqual, 1)
			})
		})
	})
}

func TestFormatRejectsUnparseable(t *testing.T) {
	Convey("Given source that does not parse", t, func() {
		_, err := Format("class {")
		Convey("Then Format returns an error and no output", func() {
			So(err, ShouldNotBeNil)
		})
	})
}

func indexOf(s, sub string) int { return strings.Index(s, sub) }
