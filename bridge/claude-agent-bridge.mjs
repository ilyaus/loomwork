#!/usr/bin/env node
// Claude Agent SDK bridge.
//
// The Claude Agent SDK is a TypeScript package, so Loomwork reaches it through
// this child process instead of linking it. The protocol is one JSON object per
// line in each direction and is documented in docs/agent-bridge-protocol.md;
// internal/provider/claudeagent.go is the only client.
//
// Install once before use:
//   cd bridge && npm install
//
// Host tools are exposed to the model through an in-process MCP server. Their
// input schema is passed through as text in the tool description rather than
// translated into Zod: the schema arrives from Go as JSON Schema at runtime, and
// re-deriving a Zod shape from it would add a second, silently diverging notion
// of what a tool accepts. Validation stays with the host tool, which is where
// the authority over its own input belongs.

import { query, tool, createSdkMcpServer } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
import readline from "node:readline";

const MCP_SERVER_NAME = "loomwork";

const state = {
  session: null,
  tools: new Map(),
  pendingToolResults: new Map(),
  inbox: [],
  wake: null,
  turn: null,
};

function send(event) {
  process.stdout.write(JSON.stringify(event) + "\n");
}

function fail(message) {
  send({ type: "error", message });
}

// hostTool exposes one Go-side tool. The handler forwards the call over stdout
// and waits for the matching tool_result line.
function hostTool(definition) {
  const schemaText = JSON.stringify(definition.input_schema ?? { type: "object" });
  return tool(
    definition.name,
    `${definition.description}\n\nInput JSON Schema:\n${schemaText}`,
    { input: z.record(z.string(), z.unknown()).describe("Arguments matching the input JSON Schema") },
    async (args) => {
      const id = `tool-${state.pendingToolResults.size + 1}-${Date.now()}`;
      const input = args?.input ?? {};
      send({ type: "tool_call", id, name: definition.name, input });
      const result = await new Promise((resolve) => {
        state.pendingToolResults.set(id, resolve);
      });
      return {
        content: [{ type: "text", text: result.content ?? "" }],
        isError: Boolean(result.is_error),
      };
    },
  );
}

// BUILTIN_TOOLS_DENIED are the SDK's own tools. Generation needs none of them:
// every input reaches the model through a host tool Loomwork registered, so the
// filesystem, the shell, and the network stay out of reach of a spec or a
// requirement that tries to talk to the agent.
const BUILTIN_TOOLS_DENIED = [
  "Bash",
  "BashOutput",
  "KillShell",
  "Read",
  "Write",
  "Edit",
  "NotebookEdit",
  "Glob",
  "Grep",
  "WebFetch",
  "WebSearch",
  "Task",
  "TodoWrite",
];

function buildOptions(spec) {
  const definitions = [...state.tools.values()].map(hostTool);
  const options = {
    model: spec.model,
    // Host tools are pre-approved through allowedTools below, so no permission
    // prompt can block a non-interactive run; everything else is denied.
    permissionMode: "default",
    disallowedTools: BUILTIN_TOOLS_DENIED,
    allowedTools: [],
  };
  if (spec.system_prompt) {
    options.systemPrompt = spec.system_prompt;
  }
  if (spec.max_turns) {
    options.maxTurns = spec.max_turns;
  }
  if (definitions.length > 0) {
    options.mcpServers = {
      [MCP_SERVER_NAME]: createSdkMcpServer({
        name: MCP_SERVER_NAME,
        version: "1.0.0",
        tools: definitions,
      }),
    };
    options.allowedTools = definitions.map((definition) => `mcp__${MCP_SERVER_NAME}__${definition.name}`);
  }
  return options;
}

// prompts is the streaming input the SDK consumes: one persistent session whose
// turns arrive as Loomwork sends them.
async function* prompts() {
  for (;;) {
    if (state.inbox.length === 0) {
      await new Promise((resolve) => {
        state.wake = resolve;
      });
    }
    const next = state.inbox.shift();
    if (next === null) {
      return;
    }
    yield {
      type: "user",
      message: { role: "user", content: next.prompt },
      parent_tool_use_id: null,
      session_id: state.sessionId ?? "loomwork",
    };
  }
}

function enqueue(item) {
  state.inbox.push(item);
  const wake = state.wake;
  state.wake = null;
  if (wake) {
    wake();
  }
}

// runSession consumes the SDK's message stream and maps it onto bridge events.
// Only what Loomwork's normalized event set covers is forwarded; anything else
// is intentionally dropped rather than leaked in a backend-specific shape.
async function runSession(spec) {
  const stream = query({ prompt: prompts(), options: buildOptions(spec) });
  send({ type: "ready" });

  let text = [];
  for await (const message of stream) {
    if (message.type === "system" && message.subtype === "init") {
      state.sessionId = message.session_id;
      send({ type: "session_started", session_id: message.session_id });
      continue;
    }
    if (message.type === "assistant") {
      for (const block of message.message?.content ?? []) {
        if (block.type === "text" && block.text) {
          text.push(block.text);
          send({ type: "text", text: block.text });
        } else if (block.type === "thinking" && block.thinking) {
          send({ type: "thinking", text: block.thinking });
        }
      }
      continue;
    }
    if (message.type === "result") {
      const usage = message.usage
        ? {
            input_tokens: message.usage.input_tokens ?? 0,
            output_tokens: message.usage.output_tokens ?? 0,
          }
        : undefined;
      const answer = typeof message.result === "string" && message.result ? message.result : text.join("");
      text = [];
      const turn = state.turn;
      state.turn = null;
      if (message.subtype !== "success" && message.is_error) {
        send({ type: "error", id: turn, message: message.subtype ?? "agent turn failed" });
        continue;
      }
      send({
        type: "turn_complete",
        id: turn,
        text: answer,
        stop_reason: message.subtype ?? "",
        usage,
      });
    }
  }
}

function handleLine(line) {
  const trimmed = line.trim();
  if (trimmed === "") {
    return;
  }
  let request;
  try {
    request = JSON.parse(trimmed);
  } catch (err) {
    fail(`bridge received a line that is not JSON: ${err.message}`);
    return;
  }

  switch (request.type) {
    case "start_session": {
      if (state.session) {
        fail("a session is already running");
        return;
      }
      const spec = request.session ?? {};
      for (const definition of spec.tools ?? []) {
        state.tools.set(definition.name, definition);
      }
      state.session = runSession(spec).catch((err) => {
        fail(`session failed: ${err?.message ?? err}`);
        process.exit(1);
      });
      return;
    }
    case "register_tool": {
      // Tools reach the model through the MCP server built at session start,
      // so a tool registered afterwards is recorded but cannot be offered to
      // the model until the next session. Say so instead of pretending.
      if (state.session) {
        fail(`tool ${request.tool?.name} was registered after the session started and will not be offered to the model`);
        return;
      }
      state.tools.set(request.tool.name, request.tool);
      return;
    }
    case "prompt": {
      if (!state.session) {
        fail("prompt received before start_session");
        return;
      }
      state.turn = request.id ?? null;
      enqueue({ prompt: request.prompt ?? "" });
      return;
    }
    case "tool_result": {
      const resolve = state.pendingToolResults.get(request.id);
      if (!resolve) {
        fail(`no pending tool call ${request.id}`);
        return;
      }
      state.pendingToolResults.delete(request.id);
      resolve(request);
      return;
    }
    case "close": {
      enqueue(null);
      setTimeout(() => process.exit(0), 100);
      return;
    }
    default:
      fail(`unknown request type ${request.type}`);
  }
}

const reader = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
reader.on("line", handleLine);
reader.on("close", () => {
  enqueue(null);
  setTimeout(() => process.exit(0), 100);
});
