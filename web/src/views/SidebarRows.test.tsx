// The open conversation's row in the sidebar (#2283): marked for assistive tech, its own fill, its actions without hover.
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { t } from "../i18n";
import { stateName } from "../lib/state-labels";
import { Icon } from "../ui/icons";
import type { AgentSession } from "../lib/agents";
import type { Session } from "../lib/types";
import { setAgentDetails } from "../lib/context-usage";
import { AgentRow, PersonaRow, SESSION_ACTIVE_ROW, SessionRow } from "./Sidebar";

// the sidebar's modules read the colour scheme and this browser's settings when they load
vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

// the avatar reads the persona store, which has no server snapshot
vi.mock("./assistant/Persona", async (importOriginal) => ({ ...(await importOriginal<typeof import("./assistant/Persona")>()), PersonaAvatar: () => null }));

const session: Session = { machine_id: "mac", session_id: "s1", title: "整理文档", cwd: "/code", status: "idle", state: "idle" };
const rowProps = { s: session, k: "s:mac/s1", reg: () => () => {}, hover: () => {}, to: { name: "session", mid: "mac", sid: "s1" } as const, go: () => () => {}, onMenu: () => {} };
const link = (html: string) => html.match(/<a [^>]*>/)![0];
const more = (html: string) => html.match(/<button [^>]*aria-label="Agent 的操作"[^>]*>/)![0];

describe("an agent's row", () => {
  it("is aria-current with the open row's elevated card fill when it is open", () => {
    const html = renderToStaticMarkup(<SessionRow {...rowProps} active />);
    expect(link(html)).toContain('aria-current="page"');
    expect(link(html)).toContain(SESSION_ACTIVE_ROW);
    expect(link(html)).toContain("bg-float");
    expect(link(html)).toContain("shadow-float");
  });
  it("is neither when it is not", () => {
    const html = renderToStaticMarkup(<SessionRow {...rowProps} active={false} />);
    expect(link(html)).not.toContain("aria-current");
    expect(link(html)).not.toContain(SESSION_ACTIVE_ROW);
  });
  it("keeps its ⋯ shown when open, on hover only otherwise", () => {
    expect(more(renderToStaticMarkup(<SessionRow {...rowProps} active />))).toMatch(/class="[^"]*\bopacity-100\b/);
    expect(more(renderToStaticMarkup(<SessionRow {...rowProps} active={false} />))).toMatch(/class="[^"]*\bopacity-0\b/);
  });
  // the owner's report: the row under the pointer lost its "working" and the line under its title
  it("keeps its state and details in sight under the pointer, in the one-line and the two-line layout", () => {
    const working: Session = { ...session, status: "working", state: "working", updated_at: "2026-01-01T00:00:00Z", pane_id: "w1:p1" };
    // the wide layout's one line (state and age at the end of the title's line), and a contextual row, whose details
    // take a line under the title in every layout
    const oneLine = renderToStaticMarkup(<SessionRow {...rowProps} s={working} active={false} />);
    const twoLines = renderToStaticMarkup(<SessionRow {...rowProps} s={{ ...working, pane_id: undefined }} hint="Project" active={false} />);
    expect(link(oneLine)).not.toContain("data-sidebar-context");
    expect(link(twoLines)).toContain("data-sidebar-context");
    for (const html of [oneLine, twoLines]) {
      expect(html).toContain('data-state="working"');
      expect(html).not.toContain(`>${stateName("working")}<`);
      expect(html).toContain("data-row-age");
      expect(html).not.toContain("invisible"); // nothing on the row hides on hover or focus
      // the wide layout keeps the ⋯'s slot at the row's end on every row, hovered or not: the ⋯ covers no words
      expect(link(html)).toMatch(/class="[^"]*lg:\[@media\(hover:hover\)\]:pr-8/);
      expect(more(html)).toMatch(/\bright-1\b/);
      expect(more(html)).toMatch(/\bsize-6\b/);
    }
  });
  it("distinguishes working, background-waiting, needs-you, and quiet idle states cleanly via StatusMark without redundant text pills or row borders", () => {
    const workingHtml = renderToStaticMarkup(<SessionRow {...rowProps} s={{ ...session, status: "working", state: "working" }} active={false} />);
    expect(workingHtml).not.toContain(`>${stateName("working")}<`);
    expect(workingHtml).toContain('data-state="working"');
    expect(workingHtml).not.toContain("bg-orange-tint");
    expect(workingHtml).not.toContain("rounded-full");
    expect(workingHtml).not.toContain("ring-1");

    const waitingHtml = renderToStaticMarkup(<SessionRow {...rowProps} s={{ ...session, status: "working", state: "working", background_waiting: true }} active={false} />);
    expect(waitingHtml).not.toContain(`>${stateName("bg_waiting")}<`);
    expect(waitingHtml).toContain('data-state="bg_waiting"');
    expect(waitingHtml).not.toContain("text-accent-ink");
    expect(waitingHtml).not.toContain("bg-orange-tint");
    expect(waitingHtml).not.toContain("rounded-full");
    expect(waitingHtml).not.toContain("ring-1");

    const needsHtml = renderToStaticMarkup(<SessionRow {...rowProps} s={{ ...session, state: "needs_you" }} active={false} />);
    expect(needsHtml).toContain('data-state="needs_you"');
    expect(needsHtml).toContain("font-medium text-ink");
    expect(needsHtml).not.toContain("ring-1");

    const idleHtml = renderToStaticMarkup(<SessionRow {...rowProps} active={false} />);
    expect(idleHtml).toContain("opacity-35");
    expect(idleHtml).toContain("font-normal text-ink-2");
    expect(idleHtml).not.toContain("ring-1");
  });
});

describe("an agent whose last turn nobody has opened (#14447)", () => {
  // the triage took the turn out of Unread (done, past Just finished's three hours): Seen as far as its state goes
  const done: Session = { ...session, status: "done", state: "idle", updated_at: "2026-01-01T00:00:00Z", work: { phase: "done", line: "Merged the fix" } };
  const render = (patch: Partial<Session>) => renderToStaticMarkup(<SessionRow {...rowProps} s={{ ...done, ...patch }} active={false} />);
  const mark = (html: string) => html.match(/<span [^>]*class="status-mark"[^>]*>/)?.[0] ?? "";
  const line = (html: string) => html.match(/<span data-row-line[^>]*>(.*?)<\/span>/)?.[1];
  it("keeps Just finished's mark under its own word, with what it did under the title, however old", () => {
    const html = render({ seen_at: "0001-01-01T00:00:00Z" });
    expect(mark(html)).toContain('data-state="unopened"');
    expect(mark(html)).toContain(`aria-label="${stateName("unopened")}"`);
    expect(line(html)).toBe("✓ Merged the fix");
    // looked at before this turn ended: still not opened since
    expect(mark(render({ seen_at: "2025-12-31T20:00:00Z" }))).toContain('data-state="unopened"');
  });
  it("is plain Seen once opened after the turn ended, or seen in its host", () => {
    for (const html of [render({ seen_at: "2026-01-01T00:00:05Z" }), render({ status: "idle", seen_at: "0001-01-01T00:00:00Z" })]) {
      expect(mark(html)).toContain('data-state="idle"');
      expect(line(html)).toBeUndefined();
    }
  });
  it("leaves a turn a usage limit stopped without it: that turn goes on by itself", () => {
    const html = render({ seen_at: "0001-01-01T00:00:00Z", turn_error: "usage limit reached", turn_error_kind: "usage_limit" });
    expect(mark(html)).toContain('data-state="idle"');
  });
});

describe("where an agent was opened, as a mark at its row's end (#2526)", () => {
  const render = (patch: Partial<Session>, extra = {}) => renderToStaticMarkup(<SessionRow {...rowProps} s={{ ...session, ...patch }} active={false} {...extra} />);
  const mark = (html: string) => html.match(/<span data-source="[^"]*"[^>]*>.*?<\/span>/)?.[0];
  it("omits terminal and desktop icons from the visible row while keeping the source name in the tooltip", () => {
    const terminalHtml = render({ pane_id: "w1:p1" });
    expect(mark(terminalHtml)).toBeUndefined();
    expect(link(terminalHtml)).toMatch(/title="整理文档\n终端/);
    for (const patch of [{ space_kind: "codex_app", source_kind: "codex_history" }, { host: "paseo", source_kind: "runtime", source_instance_id: "paseo" }]) {
      const desktopHtml = render(patch);
      expect(mark(desktopHtml)).toBeUndefined();
      expect(link(desktopHtml)).toMatch(/title="整理文档\n桌面端/);
    }
  });
  it("draws Lark's mark for a Lark group, a link to its topic when it has one", () => {
    expect(mark(render({ pane_id: "botmux:oc_1/t1" }))).toMatch(/^<span data-source="feishu" role="img"[^>]*aria-label="飞书群"/);
    const linked = mark(render({ pane_id: "botmux:oc_1/t1", external_url: "https://example.com/topic" }));
    expect(linked).toMatch(/^<span data-source="feishu" role="link" tabindex="0"[^>]*aria-label="飞书群 · 在飞书里打开这个话题"/);
    expect(linked).toContain("<img");
  });
  it("draws none for other sources, Persona's conversations and a tab with several panes", () => {
    expect(mark(render({}))).toBeUndefined();
    expect(mark(render({ source_kind: "runtime", source_instance_id: "dsh" }))).toBeUndefined();
    expect(mark(render({ pane_id: "botmux:oc_1/t1", assistant: true }))).toBeUndefined();
    expect(mark(render({ pane_id: "botmux:oc_1/t1" }, { endRoom: true }))).toBeUndefined();
    expect(link(render({ pane_id: "w1:p1" }, { endRoom: true }))).not.toMatch(/\n终端/);
  });
  it("keeps Lark marks visible under the pointer like the rest of the row's end, open or not", () => {
    expect(mark(render({ pane_id: "botmux:oc_1/t1" }))).not.toContain("invisible");
    expect(mark(renderToStaticMarkup(<SessionRow {...rowProps} s={{ ...session, pane_id: "botmux:oc_1/t1" }} active />))).not.toContain("invisible");
  });
  it("keeps desktop and source subtitles in the hover tooltip instead of the visible row", () => {
    const paseo = { host: "paseo", source_kind: "runtime", source_instance_id: "paseo", agent_label: "Paseo" };
    const writable = render({ ...paseo, capabilities: { read: true, send: true } });
    expect(writable).not.toMatch(/>Paseo</);
    expect(link(writable)).toContain("Paseo"); // still in the row's tooltip
    const claudeDesktop = render({ source_kind: "claude_desktop", agent_name: "Claude Desktop", capabilities: { read: true, send: true } });
    expect(claudeDesktop).not.toContain("Claude Desktop</span>");
    expect(link(claudeDesktop)).toContain("Claude Desktop"); // still in the row's tooltip
  });
  it("keeps the title's width first when a hint sits at the row's end", () => {
    const html = render({}, { hint: "Project" });
    expect(html).toMatch(/<span class="[^"]*flex-1 truncate[^"]*basis-auto[^"]*">整理文档<\/span>/);
    expect(html).toMatch(/<span class="[^"]*min-w-6 shrink-\[20\][^"]*">Project/);
    expect(render({ pane_id: "w1:p1" })).not.toContain("basis-auto"); // nothing at the end: as before
  });
});

describe("an agent's row with 「Agent 行显示详情」 (#2430)", () => {
  const detailed: Session = { ...session, model: "claude-opus-5-5", context_tokens: 78000, context_window: 200000 };
  const render = (s: Session, extra = {}) => renderToStaticMarkup(<SessionRow {...rowProps} s={s} active={false} {...extra} />);
  afterEach(() => {
    setAgentDetails(false);
  });
  it("is byte for byte the same as before while the setting is off", () => {
    expect(render(detailed)).toBe(render(session)); // the model and context change nothing
    expect(render(detailed)).not.toContain("opus 5.5");
  });
  it("adds a second line with folder, model and labelled context used when on", () => {
    setAgentDetails(true);
    const html = render(detailed);
    expect(html).toContain("opus 5.5");
    expect(html).toContain("上下文 39%");
    expect(html).toContain("code");
  });
  it("uses amber caution from 80% context used", () => {
    setAgentDetails(true);
    expect(render({ ...detailed, context_tokens: 170000 })).toContain("text-orange");
    const danger = render({ ...detailed, context_tokens: 185000 });
    expect(danger).toContain("text-orange");
    expect(danger).not.toContain("text-red");
  });
  it("draws no second line for a session that says nothing", () => {
    setAgentDetails(true);
    const plain = { ...session, cwd: "" };
    expect(render(plain)).not.toContain("flex-col");
  });
});

describe("a personal agent's row", () => {
  const s = { id: "a1", title: "周报助手", provider: "lark", unreadCount: 0, rankTimeMs: 0 } as AgentSession;
  const props = { s, reg: () => () => {}, hover: () => {}, go: () => () => {}, pinned: false, onMenu: () => {} };
  const pin = (html: string) => html.match(/<button [^>]*aria-label="置顶"[^>]*>/)![0];
  it("is aria-current with the open row's fill and its pin shown when open", () => {
    const html = renderToStaticMarkup(<AgentRow {...props} active />);
    expect(link(html)).toContain('aria-current="page"');
    expect(link(html)).toMatch(/class="[^"]*\bbg-float\b/);
    expect(link(html)).toMatch(/class="[^"]*\bshadow-float\b/);
    expect(pin(html)).toMatch(/class="[^"]*\bopacity-100\b/);
  });
  it("shows its pin on hover only otherwise", () => {
    const html = renderToStaticMarkup(<AgentRow {...props} active={false} />);
    expect(link(html)).not.toContain("aria-current");
    expect(pin(html)).toMatch(/class="[^"]*\bopacity-0\b/);
  });
  it("keeps its unread count in sight under the pointer, beside the hover pin's slot in the wide layout", () => {
    const html = renderToStaticMarkup(<AgentRow {...props} s={{ ...s, unreadCount: 3 }} active={false} />);
    expect(html).toMatch(/<span class="sidebar-personal-meta[^"]*"><span[^>]*>3<\/span>/);
    expect(html).not.toContain("invisible");
    expect(link(html)).toMatch(/class="[^"]*lg:\[@media\(hover:hover\)\]:pr-7/);
  });
});

describe("Persona's main row", () => {
  const main: Session = { machine_id: "hub", session_id: "main", title: "Persona", cwd: "/", status: "idle", state: "idle", assistant: true };
  const props = { reg: () => () => {}, hover: () => {}, go: () => () => {}, main };
  it("uses the existing home symbol and one Main chat line, without the assistant name or face", () => {
    const html = renderToStaticMarkup(<PersonaRow {...props} active />);
    const home = renderToStaticMarkup(<Icon name="home" size={14} stroke={1.8} className="shrink-0 text-ink" />);
    expect(html).toContain(home);
    expect(html).toContain(t("sidebar.main_chat"));
    expect(html).not.toContain("flex-col\"><span class=\"truncate text-[13.5px] font-semibold text-ink\">Persona");
    expect(html.replace(/<[^>]+>/g, "").match(new RegExp(t("sidebar.main_chat"), "g"))).toHaveLength(1);
  });
});

describe("a session's associated tab name (#7595)", () => {
  const named: Session = { ...session, slot_id: "w1:t2", slot_name: "Release Review", pane_id: "w1:p1", updated_at: "2026-11-14T09:00:00Z" };
  const render = (patch: Partial<Session>, extra = {}) => renderToStaticMarkup(<SessionRow {...rowProps} s={{ ...session, ...patch }} hint="Project" active={false} {...extra} />);
  const tabName = (label: string) => t("sidebar.tab_named", { label });

  it("keeps terminal tab numbers/names in the tooltip and aria-description instead of cluttering the visible row", () => {
    const first = render(named);
    expect(first).not.toContain(`>${tabName("Release Review")}<`);
    expect(link(first)).toContain(tabName("Release Review"));
    expect(link(first)).toContain(`aria-description="${tabName("Release Review")}"`);

    const renamed = render({ ...named, slot_name: "Triage" });
    expect(link(renamed)).toContain(tabName("Triage"));
    expect(link(renamed)).not.toContain(tabName("Release Review"));

    const numbered = render({ ...named, slot_name: "agent-8" });
    expect(numbered).not.toContain(">agent-8<");
    expect(link(numbered)).toContain(tabName("agent-8"));
  });

  it("keeps the session title clean without terminal tab suffixes", () => {
    const numeric = render({ ...named, slot_name: "2" });
    expect(numeric).toContain("整理文档");
    expect(link(numeric)).toContain(tabName("#2"));

    const missing = render({ ...named, slot_id: undefined, slot_name: undefined, pane_id: undefined });
    expect(link(missing)).not.toContain(tabName("Release Review"));
    expect(missing).toContain("整理文档");

    const unassociated = render({ ...named, slot_id: undefined, tab_id: undefined, slot_name: "Workspace name" });
    expect(link(unassociated)).not.toContain(tabName("Workspace name"));
    expect(unassociated).toContain("整理文档");

    const indexOnly = render({ ...named, slot_name: undefined, slot_index: 2 });
    expect(link(indexOnly)).toContain(tabName("#2"));
    expect(indexOnly).toContain("整理文档");
  });
});
