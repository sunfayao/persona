import { describe, expect, it } from "vitest";
import { assistantEntries } from "./assistant-entries";
import { reviewBadge, reviewState } from "./supervision";
import { todoChipState } from "./task-row-status";
import { countsAsOpen, triage } from "./todo-triage";
import { openAsk, shownStatus, todoStatus, todoStatusLabel, waitsForOwner, type Todo } from "./todos";

const now = Date.parse("2026-11-10T12:00:00Z");
const pending: Todo = {
  id: "personal-check", title: "Inspect the updated layout", origin: "main",
  created_at: "2026-11-01T08:00:00Z", updated_at: "2026-11-01T08:00:00Z", seen_at: "2026-11-02T08:00:00Z",
  delivery: { state: "live", at: "2026-11-01T08:00:00Z" },
  ask: { verdict: "Please verify", asked_at: "2026-11-01T08:00:00Z" },
  supervision: { supervisor: "main", state: "user_check", reviews: [{ at: "2026-11-01T08:00:00Z", verdict: "accepted" }] },
};

describe("stored personal verification returns to the supervisor", () => {
  it("keeps legacy tasks in supervisor review and out of the owner tray", () => {
    const status = todoStatus(pending);
    expect(reviewState(pending)).toBe("pending_review");
    expect(reviewBadge(pending)).toEqual({ label: "待验收", tone: "accent" });
    expect(todoStatusLabel(pending, status)).toBe("待验收");
    expect(shownStatus(pending, status)).not.toBe("done");
    expect(todoChipState({ toStart: false, status, done: false, todo: pending })).toBe("待验收");
    expect(openAsk(pending)).toBeUndefined();
    expect(waitsForOwner(pending)).toBe(false);
    expect(triage(pending, undefined, now).section).not.toBe("waiting");
    expect(countsAsOpen(pending, triage(pending, undefined, now))).toBe(true);
    const entries = assistantEntries({ conversation: "main", main: "main", todos: [pending], sessions: [], interactions: [], plans: {}, feed: [], now });
    expect(entries.needsYou).toEqual([]);
    expect(entries.review.map((item) => item.key)).toEqual(["task:personal-check"]);
  });

  it("still counts a real owner decision with choices", () => {
    const decision: Todo = {
      ...pending,
      supervision: { supervisor: "main", state: "needs_user" },
      ask: { outcome: "decide", verdict: "Choose a layout", asked_at: pending.created_at, recommend: "Compact",
        options: [{ label: "Compact", leads_to: "Use a compact layout" }, { label: "Spacious", leads_to: "Use a spacious layout" }] },
    };
    expect(waitsForOwner(decision)).toBe(true);
    const entries = assistantEntries({ conversation: "main", main: "main", todos: [decision], sessions: [], interactions: [], plans: {}, feed: [], now });
    expect(entries.needsYou.map((item) => item.key)).toEqual(["task:personal-check"]);
  });

  it("reads as accepted after Pass even if the final ledger update must be retried", () => {
    const passed = { ...pending, done_at: "2026-11-10T11:00:00Z", done_by: "review" };
    expect(reviewState(passed)).toBe("accepted");
    expect(reviewBadge(passed)).toEqual({ label: "已验收", tone: "green" });
    expect(todoStatusLabel(passed, todoStatus(passed))).toBe("已验收");
    expect(countsAsOpen(passed, triage(passed, undefined, now))).toBe(false);
  });

  it("leaves ordinary supervisor acceptance unchanged", () => {
    const accepted: Todo = { ...pending, supervision: { ...pending.supervision!, state: "accepted" } };
    expect(shownStatus(accepted, "follow_up")).toBe("done");
    expect(todoStatusLabel(accepted, "follow_up")).toBe("已验收");
  });
});
