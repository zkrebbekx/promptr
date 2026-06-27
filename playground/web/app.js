"use strict";

// VERSION is the promptr release this playground is built from. The WASM is
// compiled against the tagged source, so bump this whenever a new version ships.
const VERSION = "v0.12.0";

// EXAMPLES is the clickable gallery. Each entry is a self-contained .promptr
// snippet that showcases one capability; clicking a chip loads it into the DSL
// pane and recompiles. Every snippet must parse + generate cleanly.
const EXAMPLES = [
  {
    id: "extract",
    label: "Typed extraction",
    blurb: "The basics: an enum + class become a typed Go function whose output schema is baked into the prompt.",
    dsl: `enum Severity { LOW HIGH CRITICAL }

class Ticket {
  title    string
  severity Severity
  tags     string[]
  due_days int?
}

client GPT4o {
  provider "openai"
  model    "gpt-4o"
}

function ExtractTicket(text: string) -> Ticket {
  client GPT4o
  prompt #"
    Extract a support ticket from the message.
    {{ ctx.output_schema }}
    Message: {{ text }}
  "#
}`,
  },
  {
    id: "union",
    label: "Unions & attributes",
    blurb: "A union return compiles to a sealed interface; @description / @alias tune the schema and wire names; map<string,V> is supported. (v0.3)",
    dsl: `class Search {
  query string @description("the search terms to look up")
  topk  int    @alias("max_results")
}

class Escalate {
  reason   string @description("why this needs a human")
  metadata map<string, string>
}

union Action = Search | Escalate

client Default {
  provider "fake"
  model    "scripted"
}

function Route(message: string) -> Action {
  client Default
  prompt #"
    Decide how to handle the user's message: Search or Escalate.
    {{ ctx.output_schema }}
    Message: {{ message }}
  "#
}`,
  },
  {
    id: "stream",
    label: "Streaming",
    blurb: "-> stream T compiles to a function returning a channel of progressively-coerced partial values. (v0.4)",
    dsl: `class Summary {
  headline string @description("a one-line title")
  bullets  string[]
}

client Default {
  provider "fake"
  model    "scripted"
}

function SummarizeArticle(article: string) -> stream Summary {
  client Default
  prompt #"
    Summarize the article as a headline plus bullet points.
    {{ ctx.output_schema }}
    Article: {{ article }}
  "#
}`,
  },
  {
    id: "validate",
    label: "Validation",
    blurb: "@assert rules are hard (a violation is fed back to the model as a repair re-ask); @check rules are soft (surfaced to a sink). Both compile to valx tags. (v0.8)",
    dsl: `enum Plan { FREE PRO ENTERPRISE }

class Account {
  email    string @assert("required")
  username string @assert("min=3,max=20")
  age      int    @assert("gt=0,lt=130") @check("min=18")
  plan     Plan
  seats    int    @check("min=1,max=100")
}

client Default {
  provider "fake"
  model    "scripted"
}

function ExtractAccount(text: string) -> Account {
  client Default
  prompt #"
    Extract an account from the user's message.
    {{ ctx.output_schema }}
    Message: {{ text }}
  "#
}`,
  },
  {
    id: "tools",
    label: "Tool-calling agent",
    blurb: "tools [...] turns a function into a bounded agent loop: the model calls typed Go tools, results feed back, and you still get a typed value. (v0.6)",
    dsl: `class Weather {
  city       string
  conditions string
  high_c     int
}

class Flight {
  carrier string
  price   int
}

class Itinerary {
  destination string
  summary     string
  packing     string[]
}

client Smart {
  provider "anthropic"
  model    "claude-opus-4-8"
}

tool GetWeather(city: string) -> Weather {
  description "Look up the current weather for a city."
}

tool SearchFlights(from: string, to: string) -> Flight[] {
  description "Find available flights between two cities."
}

function PlanTrip(goal: string) -> Itinerary {
  client Smart
  tools [GetWeather, SearchFlights]
  prompt #"
    Plan a trip for this goal, using the tools to check weather and flights.
    {{ ctx.output_schema }}
    Goal: {{ goal }}
  "#
}`,
  },
  {
    id: "fmt",
    label: "Formatter (try it!)",
    blurb: "promptr fmt canonicalizes layout and preserves comments. This snippet is deliberately mis-aligned — hit Format to clean it up. (v0.12)",
    dsl: `// a deliberately messy schema — click "Format" to canonicalize it
enum   Sev {  LOW    HIGH }

class Account {
  email string    @assert("required")
      username   string @assert("min=3")
  // the billing plan
  plan Sev
}
client   Default {
  provider "fake"
    model "scripted"
}`,
  },
];

const $ = (id) => document.getElementById(id);

function debounce(fn, ms) {
  let t;
  return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); };
}

function runGenerate() {
  if (!window.promptrGenerate) return;
  const r = window.promptrGenerate($("dsl").value);
  const out = $("go");
  if (r.err) { out.textContent = r.err; out.classList.add("error"); }
  else { out.textContent = r.go; out.classList.remove("error"); }
}

function runFormat() {
  if (!window.promptrFormat) return;
  const r = window.promptrFormat($("dsl").value);
  if (r.err) {
    const out = $("go");
    out.textContent = "format error: " + r.err;
    out.classList.add("error");
    return;
  }
  $("dsl").value = r.src;
  runGenerate();
  flash($("fmt-btn"), "formatted ✓");
}

function runParse() {
  if (!window.promptrParse) return;
  const r = window.promptrParse($("messy").value);
  const out = $("json");
  out.textContent = r.err ? r.err + "\n\n" + r.json : r.json;
  out.classList.toggle("error", !!r.err);
}

function flash(btn, msg) {
  if (!btn) return;
  const orig = btn.textContent;
  btn.textContent = msg;
  setTimeout(() => { btn.textContent = orig; }, 1200);
}

function loadExample(ex) {
  $("dsl").value = ex.dsl;
  $("blurb").textContent = ex.blurb;
  for (const chip of document.querySelectorAll(".chip")) {
    chip.classList.toggle("active", chip.dataset.id === ex.id);
  }
  runGenerate();
}

function buildGallery() {
  const bar = $("examples");
  for (const ex of EXAMPLES) {
    const b = document.createElement("button");
    b.className = "chip";
    b.textContent = ex.label;
    b.dataset.id = ex.id;
    b.addEventListener("click", () => loadExample(ex));
    bar.appendChild(b);
  }
}

function boot() {
  buildGallery();
  loadExample(EXAMPLES[0]);
  $("messy").value = sampleMessy;
  $("dsl").addEventListener("input", debounce(runGenerate, 150));
  $("messy").addEventListener("input", debounce(runParse, 150));
  $("fmt-btn").addEventListener("click", runFormat);
  runParse();
  $("status").textContent = "ready · promptr " + VERSION + " · WebAssembly";
}

const sampleMessy = `Sure! Here's the ticket:

\`\`\`json
{
  title: 'Server is down',
  severity: 'critical priority',
  tags: ['outage', 'prod',],
  due_days: "1"
}
\`\`\`
Hope that helps!`;

const go = new Go();
WebAssembly.instantiateStreaming(fetch("main.wasm"), go.importObject)
  .then((result) => { go.run(result.instance); boot(); })
  .catch((err) => { $("status").textContent = "failed to load WASM: " + err; });
