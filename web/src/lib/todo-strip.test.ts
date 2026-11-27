// The to-do strip's model: which form it takes, where the to-do stands and the one next thing, from what the hub sent.
import { describe, expect, it } from "vitest";
import type { Supervision } from "./supervision";
import { stripCriteria, todoStrip } from "./todo-strip";
import type { Todo } from "./todos";

const NOW = Date.parse("2026-11-20T12:00:00Z");
const self = { machine_id: "mac", session_id: "s1" };
const link = { id: "todo_1", title: "Old title" };
const todo = (patch: Partial<Todo> = {}): Todo => ({
  id: "todo_1",
  title: "Fix the widget",
  created_at: "2026-11-20T08:00:00Z",
  updated_at: "2026-11-20T11:00:00Z",
  agents: [{ machine_id: "mac", session_id: "s1", linked_at: "2026-11-20T08:00:00Z" }],
  ...patch,
});
const sup = (patch: Partial<Supervision> = {}): Supervision => ({ supervisor: "conv_main", ...patch });
const three = sup({
  done_when: ["The fix is merged", "A snapshot test covers landscape", "The release notes say so"],
  criteria: [
    { criterion: "The fix is merged", state: "met" },
    { criterion: "A snapshot test covers landscape", state: "not_met" },
  ],
});

describe("todoStrip", () => {
  it("falls back to the link's title when the hub sent no row, and a completed link reads as done", () => {
    expect(todoStrip(link, undefined, undefined, self, NOW)).toEqual({ form: "missing", id: "todo_1", title: "Old title" });
    expect(todoStrip(link, undefined, undefined, self, NOW, true)).toEqual({ form: "finished", id: "todo_1", title: "Old title", how: "done" });
  });

  it("takes its finished form when done or cancelled, but a failed start stays open for the owner", () => {
    expect(todoStrip(link, todo({ done_at: "2026-11-20T11:30:00Z" }), undefined, self, NOW)).toMatchObject({ form: "finished", how: "done", title: "Fix the widget" });
    expect(todoStrip(link, todo({ done_at: "2026-11-20T11:30:00Z", cancelled_at: "2026-11-20T11:30:00Z" }), undefined, self, NOW)).toMatchObject({ form: "finished", how: "cancelled" });
    const failed = todoStrip(link, todo({ done_at: "2026-11-20T11:30:00Z", start_error: "no shell" }), "failed_start", self, NOW);
    expect(failed).toMatchObject({ form: "open", needsYou: true, next: { kind: "owner", need: "failed_start" } });
  });

  it("names the first criterion not met, its stage, and the other agents on it", () => {
    const model = todoStrip(link, todo({
      stage: "develop",
      supervision: three,
      agents: [
        { machine_id: "mac", session_id: "s1", linked_at: "2026-11-20T08:00:00Z" },
        { machine_id: "mac", session_id: "s2", linked_at: "2026-11-20T09:00:00Z", title: "Snapshot tests" },
        { machine_id: "pc", session_id: "s3", linked_at: "2026-11-20T09:00:00Z", released_at: "2026-11-20T10:00:00Z" },
      ],
    }), undefined, self, NOW);
    expect(model.form).toBe("open");
    if (model.form !== "open") return;
    expect(model.needsYou).toBe(false);
    expect(model.stage).toBe("develop");
    expect(model.criteria.map((c) => [c.n, c.mark])).toEqual([[1, "met"], [2, "missing"], [3, "open"]]);
    expect(model.next).toEqual({ kind: "criterion", criterion: { n: 2, text: "A snapshot test covers landscape", mark: "missing" } });
    expect(model.others.map((a) => a.session_id)).toEqual(["s2"]);
  });

  it("puts what the owner is asked first, in the ask's own words", () => {
    const model = todoStrip(link, todo({
      supervision: three,
      ask: { outcome: "decide", verdict: "Two ways to ship", question: "Ship behind a flag?", asked_at: "2026-11-20T11:00:00Z" },
    }), "owner_decides", self, NOW);
    expect(model).toMatchObject({ form: "open", needsYou: true, next: { kind: "owner", need: "owner_decides", text: "Ship behind a flag?" } });
  });

  it("says what the work waits on before the next criterion: a review, a merge, a deploy", () => {
    const at = (patch: Partial<Todo>) => {
      const m = todoStrip(link, todo({ supervision: three, ...patch }), undefined, self, NOW);
      return m.form === "open" ? m.next : undefined;
    };
    expect(at({ supervision: { ...three, state: "pending_review" } })).toEqual({ kind: "wait", on: "review" });
    expect(at({ delivery: { state: "pr", unblock: "other" } })).toEqual({ kind: "wait", on: "merge" });
    expect(at({ delivery: { state: "merged", unblock: "agent" } })).toEqual({ kind: "wait", on: "deploy" });
  });

  it("with no criteria shows the stage alone; unattended work still says so", () => {
    const plain = todoStrip(link, todo({ stage: "review" }), undefined, self, NOW);
    expect(plain).toMatchObject({ form: "open", stage: "review", criteria: [], next: { kind: "none" } });
    expect(todoStrip(link, todo({ stage: "review", stage_kind: "general" }), undefined, self, NOW)).toMatchObject({ stage: undefined });
    expect(todoStrip(link, todo(), "unattended", self, NOW)).toMatchObject({ needsYou: true, next: { kind: "unattended" } });
    // nobody on it, but a criterion is left: that is the more useful next thing, and it still needs the owner
    expect(todoStrip(link, todo({ supervision: three }), "unattended", self, NOW)).toMatchObject({ needsYou: true, next: { kind: "criterion" } });
  });

  it("reads every criterion met, and a brief's criteria when nothing supervises it", () => {
    const met = sup({ done_when: ["A"], criteria: [{ criterion: "A", state: "met" }] });
    expect(todoStrip(link, todo({ supervision: met }), undefined, self, NOW)).toMatchObject({ next: { kind: "all_met" } });
    const brief = todo({ brief: { id: "b1", created_at: "2026-11-20T08:00:00Z", started_by: "owner", goal: "g", done_when: ["Old"], criteria: ["Merged", "Tested"] } });
    expect(stripCriteria(brief)).toEqual([{ n: 1, text: "Merged", mark: "open" }, { n: 2, text: "Tested", mark: "open" }]);
  });
});
