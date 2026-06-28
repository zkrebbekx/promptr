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
