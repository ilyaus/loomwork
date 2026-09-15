package httpapi

import (
	"net/http"

	"github.com/ilyaus/loomwork/internal/traceability"
)

func (s *Server) requirementTests(w http.ResponseWriter, _ *http.Request, projectRef string) {
	links, err := traceability.RequirementTests(s.store, projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, links)
}
