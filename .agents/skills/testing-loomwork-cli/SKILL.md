---
name: testing-loomwork-cli
description: How to test the loomwork CLI end-to-end without cloud credentials or a local model server — isolated workspaces, provider credential status, and fake provider endpoints.
---

# Testing the loomwork CLI

## Build / static checks
```
make build   # CGO_ENABLED=0 go build -o bin/loomwork ./cmd/loomwork
make vet
make test
gofmt -l .   # must print nothing
ldd bin/loomwork   # expect "not a dynamic executable" (static, CGO off)
```
`file` is not installed on the box; use `ldd` for the static-binary check.

## Isolated workspace
Never touch `~/.loomwork`. Every command accepts `--home PATH`:
```
H=/tmp/lw-test; mkdir -p $H
cp config/config.example.json $H/config.json
bin/loomwork providers --home $H [--json]
```
`--json` is a global flag; `requirement list --json` returns a bare JSON array (not an object).

## Provider credential status (`providers`)
`internal/cli/run.go` builds azure/bedrock adapters just to compute status, so status is
`configured` or `unavailable: <reason>`. Credentials are read from the environment only:
- azure: `AZURE_AI_API_KEY` (or `azure.apiKeyEnv`)
- bedrock: both `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`, or `bedrock.profile`
Use obviously-fake sentinel values (e.g. `SENTINEL-AZURE-KEY-1234`) so you can grep all output
for the secret value to check the "never log credentials" requirement.

## Exercising remote adapters without cloud access
No live cloud creds are needed to prove the adapters work over the wire — point them at a small
local Python `http.server` fake that logs the request and returns a canned response:
- Azure: set `providers.azure.azure.endpoint` to `http://127.0.0.1:PORT`; expect
  `POST /openai/deployments/<deployment>/chat/completions?api-version=<ver>` with an `api-key`
  header and an OpenAI-shaped chat completion response.
- Bedrock: set `providers.bedrock.baseUrl` to `http://127.0.0.1:PORT` (used as the SDK
  `BaseEndpoint`); expect `POST /model/<modelId>/converse` with an `Authorization: AWS4-HMAC-SHA256`
  header. Dummy creds are enough for the SDK to sign. Response shape:
  `{"output":{"message":{"role":"assistant","content":[{"text":"..."}]}},"stopReason":"end_turn","usage":{...}}`

Then drive it through the real CLI path:
```
bin/loomwork project create --home $H --name qa-demo
bin/loomwork artifact add --home $H --project qa-demo --name spec.md --type spec --content "..."
bin/loomwork run --home $H --project qa-demo --artifact spec.md --model azure/gpt-4o-test --prompt "..."
```
Prompt runs otherwise need a local Ollama (:11434) or LM Studio (:1234/v1), neither of which is installed.

## Recording CLI evidence
There is no web UI for the CLI paths. `xterm`/`gnome-terminal` are absent but `konsole` is installed:
`nohup konsole &`, then `wmctrl -a Konsole && wmctrl -r :ACTIVE: -b add,maximized_vert,maximized_horz`.
Konsole font-size changes via `ctrl+shift+plus` and `konsoleprofile Font=...` did not take effect;
output is still legible because the recording captures the real 1600x1200 display. Put each test step
in a small `/tmp/*.sh` script and run them one at a time so the terminal shows short, readable commands.

## Agent definitions, override rules, test suites (Phase 3+)
`LOOMWORK_HOME=<dir>` also isolates state (equivalent to `--home`). Store layout per project:
```
agent-definitions/<name>.v<n>.md                # v1 file is byte-frozen when v2 is added
agent-definitions/current.json                  # {"agents":[{agent_name,current_version,versions}],"override_rules":[]}
agent-definitions/override-rules/<rule>.v<n>.json
test-suites/<suite-id>/v<n>/{suite.json,tests/tc-00N.json} + current.json
```
Contracts worth asserting (they are easy to get wrong):
- an imported suite with a case that has `requirement_ids: []`, or zero cases, is **stored and flagged**,
  never rejected: exit 0, `INCOMPLETE` in output, `"incomplete": true` in `current.json`, reason strings
  `1 test case(s) have no requirement link: tc-002` / `the suite has no test cases`.
- override rules follow the *supersede* pattern (v1 is rewritten once to `status: superseded`), unlike
  agent-definition versions which are immutable — do not assert byte-identity of rule v1 across an update.
- suite ids are validated on both write and **read** paths (`model.NormalizeSuiteID`, pattern
  `^[a-z0-9][a-z0-9-]*$`). `test-suite show/--history/import` with `../../other`, `suite/../..`,
  `Suite Orders` must exit 1 with `test suite id "<id>" must be lowercase letters, digits, and dashes`.
  Good traversal test: plant a valid suite layout at `<project>/decoy-suite` (outside `test-suites/`) and
  confirm `--suite ../decoy-suite` errors instead of rendering it, plus a `find $LOOMWORK_HOME -type d`
  diff before/after to prove nothing was created outside `test-suites/`.

## Faking the Claude agent bridge (credential-free `test-suite generate`)
`LOOMWORK_CLAUDE_BRIDGE=<script>` replaces `bridge/claude-agent-bridge.mjs`; it is run as `node <script>`
(Node 22 is installed). Minimal stdio protocol (see `internal/cli/testsuite_test.go` and
`docs/agent-bridge-protocol.md`): read JSON lines; on `start_session` reply `{"type":"ready","session_id":...}`;
on `prompt` reply `{"type":"turn_complete","id":<request.id>,"text":<suite JSON>,"stop_reason":"end_turn"}`.
- The bridge **must echo `request.id`**. The host drops a `turn_complete`/`error` whose id is not the awaited
  turn, so a hardcoded `turn-1` (or any bogus id) makes the CLI wait forever — the CLI uses
  `context.Background()`, so wrap such a run in `timeout N ...` and treat exit 124 as "turn correctly still
  waiting". If the fake bridge exits after the bad reply you instead get exit 1
  `claude agent turn failed: bridge closed its output`.
- Missing bridge script ⇒ exit 1 `claude agent bridge exited before the session started`, nothing stored.
- Never assert exit codes through a pipe (`| head`) — SIGPIPE turns into exit 141; redirect to a file or
  capture with `out=$(cmd 2>&1); code=$?`.

## Devin Secrets Needed
None for this flow. Real Azure/Bedrock verification would require `AZURE_AI_API_KEY`
(plus a real `azure.endpoint`/`deployment`) and `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`
(optionally `AWS_SESSION_TOKEN`) with Bedrock model access.
