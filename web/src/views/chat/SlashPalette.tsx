import { useEffect, useId, type ReactNode, type RefObject } from "react";
import type { ComposerCommand as Command } from "../../lib/extension-prompts";
import { t } from "../../i18n";
import { PickerMenu, type PickerButton } from "./PickerMenu";

function source(c: Command) {
  if (!c.native) return c.source;
  if (c.native === "builtin") return t("chat-slashpalette.source_builtin");
  if (c.native === "skill") return t("chat-slashpalette.source_skill");
  return t("chat-slashpalette.source_command");
}

function typed(c: Command) { return c.text.trim() === c.word ? "" : c.text.trim(); }
function description(c: Command) {
  return <>
    {c.desc && <p className="text-ink-2">{c.desc}</p>}
    {typed(c) && <p className="line-clamp-4 whitespace-pre-line text-ink-3">{t("chat-slashpalette.types", { text: typed(c) })}</p>}
  </>;
}
export type SlashProps = {
  rows: Command[]; active: number; onHover: (i: number) => void; onPick: (row: Command) => void;
  onClose: () => void; within: RefObject<HTMLElement | null>;
};
export function SlashPalette({ rows, active, onHover, onPick, onClose, within, groups, renderRow, renderDetail, status, loading, cliLoading }: SlashProps & {
  groups?: (row: Command) => string; renderRow?: (row: Command, common: PickerButton) => ReactNode;
  renderDetail?: (row: Command) => ReactNode; status?: ReactNode; loading?: boolean; cliLoading?: boolean;
}) {
  const id = useId();
  useEffect(() => {
    const down = (e: Event) => { if (!within.current?.contains(e.target as Node)) onClose(); };
    document.addEventListener("pointerdown", down, true);
    return () => document.removeEventListener("pointerdown", down, true);
  }, [onClose, within]);
  const focused = rows[active];
  const key = (c: Command) => c.key ?? c.word;
  const name = (c: Command) => c.native ? c.word : c.word.replace(/^\//, "");
  const detail = (c: Command) => renderDetail?.(c) || ((c.desc || typed(c)) ? description(c) : undefined);
  return (
    <PickerMenu mode="slash" items={rows} current={active} itemKey={key} group={groups} wide={Boolean(groups)}
      label={t(groups ? "chat-slashpalette.label_with_skills" : "chat-slashpalette.label")} moveOnly={Boolean(groups)}
      title={focused && name(focused)} detail={focused && detail(focused)} onHover={onHover} onPick={onPick}
      describedBy={(c, i) => !("skill" in c) && (c.desc || typed(c)) ? `${id}-${i}` : undefined}
      empty={!rows.length && !loading && <div role="status" className="flex h-10 items-center justify-center text-meta text-ink-3">{t(groups ? "chat-slashpalette.no_match_skills" : "chat-slashpalette.no_match")}</div>}
      footer={<>{cliLoading && <div role="status" className="px-2.5 py-2 text-meta text-ink-3">{t("chat-slashpalette.loading_cli")}</div>}{status}<div hidden>{rows.map((c, i) => !("skill" in c) && <div key={key(c)} id={`${id}-${i}`}>{detail(c)}</div>)}</div></>}
      renderRow={(c, common) => renderRow?.(c, common) || (
        <button {...common} className={`relative flex h-9 w-full items-center justify-between gap-3 rounded-control px-2.5 text-left text-sm text-ink [@media(pointer:coarse)]:h-11 ${common["aria-selected"] ? "font-medium" : "font-normal"}`}>
          {c.native && c.desc ? (
            <span className="min-w-0">
              <span className="block truncate">{name(c)}</span>
              <span className="block truncate text-meta font-normal text-ink-3">{c.desc}</span>
            </span>
          ) : <span className="min-w-0 truncate">{name(c)}</span>}
          {source(c) && <span className="max-w-[50%] shrink-0 truncate text-xs text-ink-3">{source(c)}</span>}
        </button>
      )} />
  );
}
