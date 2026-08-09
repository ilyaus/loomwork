package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLMStudioGenerateMapsParamsAndResponse(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", request.URL.Path)
		}
		captured = decodeBody(t, request)
		_, _ = io.WriteString(writer, `{"model":"qwen3-8b","choices":[{"message":{"role":"assistant","content":"answer"},
          "finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)
	}))
	defer server.Close()

	adapter := NewLMStudio(Config{Kind: KindLMStudio, BaseURL: server.URL + "/v1"})
	response, err := adapter.Generate(context.Background(), Request{
		Model:        "qwen3-8b",
		SystemPrompt: "be terse",
		Prompt:       "analyze",
		Context:      []ContextBlock{{Label: "log", Content: "log body"}},
		Params: Params{
			Temperature:     Float(0.4),
			TopP:            Float(0.7),
			TopK:            Int(50),
			MaxOutputTokens: Int(900),
			RepeatPenalty:   Float(1.2),
			ContextWindow:   Int(2048),
			Seed:            Int(3),
			Stop:            []string{"END"},
			Extra:           map[string]any{"presence_penalty": 0.5},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if response.Text != "answer" || response.FinishReason != "stop" || response.Usage.TotalTokens != 10 {
		t.Fatalf("response = %+v, want the answer with usage", response)
	}

	if captured["temperature"] != 0.4 || captured["top_p"] != 0.7 || captured["max_tokens"] != float64(900) || captured["seed"] != float64(3) {
		t.Errorf("body = %+v, want mapped OpenAI parameters", captured)
	}
	if captured["presence_penalty"] != 0.5 {
		t.Errorf("body[presence_penalty] = %v, want the Extra passthrough 0.5", captured["presence_penalty"])
	}
	// TopK, RepeatPenalty, and ContextWindow have no portable OpenAI equivalent.
	for _, dropped := range []string{"top_k", "repeat_penalty", "num_ctx", "frequency_penalty"} {
		if _, present := captured[dropped]; present {
			t.Errorf("body should not contain %q: %+v", dropped, captured)
		}
	}
	messages := captured["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %v, want system, context, and prompt", messages)
	}
}

func TestLMStudioSendsBearerTokenWhenConfigured(t *testing.T) {
	t.Setenv(EnvLMStudioAPIKey, "local-key")
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	adapter := NewLMStudio(Config{Kind: KindLMStudio, BaseURL: server.URL})
	if _, err := adapter.Generate(context.Background(), Request{Model: "m", Prompt: "hi"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if authorization != "Bearer local-key" {
		t.Fatalf("Authorization = %q, want the bearer token from the environment", authorization)
	}
}

func TestLMStudioGenerateRejectsEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"choices":[]}`)
	}))
	defer server.Close()

	adapter := NewLMStudio(Config{Kind: KindLMStudio, BaseURL: server.URL})
	if _, err := adapter.Generate(context.Background(), Request{Model: "m", Prompt: "hi"}); err == nil {
		t.Fatal("expected an error when the server returns no choices")
	}
}

func TestLMStudioGenerateWrapsHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(writer, "boom")
	}))
	defer server.Close()

	adapter := NewLMStudio(Config{Kind: KindLMStudio, BaseURL: server.URL})
	_, err := adapter.Generate(context.Background(), Request{Model: "m", Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want it to report status 500 and the body", err)
	}
}

func TestLMStudioModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", request.URL.Path)
		}
		_, _ = io.WriteString(writer, `{"data":[{"id":"qwen3-8b","owned_by":"lmstudio"}]}`)
	}))
	defer server.Close()

	models, err := NewLMStudio(Config{Kind: KindLMStudio, BaseURL: server.URL + "/v1"}).Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "qwen3-8b" || models[0].Description != "lmstudio" {
		t.Fatalf("models = %+v, want the single qwen3-8b entry", models)
	}
}

func TestNewLMStudioUsesDefaultEndpoint(t *testing.T) {
	if adapter := NewLMStudio(Config{Kind: KindLMStudio}); adapter.baseURL != DefaultLMStudioBaseURL {
		t.Fatalf("baseURL = %q, want %q", adapter.baseURL, DefaultLMStudioBaseURL)
	}
}
