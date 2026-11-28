// Every keyboard focus ring is drawn whole (AGENTS/07, "Focus rings and clip room"). Against the mock hub's invented
// data, each view below is opened at a desktop and a phone width; every control in it is focused from the keyboard,
// and its ring (the outline, or a ring-like box shadow) is checked two ways:
//   - geometry: the ring's reach (width + offset past the border) against each ancestor that clips (overflow other than
//     visible, overflow-clip-margin counted; paint containment) and the window;
//   - pixels: the ring's band is sampled on its four sides in a screenshot, which finds what geometry cannot: a later
//     positioned box painted over it (the chat's bottom fade over the dock's first row).
// A finding names the control and the clipping box with the React components around them, so it leads to the source.
//
//   PORT=18760 MOCK_GOAL=1 MOCK_TASKS=1 python3 -I scripts/mock/mock_hub.py
//   cd web && PERSONA_HUB_URL=http://127.0.0.1:18760 npx vite --port 5173 --strictPort
//   node web/e2e/focus-ring-room.mjs http://127.0.0.1:5173 <output dir> [--only <view name>] [--report]
//
// It exits 1 when any ring is cut, unless --report (the findings are written to <output dir>/focus-ring-room.json).
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { chromium } from "playwright-core";
import { RING_PAGE, ringPixels } from "./focus-ring-lib.mjs";

const args = process.argv.slice(2);
const flag = (name) => args.includes(name);
const option = (name) => (args.includes(name) ? args[args.indexOf(name) + 1] : undefined);
const [address, directory] = args.filter((a, i) => !a.startsWith("--") && !(i > 0 && ["--only"].includes(args[i - 1])));
const base = new URL(address);
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(base.hostname), "loopback only");
const health = await (await fetch(new URL("/api/v1/health", base), { signal: AbortSignal.timeout(5000) })).json();
assert.equal(health.version, "mock", "Refuse a real hub, including through a loopback proxy");
const output = resolve(directory);
await mkdir(output, { recursive: true });

const WIDGET = "a1f3c2d4-0000-4000-8000-000000000001"; // MOCK_GOAL=1: an active goal, a plan
const REVIEW = "a1f3c2d4-0000-4000-8000-000000000006"; // MOCK_GOAL=1: a finished goal, the dock's only row

/** The views, each with what to open first. `click` runs in order; a step that finds nothing is skipped. */
const VIEWS = [
  { name: "main", path: "/", ready: "main [data-chat-dock]" },
  { name: "main-tray", path: "/", ready: "main [data-assistant-actionbar] [data-tray-entry]", click: ['main [data-tray-entry="running"]'] },
  { name: "main-tray-needs", path: "/", ready: "main [data-assistant-actionbar] [data-tray-entry]", click: ['main [data-tray-entry="needs"]'] },
  { name: "main-tray-finished", path: "/", ready: "main [data-assistant-actionbar] [data-tray-entry]", click: ['main [data-tray-entry="finished"]'] },
  { name: "agent-goal", path: `/s/macbook/${REVIEW}`, ready: "main [data-goal-bar]" },
  { name: "agent-working", path: `/s/macbook/${WIDGET}`, ready: "main [data-goal-bar]" },
  { name: "goal-dialog", path: `/s/macbook/${REVIEW}`, ready: "main [data-goal-bar]", click: ["main [data-goal-bar]"], within: "[role=dialog]" },
  { name: "overview", path: "/overview", ready: "main" },
  { name: "inbox", path: "/inbox", ready: "main" },
  { name: "todos", path: "/todos", ready: "main" },
  { name: "machines", path: "/machines", ready: "main" },
  { name: "agents", path: "/agents", ready: "main" },
  { name: "settings", path: "/settings", ready: "main" },
  { name: "routines", path: "/routines", ready: "main" },
];
const WIDTHS = [{ width: 1440, height: 900 }, { width: 390, height: 844 }];

const findings = [];
const counts = {};
const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN, args: ["--no-sandbox"] });
try {
  for (const view of VIEWS.filter((v) => !option("--only") || v.name === option("--only"))) {
    for (const size of WIDTHS) {
      const phone = size.width < 500;
      const context = await browser.newContext({ viewport: size, deviceScaleFactor: 2, locale: "zh-CN", colorScheme: "light", serviceWorkers: "block", hasTouch: phone, isMobile: phone, reducedMotion: "reduce" });
      await context.addInitScript(() => { localStorage.setItem("persona.theme", "light"); localStorage.setItem("persona.language", "zh-Hans"); });
      await context.addInitScript(RING_PAGE);
      const page = await context.newPage();
      const where = `${view.name}@${size.width}`;
      try {
        await page.goto(new URL(`${view.path}?token=mock`, base).href);
        await page.locator(view.ready).first().waitFor({ timeout: 20000 });
        await page.waitForTimeout(1200);
        for (const target of view.click ?? []) {
          const el = page.locator(target).first();
          if (await el.count()) { await el.click(); await page.waitForTimeout(600); }
        }
        if (view.within) await page.locator(view.within).first().waitFor({ timeout: 5000 });
        const ids = await page.evaluate((within) => window.__ring.controls(within), view.within ?? null);
        counts[where] = { controls: ids.length, checked: 0, noRing: 0, unseen: 0 };
        await page.keyboard.press("Shift"); // keyboard modality: programmatic focus now shows :focus-visible
        for (const i of ids) {
          const focused = await page.evaluate((i) => {
            const el = document.querySelector(`[data-ring-audit='${i}']`);
            el?.focus();
            return document.activeElement === el;
          }, i);
          if (!focused) continue;
          await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
          const m = await page.evaluate((i) => window.__ring.measure(i), i);
          if (m.skip) continue;
          counts[where].checked++;
          if (m.none) { counts[where].noRing++; continue; }
          if (m.unseen) { counts[where].unseen++; continue; }
          const shot = {};
          const pixels = await ringPixels(page, m, shot);
          const hidden = pixels && !(await page.evaluate((s) => window.__ring.tooltipOver(s), m.shape))
            ? Object.entries(pixels).filter(([, v]) => v !== null && v < 60).map(([k, v]) => `${k} ${v}%`) : [];
          if (m.cuts.length || hidden.length) {
            const id = `${where}-${findings.length}`;
            if (shot.png && (hidden.length || m.cuts.length)) await writeFile(join(output, `${id}.png`), shot.png);
            findings.push({ id, where, control: m.control, ring: `${m.ring.kind} ${m.ring.width}px offset ${m.ring.offset}px`, cut: m.cuts, hidden });
          }
        }
      } catch (e) {
        findings.push({ where, error: String(e?.message || e).split("\n")[0] });
      } finally {
        await context.close();
      }
    }
  }
} finally {
  await browser.close();
}

await writeFile(join(output, "focus-ring-room.json"), JSON.stringify({ counts, findings }, null, 1));
for (const [where, c] of Object.entries(counts)) console.log(`${where}: ${c.checked}/${c.controls} focused controls checked, ${c.noRing} without a ring, ${c.unseen} not shown while focused`);
for (const f of findings) {
  if (f.error) { console.log(`ERROR ${f.where}: ${f.error}`); continue; }
  console.log(`CUT ${f.id} ${f.control.owner || f.control.tag} "${f.control.label}" [${f.control.data.join(" ")}] ${f.ring}`);
  for (const c of f.cut) console.log(`    clipped ${c.sides.join(", ")} by ${c.box.owner || c.box.tag} ${c.box.tag} [${(c.box.data || []).join(" ")}] "${c.box.cls || ""}"`);
  if (f.hidden.length) console.log(`    painted over: ${f.hidden.join(", ")}`);
}
console.log(`${findings.length} finding(s)`);
if (findings.length && !flag("--report")) process.exit(1);
