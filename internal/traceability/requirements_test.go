package traceability

import (
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

func importedRequirement(id, feature, sourceID string) *model.Requirement {
	return &model.Requirement{ID: id, Metadata: map[string]string{"import_format": "spec-kit", "feature": feature, "source_id": sourceID}}
}

func scenario(id, feature, name, content string) model.Artifact {
	return model.Artifact{ID: id, Name: name, Version: 1, Type: model.ArtifactTypeSpec, Body: model.Body{Content: content}, Metadata: map[string]string{"import_format": "spec-kit", "feature": feature, "document_kind": "test-scenario"}}
}

func TestRequirementLinksUseExplicitReferencesWithinAFeature(t *testing.T) {
	requirements := []*model.Requirement{
		importedRequirement("req-001", "001-first", "FR-001"),
		importedRequirement("req-002", "002-second", "FR-001"),
		importedRequirement("req-003", "001-first", "FR-002"),
		importedRequirement("req-004", "001-first", "FR-010"),
	}
	artifacts := []model.Artifact{
		scenario("primary", "001-first", "specs/001-first/scenarios/FR-001.api.md", "---\nfr: FR-001\n---\n# Scenarios\nNotes mention FR-002, but do not assert it."),
		scenario("endpoint", "001-first", "specs/001-first/tests/POST-orders.md", "# Tests\n**Purpose:** Covers FR-001,FR-002.\n```json\n{\"example\":\"FR-010\"}\n```\nNotes: see API-FR-010-001."),
		scenario("other-feature", "002-second", "specs/002-second/scenarios/FR-001.api.md", "# Other feature"),
		scenario("unlinked", "001-first", "specs/001-first/tests/GET-health.md", "# Health check\nNo requirement link."),
		scenario("substring", "001-first", "specs/001-first/tests/edge.md", "**Purpose:** FR-0010 and API-FR-010-001 and FR-010-extra are different identifiers."),
	}
	links := buildLinks(requirements, nil, artifacts)
	for id, count := range map[string]int{"req-001": 2, "req-002": 1, "req-003": 1, "req-004": 0} {
		if len(links[id]) != count {
			t.Errorf("%s links = %+v, want %d", id, links[id], count)
		}
	}
	if links["req-002"][0].Ref != "other-feature" {
		t.Fatalf("cross-feature link = %+v", links["req-002"])
	}
}

func TestRequirementLinksDistinguishNativeCasesAndScenarioDocuments(t *testing.T) {
	requirements := []*model.Requirement{{ID: "req-001"}, {ID: "req-002"}}
	suites := []*model.TestSuite{{SuiteID: "orders", Version: 2, Title: "Orders", Cases: []model.TestCase{
		{ID: "tc-001", Name: "Get order", RequirementIDs: []string{"req-001", "req-001"}},
		{ID: "tc-002", Name: "Update order", RequirementIDs: []string{"req-001", "req-002", "req-999"}},
	}}}
	artifacts := []model.Artifact{
		{ID: "local", Name: "local-test.md", Version: 2, Tags: []string{"test-scenario"}, Body: model.Body{Content: "**Requirements:** req-001, req-001\n```md\n**Requirements:** req-002\n```\n    **Requirements:** req-002"}},
		{ID: "doc", Name: "spec.md", Body: model.Body{Content: "**Requirements:** req-001"}},
	}
	links := buildLinks(requirements, suites, artifacts)
	if len(links["req-001"]) != 3 || len(links["req-002"]) != 1 {
		t.Fatalf("links = %+v", links)
	}
	if _, ok := links["req-999"]; ok {
		t.Fatal("linked an unknown requirement")
	}
	for _, link := range links["req-002"] {
		if link.Family != "test-cases" || link.Ref != "orders~tc-002" || link.Version != 2 || link.SuiteID != "orders" {
			t.Fatalf("case link = %+v", link)
		}
	}
}
