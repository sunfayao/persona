import { CANONICAL_TOOL_KINDS } from "./tool-registry-wire";
// Unified tool registry — maps legacy tool names, CLI labels, and MCP patterns to
// generated ToolKind values from turn-model-wire.ts. This is the single source of
// truth for tool display metadata; consumers never switch on tool_name themselves.
//
// Design goals:
//   - Every known legacy tool name resolves to exactly one ToolKind.
//   - Unknown tool names resolve to ToolKind "other" with a generic label.
//   - MCP tools (mcp_persona_*, mcp_jarvis_*) resolve to ToolKind "mcp".
//   - Shell executors (Bash, exec_command, shell, PowerShell, herdr.exec) resolve to "execute".
//   - Read-only (Read, view_file, ...) → "read"; edit/write → "edit"/"write".

import type { ToolFacet, ToolKind } from "./turn-model-wire";
import { TOOL_KINDS } from "./turn-model-wire";

/** Canonical display metadata for a tool, derived from its ToolKind and the raw name. */
export interface ToolDescriptor {
  /** The resolved canonical ToolKind from the unified facet model. */
  kind: ToolKind;
  /** Human-readable short title (no i18n — brand-labeling text lives here). */
  title: string;
  /** Display icon/variant key for the caller's component system. */
  variant: ToolVariant;
  /** Whether this tool is a command-executor (shell, PowerShell, ...). */
  isCommand: boolean;
  /** True for MCP tools matched by name pattern. */
  isMcp: boolean;
  /** Original raw tool_name passed in (for diagnostics). */
  rawName: string;
}

export type ToolVariant =
  | "execute"
  | "read"
  | "edit"
  | "write"
  | "delete"
  | "move"
  | "search"
  | "fetch"
  | "web_search"
  | "plan"
  | "ask"
  | "mode"
  | "agent"
  | "message"
  | "task"
  | "schedule"
  | "workflow"
  | "artifact"
  | "image"
  | "mcp"
  | "skill"
  | "other";

// ── Legacy tool name → ToolKind mappings (case-insensitive key) ───────────────

const COMMAND_EXECUTORS = new Set([
  "bash",
  "exec_command",
  "shell",
  "powershell",
  "cmd",
  "command",
  "herdr_exec",
  "execute_command",
  "run_command",
  "runBash",
]);

const READ_TOOLS = new Set([
  "read",
  "notebookread",
  "read_file",
  "view_file",
  "cat",
  "ls",
  "list",
  "glob",
  "grep",
  "rg",
  "list_directory",
  "read_text_file",
  "read_image",
]);

const EDIT_TOOLS = new Set([
  "edit",
  "edit_file",
  "replace",
  "replace_file",
  "apply_patch",
  "patch",
  "str_replace_editor",
  "edit_heredoc",
]);

const WRITE_TOOLS = new Set([
  "write",
  "write_file",
  "create_file",
  "new_file",
  "save",
  "append",
]);

const DELETE_TOOLS = new Set([
  "delete",
  "delete_file",
  "remove",
  "rm",
  "unlink",
]);

const MOVE_TOOLS = new Set([
  "move",
  "rename",
  "mv",
  "copy",
  "cp",
  "duplicate",
]);

const SEARCH_TOOLS = new Set([
  "search",
  "search_code",
  "search_in_files",
  "find",
  "fd",
  "ripgrep",
]);

const FETCH_TOOLS = new Set([
  "fetch",
  "web_fetch",
  "curl",
  "download",
  "http_get",
]);

const WEB_SEARCH_TOOLS = new Set([
  "web_search",
  "search_web",
  "web_search_exa",
  "search_online",
]);

const PLAN_TOOLS = new Set([
  "plan",
  "todo_write",
  "task_create",
  "taskupdate",
  "task_update",
  "task_write",
  "checklist",
]);

const ASK_TOOLS = new Set([
  "ask",
  "ask_user",
  "request_user_input",
  "get_user_input",
  "human_input",
  "dialog",
]);

const MODE_TOOLS = new Set([
  "mode",
  "mode_switch",
  "set_mode",
  "set_permission_mode",
  "approve",
  "permission",
]);

const AGENT_TOOLS = new Set([
  "agent",
  "agent_call",
  "call_agent",
  "subagent",
  "spawn",
  "spawn_subagent",
  "codex",
  "herdr",
  "agy",
  "trae",
  "traex",
  "run_agent",
  "invoke_agent",
]);

const MESSAGE_TOOLS = new Set([
  "message",
  "send_message",
  "a2a_send",
  "notify",
  "slack",
  "lark",
  "feishu",
  "discord",
]);

const TASK_TOOLS = new Set([
  "task",
  "background",
  "background_task",
  "shell_task",
  "monitor_task",
  "run_background",
  "launch",
]);

const SCHEDULE_TOOLS = new Set([
  "cronlist",
  "schedule",
  "cron",
  "timer",
  "add_cron",
  "set_timer",
]);

const WORKFLOW_TOOLS = new Set([
  "workflow",
  "workflow_run",
  "run_workflow",
  "pipeline",
]);

const ARTIFACT_TOOLS = new Set([
  "artifact",
  "write_artifact",
  "publish",
  "create_artifact",
]);

const IMAGE_TOOLS = new Set([
  "view_image",
  "image",
  "screenshot",
  "read_image",
  "browser_snapshot",
  "capture",
]);

const SKILL_TOOLS = new Set([
  "skill",
  "run_skill",
  "invoke_skill",
  "plugin",
]);

/** MCP tool name pattern: mcp_[server_name]_[tool_name], optional underscore+digest suffix. */
const MCP_RE = /^mcp_(?:persona|jarvis|[a-z][a-z0-9_]*?)(?:_[0-9a-fA-F]{8,16})?_([a-z][a-z0-9_]*)$/;

/** Normalise CLI MCP tool names to the double-underscore form. When the name is not an MCP tool
 *  or already has the canonical form, return it unchanged. */
export function canonicalizeMcpToolName(name?: string): string | undefined {
  if (!name || !name.startsWith("mcp_") || name.startsWith("mcp__")) return name;
  const m = MCP_RE.exec(name);
  return m ? `mcp__persona__${m[1]}` : name;
}

/** Resolve a legacy tool name to a canonical ToolKind. */
export function toolKindFor(rawName: string): ToolKind {
  if (Object.hasOwn(CANONICAL_TOOL_KINDS, rawName)) return CANONICAL_TOOL_KINDS[rawName];
  const lower = rawName.toLowerCase();
  if (lower.startsWith("mcp__") || MCP_RE.test(rawName)) return "mcp";
  if (COMMAND_EXECUTORS.has(lower)) return "execute";
  if (READ_TOOLS.has(lower)) return "read";
  if (EDIT_TOOLS.has(lower)) return "edit";
  if (WRITE_TOOLS.has(lower)) return "write";
  if (DELETE_TOOLS.has(lower)) return "delete";
  if (MOVE_TOOLS.has(lower)) return "move";
  if (SEARCH_TOOLS.has(lower)) return "search";
  if (FETCH_TOOLS.has(lower)) return "fetch";
  if (WEB_SEARCH_TOOLS.has(lower)) return "web_search";
  if (PLAN_TOOLS.has(lower)) return "plan";
  if (ASK_TOOLS.has(lower)) return "ask";
  if (MODE_TOOLS.has(lower)) return "mode";
  if (AGENT_TOOLS.has(lower)) return "agent";
  if (MESSAGE_TOOLS.has(lower)) return "message";
  if (TASK_TOOLS.has(lower)) return "task";
  if (SCHEDULE_TOOLS.has(lower)) return "schedule";
  if (WORKFLOW_TOOLS.has(lower)) return "workflow";
  if (ARTIFACT_TOOLS.has(lower)) return "artifact";
  if (IMAGE_TOOLS.has(lower)) return "image";
  if (SKILL_TOOLS.has(lower)) return "skill";
  return "other";
}

/** Human-readable titles for each ToolKind (English). */
const KIND_TITLES: Record<ToolKind, string> = {
  execute: "Execute",
  read: "Read",
  edit: "Edit",
  write: "Write",
  delete: "Delete",
  move: "Move",
  search: "Search",
  fetch: "Fetch",
  web_search: "Web search",
  plan: "Plan",
  ask: "Ask",
  mode: "Mode",
  agent: "Agent",
  message: "Message",
  task: "Task",
  schedule: "Schedule",
  workflow: "Workflow",
  artifact: "Artifact",
  image: "Image",
  mcp: "MCP",
  skill: "Skill",
  other: "Tool",
};

const variantOf = (kind: ToolKind): ToolVariant =>
  (TOOL_KINDS.includes(kind as any) ? kind : "other") as ToolVariant;

const isCommandKind = (kind: ToolKind): boolean => kind === "execute";

/** Produce a full tool descriptor from a raw tool name. */
export function describeTool(rawName: string): ToolDescriptor {
  const kind = toolKindFor(rawName);
  return {
    kind,
    title: KIND_TITLES[kind] ?? KIND_TITLES.other,
    variant: variantOf(kind),
    isCommand: isCommandKind(kind),
    isMcp: kind === "mcp",
    rawName,
  };
}

/** Given an Item's legacy tool fields and an optional ToolFacet, return the canonical descriptor. */
export function describeToolFromFacet(facet: ToolFacet | undefined, rawName: string | undefined): ToolDescriptor {
  if (facet) {
    const kind = facet.kind;
    return {
      kind,
      title: facet.title ?? KIND_TITLES[kind as ToolKind] ?? KIND_TITLES.other,
      variant: variantOf(kind),
      isCommand: isCommandKind(kind),
      isMcp: kind === "mcp",
      rawName: rawName ?? facet.name,
    };
  }
  return describeTool(rawName ?? "");
}

/** Legacy tool-name → short chip label, used by turns.ts toolChip() and ActionRow.tsx.
 *  Delegates to the registry so callers never switch on tool_name themselves. */
export function toolChipLabel(rawName: string | undefined): string {
  if (!rawName) return "Tool";
  const d = describeTool(rawName);
  // Prefer the uppercase of the tool's name if it's short-ish, else the kind title.
  if (rawName.length <= 16 && !rawName.includes("_")) return rawName;
  return d.title;
}
