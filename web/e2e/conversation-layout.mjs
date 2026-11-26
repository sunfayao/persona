// Run against a local Persona mock and its web bundle. Covers the Agent conversation at three viewport widths.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "playwright-core";

const [base, shots] = process.argv.slice(2);
assert.equal(new URL(base).hostname, "127.0.0.1");
const sessionId = "a1f3c2d4-0000-4000-8000-000000000006";
const requestTitle = "Review the log rotation schedule";
const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN, args: ["--no-sandbox"] });
try {
  if (shots) await mkdir(shots, { recursive: true });
  for (const width of [1280, 390, 320]) for (const theme of ["light", "dark"]) {
    const ctx = await browser.newContext({ viewport: { width, height: width === 1280 ? 850 : 740 }, hasTouch: width < 500, isMobile: width < 500, locale: "zh-CN", colorScheme: theme, serviceWorkers: "block" });
    const page = await ctx.newPage();
    try {
      await page.addInitScript((theme) => {
        localStorage.setItem("persona.theme", theme);
        localStorage.setItem("persona.language", "zh-Hans");
      }, theme);
      await page.route(/\/api\/v1\/sessions(\?|$)/, async (route) => {
        const response = await route.fetch();
        const body = await response.json();
        for (const row of body.sessions ?? []) if (row.session_id === sessionId) {
          row.title = '<persona-context kind="context" title="Build" url="">fixture</persona-context>\n\n' + requestTitle;
        }
        await route.fulfill({ response, json: body });
      });
      await page.goto(base + "/s/macbook/" + sessionId + "?token=mock");
      const input = page.locator("main textarea[data-composer-input]").first();
      await input.waitFor({ timeout: 20000 });
      const title = page.locator("[data-chat-title]").first();
      await page.waitForFunction((title) => document.querySelector("[data-chat-title]")?.textContent === title, requestTitle, { timeout: 12000 });
      assert.equal(await title.textContent(), requestTitle);
      const fields = page.locator("main [data-composer-fields]").first();
      assert.equal(await fields.evaluate((el) => getComputedStyle(el).flexDirection), "column");
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
      const add = fields.locator("[data-composer-plus]");
      const model = fields.locator("[data-model-pill]");
      assert.ok(await add.isVisible());
      assert.ok(await model.isVisible());
      const measureControls = () => page.evaluate(() => {
        const row = document.querySelector("main [data-composer-fields]");
        const input = row?.querySelector("[data-composer-input]")?.getBoundingClientRect();
        const add = row?.querySelector("[data-composer-plus]")?.getBoundingClientRect();
        const perm = row?.querySelector("[data-testid=permission-picker]")?.getBoundingClientRect();
        const model = row?.querySelector("[data-model-pill]")?.getBoundingClientRect();
        return {
          input: input && { left: input.left, right: input.right, top: input.top, bottom: input.bottom, width: input.width },
          add: add && { left: add.left, right: add.right, top: add.top, bottom: add.bottom, height: add.height },
          perm: perm && { left: perm.left, right: perm.right, top: perm.top, bottom: perm.bottom, width: perm.width },
          model: model && { left: model.left, right: model.right, top: model.top, bottom: model.bottom, width: model.width },
        };
      });
      await page.waitForFunction(() => {
        const row = document.querySelector("main [data-composer-fields]");
        const input = row?.querySelector("[data-composer-input]")?.getBoundingClientRect();
        const add = row?.querySelector("[data-composer-plus]")?.getBoundingClientRect();
        return Boolean(input?.width && add?.height && input.bottom <= add.top + 1);
      });
      const initial = await measureControls();
      if (initial.perm) {
        assert.ok(initial.perm.left >= initial.add.right - 1, `permission picker should sit right of + at ${width}px`);
        assert.ok(initial.perm.top >= initial.input.bottom - 1, `permission picker should sit below input at ${width}px`);
        assert.ok(Math.abs(initial.perm.top - initial.add.top) <= 4, `permission picker should align with + at ${width}px`);
      }
      if (shots) await page.screenshot({ path: join(shots, `chat-${width}-${theme}.png`), animations: "disabled" });
      await input.fill("你把这个修复合并到deploy分支吧，但是这个分支有很多的修改，如果遇到冲突需要你自己处理一下。");
      assert.equal(await fields.evaluate((el) => getComputedStyle(el).flexDirection), "column");
      const withCjk = await measureControls();
      assert.ok(withCjk.input.bottom <= withCjk.add.top + 1);
      if (initial.perm && withCjk.perm) {
        assert.ok(Math.abs(withCjk.perm.left - initial.perm.left) <= 1, `permission picker x shifted on CJK draft at ${width}px`);
        assert.ok(Math.abs(withCjk.perm.top - withCjk.add.top) <= 4, `permission picker misaligned with + on CJK draft at ${width}px`);
      }
      await input.fill("One short line\nA second line");
      assert.equal(await fields.evaluate((el) => getComputedStyle(el).flexDirection), "column");
      const multiline = await measureControls();
      assert.ok(multiline.input.bottom <= multiline.add.top + 1);
      if (initial.perm && multiline.perm) {
        assert.ok(Math.abs(multiline.perm.left - initial.perm.left) <= 1, `permission picker x shifted on multiline draft at ${width}px`);
        assert.ok(Math.abs(multiline.perm.top - multiline.add.top) <= 4, `permission picker misaligned with + on multiline draft at ${width}px`);
      }
      assert.ok(await fields.locator("[data-testid=composer-send]").isEnabled());
      console.log(`PASS ${width} ${theme}: readable title, stacked composer, anchored controls, multiline draft`);
    } finally {
      await page.unrouteAll({ behavior: "wait" });
      await ctx.close();
    }
  }
} finally { await browser.close(); }
