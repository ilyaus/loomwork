package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/store"
	"github.com/ilyaus/loomwork/web"
)

// newServer builds a handler over an isolated workspace, mirroring the
// per-test-workspace setup the CLI tests use.
func newServer(t *testing.T) http.Handler {
	t.Helper()
	home := t.TempDir()
	projects, err := store.NewDirStore(home + "/projects")
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	server, err := New(Options{Store: projects, Home: home})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return server.Handler()
}

// call issues a request and decodes the response body into out when out is not
// nil.
func call(t *testing.T, handler http.Handler, method, path string, body any, out any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, reader)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if out != nil {
		if err := json.Unmarshal(recorder.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s %s response %q: %v", method, path, recorder.Body.String(), err)
		}
	}
	return recorder
}

// mustCall fails when the status differs from want.
func mustCall(t *testing.T, handler http.Handler, method, path string, body any, out any, want int) {
	t.Helper()
	recorder := call(t, handler, method, path, body, out)
	if recorder.Code != want {
		t.Fatalf("%s %s = %d (%s), want %d", method, path, recorder.Code, recorder.Body.String(), want)
	}
}

// errorText returns the error message of a failed request.
func errorText(t *testing.T, handler http.Handler, method, path string, body any, wantStatus int) string {
	t.Helper()
	var payload struct {
		Error string `json:"error"`
	}
	recorder := call(t, handler, method, path, body, &payload)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s = %d (%s), want %d", method, path, recorder.Code, recorder.Body.String(), wantStatus)
	}
	return payload.Error
}

func TestProjectDirectoryAndSources(t *testing.T) {
	handler := newServer(t)

	var empty []ProjectSummary
	mustCall(t, handler, http.MethodGet, "/api/projects", nil, &empty, http.StatusOK)
	if len(empty) != 0 {
		t.Fatalf("projects = %+v, want none in a fresh workspace", empty)
	}

	var created model.Project
	mustCall(t, handler, http.MethodPost, "/api/projects", map[string]any{
		"name":        "checkout",
		"description": "Checkout regression",
		"tags":        []string{"cart", "payments"},
		"sources": []map[string]string{
			{"name": "spec", "type": "confluence", "url": "https://wiki/checkout"},
		},
	}, &created, http.StatusCreated)
	if created.ID == "" || len(created.Sources) != 1 {
		t.Fatalf("created = %+v, want an id and the initial source", created)
	}

	// The landing view reads only the project documents: the summary carries the
	// counts cached in project.json plus the not-yet-derived testability fields.
	var summaries []ProjectSummary
	mustCall(t, handler, http.MethodGet, "/api/projects", nil, &summaries, http.StatusOK)
	if len(summaries) != 1 {
		t.Fatalf("projects = %+v, want one", summaries)
	}
	summary := summaries[0]
	if summary.Name != "checkout" || summary.Sources != 1 || summary.Requirements != 0 {
		t.Errorf("summary = %+v, want the checkout summary with one source", summary)
	}
	if summary.Testability.Available || summary.Testability.LastTestedAt != nil || summary.Testability.CoveragePercent != nil || summary.Testability.OpenGaps != nil {
		t.Errorf("testability = %+v, want unavailable placeholders until execution reports land", summary.Testability)
	}

	// A project is addressable by id and by name.
	for _, ref := range []string{created.ID, "checkout"} {
		var fetched model.Project
		mustCall(t, handler, http.MethodGet, "/api/projects/"+ref, nil, &fetched, http.StatusOK)
		if fetched.ID != created.ID {
			t.Errorf("GET /api/projects/%s = %s, want %s", ref, fetched.ID, created.ID)
		}
	}

	// Linking a source with an existing name updates that link instead of
	// duplicating it.
	var sources []model.DocumentSource
	mustCall(t, handler, http.MethodPost, "/api/projects/checkout/sources", map[string]string{
		"name": "stories", "type": "ado", "url": "https://dev.azure.com/org/proj",
	}, &sources, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/projects/checkout/sources", map[string]string{
		"name": "stories", "type": "ado", "url": "https://dev.azure.com/org/other",
	}, &sources, http.StatusOK)
	if len(sources) != 2 || sources[1].URL != "https://dev.azure.com/org/other" {
		t.Fatalf("sources = %+v, want the ado link updated in place", sources)
	}
	mustCall(t, handler, http.MethodGet, "/api/projects/checkout/sources", nil, &sources, http.StatusOK)
	if len(sources) != 2 {
		t.Fatalf("sources = %+v, want both links persisted", sources)
	}
}

func TestRequirementLifecycleOverHTTP(t *testing.T) {
	handler := newServer(t)
	mustCall(t, handler, http.MethodPost, "/api/projects", map[string]string{"name": "checkout"}, nil, http.StatusCreated)

	var created model.Requirement
	mustCall(t, handler, http.MethodPost, "/api/projects/checkout/requirements", map[string]any{
		"text": "Cart totals include tax", "source_type": "ado", "source_ref": "AB#12", "tags": []string{"tax", "cart"},
	}, &created, http.StatusCreated)
	if created.ID != "req-001" || created.Version != 1 || created.Status != model.RequirementStatusActive {
		t.Fatalf("created = %+v, want an active req-001 v1", created)
	}
	if created.Origin != model.RequirementOriginAuthored || created.SourceType != model.SourceTypeADO {
		t.Errorf("created = %+v, want an authored ADO requirement", created)
	}

	// An update writes the next version, inherits the source back-reference, and
	// leaves the previous version retrievable as superseded.
	var updated model.Requirement
	mustCall(t, handler, http.MethodPatch, "/api/projects/checkout/requirements/req-001", map[string]any{
		"text": "Cart totals include tax and shipping",
	}, &updated, http.StatusOK)
	if updated.Version != 2 || updated.SourceRef != "AB#12" {
		t.Fatalf("updated = %+v, want v2 inheriting the source reference", updated)
	}
	// The editor resends the source pair it displays, so a version carrying a
	// reference stays valid: a reference without its type is rejected.
	var resent model.Requirement
	mustCall(t, handler, http.MethodPatch, "/api/projects/checkout/requirements/req-001", map[string]any{
		"text": "Cart totals include tax, shipping, and discounts", "source_type": "ado", "source_ref": "AB#12",
	}, &resent, http.StatusOK)
	if resent.Version != 3 || resent.SourceType != model.SourceTypeADO || resent.SourceRef != "AB#12" {
		t.Fatalf("resent = %+v, want v3 keeping the ADO reference", resent)
	}
	if got := errorText(t, handler, http.MethodPatch, "/api/projects/checkout/requirements/req-001",
		map[string]any{"text": "Orphan reference", "source_ref": "AB#99"}, http.StatusBadRequest); !strings.Contains(got, "source type") {
		t.Errorf("error = %q, want a missing source type error", got)
	}

	var first model.Requirement
	mustCall(t, handler, http.MethodGet, "/api/projects/checkout/requirements/req-001?version=1", nil, &first, http.StatusOK)
	if first.Text != "Cart totals include tax" || first.Status != model.RequirementStatusSuperseded {
		t.Fatalf("v1 = %+v, want the retained superseded snapshot", first)
	}
	var history []model.Requirement
	mustCall(t, handler, http.MethodGet, "/api/projects/checkout/requirements/req-001/history", nil, &history, http.StatusOK)
	if len(history) != 3 || history[0].Version != 1 || history[2].Version != 3 {
		t.Fatalf("history = %+v, want every version oldest first", history)
	}

	// Status changes are their own endpoint and retain the requirement.
	var obsolete model.Requirement
	mustCall(t, handler, http.MethodPost, "/api/projects/checkout/requirements/req-001/status",
		map[string]any{"status": "obsolete"}, &obsolete, http.StatusOK)
	if obsolete.Version != 3 || obsolete.Status != model.RequirementStatusObsolete {
		t.Fatalf("obsolete = %+v, want the current version marked obsolete", obsolete)
	}

	var listed []model.Requirement
	mustCall(t, handler, http.MethodGet, "/api/projects/checkout/requirements", nil, &listed, http.StatusOK)
	if len(listed) != 1 || listed[0].Status != model.RequirementStatusObsolete {
		t.Fatalf("listed = %+v, want the obsolete requirement retained", listed)
	}
	mustCall(t, handler, http.MethodGet, "/api/projects/checkout/requirements?status=active", nil, &listed, http.StatusOK)
	if len(listed) != 0 {
		t.Fatalf("listed = %+v, want no active requirements", listed)
	}

	// The project summary now reports the cached requirement counts.
	var summaries []ProjectSummary
	mustCall(t, handler, http.MethodGet, "/api/projects", nil, &summaries, http.StatusOK)
	if summaries[0].Requirements != 1 || summaries[0].ActiveRequirements != 0 {
		t.Fatalf("summary = %+v, want one requirement, none active", summaries[0])
	}
}

// TestRequirementWireFormatMatchesSchema pins the response shape to
// docs/schemas/requirement.schema.json, which is the frontend/backend contract.
func TestRequirementWireFormatMatchesSchema(t *testing.T) {
	handler := newServer(t)
	mustCall(t, handler, http.MethodPost, "/api/projects", map[string]string{"name": "checkout"}, nil, http.StatusCreated)

	var payload map[string]any
	mustCall(t, handler, http.MethodPost, "/api/projects/checkout/requirements", map[string]any{
		"text": "Cart totals include tax", "source_type": "github", "source_ref": "https://github.com/org/repo/spec.md",
		"origin": "extracted", "tags": []string{"cart"}, "metadata": map[string]string{"provider": "ollama"},
	}, &payload, http.StatusCreated)

	want := map[string]bool{
		"id": true, "version": true, "text": true, "status": true, "origin": true, "created_at": true,
		"source_type": true, "source_ref": true, "tags": true, "metadata": true,
	}
	for field := range payload {
		if !want[field] {
			t.Errorf("response has field %q, which the schema does not allow", field)
		}
	}
	for field := range want {
		if _, ok := payload[field]; !ok {
			t.Errorf("response is missing field %q", field)
		}
	}
	if payload["id"] != "req-001" || payload["origin"] != "extracted" {
		t.Errorf("payload = %+v, want the schema's snake_case values", payload)
	}

	// Optional fields are omitted rather than sent empty, as the schema's
	// dependentRequired rules assume.
	var minimal map[string]any
	mustCall(t, handler, http.MethodPost, "/api/projects/checkout/requirements",
		map[string]string{"text": "Guest checkout requires an email address"}, &minimal, http.StatusCreated)
	for _, field := range []string{"source_type", "source_ref", "tags", "metadata"} {
		if _, ok := minimal[field]; ok {
			t.Errorf("minimal payload has %q, want it omitted", field)
		}
	}
}

func TestErrorResponses(t *testing.T) {
	handler := newServer(t)
	mustCall(t, handler, http.MethodPost, "/api/projects", map[string]string{"name": "checkout"}, nil, http.StatusCreated)

	if got := errorText(t, handler, http.MethodGet, "/api/projects/nope", nil, http.StatusNotFound); !strings.Contains(got, "not found") {
		t.Errorf("error = %q, want a not-found error", got)
	}
	if got := errorText(t, handler, http.MethodGet, "/api/projects/checkout/requirements/req-404", nil, http.StatusNotFound); !strings.Contains(got, "req-404") {
		t.Errorf("error = %q, want the missing requirement id", got)
	}
	if got := errorText(t, handler, http.MethodPost, "/api/projects", map[string]string{"name": ""}, http.StatusBadRequest); !strings.Contains(got, "name is required") {
		t.Errorf("error = %q, want a validation error", got)
	}
	if got := errorText(t, handler, http.MethodPost, "/api/projects", map[string]string{"name": "checkout"}, http.StatusBadRequest); !strings.Contains(got, "already used") {
		t.Errorf("error = %q, want a duplicate name error", got)
	}
	if got := errorText(t, handler, http.MethodPost, "/api/projects/checkout/requirements",
		map[string]string{"text": "x", "source_ref": "AB#1"}, http.StatusBadRequest); !strings.Contains(got, "needs a source type") {
		t.Errorf("error = %q, want a source-type requirement error", got)
	}
	if got := errorText(t, handler, http.MethodPost, "/api/projects/checkout/requirements",
		map[string]string{"text": "x", "status": "superseded"}, http.StatusBadRequest); !strings.Contains(got, "only by creating a new version") {
		t.Errorf("error = %q, want superseded to be rejected as an input status", got)
	}
	if got := errorText(t, handler, http.MethodPost, "/api/projects/checkout/requirements",
		map[string]string{"text": "x", "unexpected": "y"}, http.StatusBadRequest); !strings.Contains(got, "unexpected") {
		t.Errorf("error = %q, want unknown fields rejected", got)
	}
	if got := errorText(t, handler, http.MethodGet, "/api/nope", nil, http.StatusNotFound); !strings.Contains(got, "no endpoint") {
		t.Errorf("error = %q, want an unknown endpoint error", got)
	}

	// An update never sets a status; the status endpoint does.
	mustCall(t, handler, http.MethodPost, "/api/projects/checkout/requirements", map[string]string{"text": "Cart totals include tax"}, nil, http.StatusCreated)
	if got := errorText(t, handler, http.MethodPatch, "/api/projects/checkout/requirements/req-001",
		map[string]string{"text": "x", "status": "obsolete"}, http.StatusBadRequest); !strings.Contains(got, "status endpoint") {
		t.Errorf("error = %q, want the update to reject a status", got)
	}
	if got := errorText(t, handler, http.MethodGet, "/api/projects/checkout/requirements/req-001?version=0", nil, http.StatusBadRequest); !strings.Contains(got, "1 or greater") {
		t.Errorf("error = %q, want an invalid version error", got)
	}

	recorder := call(t, handler, http.MethodDelete, "/api/projects/checkout/requirements/req-001", nil, nil)
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != "GET, PATCH" {
		t.Errorf("DELETE = %d with Allow %q, want 405 listing GET, PATCH", recorder.Code, recorder.Header().Get("Allow"))
	}
}

// TestUIAssetsAreServed checks the embedded single-page app is reachable and that
// client-side routes fall back to index.html.
func TestUIAssetsAreServed(t *testing.T) {
	projects, err := store.NewDirStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	server, err := New(Options{Store: projects, Assets: web.Assets()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	handler := server.Handler()

	for _, path := range []string{"/", "/app.js", "/projects/prj-1"} {
		recorder := call(t, handler, http.MethodGet, path, nil, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, recorder.Code)
		}
	}
	recorder := call(t, handler, http.MethodGet, "/", nil, nil)
	if !strings.Contains(recorder.Body.String(), "app.js") {
		t.Errorf("index body = %q, want the app entry point", recorder.Body.String())
	}
}
