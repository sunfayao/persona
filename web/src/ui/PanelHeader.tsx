// The header every side panel draws (views/SidePanel.tsx). Kept out of SidePanel, which loads the panels, so a panel
// never imports the module that imports it.
import { t } from "../i18n";
import { IconButton } from "./atoms";
import { Icon, type IconName } from "./icons";

// The panel's header: an icon, a title (and a quieter line under it), actions, and ×.
export function PanelHeader({ icon, title, subtitle, actions, onClose }: { icon: IconName; title: React.ReactNode; subtitle?: React.ReactNode; actions?: React.ReactNode; onClose: () => void }) {
  return (
    <header className="flex min-h-12 shrink-0 items-center gap-2.5 border-b border-line/70 bg-page/90 px-3.5 py-2 backdrop-blur-sm">
      <Icon name={icon} size={15} className="shrink-0 text-ink-3" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[14px] leading-snug font-medium tracking-[-0.01em] text-ink max-sm:whitespace-normal max-sm:break-words">{title}</div>
        {subtitle && <div className="truncate text-[11.5px] leading-tight text-ink-3">{subtitle}</div>}
      </div>
      {actions}
      <IconButton icon="x" label={t("sidepanel.close")} onClick={onClose} />
    </header>
  );
}
