package codegen_test

import (
	"reflect"
	"testing"

	"github.com/zkrebbekx/promptr/codegen"
	"github.com/zkrebbekx/promptr/coerce"
	"github.com/zkrebbekx/promptr/dsl"

	. "github.com/smartystreets/goconvey/convey"
)

func TestTargetTypeSchemaAlignedParse(t *testing.T) {
	Convey("Given a schema whose function returns a class", t, func() {
		f, err := dsl.Parse(`
class Profile {
  user_name   string
  email_addr  string
  is_active   bool
  login_count int
}
client Default { provider "fake" model "scripted" }
function ExtractProfile(text: string) -> Profile {
  client Default
  prompt #"{{ text }} {{ ctx.output_schema }}"#
}`)
		So(err, ShouldBeNil)

		Convey("When a target type is built and messy model output is coerced into it", func() {
			rt, ok := codegen.TargetType(f)
			So(ok, ShouldBeTrue)

			// Keys are scrambled by case and separator vs the snake_case schema,
			// and scalars are loose — exactly what a model emits.
			messy := `{
  "UserName": "ada",
  "Email-Addr": "ada@example.com",
  "isActive": true,
  "LoginCount": "42"
}`
			v, cerr := coerce.Value(messy, rt)
			So(cerr, ShouldBeNil)

			Convey("Then each differently-cased key snaps onto the declared field", func() {
				rv := structFields(v)
				So(rv["UserName"], ShouldEqual, "ada")
				So(rv["EmailAddr"], ShouldEqual, "ada@example.com")
				So(rv["IsActive"], ShouldEqual, true)
				So(rv["LoginCount"], ShouldEqual, 42)
			})
		})
	})

	Convey("Given a schema with classes but no function (a formatter-style snippet)", t, func() {
		f, err := dsl.Parse(`
enum Sev { LOW HIGH }
class Account { email string username string plan Sev }`)
		So(err, ShouldBeNil)

		Convey("Then the first declared class is the target", func() {
			rt, ok := codegen.TargetType(f)
			So(ok, ShouldBeTrue)
			So(rt.NumField(), ShouldEqual, 3)
		})
	})

	Convey("Given a schema that declares no class at all", t, func() {
		f, err := dsl.Parse(`client Default { provider "fake" model "scripted" }`)
		So(err, ShouldBeNil)

		Convey("Then no target type is produced (caller falls back to generic parse)", func() {
			_, ok := codegen.TargetType(f)
			So(ok, ShouldBeFalse)
		})
	})
}

func TestUnionTypesSchemaAlignedParse(t *testing.T) {
	Convey("Given a schema whose function returns a named union", t, func() {
		f, err := dsl.Parse(`
class Search   { query string topk int @alias("max_results") }
class Escalate { reason string metadata map<string, string> }
union Action = Search | Escalate
client Default { provider "fake" model "scripted" }
function Route(message: string) -> Action {
  client Default
  prompt #"{{ message }} {{ ctx.output_schema }}"#
}`)
		So(err, ShouldBeNil)

		Convey("When the union variant types are built", func() {
			uts, ok := codegen.UnionTypes(f)
			So(ok, ShouldBeTrue)
			So(len(uts), ShouldEqual, 2)
			u := coerce.NewUnionTypes(uts...)

			Convey("Then an Escalate-shaped reply resolves to Escalate, not the first variant", func() {
				v, rt, cerr := u.Resolve(`Escalating. {"reason":"needs a human","metadata":{"sev":"high"}}`)
				So(cerr, ShouldBeNil)
				So(rt.Name(), ShouldEqual, "") // dynamic struct, identified by fields
				fields := structFields(v)
				So(fields, ShouldContainKey, "Reason")
				So(fields["Reason"], ShouldEqual, "needs a human")
				So(fields, ShouldNotContainKey, "Query")
			})

			Convey("Then a Search-shaped reply still resolves to Search (with alias + int coercion)", func() {
				v, _, cerr := u.Resolve(`I'll search. {"query":"headphones","max_results":"5"}`)
				So(cerr, ShouldBeNil)
				fields := structFields(v)
				So(fields, ShouldContainKey, "Query")
				So(fields["Query"], ShouldEqual, "headphones")
				So(fields["Topk"], ShouldEqual, 5)
			})
		})
	})

	Convey("Given a schema whose function returns a plain class (no union)", t, func() {
		f, err := dsl.Parse(`
class Profile { name string }
client Default { provider "fake" model "scripted" }
function Extract(text: string) -> Profile { client Default prompt #"{{ text }}"# }`)
		So(err, ShouldBeNil)

		Convey("Then UnionTypes reports no union, so the caller uses TargetType", func() {
			_, ok := codegen.UnionTypes(f)
			So(ok, ShouldBeFalse)
		})
	})
}

// structFields reflects the coerced struct value into a name->value map so the
// test can assert per field without importing the dynamically-built type.
func structFields(v any) map[string]any {
	out := map[string]any{}
	rv := reflect.ValueOf(v)
	for i := 0; i < rv.NumField(); i++ {
		out[rv.Type().Field(i).Name] = rv.Field(i).Interface()
	}
	return out
}
