import { describe, expect, it } from "vitest";
import { backgroundSupervisorTitle, isBackgroundSupervisor, reviewedInBackground, REVIEW_BY_BACKGROUND } from "./background-supervisor";
import { autoOrigin, unlistedConversations } from "./conversation-origins";
import type { FeedItem } from "./feed";
import type { Session } from "./types";

const conv = (id: string, extra: Partial<Session> = {}) => ({ machine_id: "hub", session_id: id, title: id, cwd: "", status: "idle", ...extra }) as Session;

describe("the background supervisor (#5082)", () => {
  it("is the conversation the hub names, never the main one, and none on a hub before it", () => {
    expect(isBackgroundSupervisor({ main: "main", background_supervisor: "bg" }, "bg")).toBe(true);
    expect(isBackgroundSupervisor({ main: "main", background_supervisor: "bg" }, "other")).toBe(false);
    expect(isBackgroundSupervisor({ main: "main", background_supervisor: "main" }, "main")).toBe(false);
    expect(isBackgroundSupervisor({ main: "main" }, "bg")).toBe(false);
    expect(isBackgroundSupervisor(undefined, "bg")).toBe(false);
    expect(isBackgroundSupervisor(null, "bg")).toBe(false);
    expect(isBackgroundSupervisor({ main: "main", background_supervisor: "bg" }, undefined)).toBe(false);
  });

  it("is called what the hub called it, else by this page's own name (tests run in zh-Hans)", () => {
    expect(backgroundSupervisorTitle({ title: "Background supervisor" })).toBe("Background supervisor");
    for (const title of ["", "  ", "Claude Code", "assistant"]) expect(backgroundSupervisorTitle({ title })).toBe("后台督办");
    expect(backgroundSupervisorTitle()).toBe("后台督办");
  });

  it("is listed with the scheduled tasks' chats whatever origin its row carries", () => {
    expect(autoOrigin(conv("bg"), "bg")).toBe("task");
    expect(autoOrigin(conv("bg", { origin: "user" }), "bg")).toBe("task");
    expect(autoOrigin(conv("bg", { origin: "task" }), "bg")).toBe("task");
    // without the hub naming it, a row goes by its origin as before
    expect(autoOrigin(conv("bg", { origin: "user" }))).toBeUndefined();
    expect(autoOrigin(conv("other", { origin: "user" }), "bg")).toBeUndefined();
  });

  it("tells its reviews from the main chat's own and from a reviewer this page doesn't know", () => {
    const review = (body?: Record<string, unknown>, kind = "review") => ({ kind, body }) as Pick<FeedItem, "kind" | "body">;
    expect(reviewedInBackground(review({ verdict: "accepted", by: REVIEW_BY_BACKGROUND }))).toBe(true);
    expect(reviewedInBackground(review({ verdict: "accepted" }))).toBe(false);
    expect(reviewedInBackground(review({ verdict: "accepted", by: "someone_newer" }))).toBe(false);
    expect(reviewedInBackground(review())).toBe(false);
    expect(reviewedInBackground(review({ by: REVIEW_BY_BACKGROUND }, "report"))).toBe(false);
  });

  it("one the held answer does not name yet has the assistant read again, as any conversation it does not list (#10190)", () => {
    const held = { background_supervisor: "bg", conversations: [conv("listed", { assistant: true, origin: "task" })] };
    const pushed = [
      conv("bg", { assistant: true, origin: "task" }), // the one named
      conv("listed", { assistant: true, origin: "task" }), // already in the answer
      conv("agent", { origin: "task" }), // not the assistant's
      conv("new", { assistant: true, origin: "task" }), // made since, or the replacement of one that is gone
    ];
    expect(unlistedConversations(held, pushed)).toEqual(["new"]);
    expect(unlistedConversations({ conversations: [] }, [conv("first", { assistant: true, origin: "task" })])).toEqual(["first"]);
    expect(unlistedConversations({ background_supervisor: "new", conversations: [] }, pushed.slice(3))).toEqual([]);
  });
});
