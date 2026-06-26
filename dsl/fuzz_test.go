package dsl

import "testing"

// FuzzParse asserts the lexer+parser never panic and always terminate on
// arbitrary input — the same forward-progress guarantee the coerce parser holds.
func FuzzParse(f *testing.F) {
	f.Add(sample)
	f.Add("")
	f.Add("enum")
	f.Add("class X {")
	f.Add("function F(a:")
	f.Add("prompt #\"unterminated")
	f.Add("#\"")
	f.Add("-> ? [] {} ()")
	f.Add("client C { provider }")

	f.Fuzz(func(t *testing.T, src string) {
		f, _ := Parse(src)
		if f == nil {
			t.Fatal("Parse returned nil File")
		}
	})
}
