import { describe, expect, it } from "vitest";
import type { HubConnector } from "./connectors";
import { inboxSources } from "./inbox-sources";

const c = (over: Partial<HubConnector>): HubConnector => ({ id: "telegram", name: "Telegram", status: "not_signed_in", capabilities: ["login", "inbox"], ...over });

describe("inboxSources", () => {
  it("is not connected with no mail, no Feishu and no signed-in connector", () => {
    const r = inboxSources({ mail: 0, lark: false, connectors: [c({}), c({ id: "github", name: "GitHub", status: "not_configured", capabilities: ["login", "inbox"] })] });
    expect(r.connected).toBe(false);
  });

  it("is connected by a mail account, Feishu set up, or a signed-in connector that brings items in", () => {
    expect(inboxSources({ mail: 1, connectors: [] }).connected).toBe(true);
    expect(inboxSources({ mail: 0, lark: true, connectors: [] }).connected).toBe(true);
    expect(inboxSources({ mail: 0, connectors: [c({ status: "ok" })] }).connected).toBe(true);
  });

  it("does not count a connector that is off, keeps out of the inbox, or only talks (a bot)", () => {
    expect(inboxSources({ mail: 0, connectors: [c({ status: "ok", enabled: false })] }).connected).toBe(false);
    expect(inboxSources({ mail: 0, connectors: [c({ status: "ok", inbox: false })] }).connected).toBe(false);
    const bot = c({ id: "wechat", name: "微信", status: "ok", capabilities: ["login"] });
    expect(inboxSources({ mail: 0, connectors: [bot] }).connected).toBe(false);
  });
});
