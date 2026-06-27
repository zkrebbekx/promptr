package main

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDiagnose(t *testing.T) {
	Convey("Given a valid document", t, func() {
		text := `class A { x int }
client C { provider "fake" model "m" }
function F(q: string) -> A { client C prompt #"{{ q }}"# }`

		Convey("Then no diagnostics are produced", func() {
			So(Diagnose(text), ShouldBeEmpty)
		})
	})

	Convey("Given a semantic error (unknown return type)", t, func() {
		diags := Diagnose(`function F(q: string) -> Ghost { client C prompt #"{{q}}"# }`)

		Convey("Then an error diagnostic is emitted with severity 1 and source promptr", func() {
			So(len(diags), ShouldBeGreaterThan, 0)
			So(diags[0].Severity, ShouldEqual, 1)
			So(diags[0].Source, ShouldEqual, "promptr")
		})
	})

	Convey("Given a parse error carrying a line prefix", t, func() {
		Convey("Then splitLinePrefix extracts the zero-based line", func() {
			n, msg := splitLinePrefix("line 7: expected '}'")
			So(n, ShouldEqual, 7)
			So(msg, ShouldEqual, "expected '}'")
			diags := Diagnose("class {")
			So(diags, ShouldNotBeEmpty)
		})
	})

	Convey("Given a line-1 diagnostic", t, func() {
		Convey("Then it maps to zero-based row 0 without underflow", func() {
			d := diag(1, "msg")
			So(d.Range.Start.Line, ShouldEqual, 0)
		})
	})
}
