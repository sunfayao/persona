import { describe, expect, it } from "vitest";
import { autoOrigin, externalFrom, externalOpening, externalOrigin, openerOf, originMarkLabel, unlistedConversations } from "./conversation-origins";
import type { Item, Session } from "./types";

const conv = (id: string, extra: Partial<Session> = {}) => ({ machine_id: "hub", session_id: id, title: id, cwd: "", status: "idle", ...extra }) as Session;

describe("conversation origins (docs/protocol/protocol.md「Origin of assistant chats」)", () => {
  it("counts none, user and channel as the user's own and anything else as automatic", () => {
    expect(autoOrigin(conv("a"))).toBeUndefined();
    expect(autoOrigin(conv("a", { origin: "user" }))).toBeUndefined();
    expect(autoOrigin(conv("a", { origin: "channel" }))).toBeUndefined();
    expect(autoOrigin(conv("a", { origin: "inbox" }))).toBe("inbox");
    expect(autoOrigin(conv("a", { origin: "lark_agent" }))).toBe("lark_agent");
    expect(autoOrigin(conv("a", { origin: "something_new" }))).toBe("other");
  });

  it("counts only Lark /agent and other extensions as external, by the trimmed origin (#3758)", () => {
    expect(externalOrigin(conv("a", { origin: "lark_agent" }))).toBe("lark_agent");
    expect(externalOrigin(conv("a", { origin: " extension " }))).toBe("extension");
    for (const origin of [undefined, "", "user", "channel", "inbox", "task", "spawned", "other", "something_new", "Extension"]) {
      expect(externalOrigin(conv("a", { origin })), String(origin)).toBeUndefined();
    }
    expect(externalOrigin(undefined)).toBeUndefined();
  });

  it("says where an external conversation came from with the channel line's words (tests run in zh-Hans)", () => {
    expect(externalFrom("lark_agent")).toBe("来自 飞书 /agent");
    expect(externalFrom("extension")).toBe("来自扩展");
  });

  it("finds the message an external conversation was opened with only once the items reach its start", () => {
    const item = (id: string, role: string, kind: string, extra: Partial<Item> = {}) => ({ id, seq: 0, role, kind, ...extra }) as Item;
    const items = [item("n", "assistant", "notice"), item("tool-pic", "user", "image", { tool_use_id: "t1" }), item("u1#0", "user", "text"), item("a1", "assistant", "text"), item("u2#0", "user", "text")];
    const lark = conv("c", { origin: "lark_agent" });
    expect(externalOpening(lark, items, false)).toEqual({ id: "u1#0", origin: "lark_agent" });
    // earlier items still to load: the first one here is not the first
    expect(externalOpening(lark, items, true)).toBeUndefined();
    // a message that starts with a picture is opened by that picture's item, as lib/turns.ts groups it
    expect(externalOpening(conv("c", { origin: "extension" }), [item("p1#0", "user", "image"), item("p1#1", "user", "text")], false)).toEqual({ id: "p1#0", origin: "extension" });
    expect(externalOpening(lark, [item("a1", "assistant", "text")], false)).toBeUndefined();
    expect(externalOpening(conv("c", { origin: "channel" }), items, false)).toBeUndefined();
    expect(externalOpening(conv("c"), items, false)).toBeUndefined();
  });

  it("names the extension that opened an external conversation by opened_by, else origin_ref (#10358)", () => {
    expect(openerOf("lark_agent", { opened_by: "bridge", origin_ref: "bridge" })).toBe("bridge");
    expect(openerOf("extension", { opened_by: " notes " })).toBe("notes");
    // one opened before the hub noted opened_by: origin_ref is that extension too
    expect(openerOf("lark_agent", { origin_ref: "bridge" })).toBe("bridge");
    expect(openerOf("extension", { opened_by: "", origin_ref: "notes" })).toBe("notes");
    // the backfill knows no extension, and the other origins' refs are not extensions
    expect(openerOf("lark_agent", {})).toBeUndefined();
    expect(openerOf("lark_agent", undefined)).toBeUndefined();
    expect(openerOf("inbox", { origin_ref: "item-1" })).toBeUndefined();
    expect(openerOf("spawned", { origin_ref: "conv-1", opened_by: "x" })).toBeUndefined();
    expect(openerOf(undefined, { opened_by: "x" })).toBeUndefined();
  });
});

describe("conversations the held answer does not list", () => {
  it("names the assistant's conversations the held answer does not list, whoever made them, to read it again (#10190)", () => {
    const held = { main: "main", background_supervisor: "bg", conversations: [conv("listed", { assistant: true })] };
    const known = [
      conv("main", { assistant: true }), // the main one, which the answer names
      conv("bg", { assistant: true, origin: "task" }), // the background supervisor it names
      conv("listed", { assistant: true, state: "working" }), // listed: its change is laid over it, nothing is read
      conv("agent", { origin: "user" }), // an Agent's session, not the assistant's
      conv("old", { assistant: true, archived: true }), // the answer leaves archived ones out
      conv("gone", { assistant: true, deleted: true }),
      conv("health", { assistant: true, space: "health" }), // another space's
    ];
    const made = [
      conv("lark", { assistant: true, origin: "lark_agent" }), // an integration's POST /assistant/conversations
      conv("phone", { assistant: true, origin: "user" }), // started on the phone
      conv("morning", { assistant: true, origin: "task", origin_ref: "task_1" }), // a scheduled task's
    ];
    expect(unlistedConversations(held, known)).toEqual([]);
    expect(unlistedConversations(held, [...known, ...made])).toEqual(["lark", "phone", "morning"]);
    expect(unlistedConversations({ conversations: [] }, [conv("first", { assistant: true })])).toEqual(["first"]);
  });
});

describe("a row's origin mark (#5195)", () => {
  it("is named for its group, an external one by where it came from", () => {
    expect(originMarkLabel("inbox")).toBe("收件箱");
    expect(originMarkLabel("task")).toBe("定时任务");
    expect(originMarkLabel("spawned")).toBe("对话派生");
    expect(originMarkLabel("other")).toBe("其他");
    expect(originMarkLabel("lark_agent")).toBe(externalFrom("lark_agent"));
    expect(originMarkLabel("extension")).toBe(externalFrom("extension"));
  });
});
