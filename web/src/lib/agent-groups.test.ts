import { describe, expect, it } from "vitest";
import { type AgentMutes, awaitsReply, finishedUnopened, isQuiet, justFinished, replyAsk, sessionHost, splitAgentSessions } from "./agent-groups";
import { byAttention } from "./format";
import type { AgentState, Machine, Session } from "./types";

const machines: Machine[] = [
  { id: "machine-b", name: "电脑 B", online: true },
  { id: "machine-a", name: "电脑 A", online: true },
  { id: "machine-off", name: "离线电脑", online: false },
];
const date = (hour: number) => `2026-01-01T${String(hour).padStart(2, "0")}:00:00Z`;
const session = (id: string, state: AgentState, patch: Partial<Session> = {}): Session => ({
  machine_id: "machine-a", session_id: id, title: id, cwd: "/tmp/project", status: state, state, updated_at: date(1), ...patch,
});
const ids = (sessions: Session[]) => sessions.map((s) => s.session_id);
const mutes: AgentMutes = { spaces: ["machine-a/项目"], sessions: ["machine-a/muted-working", "machine-a/muted-needs", "machine-a/muted-idle", "machine-a/muted-waiting"], allowed: ["machine-a/allowed"] };

describe("live 与最近的边界", () => {
  it("助手不混入 Agents，离线或未知机器的会话只进入最近", () => {
    const all = [session("live", "unknown"), session("assistant", "working", { assistant: true }), session("offline", "offline"),
      session("machine-offline", "working", { machine_id: "machine-off" }), session("unknown-machine", "working", { machine_id: "unknown" })];
    const lists = splitAgentSessions(all, machines);
    expect(ids(lists.live)).toEqual(["live"]);
    expect(ids(lists.recent)).toEqual(["offline", "machine-offline", "unknown-machine"]);
  });

  it("最近按更新时间倒序并限制 40 条，不修改输入数据", () => {
    const all = Array.from({ length: 50 }, (_, i) => session(String(i).padStart(2, "0"), "offline", { updated_at: date(i % 24) }));
    const before = ids(all);
    const { recent } = splitAgentSessions(all, machines);
    expect(recent).toHaveLength(40);
    expect(recent[0].updated_at).toBe(date(23));
    expect(ids(all)).toEqual(before);
  });
});

describe("sessionHost", () => {
  it("names the host from the pane ref; a bare id is herdr's", () => {
    const s = (pane_id?: string) => ({ machine_id: "m", session_id: "s", status: "idle", pane_id }) as never;
    expect(sessionHost(s("w1:p2"))).toBe("herdr");
    expect(sessionHost(s("orca:t1:l2"))).toBe("orca");
    expect(sessionHost(s("p2"))).toBe("herdr");
    expect(sessionHost(s("botmux:abc"))).toBe("botmux");
    expect(sessionHost(s("tern:42"))).toBe("tern");
    expect(sessionHost(s("rex:block:b1"))).toBe("rex");
    expect(sessionHost(s())).toBe("");
  });
});

describe("待你回复（#5096）", () => {
  const asking = (id: string, patch: Partial<Session> = {}) => session(id, "idle", { awaits_reply: true, asks: "要不要推到 beta？", ...patch });

  it("已看 + awaits_reply、没屏蔽才算；完成未看、屏蔽的、没标的都不算", () => {
    expect(awaitsReply(asking("a"))).toBe(true);
    expect(awaitsReply(asking("a"), mutes)).toBe(true);
    expect(awaitsReply(session("u", "unread", { awaits_reply: true }))).toBe(false);
    expect(awaitsReply(asking("muted-idle"), mutes)).toBe(false);
    expect(awaitsReply(asking("in-space", { space_name: "项目" }), mutes)).toBe(false);
    expect(awaitsReply(session("plain", "idle"))).toBe(false);
  });

  it("不算安静（不收进空闲的空间），屏蔽的照旧算安静", () => {
    expect(isQuiet(asking("a"))).toBe(false);
    expect(isQuiet(session("plain", "idle"))).toBe(true);
    expect(isQuiet(asking("muted-idle"), mutes)).toBe(true);
  });

  it("组内排序（byAttention）：待你回复在完成未看之后、工作中之前", () => {
    const all = [session("idle", "idle"), session("working", "working"), asking("reply"), session("unread", "unread"), session("needs", "needs_you")];
    expect(ids(all.sort(byAttention))).toEqual(["needs", "unread", "reply", "working", "idle"]);
  });

  it("问的那句：分诊说在等你（blocked·user）时用它的 line，否则用 asks", () => {
    expect(replyAsk(asking("a"))).toBe("要不要推到 beta？");
    expect(replyAsk(asking("a", { work: { phase: "blocked", on: "user", line: "等你定 A 还是 B" } }))).toBe("等你定 A 还是 B");
    expect(replyAsk(asking("a", { work: { phase: "blocked", on: "external", line: "在等 CI" } }))).toBe("要不要推到 beta？");
    expect(replyAsk(session("plain", "idle"))).toBe("");
  });
});

describe("Just finished (#9751)", () => {
  const finished = (id: string, patch: Partial<Session> = {}) => session(id, "idle", { work: { phase: "done", line: "Merged", just_finished: true }, ...patch });

  it("is a seen agent the hub marks just finished, unless muted", () => {
    expect(justFinished(finished("a"))).toBe(true);
    expect(justFinished(finished("a"), mutes)).toBe(true);
    expect(justFinished(session("u", "unread", { work: { phase: "done", just_finished: true } }))).toBe(false);
    expect(justFinished(finished("muted-idle"), mutes)).toBe(false);
    expect(justFinished(finished("in-space", { space_name: "项目" }), mutes)).toBe(false);
    expect(justFinished(session("done", "idle", { work: { phase: "done", line: "Merged" } }))).toBe(false);
  });

  it("is not quiet, so it never folds into Idle spaces; a muted one still is", () => {
    expect(isQuiet(finished("a"))).toBe(false);
    expect(isQuiet(finished("muted-idle"), mutes)).toBe(true);
  });

  it("ranks after Working and before the seen ones (byAttention)", () => {
    const all = [session("idle", "idle", { updated_at: date(9) }), finished("done"), session("working", "working"), session("unread", "unread")];
    expect(ids(all.sort(byAttention))).toEqual(["unread", "working", "done", "idle"]);
  });
});

describe("Finished, not opened (#14447)", () => {
  // the triage took its turn out of Unread: Seen as far as its state goes, while its host still says done and nobody
  // looked since the turn ended (the hub's zero seen_at: never)
  const unopenedRow = (id: string, patch: Partial<Session> = {}) => session(id, "idle", { status: "done", seen_at: "0001-01-01T00:00:00Z", work: { phase: "done", line: "Merged" }, ...patch });

  it("is a turn that ended after the owner last looked, however long ago and whatever the triage said", () => {
    expect(finishedUnopened(unopenedRow("a"))).toBe(true);
    expect(finishedUnopened(unopenedRow("looked-before", { seen_at: date(0) }))).toBe(true);
    expect(finishedUnopened(unopenedRow("hub-wait", { work: { phase: "blocked", on: "external", wake_at: date(5) } }))).toBe(true);
    // within Just finished's three hours too: the row's mark says Just finished then (StatusMark)
    expect(finishedUnopened(unopenedRow("just", { work: { phase: "done", just_finished: true } }))).toBe(true);
  });

  it("is not one opened since, seen in its host, Unread, stopped by a usage limit, muted, or a Persona chat", () => {
    expect(finishedUnopened(unopenedRow("opened", { seen_at: date(2) }))).toBe(false);
    expect(finishedUnopened(unopenedRow("host-seen", { status: "idle" }))).toBe(false);
    expect(finishedUnopened(unopenedRow("unread", { state: "unread" }))).toBe(false);
    expect(finishedUnopened(unopenedRow("limit", { turn_error: "usage limit reached", turn_error_kind: "usage_limit" }))).toBe(false);
    expect(finishedUnopened(unopenedRow("muted-idle"), mutes)).toBe(false);
    expect(finishedUnopened(unopenedRow("in-space", { space_name: "项目" }), mutes)).toBe(false);
    expect(finishedUnopened(unopenedRow("chat", { assistant: true }))).toBe(false);
  });

  it("is not quiet, so its space stays out of Idle spaces until it is opened; a muted one still is", () => {
    expect(isQuiet(unopenedRow("old"))).toBe(false);
    expect(isQuiet(unopenedRow("opened", { seen_at: date(2) }))).toBe(true);
    expect(isQuiet(unopenedRow("muted-idle"), mutes)).toBe(true);
  });

  it("ranks as Seen: the attention order is Seen's", () => {
    const all = [unopenedRow("old", { updated_at: date(9) }), session("working", "working"), session("unread", "unread")];
    expect(ids(all.sort(byAttention))).toEqual(["unread", "working", "old"]);
  });
});

