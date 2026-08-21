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

## Devin Secrets Needed
None for this flow. Real Azure/Bedrock verification would require `AZURE_AI_API_KEY`
(plus a real `azure.endpoint`/`deployment`) and `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`
(optionally `AWS_SESSION_TOKEN`) with Bedrock model access.
