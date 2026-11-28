// Read shelving timestamps saved by earlier clients.
import { useSyncExternalStore } from "react";
import { storedValue } from "./compatibility";

const KEY = "persona.todos.restored";
let cache: ReadonlyMap<string, number> | undefined;

function read(): ReadonlyMap<string, number> {
  if (cache) return cache;
  const out = new Map<string, number>();
  try {
    const raw = JSON.parse(storedValue(KEY) ?? "{}") as Record<string, unknown>;
    for (const [id, at] of Object.entries(raw)) if (typeof at === "number" && Number.isFinite(at)) out.set(id, at);
  } catch {
    // a broken value is as none
  }
  cache = out;
  return out;
}

function subscribe(l: () => void): () => void {
  const onStorage = (e: StorageEvent) => {
    if (e.key !== KEY) return;
    cache = undefined;
    l();
  };
  window.addEventListener("storage", onStorage);
  return () => {
    window.removeEventListener("storage", onStorage);
  };
}

export function useRestoredTodos(): ReadonlyMap<string, number> {
  return useSyncExternalStore(subscribe, read, read);
}
