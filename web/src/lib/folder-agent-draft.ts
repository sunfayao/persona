import { credentialOwner, onCredentialChange } from "./api";
import { storeDraft, type DraftImage } from "./drafts";
import { currentHosts } from "./engines";
import { baseName, uuid } from "./format";
import { addContext } from "./links";
import { defaultManager } from "./manager-choice";
import { presetPrompt, type NewAgentPreset, type WorkspaceTarget } from "./new-agent";
import type { ModelChoice } from "./new-agent-panel";
import { navigate } from "./router";
import { getState } from "./store";

export interface FolderAgentDraft {
  id: string;
  key: string;
  machine: string;
  manager: string;
  owner: string;
  cwd: string;
  host: string;
  label: string;
  spaceId?: string;
  workspace?: WorkspaceTarget;
  desktopProjectId?: string;
  attachments: { images: DraftImage[] };
  choice: ModelChoice;
  permissionMode: string;
  onStarted?: NewAgentPreset["onStarted"];
  submitting: boolean;
}

const drafts = new Map<string, FolderAgentDraft>();
onCredentialChange(() => drafts.clear());

export function folderAgentDraft(id: string): FolderAgentDraft | undefined {
  const draft = drafts.get(id);
  return draft?.owner === credentialOwner() ? draft : undefined;
}

export function takeFolderAgentDraft(id: string) {
  drafts.delete(id);
}

/** Open a local draft only when the source already identifies the agent's computer and folder. */
export function openFolderAgentDraft(preset: NewAgentPreset): boolean {
  if (preset.workSession) return false;
  const workspace = preset.workspace;
  const machine = workspace?.machineId || preset.machineId;
  const cwd = (workspace?.cwd || preset.cwd || "").trim();
  if (!machine || !cwd) return false;
  const selected = getState().machines.find((m) => m.id === machine);
  if (!selected) return false;
  const manager = workspace?.manager || selected.manager || defaultManager();
  const owner = workspace?.owner || credentialOwner();
  if (owner !== credentialOwner()) return false;
  const host = workspace?.host || preset.host || currentHosts(selected).default;
  const key = JSON.stringify([owner, manager, machine, cwd, host, workspace?.workspaceId, preset.spaceId, preset.desktopProjectId, preset.launcher]);
  const seeded = preset.prompt || preset.forwards?.length || preset.images?.length || preset.onStarted;
  const existing = seeded ? undefined : [...drafts.values()].find((d) => d.key === key && !d.submitting);
  if (existing) {
    navigate({ name: "agent-draft", id: existing.id });
    return true;
  }
  const id = uuid();
  const draft = {
    id, key, machine, manager, owner, cwd, host, label: workspace?.label || baseName(cwd) || cwd,
    spaceId: preset.spaceId, workspace, desktopProjectId: preset.desktopProjectId,
    attachments: { images: [...(preset.images ?? [])] },
    choice: { launcher: preset.launcher || "", model: "", effort: "" },
    permissionMode: localStorage.getItem(`new.mode.${machine}`) ?? "",
    onStarted: preset.onStarted, submitting: false,
  };
  drafts.set(id, draft);
  if (preset.prompt) storeDraft(`agent-draft/${id}`, presetPrompt(preset.prompt, ""));
  for (const ref of preset.forwards ?? []) addContext(`agent-draft/${id}`, ref);
  navigate({ name: "agent-draft", id });
  return true;
}
