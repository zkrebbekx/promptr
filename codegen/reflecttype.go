package codegen

import (
	"reflect"

	"github.com/zkrebbekx/promptr/dsl"
)

// Package-level reflect.Type singletons for the leaf kinds a schema maps onto.
var (
	anyType     = reflect.TypeOf((*any)(nil)).Elem()
	stringType  = reflect.TypeOf("")
	intType     = reflect.TypeOf(0)
	float64Type = reflect.TypeOf(float64(0))
	boolType    = reflect.TypeOf(false)
)

// TargetType picks the schema a tolerant-parse caller (e.g. the playground's
// "schema-aligned parse" pane) should coerce messy model output into, and builds
// a reflect.Type for it. The chosen schema is the return class of the last
// function that returns a declared class — the shape the model actually produces
// — falling back to the first declared class when no function returns one.
//
// Coercing into the resulting struct is what makes parsing schema-aligned rather
// than generic: coerce binds the model's keys to the struct's fields case- and
// separator-insensitively (so userName / Email-Addr / login_count all snap into
// the declared fields) and converts loose scalars to the declared Go types.
//
// It returns ok=false when the file declares no class to align to, in which case
// the caller should fall back to generic parsing.
func TargetType(f *dsl.File) (t reflect.Type, ok bool) {
	if f == nil {
		return nil, false
	}
	b := &typeBuilder{
		classes: make(map[string]dsl.ClassDecl, len(f.Classes)),
		enums:   make(map[string]bool, len(f.Enums)),
	}
	for _, c := range f.Classes {
		b.classes[c.Name] = c
	}
	for _, e := range f.Enums {
		b.enums[e.Name] = true
	}
	name := b.targetClassName(f)
	if name == "" {
		return nil, false
	}
	return b.classStruct(b.classes[name], map[string]bool{}), true
}

type typeBuilder struct {
	classes map[string]dsl.ClassDecl
	enums   map[string]bool
}

// targetClassName prefers a function's plain (non-list, non-map, non-union)
// return class — scanning from the last function so a coordinator's final shape
// wins — then falls back to the first declared class.
func (b *typeBuilder) targetClassName(f *dsl.File) string {
	for i := len(f.Funcs) - 1; i >= 0; i-- {
		r := f.Funcs[i].Ret
		if r.List || r.Map || len(r.Union) > 0 {
			continue
		}
		if _, ok := b.classes[r.Name]; ok {
			return r.Name
		}
	}
	if len(f.Classes) > 0 {
		return f.Classes[0].Name
	}
	return ""
}

// classStruct builds a struct type for a class. onPath tracks the classes
// currently being built so a self- or mutually-recursive class degrades to any
// (reflect.StructOf cannot express a type that references itself).
func (b *typeBuilder) classStruct(c dsl.ClassDecl, onPath map[string]bool) reflect.Type {
	if onPath[c.Name] {
		return anyType
	}
	onPath[c.Name] = true
	defer delete(onPath, c.Name)

	fields := make([]reflect.StructField, 0, len(c.Fields))
	for _, fld := range c.Fields {
		fields = append(fields, reflect.StructField{
			Name: exportName(fld.Name),
			Type: b.refType(fld.Type, onPath),
			Tag:  reflect.StructTag(`json:"` + wireName(fld) + `"`),
		})
	}
	if len(fields) == 0 {
		return anyType
	}
	return reflect.StructOf(fields)
}

// refType maps a TypeRef onto a reflect.Type, recursing through lists, maps and
// nested classes. Unions and unknown names degrade to any so input still parses.
func (b *typeBuilder) refType(t dsl.TypeRef, onPath map[string]bool) reflect.Type {
	if t.Map {
		elem := anyType
		if t.Elem != nil {
			elem = b.refType(*t.Elem, onPath)
		}
		return reflect.MapOf(stringType, elem)
	}
	if len(t.Union) > 0 {
		return anyType
	}
	base := b.scalarType(t.Name, onPath)
	if t.List {
		return reflect.SliceOf(base)
	}
	return base
}

func (b *typeBuilder) scalarType(name string, onPath map[string]bool) reflect.Type {
	switch name {
	case "string":
		return stringType
	case "int":
		return intType
	case "float":
		return float64Type
	case "bool":
		return boolType
	}
	if c, ok := b.classes[name]; ok {
		return b.classStruct(c, onPath)
	}
	if b.enums[name] {
		// Enums are closed sets of string members; a string field captures them.
		return stringType
	}
	return anyType
}

// exportName turns a schema field name into a valid exported Go identifier so
// reflect.StructOf accepts it (an unexported or malformed name would panic).
func exportName(s string) string {
	n := goName(s)
	if n == "" || n[0] < 'A' || n[0] > 'Z' {
		n = "F" + n
	}
	return n
}
