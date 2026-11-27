// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it } from "vitest";
import { actionSummary, type ToolStep, type WorkGroupNode } from "../../lib/work-groups";
import { WorkGroup, type WorkProps } from "./WorkGroup";
import { forgetExpanded, openExpanded } from "./expanded";
import { presentAction } from "./ActionRow";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const tool = (id: string, name: string, input = {}): ToolStep => ({ kind: "tool", id, use: { id, seq: 1, role: "assistant", kind: "tool_use", tool_name: name, tool_use_id: id, tool_input: input }, images: [] });
const group = (id: string, tools: ToolStep[], live = false): WorkGroupNode => ({ kind: "group", id, live, children: tools.map(step => ({ kind: "step", step })) });

it("headers retain first-seen kind order, deduplicate read paths, combine edit kinds, and stop after three kinds", () => {
  const g = group("g", [tool("1", "Bash"), tool("2", "Read", { file_path: "src/example.ts" }), tool("3", "Read", { file_path: "src/example.ts" }), tool("4", "Edit"), tool("5", "Write"), tool("6", "Grep")]);
  expect(actionSummary(g)).toEqual({ text: "运行 1 条命令 · 读取 1 个文件 · 编辑 2 个文件", icon: "run" });
  expect(actionSummary(group("g", [tool("1", "Grep"), tool("2", "Read")])).icon).toBe("search");
  expect(actionSummary(group("g", [tool("1", "Edit", { file_path: "src/example.ts" }), tool("2", "Write", { file_path: "src/example.ts" })])).text).toBe("编辑 1 个文件");
  expect(actionSummary(group("g", [tool("1", "CustomTool")])).text).toContain("CustomTool");
});

it("command rows lead with the full command (including argv), search rows retain the query and target", () => {
  expect(presentAction(tool("1", "exec_command", { command: ["npm", "test"], description: "Run unit checks" }).use)).toEqual({ param: { text: "npm test", title: "Run unit checks" } });
  expect(presentAction(tool("2", "Grep", { pattern: "missing reply", path: "src/example.ts" }).use).param).toEqual({ text: "missing reply · src/example.ts", title: "missing reply · src/example.ts" });
});

it("a live stretch opened by hand stays open while it runs and closes when done; only the live stretch previews edits; jumps open it", () => {
  forgetExpanded();
  const host = document.createElement("div"); document.body.append(host);
  const root = createRoot(host);
  const edit = tool("edit", "Edit", { file_path: "src/example.ts", old_string: "old", new_string: "new" });
  const props: WorkProps = { mid: "m", sid: "s", scope: "actions:", expandFold: async () => {}, agentStatus: () => "completed" };
  const draw = (live: boolean) => act(() => root.render(<WorkGroup g={group("first", [edit], live)} p={props} />));
  const head = () => host.querySelector<HTMLButtonElement>("[data-work-group] > button")!;
  try {
    draw(true);
    expect(head().getAttribute("aria-expanded")).toBe("false");
    act(() => head().click());
    expect(head().getAttribute("aria-expanded")).toBe("true");
    expect(host.querySelectorAll("[data-live-edit-diff]")).toHaveLength(1);
    expect(host.querySelectorAll(".border-tool-rail")).toHaveLength(1);
    draw(false);
    expect(head().getAttribute("aria-expanded")).toBe("false");
    act(() => head().click());
    expect(head().getAttribute("aria-expanded")).toBe("true");
    expect(host.querySelector("[data-live-edit-diff]")).toBeNull();
    act(() => head().click());
    act(() => openExpanded(["actions:first"]));
    expect(host.querySelector('[data-tool-use="edit"]')).not.toBeNull();
  } finally { act(() => root.unmount()); host.remove(); forgetExpanded(); }
});
