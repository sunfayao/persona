import type { TermSpace, TermTab } from "../../lib/terminals";

export type ShellTab = TermTab & { source: TermTab };
export type ShellSpace = Omit<TermSpace, "tabs"> & { tabs: ShellTab[] };

/** The Terminals list (#4754): every plain pane, the ones a stopped Agent left (`agent_session`) included, never a
 *  running Agent's. Preserve the original tab for its node menu: hiding its agent panes must not turn "Close pane" into
 *  "Close tab". */
export function standaloneShells(spaces: TermSpace[]): ShellSpace[] {
  return spaces.flatMap((space) => {
    const tabs = space.tabs.flatMap((tab) => {
      const panes = tab.panes.filter((pane) => pane.state === "shell" && !pane.sessionId);
      return panes.length ? [{ ...tab, panes, source: tab }] : [];
    });
    return tabs.length || space.canOpen ? [{ ...space, tabs }] : [];
  });
}
