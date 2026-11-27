import { useMemo } from "react";
import { t } from "../../i18n";
import { assistantEntries } from "../../lib/assistant-entries";
import { useFeed } from "../../lib/feed";
import { openAgent, openTask } from "../../lib/handoff";
import { defaultManager } from "../../lib/manager-choice";
import { stateName } from "../../lib/state-labels";
import { refreshTodos, useStore } from "../../lib/store";
import { reviewState, reportText } from "../../lib/supervision";
import { mostUrgentAgent, type SessionLookup, type Todo } from "../../lib/todos";
import { todoLook, waitOf } from "../../lib/todo-wait-reason";
import { Button, Loading } from "../../ui/atoms";
import { PanelHeader } from "../../ui/PanelHeader";
import { Empty } from "../assistant/parts";
import { NeedsYouBody, progressOf, statusOf, useTaskLookup } from "../assistant/Tasks";

export interface ConversationTasksMatch {
  mid?: string;
  linkedTodoIds?: ReadonlySet<string>;
}

export function conversationTasks(
  todos: readonly Todo[],
  sid: string,
  manager: string,
  fallbackManager: string,
  match?: ConversationTasksMatch,
) {
  const mid = match?.mid;
  const linkedTodoIds = match?.linkedTodoIds;
  return todos.filter((todo) => {
    if (linkedTodoIds?.has(todo.id)) return true;
    if (todo.agents?.some((agent) => agent.session_id === sid && (!mid || agent.machine_id === mid) && !agent.dismissed_at)) return true;
    if (todo.responsible?.kind === "session" && todo.responsible.session_id === sid &&
      (!mid || !todo.responsible.machine_id || todo.responsible.machine_id === mid)) return true;
    return (todo.manager ?? fallbackManager) === manager &&
      (todo.origin === sid || todo.supervision?.supervisor === sid);
  });
}

export function ConversationTasksPanel({ mid, sid, manager, onClose }: { mid?: string; sid: string; manager: string; onClose: () => void }) {
  const allTodos = useStore((s) => s.todos);
  const loaded = useStore((s) => s.todosLoaded);
  const error = useStore((s) => s.todosError);
  const lookup = useTaskLookup();
  const detailSession = useStore((s) => (mid ? s.sessions[mid]?.find((x) => x.session_id === sid) : undefined));
  const currentSession = mid ? lookup(mid, sid) : undefined;
  const linkedTodo = currentSession?.todo ?? detailSession?.todo;
  const completedTodo = currentSession?.completed_todo ?? detailSession?.completed_todo;
  const linkedTodoIds = useMemo(() => {
    const ids = new Set<string>();
    if (linkedTodo?.id) ids.add(linkedTodo.id);
    if (completedTodo?.id) ids.add(completedTodo.id);
    return ids.size ? ids : undefined;
  }, [linkedTodo?.id, completedTodo?.id]);
  const todos = useMemo(
    () => conversationTasks(allTodos, sid, manager, defaultManager(), { mid, linkedTodoIds }),
    [allTodos, sid, manager, mid, linkedTodoIds],
  );
  const fallbackLinks = useMemo(() => {
    const seen = new Set(todos.map((todo) => todo.id));
    const out: { id: string; title: string; completed: boolean }[] = [];
    if (linkedTodo && !seen.has(linkedTodo.id)) {
      seen.add(linkedTodo.id);
      out.push({ id: linkedTodo.id, title: linkedTodo.title, completed: false });
    }
    if (completedTodo && !seen.has(completedTodo.id)) {
      seen.add(completedTodo.id);
      out.push({ id: completedTodo.id, title: completedTodo.title, completed: true });
    }
    return out;
  }, [todos, linkedTodo, completedTodo]);
  const plans = useFeed((s) => s.plans);
  const interactions = useStore((s) => s.interactions);
  const deferred = useFeed((s) => s.later);
  const needsYou = useMemo(() => assistantEntries({
    conversation: sid, todos, sessions: [], interactions, plans, feed: [], lookup, deferred,
    cards: todos.map((todo) => ({
      id: `task:${todo.id}`, todo_id: todo.id, at: todo.created_at, updated_at: todo.updated_at, state: "not_started",
    })),
  }).needsYou, [sid, todos, interactions, plans, lookup, deferred]);
  const waiting = new Set(needsYou.flatMap((item) => item.kind === "task" ? [item.todo.id] : []));
  const rows = todos.filter((todo) => !waiting.has(todo.id))
    .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at));
  const empty = rows.length === 0 && needsYou.length === 0 && fallbackLinks.length === 0;

  return <>
    <PanelHeader icon="todo" title={t("sidepanel.conversation_tasks")} onClose={onClose} />
    <div className="min-h-0 flex-1 overflow-y-auto p-3">
      {!loaded && empty ? <Loading /> : error && empty ?
        <Empty icon="todo" title={t("todos.load_failed")} action={
          <Button tone="secondary" icon="retry" onClick={() => void refreshTodos()}>{t("todos.retry")}</Button>
        }>{error}</Empty> : empty ? <p className="text-sm text-ink-3">{t("sidepanel.conversation_tasks_empty")}</p> :
        <ul className="flex flex-col gap-2">
          {needsYou.map((item) => <li key={item.key}>
            <NeedsYouBody item={item} />
            {item.kind === "task" && <TaskAgentLink todo={item.todo} lookup={lookup} currentMid={mid} currentSid={sid} />}
          </li>)}
          {rows.map((todo) => {
            const status = statusOf(todo, lookup, plans);
            const session = mostUrgentAgent(todo, lookup)?.session;
            const summary = reportText(todo.supervision) || progressOf(session)?.text || todo.start_error;
            const label = reviewState(todo) === "parked" ? t("assistant-tasks.review_parked") : todoLook(todo, status, waitOf(todo, status, lookup)).label;
            return <li key={todo.id} className="rounded-control bg-field p-3">
              <div className="flex items-baseline justify-between gap-2">
                <button type="button" className="touch-hit min-w-0 truncate text-left text-sm font-semibold hover:underline" title={todo.title} onClick={() => openTask(todo.id)}>{todo.title}</button>
                <span className="shrink-0 text-xs text-ink-2">{label}</span>
              </div>
              {summary && <p className="truncate text-xs text-ink-2" title={summary}>{summary}</p>}
              <TaskAgentLink todo={todo} lookup={lookup} currentMid={mid} currentSid={sid} />
            </li>;
          })}
          {fallbackLinks.map((link) => (
            <li key={link.id} className="rounded-control bg-field p-3">
              <div className="flex items-baseline justify-between gap-2">
                <button type="button" className="touch-hit min-w-0 truncate text-left text-sm font-semibold hover:underline" title={link.title} onClick={() => openTask(link.id)}>{link.title}</button>
                <span className="shrink-0 text-xs text-ink-2">{link.completed ? stateName("done") : t("chat-todostrip.no_detail")}</span>
              </div>
            </li>
          ))}
        </ul>}
    </div>
  </>;
}


function TaskAgentLink({ todo, lookup, currentMid, currentSid }: { todo: Todo; lookup: SessionLookup; currentMid?: string; currentSid?: string }) {
  const isSelf = (agent: { machine_id: string; session_id: string }) =>
    Boolean(currentSid) && agent.session_id === currentSid && (!currentMid || agent.machine_id === currentMid);
  const urgent = mostUrgentAgent(todo, lookup)?.agent;
  const agent = (urgent && !isSelf(urgent) ? urgent : undefined) ??
    todo.agents?.find((item) => item.session_id && !item.released_at && !isSelf(item)) ??
    todo.agents?.find((item) => item.session_id && !isSelf(item));
  if (!agent) return null;
  return <button type="button" className="touch-hit text-xs text-accent hover:underline"
    onClick={() => openAgent(agent.machine_id, agent.session_id)}>{t("sidepanel.agent")}</button>;
}
