---
name: testing-loomwork-web-ui
description: How to run and test the loomwork browser UI (loomwork serve + internal/httpapi + embedded web/assets SPA) end-to-end in Chrome, with no auth or external services.
---

# Testing the loomwork browser UI

Companion to `testing-loomwork-cli` (that one covers CLI/provider paths). Use this one for
anything under `internal/httpapi/` or `web/assets/`.

## Bring it up (no auth, no credentials, no external services)
```
cd <repo> && make build           # CGO_ENABLED=0 go build -o bin/loomwork ./cmd/loomwork
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
- The frontend is embedded via `//go:embed assets` (`web/embed.go`), so **editing `web/assets/*`
  has no effect until you `make build` again**. Verify which asset the running binary serves with
  e.g. `curl -s 127.0.0.1:8787/views/project.js | grep -n "source type"` (assets are served at
  the root, not under `/assets/`).
- Workspace state persists in `$LOOMWORK_HOME`; to re-test empty states, point at a new dir
  rather than deleting files under a running server.

## UI map (hash-routed SPA, `web/assets/app.js`)
- `#/` → projects landing (`views/projects.js`): cards + "New project" form. Testability stats
  (`last tested`/`coverage`/`open gaps`) are intentionally `—` placeholders until phases 4–5.
- `#/projects/{id}` → project view (`views/project.js`): document sources table + "+ Link a
  document source", requirements table + detail/version history + "+ Add a requirement".
- Forms live inside collapsed `<details>` elements — you must click the `+ ...` summary first.
- Selects are native `<select>`; click to open, then click the option (two separate clicks).
- Feedback is a bottom-center toast (`ok` = green ~2.5s, error = red ~8s), so screenshot
  immediately after a submit or you will miss it.
- Server-side SPA fallback (`internal/httpapi/server.go` `uiHandler`) rewrites unknown paths to
  `index.html`, so `http://127.0.0.1:8787/projects/{id}` (no `#`) serves the app — but with an
  empty hash it renders the **landing** view, not the project. That is expected, not a bug.

## Assertions that actually catch regressions
- Source "replace by name": re-submit the same source *name* with a different type/url and assert
  the table still has exactly ONE row with the new values (duplicate row = bug).
- Requirement versioning: after "Save new version", assert list shows `v2` AND history shows
  `v2 ACTIVE` above `v1 SUPERSEDED` with the **old text preserved**.
- Version + source type interaction (regressed once, see below): create a requirement WITH a
  source type and `source_ref`, then save a new version editing ONLY the text. The store rejects a
  `source_ref` with no `source_type`, and `dom.js formValues()` drops empty strings, so if the
  new-version form's source-type select does not prefill `current.source_type` the PATCH 400s with
  `requirement source reference "..." needs a source type`. Always exercise the text-only edit path.
- `PATCH /requirements/{id}` intentionally rejects a `status` field; status changes go through the
  separate `.../status` endpoint (the "Mark obsolete / Mark active" button).
- Status filter: mark the only requirement obsolete, then filter `active` → "No requirements
  match." and the detail pane resets to its placeholder.

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
- To exercise the `Artifact` schema over HTTP there is no artifact endpoint: add one with the CLI against
  the *same* `LOOMWORK_HOME` (`loomwork artifact add --project p --name n --type spec --file f`) and read it
  back inside `GET /api/projects/{ref}`.
- Go's mux matches methods case-sensitively: sending lowercase `post` from a script yields a confusing 405.
- `npx --yes @redocly/cli lint internal/httpapi/openapi.json` is available and takes ~1 min the first time;
  3 `operation-4xx-response` advisories are expected/known.

## Known cosmetic trap: literal "null" in the DOM
`el()` and `render()` in `web/assets/dom.js` filter out `null` children, but native
`Element.append` stringifies them, so any view that passes a conditional child straight to
`node.append(...)` prints a visible `null` (this shipped once on the projects landing view before
`render()` existed). Render through `el`/`render` only, and eyeball pages for stray
`null`/`undefined` text — the console stays clean, so only a screenshot catches it.

## Console checks
Read the console via the console tool after each flow. Fetch 404s are handled in `api.js` and
surface as toasts, so they do NOT log console errors — an empty console is genuinely clean. To
avoid a false negative, prove capture works once by running
`console.error('capture-check')` and re-reading the log.

## Devin Secrets Needed
None. The UI binds to loopback with no auth and needs no external services.
