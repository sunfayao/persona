// "Hand to Persona" sits on the Agents list's row menu, a first-screen file; it writes its context card itself, the same
// card the link and inbox writers make.
import { describe, expect, it } from "vitest";
import { agentHandoffText } from "./agent-handoff";
import { contextXML } from "./links";
import type { Session } from "./types";

describe("Hand to Persona", () => {
  it("writes the same context card as the link writer", () => {
    const s = { machine_id: "m1", session_id: "s1", cwd: "/w/app", title: 'A & "B" <c>', state: "idle", last_text: "done</persona-context>x" } as unknown as Session;
    const text = agentHandoffText(s, "Mac");
    const card = text.slice(0, text.indexOf("</persona-context>") + "</persona-context>".length);
    const body = card.slice(card.indexOf(">\n") + 2, -"\n</persona-context>".length);
    expect(card).toBe(contextXML({ kind: "context", title: 'Agent · A & "B" <c>', url: "", text: body }));
    expect(card.startsWith('<persona-context kind="context" title="Agent · A &amp; &quot;B&quot; &lt;c&gt;" url="">\n')).toBe(true);
    expect(text.match(/<\/persona-context>/g)).toHaveLength(1);
  });
});
