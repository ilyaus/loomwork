package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

func agentSpec() model.AgentDefinitionSpec {
	return model.AgentDefinitionSpec{
		AgentName:      "rest-api-test-generator",
		TargetProvider: model.AgentTargetClaudeSDK,
		Model:          "claude-sonnet-4-5",
		ToolsAllowed:   []string{"read_swagger", "read_requirements"},
		Body:           "# Role\n\nYou generate REST API test cases.",
	}
}

func ruleSpec() model.OverrideRuleSpec {
	return model.OverrideRuleSpec{
		ID:        "empty-list-on-missing",
		Title:     "Missing collections return an empty list",
		Condition: model.OverrideCondition{Methods: []model.HTTPMethod{model.MethodGET}, Scenario: model.ScenarioMissingItem},
		Action:    model.OverrideAction{Kind: model.OverrideActionExpectEmptyCollection},
		Rationale: "The storefront treats an absent order as an empty basket.",
	}
}

func TestAgentDefinitionVersionsAreDiscreteFilesBehindACurrentPointer(t *testing.T) {
	dirStore, project := seededStore(t)
	var _ AgentDefinitionStore = dirStore

	first, err := dirStore.CreateAgentDefinition(project.ID, agentSpec())
	if err != nil {
		t.Fatalf("CreateAgentDefinition: %v", err)
	}
	second, err := dirStore.UpdateAgentDefinition(project.ID, first.AgentName, model.AgentDefinitionSpec{
		Body: "# Role\n\nRevised instructions.",
	})
	if err != nil {
		t.Fatalf("UpdateAgentDefinition: %v", err)
	}
	if second.Version != 2 {
		t.Fatalf("Version = %d, want 2", second.Version)
	}

	root := filepath.Join(dirStore.ProjectDir(project.ID), AgentDefinitionsDirName)
	for _, name := range []string{"rest-api-test-generator.v1.md", "rest-api-test-generator.v2.md", "current.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s missing: %v", name, err)
		}
	}

	current, err := dirStore.LoadAgentDefinition(project.ID, first.AgentName, 0)
	if err != nil {
		t.Fatalf("LoadAgentDefinition(current): %v", err)
	}
	if current.Version != 2 {
		t.Errorf("current version = %d, want 2", current.Version)
	}
	earlier, err := dirStore.LoadAgentDefinition(project.ID, first.AgentName, 1)
	if err != nil {
		t.Fatalf("LoadAgentDefinition(v1): %v", err)
	}
	if earlier.Body != first.Body {
		t.Errorf("v1 body changed: %q", earlier.Body)
	}
	if earlier.Model != "claude-sonnet-4-5" || len(earlier.ToolsAllowed) != 2 {
		t.Errorf("v1 frontmatter lost: %+v", earlier)
	}

	history, err := dirStore.AgentDefinitionHistory(project.ID, first.AgentName)
	if err != nil {
		t.Fatalf("AgentDefinitionHistory: %v", err)
	}
	if len(history) != 2 || history[0].Version != 1 || history[1].Version != 2 {
		t.Errorf("history = %+v", history)
	}

	definitions, err := dirStore.ListAgentDefinitions(project.ID)
	if err != nil {
		t.Fatalf("ListAgentDefinitions: %v", err)
	}
	if len(definitions) != 1 || definitions[0].Version != 2 {
		t.Errorf("list returned %+v, want only the current version", definitions)
	}
}

func TestCreateAgentDefinitionRejectsADuplicateName(t *testing.T) {
	dirStore, project := seededStore(t)
	if _, err := dirStore.CreateAgentDefinition(project.ID, agentSpec()); err != nil {
		t.Fatalf("CreateAgentDefinition: %v", err)
	}
	if _, err := dirStore.CreateAgentDefinition(project.ID, agentSpec()); err == nil {
		t.Fatal("expected an error: a second version is published with UpdateAgentDefinition")
	}
}

func TestLoadAgentDefinitionReportsMissingEntities(t *testing.T) {
	dirStore, project := seededStore(t)
	if _, err := dirStore.LoadAgentDefinition(project.ID, "absent-agent", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := dirStore.CreateAgentDefinition(project.ID, agentSpec()); err != nil {
		t.Fatalf("CreateAgentDefinition: %v", err)
	}
	if _, err := dirStore.LoadAgentDefinition(project.ID, "rest-api-test-generator", 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestOverrideRuleVersioningSupersedesTheEarlierVersion(t *testing.T) {
	dirStore, project := seededStore(t)

	first, err := dirStore.CreateOverrideRule(project.ID, ruleSpec())
	if err != nil {
		t.Fatalf("CreateOverrideRule: %v", err)
	}
	if first.Status != model.OverrideRuleStatusActive {
		t.Fatalf("Status = %q, want active", first.Status)
	}
	second, err := dirStore.UpdateOverrideRule(project.ID, first.ID, model.OverrideRuleSpec{
		Rationale: "Checkout was rewritten; the empty basket is still the contract QA asserts.",
	})
	if err != nil {
		t.Fatalf("UpdateOverrideRule: %v", err)
	}
	if second.Version != 2 {
		t.Errorf("Version = %d, want 2", second.Version)
	}

	retained, err := dirStore.LoadOverrideRule(project.ID, first.ID, 1)
	if err != nil {
		t.Fatalf("LoadOverrideRule(v1): %v", err)
	}
	if retained.Status != model.OverrideRuleStatusSuperseded {
		t.Errorf("v1 status = %q, want superseded", retained.Status)
	}
	if retained.Rationale != first.Rationale {
		t.Errorf("v1 rationale changed: %q", retained.Rationale)
	}

	active, err := dirStore.ActiveOverrideRules(project.ID)
	if err != nil {
		t.Fatalf("ActiveOverrideRules: %v", err)
	}
	if len(active) != 1 || active[0].Ref() != second.Ref() {
		t.Errorf("active rules = %+v, want only %s", active, second.Ref())
	}

	all, err := dirStore.ListOverrideRules(project.ID)
	if err != nil {
		t.Fatalf("ListOverrideRules: %v", err)
	}
	if len(all) != 1 || all[0].Version != 2 {
		t.Errorf("list returned %+v, want only the current version", all)
	}

	history, err := dirStore.OverrideRuleHistory(project.ID, first.ID)
	if err != nil {
		t.Fatalf("OverrideRuleHistory: %v", err)
	}
	if len(history) != 2 {
		t.Errorf("history = %+v", history)
	}
}

func TestSetOverrideRuleStatusRemovesARuleFromGeneration(t *testing.T) {
	dirStore, project := seededStore(t)
	rule, err := dirStore.CreateOverrideRule(project.ID, ruleSpec())
	if err != nil {
		t.Fatalf("CreateOverrideRule: %v", err)
	}
	if _, err := dirStore.SetOverrideRuleStatus(project.ID, rule.ID, 0, model.OverrideRuleStatusObsolete); err != nil {
		t.Fatalf("SetOverrideRuleStatus: %v", err)
	}
	active, err := dirStore.ActiveOverrideRules(project.ID)
	if err != nil {
		t.Fatalf("ActiveOverrideRules: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("active rules = %+v, want none", active)
	}
}
