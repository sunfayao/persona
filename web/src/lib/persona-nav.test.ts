import { describe, expect, it } from "vitest";
import { firstAndCurrent, NAV_PAGES, extensionNavKey, sidebarActiveKey, sidebarRoute } from "./persona-nav";
import { href, parse } from "./router";

describe("Persona is the front door (#1239)", () => {
  it("opens the main conversation at / and at the old /persona and /persona links", () => {
    expect(parse("/")).toEqual({ name: "persona" });
    expect(parse("/persona")).toEqual({ name: "persona" });
    expect(href({ name: "persona" })).toBe("/");
  });

  it("opens the main conversation for the links of other conversations, the new-chat page and the history page", () => {
    expect(parse("/persona/abc")).toEqual({ name: "persona" });
    expect(parse("/persona/abc", "?machine=mac&manager=m")).toEqual({ name: "persona" });
    expect(parse("/persona/new")).toEqual({ name: "persona" });
    expect(parse("/history")).toEqual({ name: "persona" });
    expect(parse("/history/inbox")).toEqual({ name: "persona" });
  });

  it("keeps the message a link lands on", () => {
    expect(parse("/persona/abc", "?at=i1")).toEqual({ name: "persona", at: "i1" });
    expect(href({ name: "persona", at: "i1" })).toBe("/?at=i1");
    expect(parse("/", "?at=i1")).toEqual({ name: "persona", at: "i1" });
  });

  it("moves 总览 to /overview; an unknown path says it is no page (web usability audit item 42)", () => {
    expect(parse("/overview")).toEqual({ name: "home" });
    expect(href({ name: "home" })).toBe("/overview");
    expect(parse("/no-such-page")).toEqual({ name: "not-found", path: "/no-such-page" });
  });

  it("keeps the other old links", () => {
    expect(parse("/s/mac/abc")).toEqual({ name: "session", mid: "mac", sid: "abc" });
    expect(parse("/inbox/x")).toEqual({ name: "inbox", id: "x" });
    expect(parse("/machines/mac")).toEqual({ name: "machine", mid: "mac" });
  });

  it("lights the Persona row for the main conversation however it is reached", () => {
    expect(sidebarActiveKey({ name: "persona" })).toBe("persona");
    expect(sidebarActiveKey({ name: "home" })).toBe("home");
    expect(sidebarActiveKey({ name: "machine", mid: "mac" })).toBe("machines");
    expect(sidebarActiveKey({ name: "session", mid: "mac", sid: "s" })).toBe("s:mac/s");
  });

  it("keeps the row of the page an Agent's page was opened from, a drill-down (#12055, #9971)", () => {
    const agent = { name: "session", mid: "mac", sid: "s" } as const;
    // a to-do's Agent rows and hand-off
    expect(sidebarActiveKey(agent, "/todos?id=t1")).toBe("todos");
    expect(sidebarActiveKey(agent, "/todos?view=calendar&mode=week&id=t1")).toBe("todos");
    // a Persona conversation's card with How Agents open = Full page: the conversation's own row
    expect(sidebarActiveKey(agent, "/")).toBe("persona");
    expect(sidebarActiveKey(agent, "/persona/main")).toBe("persona");
    // an Agent opened from another Agent's page keeps that Agent's row
    expect(sidebarActiveKey(agent, "/s/mac/first")).toBe("s:mac/first");
    // a tab's page follows where it came from too
    expect(sidebarActiveKey({ name: "tab", mid: "mac", space: "w", slot: "1", sid: "s" }, "/todos?id=t1")).toBe("todos");
    expect(sidebarRoute(agent, "/todos?id=t1")).toEqual({ name: "todos", id: "t1" });
  });

  it("lights an Agent's own row when nothing opened it, and every other page its own whatever it carries", () => {
    const agent = { name: "session", mid: "mac", sid: "s" } as const;
    expect(sidebarActiveKey(agent)).toBe("s:mac/s");
    expect(sidebarActiveKey(agent, undefined)).toBe("s:mac/s");
    expect(sidebarRoute(agent)).toBe(agent);
    // only an Agent's page is a drill-down here: a to-do opened from a conversation lights To-dos, not the conversation
    expect(sidebarActiveKey({ name: "todos", id: "t1" }, "/persona/c2")).toBe("todos");
    expect(sidebarActiveKey({ name: "persona" }, "/todos")).toBe("persona");
  });

  it("shortens a list to its first n and the open item after them (#2283)", () => {
    const isFour = (x: number) => x === 4;
    expect(firstAndCurrent([1, 2, 3, 4, 5], 2, isFour)).toEqual([1, 2, 4]);
    expect(firstAndCurrent([1, 2, 3, 4, 5], 5, isFour)).toEqual([1, 2, 3, 4, 5]);
    expect(firstAndCurrent([1, 2, 3], 2, isFour)).toEqual([1, 2]);
  });

  it("orders the pages under Persona like the app", () => {
    expect(NAV_PAGES).toEqual(["home", "inbox", "messages", "starred", "todos", "routines", "machines", "terminal", "settings"]);
  });
});

describe("an extension's page (docs/extensions/extensions.md「UI slots」 nav_page)", () => {
  it("has its own link and lights its own sidebar row", () => {
    expect(parse("/extension/example-gateway/usage")).toEqual({ name: "extension", extension: "example-gateway", ui: "usage" });
    expect(href({ name: "extension", extension: "a b", ui: "c/d" })).toBe("/extension/a%20b/c%2Fd");
    expect(parse(href({ name: "extension", extension: "a b", ui: "c/d" }))).toEqual({ name: "extension", extension: "a b", ui: "c/d" });
    expect(parse("/extension/only-one")).toEqual({ name: "not-found", path: "/extension/only-one" });
    expect(sidebarActiveKey({ name: "extension", extension: "g", ui: "usage" })).toBe(extensionNavKey("g", "usage"));
    expect(extensionNavKey("g", "usage")).toBe("p:g/usage");
  });
});
