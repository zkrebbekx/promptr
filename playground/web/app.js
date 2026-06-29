"use strict";

// VERSION is the promptr release this playground is built from. The WASM is
// compiled against the tagged source, so bump this whenever a new version ships.
const VERSION = "v0.15.1";

// EXAMPLES is the clickable gallery. Each entry is a self-contained .promptr
// snippet that showcases one capability; clicking a chip loads it into the DSL
// pane and recompiles. Every snippet must parse + generate cleanly.
const EXAMPLES = [
  {
    id: "extract",
    label: "Typed extraction",
    blurb: "The basics: an enum + class become a typed Go function whose output schema is baked into the prompt. The right pane shows the messy reply a model gives for this schema — and the parser repairing it.",
    messy: `Sure! Here's the ticket:

\`\`\`json
{
  title: 'Server is down',
  severity: 'critical priority',
  tags: ['outage', 'prod',],
  due_days: "1"
}
\`\`\`
Hope that helps!`,
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
    blurb: "A union return compiles to a sealed interface; @description / @alias tune the schema and wire names; map<string,V> is supported. The right pane repairs a model's Search reply (note the max_results alias). (v0.3)",
    messy: `I'll search for that.

{
  "query": "wireless headphones under $100",
  "max_results": "5",
}`,
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
    blurb: "-> stream T compiles to a function returning a channel of progressively-coerced partial values. The right pane shows a full Summary reply repaired into clean JSON. (v0.4)",
    messy: `Here's the summary:
\`\`\`json
{
  headline: "Quarterly revenue beats estimates",
  bullets: [
    'Revenue up 12% YoY',
    'Cloud segment led growth',
  ]
}
\`\`\``,
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
    blurb: "@assert rules are hard (a violation is fed back to the model as a repair re-ask); @check rules are soft (surfaced to a sink). Both compile to valx tags. The right pane repairs a raw Account reply. (v0.8)",
    messy: `{
  "email": "ada@example.com",
  "username": "ada",
  "age": "31",
  "plan": "enterprise tier",
  "seats": "12"
}`,
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
    id: "fuzzy",
    label: "Fuzzy field names",
    blurb: "Field matching is case- and separator-insensitive. The model emits UserName, Email-Addr, isActive, LoginCount — and the parser snaps each onto the schema's snake_case fields, coercing the loose scalars to their declared types. Edit the keys on the right to watch it hold.",
    messy: `Here's the profile:

\`\`\`json
{
  "UserName": "ada",
  "Email-Addr": "ada@example.com",
  "isActive": true,
  "LoginCount": "42",
}
\`\`\``,
    dsl: `class Profile {
  user_name   string
  email_addr  string
  is_active   bool
  login_count int
}

client Default {
  provider "fake"
  model    "scripted"
}

function ExtractProfile(text: string) -> Profile {
  client Default
  prompt #"
    Extract a user profile from the message.
    {{ ctx.output_schema }}
    Message: {{ text }}
  "#
}`,
  },
  {
    id: "tools",
    label: "Tool-calling agent",
    blurb: "tools [...] turns a function into a bounded agent loop: the model calls typed Go tools, results feed back, and you still get a typed value. The right pane repairs the loop's final Itinerary answer. (v0.6)",
    messy: `Done planning! Here's the itinerary:

\`\`\`json
{
  destination: 'Reykjavik',
  summary: "chase the aurora",
  packing: ['thermal layers', 'camera',]
}
\`\`\``,
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
    id: "multiagent",
    label: "Multi-agent",
    blurb: "List a function in another function's tools and it becomes a self-contained sub-agent — auto-wired, no handler. The orchestrator (WriteBrief) takes no handlers struct; its loop calls ResearchTopic directly. The right pane repairs the coordinator's final Brief. (v0.14)",
    messy: `Based on the research, here's the brief:
{
  "topic": "tidal energy",
  "recommendation": 'pilot a 2MW array',
}`,
    dsl: `class Research {
  summary string
  sources string[]
}

class Brief {
  topic          string
  recommendation string
}

client Default {
  provider "fake"
  model    "scripted"
}

function ResearchTopic(topic: string) -> Research {
  client Default
  description "Research a topic and return a summary with sources."
  prompt #"
    Research the topic and summarize what you find.
    {{ ctx.output_schema }}
    Topic: {{ topic }}
  "#
}

function WriteBrief(request: string) -> Brief {
  client Default
  tools [ResearchTopic]
  prompt #"
    Write a decision brief. Use the ResearchTopic sub-agent for background.
    {{ ctx.output_schema }}
    Request: {{ request }}
  "#
}`,
  },
  {
    id: "livetest",
    label: "Live tests",
    blurb: "A test block compiles to a runnable Go test that calls the function and asserts the typed result field-by-field — see the appended _test.go below. The right pane shows the messy reply those assertions repair + check. (v0.13)",
    messy: `{
  title: "Server is down",
  severity: 'CRITICAL',
  open: "true",
  votes: "3",
}`,
    dsl: `enum Severity { LOW HIGH CRITICAL }

class Ticket {
  title    string
  severity Severity
  open     bool
  votes    int
}

client Default {
  provider "fake"
  model    "scripted"
}

function ExtractTicket(text: string) -> Ticket {
  client Default
  prompt #"
    Extract a support ticket from the message.
    {{ ctx.output_schema }}
    Message: {{ text }}
  "#
}

test outage {
  function ExtractTicket
  args {
    text "the production server is DOWN!"
  }
  expect {
    title    "Server is down"
    severity CRITICAL
    open     true
    votes    3
  }
}`,
  },
  {
    id: "fmt",
    label: "Formatter (try it!)",
    blurb: "promptr fmt canonicalizes layout and preserves comments. This snippet is deliberately mis-aligned — hit Format to clean it up. The right pane repairs a raw Account reply for this schema. (v0.12)",
    messy: `{
  email: 'ada@example.com',
  username: "ada",
  plan: 'high',
}`,
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
  if (r.err) { out.textContent = r.err; out.classList.add("error"); return; }
  // A `test` block also compiles to a sibling _test.go — append it so the
  // live-test runner is visible alongside the main generated file.
  let text = r.go;
  if (r.tests) {
    text += "\n// ── generated _test.go (from your test blocks) ──\n\n" + r.tests;
  }
  out.textContent = text;
  out.classList.remove("error");
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
  // Pass the schema (left pane) too: the parser coerces the messy reply into the
  // schema's return class, so differently-cased / -separated keys snap onto the
  // declared fields and loose scalars take their declared types.
  const r = window.promptrParse($("messy").value, $("dsl").value);
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
  // Each example is one coherent story: the left pane compiles the schema, the
  // right pane repairs the messy reply a model gives for that same schema.
  if (ex.messy) {
    $("messy").value = ex.messy;
  }
  for (const chip of document.querySelectorAll(".chip")) {
    chip.classList.toggle("active", chip.dataset.id === ex.id);
  }
  runGenerate();
  runParse();
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
  // Editing the schema recompiles *and* re-aligns the parse, since the schema is
  // now the target the right pane coerces into.
  $("dsl").addEventListener("input", debounce(() => { runGenerate(); runParse(); }, 150));
  $("messy").addEventListener("input", debounce(runParse, 150));
  $("fmt-btn").addEventListener("click", runFormat);
  // loadExample seeds both panes (schema + matching messy reply) and runs both.
  loadExample(EXAMPLES[0]);
  $("status").textContent = "ready · promptr " + VERSION + " · WebAssembly";
}

const go = new Go();
WebAssembly.instantiateStreaming(fetch("main.wasm"), go.importObject)
  .then((result) => { go.run(result.instance); boot(); })
  .catch((err) => { $("status").textContent = "failed to load WASM: " + err; });
