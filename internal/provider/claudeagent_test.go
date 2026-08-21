package provider

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBridge writes a Node script that speaks the bridge protocol from
// docs/agent-bridge-protocol.md without the SDK, so the adapter's framing,
// tool round trip, and shutdown are testable offline.
func fakeBridge(t *testing.T, handler string) []string {
	t.Helper()
	script := `
import { createInterface } from "node:readline";
const send = (event) => process.stdout.write(JSON.stringify(event) + "\n");
const seen = [];
const rl = createInterface({ input: process.stdin });
rl.on("line", (line) => {
  if (!line.trim()) return;
  const request = JSON.parse(line);
  seen.push(request);
  ` + handler + `
});
`
	path := filepath.Join(t.TempDir(), "fake-bridge.mjs")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("write fake bridge: %v", err)
	}
	return []string{"node", path}
}

func startFakeSession(t *testing.T, handler string, spec AgentSessionSpec) AgentSession {
	t.Helper()
	if _, err := os.Stat("/usr/bin/env"); err != nil {
		t.Skip("no POSIX environment")
	}
	adapter := NewClaudeAgentAdapter(ClaudeAgentConfig{
		BridgeArgv: fakeBridge(t, handler),
		EnvAllow:   []string{"PATH", "HOME"},
		Timeout:    30 * time.Second,
	})
	if adapter.Name() != string(AgentKindClaude) {
		t.Fatalf("Name = %q", adapter.Name())
	}
	session, err := adapter.StartSession(context.Background(), spec)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

const readyThenEcho = `
  if (request.type === "start_session") {
    send({ type: "ready", session_id: "session-1" });
    return;
  }
  if (request.type === "prompt") {
    send({ type: "text", text: "thinking about it" });
    send({
      type: "turn_complete",
      id: request.id,
      text: "prompt was: " + request.prompt,
      structured: request.structured_output ? { ok: true } : undefined,
      stop_reason: "end_turn",
      usage: { input_tokens: 7, output_tokens: 11 },
    });
    return;
  }
  if (request.type === "close") {
    send({ type: "text", text: "closing" });
    rl.close();
    process.exit(0);
  }
`

func TestClaudeSessionCompletesATurnOverTheBridgeProtocol(t *testing.T) {
	session := startFakeSession(t, readyThenEcho, AgentSessionSpec{Model: "claude-sonnet-4-5", SystemPrompt: "be terse"})

	result, err := session.Send(context.Background(), PromptRequest{
		Prompt:     "generate a suite",
		Structured: &StructuredOutput{Name: "test_suite", Schema: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(result.Text, "generate a suite") {
		t.Errorf("Text = %q", result.Text)
	}
	if string(result.Structured) != `{"ok":true}` {
		t.Errorf("Structured = %s", result.Structured)
	}
	if result.StopReason != "end_turn" || result.Usage.TotalTokens != 18 {
		t.Errorf("StopReason = %q, Usage = %+v", result.StopReason, result.Usage)
	}

	var kinds []AgentEventKind
	for {
		event, open := <-session.Events()
		if !open {
			break
		}
		kinds = append(kinds, event.Kind)
		if event.Kind == AgentEventTurnComplete {
			break
		}
	}
	want := []AgentEventKind{AgentEventSessionStarted, AgentEventText, AgentEventTurnComplete}
	if len(kinds) != len(want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	for i, kind := range want {
		if kinds[i] != kind {
			t.Errorf("event %d = %q, want %q", i, kinds[i], kind)
		}
	}
}

func TestClaudeSessionRunsARegisteredToolAndReturnsItsResult(t *testing.T) {
	handler := `
  if (request.type === "start_session") {
    send({ type: "ready", session_id: "session-1" });
    return;
  }
  if (request.type === "prompt") {
    send({ type: "tool_call", id: "call-1", name: "read_swagger", input: { path: "/openapi.json" } });
    return;
  }
  if (request.type === "tool_result") {
    send({ type: "turn_complete", id: "turn-1", text: "tool said: " + request.content + " error=" + !!request.is_error });
    return;
  }
  if (request.type === "close") { rl.close(); process.exit(0); }
`
	var toolInput json.RawMessage
	session := startFakeSession(t, handler, AgentSessionSpec{
		Tools: []ToolDefinition{{
			Name:        "read_swagger",
			Description: "returns the specification",
			Handler: func(ctx context.Context, input json.RawMessage) (ToolResult, error) {
				toolInput = input
				return ToolResult{Content: "openapi: 3.1.0"}, nil
			},
		}},
	})

	result, err := session.Send(context.Background(), PromptRequest{Prompt: "read the spec"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.Text != "tool said: openapi: 3.1.0 error=false" {
		t.Errorf("Text = %q", result.Text)
	}
	if !strings.Contains(string(toolInput), "/openapi.json") {
		t.Errorf("tool input = %s", toolInput)
	}
}

func TestClaudeSessionReportsAFailingToolToTheModelInsteadOfDying(t *testing.T) {
	handler := `
  if (request.type === "start_session") { send({ type: "ready", session_id: "s" }); return; }
  if (request.type === "prompt") { send({ type: "tool_call", id: "call-1", name: request.prompt, input: {} }); return; }
  if (request.type === "tool_result") {
    send({ type: "turn_complete", id: "turn-1", text: request.content, stop_reason: request.is_error ? "tool_error" : "end_turn" });
    return;
  }
  if (request.type === "close") { rl.close(); process.exit(0); }
`
	session := startFakeSession(t, handler, AgentSessionSpec{
		Tools: []ToolDefinition{{
			Name:        "read_swagger",
			Description: "returns the specification",
			Handler: func(ctx context.Context, input json.RawMessage) (ToolResult, error) {
				return ToolResult{}, errors.New("the spec file is unreadable")
			},
		}},
	})

	failed, err := session.Send(context.Background(), PromptRequest{Prompt: "read_swagger"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if failed.StopReason != "tool_error" || !strings.Contains(failed.Text, "unreadable") {
		t.Errorf("result = %+v", failed)
	}

	unknown, err := session.Send(context.Background(), PromptRequest{Prompt: "read_nothing"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if unknown.StopReason != "tool_error" || !strings.Contains(unknown.Text, "not registered") {
		t.Errorf("result = %+v", unknown)
	}
}

func TestClaudeSessionSurfacesABridgeError(t *testing.T) {
	handler := `
  if (request.type === "start_session") { send({ type: "ready", session_id: "s" }); return; }
  if (request.type === "prompt") { send({ type: "error", id: request.id, message: "credential rejected" }); return; }
  if (request.type === "close") { rl.close(); process.exit(0); }
`
	session := startFakeSession(t, handler, AgentSessionSpec{})
	_, err := session.Send(context.Background(), PromptRequest{Prompt: "generate"})
	if err == nil || !strings.Contains(err.Error(), "credential rejected") {
		t.Fatalf("err = %v, want the bridge's message", err)
	}
}

func TestClaudeSessionSurfacesABridgeThatDiesMidTurn(t *testing.T) {
	handler := `
  if (request.type === "start_session") { send({ type: "ready", session_id: "s" }); return; }
  if (request.type === "prompt") { process.stderr.write("bridge crashed\n"); process.exit(9); }
`
	session := startFakeSession(t, handler, AgentSessionSpec{})
	_, err := session.Send(context.Background(), PromptRequest{Prompt: "generate"})
	if err == nil {
		t.Fatal("expected an error when the bridge exits mid-turn")
	}
	if !strings.Contains(err.Error(), "bridge") {
		t.Errorf("err = %v", err)
	}
}

func TestClaudeSessionRejectsLateToolRegistrationAndUseAfterClose(t *testing.T) {
	session := startFakeSession(t, readyThenEcho, AgentSessionSpec{})
	tool := ToolDefinition{
		Name:        "read_swagger",
		Description: "returns the specification",
		Handler:     func(ctx context.Context, input json.RawMessage) (ToolResult, error) { return ToolResult{}, nil },
	}
	if err := session.RegisterTool(tool); !errors.Is(err, ErrLateToolRegistration) {
		t.Errorf("err = %v, want ErrLateToolRegistration", err)
	}
	if err := session.RegisterTool(ToolDefinition{Name: "no handler"}); err == nil {
		t.Error("expected a validation error for an unusable tool")
	}

	closed := make(chan error, 1)
	go func() { closed <- session.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Close deadlocked")
	}
	drained := false
	for range session.Events() {
		drained = true
	}
	if !drained {
		t.Error("the session-started event should have been buffered for a late reader")
	}
	if _, err := session.Send(context.Background(), PromptRequest{Prompt: "again"}); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("err = %v, want ErrSessionClosed", err)
	}
	if err := session.Close(); err != nil {
		t.Errorf("Close (repeated): %v", err)
	}
}

func TestStartSessionFailsWhenTheBridgeCannotStart(t *testing.T) {
	adapter := NewClaudeAgentAdapter(ClaudeAgentConfig{BridgeArgv: []string{"loomwork-no-such-bridge"}})
	if _, err := adapter.StartSession(context.Background(), AgentSessionSpec{}); err == nil {
		t.Fatal("expected an error for a missing bridge binary")
	}

	handler := `if (request.type === "start_session") { process.stderr.write("no credential\n"); process.exit(2); }`
	adapter = NewClaudeAgentAdapter(ClaudeAgentConfig{BridgeArgv: fakeBridge(t, handler), EnvAllow: []string{"PATH", "HOME"}})
	_, err := adapter.StartSession(context.Background(), AgentSessionSpec{})
	if err == nil || !strings.Contains(err.Error(), "before the session started") {
		t.Fatalf("err = %v, want a handshake failure", err)
	}
}

func TestBridgeArgvPrefersTheConfiguredScript(t *testing.T) {
	t.Setenv(BridgeEnvVar, "/tmp/custom-bridge.mjs")
	if got := (ClaudeAgentConfig{}).bridgeArgv(); got[0] != "node" || got[1] != "/tmp/custom-bridge.mjs" {
		t.Errorf("bridgeArgv = %v", got)
	}
	t.Setenv(BridgeEnvVar, "")
	if got := (ClaudeAgentConfig{}).bridgeArgv(); got[1] != DefaultBridgeScript {
		t.Errorf("bridgeArgv = %v, want the repository bridge", got)
	}
}

func TestNormalizeToolSchemaAlwaysDescribesAnObject(t *testing.T) {
	if got := normalizeToolSchema(nil); got["type"] != "object" {
		t.Errorf("schema = %v", got)
	}
	got := normalizeToolSchema(map[string]any{"properties": map[string]any{"path": map[string]any{"type": "string"}}})
	if got["type"] != "object" || got["properties"] == nil {
		t.Errorf("schema = %v", got)
	}
}
