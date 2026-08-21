package analysis

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

const validAnalysis = `{
	"verdict": "ready-with-gaps",
	"spec_in_sync": false,
	"summary": "testable but the error catalog is missing",
	"gaps": ["no error catalog"],
	"open_questions": ["is order creation idempotent?"],
	"extracted_requirements": [
		{"text": "POST /orders returns 201 with the order id", "source_type": "github", "source_ref": "https://github.com/x/y/blob/main/api.md", "tags": ["api"]},
		{"text": "cancelled orders cannot be paid"}
	]
}`

type harness struct {
	analyzer  *Analyzer
	store     *store.DirStore
	generator *fakeGenerator
	project   *model.Project
}

func newHarness(t *testing.T, generator *fakeGenerator, sources []model.DocumentSource) *harness {
	t.Helper()
	dirStore, err := store.NewDirStore(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	project, err := model.NewProject("checkout", "", nil)
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	for _, source := range sources {
		if _, err := project.AddSource(source); err != nil {
			t.Fatalf("AddSource: %v", err)
		}
	}
	if err := dirStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	registry, err := preset.New(nil)
	if err != nil {
		t.Fatalf("preset.New: %v", err)
	}
	factory := func(provider.Config) (provider.TextGenerator, error) { return generator, nil }
	return &harness{
		analyzer:  New(config.Config{}, dirStore, registry, factory),
		store:     dirStore,
		generator: generator,
		project:   project,
	}
}

func specSource(t *testing.T, content string) model.DocumentSource {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return model.DocumentSource{
		Name:      "api spec",
		Type:      model.SourceTypeGitHub,
		URL:       "https://github.com/x/y/blob/main/api.md",
		LocalPath: path,
	}
}

func TestRunStoresAnalysisAndExtractsRequirements(t *testing.T) {
	generator := &fakeGenerator{
		name: "ollama",
		response: provider.Response{
			Text:  "```json\n" + validAnalysis + "\n```",
			Model: "qwen3:8b",
			Usage: provider.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		},
	}
	harness := newHarness(t, generator, []model.DocumentSource{specSource(t, "POST /orders creates an order.")})

	result, err := harness.analyzer.Run(context.Background(), RunRequest{
		ProjectRef: "checkout",
		Selector:   "ollama/qwen3:8b",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The provider request carries every document source as labeled context.
	if generator.calls != 1 {
		t.Fatalf("calls = %d, want one generation", generator.calls)
	}
	if len(generator.captured.Context) != 1 {
		t.Fatalf("context = %+v, want one block per source", generator.captured.Context)
	}
	block := generator.captured.Context[0]
	if block.Label != "api spec [github]" {
		t.Errorf("label = %q, want the source name and type", block.Label)
	}
	if !strings.Contains(block.Content, "POST /orders creates an order.") || !strings.Contains(block.Content, "https://github.com/x/y/blob/main/api.md") {
		t.Errorf("content = %q, want the local copy text and the back-reference", block.Content)
	}
	if !strings.Contains(generator.captured.Prompt, `"verdict"`) {
		t.Errorf("prompt = %q, want it to demand the analysis JSON shape", generator.captured.Prompt)
	}
	if generator.captured.SystemPrompt == "" {
		t.Error("system prompt is empty, want the analysis default")
	}

	if result.Analysis.Verdict != VerdictReadyWithGaps {
		t.Errorf("verdict = %q, want %q", result.Analysis.Verdict, VerdictReadyWithGaps)
	}
	if result.Analysis.SpecInSync == nil || *result.Analysis.SpecInSync {
		t.Errorf("spec_in_sync = %v, want false", result.Analysis.SpecInSync)
	}

	// The analysis is stored as a doc artifact with provider provenance.
	if result.Artifact.Name != ArtifactName || result.Artifact.Type != model.ArtifactTypeDoc {
		t.Errorf("artifact = %+v, want the default analysis doc artifact", result.Artifact)
	}
	if result.Artifact.Metadata["provider"] != "ollama" || result.Artifact.Metadata["model"] != "qwen3:8b" {
		t.Errorf("artifact metadata = %v, want provider and model provenance", result.Artifact.Metadata)
	}
	if !strings.Contains(result.Artifact.Body.Content, `"ready-with-gaps"`) {
		t.Errorf("artifact body = %q, want the analysis JSON", result.Artifact.Body.Content)
	}

	// Requirements land in the phase-1 store with origin: extracted.
	if len(result.Requirements) != 2 {
		t.Fatalf("requirements = %+v, want both extracted requirements", result.Requirements)
	}
	stored, err := harness.store.ListRequirements("checkout")
	if err != nil {
		t.Fatalf("ListRequirements: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored = %+v, want both requirements persisted", stored)
	}
	first := result.Requirements[0]
	if first.Origin != model.RequirementOriginExtracted {
		t.Errorf("origin = %q, want %q", first.Origin, model.RequirementOriginExtracted)
	}
	if first.SourceType != model.SourceTypeGitHub || first.SourceRef == "" {
		t.Errorf("requirement = %+v, want the source back-reference preserved", first)
	}
	if first.Metadata["provider"] != "ollama" || first.Metadata["model"] != "qwen3:8b" {
		t.Errorf("metadata = %v, want provider and model provenance", first.Metadata)
	}
	if first.Metadata["analysisArtifact"] != result.Artifact.ID {
		t.Errorf("metadata = %v, want a back-reference to the analysis artifact", first.Metadata)
	}
	if result.Requirements[1].SourceType != "" || result.Requirements[1].SourceRef != "" {
		t.Errorf("requirement = %+v, want no source back-reference when the model gave none", result.Requirements[1])
	}
}

func TestRunSkipExtract(t *testing.T) {
	generator := &fakeGenerator{name: "ollama", response: provider.Response{Text: validAnalysis, Model: "qwen3:8b"}}
	harness := newHarness(t, generator, []model.DocumentSource{specSource(t, "spec")})

	result, err := harness.analyzer.Run(context.Background(), RunRequest{
		ProjectRef:  "checkout",
		Selector:    "ollama/qwen3:8b",
		SkipExtract: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Requirements) != 0 {
		t.Errorf("requirements = %+v, want none with --no-extract", result.Requirements)
	}
	stored, err := harness.store.ListRequirements("checkout")
	if err != nil {
		t.Fatalf("ListRequirements: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("stored = %+v, want no requirements written", stored)
	}
}

func TestRunFailures(t *testing.T) {
	sources := []model.DocumentSource{{
		Name: "api spec", Type: model.SourceTypeGitHub, URL: "https://github.com/x/y/blob/main/api.md",
	}}
	tests := []struct {
		name      string
		generator *fakeGenerator
		sources   []model.DocumentSource
		request   RunRequest
		wantErr   string
	}{
		{
			name:      "project without sources",
			generator: &fakeGenerator{name: "ollama"},
			sources:   nil,
			request:   RunRequest{ProjectRef: "checkout", Selector: "ollama/m"},
			wantErr:   "has no document sources",
		},
		{
			name:      "unknown project",
			generator: &fakeGenerator{name: "ollama"},
			sources:   sources,
			request:   RunRequest{ProjectRef: "nope", Selector: "ollama/m"},
			wantErr:   "not found",
		},
		{
			name:      "bad selector",
			generator: &fakeGenerator{name: "ollama"},
			sources:   sources,
			request:   RunRequest{ProjectRef: "checkout", Selector: "bogus/m"},
			wantErr:   "unknown provider kind",
		},
		{
			name:      "provider failure",
			generator: &fakeGenerator{name: "ollama", err: errors.New("connection refused")},
			sources:   sources,
			request:   RunRequest{ProjectRef: "checkout", Selector: "ollama/m"},
			wantErr:   "connection refused",
		},
		{
			name:      "model returns prose",
			generator: &fakeGenerator{name: "ollama", response: provider.Response{Text: "cannot analyze"}},
			sources:   sources,
			request:   RunRequest{ProjectRef: "checkout", Selector: "ollama/m"},
			wantErr:   "contains no JSON object",
		},
		{
			name:      "model returns incomplete analysis",
			generator: &fakeGenerator{name: "ollama", response: provider.Response{Text: `{"verdict":"ready"}`}},
			sources:   sources,
			request:   RunRequest{ProjectRef: "checkout", Selector: "ollama/m"},
			wantErr:   "gaps is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newHarness(t, test.generator, test.sources)
			_, err := harness.analyzer.Run(context.Background(), test.request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, test.wantErr)
			}
			// A failed run must leave the store untouched.
			project, loadErr := harness.store.Resolve("checkout")
			if loadErr != nil {
				t.Fatalf("Resolve: %v", loadErr)
			}
			if len(project.Artifacts) != 0 {
				t.Errorf("artifacts = %+v, want none stored on failure", project.Artifacts)
			}
		})
	}
}

func TestRunFailsOnOversizedLocalCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.md")
	if err := os.WriteFile(path, make([]byte, MaxSourceBytes+1), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	generator := &fakeGenerator{name: "ollama", response: provider.Response{Text: validAnalysis}}
	harness := newHarness(t, generator, []model.DocumentSource{{
		Name: "big doc", Type: model.SourceTypeOther, LocalPath: path,
	}})

	_, err := harness.analyzer.Run(context.Background(), RunRequest{ProjectRef: "checkout", Selector: "ollama/m"})
	if err == nil || !strings.Contains(err.Error(), "context limit") {
		t.Fatalf("error = %v, want the size limit error", err)
	}
	if generator.calls != 0 {
		t.Errorf("calls = %d, want no generation after a source failure", generator.calls)
	}
}

func TestImportStoresAnalysisAndExtractsRequirements(t *testing.T) {
	harness := newHarness(t, &fakeGenerator{name: "unused"}, nil)

	result, err := harness.analyzer.Import(ImportRequest{
		ProjectRef: "checkout",
		Payload:    []byte(validAnalysis),
		SourcePath: "/reviews/checkout-analysis.json",
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if result.Artifact.Name != ArtifactName || result.Artifact.Type != model.ArtifactTypeDoc {
		t.Errorf("artifact = %+v, want the default analysis doc artifact", result.Artifact)
	}
	if result.Artifact.Metadata["origin"] != "manual-import" || result.Artifact.Metadata["importedFrom"] != "/reviews/checkout-analysis.json" {
		t.Errorf("artifact metadata = %v, want manual-import provenance", result.Artifact.Metadata)
	}
	if len(result.Requirements) != 2 {
		t.Fatalf("requirements = %+v, want both extracted requirements", result.Requirements)
	}
	first := result.Requirements[0]
	if first.Origin != model.RequirementOriginExtracted {
		t.Errorf("origin = %q, want %q", first.Origin, model.RequirementOriginExtracted)
	}
	if first.Metadata["origin"] != "manual-import" || first.Metadata["importedFrom"] != "/reviews/checkout-analysis.json" {
		t.Errorf("metadata = %v, want manual-import provenance", first.Metadata)
	}
	if first.Metadata["analysisArtifact"] != result.Artifact.ID {
		t.Errorf("metadata = %v, want a back-reference to the analysis artifact", first.Metadata)
	}
	if harness.generator.calls != 0 {
		t.Errorf("calls = %d, want no provider involvement on import", harness.generator.calls)
	}
}

func TestImportRejectsInvalidPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{name: "not json", payload: "verdict: ready", wantErr: "parse document analysis"},
		{name: "unknown field", payload: `{"verdict":"ready","gaps":[],"open_questions":[],"vrdict":"x"}`, wantErr: `unknown field "vrdict"`},
		{name: "invalid verdict", payload: `{"verdict":"maybe","gaps":[],"open_questions":[]}`, wantErr: "unknown verdict"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newHarness(t, &fakeGenerator{name: "unused"}, nil)
			_, err := harness.analyzer.Import(ImportRequest{ProjectRef: "checkout", Payload: []byte(test.payload)})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestRepeatedAnalysesBecomeRevisions(t *testing.T) {
	harness := newHarness(t, &fakeGenerator{name: "unused"}, nil)

	first, err := harness.analyzer.Import(ImportRequest{ProjectRef: "checkout", Payload: []byte(validAnalysis), SkipExtract: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	second, err := harness.analyzer.Import(ImportRequest{ProjectRef: "checkout", Payload: []byte(validAnalysis), SkipExtract: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if first.Artifact.Version != 1 || second.Artifact.Version != 2 {
		t.Errorf("versions = %d then %d, want revisions 1 and 2", first.Artifact.Version, second.Artifact.Version)
	}
}
