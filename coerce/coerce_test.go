package coerce_test

import (
	"strings"
	"testing"

	"github.com/zkrebbekx/promptr/coerce"

	. "github.com/smartystreets/goconvey/convey"
)

// Severity is a string-backed enum opting into fuzzy matching via coerce.Enum.
type Severity string

func (Severity) CoerceMembers() []string {
	return []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"}
}

type Ticket struct {
	Title    string   `json:"title"`
	Severity Severity `json:"severity"`
	Tags     []string `json:"tags"`
	DueDays  int      `json:"due_days"`
	Resolved bool     `json:"resolved"`
}

func TestExtractionFromMessyWrappers(t *testing.T) {
	Convey("Given model output buried in prose and a Markdown fence", t, func() {
		raw := "Sure! Here's the ticket you asked for:\n\n```json\n" +
			`{"title": "DB down", "severity": "HIGH", "due_days": 2}` +
			"\n```\nLet me know if you need anything else."

		Convey("When coerced into a Ticket", func() {
			got, err := coerce.Into[Ticket](raw)

			Convey("Then the fenced payload is found and typed, prose ignored", func() {
				So(err, ShouldBeNil)
				So(got.Title, ShouldEqual, "DB down")
				So(got.Severity, ShouldEqual, Severity("HIGH"))
				So(got.DueDays, ShouldEqual, 2)
			})
		})
	})

	Convey("Given a bare object with a leading sentence and no fence", t, func() {
		raw := `The answer is {"title": "x", "due_days": 5} okay?`

		Convey("When coerced", func() {
			got, err := coerce.Into[Ticket](raw)

			Convey("Then the object is located and trailing prose dropped", func() {
				So(err, ShouldBeNil)
				So(got.Title, ShouldEqual, "x")
				So(got.DueDays, ShouldEqual, 5)
			})
		})
	})
}

func TestToleratesLooseJSONSyntax(t *testing.T) {
	Convey("Given JSON with trailing commas, comments, single quotes and unquoted keys", t, func() {
		raw := `{
			title: 'DB down',          // unquoted key + single quotes
			tags: ['db', 'urgent',],   /* trailing comma */
			due_days: 3,
		}`

		Convey("When coerced into a Ticket", func() {
			got, err := coerce.Into[Ticket](raw)

			Convey("Then every lenient construct parses", func() {
				So(err, ShouldBeNil)
				So(got.Title, ShouldEqual, "DB down")
				So(got.Tags, ShouldResemble, []string{"db", "urgent"})
				So(got.DueDays, ShouldEqual, 3)
			})
		})
	})
}

func TestScalarCoercion(t *testing.T) {
	Convey("Given scalars in the wrong lexical form", t, func() {
		raw := `{"title": 12345, "due_days": "7", "resolved": "yes"}`

		Convey("When coerced", func() {
			got, err := coerce.Into[Ticket](raw)

			Convey("Then number->string, string->int, and yes->bool all coerce", func() {
				So(err, ShouldBeNil)
				So(got.Title, ShouldEqual, "12345")
				So(got.DueDays, ShouldEqual, 7)
				So(got.Resolved, ShouldBeTrue)
			})
		})
	})

	Convey("Given a decorated numeric string", t, func() {
		type Money struct {
			Amount float64 `json:"amount"`
		}
		raw := `{"amount": "$1,200.50"}`

		Convey("When coerced", func() {
			got, err := coerce.Into[Money](raw)

			Convey("Then currency and thousands separators are stripped", func() {
				So(err, ShouldBeNil)
				So(got.Amount, ShouldEqual, 1200.50)
			})
		})
	})
}

func TestEnumFuzzyMatch(t *testing.T) {
	Convey("Given an enum value the model phrased loosely", t, func() {
		cases := map[string]Severity{
			`{"severity": "high"}`:          "HIGH",
			`{"severity": "HIGH priority"}`: "HIGH",
			`{"severity": "critical!!"}`:    "CRITICAL",
			`{"severity": "med"}`:           "MEDIUM",
		}

		Convey("When each is coerced", func() {
			Convey("Then it resolves to the canonical member", func() {
				for raw, want := range cases {
					got, err := coerce.Into[Ticket](raw)
					So(err, ShouldBeNil)
					So(got.Severity, ShouldEqual, want)
				}
			})
		})
	})
}

func TestKeyNameTolerance(t *testing.T) {
	Convey("Given object keys in a different case/separator style than the struct", t, func() {
		raw := `{"Title": "x", "DUE-DAYS": 9}`

		Convey("When coerced", func() {
			got, err := coerce.Into[Ticket](raw)

			Convey("Then snake/camel/case differences still bind", func() {
				So(err, ShouldBeNil)
				So(got.Title, ShouldEqual, "x")
				So(got.DueDays, ShouldEqual, 9)
			})
		})
	})
}

func TestNestedAndOptional(t *testing.T) {
	type Author struct {
		Name string `json:"name"`
	}
	type Post struct {
		Title  string  `json:"title"`
		Author Author  `json:"author"`
		Editor *Author `json:"editor"`
	}

	Convey("Given nested objects and a pointer field", t, func() {
		raw := `{"title": "t", "author": {"name": "ada"}, "editor": {"name": "grace"}}`

		Convey("When coerced", func() {
			got, err := coerce.Into[Post](raw)

			Convey("Then nested structs and *T optionals populate", func() {
				So(err, ShouldBeNil)
				So(got.Author.Name, ShouldEqual, "ada")
				So(got.Editor, ShouldNotBeNil)
				So(got.Editor.Name, ShouldEqual, "grace")
			})
		})
	})
}

func TestSingleValueIntoSlice(t *testing.T) {
	Convey("Given a single value where a list was expected", t, func() {
		raw := `{"tags": "solo"}`

		Convey("When coerced", func() {
			got, err := coerce.Into[Ticket](raw)

			Convey("Then it is wrapped into a one-element slice", func() {
				So(err, ShouldBeNil)
				So(got.Tags, ShouldResemble, []string{"solo"})
			})
		})
	})
}

func TestTruncatedRecovery(t *testing.T) {
	Convey("Given output cut off mid-object", t, func() {
		raw := `{"title": "partial answer", "tags": ["a", "b"`

		Convey("When coerced", func() {
			got, err := coerce.Into[Ticket](raw)

			Convey("Then whatever parsed is still recovered", func() {
				So(err, ShouldBeNil)
				So(got.Title, ShouldEqual, "partial answer")
				So(got.Tags, ShouldResemble, []string{"a", "b"})
			})
		})
	})
}

func TestIntoMap(t *testing.T) {
	Convey("Given an object coerced into a map", t, func() {
		raw := "```\n{\"a\": 1, \"b\": 2}\n```"

		Convey("When coerced into map[string]int", func() {
			got, err := coerce.Into[map[string]int](raw)

			Convey("Then keys and coerced values land", func() {
				So(err, ShouldBeNil)
				So(got["a"], ShouldEqual, 1)
				So(got["b"], ShouldEqual, 2)
			})
		})
	})
}

func TestStreamingPartials(t *testing.T) {
	Convey("Given an object delivered in chunks", t, func() {
		chunks := make(chan string)
		go func() {
			for _, c := range []string{`{"title": "stre`, `aming", "due_`, `days": 4}`} {
				chunks <- c
			}
			close(chunks)
		}()

		Convey("When streamed", func() {
			var last coerce.Partial[Ticket]
			var sawIncomplete bool
			for p := range coerce.Stream[Ticket](chunks) {
				if !p.Complete {
					sawIncomplete = true
				}
				last = p
			}

			Convey("Then partials are emitted and the final snapshot completes", func() {
				So(sawIncomplete, ShouldBeTrue)
				So(last.Complete, ShouldBeTrue)
				So(last.Value.Title, ShouldEqual, "streaming")
				So(last.Value.DueDays, ShouldEqual, 4)
			})
		})
	})
}

func TestBareScalarNoObject(t *testing.T) {
	Convey("Given a model that answered with a bare value", t, func() {
		Convey("When coerced into the scalar type", func() {
			n, err := coerce.Into[int](`The count is 42.`)
			b, err2 := coerce.Into[bool](`yes`)

			Convey("Then the embedded scalar is extracted", func() {
				So(err, ShouldBeNil)
				So(n, ShouldEqual, 42)
				So(err2, ShouldBeNil)
				So(b, ShouldBeTrue)
			})
		})
	})
}

func TestIntoAny(t *testing.T) {
	Convey("Given output coerced into a generic any", t, func() {
		raw := `{"nested": {"k": [1, 2]}, "flag": true}`

		Convey("When coerced into map[string]any", func() {
			got, err := coerce.Into[map[string]any](raw)

			Convey("Then a generic tree is produced", func() {
				So(err, ShouldBeNil)
				So(got["flag"], ShouldEqual, true)
				nested, ok := got["nested"].(map[string]any)
				So(ok, ShouldBeTrue)
				So(nested["k"], ShouldResemble, []any{float64(1), float64(2)})
			})
		})
	})
}

// Ensures the bare-scalar path doesn't choke on a value that is just prose.
func TestNonNumericProseIsEmptyNotPanic(t *testing.T) {
	Convey("Given prose with no parseable structure for an int target", t, func() {
		Convey("When coerced", func() {
			var f func()
			f = func() { _, _ = coerce.Into[int](strings.Repeat("words ", 5)) }

			Convey("Then it returns a zero value without panicking", func() {
				So(f, ShouldNotPanic)
			})
		})
	})
}
