import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { posix } from "node:path";
import ts from "typescript";
import { expect, test } from "vitest";

const baselinePath = "src/lib/agent-branch-baseline.json";
const update = "UPDATE_AGENT_BRANCH_BASELINE";
const agents = /^(?:codex|agy|antigravity|traex|trae|claude)$/;
const sources = /^(?:codex_.*|runtime|claude_.*)$/;
const tools = new Set("exec_command apply_patch local_shell shell_command view_image run_command read_file edit_file write_file update_steps update_plan manage_task manage_subagents ask_question spawn_agent wait_agent collab_agent".split(" "));
const markers = /mcp_persona_|trae-question:|codex_heartbeat_quiet/g;
const candidate = new RegExp(`\\b(?:agent|source_kind|narration)\\b|${markers.source}|["'\x60](?:${[...tools].join("|")})["'\x60]`);
const { SyntaxKind: k } = ts;

type Token = { kind: ts.SyntaxKind; text: string };

function* sourceFiles(dir: string): Generator<string> {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const name = posix.join(dir, entry.name);
    if (entry.isDirectory()) yield* sourceFiles(name);
    else if (entry.isFile() && /\.tsx?$/.test(name) && !/\.(?:test|spec)\.tsx?$|-wire\.ts$/.test(name) &&
             !["src/lib/agent-kind.ts", "src/lib/types.ts", "src/lib/turn-upcast.ts", "src/lib/tool-registry.ts"].includes(name)) yield name;
  }
}

function isQuoted(kind: ts.SyntaxKind | undefined): boolean {
  return kind === k.StringLiteral || kind === k.NoSubstitutionTemplateLiteral;
}

function matchesCLI(field: string | undefined, value: Token | undefined): boolean {
  if (!value || !isQuoted(value.kind)) return false;
  return (field === "agent" && agents.test(value.text)) || (field === "source_kind" && sources.test(value.text));
}

function fieldBefore(tokens: Token[], i: number): string | undefined {
  if (tokens[i]?.kind === k.CloseBracketToken && isQuoted(tokens[i - 1]?.kind) && tokens[i - 2]?.kind === k.OpenBracketToken) {
    return tokens[i - 1].text;
  }
  if (tokens[i]?.kind === k.Identifier) return tokens[i].text;
}

function fieldAfter(tokens: Token[], i: number): string | undefined {
  if (tokens[i]?.kind !== k.Identifier) return undefined;
  let name = tokens[i++].text;
  for (;;) {
    const next = tokens[i]?.kind;
    if (next === k.DotToken || next === k.QuestionDotToken) {
      if (tokens[i + 1]?.kind !== k.OpenBracketToken) {
        name = tokens[i + 1]?.text;
        i += 2;
        continue;
      }
      i++;
    }
    if (tokens[i]?.kind === k.OpenParenToken) return undefined;
    if (tokens[i]?.kind !== k.OpenBracketToken) return name;
    if (!isQuoted(tokens[i + 1]?.kind) || tokens[i + 2]?.kind !== k.CloseBracketToken) return undefined;
    name = tokens[i + 1].text;
    i += 3;
  }
}

function countBranches(path: string, text: string, countIdentity = true): number {
  if (!candidate.test(text)) return 0;
  // Tokenize rather than build a TypeScript AST for hundreds of files. Comments are trivia;
  // quoted tool names and identifiers inside template interpolations are still tokens.
  const scanner = ts.createScanner(ts.ScriptTarget.Latest, true,
    path.endsWith(".tsx") ? ts.LanguageVariant.JSX : ts.LanguageVariant.Standard, text);
  const tokens: Token[] = [];
  const operators: number[] = [];
  const templateDepths: number[] = [];
  let depth = 0;
  let count = 0;
  for (let kind = scanner.scan(); kind !== k.EndOfFileToken; kind = scanner.scan()) {
    if (kind === k.TemplateHead) templateDepths.push(depth);
    else if (kind === k.OpenBraceToken) depth++;
    else if (kind === k.CloseBraceToken) {
      if (templateDepths.at(-1) === depth) {
        kind = scanner.reScanTemplateToken(false);
        if (kind === k.TemplateTail) templateDepths.pop();
      } else depth--;
    }
    const quoted = isQuoted(kind);
    const text = quoted ? scanner.getTokenValue() : scanner.getTokenText();
    if (kind === k.EqualsEqualsEqualsToken || kind === k.ExclamationEqualsEqualsToken) operators.push(tokens.length);
    if (quoted && tools.has(text)) count++;
    if (text === "narration" && (tokens.at(-1)?.kind === k.DotToken || tokens.at(-1)?.kind === k.QuestionDotToken)) count++;
    count += text.match(markers)?.length ?? 0;
    tokens.push({ kind, text });
  }
  for (const i of countIdentity ? operators : []) {
    if (matchesCLI(fieldBefore(tokens, i - 1), tokens[i + 1]) || matchesCLI(fieldAfter(tokens, i + 1), tokens[i - 1])) count++;
  }
  return count;
}

test("CLI-specific content branches only go down", () => {
  const actual: Record<string, number> = {};
  const paths = [...sourceFiles("src/views/chat"), ...sourceFiles("src/lib")].sort();
  for (const path of paths) {
    // Provider configuration and launch routing still identify their protocols. Chat content
    // and presentation consume facets and capabilities, including for old rows.
    const content = path.startsWith("src/views/chat/") || [
      "turns", "turn-images", "work-groups", "tool-detail", "closeout-commands", "tasks", "latest-plan", "compaction", "api-errors",
    ].some((name) => path === `src/lib/${name}.ts`);
    const count = countBranches(path, readFileSync(path, "utf8"), content);
    if (count) actual[path] = count;
  }
  const baseline = JSON.parse(readFileSync(baselinePath, "utf8")) as Record<string, number>;
  const growth = Object.entries(actual).filter(([path, count]) => count > (baseline[path] ?? 0));
  expect(growth.map(([path, count]) => `${path}: ${baseline[path] ?? 0} -> ${count}`),
    "Move CLI-specific logic to the node-side adapter or a capability flag.").toEqual([]);
  const stale = Object.entries(baseline).filter(([path, count]) => (actual[path] ?? 0) < count || !(path in actual));
  if (process.env[update] === "1") {
    if (stale.length) writeFileSync(baselinePath, JSON.stringify(actual, null, 2) + "\n");
  } else {
    expect(stale.map(([path, count]) => `${path}: ${count} -> ${actual[path] ?? 0}`),
      `Run with ${update}=1 to lower the baseline.`).toEqual([]);
  }
});

test("counts both sides of comparisons and native sentinels, but not comments", () => {
  expect(countBranches("sample.tsx", `
    // s.agent === "trae"; s.narration
    const a = session.agent !== 'agy' || "codex" === session.agent;
    const b = "runtime" !== s["source_kind"] || s.source_kind === "claude_desktop";
    const c = <Row tool="read_file" />;
    const d = /^mcp_persona_/;
    const e = s?.narration || s.codex_heartbeat_quiet || rev.startsWith("trae-question:");
    const notABranch = normalize(agent) === "codex" || "agy" === agent();
  `)).toBe(9);
});
