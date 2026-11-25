// The hub keeps Overview's cards and sections for every device of the owner (GET / PUT /api/v1/navigation, its default
// manager's), and this browser keeps a copy (lib/overview-cards.ts), so Overview draws before the hub answers and keeps
// the arrangement where the hub has no such route (404). This module reads the hub's copy at each hello, sends each change
// made here and follows the hub's pushes of it (below).
import { api, ApiError } from "./api";
import { feedCacheScope } from "./feed-cache";
import { adoptOverview, getOverviewPrefs, isSection, keepSections, noteOverviewKept, noteSectionsLocal, onOverviewSaved, overviewChosen, overviewElsewhere, overviewKept, readOverview, sectionsLocal, type CardEntry, type SectionEntry } from "./overview-cards";
import { isResumed } from "./reconnect";
import { onHubEvent } from "./store";
import { useSyncExternalStore } from "react";

const listeners = new Set<() => void>();
const changed = () => listeners.forEach((l) => l());

// Whether the hub keeps the arrangement: null until its answer, false for a hub (or a manager) without the route.
let synced: boolean | null = null;
export const navigationSynced = () => synced;

/** Says the hub keeps it (true), or answered `e` (false for a 404 / 405: it has no such route). */
export function noteNavigationSynced(e?: unknown) {
  const was = synced;
  if (!e) synced = true;
  else if (e instanceof ApiError && (e.status === 404 || e.status === 405)) synced = false;
  if (synced !== was) changed();
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => void listeners.delete(l);
};
export const useNavigationSynced = () => useSyncExternalStore(subscribe, navigationSynced);

// A change of the cards and sections (lib/overview-cards.ts) is sent as it is made. A default manager from
// before the field answers a change of only them with its own 400 (OLD_MANAGER), or takes the PUT and leaves them out
// of its answer: the arrangement then stays in this browser (Overview's editor says so) until the next hello asks
// again. One from before the sections (#9745's) refuses an arrangement that names one in its own words (NO_SECTIONS):
// the cards then go on their own at once and from then on, so they follow on every device as before, while the sections
// stay in this browser (noted per hub and sign-in, laid over the cards the hub sends, and the editor says so) until a
// hello finds the manager takes them and sends them. Any other failure
// (403, 5xx, the network, another 400) keeps the change and sends it again at the next hello. One change is on its way at a
// time: one made meanwhile goes once that one is answered, the newest as this browser has it then, so the hub takes them in
// the order they were made and the last answer says whether the newest reached it. Pushes heard meanwhile, and until the
// hub's push of the last one taken comes (its answer can come first), are settled then (settleOverview), not dropped.
const OLD_MANAGER = ["start is required", "start or pages is required"]; // #9328, #9351: a default manager from before the field
const NO_SECTIONS = 'overview: "" / "" is not an extension\'s card';
let sendingOverview = false;
let againOverview = false; // a change made while one was on its way
let unsentOverview = false;
let editsOverview = 0; // bumped by each change made here and each one sent: a hello's read that one overtook is not taken
let heardOverview: unknown[] = []; // the arrangements the hub pushed meanwhile, in its order
let answeredOverview: string[] = []; // its answers to our changes, in the same order, until it pushed the last of them
let helloOverview = 0; // bumped by each hello that did not resume: the hub's pushes from before it do not come again
// a change sent before such a hello was taken: its push may have gone with the old socket, so the pushes heard since are
// settled without it, the newest taken (they come in the hub's order; its own, if it comes, is taken in turn)
let looseOverview = false;
let pushedOverview = 0; // bumped by each push of it: a hello's read begun before one is older than what it pushed
// What else changed the arrangement since a read began: a push of it, or another tab's change to this sign-in's copy
// (review of #9778, P1): a read that one overtook is not taken.
const overviewVersion = () => pushedOverview + overviewElsewhere();
// The sign-in all of this belongs to (lib/feed-cache.ts feedCacheScope: the hub, the account, the device and its
// credentials, renewed at each sign-in and sign-out, a return to the same token too; review of #9778, P1). When it
// changes, everything on its way is retired: a change sent, queued or not taken under the earlier one, its
// answers and the pushes heard with them. An older request's answer, failure or end then changes nothing, and sends
// nothing under the new one, which starts as a fresh load does, its own hello reading its own arrangement.
let scopeOverview = feedCacheScope();
let ownerOverview = 0; // bumped at each such change: a request carries the one it was sent under
let helloScope: string | null | undefined; // the sign-in of the last hello: pushes before the next one's are the old socket's
function retireOverview() {
  ownerOverview++;
  scopeOverview = feedCacheScope();
  sendingOverview = againOverview = unsentOverview = looseOverview = false;
  heardOverview = [];
  answeredOverview = [];
  noteOverviewKept(null);
}
/** The sign-in's generation, the earlier one's work retired first where it changed since. */
function ownOverview() {
  if (feedCacheScope() !== scopeOverview) retireOverview();
  return ownerOverview;
}
// The hub passes a push on as it read it (internal/hub bus_broadcast.go), a card's keys in another order than its answer's:
// an arrangement is compared card by card (and section by section).
const overviewKey = (v: unknown) =>
  JSON.stringify((Array.isArray(v) ? (v as Partial<CardEntry & SectionEntry>[]) : []).map((c) => [c?.extension, c?.id, c?.hidden ?? false, c?.span ?? "", c?.section]));
// `probe`: a hello's, sent even where the hub did not keep it, and with the sections; `ask`: only whether the manager
// takes the sections now, the cards being the hub's already.
function sendOverview(probe = false, ask = false) {
  const owner = ownOverview();
  if (synced === false || (overviewKept() === false && !probe)) return; // this browser only
  if (sendingOverview) return void (againOverview = true);
  sendingOverview = true;
  editsOverview++;
  const hello = helloOverview;
  const cardsOnly = !probe && sectionsLocal();
  const list = getOverviewPrefs();
  api<{ overview?: unknown }>("/navigation", { method: "PUT", json: { overview: cardsOnly ? list.filter((e) => !isSection(e)) : list } })
    .then(
      (r) => ({ ok: true as const, r }),
      (e: unknown) => ({ ok: false as const, e }),
    )
    .then((done) => {
      // the earlier sign-in's: its answer, failure and end change nothing here, nor is a change queued under it sent
      if (owner !== ownOverview()) return;
      try {
        if (done.ok) {
          const r = done.r;
          noteNavigationSynced();
          unsentOverview = false;
          noteOverviewKept(Array.isArray(r.overview));
          if (!cardsOnly) noteSectionsLocal(false);
          // its push is still to come, unless it went out before a hello that did not resume
          if (Array.isArray(r.overview) && hello === helloOverview) answeredOverview.push(overviewKey(r.overview));
          else if (Array.isArray(r.overview)) looseOverview = true;
        } else {
          const e = done.e;
          noteNavigationSynced(e);
          const words = e instanceof ApiError && e.status === 400 ? String(e.body?.error) : "";
          const old = OLD_MANAGER.includes(words);
          const refused = words === NO_SECTIONS;
          if (old) noteOverviewKept(false);
          if (refused) {
            noteSectionsLocal(true);
            if (!ask) againOverview = true; // the cards on their own, now
          }
          unsentOverview = !old && !refused && synced !== false;
        }
      } finally {
        sendingOverview = false;
        if (againOverview) {
          againOverview = false;
          sendOverview();
        }
        if (!sendingOverview) settleOverview();
      }
    });
}
onOverviewSaved(() => {
  editsOverview++;
  sendOverview();
});

/** Once the last change on its way is answered and the hub pushed it: the hub pushes every change, these too, in its
 *  order, to every app, so an arrangement pushed before the last of ours is older than ours and one pushed after it is
 *  newer and is taken; with none, this browser's copy (the newest change) stays, as does a change the hub did not take,
 *  which goes again at the next hello. Our answers are found among the pushes in the order they were made, each after the
 *  one before, so a later push with the value of an earlier change of ours (another device putting the cards back as they
 *  were) is not taken for ours. Until the last is found (its answer can come before its push), pushes wait with it. */
function settleOverview() {
  let last = -1;
  let next = 0;
  heardOverview.forEach((p, i) => {
    if (next < answeredOverview.length && overviewKey(p) === answeredOverview[next]) {
      next++;
      last = i;
    }
  });
  if (!unsentOverview && next < answeredOverview.length) return;
  const heard = heardOverview;
  const loose = looseOverview;
  heardOverview = [];
  answeredOverview = [];
  looseOverview = false;
  if (!unsentOverview && (last >= 0 || loose) && last < heard.length - 1) takeOverview(heard[heard.length - 1]);
}

/** Takes the hub's arrangement as this browser's: under the sections while they are this browser's alone. One naming a
 *  section says the manager keeps them now. */
function takeOverview(v: unknown) {
  const list = readOverview(v);
  if (list.some(isSection)) noteSectionsLocal(false);
  adoptOverview(sectionsLocal() ? keepSections(getOverviewPrefs(), list) : list);
}

/** The hub's arrangement, read or pushed: taken at once, or settled with the change on its way or its push to come. A
 *  change the hub did not take stays over it until the next hello sends it again. */
function hubOverview(v: unknown[]) {
  noteOverviewKept(true);
  if (!sendingOverview && !answeredOverview.length && !unsentOverview) return takeOverview(v);
  heardOverview.push(v);
  if (!sendingOverview) settleOverview();
}

// The hub's choices, read at every hello that did not resume (a resumed one gets the pushes it missed) and pushed on
// each change. A hub that never stored one is handed the one this browser kept while there was no record. A read a
// later hello or another sign-in overtook is left to that one, every field of it and any hand-over.
onHubEvent("hello", (msg) => {
  helloScope = feedCacheScope();
  if (isResumed(msg)) return;
  // what the hub pushed before it does not come again: its read stands for it; and another sign-in's work is over
  ownOverview();
  const hello = ++helloOverview;
  const scope = feedCacheScope();
  const overtaken = () => hello !== helloOverview || scope !== feedCacheScope();
  heardOverview = [];
  answeredOverview = [];
  const edits = sendingOverview ? -1 : editsOverview;
  const version = overviewVersion();
  api<{ overview?: unknown }>("/navigation").then(
    (r) => {
      if (overtaken()) return;
      noteNavigationSynced();
      // ours, unless a change of it did not reach the hub or one was on its way or made since this read began, or the
      // hub pushed one or another tab changed it meanwhile (newer than its answer, review of #9778, P1); else handed
      // over (only while nothing newer came: the hub has one then), or asked again where it was refused
      const fresh = version === overviewVersion();
      if (Array.isArray(r.overview) && !unsentOverview && edits === editsOverview && fresh) {
        hubOverview(r.overview);
        if (sectionsLocal()) sendOverview(true, true); // an updated manager is handed the sections
      } else if (unsentOverview || overviewKept() === false || (fresh && overviewChosen())) sendOverview(true);
    },
    (e) => void (overtaken() || noteNavigationSynced(e)), // the copies here stay
  );
});
onHubEvent("navigation", (msg) => {
  // not one the earlier sign-in's socket passed on before this one's hello
  if (Array.isArray(msg.overview) && helloScope === feedCacheScope()) {
    pushedOverview++;
    hubOverview(msg.overview);
  }
});
