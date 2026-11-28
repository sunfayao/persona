// @vitest-environment node
import { readFileSync, readdirSync } from "node:fs";
import ts from "typescript";
import { describe, expect, it } from "vitest";

const src = new URL("../", import.meta.url);
const source = (path: string) => readFileSync(new URL(path, src), "utf8");
const css = source("styles.css").replace(/\/\*[\s\S]*?\*\//g, "");

// Read declarations at their own depth. Keep at-rule ancestors so a rule in @media is still global,
// and a Tailwind @utility definition is not mistaken for an emitted, unlayered CSS rule.
function cssBlocks(text: string) {
  type Block = { head: string; parents: string[]; declarations: Map<string, string> };
  const blocks: Block[] = [];
  const stack: Block[] = [];
  let start = 0;
  const declare = (statement: string) => {
    const colon = statement.indexOf(":");
    if (colon !== -1) stack.at(-1)?.declarations.set(statement.slice(0, colon).trim(), statement.slice(colon + 1).trim());
  };
  for (const token of text.matchAll(/"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[{};]/g)) {
    const char = token[0];
    if (char.length !== 1) continue;
    const statement = text.slice(start, token.index).trim();
    if (char === "{") {
      const block = { head: statement.replace(/\s+/g, " "), parents: stack.map((b) => b.head), declarations: new Map<string, string>() };
      blocks.push(block);
      stack.push(block);
    } else {
      declare(statement);
      if (char === "}") stack.pop();
    }
    start = token.index + 1;
  }
  return blocks;
}

const blocks = cssBlocks(css);
const globalSelector = "button:focus-visible, a:focus-visible, summary:focus-visible";
const outline = "var(--focus-ring-width) solid var(--focus-ring-color, var(--accent))";
const radiusSelector = ":is(button, a, summary):focus-visible";
const focusProperties = ["outline", "outline-offset", "border-radius"];

const files = readdirSync(src, { recursive: true, encoding: "utf8" }).filter((file) => file.endsWith(".tsx") && !/\.(?:test|spec)\.tsx$/.test(file)).sort();
const sources = files.map((file) => ts.createSourceFile(file, source(file), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX));

type Opening = ts.JsxOpeningElement | ts.JsxSelfClosingElement;
const opening = (node: ts.Node): node is Opening => ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node);
const attribute = (node: Opening, name: string) => node.attributes.properties.find(
  (prop): prop is ts.JsxAttribute => ts.isJsxAttribute(prop) && prop.name.getText() === name,
);

function classLiterals(node: ts.Node | undefined): string[] {
  if (!node) return [];
  if (ts.isStringLiteralLike(node)) return [node.text];
  if (ts.isTemplateExpression(node)) return [node.head.text, ...node.templateSpans.map((span) => span.literal.text)];
  if (ts.isJsxExpression(node) || ts.isParenthesizedExpression(node)) return classLiterals(node.expression);
  if (ts.isCallExpression(node) && node.expression.getText() === "cx") return node.arguments.flatMap(classLiterals);
  if (ts.isConditionalExpression(node)) return [...classLiterals(node.whenTrue), ...classLiterals(node.whenFalse)];
  if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken) return classLiterals(node.right);
  return [];
}

const classes = (node: Opening) => classLiterals(attribute(node, "className")?.initializer).flatMap((value) => value.split(/\s+/)).filter(Boolean);
const utility = (value: string) => value.slice(value.lastIndexOf(":") + 1);
const intrinsicOrControl = (node: Opening) => /^(?:button|a|summary|input|textarea|select|Button|IconButton|Switch|TooltipTrigger)$/.test(node.tagName.getText());
const interactive = (node: Opening) => intrinsicOrControl(node) || Boolean(attribute(node, "tabIndex"));

function hasRingInClip(child: Opening, tokens: string[], overflow: string) {
  // A breakpoint-hidden child can't be focused while that breakpoint clips. A non-tabbable focus landing
  // box without a ring and a permanently transparent keyboard proxy don't draw rings to make room for.
  const prefix = overflow.slice(0, overflow.lastIndexOf(":") + 1);
  if (prefix && tokens.includes(prefix + "hidden")) return false;
  if (["absolute", "pointer-events-none", "opacity-0"].every((name) => tokens.includes(name))
    && !tokens.some((token) => /^opacity-(?!0$)/.test(utility(token)))) return false;
  return intrinsicOrControl(child) || !/^\{\s*-1\s*\}$/.test(attribute(child, "tabIndex")?.initializer?.getText() ?? "")
    || !tokens.includes("outline-none") || tokens.some((name) => utility(name) === "focus-ring");
}

function directChildren(node: Opening): Opening[] {
  const result: Opening[] = [];
  function visit(child: ts.Node) {
    if (ts.isJsxElement(child)) result.push(child.openingElement);
    else if (ts.isJsxSelfClosingElement(child)) result.push(child);
    else ts.forEachChild(child, visit);
  }
  // Branches, map results and fragments don't insert a DOM box. JSX in a prop is not a child.
  if (ts.isJsxOpeningElement(node)) node.parent.children.forEach(visit);
  return result;
}

function hasInteractiveDescendant(node: Opening): boolean {
  return directChildren(node).some((child) => interactive(child) || hasInteractiveDescendant(child));
}

function elements(tree: ts.SourceFile): Opening[] {
  const result: Opening[] = [];
  function visit(node: ts.Node) {
    if (opening(node)) result.push(node);
    ts.forEachChild(node, visit);
  }
  visit(tree);
  return result;
}

const jsx = sources.map((tree) => ({ tree, tags: elements(tree) }));
const location = (node: Opening) => {
  const tree = node.getSourceFile();
  return `${tree.fileName}:${tree.getLineAndCharacterOfPosition(node.getStart(tree)).line + 1} <${node.tagName.getText()}>`;
};

// Ring room can be unconditional or use the same variant as the overflow (e.g. max-sm:).
function providesRoom(parent: string[], child: string[], overflow: string) {
  const prefix = overflow.slice(0, overflow.lastIndexOf(":") + 1);
  const has = (tokens: string[], name: string) => tokens.includes(name) || tokens.includes(prefix + name);
  return has(parent, "p-ring") || (has(parent, "px-ring") && has(parent, "py-ring"))
    || has(parent, "focus-ring-inset") || has(child, "focus-ring-inset");
}

describe("focus-ring contract", () => {
  it("keeps one unlayered outline and lets controls keep their own radius", () => {
    const global = blocks.filter((b) => b.head === globalSelector);
    expect(global.map((b) => b.parents)).toEqual([[]]);
    expect(Object.fromEntries(global[0].declarations)).toEqual({ outline, "outline-offset": "var(--focus-ring-offset)" });
    const defaults = blocks.filter((b) => b.head === radiusSelector && b.parents.includes("@layer base"));
    expect(defaults.map((b) => b.declarations.get("border-radius"))).toEqual(["6px"]);
    const overrides = blocks.filter((b) => b.head.includes(":focus-visible")
      && !b.parents.some((p) => /^@(?:layer|utility)\b/.test(p))
      && focusProperties.some((property) => b.declarations.has(property)));
    expect(overrides.map((b) => b.head).sort()).toEqual([".range:focus-visible", globalSelector]);
    expect(Object.fromEntries(overrides.find((b) => b.head === ".range:focus-visible")!.declarations)).toEqual({ outline: "none" });
  });

  it("keeps a plain-length ring gutter and ties end padding to the fade height", () => {
    const root = blocks.find((b) => b.head === ":root" && b.parents.length === 0 && b.declarations.has("--focus-ring-width"));
    const width = root?.declarations.get("--focus-ring-width");
    const offset = root?.declarations.get("--focus-ring-offset");
    expect(width).toBe("2px");
    expect(offset).toBe("1px");
    const theme = blocks.filter((b) => b.head === "@theme" && b.parents.length === 0);
    const ring = theme.find((b) => b.declarations.has("--spacing-ring"))?.declarations.get("--spacing-ring");
    expect(ring).toMatch(/^\d+(?:\.\d+)?px$/);
    expect(parseFloat(ring!)).toBeGreaterThanOrEqual(parseFloat(width!) + parseFloat(offset!) + 1);
    expect(theme.some((b) => b.declarations.has("--spacing-fade"))).toBe(true);
    const fadeEnd = blocks.filter((b) => b.head === "@theme inline" && b.parents.length === 0)
      .flatMap((b) => b.declarations.get("--spacing-fade-end") ?? []);
    expect(fadeEnd).toEqual(["calc(var(--spacing-fade) + var(--spacing) * 2)"]);
  });

  it("renders the fade at its token height and gives every fade end clearance", () => {
    const fade = jsx.find(({ tree }) => tree.fileName === "ui/ScrollFade.tsx")!;
    expect(fade.tags.filter((node) => attribute(node, "data-scroll-fade")).map(classes)).toEqual([expect.arrayContaining(["h-fade"])]);
    const component = fade.tree.statements.find((node) => ts.isFunctionDeclaration(node) && node.name?.text === "ScrollFade") as ts.FunctionDeclaration;
    expect(component.parameters.map((param) => param.getText()).join(" ")).not.toMatch(/\bheight\b/);
    const missing = jsx.filter(({ tags }) => tags.some((node) => node.tagName.getText() === "ScrollFade")
      && !tags.some((node) => classes(node).includes("pb-fade-end")));
    expect(missing.map(({ tree }) => tree.fileName)).toEqual([]);
  });

  it("keeps the transcript clear of the fade and all dock room below the transcript", () => {
    const chat = jsx.find(({ tree }) => tree.fileName === "views/chat/ChatView.tsx")!;
    const classAt = (name: string) => {
      const tags = chat.tags.filter((node) => attribute(node, name));
      expect(tags, name).toHaveLength(1);
      return classes(tags[0]);
    };
    expect(classAt("data-chat-scroll")).toContain("scroll-pb-fade-end");
    expect(classAt("data-chat-content").map(utility).filter((name) => name.startsWith("pb-"))).toEqual(["pb-fade-end"]);
    const dock = classAt("data-chat-dock-above");
    expect(dock).toEqual(expect.arrayContaining(["p-ring", "-mx-ring", "-mb-ring"]));
    expect(dock.map(utility).filter((name) => /^-m(?:t|y)?-/.test(name))).toEqual([]);
  });

  it("does not add a second or inert hand-rolled focus ring", () => {
    const handRolled = /(?:focus|focus-visible):(?:ring[\w-]*|-?outline-(?!none\b)[\w-]+|outline(?=[\s"'`]))/g;
    const violations = sources.flatMap((tree) => [...tree.text.matchAll(handRolled)].map((match) => {
      const { line } = tree.getLineAndCharacterOfPosition(match.index);
      return `${tree.fileName}:${line + 1} ${match[0]}`;
    }));
    expect(violations).toEqual([]);
  });

  it("truncates text, not an ancestor of an interactive control", () => {
    const violations = jsx.flatMap(({ tags }) => tags.filter((node) => classes(node).some((value) => /^(?:truncate|line-clamp-\d+|text-ellipsis)$/.test(utility(value)))
      && hasInteractiveDescendant(node)).map(location));
    expect(violations).toEqual([]);
  });

  it("makes focus-ring room explicit for direct interactive children of clip boxes", () => {
    const violations = jsx.flatMap(({ tags }) => tags.flatMap((node) => {
      const parent = classes(node);
      const overflow = parent.filter((value) => /^overflow-(?:hidden|auto|x-auto|y-auto)$/.test(utility(value)));
      if (!overflow.length) return [];
      return directChildren(node).filter(interactive).filter((child) => {
        const tokens = classes(child);
        return overflow.some((value) => hasRingInClip(child, tokens, value) && !providesRoom(parent, tokens, value));
      }).map((child) => `${location(node)} clips ${location(child)}`);
    }));
    expect(violations).toEqual([]);
  });
});
