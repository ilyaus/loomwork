package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/config"
	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/preset"
	"github.com/ilyaus/loomwork/internal/provider"
	"github.com/ilyaus/loomwork/internal/store"
)

// fakeGenerator records the request it received and returns a canned response.
type fakeGenerator struct {
	name     string
	response provider.Response
	err      error
	captured provider.Request
	calls    int
}

func (f *fakeGenerator) Name() string { return f.name }

func (f *fakeGenerator) Models(context.Context) ([]provider.Model, error) {
	return []provider.Model{{ID: "fake"}}, nil
}

func (f *fakeGenerator) Generate(_ context.Context, req provider.Request) (provider.Response, error) {
	f.calls++
	f.captured = req
	if f.err != nil {
		return provider.Response{}, f.err
	}
	return f.response, nil
}

type harness struct {
	orchestrator *Orchestrator
	store        store.ProjectStore
	generator    *fakeGenerator
	project      *model.Project
	target       model.Artifact
}

func newHarness(t *testing.T, generator *fakeGenerator, presets *preset.Registry) *harness {
	t.Helper()
	fileStore, err := store.NewFileStore(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	project, err := model.NewProject("triage", "", nil)
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	target, err := project.AddArtifact(model.ArtifactSpec{
		Name: "api.log", Type: model.ArtifactTypeLog, Body: model.Body{Content: "ERROR timeout"},
	})
	if err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	if err := fileStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if presets == nil {
		presets = newRegistry(t, nil)
	}
	factory := func(provider.Config) (provider.TextGenerator, error) { return generator, nil }
	return &harness{
		orchestrator: New(config.Config{}, fileStore, presets, factory),
		store:        fileStore,
		generator:    generator,
		project:      project,
		target:       target,
	}
}

func newRegistry(t *testing.T, entries []preset.Entry) *preset.Registry {
	t.Helper()
	registry, err := preset.New(entries)
	if err != nil {
		t.Fatalf("preset.New: %v", err)
	}
	return registry
}

func TestRunPromptStoresGeneratedArtifact(t *testing.T) {
	generator := &fakeGenerator{
		name: "ollama",
		response: provider.Response{
			Text:         "one timeout error",
			Model:        "qwen3:8b",
			FinishReason: "stop",
			Usage:        provider.Usage{PromptTokens: 20, CompletionTokens: 4, TotalTokens: 24},
			Raw:          map[string]string{"provider": "ollama"},
		},
	}
	harness := newHarness(t, generator, nil)

	result, err := harness.orchestrator.RunPrompt(context.Background(), RunRequest{
		ProjectRef:  "triage",
		ArtifactRef: "api.log",
		Selector:    "ollama/qwen3:8b",
		Prompt:      "summarize the errors",
		Tags:        []string{"summary"},
	})
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}

	if result.Generated.Name != "api.log.qwen3:8b" {
		t.Errorf("generated name = %q, want it derived from the target and model", result.Generated.Name)
	}
	if result.Generated.Type != model.ArtifactTypeGenerated || result.Generated.Version != 1 {
		t.Errorf("generated = %+v, want a v1 generated artifact", result.Generated)
	}
	if result.Generated.ParentID != harness.target.ID {
		t.Errorf("parentId = %q, want the target artifact %q", result.Generated.ParentID, harness.target.ID)
	}
	if result.Generated.Body.Content != "one timeout error" {
		t.Errorf("body = %q, want the provider completion", result.Generated.Body.Content)
	}
	if result.Provider != "ollama" || result.Model != "qwen3:8b" || result.Usage.TotalTokens != 24 {
		t.Errorf("result = %+v, want the provider, model, and usage reported", result)
	}
	metadata := result.Generated.Metadata
	if metadata["provider"] != "ollama" || metadata["model"] != "qwen3:8b" || metadata["sourceArtifact"] != harness.target.ID {
		t.Errorf("metadata = %v, want provenance recorded", metadata)
	}
	if len(metadata["promptSha256"]) != 64 || metadata["finishReason"] != "stop" {
		t.Errorf("metadata = %v, want a prompt digest and finish reason", metadata)
	}
	if _, present := metadata["durationMs"]; !present {
		t.Errorf("metadata = %v, want a duration", metadata)
	}

	// The prompt, system prompt, and target context reach the provider.
	if harness.generator.captured.Prompt != "summarize the errors" {
		t.Errorf("captured prompt = %q", harness.generator.captured.Prompt)
	}
	if harness.generator.captured.SystemPrompt != config.DefaultSystemPrompt {
		t.Errorf("captured system prompt = %q, want the workspace default", harness.generator.captured.SystemPrompt)
	}
	blocks := harness.generator.captured.Context
	if len(blocks) != 1 || blocks[0].Content != "ERROR timeout" || !strings.Contains(blocks[0].Label, "api.log [log v1]") {
		t.Errorf("context = %+v, want the labeled target artifact", blocks)
	}

	// The generated artifact is persisted, not just returned.
	reloaded, err := harness.store.Load(harness.project.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.Artifacts) != 2 {
		t.Fatalf("artifacts = %d, want the target plus the generated artifact", len(reloaded.Artifacts))
	}
}

func TestRunPromptIncludesPinnedContextLast(t *testing.T) {
	generator := &fakeGenerator{name: "lmstudio", response: provider.Response{Text: "ok"}}
	harness := newHarness(t, generator, nil)

	pinned, err := harness.project.AddArtifact(model.ArtifactSpec{
		Name: "conventions.md", Type: model.ArtifactTypeSpec, Body: model.Body{Content: "house rules"}, Pinned: true,
	})
	if err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	if _, err := harness.project.SetPinned(harness.target.ID, true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	if err := harness.store.Save(harness.project); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := harness.orchestrator.RunPrompt(context.Background(), RunRequest{
		ProjectRef:    harness.project.ID,
		ArtifactRef:   harness.target.ID,
		Selector:      "lmstudio/qwen3-8b",
		Prompt:        "review",
		IncludePinned: true,
	}); err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}

	blocks := harness.generator.captured.Context
	if len(blocks) != 2 {
		t.Fatalf("context = %+v, want the pinned artifact plus the target", blocks)
	}
	if !strings.Contains(blocks[0].Label, pinned.Name) || !strings.Contains(blocks[0].Label, "(pinned)") {
		t.Errorf("first block = %+v, want the pinned artifact marked", blocks[0])
	}
	// The target must be last, closest to the instruction, and not duplicated.
	if !strings.Contains(blocks[1].Label, "api.log") {
		t.Errorf("last block = %+v, want the target artifact", blocks[1])
	}
}

func TestRunPromptAppliesPresetsAndOverrides(t *testing.T) {
	registry := newRegistry(t, []preset.Entry{{
		Provider: provider.KindOllama,
		Model:    "qwen3:8b",
		Defaults: provider.Params{Temperature: provider.Float(0.2), TopP: provider.Float(0.9)},
		Presets:  map[string]provider.Params{"creative": {Temperature: provider.Float(0.9)}},
	}})
	generator := &fakeGenerator{name: "ollama", response: provider.Response{Text: "ok"}}
	harness := newHarness(t, generator, registry)

	result, err := harness.orchestrator.RunPrompt(context.Background(), RunRequest{
		ProjectRef:  "triage",
		ArtifactRef: "api.log",
		Selector:    "ollama/qwen3:8b#creative",
		Prompt:      "brainstorm",
		Overrides:   provider.Params{TopP: provider.Float(0.5)},
	})
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	params := harness.generator.captured.Params
	if params.Temperature == nil || *params.Temperature != 0.9 {
		t.Errorf("temperature = %v, want the preset value 0.9", params.Temperature)
	}
	if params.TopP == nil || *params.TopP != 0.5 {
		t.Errorf("top_p = %v, want the caller override 0.5", params.TopP)
	}
	if result.Preset != "creative" || result.Generated.Metadata["preset"] != "creative" {
		t.Errorf("result = %+v, want the preset recorded", result)
	}
	if result.Generated.Name != "api.log.creative" {
		t.Errorf("generated name = %q, want it suffixed with the preset", result.Generated.Name)
	}
}

func TestRunPromptDoesNotPersistWhenProviderFails(t *testing.T) {
	generator := &fakeGenerator{name: "ollama", err: errors.New("connection refused")}
	harness := newHarness(t, generator, nil)

	_, err := harness.orchestrator.RunPrompt(context.Background(), RunRequest{
		ProjectRef:  "triage",
		ArtifactRef: "api.log",
		Selector:    "ollama/qwen3:8b",
		Prompt:      "summarize",
	})
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("error = %v, want the provider failure surfaced", err)
	}

	reloaded, loadErr := harness.store.Load(harness.project.ID)
	if loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	if len(reloaded.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want the stored project untouched", len(reloaded.Artifacts))
	}
}

func TestRunPromptValidatesInputs(t *testing.T) {
	generator := &fakeGenerator{name: "ollama", response: provider.Response{Text: "ok"}}
	harness := newHarness(t, generator, nil)

	tests := []struct {
		name    string
		request RunRequest
		wantErr string
	}{
		{
			name:    "missing prompt",
			request: RunRequest{ProjectRef: "triage", ArtifactRef: "api.log", Selector: "ollama/m"},
			wantErr: "prompt is required",
		},
		{
			name:    "unknown project",
			request: RunRequest{ProjectRef: "nope", ArtifactRef: "api.log", Selector: "ollama/m", Prompt: "p"},
			wantErr: "resolve project",
		},
		{
			name:    "unknown artifact",
			request: RunRequest{ProjectRef: "triage", ArtifactRef: "nope", Selector: "ollama/m", Prompt: "p"},
			wantErr: "not found in project",
		},
		{
			name:    "invalid selector",
			request: RunRequest{ProjectRef: "triage", ArtifactRef: "api.log", Selector: "ollama", Prompt: "p"},
			wantErr: "expected provider/model",
		},
		{
			name:    "unknown preset",
			request: RunRequest{ProjectRef: "triage", ArtifactRef: "api.log", Selector: "ollama/m#nope", Prompt: "p"},
			wantErr: "nope",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := harness.orchestrator.RunPrompt(context.Background(), test.request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
	if harness.generator.calls != 0 {
		t.Errorf("provider calls = %d, want validation to short-circuit before generation", harness.generator.calls)
	}
}

func TestRunPromptRejectsUnconfiguredProvider(t *testing.T) {
	generator := &fakeGenerator{name: "azure", response: provider.Response{Text: "ok"}}
	harness := newHarness(t, generator, nil)

	_, err := harness.orchestrator.RunPrompt(context.Background(), RunRequest{
		ProjectRef:  "triage",
		ArtifactRef: "api.log",
		Selector:    "azure/gpt4o",
		Prompt:      "summarize",
	})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error = %v, want a configuration error for a remote provider", err)
	}
}

func TestRunPromptUsesRequestSystemPromptAndOutputOptions(t *testing.T) {
	generator := &fakeGenerator{name: "ollama", response: provider.Response{Text: "ok"}}
	harness := newHarness(t, generator, nil)

	result, err := harness.orchestrator.RunPrompt(context.Background(), RunRequest{
		ProjectRef:   "triage",
		ArtifactRef:  "api.log",
		Selector:     "ollama/qwen3:8b",
		Prompt:       "document it",
		SystemPrompt: "custom system prompt",
		OutputName:   "triage-report",
		OutputType:   model.ArtifactTypeDoc,
		Tags:         []string{"report"},
		Pin:          true,
	})
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if harness.generator.captured.SystemPrompt != "custom system prompt" {
		t.Errorf("system prompt = %q, want the request override", harness.generator.captured.SystemPrompt)
	}
	if result.Generated.Name != "triage-report" || result.Generated.Type != model.ArtifactTypeDoc {
		t.Errorf("generated = %+v, want the requested name and type", result.Generated)
	}
	if !result.Generated.Pinned || len(result.Generated.Tags) != 1 || result.Generated.Tags[0] != "report" {
		t.Errorf("generated = %+v, want it pinned and tagged", result.Generated)
	}
}

func TestArtifactContent(t *testing.T) {
	if _, err := ArtifactContent(model.Artifact{Name: "empty"}); err == nil {
		t.Error("expected an error for an artifact with neither content nor reference")
	}

	content, err := ArtifactContent(model.Artifact{Name: "inline", Body: model.Body{Content: "body"}})
	if err != nil || content != "body" {
		t.Errorf("ArtifactContent = %q, %v; want the inline content", content, err)
	}

	path := filepath.Join(t.TempDir(), "run.log")
	if err := os.WriteFile(path, []byte("from disk"), 0o644); err != nil {
		t.Fatalf("write reference file: %v", err)
	}
	content, err = ArtifactContent(model.Artifact{Name: "ref", Body: model.Body{Ref: path}})
	if err != nil || content != "from disk" {
		t.Errorf("ArtifactContent = %q, %v; want the referenced file contents", content, err)
	}

	if _, err := ArtifactContent(model.Artifact{Name: "missing", Body: model.Body{Ref: path + ".absent"}}); err == nil {
		t.Error("expected an error for a missing reference")
	}
	_, err = ArtifactContent(model.Artifact{Name: "remote", Body: model.Body{Ref: "https://example.com/log"}})
	if err == nil || !strings.Contains(err.Error(), "remote references are not fetched") {
		t.Errorf("error = %v, want remote references rejected", err)
	}
}
