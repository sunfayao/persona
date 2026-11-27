import { memo, useMemo, useState, type ReactNode } from "react";
import { t } from "../../i18n";
import { flipPin } from "../../lib/agent-actions";
import {
  buildEvidenceSnippet,
  DEFAULT_DIMENSION_FILTERS,
  SEARCH_SCOPES,
  sessionHarnessLabel,
  sessionHasError,
  sessionModelId,
  sessionProjectName,
  toggleFacetValue,
  type AgentDimensionFilters,
  type AgentFacetCounts,
  type EvalContext,
  type SearchPlan,
  type SearchScope,
} from "../../lib/agent-chatlist-filter";
import { awaitsReply, finishedUnopened, justFinished, sessionSource, sourceName, type AgentMutes } from "../../lib/agent-groups";
import { ago, backgroundWaiting, sessionPreview, sessionTitle, stateOf, tabLabel } from "../../lib/format";
import { sessionCard } from "../../lib/folded-rows";
import { machineWithHub } from "../../lib/mesh";
import { plainMarkdown } from "../../lib/plain-markdown";
import { openPaletteWith } from "../../lib/palette";
import { href, type Route } from "../../lib/router";
import { draftLine, rowLine } from "../../lib/row-line";
import { useLeftDraft } from "../../lib/drafts";
import { stateName } from "../../lib/state-labels";
import { rememberSessionRow, useStore } from "../../lib/store";
import type { Session } from "../../lib/types";
import { ultracodeMarkShown } from "../../lib/ultracode";
import { usageLimitLabel } from "../../lib/usage-limit";
import { SessionMark } from "../../ui/AgentMark";
import { cx, Menu, useMenu, useNow, type MenuItem } from "../../ui/atoms";
import { contextMenuProps } from "../../ui/context-menu";
import { Icon } from "../../ui/icons";
import { StatusMark } from "../../ui/StatusMark";
import { UltraMark } from "../chat/Ultracode";
import type { GoFn, RegFn, RowMenuFn } from "../SidebarParts";
import { SESSION_ACTIVE_ROW } from "../SidebarRows";

type FilterPage = "root" | "scope" | "harnesses" | "projects" | "models" | "machines";
type FacetKey = Exclude<FilterPage, "root" | "scope">;

const FACET_PAGES: { key: FacetKey; labelKey: string }[] = [
  { key: "harnesses", labelKey: "lib-agent-filter.filter_harness" },
  { key: "projects", labelKey: "lib-agent-filter.filter_project" },
  { key: "models", labelKey: "lib-agent-filter.filter_model" },
  { key: "machines", labelKey: "lib-agent-filter.filter_machine" },
];

/** How many filters are on (the Filter button's count): the query ⌘K put on the list is one, Search as Code is a mode. */
function activeFilterCount(f: AgentDimensionFilters): number {
  return (
    (f.query.trim() ? 1 : 0) + f.harnesses.length + f.projects.length + f.models.length + f.machines.length + (f.pinnedOnly ? 1 : 0) + (f.scope !== "all" ? 1 : 0) + (f.since ? 1 : 0)
  );
}

/** Reset: every filter off, the query too; Search as Code is a mode, its plan bar has its own ×. */
const resetOf = (f: AgentDimensionFilters): AgentDimensionFilters => ({ ...DEFAULT_DIMENSION_FILTERS, searchAsCode: f.searchAsCode, customPlanCode: f.customPlanCode });

// The Agents list's Filter (#4397, #6589): an icon button on the Agents title row, beside fold all and ⋯, whose menu holds
// the scope, Pinned, the four facets, the chat-list view and Search as Code. The list has no search field of its own: ⌘K
// (Search chats) finds sessions with the same words and operators, and its "Filter Agents list" puts them on the list.
export function AgentFilterButton({
  filters,
  onChange,
  facets,
  chatList,
  onToggleChatList,
}: {
  filters: AgentDimensionFilters;
  onChange: (next: AgentDimensionFilters) => void;
  facets: AgentFacetCounts;
  chatList: boolean;
  onToggleChatList: () => void;
}) {
  const filterMenu = useMenu();
  const [page, setPage] = useState<FilterPage>("root");

  const activeScope = SEARCH_SCOPES.find((s) => s.key === filters.scope) ?? SEARCH_SCOPES[0];
  const count = activeFilterCount(filters);

  const menuItems = (): MenuItem[] => {
    const back: MenuItem = { key: "back", label: t("sidebar.back"), keepOpen: true, onSelect: () => setPage("root") };
    if (page === "scope") {
      return [
        back,
        ...SEARCH_SCOPES.map((sc, i) => ({
          key: sc.key,
          label: t(sc.labelKey),
          divider: i === 0,
          radio: true,
          checked: filters.scope === sc.key,
          keepOpen: true,
          onSelect: () => onChange({ ...filters, scope: sc.key as SearchScope }),
        })),
      ];
    }
    if (page !== "root") {
      const options = facets[page];
      const selected = filters[page];
      const allIds = options.map((o) => o.id);
      const set = (next: string[]) => onChange({ ...filters, [page]: next });
      return [
        back,
        { key: "__all__", label: t("lib-agent-filter.all"), divider: true, checked: selected.length === 0, keepOpen: true, onSelect: () => set([]) },
        ...options.map((o) => ({
          key: o.id,
          label: o.label,
          hint: String(o.count),
          checked: selected.includes(o.id),
          keepOpen: true,
          onSelect: () => set(toggleFacetValue(selected, o.id, allIds)),
        })),
      ];
    }
    const facetRows = FACET_PAGES.filter((f) => facets[f.key].length > 1 || filters[f.key].length > 0).map(
      (f, i): MenuItem => ({
        key: f.key,
        label: t(f.labelKey),
        hint: filters[f.key].length > 0 ? String(filters[f.key].length) : t("lib-agent-filter.all"),
        divider: i === 0,
        submenu: true,
        onSelect: () => setPage(f.key),
      }),
    );
    return [
      { key: "scope", label: t("lib-agent-filter.search_in"), hint: t(activeScope.labelKey), submenu: true, onSelect: () => setPage("scope") },
      {
        key: "pinned",
        label: t("lib-agent-filter.filter_pinned"),
        hint: facets.pinned > 0 ? String(facets.pinned) : undefined,
        checked: filters.pinnedOnly,
        keepOpen: true,
        onSelect: () => onChange({ ...filters, pinnedOnly: !filters.pinnedOnly }),
      },
      ...facetRows,
      { key: "chat-list", label: t("lib-agent-filter.chat_list_view"), divider: true, checked: chatList, onSelect: onToggleChatList },
      {
        key: "search-as-code",
        label: t("lib-agent-filter.search_as_code"),
        checked: filters.searchAsCode,
        onSelect: () => onChange({ ...filters, searchAsCode: !filters.searchAsCode, customPlanCode: undefined }),
      },
      ...(count > 0 ? [{ key: "reset", label: t("lib-agent-filter.reset_filters"), divider: true, onSelect: () => onChange(resetOf(filters)) }] : []),
    ];
  };

  const filterLabel = count > 0 ? `${t("lib-agent-filter.filter")} (${count})` : t("lib-agent-filter.filter");
  // the title row's other icon buttons' size and colors (⋯, fold all), and their 44 px box on a touch screen (they sit
  // 4 px apart: centred touch-hit squares would overlap); lit like ⋯ under a sort while something is on
  const lit = count > 0 || filters.searchAsCode || chatList;
  return (
    <>
      <button
        type="button"
        onClick={(e) => {
          setPage("root");
          filterMenu.open(e);
        }}
        aria-haspopup="menu"
        aria-expanded={Boolean(filterMenu.anchor)}
        aria-label={filterLabel}
        title={filterLabel}
        data-testid="agent-filter-button"
        className={cx(
          "touch-hit relative z-10 flex h-5 min-w-5 shrink-0 items-center justify-center gap-0.5 rounded-[6px] px-0.5 text-[11px] transition-[background-color,color] duration-150 hover:bg-hover hover:text-ink-2 [@media(pointer:coarse)]:h-11 [@media(pointer:coarse)]:min-w-11",
          lit ? "font-medium text-accent-ink" : "text-ink-3",
          filterMenu.anchor && "bg-hover",
        )}
      >
        <Icon name="filter" size={12} stroke={2} />
        {count > 0 && <span className="tabular-nums leading-none">{count}</span>}
      </button>
      {filterMenu.anchor && (
        <Menu anchor={filterMenu.anchor} onClose={filterMenu.close} align="right" width={220} header={t("lib-agent-filter.filter")} items={menuItems()} />
      )}
    </>
  );
}

// What filters the list, above it and only while something does (#4397): each filter a chip with ×, the query ⌘K put on
// the list among them (its text opens ⌘K with it, to change it), then Reset; and with Search as Code on, its plan bar.
export function AgentFilterChips({
  filters,
  onChange,
  facets,
  plan,
}: {
  filters: AgentDimensionFilters;
  onChange: (next: AgentDimensionFilters) => void;
  facets: AgentFacetCounts;
  /** Search as Code's plan, for its bar: a list without it (the hub's paged list) shows none. */
  plan?: SearchPlan;
}) {
  const [editingPlan, setEditingPlan] = useState(false);
  const activeScope = SEARCH_SCOPES.find((s) => s.key === filters.scope) ?? SEARCH_SCOPES[0];

  const chips: { key: string; label: string; onRemove: () => void; onEdit?: () => void }[] = [];
  const query = filters.query.trim();
  if (query) {
    chips.push({
      key: "query",
      label: `${t("lib-agent-filter.query_chip")}: ${query}`,
      onRemove: () => onChange({ ...filters, query: "" }),
      onEdit: () => openPaletteWith(query),
    });
  }
  if (filters.scope !== "all") {
    chips.push({ key: "scope", label: `${t("lib-agent-filter.search_in")}: ${t(activeScope.labelKey)}`, onRemove: () => onChange({ ...filters, scope: "all" }) });
  }
  if (filters.pinnedOnly) {
    chips.push({ key: "pinned", label: t("lib-agent-filter.filter_pinned"), onRemove: () => onChange({ ...filters, pinnedOnly: false }) });
  }
  for (const f of FACET_PAGES) {
    for (const id of filters[f.key]) {
      const label = facets[f.key].find((x) => x.id === id)?.label ?? id;
      chips.push({
        key: `${f.key}:${id}`,
        label: `${t(f.labelKey)}: ${label}`,
        onRemove: () => onChange({ ...filters, [f.key]: filters[f.key].filter((x) => x !== id) }),
      });
    }
  }
  if (filters.since) {
    chips.push({ key: "since", label: `since:${filters.since}`, onRemove: () => onChange({ ...filters, since: undefined }) });
  }
  if (chips.length === 0 && !filters.searchAsCode) return null;

  return (
    <div className="mt-1.5 flex flex-col gap-1.5 px-1" data-testid="agent-filter-chips">
      {chips.length > 0 && (
        <div className="flex flex-wrap items-center gap-1 text-[11px]" data-testid="scoped-filter-strip">
          {chips.map((chip) => (
            <span
              key={chip.key}
              data-chip={chip.key}
              className="inline-flex max-w-full items-center gap-1 rounded-[6px] bg-ink/[0.045] dark:bg-ink/[0.06] py-0.5 pr-1 pl-2 font-medium text-ink"
            >
              {chip.onEdit ? (
                <button type="button" onClick={chip.onEdit} title={t("sidebar.search_sessions")} className="min-w-0 truncate text-left hover:text-accent-ink">
                  {chip.label}
                </button>
              ) : (
                <span className="min-w-0 truncate">{chip.label}</span>
              )}
              <button
                type="button"
                aria-label={t("lib-agent-filter.remove_filter", { name: chip.label })}
                onClick={chip.onRemove}
                className="flex size-4 shrink-0 items-center justify-center rounded-full text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink"
              >
                <Icon name="x" size={9} stroke={2.4} />
              </button>
            </span>
          ))}
          <button
            type="button"
            data-testid="scoped-filter-reset"
            onClick={() => onChange(resetOf(filters))}
            className="ml-auto shrink-0 whitespace-nowrap rounded-control px-1.5 py-0.5 font-medium text-accent-ink transition-colors hover:bg-hover"
          >
            {t("lib-agent-filter.reset_filters")}
          </button>
        </div>
      )}

      {filters.searchAsCode && plan && (
        <div
          data-testid="search-as-code-plan"
          className="flex items-center gap-1.5 rounded-[8px] bg-ink/[0.035] dark:bg-ink/[0.045] px-2 py-1 text-[11px]"
        >
          <Icon name="code" size={12} stroke={2} className="shrink-0 text-accent-ink" />
          {editingPlan ? (
            <input
              autoFocus
              data-testid="search-as-code-input"
              aria-label={t("lib-agent-filter.search_plan")}
              value={filters.customPlanCode ?? plan.code}
              onChange={(e) => onChange({ ...filters, customPlanCode: e.target.value })}
              onBlur={() => setEditingPlan(false)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === "Escape") setEditingPlan(false);
              }}
              className="min-w-0 flex-1 bg-transparent font-mono text-ink outline-none"
            />
          ) : (
            <button
              type="button"
              onClick={() => setEditingPlan(true)}
              title={`${t("lib-agent-filter.search_plan")}: ${plan.code}`}
              className="min-w-0 flex-1 truncate text-left font-mono text-ink hover:text-accent-ink"
            >
              {plan.code}
            </button>
          )}
          <button
            type="button"
            data-testid="search-as-code-off"
            aria-label={t("lib-agent-filter.search_as_code_off")}
            title={t("lib-agent-filter.search_as_code_off")}
            onClick={() => {
              setEditingPlan(false);
              onChange({ ...filters, searchAsCode: false, customPlanCode: undefined });
            }}
            className="flex size-4 shrink-0 items-center justify-center rounded-full text-ink-3 hover:bg-hover hover:text-ink"
          >
            <Icon name="x" size={9} stroke={2.4} />
          </button>
        </div>
      )}
    </div>
  );
}

/** IM Chat-List style Agent Session Row:
 *  - Left: Harness avatar tile + live status dot badge on the bottom-right corner
 *  - Top line: Bold session title (+ Ultracode / Pinned indicator) + relative timestamp on the right
 *  - Second line: Role-attributed search evidence snippet (with highlighted match) OR live last message / tool summary
 *  - Bottom compact chips: `Machine · Project · Harness · Model`
 *  - Right action: One-click Pin / Unpin button + `⋯` actions menu
 */
export const AgentChatListRow = memo(function AgentChatListRow({
  s,
  k,
  reg,
  hover,
  active,
  to,
  go,
  pinned,
  mutes,
  filters,
  ctx,
  onMenu,
}: {
  s: Session;
  k: string;
  reg: RegFn;
  hover: (k: string | null) => void;
  active: boolean;
  to: Route;
  go: GoFn;
  pinned: boolean;
  mutes: AgentMutes;
  filters: AgentDimensionFilters;
  ctx: EvalContext;
  onMenu?: RowMenuFn;
}) {
  const st = stateOf(s);
  const bgWait = backgroundWaiting(s);
  const usageLimited = Boolean(usageLimitLabel(s));
  const markState = usageLimited && st === "unread" ? "idle" : st;
  const awaiting = awaitsReply(s, mutes);
  const unopened = finishedUnopened(s, mutes);
  const finished = justFinished(s, mutes);
  const quietMark = (markState === "idle" && !awaiting && !finished && !unopened) || markState === "offline";
  const ix = useStore((state) => sessionCard(state.interactions, s.machine_id, s.session_id));
  const draft = draftLine(useLeftDraft(`${s.machine_id}/${s.session_id}`));
  const line: { lead?: string; text: string; tone?: "needs" | "reply" } | undefined = draft ?? rowLine(s, ix, awaiting, finished || unopened);
  const previewText = line?.text || plainMarkdown(sessionPreview(s)) || s.activity || "";
  const now = useNow(60_000, Boolean(s.updated_at));
  const timeLabel = s.updated_at ? ago(s.updated_at, now) : "";
  const ultra = ultracodeMarkShown(s);
  const hasErr = sessionHasError(s, ctx.interactions);
  const statePill = st === "working"
    ? bgWait
      ? { label: stateName("bg_waiting"), cls: "text-ink-3" }
      : { label: stateName("working"), cls: "text-accent-ink" }
    : undefined;

  const evidence = useMemo(
    () => (filters.query.trim() || filters.scope === "errors" ? buildEvidenceSnippet(s, filters.query, filters.scope, ctx) : null),
    [s, filters.query, filters.scope, ctx],
  );

  const machineLabel = machineWithHub(s.machine_id, ctx.machines) || s.machine_id;
  const projectLabel = sessionProjectName(s);
  const hasTabAssociation = !!(s.slot_id?.trim() || s.tab_id?.trim());
  const associatedTab = hasTabAssociation ? tabLabel(s) : "";
  const tabIdentity = associatedTab ? t("sidebar.tab_named", { label: associatedTab }) : "";
  const harnessLabel = sessionHarnessLabel(s);
  const modelLabel = sessionModelId(s);
  const source = sessionSource(s);
  const regRow = useMemo(() => reg(k), [reg, k]);
  const cardTone = active ? SESSION_ACTIVE_ROW : "hover:bg-hover";
  const metaParts: { key: string; node: ReactNode }[] = [];
  if (machineLabel) {
    metaParts.push({
      key: "machine",
      node: (
        <span className="inline-flex max-w-[100px] items-center gap-0.5 truncate">
          <Icon name="machine" size={9} stroke={2} className="shrink-0" />
          <span className="truncate">{machineLabel}</span>
        </span>
      ),
    });
  }
  if (projectLabel) {
    metaParts.push({
      key: "project",
      node: (
        <span className="inline-flex max-w-[116px] items-center gap-0.5 truncate">
          <Icon name="folder" size={9} stroke={2} className="shrink-0" />
          <span className="truncate">{projectLabel}</span>
        </span>
      ),
    });
  }
  if (tabIdentity) {
    metaParts.push({
      key: "tab",
      node: <span className="max-w-[100px] truncate" title={tabIdentity}>{tabIdentity}</span>,
    });
  }
  if (harnessLabel) {
    metaParts.push({
      key: "harness",
      node: <span className="max-w-[96px] truncate">{harnessLabel}</span>,
    });
  }
  if (modelLabel) {
    metaParts.push({
      key: "model",
      node: <span className="max-w-[116px] truncate font-mono text-[10px]">{modelLabel}</span>,
    });
  }
  if (source) {
    metaParts.push({
      key: "source",
      node: <span title={sourceName(source)}>{sourceName(source)}</span>,
    });
  }

  return (
    <div
      data-testid="agent-chatlist-row"
      data-session-id={s.session_id}
      data-pinned={pinned ? "true" : "false"}
      className="group/row relative rounded-[10px]"
    >
      <a
        ref={regRow}
        href={href(to)}
        onClick={(e) => {
          rememberSessionRow(s);
          go(to)(e);
        }}
        {...(onMenu ? contextMenuProps((at, el) => onMenu(s, el, at)) : {})}
        onMouseEnter={() => hover(k)}
        onFocus={() => hover(k)}
        onBlur={() => hover(null)}
        aria-current={active ? "page" : undefined}
        className={cx(
          "relative z-10 flex w-full items-start gap-2.5 rounded-[10px] px-2.5 py-2 transition-[background-color,box-shadow,color,transform] duration-200 ease-(--ease-out-strong) active:scale-[0.99]",
          cardTone,
        )}
      >
        {/* Left: Clean borderless harness icon (no overlapping circular cutout) */}
        <span
          className={cx(
            "mt-0.5 flex size-5 shrink-0 items-center justify-center bg-transparent transition-colors",
            active || st === "needs_you" || (st === "working" && !bgWait) ? "text-ink" : "text-ink-2 group-hover/row:text-ink",
            st === "offline" && "opacity-50",
          )}
        >
          <SessionMark s={s} size={16} className="text-current" />
        </span>

        {/* Center column: Title + Time, Second line (Evidence snippet or Last message), Inline metadata */}
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          {/* Line 1: Inline StatusMark + Pinned + Session Title + Ultracode + Inline State + Relative Activity Time */}
          <span className="flex min-w-0 items-center gap-1.5">
            <span className={cx("inline-flex w-2.5 shrink-0 items-center justify-center", quietMark && "opacity-35")}>
              <StatusMark state={markState} size="sm" awaiting={awaiting} finished={finished} unopened={unopened} waiting={bgWait} />
            </span>
            {pinned && (
              <span className="shrink-0 text-accent" title={t("sidebar.pinned")}>
                <Icon name="pin" size={11} stroke={2.3} />
              </span>
            )}
            <span
              className={cx(
                "min-w-0 flex-1 truncate text-[13px] leading-tight tracking-[-0.006em]",
                active || st === "needs_you" || (st === "working" && !bgWait) || (st === "unread" && !usageLimited)
                  ? "font-medium text-ink"
                  : st === "offline"
                    ? "font-normal text-ink-3"
                    : "font-normal text-ink-2",
              )}
            >
              {sessionTitle(s)}
            </span>
            {ultra && <UltraMark size={10} />}
            {statePill && (
              <span aria-hidden className={cx("shrink-0 text-[11px] font-medium whitespace-nowrap group-hover/row:invisible", statePill.cls)}>
                {statePill.label}
              </span>
            )}
            {timeLabel && (
              <span className="shrink-0 text-[11px] text-ink-3 tabular-nums group-hover/row:invisible">
                {timeLabel}
              </span>
            )}
          </span>

          {/* Line 2: Role-attributed Evidence Snippet when searching, else Last message / status line */}
          {evidence ? (
            <span data-testid="agent-evidence-snippet" className="truncate text-[11.5px] leading-4 text-ink-2">
              <span
                className={cx(
                  "mr-1 font-semibold",
                  evidence.role === "error" ? "text-red" : evidence.role === "user" ? "text-accent-ink" : "text-ink-2",
                )}
              >
                {evidence.roleLabel}
              </span>
              <span>{evidence.before}</span>
              {evidence.match && (
                <mark className="rounded-[3px] bg-accent-tint px-0.5 font-medium text-accent-ink">
                  {evidence.match}
                </mark>
              )}
              <span>{evidence.after}</span>
            </span>
          ) : (
            (line?.lead || previewText) && (
              <span className="truncate text-[11.5px] leading-4 text-ink-3">
                {line?.lead && (
                  <span className={cx("mr-1 font-medium", line.tone === "reply" ? "text-green" : "text-attention")}>
                    {line.lead}
                  </span>
                )}
                {previewText}
              </span>
            )
          )}

          {/* Line 3: Clean inline dot-separated metadata (Machine · Project · Tab · Harness · Model) */}
          {(metaParts.length > 0 || (hasErr && st !== "needs_you")) && (
            <span className="mt-0.5 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-[10.5px] leading-4 text-ink-3" data-testid="agent-chatlist-chips">
              {metaParts.map((part, i) => (
                <span key={part.key} className="inline-flex min-w-0 items-center gap-1.5">
                  {i > 0 && <span aria-hidden className="text-ink-3/50">·</span>}
                  {part.node}
                </span>
              ))}
              {hasErr && st !== "needs_you" && (
                <span className="inline-flex items-center text-red">
                  <Icon name="warning" size={10} stroke={2.2} />
                </span>
              )}
            </span>
          )}
        </span>
      </a>

      {/* Hover / right actions: One-click Pin/Unpin button + More menu button */}
      <div className="pointer-events-none absolute top-1.5 right-1.5 z-20 flex items-center gap-0.5 opacity-0 transition-opacity duration-150 group-hover/row:pointer-events-auto group-hover/row:opacity-100 focus-within:pointer-events-auto focus-within:opacity-100 [@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100">
        <button
          type="button"
          data-testid="agent-row-pin-button"
          aria-label={pinned ? t("sidebar.unpin") : t("sidebar.pin")}
          title={pinned ? t("sidebar.unpin") : t("sidebar.pin")}
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            void flipPin(s);
          }}
          className={cx(
            "flex size-6 items-center justify-center rounded-[6px] transition-[background-color,color] duration-150",
            active ? "hover:bg-hover" : "hover:bg-hover-2",
            pinned ? "text-accent" : "text-ink-3 hover:text-ink",
          )}
        >
          <Icon name="pin" size={12} stroke={2.1} />
        </button>
        {onMenu && (
          <button
            type="button"
            aria-label={t("sidebar.agent_actions")}
            aria-haspopup="menu"
            title={t("sidebar.more")}
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              onMenu(s, e.currentTarget.parentElement ?? e.currentTarget);
            }}
            className={cx("flex size-6 items-center justify-center rounded-[6px] text-ink-3 transition-[background-color,color] duration-150 hover:text-ink", active ? "hover:bg-hover" : "hover:bg-hover-2")}
          >
            <Icon name="more" size={13} />
          </button>
        )}
      </div>
    </div>
  );
});
