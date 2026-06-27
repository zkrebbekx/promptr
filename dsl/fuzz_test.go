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

// FuzzFormat asserts the formatter never panics and is idempotent: formatting an
// already-formatted file is a fixed point. Inputs that do not parse are skipped
// (Format reports an error and emits nothing).
func FuzzFormat(f *testing.F) {
	f.Add(sample)
	f.Add("// c\nenum E { A B }")
	f.Add("class X { a string @assert(\"required\") }")
	f.Add("client C { provider \"p\" model \"m\" }")
	f.Add("union U = A | B")

	f.Fuzz(func(t *testing.T, src string) {
		once, err := Format(src)
		if err != nil {
			return
		}
		twice, err := Format(once)
		if err != nil {
			t.Fatalf("formatted output failed to re-format: %v", err)
		}
		if once != twice {
			t.Fatalf("Format not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
		}
	})
}
