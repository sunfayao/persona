// Repositories and their Issue/PR links on the to-do's page, to read: the Agent keeps this store, and the page shows it
// as rows. The store is metadata only: nothing here creates folders or moves a task's stage.
import { Fragment, useCallback, useEffect, useRef, useState } from "react";
import { useRepositoryRequestContext as useRequestContext } from "../lib/use-repository-request-context";
import { t } from "../i18n";
import { ApiError, errorText } from "../lib/api";
import {
  isExtensionProvider, loadTaskRepositories, repositoryLinkKindLabel, repositoryLinkView, repositoryProviderName,
  type RepositoryLinkLabel, type RepositoryLinkView, type TaskRepository, type TaskRepositorySet,
} from "../lib/task-repositories";
import type { RepositoryIdentity } from "../lib/repository-identity";
import { Group, Pill, Row, RowFailed } from "../views/settings/SettingsParts";
import { Loading } from "./atoms";
import { Icon } from "./icons";
import { RepositoryMark } from "./RepositoryMark";

const linkTag = (identity: RepositoryIdentity, view: RepositoryLinkView) => `${identity.provider === "gitlab" && view.kind === "pull_request" ? "!" : "#"}${view.number}`;
/** The address a person recognises as the project, without the scheme every row shares. */
const repositoryAddress = (identity: RepositoryIdentity) => identity.canonical_url.replace(/^https?:\/\//, "");
/**
 * An extension's source as a row names it (the name saved with the repository, never its raw id), and whether it is
 * unavailable; nothing for GitHub and GitLab, whose own marks say it.
 */
function extensionSource(identity: RepositoryIdentity, saved?: string, state?: TaskRepository["provider_state"]) {
  if (!isExtensionProvider(identity.provider)) return undefined;
  return { name: repositoryProviderName(identity.provider, undefined, saved) ?? t("task-repositories.another_source"), unavailable: state === "off" || state === "missing" };
}
/** Why an extension row's source can't read its links now, beneath it in the note style of the catalog's states. */
const SourceNote = ({ name }: { name: string }) => <p role="status" data-repository-note className="py-2 pr-4 pl-10 text-submeta leading-snug text-ink-3">
  {t("task-repositories.provider_unavailable", { provider: name })}
</p>;

/**
 * A saved repository on one line: its provider mark at the same small scale as the stroke icons, and the project's
 * address. The address is the link, so it is a comfortable target on a phone without a second row; an address longer
 * than the line cuts with the full value in its tooltip. An extension's source is named after the address, so a narrow
 * line cuts its name before the repository's path.
 */
function RepositoryRow({ identity, source, children }: { identity: RepositoryIdentity; source?: string; children?: React.ReactNode }) {
  return <div className="flex items-center gap-2.5 px-4 py-2" data-repository-row>
    <RepositoryMark provider={identity.provider} />
    <a href={identity.canonical_url} target="_blank" rel="noreferrer noopener" title={source ? `${identity.canonical_url} · ${source}` : identity.canonical_url}
      className="flex touch-hit min-w-0 flex-1 items-center gap-2">
      <span className="min-w-0 flex-1 truncate text-submeta text-ink-3">{repositoryAddress(identity)}{source && <span data-repository-source> · {source}</span>}</span>
    </a>
    {children}
  </div>;
}

/**
 * One saved issue or pull request: the provider's kind mark, its full title (the link), and its number. An extension's
 * source may name the kinds its own way (`labels`, shown as given).
 */
function LinkRow({ identity, view, labels }: { identity: RepositoryIdentity; view: RepositoryLinkView; labels?: readonly RepositoryLinkLabel[] }) {
  const kind = (isExtensionProvider(identity.provider) && view.kind ? repositoryLinkKindLabel(view.kind, labels) : undefined)
    ?? t(view.kind === "issue" ? "task-repositories.issue" : "task-repositories.pull_request");
  const icon = view.kind === "issue" ? "target" : identity.provider === "gitlab" ? "merge" : "pullRequest";
  const tag = linkTag(identity, view);
  return <div className="flex items-start gap-2 py-2 pr-4 pl-10" data-repository-link>
    <span data-link-icon={icon} className="mt-0.5 shrink-0 text-ink-3"><Icon name={icon} size={14} /></span>
    <a href={view.url} target="_blank" rel="noreferrer noopener" aria-label={`${kind} ${tag} · ${identity.path}${view.title ? ` · ${view.title}` : ""}`}
      className="flex touch-hit min-w-0 flex-1 items-start gap-2 text-meta leading-snug text-ink-3 hover:underline">
      {view.title
        ? <><span className="min-w-0 break-words">{view.title}</span><span className="shrink-0 font-medium tabular-nums text-accent-ink">{tag}</span></>
        : <><span className="shrink-0">{kind}</span><span className="shrink-0 font-medium tabular-nums text-accent-ink">{tag}</span></>}
      <Icon name="external" size={11} className="mt-1 ml-auto shrink-0 text-ink-3" />
    </a>
  </div>;
}

function errorMessage(error: unknown) {
  if (error instanceof ApiError && error.code?.startsWith("task_repositories_") && error.code !== "task_repositories_store_unavailable") return errorText(error);
  return t("task-repositories.failed");
}
/** A hub that does not serve the association store at all (the route is missing, not the to-do). */
const isUnsupported = (error: unknown) => error instanceof ApiError && error.status === 404 && error.code !== "task_repositories_not_found";

/** A to-do's own PR, as the page already parses it (this section does not read the stage bar, a first-screen chunk). */
type LegacyPR = { url: string; text: string };
type TodoRepositoryBindingsProps = { manager: string; owner: string; todoId: string; legacyPRs?: LegacyPR[]; version?: unknown };
export function TodoRepositoryBindings(props: TodoRepositoryBindingsProps) {
  return <TodoRepositoryBindingsSession key={`${props.owner}:${props.manager}:${props.todoId}`} {...props} />;
}
function TodoRepositoryBindingsSession({ manager, owner, todoId, legacyPRs = [], version }: TodoRepositoryBindingsProps) {
  const context = useRequestContext(manager, owner);
  const [saved, setSaved] = useState<TaskRepositorySet>();
  const [error, setError] = useState("");
  const [unsupported, setUnsupported] = useState(false);
  const loads = useRef(0);
  const versionNow = useRef(version);
  versionNow.current = version;
  const inFlight = useRef<{ id: number; version: unknown; promise: Promise<TaskRepositorySet | undefined> } | undefined>(undefined);
  // A read belongs to the version it began from. Reusing one that began before a version change would answer with the
  // state before the notification; the invalidated read returns undefined, so a fresh one is needed instead.
  const startRead = useCallback((forVersion: unknown) => {
    const id = ++loads.current;
    const promise = loadTaskRepositories(context, todoId).then(
      (result) => id === loads.current && context.isCurrent() ? result : undefined,
    );
    const entry = { id, version: forVersion, promise };
    inFlight.current = entry;
    return entry;
  }, [context, todoId]);
  // The section reads the store: the Agent keeps it, so the detail is its newest word. StrictMode runs this effect
  // twice (and the version re-runs it), so a second run adopts the read that is still current instead of starting a
  // duplicate; one that did not would leave the section on its spinner for good.
  const [attempt, setAttempt] = useState(0);
  const refresh = useCallback(async () => {
    if (!context.isCurrent()) return;
    setError("");
    let entry = inFlight.current;
    if (!entry || entry.version !== versionNow.current || entry.id !== loads.current) {
      entry = startRead(versionNow.current);
    }
    try {
      const result = await entry.promise;
      if (result && context.isCurrent()) { setSaved(result); setUnsupported(false); }
    } catch (failure) {
      // A rejected read is fenced like its answer: an invalidated background read must not surface its 404/503 over
      // the current store.
      if (!context.isCurrent() || entry.id !== loads.current || entry.version !== versionNow.current) return;
      if (isUnsupported(failure)) setUnsupported(true); else setError(errorMessage(failure));
    } finally {
      if (inFlight.current === entry) inFlight.current = undefined;
    }
  }, [context, startRead]);
  useEffect(() => {
    if (!context.isCurrent()) return;
    void refresh();
  }, [context, todoId, version, attempt, refresh]);
  const repositories = saved?.repositories ?? [];
  const empty = <Row label={t("task-repositories.no_links")} hint={t("task-repositories.agent_hint")} wrapHint />;
  return <div className="mt-3" data-todo-repositories>
    <Group>
      {unsupported
        ? legacyPRs.length
          ? legacyPRs.map((p) => <Row key={p.url} icon="pullRequest" hint={t("task-repositories.legacy_note")}
              label={<a href={p.url} target="_blank" rel="noreferrer noopener" className="text-accent-ink hover:underline">{p.text}</a>} />)
          : empty
        : <>
          {repositories.map((repository) => {
            const source = extensionSource(repository.source_snapshot.identity, repository.source_snapshot.provider_name, repository.provider_state);
            return <Fragment key={repository.repo_id}>
              <RepositoryRow identity={repository.source_snapshot.identity} source={source?.name}>
                {source?.unavailable && <Pill>{t("task-repositories.unavailable")}</Pill>}
                {!!repository.links.length && <Pill>{repository.links.length}</Pill>}
              </RepositoryRow>
              {repository.links.map((link) => {
                const view = repositoryLinkView(repository.source_snapshot.identity, link);
                return view && !view.pending ? <LinkRow key={`${link.kind}:${link.number}`} identity={repository.source_snapshot.identity} view={view} labels={repository.link_kinds} /> : null;
              })}
              {source?.unavailable && <SourceNote name={source.name} />}
              {!!repository.catalog_state && repository.catalog_state !== "current" && <p role="status" data-repository-note className="py-2 pr-4 pl-10 text-submeta leading-snug text-ink-3">
                {t(repository.catalog_state === "removed" ? "task-repositories.catalog_removed" : repository.catalog_state === "identity_changed" ? "task-repositories.identity_changed" : "task-repositories.unlisted")}
              </p>}
            </Fragment>;
          })}
          {!saved && !error && <div className="px-4 py-3"><Loading /></div>}
          {/* A successful answer is the store's whole word, even empty: the manager imports the legacy history once and
              honours an explicit removal, so `pr_url` is a fallback for a missing route (above) and nowhere else. */}
          {saved && !repositories.length && empty}
          {error && <RowFailed text={error} retry={t("task-repositories.retry")} onRetry={() => { setError(""); setAttempt((n) => n + 1); }} />}
        </>}
    </Group>
  </div>;
}
