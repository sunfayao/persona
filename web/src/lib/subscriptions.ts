// Source subscriptions (#2041): the manager draws the pages as declarative UI
// (POST /api/v1/subscriptions/-/ui); the web opens them at /subscriptions.
import { useStore } from "./store";

const SUBSCRIPTIONS_FEATURE = "source_subscriptions";
/** The manager's pages are Inbox Sources (#5278): built-in sources beside the subscriptions; Settings has its row. */
const INBOX_SOURCES_FEATURE = "inbox_sources";
/** The manager answers GET /api/v1/subscriptions/-/graph (#5741): Inbox Sources has its List / Relationships switch. */
const INBOX_SOURCES_GRAPH_FEATURE = "inbox_sources_graph";

export { SUBSCRIPTIONS_UI } from "./subscriptions-ui";

/** Whether the manager shows subscriptions (they deliver). */
export function useSubscriptionsShown(): boolean {
  return useStore((s) => s.features.includes(SUBSCRIPTIONS_FEATURE));
}

/** Whether the manager draws Inbox Sources (Settings' row, the Inbox menu's item and the page's title say so). */
export function useInboxSourcesShown(): boolean {
  return useStore((s) => s.features.includes(INBOX_SOURCES_FEATURE));
}

/** Whether Inbox Sources offers its relationship view. */
export function useInboxSourcesGraphShown(): boolean {
  return useStore((s) => s.features.includes(INBOX_SOURCES_GRAPH_FEATURE));
}

/** The pages' first state: all of them, one extension's or one connector's. */
export type SubscriptionsScope = { ext: string } | { c: string } | undefined;

export function subscriptionsHref(scope?: SubscriptionsScope): string {
  return scope ? `/subscriptions?state=${encodeURIComponent(JSON.stringify(scope))}` : "/subscriptions";
}

