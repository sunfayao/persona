import { useEffect, useState } from "react";
import { api, credentialOwner, enc, sessionPath } from "./api";
import type { ComposerCommand } from "./extension-prompts";
import type { SlashCommand, SlashCommandCatalog } from "./types";

export const CLI_SLASH_COMMANDS = "cli_slash_commands_v1";

export type SlashTarget = { machine: string; session: string; cwd?: string; launcher?: string } | { machine: string; launcher: string; cwd: string };

export function launchSlashTarget(machine: string, launcher: string, cwd: string, agent: string): SlashTarget | undefined {
  if (machine && launcher && cwd && ["claude", "codex", "agy", "traex"].includes(agent)) {
    return { machine, launcher, cwd };
  }
}

function pathFor(target: SlashTarget): string {
  if ("session" in target) return sessionPath(target.machine, target.session) + "/slash-commands";
  const params = new URLSearchParams({ launcher: target.launcher, cwd: target.cwd });
  return `/machines/${enc(target.machine)}/slash-commands?${params}`;
}

const empty: readonly SlashCommand[] = [];
const cache = new Map<string, { rows: readonly SlashCommand[]; until: number }>();
const pending = new Map<string, Promise<readonly SlashCommand[]>>();

function cacheKey(path: string, target: SlashTarget): string {
  return JSON.stringify([credentialOwner(), path, "session" in target ? [target.cwd, target.launcher] : []]);
}

function read(path: string, key: string): Promise<readonly SlashCommand[]> {
  const previous = pending.get(key);
  if (previous) return previous;
  const request = api<SlashCommandCatalog>(path)
    .then((catalog) => catalog.commands)
    .catch(() => empty)
    .then((rows) => {
      if (cache.size >= 64) cache.clear();
      cache.set(key, { rows, until: Date.now() + (rows.length ? 15_000 : 3_000) });
      return rows;
    })
    .finally(() => pending.delete(key));
  pending.set(key, request);
  return request;
}

export function useCLISlashCommands(target: SlashTarget | undefined, active: boolean) {
  const path = target && pathFor(target);
  const key = target && path && cacheKey(path, target);
  const [loaded, setLoaded] = useState<{ key: string; rows: readonly SlashCommand[] }>();
  const cached = key ? cache.get(key) : undefined;
  const fresh = cached && cached.until > Date.now() ? cached.rows : undefined;
  const rows = fresh ?? (loaded && loaded.key === key ? loaded.rows : empty);
  const [loadingKey, setLoadingKey] = useState<string>();
  useEffect(() => {
    if (!active || !path || !key) return;
    const saved = cache.get(key);
    if (saved && saved.until > Date.now()) return;
    let mounted = true;
    setLoadingKey(key);
    void read(path, key).then((rows) => {
      if (!mounted) return;
      setLoaded({ key, rows });
      setLoadingKey(undefined);
    });
    return () => { mounted = false; };
  }, [active, path, key]);
  return { rows, loading: Boolean(active && key && !fresh && (loadingKey === key || rows === empty)) };
}

export function knownCLISlashCommand(target: SlashTarget, name: string): boolean {
  return cache.get(cacheKey(pathFor(target), target))?.rows.some((row) => row.name === name) ?? false;
}

export function cliSlashRows(rows: readonly SlashCommand[]): ComposerCommand[] {
  return rows.map((row) => ({
    key: `cli:${row.name}`, word: row.name, text: row.name + " ", desc: row.description, native: row.source,
  }));
}

export function slashQuery(draft: string): string | null {
  return /^\/(\S*)$/.exec(draft)?.[1].toLowerCase() ?? null;
}
