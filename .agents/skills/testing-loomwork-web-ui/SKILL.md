---
name: testing-loomwork-web-ui
description: How to build, run, and test the Loomwork React/Vite browser UI and embedded Go HTTP API end-to-end in Chrome, with no auth or external services.
---

# Testing the loomwork browser UI

Companion to `testing-loomwork-cli` (that one covers CLI/provider paths). Use this one for
anything under `internal/httpapi/`, `web/ui/`, or the generated `web/dist/`.

## Bring it up (no auth, no credentials, no external services)
```
cd <repo> && make build           # npm ci + Vite build, then static Go binary
rm -rf /tmp/lw-ui                 # start from a genuinely empty workspace to see empty states
setsid nohup env LOOMWORK_HOME=/tmp/lw-ui ./bin/loomwork serve --addr 127.0.0.1:8787 \
  > /tmp/serve.log 2>&1 < /dev/null & disown
curl -s 127.0.0.1:8787/api/workspace   # {"home":"/tmp/lw-ui","projectsDir":"..."}
curl -s 127.0.0.1:8787/api/projects    # [] on a fresh workspace
```
Gotchas:
- Do NOT kill the server with `pkill -f "loomwork serve"` or `pkill -f "bin/loomwork"` — the
  pattern matches the killing shell's own command line and kills your exec session. Use a
  bracketed pattern instead: `pkill -f "[b]in/loomwork"`, then confirm with
  `ss -ltnp | grep 8787`.
- Start the server with `setsid ... & disown`; plain `(cmd &)` inside an exec call can die with
  the shell and leave the port free but the UI unreachable.
- The frontend is embedded from `web/dist` via `//go:embed all:dist`, so **editing `web/ui/*`
  has no effect in `loomwork serve` until you run `make build` again**. `make ui` is enough to
  refresh `web/dist`, but the running embedded server still needs a rebuilt binary and restart.
- For hot reload, run `npm ci && npm run dev` in `web/ui` while a separate `loomwork serve`
  listens on `127.0.0.1:8787`; Vite proxies `/api`, including project chat SSE, to that server.
- Workspace state persists in `$LOOMWORK_HOME`; to re-test empty states, point at a new dir
  rather than deleting files under a running server.

## Seeding fixtures that exercise every Explorer family
Seed with the CLI (`--home <workspace>`), then confirm the shape with
`curl -s 127.0.0.1:8787/api/projects/<ref>/items`. Gotchas that cost real time:
- `requirement create` assigns 3-digit ids (`req-001`, `req-002`, …), not `req-0001`. A suite
  fixture that links `req-0001` will import as INCOMPLETE — useful on purpose (it exercises the
  incomplete chip/reason), but do not then assert a working requirement link.
- `test-suite import --file` decodes with `DisallowUnknownFields`; the cases array must be
  `"cases"` (`internal/model/testsuite.go`). `"tests"` fails with `unknown field "tests"`.
- The **Reports** family is NOT fed by `artifact add`. It reads the project's on-disk `reports/`
  dir (`store.ReportsDirName`), so copy report files into
  `$LOOMWORK_HOME/projects/<prj-id>/reports/`. `artifact add --type test-result` only populates
  Artifacts. Seed one JSON report (report viewer) plus one `.csv` (unknown media type → raw
  fallback) to cover both viewer paths.
- Never combine `pkill` and `setsid nohup ... & disown` in one exec call: the pkill kills the
  shell before the server detaches and the port ends up empty. Use two separate calls.
- `/api/workspace` may return an empty body; use `/api/projects` to confirm the server is live.

## UI map (BrowserRouter React SPA)
- `/` → project cards (coverage badge, open gaps, last tested) and a "New project" dialog.
- `/projects/{id}` → the desktop: Explorer (left, resizable, with a filter box), tab strip plus
  viewer (center), collapsible chat pane (right, resizable). Widths persist in `localStorage`
  (`loomwork.projectTreeWidth`, `loomwork.chatWidth`, `loomwork.chatOpen`); open tabs persist per
  project in `sessionStorage` (`loomwork.tabs.<id>`); the theme in `loomwork.theme`
  (`system|light|dark`, applied as `<html data-theme>`).
- The Overview tab is a dashboard: stat cards (requirements, coverage, suites, last run), an
  "Open gaps" panel listing uncovered active requirements (click opens the requirement tab),
  sources table with "Link source", agents and rules, recent artifacts, and quick-action buttons
  that open the create dialogs (`.quick-actions .btn`).
- Every create/import form is a `<dialog>` (`.dialog[open]`): requirement (text, source type +
  ref, tags, origin), artifact (paste or choose a file; inline content only), suite import (paste
  or upload JSON; shows the audit before "Open the suite"), agent definition, override rule
  (methods, path glob, scenario, spec status, action, rationale), source (replace-by-name is
  announced on the button). Viewer "New version" buttons open the same dialog prefilled.
- Explorer group labels for Requirements and Test suites open list views (`.tree-group-label`);
  the chevron (`.tree-toggle`) expands the items. Other families expand in place. Reports are
  grouped by folder (`.tree-folder`). Items are `.tree-row`.
- The Requirements list (`.req-row`) has a search box, status segmented control, tag select,
  and per row: `.req-id` (opens a tab), Edit (PUT amend), New version (PATCH), History (newest
  first, `.req-history`), and an Active/Obsolete switch (`.switch input`). Only the edited or
  history row shows its inline panel.
- The Test suites list (`.suite-card`) expands to the typed `SuiteDetail`; each `.case-summary`
  row expands to the case card once (no duplicate case list). Requirement and rule chips are
  `.chip-link` buttons that open the linked entity.
- Item tabs (`ItemViewer`) show a version switcher (`.version-switcher select`) for requirements,
  agent definitions, override rules, and suites; choosing an older version shows a read-only
  notice and hides edit actions.
- Viewers by type: agent definition (parsed frontmatter + markdown, "Raw file" toggle), override
  rule (condition/action boxes + rationale), test case, suite, report (counts, pass bar, per-test
  table, "Raw JSON"), markdown (rendered/raw), HTML (`sandbox=""` iframe, "Source" toggle),
  OpenAPI (Swagger, `docExpansion="list"`, inverted in dark theme), log (level coloring,
  "Problems only"), text (line numbers, wrap, copy).
- Server-side SPA fallback (`internal/httpapi/server.go` `uiHandler`) rewrites unknown paths to
  `index.html`, so direct navigation to `/projects/{id}` must render that project.

## Scripted browser checks
`puppeteer-core` against the system Chrome works well and needs no download:
`mkdir /tmp/pw && cd /tmp/pw && npm init -y && npm i puppeteer-core`, then
`puppeteer.launch({executablePath: "/usr/bin/google-chrome", headless: true, args: ["--no-sandbox"]})`.
Fill dialog fields by their `.field-label` text; assert through the API afterwards
(`/api/projects/{id}/requirements`, `/testability`, `/items/.../history`). Two console lines are
expected and harmless when an HTML artifact is open: the sandboxed iframe blocking the artifact's
`<script>`, and (only if you inject `evaluateOnNewDocument`) a `localStorage` SecurityError from
that same frame. Anything else in the console is a finding.

## Assertions that actually catch regressions
- Source "replace by name": re-submit the same source *name* with a different type/url and assert
  the table still has exactly ONE row with the new values (duplicate row = bug).
- Requirement versioning: after "Save new version", assert the Requirements list row shows `v2` and
  its History reveal shows `v2 ACTIVE` above `v1 SUPERSEDED` with the **old text preserved**. "New
  version" grows history by one, while "Edit" (PUT) leaves history length, version, status, and
  `created_at` untouched. The Active/Obsolete switch must update the current version through the
  status endpoint without inventing history.
- Narrow viewport: resize with `wmctrl -r :ACTIVE: -e 0,0,0,860,740` (or `page.setViewport`) to cross
  the 900px breakpoint. Requirement rows must stack (actions wrap under the text) and the chat pane must
  still be fully visible: the side panes are capped at `28vw`/`34vw` and the work area at 280px, so
  all three fit down to about 730px; below 720px the chat pane hides.
- Version + source type interaction: create a requirement with a source type and `source_ref`, then
  save a new version editing only the text. The omitted source fields must inherit in the store.
- `PATCH /requirements/{id}` intentionally rejects a `status` field; status changes go through the
  separate `.../status` endpoint (the Active/Obsolete checkbox switch).
- Tree/viewer fallback: open a free-form artifact with an unknown media type and assert raw content
  renders instead of a blank viewer.
- HTML safety: open a `text/html` artifact and assert it is inside a sandboxed iframe. A
  `javascript:` source link must render as inert text; only `http:` and `https:` links are active.
- Chat SSE: use a deterministic fake provider in handler tests. For browser testing, only exercise
  chat when a local provider/model is available (Ollama and LM Studio are often running on this box;
  `ollama/llama3.1:latest` answers in a few seconds); no real or external credentials. Assert the
  reply used the attached artifact, that Stop brings the Send button back and leaves "(stopped)" in
  the turn, and that the transcript survives a reload (`sessionStorage`).
- Write flows: every dialog should end with the new entity's tab focused (`.tab.on .tab-name`). Suite
  import must show the audit (`flagged incomplete`, rule `forbids testing` for a skip-test violation)
  before the suite opens, and `/testability` must change accordingly.
- Testability: `lastTestedAt`/`lastRun` follow the newest embedded `run_timestamp` across JSON reports,
  falling back to file mtime; a non-JSON report contributes only its mtime. Coverage counts only
  active requirements linked from any current suite version.

## The OpenAPI document (`GET /api/openapi.json`) and contract drift
`internal/httpapi/openapi.json` is **hand-written** and `go:embed`ed, so it can silently drift from the
handlers. Cheap, high-value way to test it against a live server (no auth, no fixtures):
- Byte identity: `curl -s 127.0.0.1:8787/api/openapi.json -o /tmp/b.json && sha256sum /tmp/b.json internal/httpapi/openapi.json`
  (must match; also assert `Content-Type: application/json; charset=utf-8` and `openapi == "3.1.0"`).
- Method handling: every route goes through `(*Server).route`, which sets a **sorted, comma-space joined**
  `Allow` header and 405s. So `Allow` is machine-readable: for each documented path, send a bogus method
  (e.g. `TRACE`) and assert the `Allow` set equals the document's method list. Unknown paths 404 with
  `{"error":"no endpoint for <path>"}` — use that string to tell "not routed" from "wrong method".
  Note `curl -X HEAD` hangs waiting for a body; use `curl -I`.
- Schema validation: `pip install --user jsonschema` is already in the blueprint. Fetch the served doc,
  replace `#/components/` with `#/$defs/`, wrap it as `{"$defs": doc["components"], **operation_schema}`
  and run `Draft202012Validator` over each real response body. Reusable harness:
  `/tmp/pr18/contract.py` (24 documented operations) and `/tmp/pr18/strict.py` (key diff + route sweep).
- `additionalProperties: false` only exists on request schemas + `Requirement`/`DocumentSource`, so a
  validator alone can NOT catch extra fields on `Project`/`ProjectSummary`/`Workspace`/`Artifact`.
  Add an explicit key diff (actual keys − documented `properties`, and `required` − actual keys).
- Fields with `omitempty` legitimately go missing (e.g. `Artifact` shows 7 of 10 documented keys) — only
  a missing **required** key is a finding.
- Exercise artifact/viewer documents through
  `GET /api/projects/{ref}/items/{family}/{itemRef}` after adding fixtures with the CLI.
- Go's mux matches methods case-sensitively: sending lowercase `post` from a script yields a confusing 405.
- `npx --yes @redocly/cli lint internal/httpapi/openapi.json` is available and takes ~1 min the first time;
  4 `operation-4xx-response` advisories on the meta endpoints are expected/known.
- The route-coverage test only checks documented paths are routed, not that every routed path is
  documented. When adding a route, add it to `openapi.json` and extend the `{placeholder}` replacer in
  `openapi_test.go` if the path introduces a new template variable.

## Console checks
Read the console after each flow and assert there are no React, router, fetch, or Swagger viewer
errors. Also inspect failed network requests: application errors render in the relevant pane and
may not create a console entry.

## Devin Secrets Needed
None. The UI binds to loopback with no auth and needs no external services.
