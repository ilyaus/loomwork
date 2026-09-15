package httpapi

import (
	"net/http"

	"github.com/ilyaus/loomwork/internal/projectimport"
)

func (s *Server) previewProjectImport(w http.ResponseWriter, r *http.Request) {
	var request projectimport.Request
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	preview, err := projectimport.New(s.store).Preview(request)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) importProject(w http.ResponseWriter, r *http.Request) {
	var request projectimport.Request
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := projectimport.New(s.store).Import(request)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
