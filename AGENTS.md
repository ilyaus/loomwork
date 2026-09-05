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
