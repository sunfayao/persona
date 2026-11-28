// Presentation helpers used only by the lazily loaded to-do detail and link views. Keeping them out of the shared
// first-screen to-do module avoids sending their XML, source-label and link-resolution code before a view needs it.
import { api, enc } from "./api";
import { MANAGER_HEADER } from "./manager-choice";
import { t } from "../i18n";
import { isCancelled, type SessionLookup, type Todo, type TodoAgent, type TodoSpace } from "./todos";
import type { AgentLifecyclePage } from "./types";

/** Why the to-do was cancelled; older hubs carry it in done_reason. */
export function cancellationReason(todo: Todo): string {
  return isCancelled(todo) ? (todo.cancelled_reason || todo.done_reason || "") : "";
}

/**
 * Format agent source label:
 * - Spawned from project: "With project <space>" (resolved from via, or agent's own session lookup as fallback);
 *   "With project" if neither is available;
 * - Spawned from parent agent: "Spawned by <parent title>"; "Spawned" if parent is not in session list;
 * - Same Feishu chat as most of the todo's botmux agents: "Same chat";
 * - A supervisor Agent's to-do (lib/todo-supervisor.ts): "Supervisor" on its supervisor, "Worker" on a worker's own
 *   to-do, "Reviewer" on an independent reviewer;
 * - Manual: undefined (not labeled).
 */
export function formatAgentSource(
  agent: TodoAgent,
  _spaces?: TodoSpace[],
  lookup?: SessionLookup,
): string | undefined {
  if (!agent.source || agent.source === "manual") return undefined;

  if (agent.source === "space") {
    let spaceName = "";
    if (agent.via) {
      const slash = agent.via.indexOf("/");
      if (slash >= 0) {
        spaceName = agent.via.slice(slash + 1);
      } else {
        spaceName = agent.via;
      }
    }
    if (!spaceName && lookup) {
      const s = lookup(agent.machine_id, agent.session_id);
      if (s?.space_name) spaceName = s.space_name;
    }
    return spaceName ? t("lib-todos.with_project_named", { name: spaceName }) : t("lib-todos.with_project");
  }

  if (agent.source === "spawned") {
    if (agent.via && lookup) {
      const slash = agent.via.indexOf("/");
      if (slash >= 0) {
        const mid = agent.via.slice(0, slash);
        const sid = agent.via.slice(slash + 1);
        const parent = lookup(mid, sid);
        if (parent?.title) {
          return t("lib-todos.spawned_by", { title: parent.title });
        }
      }
    }
    return t("lib-todos.spawned");
  }

  if (agent.source === "chat") return t("lib-todos.same_chat");
  if (agent.source === "leader") return t("lib-todos.source_leader");
  if (agent.source === "worker") return t("lib-todos.source_worker");
  if (agent.source === "review") return t("lib-todos.source_review");

  return undefined;
}

export function fetchAgentLifecycle(id: string, manager: string | undefined, before: string | undefined, signal: AbortSignal) {
  const next = before ? `?before=${enc(before)}` : "";
  const headers = manager ? { [MANAGER_HEADER]: manager } : undefined;
  return api<AgentLifecyclePage>(`/todos/${enc(id)}/lifecycle${next}`, { headers, signal });
}
