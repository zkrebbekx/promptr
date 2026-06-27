# promptr

[![CI](https://github.com/zkrebbekx/promptr/actions/workflows/ci.yml/badge.svg)](https://github.com/zkrebbekx/promptr/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/zkrebbekx/promptr.svg)](https://pkg.go.dev/github.com/zkrebbekx/promptr)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Typed prompts for Go. `promptr` makes a language model's output **typed and
reliable** — the way [BAML](https://github.com/BoundaryML/baml) does, but as
idiomatic, dependency-free Go.

Declare your prompts and their input/output types in a small `.promptr` schema;
`promptr` compiles it to ordinary Go functions you call. Each generated function
renders the prompt, calls a model through a tiny `Provider` interface **you**
wire up (no vendor SDK in the core), and turns the loose reply into your typed
value — retrying with the parse error fed back if the first attempt won't fit.

Its core is **schema-aligned parsing**: instead of forcing a model into a rigid
JSON mode (which empirically degrades answer quality), you let it write
naturally and a tolerant, schema-driven parser coerces the result into your Go
types — recovering from prose preambles, Markdown fences, trailing commas,
single quotes, fuzzy enum spellings, and truncated objects that real model
output is full of.

```
.promptr schema ──promptr generate──▶ typed Go function
                                            │ renders prompt (schema baked in)
                                            ▼
                                    your Provider (any model)
                                            │ loose text reply
                                            ▼
                            coerce ──▶ typed value  (retry on misfit)
```

## The pipeline in one example

```promptr
// ticket.promptr
enum Severity { LOW HIGH CRITICAL }

class Ticket {
  title    string
  severity Severity
  tags     string[]
  due_days int?
}

client GPT4o { provider "openai" model "gpt-4o" }

function ExtractTicket(text: string) -> Ticket {
  client GPT4o
  prompt #"
    Extract a support ticket from the message.
    {{ ctx.output_schema }}      // compiler injects a schema description of Ticket
    Message: {{ text }}
  "#
}
```

```sh
promptr generate ./...     # -> ticket.promptr.go
```

```go
ticket, err := ExtractTicket(ctx, provider, "my server is down!!")
// ticket.Severity == SeverityCRITICAL, even if the model wrote "critical priority"
```

The generated function bakes a human-readable schema of `Ticket` into the prompt
(BAML's "show the model the shape you want" trick), then coerces the reply and
re-asks on a parse miss — all in a few lines you can read.

## `coerce` — the schema-aligned parser (usable on its own)

Don't want the DSL? Use the kernel directly on the structs you already have:

```go
import "github.com/zkrebbekx/promptr/coerce"

ticket, err := coerce.Into[Ticket](modelText) // modelText: fences, prose and all
```

`coerce.Into[T]` digests input that `encoding/json` would reject outright:

| the model emitted | `encoding/json` | `coerce` |
| --- | --- | --- |
| ` ```json { ... } ``` ` wrapped in prose | ✗ | ✓ extracts the payload |
| `{ title: 'x', tags: [1,2,], }` (unquoted key, single quotes, trailing comma) | ✗ | ✓ |
| `{"due_days": "7"}` into an `int` field | ✗ | ✓ coerces `"7"` → `7` |
| `{"amount": "$1,200.50"}` into a `float64` | ✗ | ✓ → `1200.5` |
| `{"severity": "high priority"}` into an enum | ✗ | ✓ fuzzy-matches → `HIGH` |
| `{"title": "x", "tags": ["a",` (truncated) | ✗ | ✓ recovers what parsed |

Nested structs, `*T` optionals, maps, snake/camel/case-insensitive keys, and a
single value where a list was expected are all handled. Bare prose where a
struct was expected returns a `*coerce.Error` — the signal the runtime
retries on.

### Discriminated unions

```go
u := coerce.NewUnion(Search{}, Escalate{})
act, err := coerce.ResolveInto[Action](modelText, u) // best-fit by shape or "type" discriminator
```

### Streaming

```go
for p := range coerce.Stream[Ticket](tokenChan) {
    render(p.Value)        // a progressively-completed Ticket
    if p.Complete { break }
}
```

## Prompt templates

Prompts are more than string interpolation — the template engine supports
control flow over your runtime values, so one function adapts its prompt to the
input:

```promptr
prompt #"
  Extract a support ticket.
  {{ if examples }}Here are examples of good tickets:
  {{ for e in examples }}- {{ e }}
  {{ end }}{{ end }}
  {{ ctx.output_schema }}
  Message: {{ text }}
"#
```

Supported inside `{{ }}`: `{{ var }}` (with dotted paths `{{ user.name }}`),
`{{ if cond }}…{{ else }}…{{ end }}` (truthiness, `not`, and `== "lit"` /
`!= "lit"`), `{{ for x in items }}…{{ end }}`, and the compiler-injected
`{{ ctx.output_schema }}`. Unknown names render empty rather than erroring, so a
prompt never panics on real model context. The engine (`promptr.Render`) is
usable directly, too.

## Providers

The core imports no LLM SDK. A `Provider` is one method:

```go
type Provider interface {
    Complete(ctx context.Context, messages []Message) (string, error)
}
```

| Package | Backend |
| --- | --- |
| `providers/openai` | OpenAI Chat Completions — **and anything compatible**: Azure OpenAI, Groq, Together, OpenRouter, llama.cpp/vLLM/LM Studio (just set `BaseURL`) |
| `providers/anthropic` | Anthropic Messages API |
| `providers/gemini` | Google Gemini (Generative Language API) |
| `providers/ollama` | Local models via Ollama |
| `providers/fake` | Deterministic scripted replies for tests and the playground |

Each is `net/http` only — import just the one you use. Wiring any other model is
a dozen lines.

### Client reliability policies

Declare retry/fallback/round-robin in the DSL; the compiler generates
registry-resolving constructors that wrap your wired providers:

```promptr
client Fast  { provider "openai"    model "gpt-4o-mini" }
client Smart { provider "anthropic" model "claude-opus-4-8" }
client Reliable {
  fallback [Smart, Fast]   // try Smart, fall over to Fast
  retry 3                  // each up to 3 times on transient error
}
```

```go
reg := promptr.Registry{"Smart": anthropicClient, "Fast": openaiClient}
provider := ClientReliable(reg) // promptr.Retry(promptr.Fallback(...), 3, 0)
ticket, err := ExtractTicket(ctx, provider, msg)
```

`promptr.Retry`, `promptr.Fallback`, and `promptr.RoundRobin` are also usable
directly — each is just a `Provider` that wraps other `Provider`s.

## Install

```bash
# library
go get github.com/zkrebbekx/promptr

# CLI compiler
go install github.com/zkrebbekx/promptr/cmd/promptr@latest
```

Also published as a container image (`ghcr.io/zkrebbekx/promptr`) and a Homebrew
cask.

## Playground

A WebAssembly playground (DSL → Go, and paste-messy-output → repaired value)
runs entirely client-side: `playground/`, deployed to GitHub Pages.

## Develop

```sh
make test     # go test ./...
make race     # -race
make fuzz     # fuzz the tolerant parser
make lint
go generate ./...   # regenerate examples
```

## License

MIT
