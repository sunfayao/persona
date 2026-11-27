#!/usr/bin/env python3
"""Browser evidence for a to-do's **Repositories and work** section (Web).

Input for Browser Use, from the repository root:

    MOCK_TODO_LIST=1 PORT=18961 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18961 npx vite --port 5961 --strictPort &)
    REPOSITORY_WEB=http://127.0.0.1:5961 REPOSITORY_HUB=http://127.0.0.1:18961 browser-use < web/e2e/todo-repositories-browser.py

Only the association store's replies are synthetic: the page, the to-do list, its rows and the assistant's repository
catalog come from the mock hub, and `GET/PUT /todos/{id}/repositories` is answered in the browser by an invented store,
so no Agent, repository or Git checkout is involved. Checks, in English, light and dark at 1280 and 390 px, plus Large
text at 390: the section draws saved repositories and their PRs as links, with no control that adds, edits or removes
one (the Agent keeps the store); each repository carries its provider's own mark and its project address on one line,
and its links and unlisted note are indented beneath it; a saved link's complete title is its one hyperlink (and an
untitled link falls back to its kind and number); the legacy `pr_url` is not repeated beside it; a hub without the
route shows only the legacy link. Screenshots stay outside Git (`REPOSITORY_EVIDENCE_DIR`, default `/tmp/persona-todo-repositories-evidence`).
"""

import json
import os
import sys
import time
import urllib.request
from pathlib import Path

WEB = os.environ.get("REPOSITORY_WEB", "http://127.0.0.1:5961").rstrip("/")
HUB = os.environ.get("REPOSITORY_HUB", "http://127.0.0.1:18961").rstrip("/")
EVIDENCE = Path(os.environ.get("REPOSITORY_EVIDENCE_DIR", "/tmp/persona-todo-repositories-evidence"))
EVIDENCE.mkdir(parents=True, exist_ok=True)

IDENTITY = {"provider": "github", "instance_url": "https://github.com", "path": "example/web", "canonical_url": "https://github.com/example/web"}
GITLAB_IDENTITY = {"provider": "gitlab", "instance_url": "https://gitlab.example.com", "path": "team/api", "canonical_url": "https://gitlab.example.com/team/api"}


def hub_todos():
    # A loopback hub is reached directly: a proxy from the shell's environment refuses it.
    request = urllib.request.Request(f"{HUB}/api/v1/todos")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=10) as response:
        return json.load(response)["todos"]


TODOS = hub_todos()
TODO = next((item for item in TODOS if item.get("pr_url")), TODOS[0] if TODOS else None)
assert TODO, "the mock hub lists no to-do (start it with MOCK_TODO_LIST=1)"
TODO_ID, LEGACY_PR = TODO["id"], TODO.get("pr_url", "")


def fixture_source(legacy=False, empty=False, rich=False):
    """The whole page's fetch shim, installed before any page script so the section's first read finds it."""
    return """(() => {
  const state = window.repoFixture = {calls: [], revision: 2, numbers: [3211, 3212], legacy: %(legacy)s, empty: %(empty)s, rich: %(rich)s};
  const identity = %(identity)s, gitlabIdentity = %(gitlab)s, todoId = %(todo)s;
  const row = (id, source, links) => ({repo_id: id, requested_ref: {kind: 'default'},
    source_snapshot: {identity: source, clone_url: source.canonical_url, catalog_revision: 4, transport_origin: 'canonical_default'},
    links, catalog_state: 'current'});
  // Provider marks, one-line project addresses and the links' own full titles.
  const richSet = () => ({schema_version: 1, manager: "assistant", todo_id: todoId, revision: state.revision, repositories: [
    row('web', identity, [
      {kind: 'pull_request', number: '7', canonical_url: identity.canonical_url + '/pull/7', title: 'Fix the login redirect for every provider'},
      {kind: 'issue', number: '8', canonical_url: identity.canonical_url + '/issues/8'},
    ]),
    {...row('api', gitlabIdentity, [
      {kind: 'pull_request', number: '9', canonical_url: gitlabIdentity.canonical_url + '/-/merge_requests/9', title: 'Rotate access tokens without logging out'},
      {kind: 'issue', number: '10', canonical_url: gitlabIdentity.canonical_url + '/-/issues/10', title: 'Handle expired credentials gracefully across the whole flow'},
    ]), catalog_state: 'unlisted'},
  ]});
  const set = () => state.rich ? richSet() : state.empty ? {schema_version: 1, manager: "assistant", todo_id: todoId, revision: state.revision, repositories: []}
    : ({schema_version: 1, manager: "assistant", todo_id: todoId, revision: state.revision, repositories: [{
    repo_id: 'web', requested_ref: {kind: 'default'},
    source_snapshot: {identity, clone_url: identity.canonical_url, catalog_revision: 4, transport_origin: 'canonical_default'},
    links: state.numbers.map((n) => ({kind: 'pull_request', number: String(n), canonical_url: identity.canonical_url + '/pull/' + n})),
    catalog_state: 'current'}]});
  const reply = (body, status=200) => new Response(JSON.stringify(body), {status, headers: {'Content-Type': 'application/json'}});
  const original = window.fetch.bind(window);
  window.fetch = async (url, init={}) => {
    const path = String(url).split('/api/v1')[1], method = (init.method || 'GET').toUpperCase();
    if (!path || !path.includes('/repositories')) return original(url, init);
    state.calls.push({method, path});
    if (state.legacy) return reply({code: 'not_found', error: 'Not Found'}, 404);
    return reply(set());
  };
  return true;
})()""" % {"legacy": "true" if legacy else "false", "empty": "true" if empty else "false", "rich": "true" if rich else "false",
          "identity": json.dumps(IDENTITY), "gitlab": json.dumps(GITLAB_IDENTITY), "todo": json.dumps(TODO_ID)}


def js(expression):
    result = cdp("Runtime.evaluate", expression=expression, returnByValue=True, awaitPromise=True)
    if result.get("exceptionDetails"):
        raise AssertionError(f"page script failed: {result['exceptionDetails'].get('text')}: {expression[:120]}")
    return result["result"].get("value")


def eventually(expression, seconds=10):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if js(expression):
            return
        time.sleep(0.05)
    raise AssertionError(f"never true: {expression}")


def settle():
    js("new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))")
    eventually("document.getAnimations().every(a=>a.playState!=='running'||a.effect?.getTiming().iterations===Infinity)")


def shot(name):
    settle()
    capture_screenshot(str(EVIDENCE / f"{name}.png"))


def check(condition, message):
    assert condition, message
    print(f"ok  {message}")


OWNED_TABS: set[str] = set()


def close_owned_tabs():
    """Close only tabs this fixture created; a browser may carry the user's own tabs, which it never touches."""
    for target in tuple(OWNED_TABS):
        try:
            close_tab(target)
        except Exception:
            pass
        OWNED_TABS.discard(target)


def open_fixture_tab(url):
    # Supplying a URL may reuse a blank tab; create one explicitly before taking ownership.
    target = new_tab()
    OWNED_TABS.add(target)
    goto_url(url)


def fresh_tab():
    """Close only tabs this fixture opened, retaining the current one until the next document is ready."""
    ensure_real_tab()
    current = current_tab()
    keep = current.get("target_id") or current.get("targetId")
    for target in tuple(OWNED_TABS):
        if target != keep:
            try:
                close_tab(target)
                OWNED_TABS.discard(target)
            except Exception:
                pass


def open_todo(theme, width, legacy=False, large=False, empty=False, rich=False):
    fresh_tab()
    # The page is opened first and reloaded: Page.addScriptToEvaluateOnNewDocument only reaches documents created after
    # it, not the tab's first one.
    open_fixture_tab(f"{WEB}/?token=mock")
    wait_for_load()
    js("localStorage.setItem('persona.token','mock'); localStorage.setItem('persona.language','en'); "
       "localStorage.setItem('persona.theme',%s); true" % json.dumps(theme))
    js("localStorage.setItem('persona.text-size', %s); true" % json.dumps("large" if large else "standard"))
    open_fixture_tab(f"{WEB}/todos?id={TODO_ID}")
    wait_for_load()
    cdp("Page.enable")
    # A production build registers its service worker: its cached shell carries the previous build's asset names, so the
    # reloaded page would run old code. Unregister and clear its caches (a dev server has none).
    js("navigator.serviceWorker?.getRegistrations().then((rs) => Promise.all(rs.map((r) => r.unregister())))")
    js("caches?.keys().then((ks) => Promise.all(ks.map((k) => caches.delete(k))))")
    cdp("Page.addScriptToEvaluateOnNewDocument", source=fixture_source(legacy=legacy, empty=empty, rich=rich))
    cdp("Page.reload")
    wait_for_load()
    eventually("!!window.repoFixture")
    cdp("Emulation.setDeviceMetricsOverride", width=width, height=1100 if width > 600 else 1700, deviceScaleFactor=1, mobile=width <= 600)
    eventually("!!document.querySelector('[data-todo-repositories]')", seconds=30)
    eventually("!document.querySelector('[data-todo-repositories]').innerText.includes('Loading')", seconds=30)
    if large:
        check(js("document.documentElement.dataset.textSize") == "large", "the page is at the Large text size")
    settle()


def run(theme, width, suffix=""):
    where = f"{theme} {width}px{suffix}"
    open_todo(theme, width, large=bool(suffix))
    section = "document.querySelector('[data-todo-repositories]').innerText"
    check("example/web" in js(section), f"{where}: the saved repository is drawn on the page")
    check("#3211" in js(section) and "#3212" in js(section), f"{where}: both saved PRs are links, with no editor open")
    check(js("document.querySelectorAll('[data-todo-repositories] a[href$=\"/pull/3211\"], [data-todo-repositories] a[href$=\"/pull/3212\"]').length") == 2,
          f"{where}: each PR is one link")
    check(js("!document.querySelector('[data-todo-repositories] textarea')"), f"{where}: no multiline URL form")
    if LEGACY_PR:
        # The list still carries the to-do's own PR line; the detail draws the store's links, not that legacy one as well.
        check(js("document.querySelectorAll('[data-testid=todos-detail-column] a[href=%s]').length === 0" % json.dumps(LEGACY_PR)),
              f"{where}: the detail does not draw the legacy PR beside the store's links")
    shot(f"section-{width}-{theme}{suffix}")
    check(js("document.querySelectorAll('[data-todo-repositories] button, [data-todo-repositories] input').length === 0"),
          f"{where}: the section offers nothing that adds, edits or removes a link")
    check(js("![...document.querySelectorAll('[data-testid=todos-detail-column] button')].some((b) => b.innerText.trim() === 'Add repository')"),
          f"{where}: and the page has no Add repository")
    check(js("window.repoFixture.calls.every((c) => c.method === 'GET')"), f"{where}: the page only reads the store")


def run_empty(theme="light", width=1280):
    """The canonical store's own empty answer (the last repository removed) is authoritative: no legacy PR comes back."""
    open_todo(theme, width, empty=True)
    section = "document.querySelector('[data-todo-repositories]').innerText"
    eventually("document.querySelector('[data-todo-repositories]').innerText.includes('No repositories linked')")
    check(js("document.querySelector('[data-todo-repositories] a[href=%s]') === null" % json.dumps(LEGACY_PR)),
          "a successful empty store shows no legacy PR")
    check("Add repository" not in js(section), "and offers no editor")
    shot(f"section-empty-{width}-{theme}")


def run_legacy(theme="light", width=1280):
    open_todo(theme, width, legacy=True)
    eventually("document.querySelector('[data-todo-repositories]').innerText.includes('Linked before repositories were stored here.')")
    check(js("!!document.querySelector('[data-todo-repositories] a[href=%s]')" % json.dumps(LEGACY_PR)),
          "a hub without the route shows the legacy PR link")
    check(js("!document.querySelector('[data-todo-repositories]').innerText.includes('Manage links')"), "and offers no editor there")
    shot(f"section-legacy-{width}-{theme}")


def run_rich(theme, width, suffix=""):
    """Provider marks, the one-line address, and a link's complete title as its hyperlink."""
    where = f"{theme} {width}px{suffix} marks and titles"
    open_todo(theme, width, large=bool(suffix), rich=True)
    section = "document.querySelector('[data-todo-repositories]')"
    check(js(f"{section}.querySelectorAll('[data-repository-mark=\"github\"]').length") == 1, f"{where}: the GitHub repository carries GitHub's mark")
    check(js(f"{section}.querySelectorAll('[data-repository-mark=\"gitlab\"]').length") == 1, f"{where}: the GitLab repository carries GitLab's mark")
    check(js(f"{section}.innerText.includes('github.com/example/web')"), f"{where}: the GitHub project address is drawn inline")
    check(js(f"{section}.innerText.includes('gitlab.example.com/team/api')"), f"{where}: the GitLab project address is drawn inline")
    check(js(f"{section}.innerText.includes('Fix the login redirect for every provider')"), f"{where}: a saved PR shows its full title")
    check(js(f"{section}.innerText.includes('Rotate access tokens without logging out')"), f"{where}: a saved MR shows its full title")
    check(js(f"{section}.innerText.includes('Handle expired credentials gracefully across the whole flow')"), f"{where}: a saved issue shows its full title")
    check(js(f"{section}.querySelector('a[href$=\"/-/merge_requests/9\"]').getAttribute('aria-label').includes('!9')"),
          f"{where}: a GitLab MR keeps GitLab's !number")
    check(js(f"{section}.querySelectorAll('[data-link-icon=\"merge\"]').length") == 1, f"{where}: the MR row uses the merge mark")
    check(js(f"{section}.querySelectorAll('[data-link-icon=\"pullRequest\"]').length") == 1, f"{where}: the GitHub PR row uses the pull-request mark")
    check(js(f"{section}.querySelectorAll('[data-link-icon=\"target\"]').length") == 2, f"{where}: both issue rows use the issue mark")
    check(js(f"{section}.querySelector('a[href$=\"/-/merge_requests/9\"]') !== null"), f"{where}: the MR title is one hyperlink")
    check(js(f"{section}.querySelector('a[href$=\"/pull/7\"]') !== null"), f"{where}: the PR title is one hyperlink")
    check(js(f"{section}.querySelector('a[href$=\"/issues/8\"]') !== null"), f"{where}: an untitled issue falls back to its number")
    check(js("document.documentElement.scrollWidth <= document.documentElement.clientWidth"), f"{where}: no horizontal overflow")
    # Named type tokens: the link title is text-meta (13 px) and the address text-submeta (11.5 px), both scaled by
    # the same Small/Standard/Large ratio as the rest of the page.
    scale = 14 / 13 if suffix else 1
    meta_size = js(f"parseFloat(getComputedStyle({section}.querySelector('[data-repository-link] a > span')).fontSize)")
    submeta_size = js(f"parseFloat(getComputedStyle({section}.querySelector('[data-repository-row] a span')).fontSize)")
    check(abs(meta_size - 13 * scale) <= 0.2, f"{where}: the title uses the meta token ({meta_size:.2f}px)")
    check(abs(submeta_size - 11.5 * scale) <= 0.2, f"{where}: the address uses the submeta token ({submeta_size:.2f}px)")
    # Hierarchy: a child row's mark sits under the repository's name/address, and its title starts beyond that mark.
    repo_text = js(f"{section}.querySelector('[data-repository-row] a span').getBoundingClientRect().left")
    child_icon = js(f"{section}.querySelector('[data-repository-link] [data-link-icon]').getBoundingClientRect().left")
    child_text = js(f"{section}.querySelector('[data-repository-link] a > span').getBoundingClientRect().left")
    note_left = js("""(() => {
      const note = document.querySelector('[data-todo-repositories] [data-repository-note]');
      const text = document.createRange(); text.selectNodeContents(note);
      return text.getBoundingClientRect().left;
    })()""")
    check(abs(child_icon - repo_text) <= 1, f"{where}: child rows indent under the repository ({child_icon:.1f} vs {repo_text:.1f})")
    check(child_text > child_icon + 10, f"{where}: child titles start beyond their mark ({child_text:.1f} vs {child_icon:.1f})")
    check(abs(note_left - repo_text) <= 1, f"{where}: the unlisted note text belongs to the repository ({note_left:.1f})")
    # Small geometry, not classes: the mark is inline at icon scale, and rows use the compact density (not a tall
    # min-height). The kind icon, number and external mark sit on the title's first line, including when it wraps.
    check(js(f"{section}.querySelector('[data-repository-mark=\"github\"]').getBoundingClientRect().width") <= 16,
          f"{where}: the provider mark is drawn at icon scale")
    header_h = js(f"{section}.querySelector('[data-repository-row]').getBoundingClientRect().height")
    # The shortest link row is a single line even where a long title wraps its neighbour.
    link_h = js(f"Math.min(...[...{section}.querySelectorAll('[data-repository-link]')].map((r) => r.getBoundingClientRect().height))")
    check(header_h <= 40, f"{where}: the repository row keeps the compact density ({header_h:.1f}px)")
    check(link_h <= 40, f"{where}: a single-line link row keeps the compact density ({link_h:.1f}px)")
    offsets = js("""(() => {
      const row = document.querySelector('[data-todo-repositories] [data-repository-link]');
      const title = row.querySelector('a > span');
      const spans = row.querySelectorAll('a > span');
      const line = title.getBoundingClientRect(), lh = parseFloat(getComputedStyle(title).lineHeight);
      const center = (el) => { const r = el.getBoundingClientRect(); return r.top + r.height / 2; };
      return [center(row.querySelector('[data-link-icon]')) - (line.top + lh / 2),
              center(spans[1]) - (line.top + lh / 2),
              center(row.querySelector('a > svg')) - (line.top + lh / 2)];
    })()""")
    check(all(abs(v) <= 3 for v in offsets), f"{where}: mark, number and external mark align with the title's first line ({offsets})")
    if width <= 600:
        lines = js("""(() => {
          const title = document.querySelector('[data-todo-repositories] [data-repository-link] a > span');
          return Math.round(title.getBoundingClientRect().height / parseFloat(getComputedStyle(title).lineHeight));
        })()""")
        check(lines >= 2, f"{where}: a long title still wraps in full ({lines} lines)")
    shot(f"marks-titles-{width}-{theme}{suffix}")


try:
    for theme in ("light", "dark"):
        for width in (1280, 390):
            run(theme, width)
            run_rich(theme, width)
    run("light", 390, "-large")
    run_rich("light", 390, "-large")
    run_empty()
    run_legacy()
    print("VERDICT: pass")
except AssertionError as failure:
    print(f"VERDICT: FAIL {failure}")
    sys.exit(1)
finally:
    close_owned_tabs()
