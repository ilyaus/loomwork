package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/orchestrator"
	"github.com/ilyaus/loomwork/internal/provider"
	"github.com/ilyaus/loomwork/internal/store"
)

type fakeDesktopService struct {
	models   []orchestrator.ProviderModels
	response orchestrator.ChatResponse
	request  orchestrator.ChatRequest
}

func (f *fakeDesktopService) ListModels(context.Context) []orchestrator.ProviderModels {
	return f.models
}

func (f *fakeDesktopService) Chat(_ context.Context, request orchestrator.ChatRequest) (orchestrator.ChatResponse, error) {
	f.request = request
	return f.response, nil
}

func TestProjectItemsListAndReadViewerDocuments(t *testing.T) {
	home := t.TempDir()
	projects, err := store.NewDirStore(filepath.Join(home, "projects"))
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	project, err := model.NewProject("checkout", "Checkout API", nil)
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	artifact, err := project.AddArtifact(model.ArtifactSpec{
		Name: "openapi.json", Type: model.ArtifactTypeSpec,
		Body: model.Body{Content: `{"openapi":"3.1.0","paths":{}}`, MediaType: "application/json"},
	})
	if err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	if err := projects.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := projects.CreateRequirement(project.ID, model.RequirementSpec{Text: "Cart totals include tax"}); err != nil {
		t.Fatalf("CreateRequirement: %v", err)
	}
	if _, err := projects.CreateAgentDefinition(project.ID, model.AgentDefinitionSpec{
		AgentName: "rest-api-test-generator", TargetProvider: model.AgentTargetClaudeSDK,
		Body: "# Role\n\nGenerate REST API tests.",
	}); err != nil {
		t.Fatalf("CreateAgentDefinition: %v", err)
	}
	if _, err := projects.CreateOverrideRule(project.ID, model.OverrideRuleSpec{
		ID: "missing-is-empty", Title: "Missing collections are empty",
		Condition: model.OverrideCondition{Methods: []model.HTTPMethod{model.MethodGET}},
		Action:    model.OverrideAction{Kind: model.OverrideActionExpectEmptyCollection},
		Rationale: "The storefront represents missing collections as empty.",
	}); err != nil {
		t.Fatalf("CreateOverrideRule: %v", err)
	}
	if _, err := projects.SaveTestSuite(project.ID, &model.TestSuite{
		SuiteID: "suite-checkout", Title: "Checkout API", Origin: model.TestSuiteOriginGenerated,
		Cases: []model.TestCase{{
			Name: "gets the cart", RequirementIDs: []string{"req-001"},
			Scenario: model.ScenarioHappyPath,
			Request:  model.TestRequest{Method: model.MethodGET, Path: "/cart"},
			Expected: model.TestExpectation{Status: 200},
		}},
	}); err != nil {
		t.Fatalf("SaveTestSuite: %v", err)
	}
	reportDir := filepath.Join(projects.ProjectDir(project.ID), store.ReportsDirName, "run-001")
	if err := os.MkdirAll(reportDir, 0o700); err != nil {
		t.Fatalf("create report directory: %v", err)
	}
	reportPath := filepath.Join(reportDir, "latest.json")
	if err := os.WriteFile(reportPath, []byte(`{"passed":1,"failed":0}`), 0o600); err != nil {
		t.Fatalf("write report: %v", err)
	}

	server, err := New(Options{Store: projects, Home: home})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	handler := server.Handler()
	var tree TreeResponse
	mustCall(t, handler, http.MethodGet, "/api/projects/"+project.ID+"/items", nil, &tree, http.StatusOK)
	if len(tree.Groups) != 6 {
		t.Fatalf("groups = %d, want six entity families", len(tree.Groups))
	}
	counts := map[string]int{}
	for _, group := range tree.Groups {
		counts[group.Family] = len(group.Items)
	}
	for _, family := range []string{
		familyRequirements, familyAgentDefinitions, familyOverrideRules,
		familyTestSuites, familyReports, familyArtifacts,
	} {
		if counts[family] != 1 {
			t.Errorf("%s items = %d, want one", family, counts[family])
		}
	}
	if len(tree.Groups[3].Items[0].Children) != 1 {
		t.Fatalf("suite children = %+v, want one test case", tree.Groups[3].Items[0].Children)
	}
	if tree.Groups[3].Items[0].Children[0].Name != "gets the cart" {
		t.Fatalf("test case name = %q, want gets the cart", tree.Groups[3].Items[0].Children[0].Name)
	}
	if tree.Groups[3].Items[0].Status != "ready" {
		t.Fatalf("suite status = %q, want ready", tree.Groups[3].Items[0].Status)
	}

	var document ViewerDocument
	mustCall(t, handler, http.MethodGet,
		"/api/projects/"+project.ID+"/items/artifacts/"+artifact.ID, nil, &document, http.StatusOK)
	var content string
	if err := json.Unmarshal(document.Body, &content); err != nil {
		t.Fatalf("decode artifact body: %v", err)
	}
	if document.ArtifactType != "spec" || !strings.Contains(content, `"openapi"`) {
		t.Fatalf("artifact document = %+v body %q", document, content)
	}

	caseRef := tree.Groups[3].Items[0].Children[0].Ref
	mustCall(t, handler, http.MethodGet,
		"/api/projects/"+project.ID+"/items/test-cases/"+caseRef, nil, &document, http.StatusOK)
	if document.ArtifactType != "test-case" || !strings.Contains(string(document.Body), `"requirement_ids":["req-001"]`) {
		t.Fatalf("test-case document = %+v", document)
	}

	mustCall(t, handler, http.MethodGet,
		"/api/projects/"+project.ID+"/items/reports/run-001%2Flatest.json", nil, &document, http.StatusOK)
	if document.ArtifactType != "test-report" {
		t.Fatalf("report document = %+v", document)
	}
}

func TestModelDiscoveryAndChatSSEEndpoints(t *testing.T) {
	home := t.TempDir()
	projects, err := store.NewDirStore(filepath.Join(home, "projects"))
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	project, err := model.NewProject("checkout", "", nil)
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	if err := projects.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	desktop := &fakeDesktopService{
		models: []orchestrator.ProviderModels{{
			Provider: "ollama", Kind: provider.KindOllama, Status: "available",
			Models: []orchestrator.ModelChoice{{
				ID: "qwen3:8b", Selector: "ollama/qwen3:8b", Presets: []string{"review"},
			}},
		}},
		response: orchestrator.ChatResponse{
			Text: "The checkout requirement is covered.", Model: "qwen3:8b",
			FinishReason: "stop", Usage: provider.Usage{CompletionTokens: 6},
		},
	}
	server, err := New(Options{Store: projects, Home: home, Desktop: desktop})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	handler := server.Handler()

	var models []orchestrator.ProviderModels
	mustCall(t, handler, http.MethodGet, "/api/models", nil, &models, http.StatusOK)
	if len(models) != 1 || models[0].Models[0].Selector != "ollama/qwen3:8b" {
		t.Fatalf("models = %+v", models)
	}

	recorder := call(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/chat", map[string]any{
		"selector":     "ollama/qwen3:8b#review",
		"messages":     []map[string]string{{"role": "user", "content": "Is checkout covered?"}},
		"artifactRefs": []string{"spec"},
	}, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST chat = %d (%s)", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want SSE", contentType)
	}
	stream := recorder.Body.String()
	for _, event := range []string{"event: ready", "event: message", "event: done"} {
		if !strings.Contains(stream, event) {
			t.Errorf("stream = %q, want %q", stream, event)
		}
	}
	if desktop.request.ProjectRef != project.ID || desktop.request.Selector != "ollama/qwen3:8b#review" {
		t.Errorf("chat request = %+v", desktop.request)
	}

	missing := call(t, handler, http.MethodPost, "/api/projects/missing/chat", map[string]any{
		"selector": "ollama/qwen3:8b",
		"messages": []map[string]string{{"role": "user", "content": "Hello"}},
	}, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("POST missing project chat = %d, want 404", missing.Code)
	}
}
