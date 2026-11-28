// Exercise typed turns against a mock hub at phone and desktop widths.
// Usage: node web/e2e/turn-model-outcomes.mjs <web-origin> [chromium-executable]
import assert from "node:assert/strict";
import { chromium } from "playwright-core";

const [origin, executablePath] = process.argv.slice(2);
if (!origin) throw new Error("A web preview origin is required");
const base = new URL(origin);
const browser = await chromium.launch(executablePath ? { executablePath } : {});
const at = (seq) => new Date(Date.UTC(2026, 0, 1, 0, 0, seq)).toISOString();
const entries = [
  { id: "read", title: "Review the input", status: "completed" },
  { id: "test", title: "Run the checks", status: "in_progress" },
];
function timeline(typed) {
  const plan = { mode: "replace", entries };
  const execute = { kind: "execute", name: "Bash", title: "Run the checks" };
  const withFacets = (it, facets) => (typed ? { ...it, facets: { turn_id: "turn-1", ...facets } } : it);
  const core = [
    withFacets({ id: "u1", seq: 1, ts: at(1), role: "user", kind: "text", text: "Run the checks." }, { input: { via: "typed" } }),
    withFacets({ id: "b1", seq: 2, ts: at(2), role: "assistant", kind: "tool_use", tool_name: "Bash", tool_use_id: "check", tool_input: { command: "make check" } }, { tool: execute }),
    withFacets({ id: "r1", seq: 3, ts: at(3), role: "user", kind: "tool_result", tool_use_id: "check", text: "Checks passed.", outcome: { status: "ok", exit_code: 0, duration_ms: 125 } }, { tool: { ...execute, status: "completed" } }),
    withFacets({ id: "t1", seq: 4, ts: at(4), role: "assistant", kind: "tool_use", tool_name: "TodoWrite", tool_use_id: "plan", tool_input: { todos: entries.map((e) => ({ content: e.title, status: e.status })) } }, { tool: { kind: "plan", name: "TodoWrite" }, plan }),
    withFacets({ id: "p1", seq: 5, ts: at(5), role: "user", kind: "tool_result", tool_use_id: "plan", text: "Plan updated." }, { tool: { kind: "plan", name: "TodoWrite", status: "completed" }, plan }),
    withFacets({ id: "a1", seq: 6, ts: at(6), role: "assistant", kind: "text", text: "The checks passed." }, {}),
  ];
  if (!typed) return core;
  const call = (id, seq, name, input, facets) => withFacets({
    id, seq, ts: at(seq), role: "assistant", kind: "tool_use", tool_name: name, tool_use_id: id, tool_input: input,
  }, facets);
  const result = (id, seq, text, facets) => withFacets({
    id: id + "-result", seq, ts: at(seq), role: "user", kind: "tool_result", tool_use_id: id, text,
  }, facets);
  const question = { id: "question", kind: "question", status: "pending",
    questions: [{ question: "Where should this run?", options: [{ label: "Local" }, { label: "Remote" }] }] };
  const artifact = { id: "artifact", kind: "file", op: "created", title: "Check report", path: "report.md" };
  return [...core.slice(0, -1),
    call("child", 6, "Agent", { description: "Review the deployment" },
      { tool: { kind: "agent", name: "Agent" }, subagent: { id: "child-agent", relation: "spawn" } }),
    result("child", 7, "Deployment reviewed.", { subagent: { id: "child-agent", relation: "result", status: "completed" } }),
    call("question", 8, "AskUserQuestion", {}, { tool: { kind: "ask", name: "AskUserQuestion" }, interaction: question }),
    result("question", 9, "Answer recorded.", { interaction: { ...question, status: "answered", answers: { "Where should this run?": "Local" } } }),
    call("artifact", 10, "Artifact", {}, { tool: { kind: "artifact", name: "Artifact" } }),
    result("artifact", 11, "Report written.", { artifact }),
    { ...core.at(-1), seq: 12, ts: at(12) },
  ];
}

async function run(agent, width, theme, typed = true) {
  const id = `fixture-${agent}`;
  const timer = { id: "timer", kind: "timer", status: "scheduled",
    timer: { interval_s: 600, prompt: "Review the release", next_at: new Date(Date.now() + 600_000).toISOString() } };
  const session = { machine_id: "demo", session_id: id, agent, title: "Turn model test", state: "seen", status: "done",
    updated_at: at(6), capabilities: { read: true, send: true }, context_tokens: 250, context_window: 1000,
    ...(typed ? {
      facets: { timers: [timer], context: { used: 250, window: 1000, source: "reported" } },
      subagents: [{ id: "child-agent", session_id: "example-child", tool_use_id: "child", status: "completed" }],
      background: [{ id: "job", kind: "shell", description: "Build the release", started_at: at(3) }],
    } : {}) };
  const items = timeline(typed);
  const context = await browser.newContext({
    viewport: { width, height: 900 }, colorScheme: theme, serviceWorkers: "block", hasTouch: width === 390,
  });
  try {
    await context.addInitScript(({ theme }) => {
      localStorage.setItem("persona.token", "synthetic-token");
      localStorage.setItem("persona.theme", theme);
      localStorage.setItem("persona.language", "en");
    }, { theme });
    await context.route((url) => url.origin !== base.origin, (route) => route.abort());
    await context.route("**/api/v1/**", async (route) => {
      const url = new URL(route.request().url());
      const path = url.pathname.replace(/^.*\/api\/v1/, "");
      let data = {};
      if (path === "/machines") data = { machines: [{ id: "demo", name: "Example computer", online: true, os: "linux" }] };
      if (path === "/sessions") data = { sessions: [session] };
      if (path === "/assistant") data = { machine_id: "demo", main: "assistant", conversations: [], tasks: [], memory: [] };
      if (path === `/sessions/demo/${id}`) data = { session };
      if (path === `/sessions/demo/${id}/items`) {
        data = { items: url.searchParams.has("after_seq") || url.searchParams.has("before_seq") ? [] : items, has_more: false };
      }
      await route.fulfill({ json: data });
    });
    await context.routeWebSocket((url) => url.pathname.endsWith("/api/v1/ws"), (socket) => {
      socket.send(JSON.stringify({ type: "hello", server_version: "fixture", hub_name: "Mock hub", features: typed ? ["turn_model_v1"] : [] }));
      socket.onMessage((raw) => {
        if (JSON.parse(String(raw)).type === "app_ping") socket.send(JSON.stringify({ type: "pong" }));
      });
    });
    const page = await context.newPage();
    await page.goto(new URL(`/s/demo/${id}`, base).href);
    await page.getByText("The checks passed.", { exact: true }).waitFor({ timeout: 15_000 });
    const group = page.locator("[data-work-group] button[aria-expanded='false']").first();
    if (await group.count()) await group.click();
    const exit = page.locator("[data-tool-exit-code='0']");
    const duration = page.locator("[data-tool-duration]").filter({ hasText: "125 ms" });
    await exit.waitFor({ state: "visible", timeout: 10_000 });
    assert.equal(await duration.isVisible(), true);
    assert.equal(await page.locator("[data-tool-outcome='ok']").count(), 1);
    await page.locator("[data-plan-card]").first().waitFor({ state: "visible" });
    assert.match(await page.locator("[data-plan-card]").first().innerText(), /Run the checks/);
    if (typed) {
      await page.locator("[data-subagent-card='completed']").first().waitFor({ state: "visible" });
      await page.locator("[data-artifact-card='file']").getByText("Check report").waitFor({ state: "visible" });
      const ask = page.locator("[data-tool-use=question] > button").first();
      if (await ask.count()) await ask.click();
      await page.locator("[data-step-detail='ask-user']").getByText("Where should this run?").waitFor({ state: "visible" });
      await page.getByRole("button", { name: /25%.*75%/ }).first().waitFor({ state: "visible" });
      await page.locator("[data-loop-mark]").click();
      await page.locator("[data-bg-loop]").getByText("Review the release").waitFor({ state: "visible" });
      await page.getByText("Build the release", { exact: true }).waitFor({ state: "visible" });
    }
    console.log(`${agent} ${width}px ${theme} ${typed ? "typed" : "legacy"}: outcome, checklist${typed ? ", subagent, interaction, artifact, context and background" : ""} visible`);
  } finally {
    await context.close();
  }
}

try {
  for (const agent of process.env.PERSONA_FIXTURE_AGENT ? [process.env.PERSONA_FIXTURE_AGENT] : ["agy", "codex", "traex", "claude"]) {
    for (const width of [390, 1440]) {
      for (const theme of ["light", "dark"]) await run(agent, width, theme);
    }
  }
  await run("claude", 390, "light", false);
} finally {
  await browser.close();
}
