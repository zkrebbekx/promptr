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
	Unions  []UnionDecl
	Clients []ClientDecl
	Funcs   []FuncDecl
	Tests   []TestDecl
}

// UnionDecl is `union Name = A | B | C` — a closed set of variant types the
// model output is classified into (compiles to a sealed interface + a
// coerce.Union resolver, mirroring sumx's sealed sum types).
type UnionDecl struct {
	Name     string
	Variants []string
	Line     int
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

// FieldDecl is one `name Type` line inside a class. A field may carry
// @description / @alias attributes that tune the baked schema (and, for @alias,
// the json name the coerce kernel binds the model's output to).
type FieldDecl struct {
	Name  string
	Type  TypeRef
	Desc  string // @description("...") — human guidance shown to the model
	Alias string // @alias("...") — alternate wire/prompt name for this field
}

// TypeRef names a field/return/param type: a primitive (string, int, float,
// bool) or a declared class/enum, optionally a list and/or optional. It can
// also be a map (map<string, V>) or an inline union (A | B).
//
//	string         -> {Name:"string"}
//	string[]       -> {Name:"string", List:true}
//	Severity?      -> {Name:"Severity", Optional:true}
//	map<string,int>-> {Map:true, Elem:&{Name:"int"}}
//	Search|Escalate-> {Union:["Search","Escalate"]}
type TypeRef struct {
	Name     string
	List     bool
	Optional bool
	Map      bool     // map<string, Elem>; key is always string
	Elem     *TypeRef // map value type when Map is true
	Union    []string // inline union variant names (Name is empty when set)
}

// ClientDecl is `client Name { provider "x" model "y" ... }` — a named binding
// the runtime resolves to a Provider. Provider/Model are lifted out; any other
// key/value settings are kept in Extra. A client may instead (or additionally)
// carry a reliability Policy that composes other declared clients.
type ClientDecl struct {
	Name     string
	Provider string
	Model    string
	Extra    map[string]string
	Policy   Policy
	Line     int
}

// Policy is the optional reliability wrapping on a client: retry the wrapped
// call, fail over across other named clients, or round-robin across them.
// Fallback and RoundRobin hold the names of other clients in this file.
type Policy struct {
	Retry      int
	Fallback   []string
	RoundRobin []string
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
