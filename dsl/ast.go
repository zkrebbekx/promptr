// Package dsl lexes and parses the .promptr schema language into an AST.
//
// A .promptr file declares the data shapes (class, enum), the model bindings
// (client) and the typed LLM functions (function) that promptr's compiler turns
// into idiomatic Go. The grammar is a small, non-Turing-complete subset in the
// spirit of pgparse's SQL grammar — easy to lex, easy to reason about.
package dsl

// File is the root of a parsed .promptr document.
type File struct {
	Enums   []EnumDecl
	Classes []ClassDecl
	Clients []ClientDecl
	Funcs   []FuncDecl
	Tests   []TestDecl
}

// EnumDecl is `enum Name { A B C }` — a closed set of string members.
type EnumDecl struct {
	Name    string
	Members []string
	Line    int
}

// ClassDecl is `class Name { field Type ... }` — a structured output shape.
type ClassDecl struct {
	Name   string
	Fields []FieldDecl
	Line   int
}

// FieldDecl is one `name Type` line inside a class.
type FieldDecl struct {
	Name string
	Type TypeRef
}

// TypeRef names a field/return/param type: a primitive (string, int, float,
// bool) or a declared class/enum, optionally a list and/or optional.
//
//	string      -> {Name:"string"}
//	string[]    -> {Name:"string", List:true}
//	Severity?   -> {Name:"Severity", Optional:true}
type TypeRef struct {
	Name     string
	List     bool
	Optional bool
}

// ClientDecl is `client Name { provider "x" model "y" ... }` — a named binding
// the runtime resolves to a Provider. Provider/Model are lifted out; any other
// key/value settings are kept in Extra.
type ClientDecl struct {
	Name     string
	Provider string
	Model    string
	Extra    map[string]string
	Line     int
}

// Param is one `name: Type` function parameter.
type Param struct {
	Name string
	Type TypeRef
}

// FuncDecl is a typed LLM function: inputs, an output type, the client it runs
// on, and the prompt template (with {{ ... }} holes the compiler fills).
type FuncDecl struct {
	Name   string
	Params []Param
	Ret    TypeRef
	Client string
	Prompt string
	Line   int
}

// TestDecl is `test Name { function F args { k v ... } }` — an example
// invocation the compiler turns into a runnable test.
type TestDecl struct {
	Name string
	Func string
	Args map[string]string
	Line int
}
