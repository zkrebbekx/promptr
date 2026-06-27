package dsl

import (
	"sort"
	"strings"
)

// Format pretty-prints .promptr source into a canonical, idempotent layout:
// 2-space indentation, one blank line between top-level declarations, aligned
// class fields and client settings, and a stable ordering of field attributes
// and map keys. Declarations keep their source order. `//` comments are
// preserved — a leading comment block prints directly above the declaration or
// field it precedes, and a trailing comment stays on its line.
//
// Format reports an error (and returns "") if the source does not parse, so it
// never emits a file that silently dropped a malformed declaration.
func Format(src string) (string, error) {
	f, err := Parse(src)
	if err != nil {
		return "", err
	}
	comments := lexComments(src)

	p := &printer{}
	p.file(f)
	out := mergeComments(p.lines, comments)

	var b strings.Builder
	for _, ln := range out {
		b.WriteString(strings.TrimRight(ln, " \t"))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// line is one rendered output line tagged with the source line of the construct
// that produced it (or 0 for structural lines like blanks and braces), so
// comments can be re-anchored by source position.
type line struct {
	text string
	src  int
}

type printer struct {
	lines []line
}

func (p *printer) emit(src int, text string) { p.lines = append(p.lines, line{text, src}) }
func (p *printer) blank()                    { p.lines = append(p.lines, line{"", 0}) }

// decl is one top-level declaration paired with its source line, so the printer
// can render them in source order regardless of how Parse bucketed them.
type decl struct {
	line int
	emit func(*printer)
}

func (p *printer) file(f *File) {
	var ds []decl
	for i := range f.Enums {
		e := f.Enums[i]
		ds = append(ds, decl{e.Line, func(p *printer) { p.enum(e) }})
	}
	for i := range f.Classes {
		c := f.Classes[i]
		ds = append(ds, decl{c.Line, func(p *printer) { p.class(c) }})
	}
	for i := range f.Unions {
		u := f.Unions[i]
		ds = append(ds, decl{u.Line, func(p *printer) { p.union(u) }})
	}
	for i := range f.Clients {
		c := f.Clients[i]
		ds = append(ds, decl{c.Line, func(p *printer) { p.client(c) }})
	}
	for i := range f.Tools {
		t := f.Tools[i]
		ds = append(ds, decl{t.Line, func(p *printer) { p.tool(t) }})
	}
	for i := range f.Funcs {
		fn := f.Funcs[i]
		ds = append(ds, decl{fn.Line, func(p *printer) { p.fn(fn) }})
	}
	for i := range f.Tests {
		t := f.Tests[i]
		ds = append(ds, decl{t.Line, func(p *printer) { p.test(t) }})
	}
	sort.SliceStable(ds, func(i, j int) bool { return ds[i].line < ds[j].line })

	for i, d := range ds {
		if i > 0 {
			p.blank()
		}
		d.emit(p)
	}
}

func (p *printer) enum(e EnumDecl) {
	p.emit(e.Line, "enum "+e.Name+" { "+strings.Join(e.Members, " ")+" }")
}

func (p *printer) union(u UnionDecl) {
	p.emit(u.Line, "union "+u.Name+" = "+strings.Join(u.Variants, " | "))
}

func (p *printer) class(c ClassDecl) {
	p.emit(c.Line, "class "+c.Name+" {")
	nameW, typeW := 0, 0
	for _, fld := range c.Fields {
		nameW = max(nameW, len(fld.Name))
		if attrs := fieldAttrs(fld); len(attrs) > 0 {
			typeW = max(typeW, len(renderType(fld.Type)))
		}
	}
	for _, fld := range c.Fields {
		s := "  " + pad(fld.Name, nameW) + " "
		attrs := fieldAttrs(fld)
		if len(attrs) > 0 {
			s += pad(renderType(fld.Type), typeW) + " " + strings.Join(attrs, " ")
		} else {
			s += renderType(fld.Type)
		}
		p.emit(fld.Line, s)
	}
	p.emit(0, "}")
}

func (p *printer) client(c ClientDecl) {
	p.emit(c.Line, "client "+c.Name+" {")
	type kv struct{ k, v string }
	var rows []kv
	if c.Provider != "" {
		rows = append(rows, kv{"provider", quote(c.Provider)})
	}
	if c.Model != "" {
		rows = append(rows, kv{"model", quote(c.Model)})
	}
	keys := make([]string, 0, len(c.Extra))
	for k := range c.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rows = append(rows, kv{k, quote(c.Extra[k])})
	}
	if c.Policy.Retry > 0 {
		rows = append(rows, kv{"retry", itoa(c.Policy.Retry)})
	}
	if len(c.Policy.Fallback) > 0 {
		rows = append(rows, kv{"fallback", "[" + strings.Join(c.Policy.Fallback, ", ") + "]"})
	}
	if len(c.Policy.RoundRobin) > 0 {
		rows = append(rows, kv{"round_robin", "[" + strings.Join(c.Policy.RoundRobin, ", ") + "]"})
	}
	keyW := 0
	for _, r := range rows {
		keyW = max(keyW, len(r.k))
	}
	for _, r := range rows {
		p.emit(0, "  "+pad(r.k, keyW)+" "+r.v)
	}
	p.emit(0, "}")
}

func (p *printer) tool(t ToolDecl) {
	p.emit(t.Line, "tool "+t.Name+"("+renderParams(t.Params)+") -> "+renderType(t.Ret)+" {")
	if t.Description != "" {
		p.emit(0, "  description "+quote(t.Description))
	}
	p.emit(0, "}")
}

func (p *printer) fn(f FuncDecl) {
	ret := renderType(f.Ret)
	if f.Stream {
		ret = "stream " + ret
	}
	p.emit(f.Line, "function "+f.Name+"("+renderParams(f.Params)+") -> "+ret+" {")
	if f.Client != "" {
		p.emit(0, "  client "+f.Client)
	}
	if f.Description != "" {
		p.emit(0, "  description "+quote(f.Description))
	}
	if len(f.Tools) > 0 {
		p.emit(0, "  tools ["+strings.Join(f.Tools, ", ")+"]")
	}
	if f.Prompt != "" {
		p.emit(0, `  prompt #"`+f.Prompt+`"#`)
	}
	p.emit(0, "}")
}

func (p *printer) test(t TestDecl) {
	p.emit(t.Line, "test "+t.Name+" {")
	if t.Func != "" {
		p.emit(0, "  function "+t.Func)
	}
	p.kvBlock("args", t.Args)
	p.kvBlock("expect", t.Expect)
	p.emit(0, "}")
}

// kvBlock renders a sorted `name { key value ... }` block (used for a test's args
// and expect maps), or nothing when the map is empty. Values are emitted
// verbatim for numeric/bool/enum-member literals and quoted otherwise, matching
// how the codegen typer treats them.
func (p *printer) kvBlock(name string, kv map[string]string) {
	if len(kv) == 0 {
		return
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	keyW := 0
	for _, k := range keys {
		keyW = max(keyW, len(k))
	}
	p.emit(0, "  "+name+" {")
	for _, k := range keys {
		p.emit(0, "    "+pad(k, keyW)+" "+formatValue(kv[k]))
	}
	p.emit(0, "  }")
}

// formatValue renders a test arg/expect value: a bare number or bool keyword is
// left unquoted (matching how it was lexed), everything else is quoted.
func formatValue(v string) string {
	if v == "true" || v == "false" || isNumericLit(v) {
		return v
	}
	return quote(v)
}

func isNumericLit(s string) bool {
	if s == "" {
		return false
	}
	dot, digit := false, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digit = true
		case c == '-' && i == 0:
		case c == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return digit
}

// renderParams renders `name: Type, name: Type` for a tool/function signature.
func renderParams(ps []Param) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.Name + ": " + renderType(p.Type)
	}
	return strings.Join(parts, ", ")
}

// renderType renders a TypeRef back to its source syntax.
func renderType(t TypeRef) string {
	if t.Map {
		s := "map<string, " + renderType(*t.Elem) + ">"
		if t.Optional {
			s += "?"
		}
		return s
	}
	if len(t.Union) > 0 {
		return strings.Join(t.Union, " | ")
	}
	s := t.Name
	if t.List {
		s += "[]"
	}
	if t.Optional {
		s += "?"
	}
	return s
}

// fieldAttrs renders a field's attributes in canonical order.
func fieldAttrs(f FieldDecl) []string {
	var out []string
	if f.Desc != "" {
		out = append(out, "@description("+quote(f.Desc)+")")
	}
	if f.Alias != "" {
		out = append(out, "@alias("+quote(f.Alias)+")")
	}
	if f.Assert != "" {
		out = append(out, "@assert("+quote(f.Assert)+")")
	}
	if f.Check != "" {
		out = append(out, "@check("+quote(f.Check)+")")
	}
	return out
}

// mergeComments re-inserts captured comments into the rendered lines: a trailing
// comment is appended to the output line sharing its source line; a standalone
// comment prints just above the first output line whose source line is at or
// past it (or at the end if there is none), indented to match that line.
func mergeComments(lines []line, comments []comment) []string {
	sort.SliceStable(comments, func(i, j int) bool { return comments[i].line < comments[j].line })

	leading := map[int][]string{} // output index -> comment texts to print before it
	const atEnd = -1
	var endComments []string

	for _, c := range comments {
		text := "//"
		if c.text != "" {
			text = "// " + strings.TrimLeft(c.text, " ")
		}
		if !c.standalone {
			if idx := lineWithSrc(lines, c.line); idx >= 0 {
				lines[idx].text += " " + text
				continue
			}
		}
		idx := firstAnchorAtOrAfter(lines, c.line)
		if idx == atEnd {
			endComments = append(endComments, text)
		} else {
			leading[idx] = append(leading[idx], text)
		}
	}

	var out []string
	for i, ln := range lines {
		for _, ct := range leading[i] {
			out = append(out, indentOf(ln.text)+ct)
		}
		out = append(out, ln.text)
	}
	out = append(out, endComments...)
	return out
}

// lineWithSrc returns the index of the output line tagged with source line src,
// or -1.
func lineWithSrc(lines []line, src int) int {
	for i, ln := range lines {
		if ln.src == src {
			return i
		}
	}
	return -1
}

// firstAnchorAtOrAfter returns the index of the first output line whose source
// line is >= src (and non-structural), or -1 if none follow.
func firstAnchorAtOrAfter(lines []line, src int) int {
	for i, ln := range lines {
		if ln.src >= src && ln.src != 0 {
			return i
		}
	}
	return -1
}

// indentOf returns the leading whitespace of s.
func indentOf(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return s[:i]
		}
	}
	return s
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// quote renders s as a double-quoted .promptr string with the minimal escaping
// the lexer's unescape understands.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
