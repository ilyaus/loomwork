# Working on Loomwork

Go 1.24 CLI plus an embedded React/Vite UI. Principles live in
`.specify/memory/constitution.md`; the product spec is `docs/loom-work-vision.md`.

## Build and verify
```
make build        # npm ci + vite build into web/dist, then CGO_ENABLED=0 go build -o bin/loomwork
make test         # go test ./...   (internal/exec takes ~11s; everything else is fast)
make vet          # go vet ./...
gofmt -l .        # must print nothing
ldd bin/loomwork  # "not a dynamic executable" (ldd exits 1 for static binaries; that is fine)
cd web/ui && npx tsc -b && npm run build   # type check + bundle without touching Go
```
`web/dist` is embedded with `//go:embed all:dist`, so UI edits need `make build` (or `make ui` plus
`go build`) and a server restart before `loomwork serve` shows them. For hot reload run
`npm run dev` in `web/ui` against a `loomwork serve` on `127.0.0.1:8787`; Vite proxies `/api`.

## Running against a throwaway workspace
Never point at `~/.loomwork` while testing. Every command takes `--home PATH` or `LOOMWORK_HOME`.
```
setsid nohup env LOOMWORK_HOME=/tmp/lw-ui ./bin/loomwork serve --addr 127.0.0.1:8787 \
  > /tmp/serve.log 2>&1 < /dev/null & disown
pkill -f "[b]in/loomwork serve"     # bracket the pattern or pkill kills your own shell
```
Seed fixtures with the CLI (`project create`, `requirement create`, `artifact add`,
`agent-definition create|rule-create`, `test-suite import --file`). Reports are plain files under
`$LOOMWORK_HOME/projects/<id>/reports/`; copy them in. The `testing-loomwork-cli` and
`testing-loomwork-web-ui` skills in `.agents/skills/` hold the detailed gotchas.

## Layout notes
- `internal/httpapi` is a handler layer only: routing in `server.go` (explicit `switch` on path
  segments), entity handlers per file, the hand-written `openapi.json` beside them. Adding a route
  means adding it to the switch, documenting it in `openapi.json`, and covering it in
  `entities_test.go`; `openapi_test.go` fails if a documented path is not routed.
- `internal/httpapi/testability.go` derives the coverage rollup on read; nothing is stored.
- `web/ui/src`: `pages/` (landing, project desktop), `components/` (Explorer, Overview, list views,
  ChatDock, `viewers/` registry keyed by artifact type then media type, `dialogs/` one form per
  entity), `lib/` (desktop tab model and context, theme, formatting). Styles are one file with CSS
  variables for both themes; keep class names unique across sections (a `.count` collision once
  broke the tree pills).
- The Markdown renderer builds React elements directly; never add `dangerouslySetInnerHTML`.

## Import and browser regression checks
- After `make build`, run `npm --prefix web/ui run test:ui`. It starts its own server on an ephemeral
  port and creates a temporary workspace and spec-kit fixture. Requires Node 22.12+ and Chrome at
  `/usr/bin/google-chrome`, or set `CHROME_BIN`.
- `project import --path /absolute/source --format spec-kit --preview --json` inspects without
  importing. Omit `--preview` to create a snapshot; `--features id1,id2` selects feature folders.
  Always pass a temporary `--home` when testing.
- `internal/projectimport` holds format adapters behind `Adapter`; `DirStore.CreateWithRequirements`
  stages and publishes snapshots. `Project.Import.RequirementsReadOnly` blocks all requirement writes
  in the shared store, while local artifacts, test suites, agents, rules, and reports remain writable.
- New spec-kit imports use IDs such as `008-FR-001`, preserving the spec prefix and original FR/NFR/SC
  number regardless of import order or selected features. Old imports retain their storage IDs and
  history; `display_id` exposes the canonical reference, which is also accepted by requirement reads.
  `source_id`, `source_path`, `source_line`, and `feature` metadata retain provenance.
- Reports can be uploaded through `POST /api/projects/{ref}/reports` or the UI. Names are relative
  paths, uploads are append-only, and JSON files must parse. Artifact history is available through
  the common item-history endpoint; an artifact ID still identifies its original revision by default.
- `GET /api/projects/{ref}/requirement-tests` derives links through `internal/traceability` from current
  suite cases and latest scenario artifacts. Markdown links require explicit fields or spec-kit FR
  filenames/frontmatter, scoped by feature; they never change native coverage. Local Markdown tests
  can use the `test-scenario` tag and a `Requirements: req-001` field.
- `FileTree` is shared by Explorer and file-family lists. Folder state is kept per project, family,
  and view in sessionStorage; search uses flat results without changing collapsed folders.
- Requirement pages render the selected test beneath the requirement text; row test links open that
  combined view. Back to requirements restores the list filters saved in sessionStorage.
- `Project.testDocuments.roots` configures document-suite folder prefixes through
  `GET|PUT /api/projects/{ref}/test-document-settings`. Spec-kit defaults to each feature's `sdd-qa`.
  The tree groups those documents under Test Suites with family `document-suites`, while retaining
  artifact IDs/history and the artifact read API. No files move and native suite coverage is unchanged.
