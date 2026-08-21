---
name: testing-loomwork-cli
description: How to black-box test the loomwork Go CLI, including flag placement, isolated workspaces, and stubbing the Ollama provider without a live model.
---

# Testing the loomwork CLI

## Build & run
- `make build` produces `bin/loomwork` (pure Go, no CGO). `make test` / `make vet` for unit checks.
- Global flags (`--home`, `--json`) must come AFTER the group+subcommand, e.g. `loomwork analysis import --home /tmp/lw --project X --file f.json`. Putting `--home` before the group fails with `unknown command "--home"`.
- Always use an isolated workspace: `--home /tmp/some-dir` (or `LOOMWORK_HOME`). Default is `~/.loomwork`.

## Stubbing LLM providers (no Ollama/LM Studio installed here)
- Provider endpoints are configured in `<home>/config.json`, e.g.
  `{"providers":{"ollama":{"kind":"ollama","baseUrl":"http://127.0.0.1:PORT"}}}`.
- A minimal HTTP stub answering `POST /api/chat` with
  `{"model":"m","message":{"role":"assistant","content":"<analysis JSON, prose/code fences are tolerated>"},"done":true,"done_reason":"stop","prompt_eval_count":N,"eval_count":N}`
  is enough to exercise the full `analysis run` path (context assembly, parsing, artifact storage, requirement extraction). Log the request body to verify document source content was sent as context.
- LM Studio uses the OpenAI chat-completions shape at `<baseUrl>/chat/completions`.

## Analysis feature specifics (phase 2)
- `analysis run` requires the project to have document sources with local copies (`project source --source "name=...,type=github,url=...,local=/path"`); sources >1 MiB are rejected.
- Import payloads must match docs/schemas/document-analysis.schema.json; unknown JSON fields are rejected (`DisallowUnknownFields`), verdict must be ready|ready-with-gaps|not-ready, and `gaps`/`open_questions` must be present (use `[]`).
- Repeated analyses with the same `--name` (default `document-analysis`) become artifact revisions v1, v2, ...; extracted requirements land in `requirement list --json` with `origin: "extracted"` and provenance in `metadata` (manual import: origin=manual-import + importedFrom; run: provider/model/promptSha256; both: analysisArtifact).

## Devin Secrets Needed
None — everything runs locally.
