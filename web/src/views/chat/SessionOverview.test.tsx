// @vitest-environment jsdom
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { emptyTaskIndex } from "../../lib/tasks";
import type { Item, Session } from "../../lib/types";
import { SessionOverview } from "./SessionOverview";

const emptyTasks = emptyTaskIndex();

function renderOverview(session: Session, items: Item[] = []): string {
  return renderToStaticMarkup(
    <SessionOverview
      id="ov-test"
      session={session}
      machineName="devbox"
      items={items}
      tasks={emptyTasks}
      children={[]}
    />,
  );
}

describe("SessionOverview harness parity", () => {
  it("renders unified runtime, effort, environment, and context breakdown for TRAE CLI", () => {
    const html = renderOverview({
      machine_id: "m1",
      session_id: "traex-1",
      title: "TRAE session",
      cwd: "/work/project",
      repo_root: "/work/project",
      branch: "deploy",
      status: "idle",
      agent: "traex",
      launcher: "trae-custom",
      model: "relay-pro-max",
      effort: "xhigh",
      context_tokens: 340_000,
      context_window: 950_000,
      context_breakdown: {
        source: "estimated",
        system_and_rules: 80_000,
        conversation: 120_000,
        tool_outputs: 140_000,
        free: 610_000,
        auto_compact_threshold: 909_040,
      },
    });
    expect(html).toContain("data-overview-runtime");
    expect(html).toContain('data-overview-effort="xhigh"');
    expect(html).toContain("relay-pro-max");
    expect(html).toContain("deploy");
    expect(html).toContain("36%");
    expect(html).toContain("data-insights-breakdown");
    expect(html).toContain("data-breakdown-compact-marker");
  });

  it("renders unified runtime and context breakdown for Antigravity", () => {
    const html = renderOverview({
      machine_id: "m1",
      session_id: "agy-1",
      title: "Agy session",
      cwd: "/work/devbox",
      status: "idle",
      agent: "agy",
      launcher: "agy",
      model: "gemini-4-neon-xhigh",
      effort: "xhigh",
      context_tokens: 470_000,
      context_window: 1_000_000,
      context_breakdown: {
        source: "estimated",
        system_and_rules: 90_000,
        conversation: 160_000,
        tool_outputs: 220_000,
        free: 530_000,
      },
    });
    expect(html).toContain("data-overview-runtime");
    expect(html).toContain("47%");
    expect(html).toContain("data-insights-breakdown");
    expect(html).toContain('data-breakdown-row="tool_outputs"');
  });

  it("degrades honestly to token counts when context_window is unknown", () => {
    const html = renderOverview(
      {
        machine_id: "m1",
        session_id: "agy-unknown",
        title: "Unknown window",
        cwd: "/work/devbox",
        status: "idle",
        agent: "agy",
        model: "opaque-model",
        context_tokens: 42_000,
      },
      [{ id: "a1", seq: 1, role: "assistant", kind: "text", usage: [{ in: 42_000, out: 5_200 }] }],
    );
    expect(html).toContain("data-overview-context");
    expect(html).toContain("42K");
    expect(html).not.toContain('role="progressbar"');
  });
});
