import { useEffect, useState } from "react";
import { t } from "../i18n";
import { api, ApiError, credentialOwner, errorText } from "../lib/api";
import { CLI_SLASH_COMMANDS, launchSlashTarget } from "../lib/cli-slash-commands";
import { agentId } from "../lib/agent-kind";
import { base64File, pickAttachment, releaseAttachments, type LocalAttachment } from "../lib/attachments";
import { CODEX_APP_SOURCE, startsInDesktopApp } from "../lib/desktop-start";
import { takeFolderAgentDraft, type FolderAgentDraft as Draft } from "../lib/folder-agent-draft";
import { shortPath, uuid } from "../lib/format";
import { followedBinding, launcherAgent, listedModes, machineLaunchers, nativeOmpStart, pickerLauncher, startPicks, terminalTraeStart, type MachineLaunchers } from "../lib/launcher-models";
import { initialLauncher } from "../lib/launcher-health";
import { defaultManager, MANAGER_HEADER } from "../lib/manager-choice";
import { modelPurpose, type ModelBinding } from "../lib/model-config";
import { launchAgent } from "../lib/new-agent";
import { formatFollowDetail, resolveFollowBinding, resolvedStartModel } from "../lib/new-agent-follow";
import { whenStarted } from "../lib/new-agent-started";
import { navigate } from "../lib/router";
import { BYPASS_MODE, BYPASS_WARNING, modeTitle, PERMISSION_MODES } from "../lib/session-actions";
import { getState, useStore } from "../lib/store";
import { type WorkspaceCatalogList, workspaceTarget } from "../lib/workspace-catalog";
import { directoryKey } from "../lib/workspace-directory";
import { cx, IconButton, toast } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { Composer, type Outgoing } from "./chat/Composer";
import { NewAgentModel, useModelList } from "./NewAgentModel";
import { settingsHref } from "./settings/nav";

const CLAUDE_DESKTOP = "claude_desktop";

export function FolderAgentDraft({ draft, onToggleSidebar }: { draft: Draft; onToggleSidebar: () => void }) {
  const { machine: mid, manager, cwd } = draft;
  const machine = useStore((s) => s.machines.find((m) => m.id === mid));
  const dynamicSlash = useStore((s) => s.features?.includes(CLI_SLASH_COMMANDS));
  const [choice, setChoice] = useState(draft.choice);
  const [mode, setMode] = useState(draft.permissionMode);
  const [options, setOptions] = useState<{ list: MachineLaunchers; effective: ModelBinding | null }>();
  const [failed, setFailed] = useState(false);
  const [retry, setRetry] = useState(0);
  const [placeStatus, setPlaceStatus] = useState(!draft.spaceId || draft.workspace ? "ready" : "loading");
  const [workspace, setWorkspace] = useState(draft.workspace);
  const sameOwner = draft.owner === credentialOwner() && (machine?.manager || defaultManager()) === manager;
  const online = sameOwner && Boolean(machine?.online);
  useEffect(() => {
    if (!online) return;
    const controller = new AbortController();
    setFailed(false);
    const { signal } = controller;
    void Promise.all([
      machineLaunchers(mid, { manager, signal }),
      modelPurpose("agent.new", manager).catch((e) => {
        if (e instanceof ApiError && e.status === 404) return null;
        throw e;
      }),
    ]).then(([list, config]) => {
      if (!signal.aborted) setOptions({ list, effective: resolveFollowBinding(list.default, config?.purpose.effective) });
    }).catch(() => { if (!signal.aborted) setFailed(true); });
    return () => controller.abort();
  }, [mid, manager, online, retry]);
  useEffect(() => {
    if (!online || !draft.spaceId || draft.workspace) return;
    const controller = new AbortController();
    const { signal } = controller;
    setPlaceStatus("loading");
    void api<WorkspaceCatalogList>("/workspaces", { signal, headers: { [MANAGER_HEADER]: manager } }).then(({ workspaces }) => {
      if (signal.aborted) return;
      const places = (workspaces ?? []).filter((w) => w.machine_id === mid && w.source_id === draft.spaceId && directoryKey(w.cwd) === directoryKey(cwd));
      const terminalPlaces = places.filter((w) => !startsInDesktopApp({ source: w.source }));
      const candidates = terminalPlaces.length ? terminalPlaces : places;
      const place = candidates.length === 1 ? workspaceTarget(candidates[0], draft.owner, manager) : undefined;
      // A desktop entry found by folder lookup is optional. Only an explicit desktop target can pin the draft to that app.
      if (place && terminalPlaces.length) draft.workspace = place;
      setWorkspace(place);
      setPlaceStatus(terminalPlaces.length > 1 ? "unavailable" : "ready");
    }).catch((error) => {
      if (signal.aborted) return;
      setPlaceStatus(error instanceof ApiError && error.status === 404 ? "ready" : "unavailable");
    });
    return () => controller.abort();
  }, [draft, mid, manager, online, cwd, retry]);

  const pinnedDesktopSource = startsInDesktopApp({ source: draft.workspace?.host }) ? draft.workspace?.host
    : draft.desktopProjectId && choice.launcher === "codex-app" ? CODEX_APP_SOURCE
    : draft.desktopProjectId && choice.launcher === "claude-desktop" ? CLAUDE_DESKTOP : undefined;
  const folderDesktopSource = startsInDesktopApp({ source: workspace?.host }) ? workspace?.host : undefined;
  const terminalWorkspace = folderDesktopSource ? undefined : workspace;
  const launchers = (options?.list.launchers ?? []).filter((l) => pinnedDesktopSource ? l.source === pinnedDesktopSource
    : !startsInDesktopApp(l) || l.source === folderDesktopSource);
  const configuredProvider = pickerLauncher("", options?.effective ?? {});
  const configured = configuredProvider && launchers.some((l) => l.name === configuredProvider) ? options?.effective ?? null : null;
  const picked = choice.launcher || (!configured ? initialLauncher(null, launchers) : "");
  const runs = pickerLauncher(picked, followedBinding(picked, configured));
  const launcher = launchers.find((l) => l.name === runs);
  const desktopStart = startsInDesktopApp(launcher);
  // a start outside a desktop app opens in herdr, the terminal host built into every computer, never in another host's
  // workspace
  const hostCurrent = desktopStart || Boolean(pinnedDesktopSource) || (terminalWorkspace?.host || "herdr") === "herdr";
  const followed = followedBinding(picked, configured);
  const models = useModelList(mid, runs);
  const agent = launcherAgent(runs, launchers, models.list);
  const providerModes = listedModes(launcher);
  const modes = providerModes ?? (launcher && (agent === "claude" || agent === "agy") ? PERMISSION_MODES.map((m) => m.id) : []);
  const permissionMode = modes.includes(mode) ? mode : undefined;
  const draftKey = `agent-draft/${draft.id}`;
  const launcherPinned = Boolean(choice.launcher || pinnedDesktopSource);
  const launcherReady = !launcherPinned || Boolean(launcher);
  const retryCatalog = () => setRetry((n) => n + 1);
  const blocked = !sameOwner || !hostCurrent ? { label: t("newagent.draft_destination_changed") }
    : !online ? { label: t("chat-chatview.agent_device_offline", { device: machine?.name || "" }) }
    : placeStatus === "unavailable" ? { label: t("newagent.draft_destination_changed"), onClick: retryCatalog }
    : placeStatus === "loading" || launcherPinned && !options && !failed ? { label: t("common.loading") }
    : !launcherReady ? { label: t("chat-modelpill.failed"), onClick: retryCatalog }
    : undefined;

  const changeChoice = (value: typeof choice) => {
    draft.choice = value;
    setChoice(value);
  };
  const changeMode = (value: string) => {
    draft.permissionMode = value;
    setMode(value);
  };
  const send = async (message: Outgoing) => {
    const current = getState().machines.find((m) => m.id === mid);
    if (draft.submitting || draft.owner !== credentialOwner() || !current?.online || (current.manager || defaultManager()) !== manager || blocked) return false;
    const attachments: LocalAttachment[] = [];
    try {
      for (const img of message.images) {
        attachments.push({ ...pickAttachment(base64File(img.name || "image.png", img.mime, img.data), Infinity), image: { mime: img.mime, data: img.data } });
      }
      for (const file of message.localFiles ?? []) {
        attachments.push({ id: uuid(), name: file.name, mime: "application/octet-stream", file, fileTag: true });
      }
      const picks = startPicks({ agent, list: models.list, followed, launcher, nativeOmp: nativeOmpStart(agent, launcher), model: choice.model, effort: choice.effort });
      const inline = terminalTraeStart(launcher);
      const target = agent === "codex" ? { agentType: "codex", inlinePrompt: true } : inline;
      draft.submitting = true;
      const id = launchAgent({
        machineId: mid, cwd, workspaceId: desktopStart ? undefined : terminalWorkspace?.workspaceId,
        desktopProjectId: desktopStart ? (folderDesktopSource === launcher?.source ? workspace?.workspaceId : undefined) || draft.desktopProjectId : undefined,
        launcher: desktopStart ? runs : picked, ...picks, permissionMode,
        displayLauncher: runs || undefined, displayEngine: runs ? agentId(agent) : undefined,
        displayModel: resolvedStartModel({ model: picks.model, followed, launcher, list: models.list }),
        launcherDefaultEffort: (agent === "agy" || agent === "codex" && models.list?.supports_start_effort) && Boolean(picked) && !picks.effort || undefined,
        ...target, text: message.text, attachments,
      }, { replace: true });
      takeFolderAgentDraft(draft.id);
      if (draft.onStarted) whenStarted(id, draft.onStarted);
      return true;
    } catch (error) {
      draft.submitting = false;
      releaseAttachments(attachments);
      toast(errorText(error), "error");
      return false;
    }
  };

  const modelControl = (
    <NewAgentModel mid={mid} launchers={launchers} ready={Boolean(options)}
      onRetry={failed ? retryCatalog : undefined}
      choice={choice} runs={runs} configured={configured} unavailable={options?.list.unavailable}
      follow={configured ? { active: !choice.launcher, label: t("newagent.follow_row"), detail: formatFollowDetail(configured, configuredProvider, models.list, launchers) } : undefined}
      fallback={t("chat-modelpill.default_model")} onChoice={changeChoice}
      onFollow={() => changeChoice({ launcher: "", model: "", effort: "" })}
      onHelp={() => navigate(settingsHref("models", null, false, null, "providers"))}
      onConfigure={() => navigate(settingsHref("models", null, false, null, "providers"))} />
  );
  const permissionControl = modes.length > 0 && (
    <select value={permissionMode ?? ""} onChange={(e) => changeMode(e.target.value)} aria-label={t("chat-sessiondialogs.permission_mode")}
      title={permissionMode === BYPASS_MODE ? BYPASS_WARNING : undefined}
      className={cx("h-7 min-w-0 max-w-40 shrink rounded-control bg-transparent pl-2 text-label text-ink-2 hover:bg-hover sm:max-w-none", permissionMode === BYPASS_MODE && "text-red")}>
      <option value="">{t("newagent.follow_launcher")}</option>
      {modes.map((value) => <option key={value} value={value}>{modeTitle(value)}</option>)}
    </select>
  );

  return (
    <section className="flex h-full min-h-0 min-w-0 flex-col" data-testid="folder-agent-draft">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line bg-page/85 px-3 backdrop-blur-md sm:px-4">
        <IconButton icon="sidebar" label={t("startingagent.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-label font-semibold text-ink">{t("startingagent.title")}</h1>
          <p className="truncate text-submeta text-ink-3" title={cwd}>{[machine?.name, draft.label].filter(Boolean).join(" · ")}</p>
        </div>
      </header>
      <div className="min-h-0 flex-1" />
      <div className="mx-auto w-full max-w-[760px] shrink-0 px-3 pb-3 sm:px-6">
        <Composer draftKey={draftKey} machineId={mid} agent={agent} autoFocus commands={[]}
          slashTarget={dynamicSlash && online && hostCurrent && !desktopStart && launcher ? launchSlashTarget(mid, runs, cwd, agent) : undefined}
          deferredAttachments={draft.attachments} placeholder={t("chat-chatview.agent_placeholder")}
          mention={{ cwd, self: draftKey, fileManager: manager, files: () => [] }}
          blocked={blocked}
          onSend={send}
          trailing={<div className="flex min-w-0 items-center gap-1">{modelControl}{permissionControl}</div>} />
        <p className="flex min-w-0 items-center gap-1 px-1 pt-1 text-submeta text-ink-3" title={cwd}>
          <Icon name="folder" size={12} className="shrink-0" /><span className="truncate">{shortPath(cwd)}</span>
        </p>
      </div>
    </section>
  );
}
