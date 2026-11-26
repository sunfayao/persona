import { useMemo, useState } from "react";
import { t } from "../i18n";
import { errorText } from "../lib/api";
import { foldedState, rowKey } from "../lib/child-sessions";
import { sessionTitle } from "../lib/format";
import { defaultManager } from "../lib/manager-choice";
import { retryFullAgentSessions, useFullAgentSessions } from "../lib/sessions";
import { getState, refreshTodos } from "../lib/store";
import { attachUnassignedTodoAgent, type Todo } from "../lib/todos";
import type { Machine, Session } from "../lib/types";
import { Button, cx, Dialog, Loading, toast } from "../ui/atoms";
import { SearchField } from "../ui/SearchField";
import { StatusMark } from "../ui/StatusMark";

export function TodoAgentPicker({ todo, machines, onClose }: { todo: Todo; machines: Machine[]; onClose: () => void }) {
  const { sessions, loaded, error } = useFullAgentSessions();
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<Map<string, Session>>(() => new Map());
  const [failures, setFailures] = useState<ReadonlyMap<string, string>>(new Map());
  const [busy, setBusy] = useState(false);

  const names = useMemo(() => new Map(machines.map((machine) => [machine.id, machine.name])), [machines]);
  const attached = useMemo(() => new Set((todo.agents ?? []).filter((a) => !a.released_at).map((a) => rowKey(a.machine_id, a.session_id))), [todo.agents]);
  const needle = query.trim().toLocaleLowerCase();
  const manager = todo.manager ?? defaultManager();
  const matches = useMemo(() => sessions.filter((s) =>
    (s.manager ?? defaultManager()) === manager &&
    !s.archived && !s.starting && !s.folded_under && !attached.has(rowKey(s.machine_id, s.session_id)) &&
    s.todo?.id !== todo.id && [sessionTitle(s), s.space_name, s.cwd, names.get(s.machine_id)].some((word) => word?.toLocaleLowerCase().includes(needle)),
  ), [attached, manager, names, needle, sessions, todo.id]);

  function toggle(key: string, session: Session) {
    setSelected((current) => {
      const next = new Map(current);
      if (next.has(key)) next.delete(key);
      else next.set(key, session);
      return next;
    });
  }

  async function attach() {
    if (busy || selected.size === 0) return;
    setBusy(true);
    const remaining = new Map<string, Session>();
    const errors = new Map<string, string>();
    // Keep each result. A retry resubmits only failed links, including a receipt lost before the reply arrived.
    for (const [key, session] of selected) {
      try {
        await attachUnassignedTodoAgent(todo.id, session.machine_id, session.session_id);
      } catch (e) {
        remaining.set(key, session);
        errors.set(key, errorText(e));
      }
    }
    const count = selected.size - remaining.size;
    if (count) toast(t("todos.link_agents_success", { count }), "ok");
    // A list refresh can also show an already-committed link whose response was lost.
    await refreshTodos().catch(() => {});
    const known = getState().todos.find((item) => item.id === todo.id);
    const confirmed = new Set(known?.agents?.filter((a) => !a.released_at).map((a) => rowKey(a.machine_id, a.session_id)));
    for (const key of remaining.keys()) {
      if (confirmed.has(key)) {
        remaining.delete(key);
        errors.delete(key);
      }
    }
    setSelected(remaining);
    setFailures(errors);
    setBusy(false);
    if (!remaining.size) onClose();
    else setQuery("");
  }

  return (
    <Dialog
      open
      onClose={() => { if (!busy) onClose(); }}
      title={t("todos.link_existing_agents")}
      footer={(
        <div className="flex items-center justify-end gap-2">
          <Button tone="quiet" disabled={busy} onClick={onClose}>{t("common.cancel")}</Button>
          <Button tone="accent" icon="link" busy={busy} disabled={busy || !selected.size} onClick={() => void attach()}>
            {t("todos.link_agents_submit", { count: selected.size })}
          </Button>
        </div>
      )}
    >
      <SearchField tone="inset" value={query} onChange={setQuery} placeholder={t("todos.link_agents_search")} />
      {failures.size > 0 && (
        <p role="alert" className="mt-3 text-meta text-red">
          {t("todos.link_agents_partial")} {failures.values().next().value}
        </p>
      )}
      {!loaded ? (
        error ? (
          <div className="mt-4 flex items-center justify-between gap-2 text-sm text-ink-2">
            <span role="alert">{error}</span>
            <Button tone="secondary" icon="retry" onClick={retryFullAgentSessions}>{t("todos.retry")}</Button>
          </div>
        ) : <div className="flex justify-center py-6"><Loading /></div>
      ) : (
        <div className="mt-3 flex flex-col gap-1">
          {matches.length === 0 && (
            <p className="py-4 text-center text-meta text-ink-3">{t(needle ? "todos.link_agents_no_match" : "todos.link_agents_empty")}</p>
          )}
          {matches.map((session) => {
            const key = rowKey(session.machine_id, session.session_id);
            const checked = selected.has(key);
            const assignment = session.todo ?? session.completed_todo;
            const elsewhere = assignment && assignment.id !== todo.id;
            const detail = elsewhere ? t("todos.link_agents_other_todo", { title: assignment.title }) :
              [names.get(session.machine_id), session.space_name].filter(Boolean).join(t("sidebar.list_separator"));
            return (
              <label key={key} className={cx("flex min-h-12 items-center gap-3 rounded-control px-3 py-2", checked ? "bg-accent-tint" : !elsewhere && "hover:bg-hover")}>
                <input
                  type="checkbox"
                  checked={checked}
                  disabled={busy || Boolean(elsewhere && !checked)}
                  onChange={() => toggle(key, session)}
                  className="size-4 shrink-0 accent-accent"
                />
                <StatusMark state={foldedState(session)} size="sm" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium text-ink">{sessionTitle(session)}</span>
                  {detail && <span className="block truncate text-meta text-ink-3">{detail}</span>}
                  {failures.has(key) && <span className="block text-meta text-red">{failures.get(key)}</span>}
                </span>
              </label>
            );
          })}
        </div>
      )}
    </Dialog>
  );
}
