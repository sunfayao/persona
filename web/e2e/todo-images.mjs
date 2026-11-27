// Browser acceptance for to-do images (#11084): a to-do made through the synthetic hub's API with a picture, as the main
// conversation makes one, shows it on its page, still after a reload, opens the existing zoom viewer, and offers nothing
// that adds or removes a picture. Runs desktop/phone, light/dark, and the Web Large text setting.
//
//   PORT=18984 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18984 npx vite --host 127.0.0.1 --port 5984 --strictPort &)
//   node web/e2e/todo-images.mjs http://127.0.0.1:5984 [--shots <dir>] [--executable <chromium>]
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
  console.error("usage: node web/e2e/todo-images.mjs <web URL> [--shots <dir>] [--executable <chromium>]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });
setTimeout(() => {
  console.log("FAIL timed out after 5 minutes");
  process.exit(1);
}, 300_000).unref();

const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAGAAAABACAIAAABqVuVZAAABqUlEQVR4nO3brU4DURDF8QvZ1zgW9Co0lqeBIKsqkA3vgqgEXXU0mpdAIjbB7d3u/ZqZO/eolZtf/u0kTXpz9/wZNO3j9xg07Vb6BVQPp/Mk/Q5Kh9N5eRgFxXRCCKOgVZplo6CYzigoRrNsFBQiOrGCvt8fQ+Xdv3wFxTTeC8IVOk6/g3AdjdOCsEfHV0HYSeOrICTpuCgIqTQuCkKeTs8FIZum54JQSOfn9am3glCIZtHp7SOGcuH8P3cChAo0/XwHoZqO+YJQk8Y8EEroRGgMA6F+ONtA4r9myYZjsiC0pTF2xSChY6MgCNHYAEK2TjKNdiCIhqMdCNLh6AWCGhqNV4xvD6p0FBVEfTSKgJinU4lGBRC1hqMCiIrDEQaiBRqxK0Y7Oq0Loima1kDM0BGhaQdEg+G0A6LNcFoA0ThN3SvGLnSqFMQMmvlw0fZvn0mJzny4BJWblIQTtK4MELsLpxgQ+6UpcMXYu056QXRAkw7EJB1zNClAdBNOChA9hbMPiC5prr1idKyzURB902wAJejMfdGsAo1wYkAjnFWgQRO7YkMnrOwPBY2wDBygVIEAAAAASUVORK5CYII=",
  "base64",
);
const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });
const ok = (message) => console.log(`ok  ${message}`);
const fail = (message) => {
  console.log(`FAIL ${message}`);
  process.exitCode = 1;
  throw new Error(message);
};

try {
  for (const textSize of ["standard", "large"]) {
    for (const theme of ["light", "dark"]) {
      for (const width of [1280, 390]) {
        const tag = `${width}px ${theme} ${textSize}`;
        const context = await browser.newContext({
          viewport: { width, height: width > 600 ? 900 : 844 },
          deviceScaleFactor: 2,
          colorScheme: theme,
          locale: "en-US",
          hasTouch: width <= 600,
        });
        await context.addInitScript(({ theme, textSize }) => {
          localStorage.setItem("persona.theme", theme);
          localStorage.setItem("persona.language", "en");
          localStorage.setItem("persona.text-size", textSize);
        }, { theme, textSize });
        const page = await context.newPage();
        page.setDefaultTimeout(20_000);
        await page.goto(`${base}/?token=mock`);
        await page.waitForFunction(() => localStorage.getItem("persona.token"));
        // the to-do as the main conversation makes it: through the hub, with its picture
        const id = await page.evaluate(async (data) => {
          const res = await fetch("/api/v1/todos", {
            method: "POST",
            headers: { "Content-Type": "application/json", Authorization: `Bearer ${localStorage.getItem("persona.token")}` },
            body: JSON.stringify({ title: "Check the dialog screenshot", notes: "The attached picture marks the expected spacing.",
              images: [{ id: crypto.randomUUID(), name: "diagram.png", mime: "image/png", data }] }),
          });
          return (await res.json()).todo?.id;
        }, png.toString("base64"));
        if (!id) fail(`${tag}: the hub did not make the to-do`);
        await page.goto(`${base}/todos?id=${id}`);
        const detail = page.locator('[data-testid="todos-detail-column"]');
        await detail.locator('img[alt="diagram.png"]').waitFor();
        if (await detail.locator('input[type="file"]').count()) fail(`${tag}: the page offers to add a picture`);
        if (await detail.getByRole("button", { name: /Remove diagram\.png/ }).count()) fail(`${tag}: the page offers to remove a picture`);
        if (shots) await page.screenshot({ path: join(shots, `todo-images-${width}-${theme}-${textSize}-detail.png`), fullPage: true });
        await page.reload();
        const persisted = page.locator('[data-testid="todos-detail-column"] img[alt="diagram.png"]');
        await persisted.waitFor();
        const source = await persisted.getAttribute("src");
        if (!source?.startsWith("blob:")) fail(`${tag}: detail did not read the private image into an object URL (${source})`);
        await persisted.locator("xpath=ancestor::button[1]").click();
        const viewer = page.getByRole("dialog").last();
        await viewer.getByRole("button", { name: "Download", exact: true }).waitFor();
        if (!(await viewer.locator(`img[src="${source}"]`).count())) fail(`${tag}: zoom viewer does not show the saved image`);
        if (shots) await page.screenshot({ path: join(shots, `todo-images-${width}-${theme}-${textSize}-viewer.png`), fullPage: true });
        ok(`${tag}: shown, kept after a reload and zoomed, with nothing to add or remove`);
        await context.close();
      }
    }
  }
  console.log("PASS");
} catch (error) {
  if (!process.exitCode) {
    console.log(`FAIL ${error instanceof Error ? error.message : String(error)}`);
    process.exitCode = 1;
  }
} finally {
  await browser.close();
}
