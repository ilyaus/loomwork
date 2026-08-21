package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newAzureTestAdapter(t *testing.T, endpoint string) *Azure {
	t.Helper()
	t.Setenv(EnvAzureAPIKey, "azure-key")
	adapter, err := NewAzure(Config{
		Kind:  KindAzure,
		Azure: AzureConfig{Endpoint: endpoint, Deployment: "gpt4o", APIVersion: "2024-10-21"},
	})
	if err != nil {
		t.Fatalf("NewAzure: %v", err)
	}
	return adapter
}

func TestAzureGenerateMapsParamsAndResponse(t *testing.T) {
	var captured map[string]any
	var path, query, apiKey, authorization string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path, query = request.URL.Path, request.URL.Query().Get("api-version")
		apiKey, authorization = request.Header.Get("api-key"), request.Header.Get("Authorization")
		captured = decodeBody(t, request)
		_, _ = io.WriteString(writer, `{"model":"gpt-4o","choices":[{"message":{"role":"assistant","content":"answer"},
          "finish_reason":"length"}],"usage":{"prompt_tokens":11,"completion_tokens":4,"total_tokens":15}}`)
	}))
	defer server.Close()

	adapter := newAzureTestAdapter(t, server.URL)
	response, err := adapter.Generate(context.Background(), Request{
		Model:        "gpt-4o",
		SystemPrompt: "be terse",
		Prompt:       "analyze",
		Context:      []ContextBlock{{Label: "log", Content: "log body"}},
		Params: Params{
			Temperature:     Float(0.3),
			TopP:            Float(0.8),
			TopK:            Int(40),
			MaxOutputTokens: Int(512),
			RepeatPenalty:   Float(1.1),
			ContextWindow:   Int(4096),
			Seed:            Int(9),
			Stop:            []string{"END"},
			Extra:           map[string]any{"presence_penalty": 0.25},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if path != "/openai/deployments/gpt4o/chat/completions" || query != "2024-10-21" {
		t.Errorf("request = %s?api-version=%s, want the deployment-scoped path and api-version", path, query)
	}
	if apiKey != "azure-key" || authorization != "" {
		t.Errorf("credentials = api-key %q / Authorization %q, want the key in the api-key header only", apiKey, authorization)
	}
	if response.Text != "answer" || response.Model != "gpt-4o" || response.FinishReason != "length" || response.Usage.TotalTokens != 15 {
		t.Fatalf("response = %+v, want the answer with usage", response)
	}
	if response.Raw["deployment"] != "gpt4o" {
		t.Errorf("raw = %+v, want the deployment recorded", response.Raw)
	}

	if captured["temperature"] != 0.3 || captured["top_p"] != 0.8 || captured["max_tokens"] != float64(512) || captured["seed"] != float64(9) {
		t.Errorf("body = %+v, want mapped OpenAI parameters", captured)
	}
	if captured["presence_penalty"] != 0.25 {
		t.Errorf("body[presence_penalty] = %v, want the Extra passthrough", captured["presence_penalty"])
	}
	// Dropped rather than invented: Foundry's chat API has no portable equivalent.
	for _, dropped := range []string{"top_k", "repeat_penalty", "num_ctx", "frequency_penalty"} {
		if _, present := captured[dropped]; present {
			t.Errorf("body should not contain %q: %+v", dropped, captured)
		}
	}
	if messages := captured["messages"].([]any); len(messages) != 3 {
		t.Fatalf("messages = %v, want system, context, and prompt", messages)
	}
}

func TestAzureGenerateFallsBackToDeploymentAsModel(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured = decodeBody(t, request)
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	response, err := newAzureTestAdapter(t, server.URL).Generate(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if captured["model"] != "gpt4o" || response.Model != "gpt4o" {
		t.Fatalf("model = %v / %q, want the deployment name", captured["model"], response.Model)
	}
}

func TestAzureGenerateRejectsEmptyCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"choices":[]}`)
	}))
	defer server.Close()

	if _, err := newAzureTestAdapter(t, server.URL).Generate(context.Background(), Request{Model: "m", Prompt: "hi"}); err == nil {
		t.Fatal("expected an error when the server returns no choices")
	}
}

func TestAzureGenerateWrapsHTTPErrorsWithoutLeakingTheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(writer, "access denied")
	}))
	defer server.Close()

	_, err := newAzureTestAdapter(t, server.URL).Generate(context.Background(), Request{Model: "m", Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("error = %v, want it to report status 401 and the body", err)
	}
	if strings.Contains(err.Error(), "azure-key") {
		t.Fatalf("error = %v, must not contain the API key", err)
	}
}

func TestAzureModels(t *testing.T) {
	var path, query string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path, query = request.URL.Path, request.URL.Query().Get("api-version")
		_, _ = io.WriteString(writer, `{"data":[{"id":"gpt-4o","owned_by":"azure-openai"}]}`)
	}))
	defer server.Close()

	models, err := newAzureTestAdapter(t, server.URL).Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if path != "/openai/models" || query != "2024-10-21" {
		t.Errorf("request = %s?api-version=%s, want the models path and api-version", path, query)
	}
	if len(models) != 1 || models[0].ID != "gpt-4o" || models[0].Description != "azure-openai" {
		t.Fatalf("models = %+v, want the single gpt-4o entry", models)
	}
}
