package dsl

import (
	"fmt"
	"sort"
)

// Diagnostic is a semantic problem found in a parsed File: a human-readable
// message anchored to a source line. Diagnostics are distinct from parse errors
// (malformed syntax) — they catch references that don't resolve, duplicate
// declarations and test blocks that don't match their function.
type Diagnostic struct {
	Line int
	Msg  string
}

func (d Diagnostic) String() string { return fmt.Sprintf("line %d: %s", d.Line, d.Msg) }

// Validate checks a parsed File for semantic problems and returns the
// diagnostics sorted by line. It assumes the File parsed (run Parse first); it
// is reused by `promptr check` and the LSP so both report identically.
func Validate(f *File) []Diagnostic {
	v := &validator{
		classes: map[string]bool{},
		enums:   map[string]bool{},
		unions:  map[string]bool{},
		clients: map[string]bool{},
		tools:   map[string]bool{},
		funcs:   map[string]bool{},
	}
	v.collect(f)
	v.checkUnions(f)
	v.checkClients(f)
	v.checkTools(f)
	v.checkFuncs(f)
	v.checkTests(f)

	sort.SliceStable(v.diags, func(i, j int) bool { return v.diags[i].Line < v.diags[j].Line })
	return v.diags
}

type validator struct {
	classes map[string]bool
	enums   map[string]bool
	unions  map[string]bool
	clients map[string]bool
	tools   map[string]bool
	funcs   map[string]bool
	diags   []Diagnostic
}

func (v *validator) addf(line int, format string, args ...any) {
	v.diags = append(v.diags, Diagnostic{Line: line, Msg: fmt.Sprintf(format, args...)})
}

// declares reports whether name is any declared type (class/enum/union).
func (v *validator) isType(name string) bool {
	return v.classes[name] || v.enums[name] || v.unions[name]
}

func (v *validator) collect(f *File) {
	mark := func(m map[string]bool, name string, line int, kind string) {
		if v.classes[name] || v.enums[name] || v.unions[name] || v.clients[name] || v.tools[name] || v.funcs[name] {
			v.addf(line, "duplicate declaration %q", name)
		}
		m[name] = true
		_ = kind
	}
	for _, d := range f.Enums {
		mark(v.enums, d.Name, d.Line, "enum")
	}
	for _, d := range f.Classes {
		mark(v.classes, d.Name, d.Line, "class")
	}
	for _, d := range f.Unions {
		mark(v.unions, d.Name, d.Line, "union")
	}
	for _, d := range f.Clients {
		mark(v.clients, d.Name, d.Line, "client")
	}
	for _, d := range f.Tools {
		mark(v.tools, d.Name, d.Line, "tool")
	}
	for _, d := range f.Funcs {
		mark(v.funcs, d.Name, d.Line, "function")
	}
}

// checkTools validates each tool's parameter and return types resolve.
func (v *validator) checkTools(f *File) {
	for _, t := range f.Tools {
		v.checkTypeRef(t.Line, "tool "+t.Name, t.Ret)
		for _, pm := range t.Params {
			v.checkTypeRef(t.Line, "tool "+t.Name, pm.Type)
		}
	}
}

func (v *validator) checkUnions(f *File) {
	for _, u := range f.Unions {
		if len(u.Variants) < 2 {
			v.addf(u.Line, "union %q must have at least two variants", u.Name)
		}
		for _, variant := range u.Variants {
			if !v.classes[variant] {
				v.addf(u.Line, "union %q variant %q is not a declared class", u.Name, variant)
			}
		}
	}
}

func (v *validator) checkClients(f *File) {
	for _, c := range f.Clients {
		refs := append(append([]string{}, c.Policy.Fallback...), c.Policy.RoundRobin...)
		for _, ref := range refs {
			if !v.clients[ref] {
				v.addf(c.Line, "client %q references unknown client %q", c.Name, ref)
			}
		}
	}
}

func (v *validator) checkFuncs(f *File) {
	for _, fn := range f.Funcs {
		if fn.Client != "" && !v.clients[fn.Client] {
			v.addf(fn.Line, "function %q uses unknown client %q", fn.Name, fn.Client)
		}
		for _, variant := range fn.Ret.Union {
			if !v.classes[variant] {
				v.addf(fn.Line, "function %q inline-union variant %q is not a declared class", fn.Name, variant)
			}
		}
		for _, ref := range fn.Tools {
			if !v.tools[ref] {
				v.addf(fn.Line, "function %q uses unknown tool %q", fn.Name, ref)
			}
		}
		v.checkTypeRef(fn.Line, fn.Name, fn.Ret)
		for _, pm := range fn.Params {
			v.checkTypeRef(fn.Line, fn.Name, pm.Type)
		}
	}
}

// checkTypeRef warns on a named type that resolves to nothing (not a primitive,
// not a declared type, not a multimodal part type).
func (v *validator) checkTypeRef(line int, where string, tr TypeRef) {
	if tr.Map {
		if tr.Elem != nil {
			v.checkTypeRef(line, where, *tr.Elem)
		}
		return
	}
	if len(tr.Union) > 0 || tr.Name == "" {
		return
	}
	if isPrimitive(tr.Name) || isPartName(tr.Name) || v.isType(tr.Name) {
		return
	}
	v.addf(line, "%s refers to unknown type %q", where, tr.Name)
}

func (v *validator) checkTests(f *File) {
	for _, t := range f.Tests {
		fn, ok := funcByName(f, t.Func)
		if !ok {
			v.addf(t.Line, "test %q references unknown function %q", t.Name, t.Func)
			continue
		}
		params := map[string]bool{}
		for _, pm := range fn.Params {
			params[pm.Name] = true
		}
		for arg := range t.Args {
			if !params[arg] {
				v.addf(t.Line, "test %q sets arg %q, not a parameter of %q", t.Name, arg, t.Func)
			}
		}
		for _, pm := range fn.Params {
			if _, set := t.Args[pm.Name]; !set {
				v.addf(t.Line, "test %q is missing arg %q required by %q", t.Name, pm.Name, t.Func)
			}
		}
	}
}

func funcByName(f *File, name string) (FuncDecl, bool) {
	for _, fn := range f.Funcs {
		if fn.Name == name {
			return fn, true
		}
	}
	return FuncDecl{}, false
}

func isPrimitive(name string) bool {
	switch name {
	case "string", "int", "float", "bool":
		return true
	default:
		return false
	}
}

func isPartName(name string) bool {
	switch name {
	case "image", "audio", "pdf", "file":
		return true
	default:
		return false
	}
}
