package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/store"
	"github.com/ilyaus/loomwork/internal/testgen"
)

// suiteDocument is a minimal importable suite linking one case to req-001 and
// leaving a second case unlinked, so the import is stored and flagged.
func suiteDocument(suiteID string) map[string]any {
	return map[string]any{
		"suite_id": suiteID,
		"title":    "Orders",
		"origin":   "imported",
		"cases": []map[string]any{
			{
				"name": "gets an order", "requirement_ids": []string{"req-001"}, "overrides_applied": []string{},
				"scenario": "happy-path", "request": map[string]any{"method": "GET", "path": "/orders/1"},
				"expected": map[string]any{"status": 200},
			},
			{
				"name": "creates an order", "requirement_ids": []string{}, "overrides_applied": []string{},
				"scenario": "happy-path", "request": map[string]any{"method": "POST", "path": "/orders"},
				"expected": map[string]any{"status": 201},
			},
		},
	}
}

func TestArtifactsAreAddedAndVersionedOverHTTP(t *testing.T) {
	handler, project, _ := seededServer(t)
	base := "/api/projects/" + project + "/artifacts"

	var first model.Artifact
	mustCall(t, handler, http.MethodPost, base, map[string]any{
		"name": "notes.md", "type": "doc", "content": "# Notes", "tags": []string{"b", "a"}, "pinned": true,
	}, &first, http.StatusCreated)
	if first.Version != 1 || !first.Pinned || strings.Join(first.Tags, ",") != "a,b" {
		t.Fatalf("first revision = %+v", first)
	}

	var second model.Artifact
	mustCall(t, handler, http.MethodPost, base, map[string]any{
		"name": "notes.md", "type": "doc", "content": "# Notes v2",
	}, &second, http.StatusCreated)
	if second.Version != 2 || second.ParentID != first.ID {
		t.Fatalf("second revision = %+v, want v2 derived from %s", second, first.ID)
	}

	var latest []model.Artifact
	mustCall(t, handler, http.MethodGet, base, nil, &latest, http.StatusOK)
	if len(latest) != 1 || latest[0].ID != second.ID {
		t.Fatalf("latest artifacts = %+v, want only the newest revision", latest)
	}

	if message := errorText(t, handler, http.MethodPost, base, map[string]any{"name": "x", "type": "doc"}, http.StatusBadRequest); !strings.Contains(message, "content") {
		t.Errorf("empty body error = %q, want it to name the missing content", message)
	}
	if message := errorText(t, handler, http.MethodPost, base, map[string]any{"name": "x", "type": "video", "content": "c"}, http.StatusBadRequest); !strings.Contains(message, "unknown artifact type") {
		t.Errorf("bad type error = %q", message)
	}
	if message := errorText(t, handler, http.MethodPost, base, map[string]any{"name": "notes.md", "type": "spec", "content": "c"}, http.StatusBadRequest); !strings.Contains(message, "already exists with type") {
		t.Errorf("type change error = %q", message)
	}
}

func TestTestSuiteImportStoresAndFlagsIncompleteSuites(t *testing.T) {
	handler, project, _ := seededServer(t)
	base := "/api/projects/" + project + "/test-suites"

	var result testgen.Result
	mustCall(t, handler, http.MethodPost, base, suiteDocument("suite-orders"), &result, http.StatusCreated)
	if result.Suite == nil || result.Suite.Version != 1 || !result.Suite.Incomplete {
		t.Fatalf("import result = %+v, want an incomplete v1 suite", result.Suite)
	}
	if len(result.Audit.UnlinkedCases) != 1 || result.Audit.UnlinkedCases[0] != "tc-002" {
		t.Fatalf("unlinked cases = %v, want tc-002", result.Audit.UnlinkedCases)
	}

	mustCall(t, handler, http.MethodPost, base, suiteDocument("suite-orders"), &result, http.StatusCreated)
	if result.Suite.Version != 2 {
		t.Fatalf("re-import version = %d, want 2", result.Suite.Version)
	}

	var suites []model.TestSuite
	mustCall(t, handler, http.MethodGet, base, nil, &suites, http.StatusOK)
	if len(suites) != 1 || suites[0].Version != 2 {
		t.Fatalf("suites = %+v, want one suite at v2", suites)
	}

	var history []ItemVersion
	mustCall(t, handler, http.MethodGet, "/api/projects/"+project+"/items/test-suites/suite-orders/history", nil, &history, http.StatusOK)
	if len(history) != 2 || history[0].Version != 2 || history[1].Version != 1 || history[0].Status != "incomplete" {
		t.Fatalf("history = %+v, want v2 then v1 flagged incomplete", history)
	}

	if message := errorText(t, handler, http.MethodPost, base, nil, http.StatusBadRequest); !strings.Contains(message, "required") {
		t.Errorf("empty import error = %q", message)
	}
	if message := errorText(t, handler, http.MethodPost, base, map[string]any{"suite_id": "bad", "tests": []any{}}, http.StatusBadRequest); !strings.Contains(message, "unknown field") {
		t.Errorf("unknown field error = %q, want the decoder to reject it", message)
	}
}

func TestAgentDefinitionsAndOverrideRulesOverHTTP(t *testing.T) {
	handler, project, _ := seededServer(t)
	agents := "/api/projects/" + project + "/agent-definitions"
	rules := "/api/projects/" + project + "/override-rules"

	var definition model.AgentDefinition
	mustCall(t, handler, http.MethodPost, agents, map[string]any{
		"agent_name": "rest-api-test-generator", "body": "# Role\nGenerate tests.",
		"tools_allowed": []string{"read_swagger"},
	}, &definition, http.StatusCreated)
	if definition.Version != 1 || definition.TargetProvider != model.AgentTargetClaudeSDK {
		t.Fatalf("definition = %+v, want v1 defaulting to the Claude target", definition)
	}
	mustCall(t, handler, http.MethodPatch, agents+"/rest-api-test-generator", map[string]any{
		"model": "claude-sonnet-4",
	}, &definition, http.StatusOK)
	if definition.Version != 2 || definition.Model != "claude-sonnet-4" || definition.Body != "# Role\nGenerate tests." {
		t.Fatalf("updated definition = %+v, want v2 inheriting the body", definition)
	}
	var definitions []model.AgentDefinition
	mustCall(t, handler, http.MethodGet, agents, nil, &definitions, http.StatusOK)
	if len(definitions) != 1 || definitions[0].Version != 2 {
		t.Fatalf("definitions = %+v", definitions)
	}
	var history []ItemVersion
	mustCall(t, handler, http.MethodGet, "/api/projects/"+project+"/items/agent-definitions/rest-api-test-generator/history", nil, &history, http.StatusOK)
	if len(history) != 2 || history[0].Version != 2 {
		t.Fatalf("definition history = %+v, want newest first", history)
	}
	if message := errorText(t, handler, http.MethodPost, agents, map[string]any{"agent_name": "Bad Name", "body": "x"}, http.StatusBadRequest); !strings.Contains(message, "lowercase") {
		t.Errorf("bad name error = %q", message)
	}
	if code := call(t, handler, http.MethodPatch, agents+"/missing", map[string]any{"body": "x"}, nil).Code; code != http.StatusNotFound {
		t.Errorf("PATCH missing definition = %d, want 404", code)
	}

	var rule model.OverrideRule
	mustCall(t, handler, http.MethodPost, rules, map[string]any{
		"id": "missing-is-empty", "title": "Missing collections are empty",
		"condition": map[string]any{"methods": []string{"GET"}, "scenario": "missing-item"},
		"action":    map[string]any{"kind": "expect-empty-collection"},
		"rationale": "The storefront returns an empty list.",
	}, &rule, http.StatusCreated)
	if rule.Version != 1 || rule.Status != model.OverrideRuleStatusActive || rule.Action.ExpectStatus != 200 {
		t.Fatalf("rule = %+v, want an active v1 defaulting to 200", rule)
	}
	mustCall(t, handler, http.MethodPatch, rules+"/missing-is-empty", map[string]any{
		"rationale": "Clients never special-case 404.",
	}, &rule, http.StatusOK)
	if rule.Version != 2 || rule.Title != "Missing collections are empty" {
		t.Fatalf("updated rule = %+v, want v2 inheriting the title", rule)
	}
	mustCall(t, handler, http.MethodPost, rules+"/missing-is-empty/status", map[string]any{"status": "obsolete"}, &rule, http.StatusOK)
	if rule.Status != model.OverrideRuleStatusObsolete || rule.Version != 2 {
		t.Fatalf("status change = %+v, want v2 obsolete", rule)
	}
	mustCall(t, handler, http.MethodGet, "/api/projects/"+project+"/items/override-rules/missing-is-empty/history", nil, &history, http.StatusOK)
	if len(history) != 2 || history[0].Status != "obsolete" || history[1].Status != "superseded" {
		t.Fatalf("rule history = %+v, want v2 obsolete above v1 superseded", history)
	}
	if message := errorText(t, handler, http.MethodPost, rules+"/missing-is-empty/status", map[string]any{"status": "superseded"}, http.StatusBadRequest); !strings.Contains(message, "superseded") {
		t.Errorf("superseded status error = %q", message)
	}
	if message := errorText(t, handler, http.MethodPost, rules, map[string]any{"id": "no-why", "title": "t", "action": map[string]any{"kind": "skip-test"}}, http.StatusBadRequest); !strings.Contains(message, "rationale") {
		t.Errorf("missing rationale error = %q", message)
	}

	var rulesList []model.OverrideRule
	mustCall(t, handler, http.MethodGet, rules, nil, &rulesList, http.StatusOK)
	if len(rulesList) != 1 {
		t.Fatalf("rules = %+v", rulesList)
	}
}

func TestItemHistoryRejectsUnversionedFamilies(t *testing.T) {
	handler, project, requirementID := seededServer(t)
	mustCall(t, handler, http.MethodPatch, "/api/projects/"+project+"/requirements/"+requirementID, map[string]any{"text": "v2"}, nil, http.StatusOK)

	var history []ItemVersion
	mustCall(t, handler, http.MethodGet, "/api/projects/"+project+"/items/requirements/"+requirementID+"/history", nil, &history, http.StatusOK)
	if len(history) != 2 || history[0].Version != 2 || history[0].Status != "active" || history[1].Status != "superseded" {
		t.Fatalf("requirement history = %+v, want v2 active above v1 superseded", history)
	}
	if message := errorText(t, handler, http.MethodGet, "/api/projects/"+project+"/items/artifacts/x/history", nil, http.StatusBadRequest); !strings.Contains(message, "not versioned") {
		t.Errorf("artifact history error = %q", message)
	}
	if code := call(t, handler, http.MethodGet, "/api/projects/"+project+"/items/test-suites/missing/history", nil, nil).Code; code != http.StatusNotFound {
		t.Errorf("missing suite history = %d, want 404", code)
	}
}

func TestTestabilityRollupDerivesCoverageFromSuitesAndReports(t *testing.T) {
	handler, project, _ := seededServer(t)
	mustCall(t, handler, http.MethodPost, "/api/projects/"+project+"/requirements", map[string]any{"text": "Second"}, nil, http.StatusCreated)

	var before TestabilityReport
	mustCall(t, handler, http.MethodGet, "/api/projects/"+project+"/testability", nil, &before, http.StatusOK)
	if before.Available || before.CoveragePercent != nil || before.ActiveRequirements != 2 {
		t.Fatalf("rollup without suites = %+v, want unavailable with two active requirements", before)
	}

	mustCall(t, handler, http.MethodPost, "/api/projects/"+project+"/test-suites", suiteDocument("suite-orders"), nil, http.StatusCreated)

	var after TestabilityReport
	mustCall(t, handler, http.MethodGet, "/api/projects/"+project+"/testability", nil, &after, http.StatusOK)
	if !after.Available || after.CoveragePercent == nil || *after.CoveragePercent != 50 || after.OpenGaps == nil || *after.OpenGaps != 1 {
		t.Fatalf("rollup with one linked case = %+v, want 50%% coverage and one gap", after)
	}
	if after.Cases != 2 || after.UnlinkedCases != 1 || after.IncompleteSuites != 1 || after.LastTestedAt != nil {
		t.Fatalf("rollup counts = %+v", after)
	}
	if strings.Join(after.CoveredRequirements, ",") != "req-001" || strings.Join(after.UncoveredRequirements, ",") != "req-002" {
		t.Fatalf("coverage lists = %v / %v", after.CoveredRequirements, after.UncoveredRequirements)
	}

	var summaries []ProjectSummary
	mustCall(t, handler, http.MethodGet, "/api/projects", nil, &summaries, http.StatusOK)
	if len(summaries) != 1 || !summaries[0].Testability.Available || summaries[0].Testability.OpenGaps == nil {
		t.Fatalf("landing summaries = %+v, want the rollup on the project row", summaries)
	}
}

func TestTestabilityPrefersTheNewestEmbeddedRunTimestamp(t *testing.T) {
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
	reportDir := filepath.Join(projects.ProjectDir(project.ID), store.ReportsDirName)
	if err := os.MkdirAll(reportDir, 0o700); err != nil {
		t.Fatalf("mkdir reports: %v", err)
	}
	// Written first but records the later run; the second file is newer on disk.
	newerRun := []byte(`{"run_timestamp":"2026-09-02T09:30:00Z","summary":{"total":2,"passed":2,"failed":0}}`)
	olderRun := []byte(`{"run_timestamp":"2026-09-01T10:00:00Z","summary":{"total":2,"passed":1,"failed":1}}`)
	if err := os.WriteFile(filepath.Join(reportDir, "a.json"), newerRun, 0o600); err != nil {
		t.Fatalf("write report: %v", err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "b.json"), olderRun, 0o600); err != nil {
		t.Fatalf("write report: %v", err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "notes.csv"), []byte("id,status\n"), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	past := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{"a.json", "b.json", "notes.csv"} {
		if err := os.Chtimes(filepath.Join(reportDir, name), past, past); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}

	server, err := New(Options{Store: projects, Home: home})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var report TestabilityReport
	mustCall(t, server.Handler(), http.MethodGet, "/api/projects/"+project.ID+"/testability", nil, &report, http.StatusOK)
	if report.LastRun == nil || report.LastRun.Report != "a.json" || report.LastRun.Failed != 0 {
		t.Fatalf("last run = %+v, want a.json (newest run_timestamp)", report.LastRun)
	}
	if report.LastTestedAt == nil || report.LastTestedAt.Day() != 2 {
		t.Fatalf("lastTestedAt = %v, want the embedded 2026-09-02 timestamp", report.LastTestedAt)
	}
	if report.Reports != 3 || !report.Available {
		t.Fatalf("report counts = %+v", report)
	}
}

func TestSummarizeRunReportReadsCommonShapes(t *testing.T) {
	nested, at := summarizeRunReport("r.json", []byte(`{"outcome":"failed","summary":{"total":3,"passed":2,"failed":1},"run_timestamp":"2026-09-01T10:00:00Z"}`))
	if nested == nil || nested.Total != 3 || nested.Failed != 1 || nested.Outcome != "failed" || at == nil || at.Year() != 2026 {
		t.Fatalf("nested summary = %+v at %v", nested, at)
	}
	rows, at := summarizeRunReport("r.json", []byte(`{"results":[{"status":"passed"},{"status":"failed"},{"status":"skipped"}]}`))
	if rows == nil || rows.Total != 3 || rows.Passed != 1 || rows.Failed != 1 || rows.Skipped != 1 || rows.Outcome != "failed" || at != nil {
		t.Fatalf("row-derived summary = %+v at %v", rows, at)
	}
	if run, at := summarizeRunReport("r.csv", []byte("id,status\n1,passed\n")); run != nil || at != nil {
		t.Fatalf("csv summary = %+v at %v, want none", run, at)
	}
}
