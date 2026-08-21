package testgen

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/provider"
	"github.com/ilyaus/loomwork/internal/store"
)

// fixture is a project with one agent definition, two requirements, and one
// active override rule: the inputs a generation run is defined to receive.
type fixture struct {
	store     *store.DirStore
	projectID string
	specPath  string
	rule      *model.OverrideRule
}

func newFixture(t *testing.T, tools []string) fixture {
	t.Helper()
	dirStore, err := store.NewDirStore(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	project, err := model.NewProject("orders", "", nil)
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	if err := dirStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := dirStore.CreateAgentDefinition(project.ID, model.AgentDefinitionSpec{
		AgentName:      "rest-api-test-generator",
		TargetProvider: model.AgentTargetClaudeSDK,
		Model:          "claude-sonnet-4-5",
		ToolsAllowed:   tools,
		Body:           "# Role\n\nYou generate REST API test cases.",
	}); err != nil {
		t.Fatalf("CreateAgentDefinition: %v", err)
	}
	for _, text := range []string{"Listing the items of an order returns them.", "A missing order lists no items."} {
		if _, err := dirStore.CreateRequirement(project.ID, model.RequirementSpec{Text: text}); err != nil {
			t.Fatalf("CreateRequirement: %v", err)
		}
	}
	rule, err := dirStore.CreateOverrideRule(project.ID, model.OverrideRuleSpec{
		ID:    "empty-list-on-missing",
		Title: "Missing collections return an empty list",
		Condition: model.OverrideCondition{
			Methods:    []model.HTTPMethod{model.MethodGET},
			Scenario:   model.ScenarioMissingItem,
			SpecStatus: 404,
		},
		Action:    model.OverrideAction{Kind: model.OverrideActionExpectEmptyCollection},
		Rationale: "The storefront treats an absent order as an empty basket.",
	})
	if err != nil {
		t.Fatalf("CreateOverrideRule: %v", err)
	}

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(specPath, []byte(`{"openapi":"3.1.0","paths":{"/orders/{id}/items":{"get":{}}}}`), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return fixture{store: dirStore, projectID: project.ID, specPath: specPath, rule: rule}
}

func (f fixture) generate(t *testing.T, adapter provider.AgentAdapter, request GenerateRequest) (Result, error) {
	t.Helper()
	generator := New(f.store, func(model.AgentTarget) (provider.AgentAdapter, error) { return adapter, nil })
	if request.ProjectRef == "" {
		request.ProjectRef = f.projectID
	}
	if request.SuiteID == "" {
		request.SuiteID = "suite-orders-api"
	}
	if request.AgentName == "" {
		request.AgentName = "rest-api-test-generator"
	}
	if request.SpecPath == "" {
		request.SpecPath = f.specPath
	}
	return generator.Generate(context.Background(), request)
}

func agentReply(cases string) string {
	return "Here is the suite:\n```json\n{\"title\":\"Orders API\",\"cases\":[" + cases + "]}\n```"
}

const linkedCase = `{"name":"lists the items of an order","requirement_ids":["req-001"],"overrides_applied":[],` +
	`"scenario":"happy-path","request":{"method":"GET","path":"/orders/42/items"},"expected":{"status":200}}`

const overriddenCase = `{"name":"a missing order lists no items","requirement_ids":["req-002"],"overrides_applied":[],` +
	`"scenario":"missing-item","request":{"method":"GET","path":"/orders/999/items"},` +
	`"expected":{"status":200,"empty_collection":true}}`

const unlinkedCase = `{"name":"rejects a malformed order id","requirement_ids":[],"overrides_applied":[],` +
	`"scenario":"invalid-input","request":{"method":"GET","path":"/orders/x/items"},"expected":{"status":400}}`

func TestGenerateStoresAVersionedSuiteWithProvenance(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	adapter := provider.NewStubAgentAdapter(provider.StubTurn{Text: agentReply(linkedCase + "," + overriddenCase)})

	result, err := fixture.generate(t, adapter, GenerateRequest{Tags: []string{"phase3"}})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	suite := result.Suite
	if suite.Version != 1 || suite.Origin != model.TestSuiteOriginGenerated {
		t.Errorf("suite = v%d %s", suite.Version, suite.Origin)
	}
	if suite.AgentDefinition != "rest-api-test-generator-v1" {
		t.Errorf("AgentDefinition = %q", suite.AgentDefinition)
	}
	if suite.SpecRef != fixture.specPath {
		t.Errorf("SpecRef = %q", suite.SpecRef)
	}
	specBytes, err := os.ReadFile(fixture.specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if want := fmt.Sprintf("%x", sha256.Sum256(specBytes)); suite.Metadata["spec_sha256"] != want {
		t.Errorf("spec_sha256 = %q, want %q", suite.Metadata["spec_sha256"], want)
	}
	if got := suite.RequirementVersions["req-001"]; got != 1 {
		t.Errorf("RequirementVersions = %v", suite.RequirementVersions)
	}
	if len(suite.OverrideRules) != 1 || suite.OverrideRules[0] != fixture.rule.Ref() {
		t.Errorf("OverrideRules = %v", suite.OverrideRules)
	}
	if suite.Incomplete {
		t.Errorf("suite is incomplete: %v", suite.IncompleteReasons)
	}
	if result.Adapter != "stub" || result.Model != "claude-sonnet-4-5" {
		t.Errorf("adapter = %q, model = %q", result.Adapter, result.Model)
	}

	stored, err := fixture.store.LoadTestSuite(fixture.projectID, "suite-orders-api", 0)
	if err != nil {
		t.Fatalf("LoadTestSuite: %v", err)
	}
	if len(stored.Cases) != 2 {
		t.Fatalf("stored %d case(s)", len(stored.Cases))
	}

	second, err := fixture.generate(t, provider.NewStubAgentAdapter(provider.StubTurn{Text: agentReply(linkedCase)}), GenerateRequest{})
	if err != nil {
		t.Fatalf("Generate(second): %v", err)
	}
	if second.Suite.Version != 2 {
		t.Errorf("second version = %d, want 2", second.Suite.Version)
	}
	history, err := fixture.store.TestSuiteHistory(fixture.projectID, "suite-orders-api")
	if err != nil {
		t.Fatalf("TestSuiteHistory: %v", err)
	}
	if len(history) != 2 || len(history[0].CaseIDs) != 2 {
		t.Errorf("history = %+v: the earlier version must be retained intact", history)
	}
}

func TestGenerateAnnotatesFollowedOverrideRules(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	adapter := provider.NewStubAgentAdapter(provider.StubTurn{Text: agentReply(overriddenCase)})

	result, err := fixture.generate(t, adapter, GenerateRequest{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := result.Suite.Cases[0].OverridesApplied; len(got) != 1 || got[0] != fixture.rule.Ref() {
		t.Errorf("overrides_applied = %v, want [%s]", got, fixture.rule.Ref())
	}
	if len(result.Audit.Annotated()) != 1 {
		t.Errorf("Annotated = %+v", result.Audit.Annotated())
	}

	stored, err := fixture.store.LoadTestSuite(fixture.projectID, "suite-orders-api", 0)
	if err != nil {
		t.Fatalf("LoadTestSuite: %v", err)
	}
	if got := stored.Cases[0].OverridesApplied; len(got) != 1 || got[0] != fixture.rule.Ref() {
		t.Errorf("stored overrides_applied = %v", got)
	}
}

func TestGenerateFlagsAViolatedOverrideRule(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	literal := strings.Replace(overriddenCase, `"expected":{"status":200,"empty_collection":true}`, `"expected":{"status":404}`, 1)
	adapter := provider.NewStubAgentAdapter(provider.StubTurn{Text: agentReply(literal)})

	result, err := fixture.generate(t, adapter, GenerateRequest{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !result.Suite.Incomplete {
		t.Fatal("a case contradicting an active override rule must flag the suite incomplete")
	}
	problems := result.Audit.Problems()
	if len(problems) != 1 || problems[0].Kind != model.OverrideFindingViolated {
		t.Errorf("Problems = %+v", problems)
	}
}

func TestGenerateFlagsUnlinkedCasesIncompleteButStillStoresTheSuite(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	adapter := provider.NewStubAgentAdapter(provider.StubTurn{Text: agentReply(linkedCase + "," + unlinkedCase)})

	result, err := fixture.generate(t, adapter, GenerateRequest{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !result.Suite.Incomplete {
		t.Fatal("a suite with an unlinked case must be flagged incomplete")
	}
	if len(result.Audit.UnlinkedCases) != 1 || result.Audit.UnlinkedCases[0] != "tc-002" {
		t.Errorf("UnlinkedCases = %v", result.Audit.UnlinkedCases)
	}
	stored, err := fixture.store.LoadTestSuite(fixture.projectID, "suite-orders-api", 0)
	if err != nil {
		t.Fatalf("LoadTestSuite: %v", err)
	}
	if !stored.Incomplete || len(stored.Cases) != 2 {
		t.Errorf("stored suite = incomplete %t with %d case(s)", stored.Incomplete, len(stored.Cases))
	}
}

func TestGenerateFlagsALinkToAnUnknownRequirement(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	invented := strings.Replace(linkedCase, `"requirement_ids":["req-001"]`, `"requirement_ids":["req-404"]`, 1)
	adapter := provider.NewStubAgentAdapter(provider.StubTurn{Text: agentReply(invented)})

	result, err := fixture.generate(t, adapter, GenerateRequest{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !result.Suite.Incomplete {
		t.Fatal("a dangling requirement link must flag the suite incomplete")
	}
	if !strings.Contains(strings.Join(result.Suite.IncompleteReasons, " "), "req-404") {
		t.Errorf("IncompleteReasons = %v", result.Suite.IncompleteReasons)
	}
}

func TestGeneratePassesEveryInputToTheAgent(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	templatePath := filepath.Join(t.TempDir(), "template.json")
	if err := os.WriteFile(templatePath, []byte(`{"template":"rest-case"}`), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	adapter := provider.NewStubAgentAdapter(provider.StubTurn{
		ToolCalls: []provider.StubToolCall{
			{Name: ToolReadSwagger},
			{Name: ToolReadRequirements},
			{Name: ToolReadOverrideRules},
			{Name: ToolReadTemplates},
		},
		Text: agentReply(linkedCase),
	})

	if _, err := fixture.generate(t, adapter, GenerateRequest{
		TemplatePaths: []string{templatePath},
		Instructions:  "Cover pagination too.",
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	sessions := adapter.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("opened %d session(s)", len(sessions))
	}
	spec := sessions[0].Spec()
	if !strings.Contains(spec.SystemPrompt, "You generate REST API test cases") {
		t.Errorf("the agent definition body must be the system prompt: %q", spec.SystemPrompt)
	}
	if len(spec.Tools) != len(GenerationTools()) {
		t.Errorf("registered %d tool(s), want %d", len(spec.Tools), len(GenerationTools()))
	}
	prompts := sessions[0].Prompts()
	if len(prompts) != 1 {
		t.Fatalf("sent %d prompt(s)", len(prompts))
	}
	for _, want := range []string{
		"openapi",                         // the spec
		"Listing the items of an order",   // current-version requirements
		"empty-list-on-missing-v1",        // the exact override reference to cite
		"absent order as an empty basket", // the rule's free-text rationale
		"rest-case",                       // the templates
		"Cover pagination too.",           // per-run instructions
	} {
		if !strings.Contains(prompts[0].Prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	if prompts[0].Structured == nil {
		t.Fatal("generation must request structured output")
	}
	if _, err := json.Marshal(prompts[0].Structured.Schema); err != nil {
		t.Errorf("structured output schema is not encodable: %v", err)
	}
}

func TestGenerateHonorsTheToolAllowlist(t *testing.T) {
	fixture := newFixture(t, []string{ToolReadSwagger})
	adapter := provider.NewStubAgentAdapter(provider.StubTurn{Text: agentReply(linkedCase)})

	if _, err := fixture.generate(t, adapter, GenerateRequest{}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	spec := adapter.Sessions()[0].Spec()
	if len(spec.Tools) != 1 || spec.Tools[0].Name != ToolReadSwagger {
		t.Errorf("tools = %+v, want only %s", spec.Tools, ToolReadSwagger)
	}
}

func TestGenerateRequiresRequirementsAndASpec(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	adapter := provider.NewStubAgentAdapter(provider.StubTurn{Text: agentReply(linkedCase)})

	if _, err := fixture.generate(t, adapter, GenerateRequest{SpecPath: filepath.Join(t.TempDir(), "absent.json")}); err == nil {
		t.Error("expected an error for a missing spec file")
	}

	empty, err := store.NewDirStore(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	project, err := model.NewProject("bare", "", nil)
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	if err := empty.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := empty.CreateAgentDefinition(project.ID, model.AgentDefinitionSpec{
		AgentName:      "rest-api-test-generator",
		TargetProvider: model.AgentTargetClaudeSDK,
		Body:           "# Role",
	}); err != nil {
		t.Fatalf("CreateAgentDefinition: %v", err)
	}
	generator := New(empty, func(model.AgentTarget) (provider.AgentAdapter, error) { return adapter, nil })
	_, err = generator.Generate(context.Background(), GenerateRequest{
		ProjectRef: project.ID,
		SuiteID:    "suite-orders-api",
		AgentName:  "rest-api-test-generator",
		SpecPath:   fixture.specPath,
	})
	if err == nil || !strings.Contains(err.Error(), "requirement") {
		t.Errorf("err = %v, want a missing-requirements error", err)
	}
}

func TestGenerateRejectsANonSuiteAgentResponse(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	adapter := provider.NewStubAgentAdapter(provider.StubTurn{Text: "I could not produce a suite."})
	if _, err := fixture.generate(t, adapter, GenerateRequest{}); err == nil {
		t.Fatal("expected an error for a response that is not a suite")
	}
	if _, err := fixture.store.ListTestSuites(fixture.projectID); err != nil {
		t.Fatalf("ListTestSuites: %v", err)
	}
	suites, _ := fixture.store.ListTestSuites(fixture.projectID)
	if len(suites) != 0 {
		t.Errorf("a failed generation stored %d suite(s)", len(suites))
	}
}

func TestImportStoresAnExternalSuiteInTheSameVersionedStore(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	payload := []byte(`{"suite_id":"suite-legacy","cases":[` + linkedCase + `,` + overriddenCase + `]}`)

	generator := New(fixture.store, nil)
	result, err := generator.Import(ImportRequest{
		ProjectRef: fixture.projectID,
		Payload:    payload,
		SourcePath: "/tmp/legacy-suite.json",
		Title:      "Legacy suite",
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	suite := result.Suite
	if suite.Origin != model.TestSuiteOriginImported || suite.Version != 1 {
		t.Errorf("suite = v%d %s", suite.Version, suite.Origin)
	}
	if suite.Metadata["imported_from"] != "/tmp/legacy-suite.json" {
		t.Errorf("Metadata = %v", suite.Metadata)
	}
	if suite.Incomplete {
		t.Errorf("suite is incomplete: %v", suite.IncompleteReasons)
	}
	if got := suite.Cases[1].OverridesApplied; len(got) != 1 || got[0] != fixture.rule.Ref() {
		t.Errorf("an imported case following a rule must be annotated too: %v", got)
	}
	if _, err := fixture.store.LoadTestSuite(fixture.projectID, "suite-legacy", 1); err != nil {
		t.Fatalf("LoadTestSuite: %v", err)
	}
}

func TestImportFlagsUnlinkedCasesIncomplete(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	payload := []byte(`{"suite_id":"suite-legacy","cases":[` + unlinkedCase + `]}`)

	generator := New(fixture.store, nil)
	result, err := generator.Import(ImportRequest{ProjectRef: fixture.projectID, Payload: payload})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !result.Suite.Incomplete {
		t.Fatal("an imported suite with an unlinked case must be flagged incomplete, not trusted")
	}
}

func TestImportRejectsAMalformedPayload(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	generator := New(fixture.store, nil)
	for name, payload := range map[string]string{
		"not json":      "{",
		"unknown field": `{"suite_id":"suite-legacy","cases":[],"typo":1}`,
		"bad suite id":  `{"suite_id":"Suite Legacy","cases":[]}`,
	} {
		if _, err := generator.Import(ImportRequest{ProjectRef: fixture.projectID, Payload: []byte(payload)}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestImportFlagsACaselessSuiteIncomplete(t *testing.T) {
	fixture := newFixture(t, GenerationTools())
	generator := New(fixture.store, nil)

	result, err := generator.Import(ImportRequest{
		ProjectRef: fixture.projectID,
		Payload:    []byte(`{"suite_id":"suite-legacy","cases":[]}`),
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !result.Suite.Incomplete || len(result.Suite.IncompleteReasons) == 0 {
		t.Error("a suite with no cases must be stored and flagged, not silently rejected")
	}
}

func TestBuildAdapterRejectsATargetWithoutABackend(t *testing.T) {
	if _, err := BuildAdapter(model.AgentTargetClaudeSDK); err != nil {
		t.Fatalf("BuildAdapter(claude): %v", err)
	}
	if _, err := BuildAdapter(model.AgentTargetCopilotSDK); err == nil {
		t.Error("expected an error: no Copilot backend is implemented yet")
	}
	if _, err := BuildAdapter("gemini-sdk"); err == nil {
		t.Error("expected an error for an unknown target")
	}
}
