package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func decodeBody(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode request body %q: %v", raw, err)
	}
	return decoded
}

func TestOllamaGenerateMapsParamsAndResponse(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", request.URL.Path)
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q, want application/json", request.Header.Get("Content-Type"))
		}
		captured = decodeBody(t, request)
		_, _ = io.WriteString(writer, `{"model":"qwen3:8b","message":{"role":"assistant","content":"summary text"},
          "done":true,"done_reason":"stop","prompt_eval_count":11,"eval_count":5}`)
	}))
	defer server.Close()

	adapter := NewOllama(Config{Kind: KindOllama, BaseURL: server.URL})
	response, err := adapter.Generate(context.Background(), Request{
		Model:        "qwen3:8b",
		SystemPrompt: "be terse",
		Prompt:       "summarize",
		Context:      []ContextBlock{{Label: "api.md [spec v1]", Content: "spec body"}},
		Params: Params{
			Temperature:     Float(0.15),
			TopP:            Float(0.8),
			TopK:            Int(30),
			MaxOutputTokens: Int(512),
			RepeatPenalty:   Float(1.1),
			ContextWindow:   Int(4096),
			Seed:            Int(42),
			Stop:            []string{"###"},
			Extra:           map[string]any{"mirostat": float64(2)},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if response.Text != "summary text" || response.FinishReason != "stop" {
		t.Fatalf("response = %+v, want the completion text and finish reason", response)
	}
	if response.Usage.PromptTokens != 11 || response.Usage.CompletionTokens != 5 || response.Usage.TotalTokens != 16 {
		t.Fatalf("usage = %+v, want 11/5/16", response.Usage)
	}
	if adapter.Name() != "ollama" {
		t.Fatalf("Name = %q, want ollama", adapter.Name())
	}

	if captured["stream"] != false {
		t.Fatalf("stream = %v, want false", captured["stream"])
	}
	options, ok := captured["options"].(map[string]any)
	if !ok {
		t.Fatalf("options missing from request body %+v", captured)
	}
	expected := map[string]any{
		"temperature":    0.15,
		"top_p":          0.8,
		"top_k":          float64(30),
		"num_predict":    float64(512),
		"repeat_penalty": 1.1,
		"num_ctx":        float64(4096),
		"seed":           float64(42),
		"mirostat":       float64(2),
	}
	for key, want := range expected {
		if options[key] != want {
			t.Errorf("options[%q] = %v, want %v", key, options[key], want)
		}
	}
	if stops, ok := options["stop"].([]any); !ok || len(stops) != 1 || stops[0] != "###" {
		t.Errorf("options[stop] = %v, want [###]", options["stop"])
	}

	messages, ok := captured["messages"].([]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("messages = %v, want system, context, and prompt messages", captured["messages"])
	}
	first := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "be terse" {
		t.Errorf("first message = %v, want the system prompt", first)
	}
	second := messages[1].(map[string]any)
	if !strings.Contains(second["content"].(string), "### api.md [spec v1]") {
		t.Errorf("context message = %v, want a labeled context block", second)
	}
	third := messages[2].(map[string]any)
	if third["content"] != "summarize" {
		t.Errorf("last message = %v, want the user prompt", third)
	}
}

func TestOllamaGenerateOmitsOptionsWhenNoParams(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured = decodeBody(t, request)
		_, _ = io.WriteString(writer, `{"message":{"content":"ok"},"done":true}`)
	}))
	defer server.Close()

	adapter := NewOllama(Config{Kind: KindOllama, BaseURL: server.URL, DefaultModel: "llama3"})
	if _, err := adapter.Generate(context.Background(), Request{Prompt: "hello"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, present := captured["options"]; present {
		t.Fatalf("options should be omitted when no parameters are set: %+v", captured)
	}
	if captured["model"] != "llama3" {
		t.Fatalf("model = %v, want the configured default llama3", captured["model"])
	}
	if messages := captured["messages"].([]any); len(messages) != 1 {
		t.Fatalf("messages = %v, want only the user prompt", messages)
	}
}

func TestOllamaGenerateValidatesRequest(t *testing.T) {
	adapter := NewOllama(Config{Kind: KindOllama, BaseURL: "http://127.0.0.1:1"})
	if _, err := adapter.Generate(context.Background(), Request{Prompt: "hi"}); err == nil {
		t.Fatal("expected an error when no model is set")
	}
	if _, err := adapter.Generate(context.Background(), Request{Model: "m"}); err == nil {
		t.Fatal("expected an error when no prompt is set")
	}
}

func TestOllamaGenerateWrapsHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(writer, `{"error":"model not found"}`)
	}))
	defer server.Close()

	adapter := NewOllama(Config{Kind: KindOllama, BaseURL: server.URL})
	_, err := adapter.Generate(context.Background(), Request{Model: "absent", Prompt: "hi"})
	if err == nil {
		t.Fatal("expected an error for a non-2xx response")
	}
	for _, fragment := range []string{"ollama generate", "absent", "404", "model not found"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("error %q should contain %q", err, fragment)
		}
	}
}

func TestOllamaGenerateRejectsEmptyCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"message":{"content":"   "},"done":true}`)
	}))
	defer server.Close()

	adapter := NewOllama(Config{Kind: KindOllama, BaseURL: server.URL})
	if _, err := adapter.Generate(context.Background(), Request{Model: "m", Prompt: "hi"}); err == nil {
		t.Fatal("expected an error for an empty completion")
	}
}

func TestOllamaGenerateHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(writer, `{"message":{"content":"late"},"done":true}`)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	adapter := NewOllama(Config{Kind: KindOllama, BaseURL: server.URL})
	if _, err := adapter.Generate(ctx, Request{Model: "m", Prompt: "hi"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want a deadline exceeded error", err)
	}
}

func TestOllamaModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/tags" {
			t.Errorf("path = %q, want /api/tags", request.URL.Path)
		}
		_, _ = io.WriteString(writer, `{"models":[{"name":"qwen3:8b"},{"model":"llama3:latest"}]}`)
	}))
	defer server.Close()

	models, err := NewOllama(Config{Kind: KindOllama, BaseURL: server.URL}).Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 || models[0].ID != "qwen3:8b" || models[1].ID != "llama3:latest" {
		t.Fatalf("models = %+v, want qwen3:8b and llama3:latest", models)
	}
}

func TestNewOllamaUsesDefaultEndpoint(t *testing.T) {
	adapter := NewOllama(Config{Kind: KindOllama})
	if adapter.baseURL != DefaultOllamaBaseURL {
		t.Fatalf("baseURL = %q, want %q", adapter.baseURL, DefaultOllamaBaseURL)
	}
}

func TestRenderContextLabelsBlocks(t *testing.T) {
	rendered := RenderContext([]ContextBlock{{Content: "one"}, {Label: "two", Content: "body"}})
	if !strings.Contains(rendered, "### context-1") || !strings.Contains(rendered, "### two") {
		t.Fatalf("rendered = %q, want labeled blocks", rendered)
	}
	if RenderContext(nil) != "" {
		t.Fatal("expected an empty string for no context blocks")
	}
}
