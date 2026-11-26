// Use only an isolated mock started with MOCK_SESSIONS_LIST=1 MOCK_SESSIONS_CATALOG=1 MOCK_MANAGERS=1.
import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const [url, captures] = process.argv.slice(2);
assert.ok(url && captures && process.env.PERSONA_E2E_CHROMIUM);
assert.equal(new URL(url).hostname, "127.0.0.1");
mkdirSync(captures, { recursive: true });
const browser = await chromium.launch({ executablePath: process.env.PERSONA_E2E_CHROMIUM, args: ["--no-sandbox"] });
const cwd = "/Users/me/code/weather";
const firstText = "Review the weather fixture";
const target = { machine_id: "macbook", source_id: "w1", source: "herdr", cwd, title: "weather" };
const fixtureModel = { id: "claude-fixture", name: "Preview model", efforts: ["medium", "high"] };

async function verifyDesktopProjectFolder(theme, selected) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: theme, locale: "en-US" });
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(12_000);
    await page.addInitScript((theme) => {
      localStorage.setItem("persona.theme", theme);
      localStorage.setItem("persona.language", "en");
    }, theme);
    const launchers = [
      { name: "codex", agent: "codex", source: "zshrc" },
      { name: "agy", agent: "agy", source: "agy" },
      { name: "traex", agent: "traex", source: "zshrc" },
      { name: "claude-desktop", agent: "claude", source: "claude_desktop" },
    ];
    await page.route("**/api/v1/pins", (route) => route.fulfill({ json: { spaces: [], sessions: [] } }));
    await page.route("**/api/v1/workspaces", (route) => route.fulfill({ json: { workspaces: [{ ...target, source: "claude_desktop" }] } }));
    await page.route("**/api/v1/machines/macbook/launchers", (route) =>
      route.fulfill({ json: { default: { provider: "codex" }, launchers } }));
    await page.route("**/api/v1/machines/macbook/launchers/*/models*", (route) => {
      const launcher = launchers.find((l) => new URL(route.request().url()).pathname.endsWith(`/${l.name}/models`));
      return route.fulfill({ json: { agent: launcher?.agent, models: [{ id: launcher?.name, name: `Fixture ${launcher?.name}` }] } });
    });
    await page.route("**/api/v1/sessions", (route) => route.request().method() === "POST"
      ? route.fulfill({ status: 409, json: { code: "fixture_blocked", error: "Synthetic startup refusal", delivered: "none" } })
      : route.continue());

    await page.goto(`${url}/?token=mock`);
    await page.locator(`[data-session-group='${JSON.stringify([target.machine_id, target.source_id])}'] [data-space-add] button`).first().click();
    const draft = page.getByTestId("folder-agent-draft");
    await draft.locator("button[data-model-pill=ready]").click();
    const picker = page.locator("[data-model-panel]");
    const provider = (name) => picker.locator(`[data-panel-side="${name}"]`);
    await provider("claude-desktop").waitFor();
    for (const { name } of launchers) await provider(name).waitFor({ timeout: 3000 });
    await page.screenshot({ path: join(captures, `desktop-folder-${theme}.png`) });
    await provider(selected).click();
    await picker.getByText(`Fixture ${selected}`, { exact: true }).click();
    await draft.locator("textarea").fill(firstText);
    const request = page.waitForRequest((req) => req.method() === "POST" && new URL(req.url()).pathname === "/api/v1/sessions");
    await draft.getByTestId("composer-send").click();
    const body = (await request).postDataJSON();
    assert.equal(body.cwd, cwd);
    assert.equal(body.launcher, selected);
    assert.equal(body.workspace_id, undefined);
    assert.equal(body.host, selected === "claude-desktop" ? undefined : "herdr");
    assert.equal(body.desktop_project_id, selected === "claude-desktop" ? target.source_id : undefined);
    console.log(`PASS ${theme}: all Mac launchers remain selectable in a desktop project folder; ${selected} keeps the correct destination`);
  } finally {
    await context.close();
  }
}

try {
  await verifyDesktopProjectFolder("dark", "traex");
  await verifyDesktopProjectFolder("light", "claude-desktop");
  for (const [theme, narrow] of [["dark", false], ["light", false], ["dark", true]]) {
    const tag = theme + (narrow ? "-narrow" : "-wide");
    const context = await browser.newContext({
      viewport: narrow ? { width: 390, height: 844 } : { width: 1440, height: 900 },
      colorScheme: theme, locale: "en-US", hasTouch: narrow, isMobile: narrow, reducedMotion: "reduce",
    });
    try {
      const page = await context.newPage();
      page.setDefaultTimeout(22_000);
      await page.addInitScript((theme) => {
        localStorage.setItem("persona.theme", theme);
        localStorage.setItem("persona.language", "en");
      }, theme);
      const errors = [];
      const starts = [];
      const messages = [];
      const uploads = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await page.route("**/api/v1/pins", (route) => route.fulfill({ json: { spaces: [], sessions: [] } }));
      await page.route("**/api/v1/workspaces", (route) => route.fulfill({ json: { workspaces: [target] } }));
      await page.route("**/api/v1/machines/macbook/launchers/claude/models*", (route) =>
        route.fulfill({ json: { agent: "claude", models: [fixtureModel] } }));
      page.on("request", (request) => {
        if (request.method() !== "POST") return;
        const { pathname } = new URL(request.url());
        if (pathname === "/api/v1/sessions") starts.push(request.postDataJSON());
        if (/^\/api\/v1\/sessions\/[^/]+\/[^/]+\/messages$/.test(pathname)) messages.push(request.postDataJSON());
        if (pathname === "/api/v1/machines/macbook/files") uploads.push(request);
      });
      let refuse = !narrow && theme === "light";
      await page.route("**/api/v1/sessions", async (route) => {
        if (route.request().method() === "POST" && refuse) {
          refuse = false;
          return route.fulfill({ status: 409, json: { code: "fixture_blocked", error: "Synthetic startup refusal", delivered: "none" } });
        }
        return route.continue();
      });

      await page.goto(`${url}${narrow ? "/agents" : "/"}?token=mock`);
      const project = page.locator(`${narrow ? "main " : ""}[data-session-group='${JSON.stringify([target.machine_id, target.source_id])}'] [data-space-add] button`).first();
      await project.click();
      const draftRoute = page.url();
      const draft = page.getByTestId("folder-agent-draft");
      const box = draft.locator("textarea");
      await box.waitFor();
      assert.match(draftRoute, /\/agent-draft\//);
      assert.equal(await page.getByRole("dialog").count(), 0);
      const model = draft.locator("button[data-model-pill]");
      await model.waitFor();
      if (!narrow) {
        await model.click();
        if (theme === "light") await page.getByRole("dialog").getByText("Preview model", { exact: true }).click();
        else await page.keyboard.press("Escape");
        await page.waitForFunction(() => !history.state?.personaOverlay);
      }
      await box.fill(firstText);
      const imageData = await page.evaluate(() => {
        const square = document.createElement("canvas");
        square.width = 8;
        square.height = 8;
        const ink = square.getContext("2d");
        ink.fillStyle = "steelblue";
        ink.fillRect(0, 0, 8, 8);
        return square.toDataURL("image/png").split(",")[1];
      });
      await draft.locator('input[type="file"]').setInputFiles([
        { name: "fixture.txt", mimeType: "text/plain", buffer: Buffer.from("runner fixture") },
        { name: "fixture.png", mimeType: "image/png", buffer: Buffer.from(imageData, "base64") },
      ]);
      await draft.getByText("fixture.txt", { exact: true }).waitFor();
      await draft.locator('img[src^="blob:"]').waitFor();
      assert.deepEqual([starts.length, messages.length, uploads.length], [0, 0, 0], "selection, typing, and attachments make no launch/upload request");
      await page.goBack();
      await page.waitForURL((target) => !target.pathname.includes("/agent-draft/"));
      await project.click();
      assert.equal(page.url(), draftRoute, "reopening the same project returns to the same draft");
      await box.waitFor();
      await draft.locator('img[src^="blob:"]').waitFor();
      await draft.getByText("fixture.txt", { exact: true }).waitFor();
      assert.equal(await box.inputValue(), firstText);
      if (theme === "light") assert.match(await model.innerText(), /Preview model/);
      assert.equal(await draft.evaluate((node) => node.scrollWidth > node.clientWidth), false);
      await page.screenshot({ path: join(captures, `${tag}.png`) });

      await draft.getByTestId("composer-send").click();
      if (theme === "light") {
        await page.getByText(/Synthetic startup refusal/).waitFor();
        await page.getByRole("button", { name: "Retry", exact: true }).click();
      }
      await page.waitForURL(/\/s\/macbook\/[^/]+$/);
      await page.waitForFunction(() => !location.pathname.includes("/local-"));
      assert.equal(starts.length, theme === "light" ? 2 : 1);
      assert.equal(uploads.length, 1);
      assert.equal(messages.length, 1, "the first turn is delivered once");
      for (const body of starts) {
        assert.equal(body.machine_id, target.machine_id);
        assert.equal(body.cwd, target.cwd);
        assert.equal(body.workspace_id, target.source_id);
        assert.equal(body.host, target.source);
        if (theme === "light") assert.equal(body.model, fixtureModel.id);
      }
      assert.ok(messages[0].text.startsWith(firstText));
      assert.ok(messages[0].text.includes("fixture.txt"));
      assert.equal(messages[0].images.length, 1);
      assert.deepEqual(errors, []);
      console.log(`PASS ${tag}: local draft, Back, model picker, attachments, pinned creation, first turn`);
    } finally {
      await context.close();
    }
  }
} finally {
  await browser.close();
}
