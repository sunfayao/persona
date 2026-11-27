// The To-dos page with a keyboard and on wide and narrow screens (web audit items 74, 75 and 88), in Chrome against the
// fake hub's to-dos:
//
//   - item 74: at a phone's width, Enter on a row opens the to-do with the focus on its Back; Esc, Back and the
//     browser's Back return to the list with the focus on that row and the list scrolled where it was; Esc on a to-do
//     opened by a link puts the focus on its row;
//   - item 75: at 1920 wide the detail keeps a page's reading width (760 px), centered in its column;
//   - item 88: back on the page from another one (the browser's Back), the list is scrolled where it was and the open
//     to-do's row is in view.
//
// Needs the web page against the fake hub with its tasks (made-up data), and Chrome:
//
//   MOCK_TASKS=1 MOCK_TODO_ASKS=1 PORT=18931 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18931 npx vite --port 5931 --strictPort &)
//   node web/e2e/todos-keyboard-layout.mjs http://127.0.0.1:5931 [--shots <dir>] [--executable <chromium>]
//
// Runs in Chinese, light and dark. Prints one line per check and a final verdict; exits 1 on a failed check and after
// four minutes.
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const flag = (name) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : undefined;
};
const base = (args[0] ?? "").replace(/\/$/, "");
const shots = flag("--shots");
const executable = flag("--executable");
if (!base) {
  console.error("usage: node web/e2e/todos-keyboard-layout.mjs <web URL> [--shots <dir>] [--executable <chromium>]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });
setTimeout(() => {
  console.log("VERDICT: timeout after 4 minutes");
  process.exit(1);
}, 240_000).unref();

let failed = 0;
const check = (cond, msg) => {
  console.log(`${cond ? "ok  " : "FAIL"} ${msg}`);
  if (!cond) failed++;
};
// one part of the run: a step that throws (a row that never shows) is a failed check, and the run goes on
const step = async (name, run) => {
  try {
    await run();
  } catch (e) {
    check(false, `${name}: ${String(e?.message ?? e).split("\n")[0]}`);
  }
};
const shot = async (page, name) => {
  if (shots) await page.screenshot({ path: join(shots, `${name}.png`) });
};

async function open(browser, theme, viewport, path) {
  const ctx = await browser.newContext({ viewport, deviceScaleFactor: 2, locale: "zh-CN" });
  await ctx.addInitScript((t) => {
    localStorage.setItem("persona.theme", t);
    localStorage.setItem("persona.language", "zh-Hans");
    localStorage.setItem("persona.todos.grouping", "status");
  }, theme);
  const page = await ctx.newPage();
  page.setDefaultTimeout(10000);
  await page.goto(`${base}${path}${path.includes("?") ? "&" : "?"}token=mock`);
  // attached, not visible: a phone's width opened at a to-do hides the list under it
  await page.waitForSelector('[data-testid="todos-sections"] [data-testid="todo-section"]', { state: "attached", timeout: 20000 });
  await page.waitForTimeout(500);
  return { ctx, page };
}

// The two rows the checks use, by their titles in the fake hub's to-dos.
const WEATHER = "天气 App 发 1.3 版";
const NAS = "给 NAS 配自动备份";
const rowOf = (page, title) => page.locator('div[role="button"]').filter({ has: page.locator('[data-testid="todo-title"]', { hasText: title }) }).first();
// what has the focus: a row (by its title), Back, a test id, or the tag
const focus = (page) =>
  page.evaluate(() => {
    const a = document.activeElement;
    if (!a || a === document.body) return "body";
    if (a.getAttribute("role") === "button" && a.querySelector("[data-testid=todo-title]")) return `row:${a.querySelector("[data-testid=todo-title]").textContent}`;
    return a.closest("[data-testid=todos-back]") ? "back" : a.getAttribute("data-testid") || a.tagName;
  });
const listTop = (page) => page.locator('[data-testid="todos-list-column"] .overflow-y-auto').first().evaluate((el) => el.scrollTop);
// the row is inside the list's visible part
const inView = async (page, title) => {
  const r = await rowOf(page, title).boundingBox();
  const l = await page.locator('[data-testid="todos-list-column"] .overflow-y-auto').first().boundingBox();
  return Boolean(r && l && r.height > 0 && r.y >= l.y - 1 && r.y + r.height <= l.y + l.height + 1);
};

const browser = await chromium.launch({ ...(executable ? { executablePath: executable } : { channel: "chrome" }), headless: true });
try {
  for (const theme of ["light", "dark"]) {
    // ── item 74: a phone's width, the list and the detail taking turns ──
    await step(`${theme}: a phone's width`, async () => {
      const { ctx, page } = await open(browser, theme, { width: 390, height: 844 }, "/todos");
      const row = rowOf(page, WEATHER);
      await row.focus();
      await page.keyboard.press("Enter");
      await page.waitForTimeout(500);
      check(page.url().includes("id=todo_weather") && (await focus(page)) === "back", `${theme}: Enter on a row opens the to-do with the focus on Back (${await focus(page)})`);
      await shot(page, `todos-narrow-opened-${theme}`);
      await page.locator('[data-testid="todos-back"] button').focus();
      await page.keyboard.press("Escape");
      await page.waitForTimeout(500);
      check(!page.url().includes("id=") && (await focus(page)) === `row:${WEATHER}`, `${theme}: Esc goes back to the list with the focus on the row (${page.url().replace(base, "")}, ${await focus(page)})`);
      await shot(page, `todos-narrow-after-esc-${theme}`);

      // a row further down: the list comes back scrolled where it was, by Back and by the browser's Back
      const far = rowOf(page, NAS);
      await far.scrollIntoViewIfNeeded();
      const before = await listTop(page);
      await far.focus();
      await page.keyboard.press("Enter");
      await page.waitForTimeout(500);
      await page.locator('[data-testid="todos-back"] button').click();
      await page.waitForTimeout(600);
      const afterBack = await listTop(page);
      check(before > 0 && Math.abs(afterBack - before) <= 2 && (await focus(page)) === `row:${NAS}`, `${theme}: Back returns to the list scrolled where it was (${before} → ${afterBack}) with the focus on the row (${await focus(page)})`);
      await far.focus();
      await page.keyboard.press("Enter");
      await page.waitForTimeout(500);
      await page.goBack();
      await page.waitForTimeout(600);
      check(Math.abs((await listTop(page)) - before) <= 2 && (await focus(page)) === `row:${NAS}`, `${theme}: the browser's Back does the same (${await listTop(page)}, ${await focus(page)})`);
      await ctx.close();
    });
    await step(`${theme}: a phone's width, opened by a link`, async () => {
      const { ctx, page } = await open(browser, theme, { width: 390, height: 844 }, "/todos?id=todo_nas");
      await page.keyboard.press("Escape");
      await page.waitForTimeout(500);
      check(!page.url().includes("id=") && (await focus(page)) === `row:${NAS}`, `${theme}: Esc on a to-do opened by a link goes to the list with the focus on its row (${page.url().replace(base, "")}, ${await focus(page)})`);
      await ctx.close();
    });

    // ── item 75: the detail's reading width on a wide screen ──
    await step(`${theme}: the reading width`, async () => {
      const { ctx, page } = await open(browser, theme, { width: 1920, height: 1080 }, "/todos?id=todo_weather");
      // the detail's own box: the column's last child (a phone's width puts Back above it)
      const detail = page.locator('[data-testid="todos-detail-column"] > div:last-child');
      await detail.waitFor({ timeout: 10000 });
      const box = await detail.boundingBox();
      const col = await page.locator('[data-testid="todos-detail-column"]').boundingBox();
      const left = box.x - col.x;
      const right = col.x + col.width - (box.x + box.width);
      check(box.width <= 760 && Math.abs(left - right) <= 16, `${theme}: at 1920 the detail is ${Math.round(box.width)} px wide, centered (${Math.round(left)} / ${Math.round(right)})`);
      await shot(page, `todos-detail-1920-${theme}`);
      await ctx.close();
    });

    // ── item 88: back on the page from another one ──
    await step(`${theme}: back on the page`, async () => {
      const { ctx, page } = await open(browser, theme, { width: 1440, height: 700 }, "/todos");
      const row = rowOf(page, NAS);
      await row.scrollIntoViewIfNeeded();
      await row.click();
      await page.waitForTimeout(500);
      const before = await listTop(page);
      await page.getByRole("link", { name: /机器/ }).first().click();
      await page.waitForTimeout(800);
      await page.goBack();
      await page.waitForSelector('[data-testid="todos-sections"]', { timeout: 10000 });
      await page.waitForTimeout(1200);
      const after = await listTop(page);
      check(before > 0 && Math.abs(after - before) <= 2 && (await inView(page, NAS)), `${theme}: back from Machines the list is scrolled where it was (${before} → ${after}) and the open to-do's row is in view`);
      await shot(page, `todos-list-after-back-${theme}`);
      await ctx.close();
    });
  }
} finally {
  await browser.close();
}
console.log(failed ? `VERDICT: fail (${failed} checks)` : "VERDICT: pass");
process.exit(failed ? 1 : 0);
