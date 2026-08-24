package httpapi

import (
	_ "embed"
	"net/http"
)

// openAPIDocument is the hand-written description of the endpoints this package
// routes. It is embedded rather than generated: the router is explicit Go, so a
// generator would add a build step without adding truth, and keeping the document
// next to the handlers makes an undocumented endpoint a test failure (see
// TestOpenAPIDocumentCoversEveryRoute).
//
//go:embed openapi.json
var openAPIDocument []byte

// OpenAPIDocument returns the embedded OpenAPI 3.1 description of the API, so a
// caller that wants the contract without starting a server can read it too.
func OpenAPIDocument() []byte {
	document := make([]byte, len(openAPIDocument))
	copy(document, openAPIDocument)
	return document
}

// openAPI serves the description of the API from the same binary that implements
// it, which keeps the two versions from drifting apart in a client's hands.
func (s *Server) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPIDocument)
}
