import { lazy, Suspense, useEffect, useRef, useState, type ComponentProps, type RefObject } from "react";
import { cliSlashRows, slashQuery, useCLISlashCommands, type SlashTarget } from "../../lib/cli-slash-commands";
import { rankCommands } from "../../lib/composer-commands";
import type { ComposerCommand } from "../../lib/extension-prompts";
import { useSavedPromptRows } from "../../lib/saved-prompt-rows";

const SlashPalette = lazy(() => import("./SlashPalette").then((m) => ({ default: m.SlashPalette })));

export function NewAgentSlashField({ target, input, value, onValue, onKeyDown, ...props }: Omit<ComponentProps<"textarea">, "value" | "onChange"> & {
  target?: SlashTarget;
  input: RefObject<HTMLTextAreaElement | null>;
  value: string;
  onValue: (value: string) => void;
}) {
  const anchor = useRef<HTMLDivElement>(null);
  const [dismissed, setDismissed] = useState(false);
  const [composing, setComposing] = useState(false);
  const [active, setActive] = useState(0);
  const query = dismissed || composing || !target ? null : slashQuery(value);
  const { rows: native, loading } = useCLISlashCommands(target, query !== null);
  const saved = useSavedPromptRows(query !== null);
  const rows = query === null ? [] : rankCommands([...cliSlashRows(native), ...saved], query, native.length + saved.length);
  const index = Math.min(active, Math.max(0, rows.length - 1));
  useEffect(() => setActive(0), [query, target?.machine, target && "launcher" in target && target.launcher,
    target && "cwd" in target && target.cwd]);
  const pick = (row: ComposerCommand) => {
    onValue(row.text);
    setDismissed(true);
    input.current?.focus({ preventScroll: true });
  };
  return (
    <div ref={anchor} className="relative">
      {query !== null && (
        <Suspense fallback={null}>
          <SlashPalette rows={rows} active={index} onHover={setActive} onPick={pick}
            onClose={() => setDismissed(true)} within={anchor} loading={loading} cliLoading={loading} />
        </Suspense>
      )}
      <textarea
        {...props} ref={input} value={value}
        onChange={(e) => { setDismissed(false); onValue(e.target.value); }}
        onCompositionStart={() => setComposing(true)}
        onCompositionEnd={() => setComposing(false)}
        onKeyDown={(e) => {
          if (query !== null && !e.nativeEvent.isComposing && e.keyCode !== 229 && !e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey) {
            if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              setDismissed(true);
              return;
            }
            if (rows.length && (e.key === "ArrowUp" || e.key === "ArrowDown")) {
              e.preventDefault();
              setActive((index + (e.key === "ArrowDown" ? 1 : rows.length - 1)) % rows.length);
              return;
            }
            if (rows.length && (e.key === "Enter" || e.key === "Tab")) {
              e.preventDefault();
              pick(rows[index]);
              return;
            }
          }
          onKeyDown?.(e);
        }}
      />
    </div>
  );
}
