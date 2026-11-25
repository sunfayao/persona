// @vitest-environment jsdom
// A link to a subagent's own conversation opens its parent's chat at its card (#5030).
import { act } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it } from "vitest";
import { childOfCall } from "./child-cards";
import { closePanel, getPanel, openPanel } from "./panel";
import { doneSubagentLink, followChildSession, followSubagentLink, splitSubagentLink, useSubagentLink } from "./subagent-link";
import type { Session } from "./types";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

describe("subagent links", () => {
  it("splits <parent>@<agent> only", () => {
    expect(splitSubagentLink("p1@a1")).toEqual({ parent: "p1", agent: "a1" });
    expect(splitSubagentLink("p1")).toBeNull();
    expect(splitSubagentLink("@a1")).toBeNull();
    expect(splitSubagentLink("p1@")).toBeNull();
  });

  it("opens the parent and asks its chat for the card until the chat did", () => {
    let seen: string | undefined;
    function Probe() {
      seen = useSubagentLink("m", "p1");
      return null;
    }
    const host = document.createElement("div");
    const root = createRoot(host);
    act(() => root.render(<Probe />));
    expect(seen).toBeUndefined();
    act(() => void expect(followSubagentLink("m", "p1@a1")).toBe("p1"));
    expect(seen).toBe("a1");
    expect(followSubagentLink("m", "p2")).toBe("p2");
    act(() => doneSubagentLink("m", "p1"));
    expect(seen).toBeUndefined();
    act(() => root.unmount());
  });

  it("an agent panel for a subagent opens its parent's chat", () => {
    openPanel({ kind: "agent", mid: "m", sid: "p9@a9" });
    expect(getPanel()).toEqual({ kind: "agent", mid: "m", sid: "p9" });
    closePanel();
    doneSubagentLink("m", "p9");
  });

  it("a child session's link opens its parent and asks for the child's card (#5120)", () => {
    let seen: string | undefined;
    function Probe() {
      seen = useSubagentLink("m", "lead");
      return null;
    }
    const root = createRoot(document.createElement("div"));
    act(() => root.render(<Probe />));
    act(() => void expect(followChildSession("m", "kid", "lead")).toBe("lead"));
    expect(seen).toBe("child:kid");
    expect(childOfCall(seen!)).toBe("kid");
    expect(childOfCall("a1")).toBeUndefined();
    act(() => doneSubagentLink("m", "lead"));
    expect(seen).toBeUndefined();
    act(() => root.unmount());
  });
});
