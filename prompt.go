package promptr

import (
	"fmt"
	"reflect"
	"strings"
)

// Render expands a prompt template against a context map and returns the
// finished prompt. It is the small, dependency-free template engine the
// compiler targets: generated functions call Render with the call's parameters
// (plus a "ctx" entry carrying the baked output-schema) so prompts can use
// control flow over runtime values.
//
// Supported syntax (all inside {{ }}):
//
//	{{ name }}              value lookup, with dotted paths: {{ user.name }}
//	{{ ctx.output_schema }} the compiler-injected schema description
//	{{ if cond }}…{{ end }} conditional, with optional {{ else }}
//	{{ for x in items }}…{{ end }}   iterate a slice, binding x in the body
//
// A condition is a path (truthy test), `not <path>`, or `<path> == "lit"` /
// `<path> != "lit"`. Lookups miss softly: an unknown name renders as empty
// rather than erroring, so a template can never panic on real model context.
// Render only returns an error for a structurally broken template (an
// unterminated {{ if }}/{{ for }}).
func Render(tmpl string, ctx map[string]any) (string, error) {
	nodes, _, err := parseTemplate(lexTemplate(tmpl), 0, "")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	renderNodes(&b, nodes, []map[string]any{ctx})
	return b.String(), nil
}

// --- lexing: split into literal text and {{ tag }} segments ----------------

type segKind uint8

const (
	segText segKind = iota
	segTag
)

type segment struct {
	kind segKind
	text string // literal text, or the trimmed tag body
}

func lexTemplate(s string) []segment {
	var segs []segment
	for {
		open := strings.Index(s, "{{")
		if open < 0 {
			if s != "" {
				segs = append(segs, segment{segText, s})
			}
			return segs
		}
		endIdx := strings.Index(s[open:], "}}")
		if endIdx < 0 {
			// No closing braces — the rest is literal text.
			segs = append(segs, segment{segText, s})
			return segs
		}
		if open > 0 {
			segs = append(segs, segment{segText, s[:open]})
		}
		segs = append(segs, segment{segTag, strings.TrimSpace(s[open+2 : open+endIdx])})
		s = s[open+endIdx+2:]
	}
}

// --- parsing: segments -> a node tree --------------------------------------

type node interface{}

type textNode struct{ text string }
type varNode struct{ path string }
type ifNode struct {
	cond      string
	then, alt []node
}
type forNode struct {
	name string
	path string
	body []node
}

// parseTemplate consumes segments starting at i, building nodes until it hits a
// terminator tag (one of stop, comma-separated). It returns the nodes, the
// index just past the terminator, and the terminator text.
func parseTemplate(segs []segment, i int, stop string) ([]node, int, error) {
	var nodes []node
	stops := map[string]bool{}
	for _, s := range strings.Split(stop, ",") {
		if s != "" {
			stops[s] = true
		}
	}
	for i < len(segs) {
		s := segs[i]
		if s.kind == segText {
			nodes = append(nodes, textNode{s.text})
			i++
			continue
		}
		head := tagHead(s.text)
		if stops[head] || (stop != "" && stops[s.text]) {
			return nodes, i + 1, nil
		}
		switch head {
		case "if":
			n, next, err := parseIf(segs, i)
			if err != nil {
				return nil, 0, err
			}
			nodes = append(nodes, n)
			i = next
		case "for":
			n, next, err := parseFor(segs, i)
			if err != nil {
				return nil, 0, err
			}
			nodes = append(nodes, n)
			i = next
		case "end", "else":
			// A stray terminator with no opener: treat as plain text so a
			// prompt mentioning "end" can't break rendering.
			nodes = append(nodes, textNode{"{{" + s.text + "}}"})
			i++
		default:
			nodes = append(nodes, varNode{s.text})
			i++
		}
	}
	if stop != "" {
		return nil, 0, &TemplateError{Msg: "unterminated {{ " + stop + " }}"}
	}
	return nodes, i, nil
}

func parseIf(segs []segment, i int) (node, int, error) {
	cond := strings.TrimSpace(strings.TrimPrefix(segs[i].text, "if"))
	then, next, err := parseTemplate(segs, i+1, "else,end")
	if err != nil {
		return nil, 0, err
	}
	n := ifNode{cond: cond, then: then}
	// If parseTemplate stopped on else, the body up to end is the alt branch.
	if next-1 < len(segs) && segs[next-1].kind == segTag && segs[next-1].text == "else" {
		alt, after, err := parseTemplate(segs, next, "end")
		if err != nil {
			return nil, 0, err
		}
		n.alt = alt
		next = after
	}
	return n, next, nil
}

func parseFor(segs []segment, i int) (node, int, error) {
	// for X in PATH
	fields := strings.Fields(segs[i].text)
	if len(fields) != 4 || fields[2] != "in" {
		return nil, 0, &TemplateError{Msg: "malformed for: {{ " + segs[i].text + " }}"}
	}
	body, next, err := parseTemplate(segs, i+1, "end")
	if err != nil {
		return nil, 0, err
	}
	return forNode{name: fields[1], path: fields[3], body: body}, next, nil
}

func tagHead(tag string) string {
	if i := strings.IndexAny(tag, " \t"); i >= 0 {
		return tag[:i]
	}
	return tag
}

// TemplateError reports a structurally invalid template.
type TemplateError struct{ Msg string }

func (e *TemplateError) Error() string { return "promptr: template: " + e.Msg }

// --- rendering -------------------------------------------------------------

func renderNodes(b *strings.Builder, nodes []node, scopes []map[string]any) {
	for _, n := range nodes {
		switch n := n.(type) {
		case textNode:
			b.WriteString(n.text)
		case varNode:
			b.WriteString(stringify(lookup(n.path, scopes)))
		case ifNode:
			if truthy(evalCond(n.cond, scopes)) {
				renderNodes(b, n.then, scopes)
			} else {
				renderNodes(b, n.alt, scopes)
			}
		case forNode:
			each(lookup(n.path, scopes), func(v any) {
				renderNodes(b, n.body, append(scopes, map[string]any{n.name: v}))
			})
		}
	}
}

// evalCond returns the value a condition resolves to: a bool for comparisons
// and negation, or the looked-up value for a bare truthiness test.
func evalCond(cond string, scopes []map[string]any) any {
	cond = strings.TrimSpace(cond)
	if rest, ok := strings.CutPrefix(cond, "not "); ok {
		return !truthy(evalCond(rest, scopes))
	}
	if op, lhs, rhs, ok := splitCompare(cond); ok {
		got := stringify(lookup(strings.TrimSpace(lhs), scopes))
		want := strings.Trim(strings.TrimSpace(rhs), `"`)
		if op == "==" {
			return got == want
		}
		return got != want
	}
	return lookup(cond, scopes)
}

func splitCompare(s string) (op, lhs, rhs string, ok bool) {
	for _, o := range []string{"==", "!="} {
		if i := strings.Index(s, o); i >= 0 {
			return o, s[:i], s[i+2:], true
		}
	}
	return "", "", "", false
}

// lookup resolves a dotted path against the scope stack (innermost first),
// descending through maps and structs. A miss returns nil.
func lookup(path string, scopes []map[string]any) any {
	parts := strings.Split(strings.TrimSpace(path), ".")
	if len(parts) == 0 {
		return nil
	}
	var cur any
	found := false
	for i := len(scopes) - 1; i >= 0; i-- {
		if v, ok := scopes[i][parts[0]]; ok {
			cur, found = v, true
			break
		}
	}
	if !found {
		return nil
	}
	for _, p := range parts[1:] {
		cur = field(cur, p)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// field descends one level into a map or struct by key/field name.
func field(v any, name string) any {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		mv := rv.MapIndex(reflect.ValueOf(name))
		if mv.IsValid() {
			return mv.Interface()
		}
	case reflect.Struct:
		if f := rv.FieldByNameFunc(func(s string) bool {
			return strings.EqualFold(s, name)
		}); f.IsValid() && f.CanInterface() {
			return f.Interface()
		}
	}
	return nil
}

// each calls fn for every element of a slice/array value; non-iterables are a
// no-op, so {{ for }} over a missing or scalar value renders nothing.
func each(v any, fn func(any)) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return
	}
	for i := 0; i < rv.Len(); i++ {
		fn(rv.Index(i).Interface())
	}
}

func truthy(v any) bool {
	if v == nil {
		return false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Bool:
		return rv.Bool()
	case reflect.String:
		return rv.Len() > 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() != 0
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() > 0
	case reflect.Pointer, reflect.Interface:
		return !rv.IsNil()
	}
	return true
}

// stringify renders a value for interpolation. Strings pass through; everything
// else uses fmt-style formatting via reflect to avoid importing fmt's verbs for
// the common path.
func stringify(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
