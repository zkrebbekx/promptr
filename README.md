# promptr

[![Go Reference](https://pkg.go.dev/badge/github.com/zkrebbekx/promptr.svg)](https://pkg.go.dev/github.com/zkrebbekx/promptr)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Typed prompts for Go. `promptr` makes a language model's output **typed and
reliable** — the way [BAML](https://github.com/BoundaryML/baml) does, but as
idiomatic, dependency-free Go.

Its core is **schema-aligned parsing**: instead of forcing a model into a rigid
JSON mode (which empirically degrades answer quality), you let it write
naturally and a tolerant, schema-driven parser coerces the result into your Go
types — recovering from the prose preambles, Markdown fences, trailing commas,
single quotes, fuzzy enum spellings, and truncated objects that real model
output is full of.

> **Status:** the `coerce` kernel (below) is built and tested. The `.promptr`
> DSL + compiler — declare a typed LLM function in a schema file and generate
> the Go client — is in progress on top of it.

## `coerce` — schema-aligned parser (available now)

```go
import "github.com/zkrebbekx/promptr/coerce"

type Severity string
func (Severity) CoerceMembers() []string { return []string{"LOW", "HIGH", "CRITICAL"} }

type Ticket struct {
    Title    string   `json:"title"`
    Severity Severity `json:"severity"`
    Tags     []string `json:"tags"`
    DueDays  int      `json:"due_days"`
}

// modelText is whatever the model actually returned — fences, prose and all.
ticket, err := coerce.Into[Ticket](modelText)
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

It also handles nested structs, `*T` optionals, maps, snake/camel/case-insensitive
key matching, and a single value where a list was expected.

### Discriminated unions

The "classify into one of N shapes" case — a sealed interface of variants:

```go
type Action interface{ isAction() }
type Search   struct{ Query  string `json:"query"` }
type Escalate struct{ Team, Reason string }
func (Search) isAction()   {}
func (Escalate) isAction() {}

u := coerce.NewUnion(Search{}, Escalate{})
act, err := coerce.ResolveInto[Action](modelText, u) // best-fit by shape, or by a "type" discriminator
```

### Streaming

```go
for p := range coerce.Stream[Ticket](tokenChan) {
    render(p.Value)        // a progressively-completed Ticket
    if p.Complete { break }
}
```

## Install

```bash
go get github.com/zkrebbekx/promptr
```

Zero dependencies. Nothing you import pulls in an LLM SDK — `coerce` works on
the structs you already have, and the (upcoming) generated clients call a small
`Provider` interface you wire to the model of your choice.

## Develop

```sh
make test     # go test ./...
make race     # -race
make fuzz     # fuzz the tolerant parser
make lint
```

## License

MIT
