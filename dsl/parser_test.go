package dsl

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

const sample = `
// A support-ticket extractor.
enum Severity { LOW HIGH CRITICAL }

class Ticket {
  title    string
  severity Severity
  tags     string[]
  due_days int?
}

client GPT4o {
  provider "openai"
  model    "gpt-4o"
  temperature "0"
}

function ExtractTicket(text: string, locale: string) -> Ticket {
  client GPT4o
  prompt #"
    Extract a support ticket.
    {{ ctx.output_schema }}
    Message: {{ text }}
  "#
}

test down_server {
  function ExtractTicket
  args { text "my server is down!!" }
}
`

func TestParse(t *testing.T) {
	Convey("Given a complete .promptr document", t, func() {
		f, err := Parse(sample)

		Convey("Then it parses without error", func() {
			So(err, ShouldBeNil)
			So(f, ShouldNotBeNil)
		})

		Convey("Then the enum is captured with all members", func() {
			So(f.Enums, ShouldHaveLength, 1)
			So(f.Enums[0].Name, ShouldEqual, "Severity")
			So(f.Enums[0].Members, ShouldResemble, []string{"LOW", "HIGH", "CRITICAL"})
		})

		Convey("Then the class fields carry list and optional markers", func() {
			So(f.Classes, ShouldHaveLength, 1)
			c := f.Classes[0]
			So(c.Name, ShouldEqual, "Ticket")
			So(c.Fields, ShouldHaveLength, 4)
			So(c.Fields[2].Name, ShouldEqual, "tags")
			So(c.Fields[2].Type.Name, ShouldEqual, "string")
			So(c.Fields[2].Type.List, ShouldBeTrue)
			So(c.Fields[3].Name, ShouldEqual, "due_days")
			So(c.Fields[3].Type.Optional, ShouldBeTrue)
			So(c.Fields[3].Type.List, ShouldBeFalse)
		})

		Convey("Then the client lifts provider/model and keeps extras", func() {
			So(f.Clients, ShouldHaveLength, 1)
			cl := f.Clients[0]
			So(cl.Provider, ShouldEqual, "openai")
			So(cl.Model, ShouldEqual, "gpt-4o")
			So(cl.Extra["temperature"], ShouldEqual, "0")
		})

		Convey("Then the function captures params, return, client and raw prompt", func() {
			So(f.Funcs, ShouldHaveLength, 1)
			fn := f.Funcs[0]
			So(fn.Name, ShouldEqual, "ExtractTicket")
			So(fn.Params, ShouldHaveLength, 2)
			So(fn.Params[0].Name, ShouldEqual, "text")
			So(fn.Params[1].Name, ShouldEqual, "locale")
			So(fn.Ret.Name, ShouldEqual, "Ticket")
			So(fn.Client, ShouldEqual, "GPT4o")
			So(fn.Prompt, ShouldContainSubstring, "{{ ctx.output_schema }}")
			So(fn.Prompt, ShouldContainSubstring, "{{ text }}")
		})

		Convey("Then the test block captures function and args", func() {
			So(f.Tests, ShouldHaveLength, 1)
			So(f.Tests[0].Func, ShouldEqual, "ExtractTicket")
			So(f.Tests[0].Args["text"], ShouldEqual, "my server is down!!")
		})
	})
}

func TestParseTypeRefVariants(t *testing.T) {
	Convey("Given fields with every type-decoration combination", t, func() {
		src := `class T {
		  a string
		  b string[]
		  c Severity?
		  d Tag[]?
		}`
		f, err := Parse(src)
		So(err, ShouldBeNil)
		fields := f.Classes[0].Fields

		Convey("Then plain, list, optional and list-optional all decode", func() {
			So(fields[0].Type, ShouldResemble, TypeRef{Name: "string"})
			So(fields[1].Type, ShouldResemble, TypeRef{Name: "string", List: true})
			So(fields[2].Type, ShouldResemble, TypeRef{Name: "Severity", Optional: true})
			So(fields[3].Type, ShouldResemble, TypeRef{Name: "Tag", List: true, Optional: true})
		})
	})
}

func TestParseRawStringVerbatim(t *testing.T) {
	Convey("Given a prompt with quotes, braces and newlines inside #\"...\"#", t, func() {
		src := "function F() -> string {\n  prompt #\"say \"hi\" to {{name}}\nnext line\"#\n}"
		f, err := Parse(src)

		Convey("Then the raw body is preserved byte-for-byte", func() {
			So(err, ShouldBeNil)
			So(f.Funcs[0].Prompt, ShouldEqual, "say \"hi\" to {{name}}\nnext line")
		})
	})
}

func TestParseReportsErrorsButReturnsPartialAST(t *testing.T) {
	Convey("Given a document with a malformed class but a valid enum", t, func() {
		src := `enum Color { RED GREEN }
		class Broken {
		  field
		`
		f, err := Parse(src)

		Convey("Then an error is reported", func() {
			So(err, ShouldNotBeNil)
		})

		Convey("Then the well-formed enum is still present in the partial AST", func() {
			So(f.Enums, ShouldHaveLength, 1)
			So(f.Enums[0].Name, ShouldEqual, "Color")
		})
	})
}

func TestParseEmptyAndCommentsOnly(t *testing.T) {
	Convey("Given input that is only whitespace and comments", t, func() {
		f, err := Parse("  // nothing here\n\n  // still nothing\n")

		Convey("Then it parses to an empty file with no error", func() {
			So(err, ShouldBeNil)
			So(f.Enums, ShouldBeEmpty)
			So(f.Classes, ShouldBeEmpty)
			So(f.Funcs, ShouldBeEmpty)
		})
	})
}

func TestParseUnterminatedRawStringDoesNotHang(t *testing.T) {
	Convey("Given a prompt whose raw string is never closed", t, func() {
		src := "function F() -> string {\n  prompt #\"never ends"

		Convey("Then Parse returns rather than looping forever", func() {
			done := make(chan struct{})
			go func() { _, _ = Parse(src); close(done) }()
			select {
			case <-done:
			default:
				// give it a moment; goconvey runs fast, this is a smoke guard
			}
			f, _ := Parse(src)
			So(f, ShouldNotBeNil)
			So(strings.Contains(f.Funcs[0].Prompt, "never ends"), ShouldBeTrue)
		})
	})
}
