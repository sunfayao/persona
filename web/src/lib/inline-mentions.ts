import type { ContextRef } from "./links";

type MentionAttrs = { attrs?: Record<string, string> };

function anchor(ref: MentionAttrs) {
  const label = ref.attrs?.inline_label;
  const at = ref.attrs?.inline_start;
  if (!label?.startsWith("@") || label.length < 2 || !at || !/^\d+$/.test(at)) return;
  const start = Number(at);
  if (Number.isSafeInteger(start)) return { label, start, end: start + label.length };
}

export function isInlineMention(ref: MentionAttrs): boolean {
  return !!anchor(ref);
}

export function inlineMentionAt(ref: MentionAttrs, text: string): boolean {
  const a = anchor(ref);
  return Boolean(a && text.slice(a.start, a.end) === a.label);
}

/** Only picked session references with intact UTF-16 ranges get a highlight. */
export function sessionMentionRanges(refs: readonly (MentionAttrs & { url?: string })[], text: string, offset = 0) {
  const ranges = refs.flatMap((ref) => {
    const a = anchor(ref);
    if (!a || !ref.url?.startsWith("persona://session/")) return [];
    const start = a.start - offset;
    const end = a.end - offset;
    return start >= 0 && text.slice(start, end) === a.label ? [{ start, end, url: ref.url }] : [];
  }).sort((a, b) => a.start - b.start);
  let end = 0;
  return ranges.filter((range) => {
    if (range.start < end) return false;
    end = range.end;
    return true;
  });
}

export function inlineMention(ref: ContextRef, label: string, start: number): ContextRef {
  return { ...ref, attrs: { ...ref.attrs, inline_label: label, inline_start: String(start) } };
}

export function shiftInlineMentions(refs: ContextRef[], offset: number): ContextRef[] {
  if (!offset) return refs;
  return refs.flatMap((r) => {
    const a = anchor(r);
    return !a ? [r] : a.start + offset < 0 ? [] : [inlineMention(r, a.label, a.start + offset)];
  });
}

/** Only intact selections travel. Use browser UTF-16 offsets, like textarea.selectionStart. */
export function messageMentionRefs(refs: ContextRef[], draft: string, offset = 0): ContextRef[] {
  const attached = refs.filter((r) => !isInlineMention(r));
  const inline = refs.filter((r) => inlineMentionAt(r, draft))
    .sort((a, b) => anchor(a)!.start - anchor(b)!.start);
  return [...attached, ...shiftInlineMentions(inline, offset)];
}

/** Keep only unchanged mentions before or after a single text edit. The post-edit caret disambiguates edits to
 * repeated visible labels. Programmatic changes without a caret use their common prefix and suffix. */
export function editInlineMentions(refs: ContextRef[], before: string, after: string, caret?: number): ContextRef[] {
  if (!refs.some(isInlineMention) || before === after) return refs;
  const tail = caret === undefined ? null : after.slice(caret);
  let oldEnd = before.length;
  let newEnd = after.length;
  if (tail !== null && before.endsWith(tail)) {
    oldEnd -= tail.length;
    newEnd -= tail.length;
  }
  let start = 0;
  while (start < oldEnd && start < newEnd && before[start] === after[start]) start++;
  if (tail === null || !before.endsWith(tail)) {
    while (oldEnd > start && newEnd > start && before[oldEnd - 1] === after[newEnd - 1]) {
      oldEnd--;
      newEnd--;
    }
  }
  return refs.flatMap((r) => {
    const a = anchor(r);
    if (!a) return [r];
    if (!inlineMentionAt(r, before)) return [];
    if (a.end <= start) return [r];
    if (a.start < oldEnd) return [];
    const next = newEnd === oldEnd ? r : inlineMention(r, a.label, a.start + newEnd - oldEnd);
    return inlineMentionAt(next, after) ? [next] : [];
  });
}
