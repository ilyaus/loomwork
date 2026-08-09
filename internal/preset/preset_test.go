package preset

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/provider"
)

func TestParseSelector(t *testing.T) {
	cases := []struct {
		raw    string
		want   Selector
		errMsg string
	}{
		{raw: "ollama/qwen3:8b", want: Selector{Provider: provider.KindOllama, Model: "qwen3:8b"}},
		{raw: "ollama/qwen3:8b#code-review", want: Selector{Provider: provider.KindOllama, Model: "qwen3:8b", Preset: "code-review"}},
		{raw: "lmstudio/openai/gpt-oss-20b#terse", want: Selector{Provider: provider.KindLMStudio, Model: "openai/gpt-oss-20b", Preset: "terse"}},
		{raw: " bedrock/anthropic.claude-3 ", want: Selector{Provider: provider.KindBedrock, Model: "anthropic.claude-3"}},
		{raw: "", errMsg: "selector is required"},
		{raw: "ollama", errMsg: "expected provider/model"},
		{raw: "/qwen3", errMsg: "expected provider/model"},
		{raw: "ollama/", errMsg: "expected provider/model"},
		{raw: "ollama/qwen3#", errMsg: "preset name after '#' is empty"},
		{raw: "vertex/gemini", errMsg: "unknown provider kind"},
	}

	for _, testCase := range cases {
		t.Run(testCase.raw, func(t *testing.T) {
			selector, err := ParseSelector(testCase.raw)
			if testCase.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.errMsg) {
					t.Fatalf("ParseSelector(%q) error = %v, want it to contain %q", testCase.raw, err, testCase.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSelector(%q): %v", testCase.raw, err)
			}
			if selector != testCase.want {
				t.Fatalf("ParseSelector(%q) = %+v, want %+v", testCase.raw, selector, testCase.want)
			}
		})
	}
}

func TestSelectorString(t *testing.T) {
	selector := Selector{Provider: provider.KindOllama, Model: "qwen3:8b", Preset: "terse"}
	if got, want := selector.String(), "ollama/qwen3:8b#terse"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}
}

func registryFixture(t *testing.T) *Registry {
	t.Helper()
	registry, err := New([]Entry{
		{
			Provider: provider.KindOllama,
			Model:    WildcardModel,
			Defaults: provider.Params{TopK: provider.Int(40)},
			Presets:  map[string]provider.Params{"deterministic": {Temperature: provider.Float(0)}},
		},
		{
			Provider: provider.KindOllama,
			Model:    "qwen3:8b",
			Defaults: provider.Params{Temperature: provider.Float(0.3), ContextWindow: provider.Int(8192)},
			Presets: map[string]provider.Params{
				"code-review": {Temperature: provider.Float(0.1), MaxOutputTokens: provider.Int(2048)},
				"brainstorm":  {Temperature: provider.Float(0.9), TopP: provider.Float(0.95)},
			},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return registry
}

func TestResolveLayersDefaultsPresetsAndOverrides(t *testing.T) {
	registry := registryFixture(t)

	// Model defaults over provider built-ins and the wildcard entry.
	params, err := registry.Resolve(Selector{Provider: provider.KindOllama, Model: "qwen3:8b"}, provider.Params{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if *params.Temperature != 0.3 {
		t.Fatalf("temperature = %v, want the model default 0.3", *params.Temperature)
	}
	if *params.TopP != 0.9 {
		t.Fatalf("top_p = %v, want the provider built-in 0.9", *params.TopP)
	}
	if *params.TopK != 40 {
		t.Fatalf("top_k = %v, want the wildcard default 40", *params.TopK)
	}
	if *params.ContextWindow != 8192 {
		t.Fatalf("num_ctx = %v, want 8192", *params.ContextWindow)
	}

	// Named preset over model defaults.
	params, err = registry.Resolve(Selector{Provider: provider.KindOllama, Model: "qwen3:8b", Preset: "code-review"}, provider.Params{})
	if err != nil {
		t.Fatalf("Resolve preset: %v", err)
	}
	if *params.Temperature != 0.1 || *params.MaxOutputTokens != 2048 {
		t.Fatalf("preset params = %+v, want temperature 0.1 and 2048 max tokens", params)
	}

	// Caller overrides win over everything.
	params, err = registry.Resolve(
		Selector{Provider: provider.KindOllama, Model: "qwen3:8b", Preset: "code-review"},
		provider.Params{Temperature: provider.Float(1.5), Seed: provider.Int(7)},
	)
	if err != nil {
		t.Fatalf("Resolve overrides: %v", err)
	}
	if *params.Temperature != 1.5 || *params.Seed != 7 {
		t.Fatalf("override params = %+v, want temperature 1.5 and seed 7", params)
	}
}

func TestResolveFindsWildcardPresetAndIsCaseInsensitive(t *testing.T) {
	registry := registryFixture(t)
	params, err := registry.Resolve(Selector{Provider: provider.KindOllama, Model: "qwen3:8b", Preset: "DETERMINISTIC"}, provider.Params{})
	if err != nil {
		t.Fatalf("Resolve wildcard preset: %v", err)
	}
	if *params.Temperature != 0 {
		t.Fatalf("temperature = %v, want 0 from the wildcard preset", *params.Temperature)
	}
}

func TestResolveUnknownPresetListsAvailable(t *testing.T) {
	registry := registryFixture(t)
	_, err := registry.Resolve(Selector{Provider: provider.KindOllama, Model: "qwen3:8b", Preset: "nope"}, provider.Params{})
	if err == nil {
		t.Fatal("expected an error for an unknown preset")
	}
	for _, expected := range []string{"nope", "brainstorm", "code-review", "deterministic"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q should mention %q", err, expected)
		}
	}
}

func TestResolveWithEmptyRegistryUsesBuiltinDefaults(t *testing.T) {
	registry, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	params, err := registry.Resolve(Selector{Provider: provider.KindLMStudio, Model: "qwen3-8b"}, provider.Params{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if params.Temperature == nil || *params.Temperature != 0.2 {
		t.Fatalf("temperature = %+v, want the built-in 0.2", params.Temperature)
	}
}

func TestNewRejectsInvalidEntries(t *testing.T) {
	cases := map[string]struct {
		entries []Entry
		errMsg  string
	}{
		"unknown provider": {
			entries: []Entry{{Provider: provider.Kind("vertex"), Model: "gemini"}},
			errMsg:  "unknown provider kind",
		},
		"missing model": {
			entries: []Entry{{Provider: provider.KindOllama}},
			errMsg:  "model is required",
		},
		"duplicate key": {
			entries: []Entry{{Provider: provider.KindOllama, Model: "a"}, {Provider: provider.KindOllama, Model: "a"}},
			errMsg:  "duplicate preset entry",
		},
		"temperature range": {
			entries: []Entry{{Provider: provider.KindOllama, Model: "a", Defaults: provider.Params{Temperature: provider.Float(3)}}},
			errMsg:  "temperature 3.000 out of range",
		},
		"top_p range": {
			entries: []Entry{{Provider: provider.KindOllama, Model: "a", Presets: map[string]provider.Params{"x": {TopP: provider.Float(1.5)}}}},
			errMsg:  "top_p 1.500 out of range",
		},
		"negative top_k": {
			entries: []Entry{{Provider: provider.KindOllama, Model: "a", Presets: map[string]provider.Params{"x": {TopK: provider.Int(-1)}}}},
			errMsg:  "top_k -1 must be >= 0",
		},
		"zero max tokens": {
			entries: []Entry{{Provider: provider.KindOllama, Model: "a", Presets: map[string]provider.Params{"x": {MaxOutputTokens: provider.Int(0)}}}},
			errMsg:  "max_output_tokens 0 must be >= 1",
		},
		"negative repeat penalty": {
			entries: []Entry{{Provider: provider.KindOllama, Model: "a", Presets: map[string]provider.Params{"x": {RepeatPenalty: provider.Float(-0.5)}}}},
			errMsg:  "repeat_penalty -0.500 must be >= 0",
		},
		"zero context window": {
			entries: []Entry{{Provider: provider.KindOllama, Model: "a", Presets: map[string]provider.Params{"x": {ContextWindow: provider.Int(0)}}}},
			errMsg:  "num_ctx 0 must be >= 1",
		},
		"blank preset name": {
			entries: []Entry{{Provider: provider.KindOllama, Model: "a", Presets: map[string]provider.Params{" ": {}}}},
			errMsg:  "preset name is empty",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := New(testCase.entries)
			if err == nil || !strings.Contains(err.Error(), testCase.errMsg) {
				t.Fatalf("New error = %v, want it to contain %q", err, testCase.errMsg)
			}
		})
	}
}

func TestLoadFromJSONAndKeys(t *testing.T) {
	document := `{
      "entries": [
        {
          "provider": "ollama",
          "model": "qwen3:8b",
          "defaults": {"temperature": 0.25},
          "presets": {"terse": {"max_output_tokens": 256}}
        },
        {"provider": "lmstudio", "model": "*", "defaults": {"top_p": 0.8}}
      ]
    }`
	registry, err := Load(strings.NewReader(document))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := strings.Join(registry.Keys(), ","), "lmstudio/*,ollama/qwen3:8b"; got != want {
		t.Fatalf("Keys = %q, want %q", got, want)
	}
	params, err := registry.Resolve(Selector{Provider: provider.KindOllama, Model: "qwen3:8b", Preset: "terse"}, provider.Params{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if *params.MaxOutputTokens != 256 || *params.Temperature != 0.25 {
		t.Fatalf("params = %+v, want 256 max tokens and temperature 0.25", params)
	}
	if names := registry.PresetNames(provider.KindOllama, "qwen3:8b"); len(names) != 1 || names[0] != "terse" {
		t.Fatalf("PresetNames = %v, want [terse]", names)
	}
}

func TestLoadFileMissingYieldsEmptyRegistry(t *testing.T) {
	registry, err := LoadFile(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(registry.Keys()) != 0 {
		t.Fatalf("Keys = %v, want an empty registry", registry.Keys())
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	if _, err := Load(strings.NewReader("{")); err == nil {
		t.Fatal("expected a decode error for malformed JSON")
	}
}
