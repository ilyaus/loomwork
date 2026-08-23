package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// openAPISpec is the subset of the document the tests navigate.
type openAPISpec struct {
	OpenAPI string                    `json:"openapi"`
	Paths   map[string]map[string]any `json:"paths"`
	// Raw is the same document as an untyped tree, for walking $refs.
	Raw map[string]any `json:"-"`
}

// httpMethods are the keys of a path item that name an operation; the others
// (parameters, summary) describe the path itself.
var httpMethods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
}

// loadSpec decodes the embedded document once per test.
func loadSpec(t *testing.T) openAPISpec {
	t.Helper()
	var spec openAPISpec
	if err := json.Unmarshal(OpenAPIDocument(), &spec); err != nil {
		t.Fatalf("decode the embedded document: %v", err)
	}
	if err := json.Unmarshal(OpenAPIDocument(), &spec.Raw); err != nil {
		t.Fatalf("decode the embedded document as a tree: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.1") {
		t.Fatalf("openapi = %q, want a 3.1 document", spec.OpenAPI)
	}
	if len(spec.Paths) == 0 {
		t.Fatal("the document describes no paths")
	}
	return spec
}

// operations lists every documented method for a path, upper-cased.
func operations(item map[string]any) []string {
	methods := make([]string, 0, len(item))
	for key := range item {
		for _, method := range httpMethods {
			if strings.EqualFold(key, method) {
				methods = append(methods, method)
			}
		}
	}
	sort.Strings(methods)
	return methods
}

// seededServer returns a handler over a workspace holding one project and one
// requirement, so a documented path can be exercised with references that exist.
func seededServer(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	handler := newServer(t)

	var project struct {
		ID string `json:"id"`
	}
	mustCall(t, handler, http.MethodPost, "/api/projects", map[string]any{
		"name": "checkout",
	}, &project, http.StatusCreated)

	var requirement struct {
		ID string `json:"id"`
	}
	mustCall(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/requirements", map[string]any{
		"text": "Cart totals include tax",
	}, &requirement, http.StatusCreated)

	return handler, project.ID, requirement.ID
}

func TestOpenAPIDocumentIsServedFromTheBinary(t *testing.T) {
	handler := newServer(t)

	recorder := call(t, handler, http.MethodGet, "/api/openapi.json", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/openapi.json = %d (%s), want 200", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", contentType)
	}
	if !json.Valid(recorder.Body.Bytes()) {
		t.Fatalf("the served body is not valid JSON: %s", recorder.Body.String())
	}
	if got, want := recorder.Body.String(), string(OpenAPIDocument()); got != want {
		t.Error("the served document differs from the embedded one")
	}
}

// TestOpenAPIDocumentCoversEveryRoute is the reason the document lives beside the
// router: a documented operation must reach a handler, and the Allow header of a
// rejected method must list exactly the documented methods, so a route the
// document omits fails here instead of surprising a client.
func TestOpenAPIDocumentCoversEveryRoute(t *testing.T) {
	handler, project, requirementID := seededServer(t)
	spec := loadSpec(t)

	for path, item := range spec.Paths {
		url := strings.NewReplacer(
			"{projectRef}", project,
			"{requirementId}", requirementID,
		).Replace(path)
		if strings.Contains(url, "{") {
			t.Fatalf("path %q has a template variable the test does not know how to fill", path)
		}

		documented := operations(item)
		if len(documented) == 0 {
			t.Errorf("path %q documents no operations", path)
			continue
		}

		for _, method := range documented {
			// An empty body makes a write fail validation rather than mutate the
			// workspace; either way the request reached a handler, which is what
			// the routing table is being checked for.
			recorder := call(t, handler, method, url, nil, nil)
			if recorder.Code == http.StatusMethodNotAllowed {
				t.Errorf("%s %s = 405, want the documented operation to be routed", method, url)
			}
			if body := recorder.Body.String(); strings.Contains(body, "no endpoint for") {
				t.Errorf("%s %s is documented but not routed: %s", method, url, body)
			}
		}

		// TRACE is routed by no endpoint, so the rejection reports the methods the
		// router really serves for this path.
		recorder := call(t, handler, http.MethodTrace, url, nil, nil)
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("TRACE %s = %d (%s), want 405", url, recorder.Code, recorder.Body.String())
			continue
		}
		allowed := strings.Split(recorder.Header().Get("Allow"), ", ")
		sort.Strings(allowed)
		if strings.Join(allowed, ",") != strings.Join(documented, ",") {
			t.Errorf("%s serves %v but the document describes %v", path, allowed, documented)
		}
	}
}

// TestOpenAPIDocumentReferencesResolve keeps a typo in a $ref from shipping as a
// document no client can dereference.
func TestOpenAPIDocumentReferencesResolve(t *testing.T) {
	spec := loadSpec(t)
	for _, ref := range collectRefs(spec.Raw) {
		if !strings.HasPrefix(ref, "#/") {
			t.Errorf("$ref %q is not a local pointer", ref)
			continue
		}
		if !resolvePointer(spec.Raw, strings.Split(strings.TrimPrefix(ref, "#/"), "/")) {
			t.Errorf("$ref %q does not resolve", ref)
		}
	}
}

// collectRefs walks a decoded JSON tree and returns every $ref value.
func collectRefs(node any) []string {
	switch value := node.(type) {
	case map[string]any:
		refs := make([]string, 0, 4)
		for key, child := range value {
			if key == "$ref" {
				if ref, ok := child.(string); ok {
					refs = append(refs, ref)
					continue
				}
			}
			refs = append(refs, collectRefs(child)...)
		}
		return refs
	case []any:
		refs := make([]string, 0, len(value))
		for _, child := range value {
			refs = append(refs, collectRefs(child)...)
		}
		return refs
	default:
		return nil
	}
}

// resolvePointer reports whether a JSON pointer's segments exist in the tree.
func resolvePointer(node any, segments []string) bool {
	if len(segments) == 0 {
		return true
	}
	object, ok := node.(map[string]any)
	if !ok {
		return false
	}
	child, ok := object[segments[0]]
	if !ok {
		return false
	}
	return resolvePointer(child, segments[1:])
}
