import { describe, expect, it } from "vitest";
import { agentId, agentLetter, agentName, isMarked, markId, sessionAgent, sessionAgentName } from "./agent-kind";

describe("agentId", () => {
  it("reads nothing as Claude Code and folds the other names of a program into one", () => {
    expect(["", undefined, null, "claude", "Claude-Code", " claudecode "].map(agentId)).toEqual(Array(6).fill("claude"));
    expect(["codex", "Codex-CLI"].map(agentId)).toEqual(["codex", "codex"]);
    expect(["gemini-cli", "cursor-agent", "pi-web", "OpenCode"].map(agentId)).toEqual(["gemini", "cursor", "pi", "opencode"]);
  });

  it("folds a name written with spaces or underscores, as a provider's label may be (#8142)", () => {
    expect(["TRAE CLI", "traecli", "trae_cli", "Gemini CLI", "Cursor Agent"].map(agentId)).toEqual(["trae", "trae", "trae", "gemini", "cursor"]);
    expect(["Oh My Pi", "kimi-cli"].map(agentId)).toEqual(["oh my pi", "kimi-cli"]);
  });

  it("keeps any other agent as it is", () => {
    expect(["traex", "dsh", "amp"].map(agentId)).toEqual(["traex", "dsh", "amp"]);
    expect(["traex", "omp", "amp"].some(isMarked)).toBe(false);
  });
});

describe("agentName", () => {
  it("names the known agents by their product", () => {
    expect(["claude", "codex", "opencode", "pi", "gemini", "cursor", "trae"].map((a) => agentName(a)))
      .toEqual(["Claude Code", "Codex", "opencode", "pi", "Gemini CLI", "Cursor", "TRAE"]);
    expect(["traex", "traecli", "TRAE CLI"].map((a) => agentName(a))).toEqual(Array(3).fill("TRAE CLI"));
    expect(agentName("")).toBe("Claude Code");
  });

  it("calls an agent the app does not know by its runtime's label, else as reported", () => {
    expect(agentName("dsh", "DSH")).toBe("DSH");
    expect(agentName("amp")).toBe("amp");
  });
});

describe("sessionAgent", () => {
  it("is Codex for a thread read off the app-server or its history, whatever it reports", () => {
    expect(sessionAgent({ source_kind: "codex_app_server" }).agent).toBe("codex");
    expect(sessionAgent({ agent: "claude", source_kind: "codex_history" }).agent).toBe("codex");
    expect(sessionAgent({ agent: "opencode", agent_label: "x" })).toEqual({ agent: "opencode", label: "x", surface: undefined });
    expect(sessionAgent({ agent: "claude", agent_surface: "cli" }).surface).toBe("cli");
    expect(sessionAgent({ agent: "codex", agent_surface: "desktop" }).surface).toBe("desktop");
    expect(sessionAgent({ agent: "codex", agent_surface: "sdk" }).surface).toBeUndefined();
  });

  it("keeps a runtime's own agent as it reports it", () => {
    expect(sessionAgent({ source_kind: "runtime", agent: "dsh", agent_label: "DSH" }).agent).toBe("dsh");
  });

  it("names a session's agent by its product, else its runtime's label", () => {
    expect(sessionAgentName({ source_kind: "runtime", agent: "grok", agent_label: "Grok" })).toBe("Grok");
    expect(sessionAgentName({ source_kind: "runtime", agent: "kimi" })).toBe("kimi");
    expect(sessionAgentName({ source_kind: "runtime", agent: "claude", agent_label: "My Claude" })).toBe("Claude Code");
  });
});

describe("markId", () => {
  it("draws the mark a name says when the agent has none of its own (#8142)", () => {
    expect(["Cursor", "TRAE CLI", "OpenCode", "Pi", "Gemini"].map((l) => markId("acme", l))).toEqual(["cursor", "trae", "opencode", "pi", "gemini"]);
    expect(markId("acme", "Grok · Official")).toBe("grok");
    expect(["grok-chat", "grok-cli", "Grok Build", "Google Antigravity", "DeepSeek Harness", "Kimi CLI", "GitHub Copilot CLI"]
      .map((name) => markId(name))).toEqual(["grok", "grok", "grok", "agy", "dsh", "kimi", "copilot"]);
    expect(markId("acme", "Oh My Pi")).toBe("acme");
    expect(markId("acme", "  ")).toBe("acme");
    expect(markId("acme", "· Official")).toBe("acme");
    expect(markId("acme")).toBe("acme");
    // an agent with a mark of its own keeps it, whatever its label says
    expect(markId("codex", "Cursor")).toBe("codex");
    expect(markId("dsh", "DSH")).toBe("dsh");
  });
});

describe("agentLetter", () => {
  it("is the first letter or digit, upper case", () => {
    expect(["dsh", "  amp", "-x", "3d", "小车"].map(agentLetter)).toEqual(["D", "A", "X", "3", "小"]);
    expect(agentLetter("")).toBe("?");
  });
});

// What every client calls each agent (spec/fixtures/agent-names, #10042): the iPhone checks the same file.
describe("agent names", () => {
  it("are the shared fixture's, the product's own spelling", async () => {
    const { readFileSync } = await import("node:fs");
    const fixture = JSON.parse(readFileSync(new URL("../../../spec/fixtures/agent-names/names.json", import.meta.url), "utf8")) as { names: Record<string, string> };
    for (const [id, name] of Object.entries(fixture.names)) expect([id, agentName(id)]).toEqual([id, name]);
  });
});
