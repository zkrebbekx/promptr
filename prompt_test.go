package promptr_test

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/zkrebbekx/promptr"
)

func render(t *testing.T, tmpl string, ctx map[string]any) string {
	t.Helper()
	out, err := promptr.Render(tmpl, ctx)
	So(err, ShouldBeNil)
	return out
}

func TestRenderVars(t *testing.T) {
	Convey("Given a template with simple and dotted variable holes", t, func() {
		tmpl := "Hi {{ name }}, you are {{ user.role }}."
		ctx := map[string]any{
			"name": "Ada",
			"user": map[string]any{"role": "admin"},
		}

		Convey("Then both holes are interpolated, dotted paths included", func() {
			So(render(t, tmpl, ctx), ShouldEqual, "Hi Ada, you are admin.")
		})

		Convey("Then an unknown name renders as empty, never panics", func() {
			So(render(t, "x={{ missing }}!", map[string]any{}), ShouldEqual, "x=!")
		})
	})
}

func TestRenderStructFields(t *testing.T) {
	Convey("Given a dotted path into a struct value", t, func() {
		type U struct{ Name string }
		ctx := map[string]any{"u": U{Name: "Grace"}}

		Convey("Then struct fields resolve case-insensitively", func() {
			So(render(t, "{{ u.name }}", ctx), ShouldEqual, "Grace")
		})
	})
}

func TestRenderConditionals(t *testing.T) {
	Convey("Given if/else over truthiness and comparison", t, func() {
		tmpl := "{{ if admin }}root{{ else }}user{{ end }}"

		Convey("Then a truthy value takes the then-branch", func() {
			So(render(t, tmpl, map[string]any{"admin": true}), ShouldEqual, "root")
		})
		Convey("Then a falsey value takes the else-branch", func() {
			So(render(t, tmpl, map[string]any{"admin": false}), ShouldEqual, "user")
		})
		Convey("Then an empty string is falsey", func() {
			So(render(t, tmpl, map[string]any{"admin": ""}), ShouldEqual, "user")
		})
		Convey("Then 'not' negates the condition", func() {
			out := render(t, "{{ if not done }}pending{{ end }}", map[string]any{"done": false})
			So(out, ShouldEqual, "pending")
		})
		Convey("Then equality comparison against a literal works", func() {
			tmpl := `{{ if role == "admin" }}yes{{ else }}no{{ end }}`
			So(render(t, tmpl, map[string]any{"role": "admin"}), ShouldEqual, "yes")
			So(render(t, tmpl, map[string]any{"role": "guest"}), ShouldEqual, "no")
		})
	})
}

func TestRenderLoops(t *testing.T) {
	Convey("Given a for-loop over a slice", t, func() {
		tmpl := "{{ for t in tags }}[{{ t }}]{{ end }}"

		Convey("Then the body renders once per element with the loop var bound", func() {
			ctx := map[string]any{"tags": []string{"a", "b", "c"}}
			So(render(t, tmpl, ctx), ShouldEqual, "[a][b][c]")
		})
		Convey("Then a missing or non-slice value yields nothing", func() {
			So(render(t, tmpl, map[string]any{}), ShouldEqual, "")
			So(render(t, tmpl, map[string]any{"tags": 5}), ShouldEqual, "")
		})
		Convey("Then loops can iterate structs and read fields", func() {
			type Item struct{ Label string }
			tmpl := "{{ for it in items }}{{ it.label }};{{ end }}"
			ctx := map[string]any{"items": []Item{{Label: "x"}, {Label: "y"}}}
			So(render(t, tmpl, ctx), ShouldEqual, "x;y;")
		})
	})
}

func TestRenderOutputSchemaHole(t *testing.T) {
	Convey("Given the compiler-injected ctx.output_schema entry", t, func() {
		ctx := map[string]any{"ctx": map[string]any{"output_schema": "SHAPE"}}

		Convey("Then {{ ctx.output_schema }} expands to the baked schema", func() {
			So(render(t, "Reply with {{ ctx.output_schema }}", ctx), ShouldEqual, "Reply with SHAPE")
		})
	})
}

func TestRenderBrokenTemplateErrors(t *testing.T) {
	Convey("Given a template with an unterminated block", t, func() {
		_, err := promptr.Render("{{ if x }}oops", map[string]any{"x": true})

		Convey("Then Render returns a TemplateError rather than panicking", func() {
			So(err, ShouldNotBeNil)
		})
	})
}

func TestRenderStrayKeywordsAreLiteral(t *testing.T) {
	Convey("Given prose that happens to contain end/else words in holes", t, func() {
		Convey("Then a stray {{ end }} with no opener is kept as literal text", func() {
			So(render(t, "a{{ end }}b", map[string]any{}), ShouldEqual, "a{{end}}b")
		})
	})
}
