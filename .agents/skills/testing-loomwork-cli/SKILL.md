---
name: testing-loomwork-cli
description: How to build, run, and end-to-end test the loomwork CLI (projects, document sources, versioned requirements, artifacts) without any server, network, or LLM provider.
---

# Testing the loomwork CLI

## Build and run
- `make build` produces `bin/loomwork` (CGO_ENABLED=0). Also available: `make fmt`, `make vet`, `make test`.
- There is no HTTP server or web UI: the earlier `loomwork serve` / `internal/server` surface was removed.
  Everything is exercised through the binary. `loomwork serve` should exit 1 with `unknown command "serve"`.
- Point the workspace at a scratch dir per test run: `export LOOMWORK_HOME=/tmp/lw` (or pass `--home PATH`).
  No credentials are needed for project/requirement/artifact flows; only `run`/`providers` touch providers,
  and `run` must not be exercised against a real provider.
- `--json` is a global flag on every command and is the best way to assert exact field values.
- Every failure path prints `loomwork: <message>` on stderr and exits 1; success exits 0. `--help` exits 0.
- Careful when piping CLI output through `head` in test harnesses: SIGPIPE makes the observed exit code 141
  and hides the real code. Redirect to files instead.

## Where state lives (assert on disk, not just stdout)
```
$LOOMWORK_HOME/projects/<project-id>/
  project.json          # name, description, tags, sources[], index{requirements,activeRequirements}
  requirements/
    req-001.v1.json     # one immutable file per version
    index.json          # per-id {id,current_version,versions[],status,updated_at}
  agent-definitions/ test-suites/ executor-config/ reports/   # created empty at project create
```
- A legacy flat `projects/<id>.json` is still readable and is migrated to a directory (and the flat file
  deleted) on the first write, e.g. `project source --project <name> --source ...`.
- Writes are atomic (temp file + rename) and guarded by `projects/.lock`, so concurrency tests should
  launch several CLI processes in parallel and then assert `index.json` is valid JSON with unique ids and
  contiguous `versions[]`.

## Useful assertions
- Requirement JSON files should validate against `docs/schemas/requirement.schema.json`
  (`pip install jsonschema`; the index document is `$defs.index` in the same file).
- An update writes `req-NNN.v<N+1>.json` first and then rewrites the previous version once to set
  `status: superseded`; after that, older version files must never change again (compare sha256/mtime).
  When the write ordering in `DirStore.UpdateRequirement` changes, re-run the hash/mtime retention checks
  rather than reusing earlier evidence.
- `superseded` is not a directly settable status: `requirement create --status superseded` and
  `requirement set-status --status superseded` must exit 1 with
  `requirement status "superseded" is set only by creating a new version: choose active or obsolete`.
  A distinct message covers a version that is *already* superseded:
  `requirement <id> v<N> is superseded: its status is fixed because a newer version exists`.
  Both messages should be reachable and must not be confused with one another.
  Note `requirement list --status superseded` still parses (it filters current versions, so it is
  normally empty), and `unknown requirement status` errors still list superseded as a valid value.
- Not-found errors come from `store.ErrNotFound` (`store: not found`) and the caller names the entity,
  e.g. `project "ghost": store: not found`, `requirement "req-999" in project Alpha: store: not found`,
  `requirement req-001 v99: store: not found`. If a message names the wrong noun, that is a bug.
- `--version 0`/omitted means the current version; negative `--version` is rejected up front by
  `requirement show`/`set-status` with `--version must be 1 or greater`.
- Artifact types are `spec, log, test-result, diagram, doc, generated` — `note` is NOT valid, so use
  `--type spec` in artifact regression flows.

## Devin Secrets Needed
None. All project/requirement/artifact/source flows are local filesystem only.
