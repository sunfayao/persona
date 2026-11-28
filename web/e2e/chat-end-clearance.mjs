// The chat's last line clears the fade at its end, whatever sits under the chat (AGENTS/07 rule 13), and the rows
// over the composer draw their focus rings whole (rule 12). A long invented transcript ending in a worked turn (its
// last reply says when, and how long it worked) is loaded into three chats of the mock hub: an Agent with nothing over
// its composer, an Agent whose goal is the only row over its composer (MOCK_GOAL=1), and the main conversation with its
// task capsules (MOCK_TASKS=1). Each chat is checked as it is, with a five-line draft and with a picture attached, at a
// desktop and a phone width, light and dark:
//   - scrolled to the end, the chat's last box ends, and the last reply's "worked for" line's glyphs end, at least
//     8 px above where the fade begins;
//   - the dock starts where the chat's box ends: nothing of it reaches up into the chat, where the fade is drawn;
//   - the goal bar (clicked, its dialog closed with Esc, as the owner does) and every task capsule (focused from the
//     keyboard) show their focus ring on all four sides.
//
//   PORT=18760 MOCK_GOAL=1 MOCK_TASKS=1 python3 -I scripts/mock/mock_hub.py
//   cd web && PERSONA_HUB_URL=http://127.0.0.1:18760 npx vite --port 5173 --strictPort
//   node web/e2e/chat-end-clearance.mjs http://127.0.0.1:5173 <output dir>
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { chromium } from "playwright-core";
import { RING_PAGE, ringPixels } from "./focus-ring-lib.mjs";

const [address, directory] = process.argv.slice(2);
const base = new URL(address);
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(base.hostname), "loopback only");
const health = await (await fetch(new URL("/api/v1/health", base), { signal: AbortSignal.timeout(5000) })).json();
assert.equal(health.version, "mock", "Refuse a real hub, including through a loopback proxy");
const output = resolve(directory);
await mkdir(output, { recursive: true });

/** The gap the end padding leaves between the chat's last box and the fade (--spacing-fade-end minus --spacing-fade). */
const GAP = 8;
const CHATS = [
  { name: "agent", path: "/s/macbook/a1f3c2d4-0000-4000-8000-000000005426" },
  { name: "agent-goal", path: "/s/macbook/a1f3c2d4-0000-4000-8000-000000000006", goal: true },
  { name: "main", path: "/", trays: true },
];
const DOCKS = ["as-is", "draft", "picture"];

function transcript() {
  const now = Date.now(), items = [];
  const add = (role, kind, minutes, more) => items.push({ id: `clearance-${items.length + 1}#0`, seq: items.length + 1, ts: new Date(now - minutes * 60_000).toISOString(), role, kind, ...more });
  for (let i = 0; i < 6; i++) {
    add("user", "text", 60 - i * 5, { text: `Example question ${i + 1}` });
    add("assistant", "text", 59 - i * 5, { text: Array(4).fill("An invented answer, long enough that the conversation scrolls past one screen.").join("\n\n") });
  }
  add("user", "text", 12, { text: "Example final question" });
  add("assistant", "thinking", 11.9, { text: "Read the file first." });
  add("assistant", "tool_use", 11.8, { tool_use_id: "toolu_clearance", tool_name: "Read", tool_input: { file_path: "/example/src/app.ts" } });
  add("user", "tool_result", 11.8, { tool_use_id: "toolu_clearance", text: "export const app = 1" });
  add("assistant", "text", 11, { text: "Done: the example change is in place." });
  return items;
}
const ITEMS = transcript();

/** A small picture to attach, drawn in the page. */
const picture = async (page) => Buffer.from((await page.evaluate(() => {
  const c = document.createElement("canvas");
  c.width = 320;
  c.height = 200;
  const g = c.getContext("2d");
  g.fillStyle = "#5aa9e6";
  g.fillRect(0, 0, 320, 200);
  g.fillStyle = "#ffd166";
  g.fillRect(40, 40, 120, 80);
  return c.toDataURL("image/png");
})).split(",")[1], "base64");

/** Scrolled to the end: the chat's box, the fade, the last box, the "worked for" glyphs and the dock. */
const atEnd = (page) => page.evaluate(() => {
  const sc = document.querySelector("main [data-chat-scroll]");
  sc.scrollTop = sc.scrollHeight;
  const box = (el) => { const r = el?.getBoundingClientRect(); return r && r.height ? { top: r.top, bottom: r.bottom } : null; };
  // the lowest box drawn in the transcript (its rows are display: contents, so not the last child's box)
  const all = document.createRange();
  all.selectNodeContents(sc.querySelector("[data-chat-content]"));
  const last = { bottom: all.getBoundingClientRect().bottom };
  const work = [...sc.querySelectorAll("[data-work-time]")].at(-1);
  let glyphs = null;
  if (work) {
    const range = document.createRange();
    range.selectNodeContents(work);
    const g = box(range);
    if (g && last.bottom - g.bottom < 30) glyphs = g; // the last reply's line is the chat's end (not a card after it)
  }
  return {
    scroller: box(sc), fade: box(sc.parentElement.querySelector(":scope > [data-scroll-fade]")), last, glyphs,
    work: work?.textContent, dock: box(document.querySelector("main [data-chat-dock]")),
    above: box(document.querySelector("main [data-chat-dock-above]:not([hidden])")),
  };
});

/** The focused control's ring: not clipped by any box, and drawn on all four sides. */
async function wholeRing(page, what) {
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  const m = await page.evaluate(() => window.__ring.measure());
  assert.ok(!m.skip && !m.none && !m.unseen, `${what}: focused with a visible ring (${JSON.stringify(m.skip ?? m)})`);
  assert.deepEqual(m.cuts, [], `${what}: no box clips its ring`);
  const shot = {};
  const pixels = await ringPixels(page, m, shot);
  assert.ok(pixels, `${what}: an outline ring to sample`);
  const missing = Object.entries(pixels).filter(([, share]) => share !== null && share < 90);
  if (missing.length && shot.png) {
    await writeFile(join(output, `${what.replace(/\W+/g, "-")}-ring.png`), shot.png);
    await writeFile(join(output, `${what.replace(/\W+/g, "-")}-ring.json`), JSON.stringify({ m, pixels }, null, 1));
  }
  for (const [side, share] of missing) assert.fail(`${what}: ring drawn on its ${side} (${share}%)`);
  return pixels;
}

const results = [];
const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN, args: ["--no-sandbox"] });
try {
  for (const chat of CHATS.filter((c) => !process.env.CLEARANCE_ONLY || c.name === process.env.CLEARANCE_ONLY)) for (const size of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) for (const theme of ["light", "dark"]) {
    const phone = size.width < 500;
    const context = await browser.newContext({ viewport: size, deviceScaleFactor: 2, locale: "zh-CN", colorScheme: theme, serviceWorkers: "block", hasTouch: phone, isMobile: phone, reducedMotion: "reduce" });
    await context.addInitScript((theme) => { localStorage.setItem("persona.theme", theme); localStorage.setItem("persona.language", "zh-Hans"); }, theme);
    await context.addInitScript(RING_PAGE);
    await context.route(/\/api\/v1\/sessions\/[^/]+\/[^/]+\/items(\?|$)/, (route) => route.fulfill({ json: { items: ITEMS, has_more: false, more_after: false, history: "clearance-fixture", from_seq: 1, to_seq: ITEMS.length } }));
    const page = await context.newPage();
    const where = `${chat.name}-${size.width}-${theme}`;
    try {
      await page.goto(new URL(`${chat.path}?token=mock`, base).href);
      await page.locator("main [data-work-time]").last().waitFor({ state: "attached", timeout: 20000 });
      if (chat.goal) await page.locator("main [data-goal-bar]").first().waitFor({ timeout: 10000 });
      if (chat.trays) await page.locator("main [data-assistant-actionbar] [data-tray-entry]").first().waitFor({ timeout: 10000 });
      await page.waitForTimeout(800);
      for (const dock of DOCKS) {
        const input = page.locator("main textarea[data-composer-input]").first();
        if (dock === "draft") await input.fill(Array.from({ length: 5 }, (_, i) => `Draft line ${i + 1}`).join("\n"));
        if (dock === "picture") {
          await input.fill("");
          await page.locator('main input[type="file"]').first().setInputFiles({ name: "example.png", mimeType: "image/png", buffer: await picture(page) });
        }
        await page.waitForTimeout(500);
        await atEnd(page); // scrolled to the end once, then again once what it laid out (a picture) has settled
        await page.waitForTimeout(150);
        const end = await atEnd(page);
        assert.ok(end.fade && end.last, `${where} ${dock}: the chat has a fade and a last box`);
        assert.ok(end.last.bottom <= end.fade.top - GAP + 0.5, `${where} ${dock}: the last box ends ${GAP} px above the fade (${end.last.bottom} / ${end.fade.top})`);
        if (end.glyphs) assert.ok(end.glyphs.bottom <= end.fade.top - GAP, `${where} ${dock}: "${end.work}" ends above the fade (${end.glyphs.bottom} / ${end.fade.top})`);
        assert.ok(end.dock.top >= end.scroller.bottom - 0.5, `${where} ${dock}: the dock starts where the chat ends`);
        if (end.above) assert.ok(end.above.top >= end.scroller.bottom - 0.5, `${where} ${dock}: nothing over the composer reaches into the chat`);
        const shot = join(output, `${where}-${dock}.png`);
        await page.screenshot({ path: shot, clip: { x: 0, y: Math.max(0, end.fade.top - 120), width: size.width, height: Math.min(size.height - Math.max(0, end.fade.top - 120), 420) } });
        results.push({ where, dock, clearance: +(end.fade.top - (end.glyphs ?? end.last).bottom).toFixed(1), screenshot: shot });
        console.log(`PASS ${where} ${dock}: last line ${(end.fade.top - (end.glyphs ?? end.last).bottom).toFixed(1)} px above the fade`);
      }
      await page.locator("main textarea[data-composer-input]").first().fill("");
      if (chat.goal) {
        const goal = page.locator("main [data-goal-bar]").first();
        await goal.click();
        await page.locator("[role=dialog]").first().waitFor();
        await page.keyboard.press("Escape");
        await page.waitForFunction(() => document.activeElement?.matches("[data-goal-bar]:focus-visible"), null, { timeout: 5000 });
        const pixels = await wholeRing(page, `${where} goal bar`);
        await page.screenshot({ path: join(output, `${where}-goal-focus.png`), clip: await page.evaluate(() => { const r = document.activeElement.getBoundingClientRect(); return { x: Math.max(0, r.left - 24), y: r.top - 24, width: Math.min(innerWidth - Math.max(0, r.left - 24), r.width + 48), height: r.height + 48 }; }) });
        console.log(`PASS ${where} goal bar: ring on every side ${JSON.stringify(pixels)}`);
      }
      if (chat.trays) {
        await page.keyboard.press("Shift");
        for (const entry of await page.locator("main [data-assistant-actionbar] [data-tray-entry]").all()) {
          await entry.focus();
          await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
          const key = await entry.getAttribute("data-tray-entry");
          const pixels = await wholeRing(page, `${where} capsule ${key}`);
          console.log(`PASS ${where} capsule ${key}: ring on every side ${JSON.stringify(pixels)}`);
        }
        await page.screenshot({ path: join(output, `${where}-capsule-focus.png`), clip: await page.evaluate(() => { const r = document.activeElement.getBoundingClientRect(); return { x: 0, y: r.top - 40, width: innerWidth, height: r.height + 80 }; }) });
      }
    } finally {
      await context.close();
    }
  }
} finally {
  await browser.close();
  await writeFile(join(output, "chat-end-clearance.json"), JSON.stringify(results, null, 1));
}
console.log(`${results.length} chat ends checked`);
