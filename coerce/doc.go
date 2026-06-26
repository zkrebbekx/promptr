// Package coerce is a schema-aligned parser: it turns the loose, near-JSON
// text that language models actually emit into typed Go values.
//
// Strict JSON parsing (encoding/json) rejects the everyday reality of model
// output — a prose preamble ("Sure, here's the JSON:"), a Markdown code
// fence, a trailing comma, a single-quoted string, an unquoted key, or a
// response that was cut off mid-object. coerce tolerates all of these,
// recovers as much structure as it can, and then coerces the result into the
// target Go type you ask for, applying sensible conversions along the way
// (the string "5" becomes an int, "yes" becomes a bool, "HIGH priority"
// fuzzy-matches an enum member).
//
// This is the technique BAML calls Schema-Aligned Parsing: let the model
// write naturally and make the parser do the work, rather than forcing the
// model into a rigid JSON-mode that empirically degrades quality.
//
//	type Ticket struct {
//	    Title    string   `json:"title"`
//	    Severity Severity `json:"severity"`
//	    Tags     []string `json:"tags"`
//	    DueDays  int      `json:"due_days"`
//	}
//
//	out, err := coerce.Into[Ticket](modelText)
//
// The zero dependency footprint and reflection-driven design mean coerce works
// on any struct you already have — no schema registration, no code generation
// required (though promptr's compiler builds on it).
package coerce
