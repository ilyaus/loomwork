package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/testgen"
)

// artifactRequest adds one inline-content artifact revision. File references
// stay a CLI concern: a browser cannot point at a path on the server's disk.
type artifactRequest struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Content   string   `json:"content"`
	MediaType string   `json:"mediaType"`
	Tags      []string `json:"tags"`
	Pinned    bool     `json:"pinned"`
}

func (s *Server) listArtifacts(w http.ResponseWriter, _ *http.Request, projectRef string) {
	project, err := s.store.Resolve(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project.LatestArtifacts())
}

// addArtifact appends a revision; the same name becomes the next version,
// matching `artifact add`.
func (s *Server) addArtifact(w http.ResponseWriter, r *http.Request, projectRef string) {
	var request artifactRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rawType := request.Type
	if strings.TrimSpace(rawType) == "" {
		rawType = string(model.ArtifactTypeDoc)
	}
	artifactType, err := model.ParseArtifactType(rawType)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var artifact model.Artifact
	if _, err := s.store.Update(projectRef, func(project *model.Project) error {
		added, err := project.AddArtifact(model.ArtifactSpec{
			Name:   request.Name,
			Type:   artifactType,
			Body:   model.Body{Content: request.Content, MediaType: strings.TrimSpace(request.MediaType)},
			Tags:   request.Tags,
			Pinned: request.Pinned,
		})
		artifact = added
		return err
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, artifact)
}

func (s *Server) listReports(w http.ResponseWriter, _ *http.Request, projectRef string) {
	reports, err := s.store.ListReports(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) addReport(w http.ResponseWriter, r *http.Request, projectRef string) {
	var request struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	report, err := s.store.AddReport(projectRef, request.Name, []byte(request.Content))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, report)
}

func (s *Server) listTestSuites(w http.ResponseWriter, _ *http.Request, projectRef string) {
	suites, err := s.store.ListTestSuites(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, suites)
}

// importTestSuite stores a suite authored elsewhere as the next version of its
// suite id. The body is the suite document itself, so a file that works with
// `test-suite import --file` can be posted unchanged. Incomplete suites are
// stored and flagged, never rejected.
func (s *Server) importTestSuite(w http.ResponseWriter, r *http.Request, projectRef string) {
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read request body: %w", err))
		return
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("a test suite document is required"))
		return
	}
	generator := testgen.New(s.store, nil)
	result, err := generator.Import(testgen.ImportRequest{
		ProjectRef: projectRef,
		SuiteID:    strings.TrimSpace(r.URL.Query().Get("suiteId")),
		Payload:    payload,
		SourcePath: "browser-import",
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// agentDefinitionRequest mirrors docs/schemas/agent-definition.schema.json minus
// the fields the store assigns. On update, empty fields inherit.
type agentDefinitionRequest struct {
	AgentName      string            `json:"agent_name"`
	TargetProvider string            `json:"target_provider"`
	Model          string            `json:"model"`
	ToolsAllowed   []string          `json:"tools_allowed"`
	Body           string            `json:"body"`
	Description    string            `json:"description"`
	Tags           []string          `json:"tags"`
	Metadata       map[string]string `json:"metadata"`
}

func (r agentDefinitionRequest) spec() model.AgentDefinitionSpec {
	return model.AgentDefinitionSpec{
		AgentName:      r.AgentName,
		TargetProvider: model.AgentTarget(strings.TrimSpace(r.TargetProvider)),
		Model:          r.Model,
		ToolsAllowed:   r.ToolsAllowed,
		Body:           r.Body,
		Description:    r.Description,
		Tags:           r.Tags,
		Metadata:       r.Metadata,
	}
}

func (s *Server) listAgentDefinitions(w http.ResponseWriter, _ *http.Request, projectRef string) {
	definitions, err := s.store.ListAgentDefinitions(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitions)
}

func (s *Server) createAgentDefinition(w http.ResponseWriter, r *http.Request, projectRef string) {
	var request agentDefinitionRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	spec := request.spec()
	if spec.TargetProvider == "" {
		spec.TargetProvider = model.AgentTargetClaudeSDK
	}
	definition, err := s.store.CreateAgentDefinition(projectRef, spec)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, definition)
}

// updateAgentDefinition writes the next version; the previous file is never
// touched.
func (s *Server) updateAgentDefinition(w http.ResponseWriter, r *http.Request, projectRef, agentName string) {
	var request agentDefinitionRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	definition, err := s.store.UpdateAgentDefinition(projectRef, agentName, request.spec())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definition)
}

// overrideRuleRequest mirrors the override_rule definition in
// docs/schemas/agent-definition.schema.json minus version, status, and
// created_at. Status changes go through the status endpoint.
type overrideRuleRequest struct {
	ID        string                  `json:"id"`
	Title     string                  `json:"title"`
	Condition model.OverrideCondition `json:"condition"`
	Action    model.OverrideAction    `json:"action"`
	Rationale string                  `json:"rationale"`
	Tags      []string                `json:"tags"`
	Metadata  map[string]string       `json:"metadata"`
}

func (r overrideRuleRequest) spec() model.OverrideRuleSpec {
	return model.OverrideRuleSpec{
		ID:        r.ID,
		Title:     r.Title,
		Condition: r.Condition,
		Action:    r.Action,
		Rationale: r.Rationale,
		Tags:      r.Tags,
		Metadata:  r.Metadata,
	}
}

func (s *Server) listOverrideRules(w http.ResponseWriter, _ *http.Request, projectRef string) {
	rules, err := s.store.ListOverrideRules(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) createOverrideRule(w http.ResponseWriter, r *http.Request, projectRef string) {
	var request overrideRuleRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rule, err := s.store.CreateOverrideRule(projectRef, request.spec())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

// updateOverrideRule writes the next version and supersedes the current one.
func (s *Server) updateOverrideRule(w http.ResponseWriter, r *http.Request, projectRef, ruleID string) {
	var request overrideRuleRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rule, err := s.store.UpdateOverrideRule(projectRef, ruleID, request.spec())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) setOverrideRuleStatus(w http.ResponseWriter, r *http.Request, projectRef, ruleID string) {
	var request statusRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if request.Version < 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("version %d must be 1 or greater (0 or omitted changes the current version)", request.Version))
		return
	}
	status, err := model.ParseOverrideRuleStatus(request.Status)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rule, err := s.store.SetOverrideRuleStatus(projectRef, ruleID, request.Version, status)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}
