// Settings › Notifications: this browser's system notifications and Web Push (WebPushSettings.tsx),
// the two that need the browser's own permission prompt. Everything else about what Persona tells you is said to the main
// conversation.
import { useState } from "react";
import { t } from "../../i18n";
import { setSystemNotify, useSystemNotify, type Permission } from "../../lib/notify";
import { toast } from "../../ui/atoms";
import { Group, Pill, Row, Switch } from "./SettingsParts";
import { WebPushSettings } from "./WebPushSettings";

function permissionHint(p: Permission, on: boolean): string | undefined {
  if (p === "unsupported") return t("settings-notifications.system_unsupported");
  if (p === "denied") return t("settings-notifications.system_denied_short");
  if (p === "default" || !on) return t("settings-notifications.system_ask");
  return t("settings-notifications.system_on");
}

export function NotificationSettings() {
  const system = useSystemNotify();
  const [asking, setAsking] = useState(false);

  const toggleSystem = async (on: boolean) => {
    setAsking(true);
    try {
      const p = await setSystemNotify(on);
      if (on && p === "denied") toast(t("settings-notifications.system_denied"), "error");
    } finally {
      setAsking(false);
    }
  };

  const systemActive = system.on && system.permission === "granted";

  return (
    <>
      <Group title={t("settings-notifications.system_title")} anchor="system" footer={t(system.permission === "denied" ? "settings-notifications.system_denied" : "settings-notifications.system_footer")}>
        <Row icon="bell" anchor="notify-system" label={t("settings-notifications.system")} hint={permissionHint(system.permission, system.on)}>
          {system.permission === "denied" && <Pill tone="orange">{t("settings-notifications.denied_pill")}</Pill>}
          <Switch
            on={systemActive}
            onChange={(on) => void toggleSystem(on)}
            disabled={asking || system.permission === "unsupported" || system.permission === "denied"}
            label={t("settings-notifications.system")}
          />
        </Row>
      </Group>

      <WebPushSettings />

      <p className="px-1 text-[11.5px] leading-relaxed text-ink-3">{t("settings-notifications.device_note")}</p>
    </>
  );
}
