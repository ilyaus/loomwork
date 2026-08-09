package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/provider"
)

func TestHomeResolutionOrder(t *testing.T) {
	t.Setenv(EnvHome, "/from/env")
	if got, err := Home("/explicit/"); err != nil || got != "/explicit" {
		t.Errorf("Home(override) = %q, %v; want the cleaned override", got, err)
	}
	if got, err := Home(""); err != nil || got != "/from/env" {
		t.Errorf("Home(\"\") = %q, %v; want the environment value", got, err)
	}

	t.Setenv(EnvHome, "")
	home, err := Home("")
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no user home directory: %v", err)
	}
	if home != filepath.Join(userHome, DefaultHomeDirName) {
		t.Errorf("Home = %q, want %q", home, filepath.Join(userHome, DefaultHomeDirName))
	}
}

func TestResolvePaths(t *testing.T) {
	paths := ResolvePaths("/ws")
	if paths.ConfigFile != filepath.Join("/ws", ConfigFileName) ||
		paths.PresetsFile != filepath.Join("/ws", PresetsFileName) ||
		paths.ProjectsDir != filepath.Join("/ws", ProjectsDirName) {
		t.Fatalf("paths = %+v, want the standard workspace layout", paths)
	}
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), ConfigFileName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Providers) != 2 || cfg.SystemPrompt != DefaultSystemPrompt {
		t.Fatalf("cfg = %+v, want the zero-configuration defaults", cfg)
	}
	if names := cfg.ProviderNames(); strings.Join(names, ",") != "lmstudio,ollama" {
		t.Errorf("names = %v, want the two local providers sorted", names)
	}
}

func TestLoadAppliesDefaultsAndInfersKindFromKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), ConfigFileName)
	document := `{"providers":{"lmstudio":{"baseUrl":"http://localhost:9999/v1","defaultModel":"qwen3-8b"}}}`
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	declared := cfg.Providers["lmstudio"]
	if declared.Kind != provider.KindLMStudio {
		t.Errorf("kind = %q, want it inferred from the map key", declared.Kind)
	}
	if declared.BaseURL != "http://localhost:9999/v1" || declared.DefaultModel != "qwen3-8b" {
		t.Errorf("declared = %+v, want the configured endpoint and model", declared)
	}
	if cfg.SystemPrompt != DefaultSystemPrompt {
		t.Errorf("system prompt = %q, want the default", cfg.SystemPrompt)
	}
}

func TestLoadReportsMalformedAndInvalidDocuments(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed.json")
	if err := os.WriteFile(malformed, []byte("{oops"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(malformed); err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Errorf("error = %v, want a parse failure", err)
	}

	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte(`{"providers":{"azure":{"kind":"azure"}}}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(invalid); err == nil || !strings.Contains(err.Error(), "azure.endpoint") {
		t.Errorf("error = %v, want provider validation to fail", err)
	}
}

func TestProviderConfigFallsBackToLocalDefaults(t *testing.T) {
	empty := Config{}
	for _, test := range []struct {
		kind    provider.Kind
		baseURL string
	}{
		{provider.KindOllama, provider.DefaultOllamaBaseURL},
		{provider.KindLMStudio, provider.DefaultLMStudioBaseURL},
		{provider.KindImGen, provider.DefaultImGenBaseURL},
	} {
		declared, err := empty.ProviderConfig(test.kind)
		if err != nil {
			t.Fatalf("ProviderConfig(%s): %v", test.kind, err)
		}
		if declared.BaseURL != test.baseURL {
			t.Errorf("baseURL = %q, want %q", declared.BaseURL, test.baseURL)
		}
	}

	if _, err := empty.ProviderConfig(provider.KindAzure); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %v, want remote providers to require configuration", err)
	}
}

func TestProviderConfigMatchesByNameThenKind(t *testing.T) {
	cfg := Config{Providers: map[string]provider.Config{
		"local-box": {Kind: provider.KindOllama, BaseURL: "http://box:11434"},
	}}
	declared, err := cfg.ProviderConfig(provider.KindOllama)
	if err != nil {
		t.Fatalf("ProviderConfig: %v", err)
	}
	if declared.BaseURL != "http://box:11434" {
		t.Fatalf("baseURL = %q, want the declaration found by kind under a custom name", declared.BaseURL)
	}
}

func TestWithDefaultsKeepsExplicitSystemPrompt(t *testing.T) {
	cfg := Config{SystemPrompt: "custom"}.WithDefaults()
	if cfg.SystemPrompt != "custom" {
		t.Errorf("system prompt = %q, want it preserved", cfg.SystemPrompt)
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("providers = %v, want the local defaults filled in", cfg.Providers)
	}
}
