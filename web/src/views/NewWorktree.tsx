import { useEffect, useId, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { t } from "../i18n";
import { credentialOwner, errorText, subscribeCredentialOwner } from "../lib/api";
import { useKnownDefault } from "../lib/manager-choice";
import { openNewAgent } from "../lib/new-agent";
import { useStore } from "../lib/store";
import { useWorkspaceCatalog, workspaceTarget } from "../lib/workspace-catalog";
import type { WorkspaceTarget } from "../lib/workspace-target";
import { createdWorkspace, creationSession, sourceKey, type WorktreeCreationSession } from "../lib/worktree-creation";
import { openCreated, prepareWorktree, suggestedPath, typeBranch, typePath, worktreeAdmission,
  worktreeProblem, WORKTREE_COPY, type WorktreePrepare, type WorktreeRequest, type WorktreeTarget } from "../lib/worktree";
import { Button, cx, Dialog, Spinner } from "../ui/atoms";

export function NewWorktree({ request, onClose }: { request: WorktreeRequest; onClose: () => void }) {
  const [loaded, setLoaded] = useState<{ request: WorktreeRequest; session: WorktreeCreationSession }>();
  const session = loaded?.request === request ? loaded.session : undefined;
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    creationSession(request).then((value) => { if (active) setLoaded({ request, session: value }); }, (e) => { if (active) setError(errorText(e)); });
    return () => { active = false; };
  }, [request]);
  return session ? <Composer session={session} request={request} onClose={onClose} /> :
    <Dialog open title={t("worktree.dialog_title")} onClose={onClose} footer={<Button onClick={onClose}>{t("common.cancel")}</Button>}>
      {error ? <p role="alert" className="text-sm text-red">{error}</p> : <p role="status" className="flex gap-2 text-sm text-ink-3"><Spinner />{WORKTREE_COPY.reading}</p>}
    </Dialog>;
}

type Read = { key: string; info?: WorktreePrepare; error?: string };
function Composer({ session, request, onClose }: { session: WorktreeCreationSession; request: WorktreeRequest; onClose: () => void }) {
  const { draft, intent, storageError } = useSyncExternalStore(session.subscribe, session.getSnapshot, session.getSnapshot);
  const owner = useSyncExternalStore(subscribeCredentialOwner, credentialOwner, credentialOwner);
  useKnownDefault();
  const machines = useStore((s) => s.machines);
  const catalog = useWorkspaceCatalog(true);
  const source = draft.source;
  const key = sourceKey(source);
  const [read, setRead] = useState<Read>({ key });
  const [readVersion, setReadVersion] = useState(0);
  const [error, setError] = useState("");
  const alive = useRef(true);
  const sourceGeneration = useRef(0);
  const notified = useRef("");
  const launched = useRef(false);
  const validOwner = owner === source.owner && worktreeAdmission(source);
  const locked = Boolean(intent && intent.phase !== "refused");
  const sending = intent?.phase === "sending";
  const info = read.key === key ? read.info : undefined;
  const actual = intent && createdWorkspace(intent);
  const machine = machines.find((m) => m.id === source.mid);
  const offline = machine?.online === false;
  const startAgent = () => {
    if (actual && !launched.current && worktreeAdmission(source)) { launched.current = true; openNewAgent({ workspace: actual }); onClose(); }
  };

  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; sourceGeneration.current++; };
  }, []);
  useEffect(() => {
    const generation = ++sourceGeneration.current;
    const controller = new AbortController();
    if (!validOwner) return () => controller.abort();
    prepareWorktree(source, { signal: controller.signal }).then((value) => {
      if (!controller.signal.aborted && generation === sourceGeneration.current && worktreeAdmission(source)) setRead({ key, info: value });
    }, (e) => {
      if (!controller.signal.aborted && generation === sourceGeneration.current && worktreeAdmission(source)) setRead({ key, error: worktreeProblem(e, WORKTREE_COPY.readFailed).text });
    });
    return () => controller.abort();
  }, [source, key, readVersion, validOwner]);
  useEffect(() => {
    if (!intent?.created || !actual || !validOwner || notified.current === intent.id) return;
    notified.current = intent.id;
    request.changed();
    request.onCreated?.(intent.created, actual);
  }, [intent, actual, validOwner, request]);

  const catalogRows = catalog.owner === owner && catalog.manager === source.manager ? catalog.workspaces : [];
  const gitRows = catalogRows.filter((w) => w.source === "herdr" && !isFolder(w.workspace_kind) && w.available !== false && w.online !== false);
  const choices: { row: import("../lib/workspace-catalog").HostWorkspace; target: WorkspaceTarget }[] = gitRows.map((w) => ({ row: w, target: workspaceTarget(w, catalog.owner, catalog.manager) }));
  if (!choices.some((c) => sourceKey(toSource(c.target)) === key)) choices.unshift({ row: { id: "", machine_id: source.mid, source: source.host || "", source_id: source.workspaceId,
    title: source.label || info?.repoName || source.cwd || "", cwd: source.cwd || "", project_name: info?.repoName },
    target: { machineId: source.mid, host: source.host || "", workspaceId: source.workspaceId, cwd: source.cwd || "", label: source.label, owner: source.owner, manager: source.manager } });
  const sourceRow = choices.find((c) => sourceKey(toSource(c.target)) === key)?.row;
  const projectOf = (row: typeof sourceRow) => row?.project_id || row?.project_path || row?.project_name || row?.cwd || "";
  const project = projectOf(sourceRow);
  const availableMachines = machines.filter((m) => choices.some((c) => c.target.machineId === m.id));
  const sameHost = choices.filter((c) => c.target.machineId === source.mid && c.target.host === (source.host || ""));
  const projects = [...new Map(sameHost.map((c) => [projectOf(c.row), c.row])).entries()];

  function selectSource(next: WorkspaceTarget) {
    sourceGeneration.current++;
    setError("");
    session.update({ source: toSource(next) });
  }
  const submit = () => {
    if (!validOwner || sending || offline || intent?.phase === "created") return;
    setError("");
    try { void session.submit(info).catch((e) => { if (alive.current) setError(errorText(e)); }); }
    catch (e) { setError(errorText(e)); }
  };

  return <Dialog open onClose={onClose} title={intent?.created ? t("worktree.created") : t("worktree.dialog_title")}
    footer={<>
      <Button tone="quiet" onClick={onClose}>{t(intent?.created ? "worktree-composer.done" : "common.cancel")}</Button>
      {intent?.created ? <Button tone="primary" disabled={!validOwner} onClick={() => { openCreated(source.mid, intent.created!); onClose(); }}>{t("worktree-composer.open")}</Button> :
        <Button tone="primary" busy={sending} disabled={!validOwner || offline || (!info && !intent) || Boolean(read.error && !intent)} onClick={submit}>
          {t(locked ? "worktree-composer.check_result" : "worktree-composer.create")}</Button>}
    </>}>
    {!validOwner ? <Notice error>{t("worktree.owner_changed")}</Notice> : intent?.created && actual ? <div className="flex flex-col gap-3 text-sm">
      <p className="font-medium text-ink">{actual.label}</p>
      <p className="break-all font-mono text-ink-2">{intent.created.path}</p>
      <p className="break-all text-ink-2">{intent.created.branch}</p>
      <p className="text-ink-3">{t("worktree-composer.created_help")}</p>
      <Button disabled={launched.current} onClick={startAgent}>{t("worktree-composer.start_agent")}</Button>
      <Button tone="quiet" onClick={() => { notified.current = ""; session.fresh(); }}>{t("worktree-composer.create_another")}</Button>
    </div> : <form className="flex flex-col gap-3 text-sm text-ink-2" onSubmit={(e) => { e.preventDefault(); submit(); }}>
      <fieldset disabled={locked || !validOwner} className="flex min-w-0 flex-col gap-3">
        <Select label={t("worktree-composer.machine")} value={source.mid} onChange={(v) => { const choice = choices.find((c) => c.target.machineId === v); if (choice) selectSource(choice.target); }}>
          {availableMachines.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
          {!availableMachines.length && <option value={source.mid}>{source.label || source.cwd || t("worktree-composer.current_machine")}</option>}
        </Select>
        <Select label={t("worktree-composer.project")} value={project} onChange={(v) => { const choice = sameHost.find((c) => projectOf(c.row) === v); if (choice) selectSource(choice.target); }}>
          {projects.map(([id, row]) => <option key={id} value={id}>{row.project_name || row.title || row.cwd}</option>)}
        </Select>
        <Select label={t("worktree-composer.git_source")} value={key} onChange={(v) => { const choice = sameHost.find((c) => sourceKey(toSource(c.target)) === v); if (choice) selectSource(choice.target); }}>
          {sameHost.filter((c) => projectOf(c.row) === project).map((c) => <option key={sourceKey(toSource(c.target))} value={sourceKey(toSource(c.target))}>{c.row.title || c.row.branch || c.row.cwd}</option>)}
        </Select>
        {source.cwd && <p className="break-all text-xs text-ink-3">{source.cwd}</p>}
        {!info ? <Notice error={Boolean(read.key === key && read.error)}>{read.key === key && read.error || WORKTREE_COPY.reading}</Notice> : <>
          <p className="truncate text-xs text-ink-3" title={info.root}>{info.branch ? t("worktree.repo_on_branch", { repo: info.repoName, branch: info.branch }) : t("worktree.repo_detached", { repo: info.repoName })}</p>
          {info.automatic_name && <Select label={t("worktree-composer.naming")} value={draft.automaticName ? "auto" : "manual"} onChange={(v) => session.update({ automaticName: v === "auto" })}>
            <option value="manual">{t("worktree-composer.manual")}</option><option value="auto">{t("worktree-composer.generated")}</option>
          </Select>}
          {!draft.automaticName && <>
            <Input label={t("worktree.branch_label")} placeholder={t("worktree.branch_placeholder")} value={draft.branch} onChange={(v) => session.update(typeBranch(draft, info.path, v))} />
            <Input label={t("worktree.path_label")} value={draft.pathEdited ? draft.path : suggestedPath(info.path, draft.branch)} onChange={(v) => session.update(typePath(draft, info.path, v))} hint={t("worktree.path_hint")} />
          </>}
          <p className="text-xs text-ink-3">{t("worktree.note")}</p>
        </>}
      </fieldset>
      {offline && <Notice error>{t("worktree-composer.offline")}</Notice>}
      {read.key === key && read.error && !locked && <Button onClick={() => { setRead({ key }); setReadVersion((v) => v + 1); }}>{t("worktree-composer.retry")}</Button>}
      {locked && <Notice>{t(intent?.phase === "pending" || sending ? "worktree-composer.pending" : "worktree.unknown")}</Notice>}
      {(error || storageError || intent?.error) && <Notice error>{error || storageError || intent?.error}</Notice>}
      <button type="submit" hidden />
    </form>}
  </Dialog>;
}

const isFolder = (kind?: string) => kind === "folder" || kind === "folder-workspace";
const toSource = (target: WorkspaceTarget): WorktreeTarget => ({ mid: target.machineId, workspaceId: target.workspaceId, host: target.host,
  label: target.label, cwd: target.cwd, owner: target.owner, manager: target.manager });
const control = "min-h-9 w-full min-w-0 rounded-control bg-field px-3 py-2 text-sm text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)]";
function Notice({ error, children }: { error?: boolean; children: ReactNode }) { return <p role={error ? "alert" : "status"} className={cx("break-words text-sm", error ? "text-red" : "text-ink-3")}>{children}</p>; }
function Select({ label, value, onChange, children }: { label: string; value: string; onChange: (value: string) => void; children: ReactNode }) {
  const id = useId();
  return <div className="min-w-0"><label className="mb-1 block text-xs font-medium text-ink-2" htmlFor={id}>{label}</label>
    <select id={id} className={control} value={value} onChange={(e) => onChange(e.target.value)}>{children}</select>
  </div>;
}
function Input({ label, value, onChange, placeholder, hint }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string; hint?: string }) {
  const id = useId();
  return <div className="min-w-0"><label className="mb-1 block text-xs font-medium text-ink-2" htmlFor={id}>{label}</label>
    <input id={id} className={control} value={value} placeholder={placeholder} aria-describedby={hint ? `${id}-hint` : undefined}
      autoComplete="off" spellCheck={false} autoCapitalize="off" onChange={(e) => onChange(e.target.value)} />
    {hint && <p id={`${id}-hint`} className="mt-1 text-xs text-ink-3">{hint}</p>}
  </div>;
}
