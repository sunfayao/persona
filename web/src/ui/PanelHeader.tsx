// The header every side panel draws (views/SidePanel.tsx). Kept out of SidePanel, which loads the panels, so a panel
// never imports the module that imports it.
import { t } from "../i18n";
import { IconButton } from "./atoms";
import { Icon, type IconName } from "./icons";

// The panel's header: an icon, a title (and a quieter line under it), actions, and ×.
export function PanelHeader({ icon, title, subtitle, actions, onClose }: { icon: IconName; title: React.ReactNode; subtitle?: React.ReactNode; actions?: React.ReactNode; onClose: () => void }) {
  return (
    <header className="flex min-h-12 shrink-0 items-center gap-2 border-b border-line px-3 py-2">
      <Icon name={icon} size={16} className="shrink-0 text-ink-3" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm leading-snug font-semibold text-ink max-sm:whitespace-normal max-sm:break-words">{title}</div>
        {subtitle && <div className="truncate text-[11.5px] text-ink-3">{subtitle}</div>}
      </div>
      {actions}
      <IconButton icon="x" label={t("sidepanel.close")} onClick={onClose} />
    </header>
  );
}
