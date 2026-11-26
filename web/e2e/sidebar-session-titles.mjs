// Verify title layout in the real sidebar, using the synthetic mock hub with
// MOCK_MANAGERS=1 MOCK_SESSIONS_LIST=1 MOCK_SESSIONS_CATALOG=1.
import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const [origin, shots, mode] = process.argv.slice(2);
assert.ok(/^http:\/\/(localhost|127\.0\.0\.1):\d+$/.test(origin) && shots, "supply mock web origin and screenshot directory");
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

const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN, args: ["--no-sandbox"] });
try {
  for (const [width, theme, fitting] of [[320, "dark", false], [390, "light", false], [1280, "dark", false], [600, "light", true]]) {
    const phone = width < 1024;
    const long = fitting ? fits : prompt;
    const ctx = await browser.newContext({ viewport: { width, height: 900 }, hasTouch: phone, isMobile: phone, colorScheme: theme, locale: "zh-CN", serviceWorkers: "block" });
    let closing = false;
    try {
      await ctx.addInitScript(theme => {
        localStorage.setItem("persona.language", "zh-Hans");
        localStorage.setItem("persona.theme", theme);
      }, theme);
      await ctx.route("**/api/v1/sessions*", async route => {
        try {
        if (new URL(route.request().url()).pathname !== "/api/v1/sessions") return route.continue();
        const source = await route.fetch();
        const body = await source.json();
        for (const row of body.sessions ?? []) {
          if (row.session_id === widget) row.title = ordinary;
          if (row.session_id === tests) row.title = long;
        }
        await route.fulfill({ response: source, json: body });
        } catch (error) { if (!closing) throw error; }
      });
      const page = await ctx.newPage();
      page.setDefaultTimeout(15000);
      await page.goto(origin + "/overview?token=mock");
      if (phone) await page.locator("main").getByRole("button", { name: /侧边栏/ }).first().click();
      const drawer = page.locator("[data-sidebar-column]");
      const name = drawer.getByText(ordinary, { exact: true }).first();
      await name.scrollIntoViewIfNeeded();
      await page.evaluate(() => document.fonts.ready);
      const before = await measure(name);
      const drawerWidth = Math.round((await drawer.boundingBox()).width);
      console.log(JSON.stringify({ mode: baseline ? "before" : "after", width, drawerWidth, title: before }));
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
      closing = true;
      await ctx.close();
    }
  }
} finally {
  await browser.close();
}
