// Package testgen turns a project's inputs — an OpenAPI/Swagger spec, the
// current requirements, the current override rules, and test templates — into a
// versioned test suite through a stateful agent session, and ingests suites
// authored elsewhere into the same store. It owns no persistence and no provider
// details: the store and the agent adapter are injected.
package testgen

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/provider"
	"github.com/ilyaus/loomwork/internal/store"
)

// Store is the persistence generation needs. *store.DirStore satisfies it.
type Store interface {
	store.ProjectStore
	store.RequirementStore
	store.AgentDefinitionStore
	store.TestSuiteStore
}

// AdapterFactory builds the agent adapter a definition targets. It is injectable
// so tests and dry runs use provider.StubAgentAdapter and never a live SDK.
type AdapterFactory func(target model.AgentTarget) (provider.AgentAdapter, error)

// MaxSpecBytes bounds how much of a spec or template file is loaded as context.
const MaxSpecBytes = 2 << 20 // 2 MiB

// Generator generates and imports test suites for a project.
type Generator struct {
	store   Store
	adapter AdapterFactory
	now     func() time.Time
}

// New builds a generator. A nil factory uses BuildAdapter.
func New(projects Store, factory AdapterFactory) *Generator {
	if factory == nil {
		factory = BuildAdapter
	}
	return &Generator{store: projects, adapter: factory, now: time.Now}
}

// BuildAdapter is the default adapter factory: the Claude Agent SDK bridge for a
// Claude-targeted definition. Copilot is a declared target with no adapter yet,
// so it fails with provider.ErrNotImplemented instead of silently using Claude.
func BuildAdapter(target model.AgentTarget) (provider.AgentAdapter, error) {
	switch target {
	case model.AgentTargetClaudeSDK:
		return provider.NewClaudeAgentAdapter(provider.ClaudeAgentConfig{}), nil
	case model.AgentTargetCopilotSDK:
		return nil, fmt.Errorf("agent target %s: %w", target, provider.ErrNotImplemented)
	default:
		return nil, fmt.Errorf("unknown agent target %q", target)
	}
}

// GenerateRequest describes one agent-driven generation.
type GenerateRequest struct {
	// ProjectRef is a project id or name.
	ProjectRef string
	// SuiteID names the suite; a new version is written every run.
	SuiteID string
	// AgentName selects the agent definition; its current version is used.
	AgentName string
	// SpecPath is the OpenAPI/Swagger file.
	SpecPath string
	// TemplatePaths are test template files handed to the agent as examples.
	TemplatePaths []string
	// Model overrides the model named by the agent definition.
	Model string
	// Title and Description describe the suite version.
	Title       string
	Description string
	Tags        []string
	// Instructions are appended to the generated prompt.
	Instructions string
}

// ImportRequest describes ingesting a suite authored outside Loomwork.
type ImportRequest struct {
	// ProjectRef is a project id or name.
	ProjectRef string
	// SuiteID overrides the suite id in the payload.
	SuiteID string
	// Payload is the raw suite JSON, which must satisfy
	// docs/schemas/test-case.schema.json.
	Payload []byte
	// SourcePath records where the payload came from. Optional.
	SourcePath  string
	Title       string
	Description string
	Tags        []string
}

// Result reports what generation or import produced. Audit is returned even on
// success: annotations changed the stored suite and problems made it incomplete,
// and both are things a QA engineer has to see.
type Result struct {
	ProjectID  string               `json:"projectId"`
	Suite      *model.TestSuite     `json:"suite"`
	Audit      model.TestSuiteAudit `json:"audit"`
	Agent      string               `json:"agent,omitempty"`
	Adapter    string               `json:"adapter,omitempty"`
	Model      string               `json:"model,omitempty"`
	Usage      provider.Usage       `json:"usage,omitempty"`
	Events     int                  `json:"events,omitempty"`
	DurationMs int64                `json:"durationMs,omitempty"`
}

// Generate runs one agent session and stores the suite it produces as the next
// version. Nothing is executed: Loomwork hands suites to external executors.
func (g *Generator) Generate(ctx context.Context, request GenerateRequest) (Result, error) {
	project, err := g.store.Resolve(request.ProjectRef)
	if err != nil {
		return Result{}, fmt.Errorf("resolve project %q: %w", request.ProjectRef, err)
	}
	if strings.TrimSpace(request.SuiteID) == "" {
		return Result{}, fmt.Errorf("suite id is required")
	}
	if strings.TrimSpace(request.SpecPath) == "" {
		return Result{}, fmt.Errorf("an OpenAPI/Swagger spec is required: pass --spec")
	}

	definition, err := g.store.LoadAgentDefinition(project.ID, request.AgentName, 0)
	if err != nil {
		return Result{}, err
	}
	requirements, err := g.currentRequirements(project.ID)
	if err != nil {
		return Result{}, err
	}
	if len(requirements) == 0 {
		return Result{}, fmt.Errorf("project %s has no active requirements: every generated test case must link to one", project.Name)
	}
	rules, err := g.store.ActiveOverrideRules(project.ID)
	if err != nil {
		return Result{}, err
	}
	spec, err := readBoundedFile(request.SpecPath)
	if err != nil {
		return Result{}, err
	}
	templates := make([]namedDocument, 0, len(request.TemplatePaths))
	for _, path := range request.TemplatePaths {
		content, err := readBoundedFile(path)
		if err != nil {
			return Result{}, err
		}
		templates = append(templates, namedDocument{Name: filepath.Base(path), Content: content})
	}

	adapter, err := g.adapter(definition.TargetProvider)
	if err != nil {
		return Result{}, err
	}
	inputs := agentInputs{
		spec:         spec,
		specPath:     request.SpecPath,
		requirements: requirements,
		rules:        rules,
		templates:    templates,
	}
	modelID := strings.TrimSpace(request.Model)
	if modelID == "" {
		modelID = definition.Model
	}
	session, err := adapter.StartSession(ctx, provider.AgentSessionSpec{
		Model:        modelID,
		SystemPrompt: definition.Body,
		Tools:        inputs.tools(*definition),
		Metadata:     map[string]string{"agent_definition": definition.Ref(), "project": project.ID},
	})
	if err != nil {
		return Result{}, fmt.Errorf("start %s session for agent %s: %w", adapter.Name(), definition.Ref(), err)
	}
	defer func() { _ = session.Close() }()

	events := 0
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range session.Events() {
			events++
		}
	}()

	started := g.now()
	result, err := session.Send(ctx, provider.PromptRequest{
		Prompt:     inputs.prompt(request),
		Structured: &provider.StructuredOutput{Name: "test_suite", Description: "the generated test suite", Schema: SuiteSchema()},
	})
	if err != nil {
		return Result{}, fmt.Errorf("generate test suite with agent %s: %w", definition.Ref(), err)
	}
	duration := g.now().Sub(started)

	payload := result.Text
	if len(result.Structured) > 0 {
		payload = string(result.Structured)
	}
	suite, err := model.ParseTestSuiteModelOutput(payload, strings.ToLower(strings.TrimSpace(request.SuiteID)))
	if err != nil {
		return Result{}, err
	}

	suite.Origin = model.TestSuiteOriginGenerated
	suite.AgentDefinition = definition.Ref()
	suite.SpecRef = request.SpecPath
	if suite.Metadata == nil {
		suite.Metadata = map[string]string{}
	}
	// The path alone is not provenance: the file it names can change under a
	// stored suite, so the digest of what the agent actually read is recorded.
	suite.Metadata["spec_sha256"] = fmt.Sprintf("%x", sha256.Sum256([]byte(spec)))
	suite.RequirementVersions = requirementVersions(requirements)
	suite.OverrideRules = ruleRefs(rules)
	suite.CreatedAt = g.now().UTC()
	if strings.TrimSpace(request.Title) != "" {
		suite.Title = request.Title
	}
	if strings.TrimSpace(request.Description) != "" {
		suite.Description = request.Description
	}
	if len(request.Tags) > 0 {
		suite.Tags = request.Tags
	}

	stored, audit, err := g.finalize(project.ID, suite, rules, requirements)
	if err != nil {
		return Result{}, err
	}
	_ = session.Close()
	<-drained

	return Result{
		ProjectID:  project.ID,
		Suite:      stored,
		Audit:      audit,
		Agent:      definition.Ref(),
		Adapter:    adapter.Name(),
		Model:      modelID,
		Usage:      result.Usage,
		Events:     events,
		DurationMs: duration.Milliseconds(),
	}, nil
}

// Import stores a suite authored outside Loomwork as a new version of the same
// suite id, audited exactly like a generated one: an imported suite that leaves
// cases unlinked is flagged incomplete rather than trusted.
func (g *Generator) Import(request ImportRequest) (Result, error) {
	project, err := g.store.Resolve(request.ProjectRef)
	if err != nil {
		return Result{}, fmt.Errorf("resolve project %q: %w", request.ProjectRef, err)
	}
	suite, err := model.ParseTestSuite(request.Payload, strings.ToLower(strings.TrimSpace(request.SuiteID)))
	if err != nil {
		return Result{}, err
	}
	suite.Origin = model.TestSuiteOriginImported
	suite.CreatedAt = g.now().UTC()
	if strings.TrimSpace(request.Title) != "" {
		suite.Title = request.Title
	}
	if strings.TrimSpace(request.Description) != "" {
		suite.Description = request.Description
	}
	if len(request.Tags) > 0 {
		suite.Tags = request.Tags
	}
	if strings.TrimSpace(request.SourcePath) != "" {
		if suite.Metadata == nil {
			suite.Metadata = map[string]string{}
		}
		suite.Metadata["imported_from"] = strings.TrimSpace(request.SourcePath)
	}

	requirements, err := g.currentRequirements(project.ID)
	if err != nil {
		return Result{}, err
	}
	rules, err := g.store.ActiveOverrideRules(project.ID)
	if err != nil {
		return Result{}, err
	}
	if len(suite.RequirementVersions) == 0 {
		suite.RequirementVersions = requirementVersions(requirements)
	}
	if len(suite.OverrideRules) == 0 {
		suite.OverrideRules = ruleRefs(rules)
	}

	stored, audit, err := g.finalize(project.ID, suite, rules, requirements)
	if err != nil {
		return Result{}, err
	}
	return Result{ProjectID: project.ID, Suite: stored, Audit: audit}, nil
}

// finalize audits the suite against the override rules and the requirement store,
// then stores it as the next version. Auditing before storing is what keeps an
// incomplete suite honest: the flag and its reasons are part of the stored
// version, not a transient warning.
func (g *Generator) finalize(projectID string, suite *model.TestSuite, rules []*model.OverrideRule, requirements []*model.Requirement) (*model.TestSuite, model.TestSuiteAudit, error) {
	audit := suite.ApplyOverrideRules(dereferenceRules(rules))
	flagUnknownRequirements(suite, requirements)
	stored, err := g.store.SaveTestSuite(projectID, suite)
	if err != nil {
		return nil, audit, err
	}
	return stored, audit, nil
}

// flagUnknownRequirements adds an incompleteness reason for a case linked to a
// requirement the project does not have. A dangling link looks like traceability
// but proves nothing, so it cannot pass silently.
func flagUnknownRequirements(suite *model.TestSuite, requirements []*model.Requirement) {
	known := make(map[string]bool, len(requirements))
	for _, requirement := range requirements {
		known[strings.ToLower(requirement.ID)] = true
	}
	for _, testCase := range suite.Cases {
		for _, id := range testCase.RequirementIDs {
			if !known[id] {
				suite.FlagIncomplete(fmt.Sprintf("%s: links to requirement %s, which is not an active requirement of this project", testCase.ID, id))
			}
		}
	}
}

func (g *Generator) currentRequirements(projectID string) ([]*model.Requirement, error) {
	requirements, err := g.store.ListRequirements(projectID)
	if err != nil {
		return nil, err
	}
	active := make([]*model.Requirement, 0, len(requirements))
	for _, requirement := range requirements {
		if requirement.Status == model.RequirementStatusActive {
			active = append(active, requirement)
		}
	}
	return active, nil
}

func requirementVersions(requirements []*model.Requirement) map[string]int {
	versions := make(map[string]int, len(requirements))
	for _, requirement := range requirements {
		versions[requirement.ID] = requirement.Version
	}
	return versions
}

func ruleRefs(rules []*model.OverrideRule) []string {
	refs := make([]string, 0, len(rules))
	for _, rule := range rules {
		refs = append(refs, rule.Ref())
	}
	sort.Strings(refs)
	return refs
}

func dereferenceRules(rules []*model.OverrideRule) []model.OverrideRule {
	values := make([]model.OverrideRule, 0, len(rules))
	for _, rule := range rules {
		values = append(values, *rule)
	}
	return values
}

func readBoundedFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if info.Size() > MaxSpecBytes {
		return "", fmt.Errorf("%s is %d bytes, exceeding the %d byte context limit", path, info.Size(), MaxSpecBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(raw), nil
}

// SuiteSchema is the JSON Schema the agent is required to satisfy. It is the
// generation-facing subset of docs/schemas/test-case.schema.json: the fields the
// agent decides, without the provenance the store fills in.
func SuiteSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"required":             []any{"cases"},
		"additionalProperties": false,
		"properties": map[string]any{
			"title":       map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"cases": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items": map[string]any{
					"type":                 "object",
					"required":             []any{"name", "requirement_ids", "overrides_applied", "scenario", "request", "expected"},
					"additionalProperties": false,
					"properties": map[string]any{
						"id":                map[string]any{"type": "string", "pattern": "^tc-[0-9]{3,}$"},
						"name":              map[string]any{"type": "string"},
						"requirement_ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string", "pattern": "^req-[0-9]{3,}$"}},
						"overrides_applied": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"scenario":          map[string]any{"type": "string", "enum": scenarioEnum()},
						"request": map[string]any{
							"type":                 "object",
							"required":             []any{"method", "path"},
							"additionalProperties": false,
							"properties": map[string]any{
								"method":          map[string]any{"type": "string", "enum": methodEnum()},
								"path":            map[string]any{"type": "string"},
								"query":           map[string]any{"type": "object"},
								"headers":         map[string]any{"type": "object"},
								"body":            map[string]any{"type": "string"},
								"body_media_type": map[string]any{"type": "string"},
							},
						},
						"expected": map[string]any{
							"type":                 "object",
							"required":             []any{"status"},
							"additionalProperties": false,
							"properties": map[string]any{
								"status":           map[string]any{"type": "integer", "minimum": 100, "maximum": 599},
								"empty_collection": map[string]any{"type": "boolean"},
								"body_fields":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								"max_latency_ms":   map[string]any{"type": "integer", "minimum": 1},
								"notes":            map[string]any{"type": "string"},
							},
						},
						"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
				},
			},
		},
	}
}

func scenarioEnum() []any {
	values := make([]any, 0, len(model.TestScenarios()))
	for _, scenario := range model.TestScenarios() {
		values = append(values, string(scenario))
	}
	return values
}

func methodEnum() []any {
	values := make([]any, 0, len(model.HTTPMethods()))
	for _, method := range model.HTTPMethods() {
		values = append(values, string(method))
	}
	return values
}

// encodeJSON renders a value for embedding in a prompt.
func encodeJSON(value any) string {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(payload)
}
