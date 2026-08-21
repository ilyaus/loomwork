package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newBedrockTestAdapter points the adapter at a local endpoint with static
// credentials, so the SDK signs and sends real requests without any live cloud.
func newBedrockTestAdapter(t *testing.T, endpoint string) *Bedrock {
	t.Helper()
	t.Setenv(EnvAWSAccessKeyID, "aws-id")
	t.Setenv(EnvAWSSecretAccessKey, "aws-secret")
	t.Setenv(EnvAWSSessionToken, "aws-token")
	adapter, err := NewBedrock(Config{
		Kind:    KindBedrock,
		BaseURL: endpoint,
		Bedrock: BedrockConfig{Region: "us-west-2", ModelID: "anthropic.claude-3"},
	})
	if err != nil {
		t.Fatalf("NewBedrock: %v", err)
	}
	return adapter
}

func TestBedrockGenerateMapsParamsSignsAndDecodes(t *testing.T) {
	var captured map[string]any
	var path, authorization, sessionToken string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		authorization = request.Header.Get("Authorization")
		sessionToken = request.Header.Get("X-Amz-Security-Token")
		captured = decodeBody(t, request)
		_, _ = io.WriteString(writer, `{"output":{"message":{"role":"assistant","content":[{"text":"an"},{"text":"swer"}]}},
          "stopReason":"end_turn","usage":{"inputTokens":11,"outputTokens":4,"totalTokens":15},"metrics":{"latencyMs":42}}`)
	}))
	defer server.Close()

	response, err := newBedrockTestAdapter(t, server.URL).Generate(context.Background(), Request{
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
			Extra:           map[string]any{"top_k": 40},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if path != "/model/anthropic.claude-3/converse" {
		t.Errorf("path = %q, want the Converse path for the configured model", path)
	}
	if !strings.HasPrefix(authorization, "AWS4-HMAC-SHA256 ") || !strings.Contains(authorization, "/us-west-2/bedrock/aws4_request") {
		t.Errorf("Authorization = %q, want a SigV4 signature for the bedrock service", authorization)
	}
	if strings.Contains(authorization, "aws-secret") {
		t.Errorf("Authorization = %q, must not contain the secret access key", authorization)
	}
	if sessionToken != "aws-token" {
		t.Errorf("X-Amz-Security-Token = %q, want the session token from the environment", sessionToken)
	}

	if response.Text != "answer" || response.Model != "anthropic.claude-3" || response.FinishReason != "end_turn" {
		t.Fatalf("response = %+v, want the concatenated answer with the stop reason", response)
	}
	if response.Usage != (Usage{PromptTokens: 11, CompletionTokens: 4, TotalTokens: 15}) {
		t.Errorf("usage = %+v, want the token counts from the Converse response", response.Usage)
	}

	inference, ok := captured["inferenceConfig"].(map[string]any)
	if !ok {
		t.Fatalf("body = %+v, want an inferenceConfig", captured)
	}
	if inference["temperature"] != 0.3 || inference["topP"] != 0.8 || inference["maxTokens"] != float64(512) {
		t.Errorf("inferenceConfig = %+v, want the mapped inference parameters", inference)
	}
	if stops, _ := inference["stopSequences"].([]any); len(stops) != 1 || stops[0] != "END" {
		t.Errorf("inferenceConfig[stopSequences] = %v, want [END]", inference["stopSequences"])
	}
	// Dropped rather than invented: Converse has no portable field for these.
	for _, dropped := range []string{"topK", "repeatPenalty", "seed", "numCtx"} {
		if _, present := inference[dropped]; present {
			t.Errorf("inferenceConfig should not contain %q: %+v", dropped, inference)
		}
	}
	if extra, _ := captured["additionalModelRequestFields"].(map[string]any); extra["top_k"] != float64(40) {
		t.Errorf("additionalModelRequestFields = %v, want the Extra passthrough", captured["additionalModelRequestFields"])
	}

	system, _ := captured["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("system = %v, want the system prompt block", captured["system"])
	}
	messages, _ := captured["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %v, want the rendered context and the prompt", messages)
	}
}

func TestBedrockGenerateOmitsInferenceConfigWhenNoParamsSet(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured = decodeBody(t, request)
		_, _ = io.WriteString(writer, `{"output":{"message":{"role":"assistant","content":[{"text":"ok"}]}},
          "stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2},"metrics":{"latencyMs":1}}`)
	}))
	defer server.Close()

	if _, err := newBedrockTestAdapter(t, server.URL).Generate(context.Background(), Request{Prompt: "hi"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, absent := range []string{"inferenceConfig", "system", "additionalModelRequestFields"} {
		if _, present := captured[absent]; present {
			t.Errorf("body should not contain %q: %+v", absent, captured)
		}
	}
}

func TestBedrockGenerateRejectsEmptyCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"output":{"message":{"role":"assistant","content":[]}},
          "stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":0,"totalTokens":1},"metrics":{"latencyMs":1}}`)
	}))
	defer server.Close()

	if _, err := newBedrockTestAdapter(t, server.URL).Generate(context.Background(), Request{Prompt: "hi"}); err == nil {
		t.Fatal("expected an error when the model returns no text")
	}
}

func TestBedrockGenerateWrapsAPIErrorsWithoutLeakingCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"__type":"ValidationException","message":"model not enabled"}`)
	}))
	defer server.Close()

	_, err := newBedrockTestAdapter(t, server.URL).Generate(context.Background(), Request{Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "model not enabled") {
		t.Fatalf("error = %v, want the API error surfaced", err)
	}
	if strings.Contains(err.Error(), "aws-secret") || strings.Contains(err.Error(), "aws-token") {
		t.Fatalf("error = %v, must not contain credentials", err)
	}
}

func TestBedrockGenerateRequiresAModel(t *testing.T) {
	t.Setenv(EnvAWSAccessKeyID, "aws-id")
	t.Setenv(EnvAWSSecretAccessKey, "aws-secret")
	adapter, err := NewBedrock(Config{Kind: KindBedrock, Bedrock: BedrockConfig{Region: "us-west-2"}})
	if err != nil {
		t.Fatalf("NewBedrock: %v", err)
	}
	if _, err := adapter.Generate(context.Background(), Request{Prompt: "hi"}); err == nil {
		t.Fatal("expected an error without a model id")
	}
}

func TestBedrockModels(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		_, _ = io.WriteString(writer, `{"modelSummaries":[{"modelId":"anthropic.claude-3","modelName":"Claude 3","providerName":"Anthropic"}]}`)
	}))
	defer server.Close()

	models, err := newBedrockTestAdapter(t, server.URL).Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if path != "/foundation-models" {
		t.Errorf("path = %q, want the ListFoundationModels path", path)
	}
	if len(models) != 1 || models[0].ID != "anthropic.claude-3" || models[0].Description != "Anthropic Claude 3" {
		t.Fatalf("models = %+v, want the single Claude entry", models)
	}
}
