import { EASE, EXIT_MS, exitsPlay } from "../ui/atoms";

type Capture = (key: string) => () => void;
const watchers = new Set<Capture>();

export function onSidebarAgentRemoval(capture: Capture) {
  watchers.add(capture);
  return () => { watchers.delete(capture); };
}

/** Capture while the row is still present. Only a successful DELETE plays it. */
export function prepareSidebarAgentRemoval(machine: string, session: string) {
  const captures = [...watchers].map((capture) => capture(`s:${machine}/${session}`));
  return () => captures.forEach((finish) => finish());
}

/** The sidebar can receive the authoritative list removal before or after the DELETE receipt. */
export function captureSidebarAgentRow(root: HTMLElement, rows: ReadonlyMap<string, HTMLElement>, key: string) {
  const row = rows.get(key);
  const scroller = root.closest("[data-row-scroller]");
  if (!row || !scroller || !exitsPlay()) return;
  const capture = () => {
    const rect = row.getBoundingClientRect();
    const bounds = scroller.getBoundingClientRect();
    if (!rect.height || rect.bottom <= bounds.top || rect.top >= bounds.bottom) return;
    const rootRect = root.getBoundingClientRect();
    const topOffset = root.scrollTop - rootRect.top;
    const below = new Map<string, number>();
    for (const [id, el] of rows) {
      if (!(row.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING)) continue;
      const next = el.getBoundingClientRect();
      if (next.height && next.top < bounds.bottom) below.set(id, next.top + topOffset);
    }
    return {
      top: rect.top + topOffset,
      left: rect.left - rootRect.left + root.scrollLeft,
      width: rect.width, height: rect.height, below,
      clone: row.cloneNode(true) as HTMLElement,
    };
  };
  const before = capture();
  if (!before) return;

  return () => {
    if (!root.isConnected || !exitsPlay()) return;
    const from = row.isConnected ? capture() : before;
    if (!from) return;
    const play = () => {
      if (rows.get(key)?.isConnected || !root.isConnected) return;
      observer.disconnect();
      clearTimeout(timeout);
      const ghost = from.clone;
      ghost.inert = true;
      ghost.setAttribute("aria-hidden", "true");
      ghost.setAttribute("data-agent-removal", "");
      ghost.classList.add("pointer-events-none", "bg-hover", "z-20");
      Object.assign(ghost.style, { position: "absolute", top: `${from.top}px`, left: `${from.left}px`, width: `${from.width}px`, height: `${from.height}px` });
      root.appendChild(ghost);
      const exit = ghost.animate([{ opacity: 1 }, { opacity: 0 }], { duration: EXIT_MS.card, easing: EASE, fill: "forwards" });
      const clear = () => { clearTimeout(cleanup); ghost.remove(); };
      const cleanup = setTimeout(clear, EXIT_MS.card + 120);
      void exit.finished.then(clear, clear);
      const topOffset = root.scrollTop - root.getBoundingClientRect().top;
      for (const [id, first] of from.below) {
        const el = rows.get(id);
        if (!el?.isConnected) continue;
        const dy = first - (el.getBoundingClientRect().top + topOffset);
        if (dy >= 0.5) el.animate([{ transform: `translateY(${dy}px)` }, { transform: "none" }], { duration: EXIT_MS.glide, easing: EASE });
      }
    };
    const observer = new MutationObserver(play);
    observer.observe(root, { childList: true, subtree: true });
    const timeout = setTimeout(() => observer.disconnect(), 5_000);
    play();
  };
}
