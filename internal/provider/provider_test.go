package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseKind(t *testing.T) {
	for _, raw := range []string{"ollama", " LMSTUDIO ", "Azure", "bedrock", "imgen"} {
		if _, err := ParseKind(raw); err != nil {
			t.Errorf("ParseKind(%q) returned %v, want a known kind", raw, err)
		}
	}
	if _, err := ParseKind("openai"); err == nil {
		t.Fatal("expected an error for an unsupported provider kind")
	}
}

func TestParamsMergeAppliesOnlySetValues(t *testing.T) {
	base := Params{
		Temperature: Float(0.2),
		TopP:        Float(0.9),
		Stop:        []string{"A"},
		Extra:       map[string]any{"keep": 1, "replace": 1},
	}
	merged := base.Merge(Params{
		Temperature:     Float(0.7),
		TopK:            Int(40),
		MaxOutputTokens: Int(256),
		RepeatPenalty:   Float(1.05),
		ContextWindow:   Int(8192),
		Seed:            Int(9),
		Stop:            []string{"B"},
		Extra:           map[string]any{"replace": 2},
	})

	if *merged.Temperature != 0.7 || *merged.TopP != 0.9 {
		t.Errorf("merged temperature/top_p = %v/%v, want 0.7/0.9", *merged.Temperature, *merged.TopP)
	}
	if *merged.TopK != 40 || *merged.MaxOutputTokens != 256 || *merged.RepeatPenalty != 1.05 || *merged.ContextWindow != 8192 || *merged.Seed != 9 {
		t.Errorf("merged = %+v, want every overridden value applied", merged)
	}
	if len(merged.Stop) != 1 || merged.Stop[0] != "B" {
		t.Errorf("merged stop = %v, want [B]", merged.Stop)
	}
	if merged.Extra["keep"] != 1 || merged.Extra["replace"] != 2 {
		t.Errorf("merged extra = %v, want keep=1 and replace=2", merged.Extra)
	}
	if base.Extra["replace"] != 1 || *base.Temperature != 0.2 || len(base.Stop) != 1 || base.Stop[0] != "A" {
		t.Errorf("Merge mutated the receiver: %+v", base)
	}
}

func TestParamsCloneIsDeep(t *testing.T) {
	original := Params{Temperature: Float(0.1), Stop: []string{"X"}, Extra: map[string]any{"a": 1}}
	clone := original.Clone()
	*clone.Temperature = 0.9
	clone.Stop[0] = "Y"
	clone.Extra["a"] = 2

	if *original.Temperature != 0.1 || original.Extra["a"] != 1 {
		t.Errorf("clone shares state with the original: %+v", original)
	}
	if original.Stop[0] != "X" {
		t.Errorf("clone shares the stop slice with the original: %v", original.Stop)
	}
}

func TestParamsJSONOmitsUnsetFields(t *testing.T) {
	encoded, err := json.Marshal(Params{Temperature: Float(0.3)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != `{"temperature":0.3}` {
		t.Fatalf("encoded = %s, want only the temperature field", encoded)
	}
}

func TestConfigValidate(t *testing.T) {
	t.Setenv(EnvAWSRegion, "")
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{name: "local provider needs nothing", cfg: Config{Kind: KindOllama}},
		{name: "unknown kind", cfg: Config{Kind: "openai"}, wantErr: "unknown provider kind"},
		{name: "azure without endpoint", cfg: Config{Kind: KindAzure, Azure: AzureConfig{Deployment: "d"}}, wantErr: "azure.endpoint"},
		{name: "azure without deployment", cfg: Config{Kind: KindAzure, Azure: AzureConfig{Endpoint: "https://e"}}, wantErr: "azure.deployment"},
		{name: "azure fully wired", cfg: Config{Kind: KindAzure, Azure: AzureConfig{Endpoint: "https://e", Deployment: "d"}}},
		{name: "bedrock without region", cfg: Config{Kind: KindBedrock}, wantErr: "bedrock.region"},
		{name: "bedrock with region", cfg: Config{Kind: KindBedrock, Bedrock: BedrockConfig{Region: "us-east-1"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.cfg.Validate()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want no error", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, test.wantErr)
			}
		})
	}
}

func TestConfigTimeout(t *testing.T) {
	if got := (Config{TimeoutSeconds: 5}).Timeout(); got.Seconds() != 5 {
		t.Errorf("Timeout() = %v, want 5s", got)
	}
	if got := (Config{}).Timeout(); got != DefaultGenerateTimeout {
		t.Errorf("Timeout() = %v, want the default", got)
	}
}

func TestBuildTextGenerator(t *testing.T) {
	t.Setenv(EnvAzureAPIKey, "azure-key")
	t.Setenv(EnvAWSAccessKeyID, "aws-id")
	t.Setenv(EnvAWSSecretAccessKey, "aws-secret")

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "ollama", cfg: Config{Kind: KindOllama}, want: "ollama"},
		{name: "lmstudio", cfg: Config{Kind: KindLMStudio}, want: "lmstudio"},
		{name: "azure", cfg: Config{Kind: KindAzure, Azure: AzureConfig{Endpoint: "https://e", Deployment: "d"}}, want: "azure"},
		{name: "bedrock", cfg: Config{Kind: KindBedrock, Bedrock: BedrockConfig{Region: "us-east-1"}}, want: "bedrock"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			generator, err := BuildTextGenerator(test.cfg)
			if err != nil {
				t.Fatalf("BuildTextGenerator: %v", err)
			}
			if generator.Name() != test.want {
				t.Fatalf("Name() = %q, want %q", generator.Name(), test.want)
			}
		})
	}

	if _, err := BuildTextGenerator(Config{Kind: KindImGen}); err == nil {
		t.Fatal("expected an error building a text generator from an image provider")
	}
}

func TestBuildImageGenerator(t *testing.T) {
	generator, err := BuildImageGenerator(Config{Kind: KindImGen})
	if err != nil {
		t.Fatalf("BuildImageGenerator: %v", err)
	}
	if generator.Name() != "imgen" {
		t.Fatalf("Name() = %q, want imgen", generator.Name())
	}
	if _, err := BuildImageGenerator(Config{Kind: KindOllama}); err == nil {
		t.Fatal("expected an error building an image generator from a text provider")
	}
}

func TestAzureWiring(t *testing.T) {
	t.Setenv("CUSTOM_AZURE_KEY", "secret-value")
	adapter, err := NewAzure(Config{
		Kind:  KindAzure,
		Azure: AzureConfig{Endpoint: "https://example.openai.azure.com/", Deployment: "gpt4o", APIKeyEnv: "CUSTOM_AZURE_KEY"},
	})
	if err != nil {
		t.Fatalf("NewAzure: %v", err)
	}
	wantURL := "https://example.openai.azure.com/openai/deployments/gpt4o/chat/completions?api-version=" + DefaultAzureAPIVersion
	if got := adapter.ChatCompletionsURL(); got != wantURL {
		t.Errorf("ChatCompletionsURL() = %q, want %q", got, wantURL)
	}
	wantModelsURL := "https://example.openai.azure.com/openai/models?api-version=" + DefaultAzureAPIVersion
	if got := adapter.ModelsURL(); got != wantModelsURL {
		t.Errorf("ModelsURL() = %q, want %q", got, wantModelsURL)
	}
}

func TestNewAzureRequiresCredentialsAndCoordinates(t *testing.T) {
	t.Setenv(EnvAzureAPIKey, "")
	if _, err := NewAzure(Config{Kind: KindAzure, Azure: AzureConfig{Deployment: "d"}}); err == nil {
		t.Error("expected an error without an endpoint")
	}
	if _, err := NewAzure(Config{Kind: KindAzure, Azure: AzureConfig{Endpoint: "https://e"}}); err == nil {
		t.Error("expected an error without a deployment")
	}
	_, err := NewAzure(Config{Kind: KindAzure, Azure: AzureConfig{Endpoint: "https://e", Deployment: "d"}})
	if err == nil || !strings.Contains(err.Error(), EnvAzureAPIKey) {
		t.Errorf("error = %v, want it to name the missing credential variable", err)
	}
}

func TestBedrockWiring(t *testing.T) {
	t.Setenv(EnvAWSAccessKeyID, "aws-id")
	t.Setenv(EnvAWSSecretAccessKey, "aws-secret")
	adapter, err := NewBedrock(Config{Kind: KindBedrock, Bedrock: BedrockConfig{Region: "us-west-2", ModelID: "anthropic.claude"}})
	if err != nil {
		t.Fatalf("NewBedrock: %v", err)
	}
	wantURL := "https://bedrock-runtime.us-west-2.amazonaws.com/model/anthropic.claude/converse"
	if got := adapter.ConverseURL(""); got != wantURL {
		t.Errorf("ConverseURL() = %q, want %q", got, wantURL)
	}
}

func TestNewBedrockAcceptsProfileWithoutStaticKeys(t *testing.T) {
	t.Setenv(EnvAWSAccessKeyID, "")
	t.Setenv(EnvAWSSecretAccessKey, "")
	if _, err := NewBedrock(Config{Kind: KindBedrock, Bedrock: BedrockConfig{Region: "eu-west-1"}}); err == nil {
		t.Error("expected an error without credentials or a profile")
	}
	if _, err := NewBedrock(Config{Kind: KindBedrock, Bedrock: BedrockConfig{Region: "eu-west-1", Profile: "dev"}}); err != nil {
		t.Errorf("NewBedrock with a profile returned %v, want no error", err)
	}
}

func TestImGenGenerateImagesPollsUntilSuccess(t *testing.T) {
	var submitted map[string]any
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/jobs":
			submitted = decodeBody(t, request)
			_, _ = io.WriteString(writer, `{"job_id":"job-1","status":"queued"}`)
		case request.URL.Path == "/jobs/job-1":
			polls++
			if polls < 2 {
				_, _ = io.WriteString(writer, `{"job_id":"job-1","status":"running"}`)
				return
			}
			_, _ = io.WriteString(writer, `{"job_id":"job-1","status":"succeeded","result":{"model":"sdxl",
              "artifacts":[{"filename":"a.png","path":"/out/a.png","size_bytes":12,"download_url":"/files/a.png"}]}}`)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	adapter := NewImGen(Config{Kind: KindImGen, BaseURL: server.URL, ImGen: ImGenConfig{PollIntervalSeconds: 0}})
	adapter.pollInterval = time.Millisecond
	result, err := adapter.GenerateImages(context.Background(), ImageRequest{
		Model:          "sdxl",
		Prompt:         "a loom",
		NegativePrompt: "blurry",
		Width:          512,
		Height:         512,
		Count:          2,
		Steps:          20,
		GuidanceScale:  Float(7.5),
		Seed:           Int(11),
		Extra:          map[string]any{"scheduler": "ddim"},
	})
	if err != nil {
		t.Fatalf("GenerateImages: %v", err)
	}
	if result.JobID != "job-1" || result.Model != "sdxl" || len(result.Artifacts) != 1 {
		t.Fatalf("result = %+v, want one artifact from job-1", result)
	}
	if artifact := result.Artifacts[0]; artifact.Filename != "a.png" || artifact.MediaType != "image/png" {
		t.Errorf("artifact = %+v, want a.png defaulted to image/png", artifact)
	}

	expected := map[string]any{
		"model":                 "sdxl",
		"prompt":                "a loom",
		"neg_prompt":            "blurry",
		"width":                 float64(512),
		"height":                float64(512),
		"num_images_per_prompt": float64(2),
		"inf_steps":             float64(20),
		"guidance_scale":        7.5,
		"seed":                  float64(11),
		"scheduler":             "ddim",
	}
	for key, want := range expected {
		if submitted[key] != want {
			t.Errorf("submitted[%q] = %v, want %v", key, submitted[key], want)
		}
	}
}

func TestImGenGenerateImagesReportsJobFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			_, _ = io.WriteString(writer, `{"job_id":"job-2","status":"queued"}`)
			return
		}
		_, _ = io.WriteString(writer, `{"job_id":"job-2","status":"failed","error":"out of memory"}`)
	}))
	defer server.Close()

	adapter := NewImGen(Config{Kind: KindImGen, BaseURL: server.URL})
	adapter.pollInterval = time.Millisecond
	_, err := adapter.GenerateImages(context.Background(), ImageRequest{Model: "sdxl", Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "out of memory") {
		t.Fatalf("error = %v, want the reported job failure", err)
	}
}

func TestImGenGenerateImagesValidatesRequest(t *testing.T) {
	adapter := NewImGen(Config{Kind: KindImGen})
	if _, err := adapter.GenerateImages(context.Background(), ImageRequest{Prompt: "x"}); err == nil {
		t.Error("expected an error without a model")
	}
	if _, err := adapter.GenerateImages(context.Background(), ImageRequest{Model: "m"}); err == nil {
		t.Error("expected an error without a prompt")
	}
	if adapter.baseURL != DefaultImGenBaseURL {
		t.Errorf("baseURL = %q, want %q", adapter.baseURL, DefaultImGenBaseURL)
	}
}

func TestImGenModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", request.URL.Path)
		}
		_, _ = io.WriteString(writer, `[{"name":"sdxl","description":"SDXL base"}]`)
	}))
	defer server.Close()

	models, err := NewImGen(Config{Kind: KindImGen, BaseURL: server.URL}).Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "sdxl" || models[0].Description != "SDXL base" {
		t.Fatalf("models = %+v, want the single sdxl entry", models)
	}
}
