package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
)

// requirementRequest is the writable half of docs/schemas/requirement.schema.json:
// the field names match the schema exactly, minus id, version, and created_at,
// which the store assigns. Responses are the stored model.Requirement, so one
// document shape is the contract for both directions.
type requirementRequest struct {
	Text       string            `json:"text"`
	SourceType string            `json:"source_type"`
	SourceRef  string            `json:"source_ref"`
	Status     string            `json:"status"`
	Origin     string            `json:"origin"`
	Tags       []string          `json:"tags"`
	Metadata   map[string]string `json:"metadata"`
}

// spec converts a request into a store spec. Empty fields stay empty so the
// domain applies its own defaults and, on update, inherits from the current
// version.
func (r requirementRequest) spec() (model.RequirementSpec, error) {
	spec := model.RequirementSpec{
		Text:      r.Text,
		SourceRef: strings.TrimSpace(r.SourceRef),
		Tags:      r.Tags,
		Metadata:  r.Metadata,
	}
	if strings.TrimSpace(r.SourceType) != "" {
		sourceType, err := model.ParseSourceType(r.SourceType)
		if err != nil {
			return model.RequirementSpec{}, err
		}
		spec.SourceType = sourceType
	}
	if strings.TrimSpace(r.Status) != "" {
		status, err := model.ParseRequirementStatus(r.Status)
		if err != nil {
			return model.RequirementSpec{}, err
		}
		spec.Status = status
	}
	if strings.TrimSpace(r.Origin) != "" {
		origin, err := model.ParseRequirementOrigin(r.Origin)
		if err != nil {
			return model.RequirementSpec{}, err
		}
		spec.Origin = origin
	}
	return spec, nil
}

func (s *Server) listRequirements(w http.ResponseWriter, r *http.Request, projectRef string) {
	requirements, err := s.store.ListRequirements(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		wanted, err := model.ParseRequirementStatus(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		filtered := make([]*model.Requirement, 0, len(requirements))
		for _, requirement := range requirements {
			if requirement.Status == wanted {
				filtered = append(filtered, requirement)
			}
		}
		requirements = filtered
	}
	writeJSON(w, http.StatusOK, requirements)
}

func (s *Server) createRequirement(w http.ResponseWriter, r *http.Request, projectRef string) {
	var request requirementRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	spec, err := request.spec()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	requirement, err := s.store.CreateRequirement(projectRef, spec)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, requirement)
}

// getRequirement returns one version; ?version=N selects a retained snapshot and
// omitting it returns the current one.
func (s *Server) getRequirement(w http.ResponseWriter, r *http.Request, projectRef, requirementID string) {
	version, err := versionParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	requirement, err := s.store.LoadRequirement(projectRef, requirementID, version)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, requirement)
}

// updateRequirement writes the next version and supersedes the previous one.
// Status is not settable here: the new version is active and the old one becomes
// superseded, exactly as in the CLI.
func (s *Server) updateRequirement(w http.ResponseWriter, r *http.Request, projectRef, requirementID string) {
	var request requirementRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(request.Status) != "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("an update always writes an active version: use the status endpoint to change a status"))
		return
	}
	spec, err := request.spec()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	requirement, err := s.store.UpdateRequirement(projectRef, requirementID, spec)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, requirement)
}

func (s *Server) requirementHistory(w http.ResponseWriter, _ *http.Request, projectRef, requirementID string) {
	history, err := s.store.RequirementHistory(projectRef, requirementID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, history)
}

// statusRequest changes one version's status; version 0 (or omitted) means the
// current version.
type statusRequest struct {
	Status  string `json:"status"`
	Version int    `json:"version"`
}

func (s *Server) setRequirementStatus(w http.ResponseWriter, r *http.Request, projectRef, requirementID string) {
	var request statusRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if request.Version < 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("version %d must be 1 or greater (0 or omitted changes the current version)", request.Version))
		return
	}
	status, err := model.ParseRequirementStatus(request.Status)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	requirement, err := s.store.SetRequirementStatus(projectRef, requirementID, request.Version, status)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, requirement)
}
