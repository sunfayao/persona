// Verify title layout in the real sidebar, using the synthetic mock hub with
// MOCK_MANAGERS=1 MOCK_SESSIONS_LIST=1 MOCK_SESSIONS_CATALOG=1.
import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const [origin, mock, shots, mode] = process.argv.slice(2);
for (const url of [origin, mock]) assert.match(url, /^http:\/\/(localhost|127\.0\.0\.1):\d+$/);
assert.ok(shots, "supply mock web origin, mock API origin and screenshot directory");
const baseline = mode === "--baseline";
const ordinary = "检查设置页面在手机横屏和竖屏时的长标题换行、按钮可读性以及最后一项是否完整显示";
const prompt = "Verify that every conversation remains readable on small screens. ".repeat(4).trim();
const fits = "ill ".repeat(30) + "i";
const widget = "a1f3c2d4-0000-4000-8000-000000000001";
const tests = "a1f3c2d4-0000-4000-8000-000000000002";
mkdirSync(shots, { recursive: true });

const measure = (title) => title.evaluate((el) => {
  const range = document.createRange();
  range.selectNodeContents(el);
  const box = el.getBoundingClientRect();
  return { width: Math.round(box.width), height: Math.round(box.height), lines: range.getClientRects().length,
    clipped: el.scrollHeight > el.clientHeight + 1 || el.scrollWidth > el.clientWidth + 1 };
});

const headers = { Authorization: "Bearer mock", "Content-Type": "application/json" };
const reads = await (await fetch(mock + "/mock/sessions_reads")).json();
assert.ok("reads" in reads && "hints" in reads, "requires the dedicated mock hub");
const original = (await (await fetch(mock + "/api/v1/sessions", { headers })).json()).sessions;
const rename = async (id, title) => {
  const response = await fetch(mock + "/api/v1/sessions/macbook/" + id + "/rename", { method: "POST", headers, body: JSON.stringify({ title }) });
  assert.ok(response.ok, "failed to rename the synthetic fixture");
};
const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN, args: ["--no-sandbox"] });
try {
  for (const [width, theme, fitting] of [[320, "dark", false], [390, "light", false], [1280, "dark", false], [600, "light", true]]) {
    const phone = width < 1024;
    const long = fitting ? fits : prompt;
    const ctx = await browser.newContext({ viewport: { width, height: 900 }, hasTouch: phone, isMobile: phone, colorScheme: theme, locale: "zh-CN", serviceWorkers: "block" });
    try {
      await ctx.addInitScript(theme => {
        localStorage.setItem("persona.language", "zh-Hans");
        localStorage.setItem("persona.theme", theme);
      }, theme);
      await rename(widget, ordinary);
      await rename(tests, long);
      const page = await ctx.newPage();
      page.setDefaultTimeout(15000);
      await page.goto(origin + "/overview?token=mock");
      if (phone) await page.locator("main").getByRole("button", { name: /侧边栏/ }).first().click();
      await page.waitForLoadState("networkidle");
      const drawer = page.locator("[data-sidebar-column]");
      const name = drawer.getByText(ordinary, { exact: true }).first();
      await name.scrollIntoViewIfNeeded();
      await page.evaluate(() => document.fonts.ready);
      const before = await measure(name);
      const drawerWidth = Math.round((await drawer.boundingBox()).width);
      console.log(JSON.stringify({ mode: baseline ? "before" : "after", width, drawerWidth, title: before }));
      if (!baseline && !phone) assert.ok(before.width >= 160, "desktop title gets the context column width");
      if (!baseline && phone) {
        assert.ok(drawerWidth <= width - 43 && drawerWidth <= 401, "leave a dismissal target");
        assert.ok(!before.clipped && before.lines > 1, "ordinary CJK title fully visible");
        const row = drawer.locator("[data-sidebar-long-title]", { has: page.getByText(long, { exact: true }) }).first();
        const disclosure = row.locator(".sidebar-title-disclosure");
        const longTitle = row.getByText(long, { exact: true });
        await longTitle.scrollIntoViewIfNeeded();
        if (fitting) {
          assert.ok(!(await measure(longTitle)).clipped, "narrow glyphs fit within three lines");
          assert.ok(await disclosure.isHidden(), "fitting names have no placeholder action");
        } else {
          await disclosure.waitFor({ state: "visible" });
          assert.ok((await measure(longTitle)).clipped, "prompt starts folded");
          await disclosure.tap();
          assert.equal(await disclosure.getAttribute("aria-expanded"), "true");
          assert.ok(!(await measure(longTitle)).clipped, "full prompt fits after expansion");
          await disclosure.tap();
          assert.equal(await disclosure.getAttribute("aria-expanded"), "false");
          const menu = row.locator("button[aria-haspopup=menu]");
          await menu.tap();
          await page.getByRole("menu").first().waitFor();
          await page.keyboard.press("Escape");
        }
      }
      await name.scrollIntoViewIfNeeded();
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await page.screenshot({ path: join(shots, `${width}-${theme}.png`), animations: "disabled" });
    } finally {
      await ctx.close();
    }
  }
} finally {
  await browser.close();
  for (const id of [widget, tests]) await rename(id, original.find(row => row.session_id === id).title);
}
