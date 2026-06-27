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
			switch {
			case v.tools[ref]:
				// a Go-backed tool — fine.
			case v.funcs[ref]:
				v.checkSubAgent(f, fn, ref)
			default:
				v.addf(fn.Line, "function %q uses unknown tool or sub-agent %q", fn.Name, ref)
			}
		}
		v.checkTypeRef(fn.Line, fn.Name, fn.Ret)
		for _, pm := range fn.Params {
			v.checkTypeRef(fn.Line, fn.Name, pm.Type)
		}
	}
	v.checkDelegationCycles(f)
}

// checkSubAgent validates that a function delegated to as a sub-agent (named in
// another function's `tools` list) is wireable without a caller-supplied handler:
// it must not stream, must take no binary part params (their JSON args can't be
// coerced into a Part), and must not itself need handlers — i.e. its own tools
// list must be pure sub-agents, never a Go-backed `tool`.
func (v *validator) checkSubAgent(f *File, parent FuncDecl, ref string) {
	if ref == parent.Name {
		v.addf(parent.Line, "function %q cannot delegate to itself as a sub-agent", parent.Name)
		return
	}
	sub, ok := funcByName(f, ref)
	if !ok {
		return
	}
	if sub.Stream {
		v.addf(parent.Line, "function %q cannot use streaming function %q as a sub-agent", parent.Name, ref)
	}
	for _, pm := range sub.Params {
		if isPartName(pm.Type.Name) {
			v.addf(parent.Line, "function %q cannot use sub-agent %q: it takes a binary part parameter", parent.Name, ref)
			break
		}
	}
	for _, inner := range sub.Tools {
		if v.tools[inner] {
			v.addf(parent.Line, "function %q cannot use sub-agent %q: it requires tool handlers (calls tool %q)", parent.Name, ref, inner)
			break
		}
	}
}

// checkDelegationCycles reports a cycle in the function→sub-agent delegation
// graph, which would otherwise generate mutually-recursive, non-terminating Go.
func (v *validator) checkDelegationCycles(f *File) {
	state := map[string]int{} // 0 unvisited, 1 on-stack, 2 done
	var walk func(name string) bool
	walk = func(name string) bool {
		state[name] = 1
		fn, ok := funcByName(f, name)
		if ok {
			for _, ref := range fn.Tools {
				if !v.funcs[ref] {
					continue // only function refs form delegation edges
				}
				switch state[ref] {
				case 1:
					v.addf(fn.Line, "sub-agent delegation cycle through function %q", ref)
					return true
				case 0:
					if walk(ref) {
						return true
					}
				}
			}
		}
		state[name] = 2
		return false
	}
	for _, fn := range f.Funcs {
		if state[fn.Name] == 0 {
			if walk(fn.Name) {
				return
			}
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
		// A generated Go test invokes the function directly, so the target must be
		// a plain (non-streaming, non-tool) function with no binary-part params.
		switch {
		case fn.Stream:
			v.addf(t.Line, "test %q cannot target streaming function %q", t.Name, t.Func)
		case len(fn.Tools) > 0:
			v.addf(t.Line, "test %q cannot target tool-using function %q", t.Name, t.Func)
		}
		for _, pm := range fn.Params {
			if isPartName(pm.Type.Name) {
				v.addf(t.Line, "test %q cannot target %q: parameter %q is a binary part", t.Name, t.Func, pm.Name)
			}
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
		v.checkExpect(f, t, fn)
	}
}

// checkExpect validates a test's `expect` block: the function's return type must
// be a class, each expected key a field of it, and each value compatible with
// that field's (scalar or enum) type. List/map/union fields and non-class
// returns are not assertable.
func (v *validator) checkExpect(f *File, t TestDecl, fn FuncDecl) {
	if len(t.Expect) == 0 {
		return
	}
	cls, ok := classByName(f, fn.Ret.Name)
	if !ok || fn.Ret.List || fn.Ret.Map || len(fn.Ret.Union) > 0 {
		v.addf(t.Line, "test %q has expect, but %q does not return a class", t.Name, t.Func)
		return
	}
	fields := map[string]TypeRef{}
	for _, fld := range cls.Fields {
		fields[fld.Name] = fld.Type
	}
	for _, k := range t.ExpectKeys {
		ft, isField := fields[k]
		if !isField {
			v.addf(t.Line, "test %q expects field %q, not a field of %q", t.Name, k, cls.Name)
			continue
		}
		if ft.List || ft.Map || len(ft.Union) > 0 {
			v.addf(t.Line, "test %q cannot assert field %q: only scalar and enum fields are assertable", t.Name, k)
			continue
		}
		val := t.Expect[k]
		switch {
		case isPrimitive(ft.Name):
			if !valueFitsPrimitive(ft.Name, val) {
				v.addf(t.Line, "test %q field %q expects a %s, got %q", t.Name, k, ft.Name, val)
			}
		case v.enums[ft.Name]:
			if !enumHasMember(f, ft.Name, val) {
				v.addf(t.Line, "test %q field %q expects a %s member, got %q", t.Name, k, ft.Name, val)
			}
		default:
			v.addf(t.Line, "test %q cannot assert field %q of type %q", t.Name, k, ft.Name)
		}
	}
}

func classByName(f *File, name string) (ClassDecl, bool) {
	for _, c := range f.Classes {
		if c.Name == name {
			return c, true
		}
	}
	return ClassDecl{}, false
}

func enumHasMember(f *File, enum, member string) bool {
	for _, e := range f.Enums {
		if e.Name != enum {
			continue
		}
		for _, m := range e.Members {
			if m == member {
				return true
			}
		}
	}
	return false
}

// valueFitsPrimitive reports whether the literal text is well-formed for a
// primitive field type (numbers must parse; bools must be true/false; strings
// accept anything).
func valueFitsPrimitive(prim, val string) bool {
	switch prim {
	case "string":
		return true
	case "bool":
		return val == "true" || val == "false"
	case "int":
		return isIntLit(val)
	case "float":
		return isFloatLit(val)
	default:
		return false
	}
}

func isIntLit(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '-' {
		if len(s) == 1 {
			return false
		}
		i = 1
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isFloatLit(s string) bool {
	if s == "" {
		return false
	}
	i, dots, digits := 0, 0, 0
	if s[0] == '-' {
		i = 1
	}
	for ; i < len(s); i++ {
		switch {
		case s[i] >= '0' && s[i] <= '9':
			digits++
		case s[i] == '.':
			dots++
		default:
			return false
		}
	}
	return digits > 0 && dots <= 1
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
