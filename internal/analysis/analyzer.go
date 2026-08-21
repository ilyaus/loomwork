package analysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ilyaus/loomwork/internal/config"
	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/preset"
	"github.com/ilyaus/loomwork/internal/provider"
	"github.com/ilyaus/loomwork/internal/store"
)

// Store is the persistence an analyzer needs: project resolution plus the
// phase-1 requirement store. *store.DirStore satisfies it.
type Store interface {
	store.ProjectStore
	store.RequirementStore
}

// TextGeneratorFactory builds a text provider from a declaration. It is
// injectable so tests can substitute a fake without any network.
type TextGeneratorFactory func(cfg provider.Config) (provider.TextGenerator, error)

// Analyzer runs document analyses against a project's document sources.
type Analyzer struct {
	config   config.Config
	store    Store
	presets  *preset.Registry
	newModel TextGeneratorFactory
	now      func() time.Time
}

// New builds an analyzer. A nil factory uses provider.BuildTextGenerator.
func New(cfg config.Config, projects Store, presets *preset.Registry, factory TextGeneratorFactory) *Analyzer {
	if factory == nil {
		factory = provider.BuildTextGenerator
	}
	return &Analyzer{
		config:   cfg.WithDefaults(),
		store:    projects,
		presets:  presets,
		newModel: factory,
		now:      time.Now,
	}
}

// MaxSourceBytes bounds how much of a source's local copy is loaded as context.
const MaxSourceBytes = 1 << 20 // 1 MiB

// ArtifactName is the default name of the stored analysis artifact. Repeated
// analyses of the same project become revisions of it.
const ArtifactName = "document-analysis"

// RunRequest describes one provider-driven document analysis.
type RunRequest struct {
	// ProjectRef is a project id or name.
	ProjectRef string
	// Selector is `provider/model[#preset]`.
	Selector string
	// SystemPrompt overrides the workspace default.
	SystemPrompt string
	// OutputName names the stored analysis artifact; defaults to ArtifactName.
	OutputName string
	// Tags are applied to the stored analysis artifact.
	Tags []string
	// SkipExtract stores the analysis without writing extracted requirements.
	SkipExtract bool
	// Overrides win over every preset and default.
	Overrides provider.Params
}

// ImportRequest describes ingesting an analysis produced outside Loomwork.
type ImportRequest struct {
	// ProjectRef is a project id or name.
	ProjectRef string
	// Payload is the raw analysis JSON, which must satisfy
	// docs/schemas/document-analysis.schema.json.
	Payload []byte
	// SourcePath records where the payload came from in the provenance
	// metadata. Optional.
	SourcePath string
	// OutputName names the stored analysis artifact; defaults to ArtifactName.
	OutputName string
	// Tags are applied to the stored analysis artifact.
	Tags []string
	// SkipExtract stores the analysis without writing extracted requirements.
	SkipExtract bool
}

// Result reports what an analysis produced.
type Result struct {
	Project      *model.Project       `json:"-"`
	ProjectID    string               `json:"projectId"`
	Analysis     Document             `json:"analysis"`
	Artifact     model.Artifact       `json:"artifact"`
	Requirements []*model.Requirement `json:"requirements,omitempty"`
	Provider     string               `json:"provider,omitempty"`
	Model        string               `json:"model,omitempty"`
	Preset       string               `json:"preset,omitempty"`
	Usage        provider.Usage       `json:"usage,omitempty"`
	DurationMs   int64                `json:"durationMs,omitempty"`
}

// Run assembles the project's document sources into a prompt, calls the
// provider, parses the structured analysis, stores it as a project artifact,
// and writes the extracted requirements to the requirement store.
func (a *Analyzer) Run(ctx context.Context, request RunRequest) (Result, error) {
	project, err := a.store.Resolve(request.ProjectRef)
	if err != nil {
		return Result{}, fmt.Errorf("resolve project %q: %w", request.ProjectRef, err)
	}
	if len(project.Sources) == 0 {
		return Result{}, fmt.Errorf("project %q has no document sources: add them with `project source` first", project.Name)
	}

	selector, err := preset.ParseSelector(request.Selector)
	if err != nil {
		return Result{}, err
	}
	params, err := a.presets.Resolve(selector, request.Overrides)
	if err != nil {
		return Result{}, err
	}
	providerConfig, err := a.config.ProviderConfig(selector.Provider)
	if err != nil {
		return Result{}, err
	}
	generator, err := a.newModel(providerConfig)
	if err != nil {
		return Result{}, fmt.Errorf("build provider %q: %w", selector.Provider, err)
	}

	blocks, err := sourceBlocks(project.Sources)
	if err != nil {
		return Result{}, err
	}

	generateRequest := provider.Request{
		Model:        selector.Model,
		SystemPrompt: firstNonEmpty(request.SystemPrompt, analysisSystemPrompt),
		Prompt:       analysisPrompt,
		Context:      blocks,
		Params:       params,
	}

	started := a.now()
	response, err := generator.Generate(ctx, generateRequest)
	if err != nil {
		return Result{}, fmt.Errorf("analyze sources of project %q with %s: %w", project.Name, selector, err)
	}
	duration := a.now().Sub(started)

	analysis, err := ParseModelOutput(response.Text)
	if err != nil {
		return Result{}, err
	}
	if analysis.Sources == nil {
		analysis.Sources = sourceNames(project.Sources)
	}

	provenance := map[string]string{
		"provider":     generator.Name(),
		"model":        firstNonEmpty(response.Model, selector.Model),
		"promptSha256": promptDigest(analysisPrompt),
	}
	if selector.Preset != "" {
		provenance["preset"] = selector.Preset
	}

	artifactMetadata := copyMetadata(provenance)
	artifactMetadata["durationMs"] = strconv.FormatInt(duration.Milliseconds(), 10)
	if response.FinishReason != "" {
		artifactMetadata["finishReason"] = response.FinishReason
	}
	result, err := a.persist(project.ID, analysis, request.OutputName, request.Tags, request.SkipExtract, artifactMetadata, provenance)
	if err != nil {
		return Result{}, err
	}
	result.Provider = generator.Name()
	result.Model = firstNonEmpty(response.Model, selector.Model)
	result.Preset = selector.Preset
	result.Usage = response.Usage
	result.DurationMs = duration.Milliseconds()
	return result, nil
}

// Import ingests an analysis authored outside Loomwork: it validates the
// payload against the fixed schema shape, stores it as a project artifact, and
// writes the extracted requirements to the requirement store.
func (a *Analyzer) Import(request ImportRequest) (Result, error) {
	project, err := a.store.Resolve(request.ProjectRef)
	if err != nil {
		return Result{}, fmt.Errorf("resolve project %q: %w", request.ProjectRef, err)
	}
	analysis, err := Parse(request.Payload)
	if err != nil {
		return Result{}, err
	}

	provenance := map[string]string{"origin": "manual-import"}
	if strings.TrimSpace(request.SourcePath) != "" {
		provenance["importedFrom"] = strings.TrimSpace(request.SourcePath)
	}
	return a.persist(project.ID, analysis, request.OutputName, request.Tags, request.SkipExtract, copyMetadata(provenance), provenance)
}

// persist stores the analysis as a project artifact and, unless skipped, writes
// its extracted requirements through the phase-1 store with origin: extracted
// and the given provenance in each requirement's metadata. The artifact is
// stored first so requirement provenance can point back at it.
func (a *Analyzer) persist(projectID string, analysis *Document, outputName string, tags []string, skipExtract bool, artifactMetadata, provenance map[string]string) (Result, error) {
	if analysis.CreatedAt.IsZero() {
		analysis.CreatedAt = a.now().UTC()
	}
	encoded, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encode document analysis: %w", err)
	}

	spec := model.ArtifactSpec{
		Name:     firstNonEmpty(outputName, ArtifactName),
		Type:     model.ArtifactTypeDoc,
		Body:     model.Body{Content: string(encoded), MediaType: "application/json"},
		Tags:     tags,
		Metadata: artifactMetadata,
	}
	var artifact model.Artifact
	project, err := a.store.Update(projectID, func(current *model.Project) error {
		added, err := current.AddArtifact(spec)
		artifact = added
		return err
	})
	if err != nil {
		return Result{}, fmt.Errorf("store document analysis: %w", err)
	}

	result := Result{
		Project:   project,
		ProjectID: project.ID,
		Analysis:  *analysis,
		Artifact:  artifact,
	}
	if skipExtract || len(analysis.ExtractedRequirements) == 0 {
		return result, nil
	}

	metadata := copyMetadata(provenance)
	metadata["analysisArtifact"] = artifact.ID
	for i, extracted := range analysis.ExtractedRequirements {
		requirement, err := a.store.CreateRequirement(project.ID, model.RequirementSpec{
			Text:       extracted.Text,
			SourceType: extracted.SourceType,
			SourceRef:  extracted.SourceRef,
			Status:     model.RequirementStatusActive,
			Origin:     model.RequirementOriginExtracted,
			Tags:       extracted.Tags,
			Metadata:   metadata,
		})
		if err != nil {
			return Result{}, fmt.Errorf("store extracted requirement %d of %d: %w (analysis artifact %s was stored)",
				i+1, len(analysis.ExtractedRequirements), err, artifact.ID)
		}
		result.Requirements = append(result.Requirements, requirement)
	}
	return result, nil
}

// sourceBlocks renders each document source as one context block: its
// back-references always, plus the text of a local copy when the project has
// one. Remote copies are never fetched, matching the orchestrator's rule that
// a run makes no unexpected network calls.
func sourceBlocks(sources []model.DocumentSource) ([]provider.ContextBlock, error) {
	blocks := make([]provider.ContextBlock, 0, len(sources))
	for _, source := range sources {
		content, err := sourceContent(source)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, provider.ContextBlock{
			Label:   fmt.Sprintf("%s [%s]", source.Name, source.Type),
			Content: content,
		})
	}
	return blocks, nil
}

func sourceContent(source model.DocumentSource) (string, error) {
	var builder strings.Builder
	if source.URL != "" {
		fmt.Fprintf(&builder, "URL: %s\n", source.URL)
	}
	if source.S3URI != "" {
		fmt.Fprintf(&builder, "S3 copy (not fetched): %s\n", source.S3URI)
	}
	if source.LocalPath == "" {
		builder.WriteString("No local copy is available; only the reference above is known.\n")
		return builder.String(), nil
	}
	info, err := os.Stat(source.LocalPath)
	if err != nil {
		return "", fmt.Errorf("document source %q local copy %s: %w", source.Name, source.LocalPath, err)
	}
	if info.Size() > MaxSourceBytes {
		return "", fmt.Errorf("document source %q local copy %s is %d bytes, exceeding the %d byte context limit",
			source.Name, source.LocalPath, info.Size(), MaxSourceBytes)
	}
	raw, err := os.ReadFile(source.LocalPath)
	if err != nil {
		return "", fmt.Errorf("read document source %q local copy %s: %w", source.Name, source.LocalPath, err)
	}
	builder.Write(raw)
	return builder.String(), nil
}

func sourceNames(sources []model.DocumentSource) []string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, source.Name)
	}
	return names
}

// analysisSystemPrompt frames the model as a QA documentation reviewer.
const analysisSystemPrompt = "You are Loomwork, a QA workbench assistant reviewing a service's documentation for test readiness. " +
	"Judge only from the supplied documents. Be precise, and state explicitly when the documents do not contain an answer."

// analysisPrompt is the fixed instruction whose required output shape is
// docs/schemas/document-analysis.schema.json.
const analysisPrompt = `Analyze the documentation supplied as context and decide whether the service is ready for QA test work.

Respond with a single JSON object and nothing else, in exactly this shape:
{
  "verdict": "ready" | "ready-with-gaps" | "not-ready",
  "spec_in_sync": true | false,
  "summary": "one short paragraph justifying the verdict",
  "gaps": ["each thing the documentation is missing or contradicts itself on"],
  "open_questions": ["each question QA must have answered before testing the affected behavior"],
  "extracted_requirements": [{"text": "one testable requirement in tester-friendly language"}]
}

Rules:
- "verdict": "ready" only when testing can proceed without blockers; "ready-with-gaps" when testing can start but the gaps limit coverage; "not-ready" when the documentation cannot support meaningful test work.
- Omit "spec_in_sync" unless the documents include both requirements and an API spec to compare.
- "gaps" and "open_questions" must always be present; use [] when there are none.
- Each "extracted_requirements" entry must state one observable, testable behavior found in the documents. Do not invent behavior the documents do not describe.`

func promptDigest(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

func copyMetadata(values map[string]string) map[string]string {
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
