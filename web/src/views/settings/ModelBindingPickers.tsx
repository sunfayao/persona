// Shared model, provider, and effort picker controls used by both ModelConfiguration and ModelProfileEditor
// so profile editing uses the exact same candidate filtering, labels, and /model list behavior.
import { useEffect, useRef, useState } from "react";
import { errorText } from "../../lib/api";
import { providerModels } from "../../lib/launcher-models";
import { lazyPage } from "../../lib/lazy-page";
import type { ModelList } from "../../ui/ModelPicker";

// Shared lazy ModelPicker chunk (loaded only when picking a model, outside Settings' preload dependencies).
export const ModelPicker = lazyPage(() => import("../../ui/ModelPicker").then((m) => m.ModelPicker));


export type LauncherModelState = ModelList & { machine?: string; provider?: string };

/** Loads a launcher provider's /model list (`providerModels`), keeping the previous list during explicit reloads. */
export function useLauncherModels(
  providerId: string,
  opts: { manager?: string; machine?: string; onProviderChange?: () => void } = {},
): { list: LauncherModelState; reload: () => void; hasPicker: boolean } {
  const [list, setList] = useState<LauncherModelState>({ status: "idle" });
  const [reloadSeq, setReloadSeq] = useState(0);
  const refreshNext = useRef(false);
  const onChangeRef = useRef(opts.onProviderChange);
  onChangeRef.current = opts.onProviderChange;

  useEffect(() => {
    const refresh = refreshNext.current;
    refreshNext.current = false;
    if (!refresh) onChangeRef.current?.();
    if (!providerId) {
      setList({ status: "idle" });
      return;
    }
    const controller = new AbortController();
    setList((prev) => ({
      status: "loading",
      provider: providerId,
      list: refresh && prev.provider === providerId ? prev.list : undefined,
    }));
    providerModels(providerId, { manager: opts.manager, refresh, machine: opts.machine, signal: controller.signal })
      .then((res) => setList({ status: "ready", list: res, machine: res?.machine, provider: providerId }))
      .catch((e) => {
        if (!controller.signal.aborted) setList({ status: "error", error: errorText(e), provider: providerId });
      });
    return () => controller.abort();
  }, [providerId, reloadSeq, opts.manager, opts.machine]);

  const reload = () => {
    refreshNext.current = true;
    setReloadSeq((n) => n + 1);
  };
  const hasPicker = list.status === "loading" || (list.status === "ready" && !!list.list?.models.length);
  return { list, reload, hasPicker };
}





