package promptr_test

import (
	"testing"

	"github.com/zkrebbekx/promptr"
)

// FuzzRender asserts the template engine never panics and always terminates on
// arbitrary template text — the same forward-progress guarantee the DSL parser
// and coerce kernel hold.
func FuzzRender(f *testing.F) {
	seeds := []string{
		"",
		"plain text",
		"{{ name }}",
		"{{ if x }}a{{ else }}b{{ end }}",
		"{{ for t in tags }}{{ t }}{{ end }}",
		"{{ unterminated",
		"{{ if }}{{ for }}{{ end }}{{ end }}",
		"{{ ctx.output_schema }}",
		"{{ a.b.c.d }}",
		"{{ if not x == \"y\" }}{{ end }}",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	ctx := map[string]any{
		"name": "v",
		"x":    true,
		"tags": []string{"a", "b"},
		"a":    map[string]any{"b": map[string]any{"c": 1}},
		"ctx":  map[string]any{"output_schema": "S"},
	}
	f.Fuzz(func(_ *testing.T, tmpl string) {
		_, _ = promptr.Render(tmpl, ctx)
	})
}
