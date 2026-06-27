//go:build js && wasm

// Command playground is the WASM backend for the promptr web playground. It
// exposes three functions to JavaScript:
//
//	promptrGenerate(src)  -> { go, err }   compile .promptr source to Go
//	promptrFormat(src)    -> { src, err }  canonically format .promptr source
//	promptrParse(raw)     -> { json, err } run the tolerant coerce parser
//
// All run fully client-side — no API calls — so the page is a self-contained
// demo of the compiler, the canonical formatter, and the schema-aligned parser
// kernel.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/zkrebbekx/promptr/codegen"
	"github.com/zkrebbekx/promptr/coerce"
	"github.com/zkrebbekx/promptr/dsl"
)

func generate(_ js.Value, args []js.Value) any {
	res := map[string]any{"go": "", "tests": "", "err": ""}
	if len(args) < 1 {
		res["err"] = "no input"
		return js.ValueOf(res)
	}
	f, perr := dsl.Parse(args[0].String())
	out, gerr := codegen.Generate("app", f)
	res["go"] = string(out)
	// `test` blocks compile to a sibling _test.go; surface it too so the live-test
	// feature is visible in the playground.
	if tests, terr := codegen.GenerateTests("app", f); terr == nil && tests != nil {
		res["tests"] = string(tests)
	}
	switch {
	case perr != nil:
		res["err"] = perr.Error()
	case gerr != nil:
		res["err"] = gerr.Error()
	}
	return js.ValueOf(res)
}

func format(_ js.Value, args []js.Value) any {
	res := map[string]any{"src": "", "err": ""}
	if len(args) < 1 {
		res["err"] = "no input"
		return js.ValueOf(res)
	}
	out, err := dsl.Format(args[0].String())
	if err != nil {
		res["err"] = err.Error()
		return js.ValueOf(res)
	}
	res["src"] = out
	return js.ValueOf(res)
}

func parse(_ js.Value, args []js.Value) any {
	res := map[string]any{"json": "", "err": ""}
	if len(args) < 1 {
		res["err"] = "no input"
		return js.ValueOf(res)
	}
	v, err := coerce.Into[any](args[0].String())
	if err != nil {
		res["err"] = err.Error()
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	res["json"] = string(b)
	return js.ValueOf(res)
}

func main() {
	js.Global().Set("promptrGenerate", js.FuncOf(generate))
	js.Global().Set("promptrFormat", js.FuncOf(format))
	js.Global().Set("promptrParse", js.FuncOf(parse))
	select {}
}
