package httpapi

import (
	"net/http"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/testgen"
	"github.com/ilyaus/loomwork/internal/traceability"
)

func TestDocumentFolderSettingsPreserveArtifactsAndLinks(t *testing.T) {
	handler, project, _ := seededServer(t)
	base := "/api/projects/" + project
	var artifact model.Artifact
	mustCall(t, handler, http.MethodPost, base+"/artifacts", map[string]any{"name": "qa/run/test.md", "type": "spec", "tags": []string{"test-scenario"}, "content": "Requirements: req-001"}, &artifact, http.StatusCreated)
	var settings testDocumentSettings
	mustCall(t, handler, http.MethodPut, base+"/test-document-settings", map[string]any{"roots": []string{"qa"}}, &settings, http.StatusOK)
	var tree TreeResponse
	mustCall(t, handler, http.MethodGet, base+"/items", nil, &tree, http.StatusOK)
	if len(tree.Groups[3].Items) != 1 || tree.Groups[3].Items[0].Family != "document-suites" || len(tree.Groups[5].Items) != 0 {
		t.Fatalf("tree = %+v", tree)
	}
	var doc ViewerDocument
	mustCall(t, handler, http.MethodGet, base+"/items/document-suites/qa", nil, &doc, http.StatusOK)
	if doc.ArtifactType != "document-suite" {
		t.Fatalf("viewer = %+v", doc)
	}
	var links map[string][]traceability.TestLink
	mustCall(t, handler, http.MethodGet, base+"/requirement-tests", nil, &links, http.StatusOK)
	if len(links["req-001"]) != 1 || links["req-001"][0].Ref != artifact.ID {
		t.Fatalf("links = %+v", links)
	}
	mustCall(t, handler, http.MethodPut, base+"/test-document-settings", map[string]any{"roots": []string{"qa", "qa/run"}}, nil, http.StatusBadRequest)
	mustCall(t, handler, http.MethodGet, base+"/test-document-settings", nil, &settings, http.StatusOK)
	if len(settings.Roots) != 1 || settings.Roots[0] != "qa" {
		t.Fatalf("invalid update changed roots: %+v", settings)
	}
	mustCall(t, handler, http.MethodPut, base+"/test-document-settings", map[string]any{"roots": []string{}}, nil, http.StatusOK)
	mustCall(t, handler, http.MethodGet, base+"/items", nil, &tree, http.StatusOK)
	if len(tree.Groups[3].Items) != 0 || len(tree.Groups[5].Items) != 1 || tree.Groups[5].Items[0].Ref != artifact.ID {
		t.Fatalf("regrouping changed documents: %+v", tree)
	}
	mustCall(t, handler, http.MethodGet, base+"/items/artifacts/"+artifact.ID, nil, &doc, http.StatusOK)
	if doc.Version != 1 {
		t.Fatal("regrouping changed document history")
	}
}

func TestNativeSuiteCanLinkCanonicalLegacyRequirementAlias(t *testing.T) {
	handler, project, _ := seededServer(t)
	base := "/api/projects/" + project
	var requirement model.Requirement
	mustCall(t, handler, http.MethodPost, base+"/requirements", map[string]any{"text": "Legacy imported requirement", "origin": "imported", "metadata": map[string]string{"import_format": "spec-kit", "feature": "008-example", "source_id": "FR-001"}}, &requirement, http.StatusCreated)
	var alias model.Requirement
	mustCall(t, handler, http.MethodGet, base+"/requirements/008-FR-001", nil, &alias, http.StatusOK)
	if alias.ID != requirement.ID || alias.DisplayID != "008-FR-001" {
		t.Fatalf("alias = %+v", alias)
	}
	suite := suiteDocument("canonical")
	cases := suite["cases"].([]map[string]any)[:1]
	cases[0]["requirement_ids"] = []string{"008-FR-001"}
	suite["cases"] = cases
	var result testgen.Result
	mustCall(t, handler, http.MethodPost, base+"/test-suites", suite, &result, http.StatusCreated)
	if result.Suite.Incomplete {
		t.Fatalf("canonical alias rejected by audit: %+v", result)
	}
	var health TestabilityReport
	mustCall(t, handler, http.MethodGet, base+"/testability", nil, &health, http.StatusOK)
	if len(health.CoveredRequirements) != 1 || health.CoveredRequirements[0] != requirement.ID {
		t.Fatalf("coverage = %+v", health)
	}
}
