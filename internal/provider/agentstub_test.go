package provider

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func recordingTool(seen *[]string) ToolDefinition {
	return ToolDefinition{
		Name:        "read_swagger",
		Description: "returns the specification",
		Handler: func(ctx context.Context, input json.RawMessage) (ToolResult, error) {
			*seen = append(*seen, string(input))
			return ToolResult{Content: "openapi: 3.1.0"}, nil
		},
	}
}

func TestStubSessionReplaysTurnsAndRunsTools(t *testing.T) {
	var seen []string
	adapter := NewStubAgentAdapter(
		provTurn("first", `{"suite":1}`, "read_swagger"),
		provTurn("second", "", ""),
	)
	var _ AgentAdapter = adapter
	if adapter.Name() != string(AgentKindStub) {
		t.Fatalf("Name = %q", adapter.Name())
	}

	session, err := adapter.StartSession(context.Background(), AgentSessionSpec{Tools: []ToolDefinition{recordingTool(&seen)}})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	first, err := session.Send(context.Background(), PromptRequest{Prompt: "generate"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if first.Text != "first" || len(seen) != 1 {
		t.Errorf("result = %+v, tool calls = %v", first, seen)
	}
	second, err := session.Send(context.Background(), PromptRequest{Prompt: "again"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if second.Text != "second" {
		t.Errorf("Text = %q", second.Text)
	}
	if _, err := session.Send(context.Background(), PromptRequest{Prompt: "once more"}); err == nil {
		t.Error("expected an error once the script runs out")
	}
	if prompts := session.(*StubSession).Prompts(); len(prompts) != 3 {
		t.Errorf("recorded %d prompt(s), want 3", len(prompts))
	}
}

func provTurn(text, structured, tool string) StubTurn {
	turn := StubTurn{Text: text}
	if structured != "" {
		turn.Structured = json.RawMessage(structured)
	}
	if tool != "" {
		turn.ToolCalls = []StubToolCall{{Name: tool, Input: json.RawMessage(`{"path":"/openapi.json"}`)}}
	}
	return turn
}

func TestStubSessionExtractsStructuredOutputFromText(t *testing.T) {
	adapter := NewStubAgentAdapter(StubTurn{Text: "Here it is:\n```json\n{\"cases\":[]}\n```"})
	session, err := adapter.StartSession(context.Background(), AgentSessionSpec{})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.Send(context.Background(), PromptRequest{
		Prompt:     "generate",
		Structured: &StructuredOutput{Name: "test_suite", Schema: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if string(result.Structured) != `{"cases":[]}` {
		t.Errorf("Structured = %s", result.Structured)
	}

	adapter = NewStubAgentAdapter(StubTurn{Text: "no json here"})
	session, err = adapter.StartSession(context.Background(), AgentSessionSpec{})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Send(context.Background(), PromptRequest{
		Prompt:     "generate",
		Structured: &StructuredOutput{Name: "test_suite"},
	}); err == nil {
		t.Error("expected an error when structured output was requested and none arrived")
	}
}

func TestStubSessionRejectsUnusableToolsAndClosedSessions(t *testing.T) {
	adapter := NewStubAgentAdapter(StubTurn{Text: "ok"})
	if _, err := adapter.StartSession(context.Background(), AgentSessionSpec{
		Tools: []ToolDefinition{{Name: "read_swagger"}},
	}); err == nil {
		t.Error("expected an error for a tool with no handler")
	}

	session, err := adapter.StartSession(context.Background(), AgentSessionSpec{})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	var seen []string
	if err := session.RegisterTool(recordingTool(&seen)); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}
	if _, err := session.Send(context.Background(), PromptRequest{}); err == nil {
		t.Error("expected an error for an empty prompt")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := session.Send(context.Background(), PromptRequest{Prompt: "generate"}); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("err = %v, want ErrSessionClosed", err)
	}
	if err := session.RegisterTool(recordingTool(&seen)); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("err = %v, want ErrSessionClosed", err)
	}
	if err := session.Close(); err != nil {
		t.Errorf("Close (repeated): %v", err)
	}
}

func TestStubAdapterRespondOverridesTheScript(t *testing.T) {
	adapter := &StubAgentAdapter{
		AdapterName: "scripted",
		Respond: func(ctx context.Context, spec AgentSessionSpec, req PromptRequest) (PromptResult, error) {
			return PromptResult{Text: spec.Model + ":" + req.Prompt}, nil
		},
	}
	session, err := adapter.StartSession(context.Background(), AgentSessionSpec{Model: "test-model"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer func() { _ = session.Close() }()
	result, err := session.Send(context.Background(), PromptRequest{Prompt: "generate"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.Text != "test-model:generate" {
		t.Errorf("Text = %q", result.Text)
	}
	if adapter.Name() != "scripted" || len(adapter.Sessions()) != 1 {
		t.Errorf("Name = %q, sessions = %d", adapter.Name(), len(adapter.Sessions()))
	}
}

func TestStubSessionEmitsNormalizedEvents(t *testing.T) {
	var seen []string
	adapter := NewStubAgentAdapter(provTurn("done", "", "read_swagger"))
	session, err := adapter.StartSession(context.Background(), AgentSessionSpec{Tools: []ToolDefinition{recordingTool(&seen)}})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, err := session.Send(context.Background(), PromptRequest{Prompt: "generate"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var kinds []AgentEventKind
	for event := range session.Events() {
		kinds = append(kinds, event.Kind)
	}
	want := []AgentEventKind{
		AgentEventSessionStarted,
		AgentEventToolCall,
		AgentEventToolResult,
		AgentEventText,
		AgentEventTurnComplete,
	}
	if len(kinds) != len(want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	for i, kind := range want {
		if kinds[i] != kind {
			t.Errorf("event %d = %q, want %q", i, kinds[i], kind)
		}
	}
}
