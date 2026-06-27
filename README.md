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

## Type system

Beyond classes and enums, the schema language expresses the shapes real LLM
tasks need — and each maps to idiomatic Go the `coerce` kernel already parses.

**Unions** — classify output into one of several typed shapes. Compiles to a
sealed interface (sumx-style) plus a `coerce` resolver that picks the best-fit
variant (by shape, or an explicit `type`/`kind` discriminator):

```promptr
class Search   { query string }
class Escalate { reason string }
union Action = Search | Escalate          // or inline:  -> Search | Escalate

function Route(message: string) -> Action {
  client Default
  prompt #"Search or escalate? {{ ctx.output_schema }} Message: {{ message }}"#
}
```

```go
act, err := Route(ctx, p, "I want a refund NOW")
switch a := act.(type) {            // exhaustive, type-safe
case Escalate: alertHuman(a.Reason)
case Search:   run(a.Query)
}
```

**Maps** — `map<string, int>` → `map[string]int`.

**Field attributes** tune the schema shown to the model (better prompt ⇒ better
parse), the BAML "symbol tuning" idea:

```promptr
class Profile {
  name  string @description("the person's full legal name") @alias("full_name")
  score int
}
```

`@description` annotates the field in the baked schema; `@alias` renames it on
the wire — the model is shown `full_name`, and `coerce` binds that back to
`Name`.

## Validation — `@assert` & `@check`

Coercion shapes a reply into your type; validation enforces what the *values*
must be. Two field attributes compile to [valx](https://github.com/zkrebbekx/valx)
rules the runtime applies after coercion:

```promptr
class Account {
  email    string @assert("required")
  username string @assert("min=3,max=20")
  age      int    @assert("gt=0,lt=130") @check("min=18")
  seats    int    @check("min=1,max=100")
}
```

- **`@assert`** is *hard*. A violation is fed back to the model as a repair
  re-ask — exactly like a parse failure — so it self-corrects within
  `Attempts`. If it never satisfies the rules, the call returns the validation
  error.
- **`@check`** is *soft*. Violations never block the value; they're delivered to
  an `OnCheck` sink so you can log or meter them while still using the result.

Generated functions take a trailing `...promptr.Option`, so checks (and retry
budgets, a system preamble, …) are opt-in without the signature changing:

```go
acc, err := ExtractAccount(ctx, p, msg,
    promptr.OnCheck(func(e error) { log.Println("soft:", e) }),
    promptr.WithAttempts(3),
)
```

The validator lives in *generated* code, so the core packages stay
zero-dependency — importing `promptr` pulls in nothing extra. See
[`examples/validate`](examples/validate).

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

## Streaming & multimodal

Mark a function `-> stream T` and the generated function returns a channel of
**progressively-completed** `T` values — the schema-aligned parser coerces each
growing prefix, so you can render a partial object while tokens are still
arriving:

```promptr
function SummarizeArticle(article: string) -> stream Summary {
  client Default
  prompt #"Summarize it. {{ ctx.output_schema }} Article: {{ article }}"#
}
```

```go
ch, err := SummarizeArticle(ctx, provider, text)
for part := range ch {           // promptr.Partial[Summary]
    if part.Err != nil { break }
    render(part.Value)           // headline fills in before the bullets do
    if part.Complete { break }
}
```

Any provider implementing the optional `StreamProvider` (`openai`, `anthropic`,
`fake`) streams real tokens via SSE; others transparently fall back to a single
complete value. `promptr.ExtractStream[T]` is usable directly, too.

**Multimodal inputs.** Give a parameter the type `image`, `audio`, `pdf` or
`file` and it becomes a `promptr.Part` attached to the user message (not
templated into the prompt text):

```promptr
function CaptionImage(photo: image, hint: string) -> Summary { … }
```

```go
cap, err := CaptionImage(ctx, provider, promptr.ImagePart("image/png", bytes), "be terse")
```

Providers map parts to their native content arrays (OpenAI `image_url`,
Anthropic `image` blocks; inline bytes are base64 data-URLs, or pass a URL with
`promptr.ImageURL`). See `examples/stream`.

## Tool-calling & agents

Declare `tool`s and hand them to a `function` with `tools [...]`. promptr runs
the **bounded model → tool → model loop** for you and still returns a typed Go
value at the end — single-shot extraction becomes a typed agent without giving
up type safety:

```promptr
tool GetWeather(city: string) -> Weather {
  description "Look up the current weather for a city."
}
tool SearchFlights(from: string, to: string) -> Flight[] {
  description "Find available flights between two cities."
}

function PlanTrip(goal: string) -> Itinerary {
  client Smart
  tools [GetWeather, SearchFlights]
  prompt #"Plan a trip for this goal. {{ ctx.output_schema }} Goal: {{ goal }}"#
}
```

The generated function takes a **typed handlers struct** — one func per tool,
its argument a generated `<Tool>Args` struct coerced from the model's JSON:

```go
itin, err := PlanTrip(ctx, provider, "see the northern lights", PlanTripTools{
    GetWeather: func(ctx context.Context, a GetWeatherArgs) (Weather, error) {
        return lookupWeather(a.City)
    },
    SearchFlights: func(ctx context.Context, a SearchFlightsArgs) ([]Flight, error) {
        return searchFlights(a.From, a.To)
    },
})
```

The loop dispatches each requested tool, feeds the result back, and repeats up
to `Options.MaxSteps` (default 8) until the model answers — coerced into
`Itinerary`. Unknown-tool and handler-error turns are fed back as text so the
model can recover rather than aborting. `promptr.RunTools[T]` is usable directly,
too. Works on providers implementing the optional `ToolProvider` interface
(`openai`, `anthropic`, `fake`); others return a clear "does not support tool
calls" error. See `examples/agent`.

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
| `providers/recorded` | Replays hand-authored JSON cassettes — a VCR for deterministic, offline tests |

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

## Observability

A `Middleware` is just a `Provider`-to-`Provider` function, so you can wrap any
provider without touching generated code. The built-in `Collector` records
latency and token usage per call (exact when a provider implements
`UsageReporter`, otherwise a `chars/4` estimate):

```go
col := &promptr.Collector{}
p := promptr.Chain(openaiClient, col.Collect) // outermost-first
_, _ = ExtractTicket(ctx, p, msg)

s := col.Stats()
log.Printf("%d calls, %d tokens, %s avg", s.Calls, s.TotalTokens(), s.AvgLatency())
```

**Hooks (streaming + tool calls too).** `Middleware`/`Chain` wrap only
`Complete`, so wrapping a streaming or tool-calling provider hides those
capabilities. For observability across **all** paths use `WithHooks`, which is
capability-preserving — the wrapped provider still streams and runs tools — and
fires a `Hook` before and after every `Complete`, `Stream` and `CompleteTools`:

```go
type Hook interface {
    BeforeCall(ctx context.Context, info CallInfo) AfterFunc // AfterFunc(Outcome)
}

col := &promptr.Collector{}
p := promptr.WithHooks(openaiClient,
    col.Hook(),                       // latency + tokens on every path
    promptr.LogHook(slog.Default()),  // structured logs, zero deps
)
itin, _ := PlanTrip(ctx, p, goal, handlers) // tool calls still work, now observed
```

A `Hook` is the whole extension surface: an OpenTelemetry span exporter is a
~20-line `Hook` that opens a span in `BeforeCall` and ends it in the returned
`AfterFunc`, kept out of core so it stays dependency-free. `LogHook` (built on
stdlib `log/slog`) is the reference implementation.

## Tooling & editor support

```sh
promptr generate ./...   # compile .promptr -> Go (run under //go:generate)
promptr check ./...       # parse + validate without writing Go (CI-friendly)
```

`promptr check` reports unresolved types/clients, malformed unions and `test`
blocks whose args don't match their function — the same checks the language
server surfaces in your editor. Install `cmd/promptr-lsp` for live diagnostics
and see [`editor/`](editor/) for the tree-sitter grammar and editor wiring.

> Deferred for a focused follow-up: `promptr fmt` (a canonical formatter needs
> the lexer to retain comments, currently dropped as trivia) and a
> live-execution `test` runner with typed assertions (best done by emitting Go
> tests from `test` blocks, which deserves its own provider-wiring design).
> `providers/recorded` is the deterministic substrate both will build on.

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
