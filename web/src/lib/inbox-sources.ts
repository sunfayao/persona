// Whether anything feeds the inbox yet (docs/assistant/onboarding.md 4.5 "Rewriting the empty states"): an empty inbox with no source connected says
// "No message sources yet", not "All caught up". A source is connected when the hub has a mail
// account, Feishu set up (feature lark), or a connector that brings items in and is signed in.
import { useEffect, useState } from "react";
import { api } from "./api";
import { isEnabled, isSignedIn, listConnectors, otherConnectors, storesData, type HubConnector } from "./connectors";
import { loadMailAccounts } from "./mail";
import { larkOn, useStore } from "./store";

export interface SourcesInput {
  mail: number;
  /** Feishu: undefined on a hub without it (feature lark), else whether it is set up. */
  lark?: boolean;
  connectors: HubConnector[];
}

/** A connector that brings items in and is switched on and signed in feeds the inbox (its `inbox` switch, when it has one). */
function feeds(c: HubConnector): boolean {
  return isEnabled(c) && isSignedIn(c) && storesData(c) && c.inbox !== false;
}

/** Whether a source is connected: a mail account, Feishu set up, or a signed-in connector that brings items in. */
export function inboxSources(input: SourcesInput): { connected: boolean } {
  const others = otherConnectors(input.connectors, input.lark !== undefined);
  return { connected: input.mail > 0 || input.lark === true || others.some(feeds) };
}

/** The sources, read once `on` (an empty inbox); null while unknown or when the hub could not say (the inbox then
 *  keeps its plain "All caught up"). */
export function useInboxSources(on: boolean): { connected: boolean } | null {
  const lark = useStore(larkOn);
  const hasConnectors = useStore((s) => s.features.includes("connectors"));
  const [sources, setSources] = useState<{ connected: boolean } | null>(null);
  useEffect(() => {
    if (!on) return;
    let live = true;
    Promise.all([
      loadMailAccounts(),
      lark ? api<{ configured?: boolean }>("/lark/status").then((s) => !!s.configured) : Promise.resolve(undefined),
      hasConnectors ? listConnectors() : Promise.resolve([] as HubConnector[]),
    ])
      .then(([mail, larkSet, connectors]) => live && setSources(inboxSources({ mail: mail.length, lark: larkSet, connectors })))
      .catch(() => live && setSources(null));
    return () => {
      live = false;
    };
  }, [on, lark, hasConnectors]);
  return on ? sources : null;
}
