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
      assert.equal(await fields.evaluate((el) => getComputedStyle(el).flexDirection), width === 1280 ? "row" : "column");
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
      const add = fields.locator("[data-composer-plus]");
      const model = fields.locator("[data-model-pill]");
      assert.ok(await add.isVisible());
      assert.ok(await model.isVisible());
      await page.waitForFunction((wide) => {
        const row = document.querySelector("main [data-composer-fields]");
        const input = row?.querySelector("[data-composer-input]")?.getBoundingClientRect();
        const add = row?.querySelector("[data-composer-plus]")?.getBoundingClientRect();
        return input?.width && add?.height && (wide ? add.right <= input.left + 1 : input.bottom <= add.top + 1);
      }, width === 1280);
      if (shots) await page.screenshot({ path: join(shots, `chat-${width}-${theme}.png`), animations: "disabled" });
      await input.fill("One short line\nA second line");
      assert.equal(await fields.evaluate((el) => getComputedStyle(el).flexDirection), "column");
      assert.ok(await fields.locator("[data-testid=composer-send]").isEnabled());
      console.log(`PASS ${width} ${theme}: readable title, adaptive input, visible controls, multiline draft`);
    } finally {
      await page.unrouteAll({ behavior: "wait" });
      await ctx.close();
    }
  }
} finally { await browser.close(); }
