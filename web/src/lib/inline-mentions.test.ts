import { describe, expect, it } from "vitest";
import { withContexts } from "./context-xml";
import { editInlineMentions, inlineMention, inlineMentionAt, messageMentionRefs } from "./inline-mentions";
import { composerDraft, composerEditText, joinComposerDrafts, splitComposerDraft } from "./skill-mentions";
import { parseUserText } from "./turns";
import type { ContextRef } from "./links";

const selected = (id: string, at: number, label = "@Review"): ContextRef =>
  inlineMention({ id, kind: "context", title: label.slice(1), url: `persona://session/m/${id}`, text: "Selection" }, label, at);

describe("inline mentions", () => {
  it("uses the caret to remove the first of two identical labels without retaining its hidden target", () => {
    const draft = "@Review @Review";
    const first = selected("first", 0);
    const second = selected("second", 8);
    const edited = editInlineMentions([first, second], draft, "@Review", 0);
    expect(edited.map((r) => r.url)).toEqual([second.url]);
    expect(edited[0].attrs?.inline_start).toBe("0");
    expect(editInlineMentions(edited, "@Review", "@Revew", 5)).toEqual([]);
  });

  it("normalizes wire offsets for Unicode and leading whitespace without changing draft offsets", () => {
    const text = "  😀 Compare @Review\nthen @Audit  ";
    const refs = [selected("audit", text.indexOf("@Audit"), "@Audit"), selected("review", text.indexOf("@Review"))];
    const body = "> earlier words\n\n" + text;
    const wire = withContexts(messageMentionRefs(refs, text, body.trimStart().length - text.length), body);
    const parsed = parseUserText(wire);
    expect(parsed.text).toBe(body.trim());
    expect(parsed.contexts.map((r) => r.url)).toEqual([refs[1].url, refs[0].url]);
    expect(parsed.contexts.every((r) => inlineMentionAt(r, parsed.text))).toBe(true);
    const edit = splitComposerDraft(composerEditText(parsed.contexts, parsed.text));
    expect(edit.refs.map((r) => r.url)).toEqual([refs[1].url, refs[0].url]);
    expect(splitComposerDraft(composerDraft(refs, text)).text).toBe(text);
  });

  it("preserves both targets and their positions across a stored draft append or an unsent restoration", () => {
    const a = composerDraft([selected("earlier", 2)], "  @Review");
    const b = composerDraft([selected("later", 3)], "😀 @Review");
    const merged = splitComposerDraft(joinComposerDrafts(a, b));
    expect(merged.text).toBe("  @Review\n😀 @Review");
    expect(merged.refs.map((r) => r.url)).toEqual(["persona://session/m/earlier", "persona://session/m/later"]);
    expect(merged.refs.map((r) => Number(r.attrs?.inline_start))).toEqual([2, 13]);
    expect(merged.refs.every((r) => inlineMentionAt(r, merged.text))).toBe(true);
    expect(splitComposerDraft(composerDraft(merged.refs, "Both labels removed")).refs).toEqual([]);
  });
});
