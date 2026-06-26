package coerce_test

import (
	"testing"

	"github.com/zkrebbekx/promptr/coerce"
)

// FuzzInto asserts the tolerant parser/coercer never panics, no matter how
// malformed the input — the whole point of schema-aligned parsing is to digest
// whatever a model emits.
func FuzzInto(f *testing.F) {
	seeds := []string{
		``, `{`, `[`, `}`, `]`, `:`, `,`,
		`{"a":`, `{"a": [1, 2`, `'single'`, "```json\n{}",
		`{a: b, c: 'd',}`, `null`, `true`, `-`, `$`, `{"x": "\u00"`,
		`[[[[[[[[`, `{"k":{"k":{"k":`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = coerce.Into[Ticket](s)
		_, _ = coerce.Into[map[string]any](s)
		_, _ = coerce.Into[[]int](s)
		_, _ = coerce.Into[int](s)
	})
}
