package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/orchestrator"
	"github.com/ilyaus/loomwork/internal/store"
)

const (
	familyRequirements     = "requirements"
	familyAgentDefinitions = "agent-definitions"
	familyOverrideRules    = "override-rules"
	familyTestSuites       = "test-suites"
	familyTestCases        = "test-cases"
	familyReports          = "reports"
	familyArtifacts        = "artifacts"
)

// TreeResponse is the project-directory navigation tree.
type TreeResponse struct {
	Groups []TreeGroup `json:"groups"`
}

// TreeGroup is one on-disk entity family.
type TreeGroup struct {
	Family string     `json:"family"`
	Label  string     `json:"label"`
	Items  []TreeItem `json:"items"`
}

// TreeItem opens one viewer document. Test suites include their cases as child
// nodes while remaining selectable themselves.
type TreeItem struct {
	Ref          string     `json:"ref"`
	Name         string     `json:"name"`
	Family       string     `json:"family"`
	ArtifactType string     `json:"artifactType"`
	MediaType    string     `json:"mediaType"`
	Version      int        `json:"version,omitempty"`
	Status       string     `json:"status,omitempty"`
	Children     []TreeItem `json:"children,omitempty"`
}

// ViewerDocument is the registry input returned for one selected tree node.
type ViewerDocument struct {
	Ref          string            `json:"ref"`
	Name         string            `json:"name"`
	Family       string            `json:"family"`
	ArtifactType string            `json:"artifactType"`
	MediaType    string            `json:"mediaType"`
	Version      int               `json:"version,omitempty"`
	Body         json.RawMessage   `json:"body"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func (s *Server) listProjectItems(w http.ResponseWriter, _ *http.Request, projectRef string) {
	requirements, err := s.store.ListRequirements(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	definitions, err := s.store.ListAgentDefinitions(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	rules, err := s.store.ListOverrideRules(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	suites, err := s.store.ListTestSuites(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	reports, err := s.store.ListReports(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	project, err := s.store.Resolve(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	groups := []TreeGroup{
		{Family: familyRequirements, Label: "Requirements", Items: make([]TreeItem, 0, len(requirements))},
		{Family: familyAgentDefinitions, Label: "Agent definitions", Items: make([]TreeItem, 0, len(definitions))},
		{Family: familyOverrideRules, Label: "Override rules", Items: make([]TreeItem, 0, len(rules))},
		{Family: familyTestSuites, Label: "Test suites", Items: make([]TreeItem, 0, len(suites))},
		{Family: familyReports, Label: "Reports", Items: make([]TreeItem, 0, len(reports))},
		{Family: familyArtifacts, Label: "Artifacts", Items: make([]TreeItem, 0, len(project.Artifacts))},
	}
	for _, requirement := range requirements {
		groups[0].Items = append(groups[0].Items, TreeItem{
			Ref: requirement.ID, Name: requirement.ID, Family: familyRequirements,
			ArtifactType: "requirement", MediaType: "application/json", Version: requirement.Version,
			Status: string(requirement.Status),
		})
	}
	for _, definition := range definitions {
		groups[1].Items = append(groups[1].Items, TreeItem{
			Ref: definition.AgentName, Name: definition.AgentName, Family: familyAgentDefinitions,
			ArtifactType: "agent-definition", MediaType: "text/markdown", Version: definition.Version,
		})
	}
	for _, rule := range rules {
		groups[2].Items = append(groups[2].Items, TreeItem{
			Ref: rule.ID, Name: rule.Title, Family: familyOverrideRules,
			ArtifactType: "override-rule", MediaType: "application/json", Version: rule.Version,
			Status: string(rule.Status),
		})
	}
	for _, suite := range suites {
		detailed, err := s.store.LoadTestSuite(projectRef, suite.SuiteID, suite.Version)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		children := make([]TreeItem, 0, len(detailed.Cases))
		for _, testCase := range detailed.Cases {
			children = append(children, TreeItem{
				Ref: suite.SuiteID + "~" + testCase.ID, Name: firstNonEmpty(testCase.Name, testCase.ID), Family: familyTestCases,
				ArtifactType: "test-case", MediaType: "application/vnd.loomwork.test-case+json",
				Version: suite.Version,
			})
		}
		status := "ready"
		if suite.Incomplete {
			status = "incomplete"
		}
		groups[3].Items = append(groups[3].Items, TreeItem{
			Ref: suite.SuiteID, Name: firstNonEmpty(suite.Title, suite.SuiteID), Family: familyTestSuites,
			ArtifactType: "test-suite", MediaType: "application/json", Version: suite.Version,
			Status: status, Children: children,
		})
	}
	for _, report := range reports {
		groups[4].Items = append(groups[4].Items, TreeItem{
			Ref: report.Name, Name: report.Name, Family: familyReports,
			ArtifactType: "test-report", MediaType: report.MediaType,
		})
	}
	for _, artifact := range project.LatestArtifacts() {
		groups[5].Items = append(groups[5].Items, TreeItem{
			Ref: artifact.ID, Name: artifact.Name, Family: familyArtifacts,
			ArtifactType: string(artifact.Type), MediaType: artifactMediaType(artifact),
			Version: artifact.Version,
		})
	}
	writeJSON(w, http.StatusOK, TreeResponse{Groups: groups})
}

func (s *Server) getProjectItem(w http.ResponseWriter, r *http.Request, projectRef, family, ref string) {
	version, err := versionParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var document ViewerDocument
	switch family {
	case familyRequirements:
		requirement, err := s.store.LoadRequirement(projectRef, ref, version)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		document, err = jsonDocument(ref, ref, family, "requirement", "application/json", requirement.Version, requirement, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	case familyAgentDefinitions:
		definition, err := s.store.LoadAgentDefinition(projectRef, ref, version)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		document = textDocument(ref, definition.AgentName, family, "agent-definition", "text/markdown", definition.Version, definition.Markdown(), nil)
	case familyOverrideRules:
		rule, err := s.store.LoadOverrideRule(projectRef, ref, version)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		document, err = jsonDocument(ref, rule.Title, family, "override-rule", "application/json", rule.Version, rule, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	case familyTestSuites:
		suite, err := s.store.LoadTestSuite(projectRef, ref, version)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		document, err = jsonDocument(ref, firstNonEmpty(suite.Title, suite.SuiteID), family, "test-suite", "application/json", suite.Version, suite, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	case familyTestCases:
		suiteID, caseID, ok := strings.Cut(ref, "~")
		if !ok || suiteID == "" || caseID == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("test-case ref %q must look like suite~tc-001", ref))
			return
		}
		suite, err := s.store.LoadTestSuite(projectRef, suiteID, version)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		testCase, found := findTestCase(suite.Cases, caseID)
		if !found {
			writeStoreError(w, fmt.Errorf("test case %q in suite %s: %w", caseID, suiteID, store.ErrNotFound))
			return
		}
		document, err = jsonDocument(ref, testCase.Name, family, "test-case", "application/vnd.loomwork.test-case+json", suite.Version, testCase, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	case familyReports:
		report, raw, err := s.store.LoadReport(projectRef, ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		document = textDocument(ref, report.Name, family, "test-report", report.MediaType, 0, string(raw), nil)
	case familyArtifacts:
		project, err := s.store.Resolve(projectRef)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		artifact, ok := project.ResolveArtifact(ref)
		if !ok {
			writeStoreError(w, fmt.Errorf("artifact %q in project %s: %w", ref, project.Name, store.ErrNotFound))
			return
		}
		content, contentErr := orchestrator.ArtifactContent(artifact)
		if contentErr != nil {
			content = artifact.Body.Ref
		}
		document = textDocument(
			artifact.ID, artifact.Name, family, string(artifact.Type), artifactMediaType(artifact),
			artifact.Version, content, artifact.Metadata,
		)
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown item family %q", family))
		return
	}
	writeJSON(w, http.StatusOK, document)
}

func jsonDocument(ref, name, family, artifactType, mediaType string, version int, value any, metadata map[string]string) (ViewerDocument, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return ViewerDocument{}, fmt.Errorf("encode %s %q: %w", family, ref, err)
	}
	return ViewerDocument{
		Ref: ref, Name: name, Family: family, ArtifactType: artifactType,
		MediaType: mediaType, Version: version, Body: raw, Metadata: metadata,
	}, nil
}

func textDocument(ref, name, family, artifactType, mediaType string, version int, content string, metadata map[string]string) ViewerDocument {
	raw, _ := json.Marshal(content)
	return ViewerDocument{
		Ref: ref, Name: name, Family: family, ArtifactType: artifactType,
		MediaType: mediaType, Version: version, Body: raw, Metadata: metadata,
	}
}

func artifactMediaType(artifact model.Artifact) string {
	if mediaType := strings.TrimSpace(artifact.Body.MediaType); mediaType != "" {
		return mediaType
	}
	switch strings.ToLower(filepath.Ext(artifact.Name)) {
	case ".html", ".htm":
		return "text/html"
	case ".md", ".markdown":
		return "text/markdown"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	default:
		return "text/plain"
	}
}

func findTestCase(cases []model.TestCase, id string) (model.TestCase, bool) {
	for _, testCase := range cases {
		if testCase.ID == id {
			return testCase, true
		}
	}
	return model.TestCase{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
