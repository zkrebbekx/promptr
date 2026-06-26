"use strict";

const sampleDSL = `enum Severity { LOW HIGH CRITICAL }

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
}`;

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

function runParse() {
  if (!window.promptrParse) return;
  const r = window.promptrParse($("messy").value);
  const out = $("json");
  out.textContent = r.err ? r.err + "\n\n" + r.json : r.json;
  out.classList.toggle("error", !!r.err);
}

function boot() {
  $("dsl").value = sampleDSL;
  $("messy").value = sampleMessy;
  $("dsl").addEventListener("input", debounce(runGenerate, 150));
  $("messy").addEventListener("input", debounce(runParse, 150));
  runGenerate();
  runParse();
  $("status").textContent = "ready · promptr playground (WebAssembly)";
}

const go = new Go();
WebAssembly.instantiateStreaming(fetch("main.wasm"), go.importObject)
  .then((result) => { go.run(result.instance); boot(); })
  .catch((err) => { $("status").textContent = "failed to load WASM: " + err; });
