package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
)

type testDocumentSettings struct {
	Roots    []string `json:"roots"`
	Defaults []string `json:"defaults"`
}

func documentSettings(project *model.Project) testDocumentSettings {
	return testDocumentSettings{Roots: project.TestDocumentRoots(), Defaults: project.DefaultTestDocumentRoots()}
}

func (s *Server) getTestDocumentSettings(w http.ResponseWriter, _ *http.Request, projectRef string) {
	project, err := s.store.Resolve(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, documentSettings(project))
}

func (s *Server) setTestDocumentSettings(w http.ResponseWriter, r *http.Request, projectRef string) {
	var request struct {
		Roots *[]string `json:"roots"`
	}
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if request.Roots == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("roots is required; use an empty array to disable document suites"))
		return
	}
	roots, err := model.NormalizeTestDocumentRoots(*request.Roots)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	project, err := s.store.Update(projectRef, func(project *model.Project) error {
		project.TestDocuments = &model.TestDocumentConfig{Roots: roots}
		project.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, documentSettings(project))
}
