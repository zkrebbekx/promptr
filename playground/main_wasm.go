//go:build js && wasm

// Command playground is the WASM backend for the promptr web playground. It
// exposes two functions to JavaScript:
//
//	promptrGenerate(src)  -> { go, err }   compile .promptr source to Go
//	promptrParse(raw)     -> { json, err } run the tolerant coerce parser
//
// Both run fully client-side — no API calls — so the page is a self-contained
// demo of the compiler and the schema-aligned parser kernel.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/zkrebbekx/promptr/codegen"
	"github.com/zkrebbekx/promptr/coerce"
	"github.com/zkrebbekx/promptr/dsl"
)

func generate(_ js.Value, args []js.Value) any {
	res := map[string]any{"go": "", "err": ""}
	if len(args) < 1 {
		res["err"] = "no input"
		return js.ValueOf(res)
	}
	f, perr := dsl.Parse(args[0].String())
	out, gerr := codegen.Generate("app", f)
	res["go"] = string(out)
	switch {
	case perr != nil:
		res["err"] = perr.Error()
	case gerr != nil:
		res["err"] = gerr.Error()
	}
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
	js.Global().Set("promptrParse", js.FuncOf(parse))
	select {}
}
