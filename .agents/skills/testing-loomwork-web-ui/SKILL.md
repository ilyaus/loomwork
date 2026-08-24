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
- `/` → project cards plus the new-project form.
- `/projects/{id}` → the Agent Desktop: resizable entity tree, tabbed viewer area, and agent chat.
- The project overview tab contains document source linking and requirement creation.
- Requirements and Test suites are main-area list views opened from the Explorer. Requirement rows
  stay compact and do not expand: each has Edit (PUT amend-in-place), New version (PATCH
  supersede-and-bump), and Mark obsolete/active actions, with only the edited row showing its
  inline text/tag form. Test suite rows still expand inline to the typed detail.
- The Test suites list exposes every nested test case; reports and artifacts select viewers by
  artifact/media type.
- The splitter width persists in `localStorage` under `loomwork.projectTreeWidth`.
- Server-side SPA fallback (`internal/httpapi/server.go` `uiHandler`) rewrites unknown paths to
  `index.html`, so direct navigation to `/projects/{id}` must render that project.

## Assertions that actually catch regressions
- Source "replace by name": re-submit the same source *name* with a different type/url and assert
  the table still has exactly ONE row with the new values (duplicate row = bug).
- Requirement versioning: after "Save new version", assert the Requirements list row shows `v2`.
  The list renders no history, so assert the version semantics against the read-only
  `GET /api/projects/{ref}/requirements/{id}/history` endpoint from the shell: "New version" grows
  history by one with the previous version `superseded` and its **old text preserved**, while "Edit"
  (PUT) leaves history length, version, status, and `created_at` untouched.
- Version + source type interaction: create a requirement with a source type and `source_ref`, then
  save a new version editing only the text. The omitted source fields must inherit in the store.
- `PATCH /requirements/{id}` intentionally rejects a `status` field; status changes go through the
  separate `.../status` endpoint (the "Mark obsolete / Mark active" button).
- Tree/viewer fallback: open a free-form artifact with an unknown media type and assert raw content
  renders instead of a blank viewer.
- HTML safety: open a `text/html` artifact and assert it is inside a sandboxed iframe. A
  `javascript:` source link must render as inert text; only `http:` and `https:` links are active.
- Chat SSE: use a deterministic fake provider in handler tests. For browser testing, only exercise
  chat when a local provider/model is available; no real or external credentials.

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
  3 `operation-4xx-response` advisories are expected/known.

## Console checks
Read the console after each flow and assert there are no React, router, fetch, or Swagger viewer
errors. Also inspect failed network requests: application errors render in the relevant pane and
may not create a console entry.

## Devin Secrets Needed
None. The UI binds to loopback with no auth and needs no external services.
