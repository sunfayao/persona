import { describe, expect, it } from "vitest";
import { channelOf, conversationChannel, type Channel } from "./channels";

const ch = (c: Partial<Channel> & Pick<Channel, "id">): Channel => ({
  name: c.id, kind: "im", source: "plugin", installed: true, enabled: true, state: "connected", capabilities: {}, bind: "qr", ...c,
});

const app = ch({ id: "app", name: "Persona", kind: "app", source: "builtin", bind: "none", read_only: true, surfaces: ["网页"] });
const wechat = ch({ id: "wechat", name: "微信", account: { name: "微信用户 a1b2c3" } });
const feishu = ch({ id: "feishu", name: "飞书机器人", source: "builtin", bind: "fields", state: "error", error: "长连接断了", account: { name: "示例用户" } });
describe("where a message came from", () => {
  const list = [app, wechat, feishu];
  it("reads the client_msg_id's prefix against the channels there are", () => {
    expect(channelOf("wechat-message:12", list)?.name).toBe("微信");
    expect(channelOf("feishu-om_1", list)?.id).toBe("feishu");
    expect(channelOf("3f2a9c1e-1111-4222-8333-444455556666", list)).toBeUndefined(); // the app's own
    expect(channelOf("telegram-9", list)).toBeUndefined();
    expect(channelOf("app-1", list)).toBeUndefined(); // the apps are not a source to mark
    expect(channelOf(undefined, list)).toBeUndefined();
  });

  it("names a conversation's latest channel, even one this hub no longer has", () => {
    expect(conversationChannel(["feishu", "wechat"], list)?.name).toBe("飞书机器人");
    expect(conversationChannel(["gone"], list)).toEqual({ id: "gone", name: "gone" });
    expect(conversationChannel([], list)).toBeUndefined();
  });
});
