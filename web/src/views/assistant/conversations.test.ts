import { describe, expect, it } from "vitest";
import { conversationTitle } from "./conversations";

describe("titles", () => {
  it("calls an untitled one 新对话", () => {
    expect(conversationTitle({ title: "" })).toBe("新对话");
    expect(conversationTitle({ title: "Claude Code" })).toBe("新对话");
    expect(conversationTitle({ title: " 早报 " })).toBe("早报");
  });

  it("reads a chat titled by its <persona-context> tag: what was typed after it, else the tag's title, else 新对话", () => {
    expect(conversationTitle({ title: '<persona-context kind="context" title="飞书 · 发布群 &quot;周五&quot;" url="">' })).toBe('飞书 · 发布群 "周五"');
    expect(conversationTitle({ title: '<persona-context kind="context" title="A &amp; B &lt;x&gt;" url="">\n正文' })).toBe("A & B <x>");
    expect(conversationTitle({ title: '<persona-context kind="context" title="群" url="">\n正文\n</persona-context>\n\n帮我总结一下\n第二行' })).toBe("帮我总结一下");
    expect(conversationTitle({ title: '<persona-context kind="context" title="" url="">' })).toBe("新对话");
    expect(conversationTitle({ title: "<persona-context kind=" })).toBe("新对话");
    // a title that only mentions the tag further on is the user's own words
    expect(conversationTitle({ title: "什么是 <persona-context> 标签" })).toBe("什么是 <persona-context> 标签");
  });

  it("calls the main conversation Persona, whatever its first message was (#1313)", () => {
    expect(conversationTitle({ title: "我的邮箱里最近有哪些需要我处理或回复的邮件？挑重要的说。" }, true)).toBe("Persona");
    expect(conversationTitle({ title: "" }, true)).toBe("Persona");
  });
});
