// Package httpapi exposes the Loomwork domain over JSON for the browser UI. It
// is a handler layer only: every behavior lives in internal/store and
// internal/orchestrator, which stay transport agnostic and are used unchanged.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/ilyaus/loomwork/internal/store"
)

// maxRequestBytes bounds a request body. The UI posts requirement text, not
// documents, so a small ceiling is enough to keep a malformed client from
// exhausting memory.
const maxRequestBytes = 1 << 20

// Options configures a Server.
type Options struct {
	// Store is the project store every handler reads and writes.
	Store *store.DirStore
	// Assets serves the browser UI. A nil value disables the UI routes, which
	// keeps handler tests independent of the built frontend.
	Assets fs.FS
	// Home is the workspace directory, reported by /api/workspace so the UI can
	// show which workspace it is editing.
	Home string
}

// Server routes JSON endpoints over a project store and serves the embedded UI.
type Server struct {
	store  *store.DirStore
	assets fs.FS
	home   string
}

// New validates options and builds a server.
func New(options Options) (*Server, error) {
	if options.Store == nil {
		return nil, fmt.Errorf("httpapi: a project store is required")
	}
	return &Server{store: options.Store, assets: options.Assets, home: options.Home}, nil
}

// Handler returns the routed handler: /api/... for JSON, everything else for the
// browser UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.routeAPI)
	if s.assets != nil {
		mux.Handle("/", s.uiHandler())
	}
	return mux
}

// routeAPI dispatches on the path segments after /api/. Go 1.21's ServeMux has no
// method or wildcard patterns, so routing is explicit.
func (s *Server) routeAPI(w http.ResponseWriter, r *http.Request) {
	segments, err := pathSegments(strings.TrimPrefix(r.URL.EscapedPath(), "/api/"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	switch {
	case len(segments) == 1 && segments[0] == "health":
		s.route(w, r, map[string]http.HandlerFunc{http.MethodGet: s.health})
	case len(segments) == 1 && segments[0] == "workspace":
		s.route(w, r, map[string]http.HandlerFunc{http.MethodGet: s.workspace})
	case len(segments) == 1 && segments[0] == "projects":
		s.route(w, r, map[string]http.HandlerFunc{
			http.MethodGet:  s.listProjects,
			http.MethodPost: s.createProject,
		})
	case len(segments) == 2 && segments[0] == "projects":
		s.route(w, r, map[string]http.HandlerFunc{
			http.MethodGet: s.projectHandler(s.getProject),
		})
	case len(segments) == 3 && segments[0] == "projects" && segments[2] == "sources":
		s.route(w, r, map[string]http.HandlerFunc{
			http.MethodGet:  s.projectHandler(s.listSources),
			http.MethodPost: s.projectHandler(s.addSource),
		})
	case len(segments) == 3 && segments[0] == "projects" && segments[2] == "requirements":
		s.route(w, r, map[string]http.HandlerFunc{
			http.MethodGet:  s.projectHandler(s.listRequirements),
			http.MethodPost: s.projectHandler(s.createRequirement),
		})
	case len(segments) == 4 && segments[0] == "projects" && segments[2] == "requirements":
		s.route(w, r, map[string]http.HandlerFunc{
			http.MethodGet:   s.requirementHandler(s.getRequirement),
			http.MethodPatch: s.requirementHandler(s.updateRequirement),
		})
	case len(segments) == 5 && segments[0] == "projects" && segments[2] == "requirements" && segments[4] == "history":
		s.route(w, r, map[string]http.HandlerFunc{
			http.MethodGet: s.requirementHandler(s.requirementHistory),
		})
	case len(segments) == 5 && segments[0] == "projects" && segments[2] == "requirements" && segments[4] == "status":
		s.route(w, r, map[string]http.HandlerFunc{
			http.MethodPost: s.requirementHandler(s.setRequirementStatus),
		})
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("no endpoint for %s", r.URL.Path))
	}
}

// route selects the handler for the request method and reports the allowed set
// otherwise.
func (s *Server) route(w http.ResponseWriter, r *http.Request, handlers map[string]http.HandlerFunc) {
	if handler, ok := handlers[r.Method]; ok {
		handler(w, r)
		return
	}
	allowed := make([]string, 0, len(handlers))
	for method := range handlers {
		allowed = append(allowed, method)
	}
	sort.Strings(allowed)
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed for %s", r.Method, r.URL.Path))
}

// projectHandler resolves the project reference in the path before the handler
// runs, so a bad reference is one 404 in one place.
func (s *Server) projectHandler(handler func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		segments, err := pathSegments(strings.TrimPrefix(r.URL.EscapedPath(), "/api/"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		handler(w, r, segments[1])
	}
}

// requirementHandler passes both the project reference and the requirement id.
func (s *Server) requirementHandler(handler func(http.ResponseWriter, *http.Request, string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		segments, err := pathSegments(strings.TrimPrefix(r.URL.EscapedPath(), "/api/"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		handler(w, r, segments[1], segments[3])
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// workspace reports which workspace the server edits, so a UI opened against the
// wrong home is obvious.
func (s *Server) workspace(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"home":        s.home,
		"projectsDir": s.store.Root(),
	})
}

// uiHandler serves the embedded assets, falling back to index.html so the
// single-page app owns client-side routes.
func (s *Server) uiHandler() http.Handler {
	files := http.FileServer(http.FS(s.assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(s.assets, name); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

// pathSegments splits and unescapes an already-trimmed path, rejecting empty
// segments so /api/projects//sources is a clear error rather than a lookup for
// an empty project reference.
func pathSegments(path string) ([]string, error) {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil, nil
	}
	raw := strings.Split(path, "/")
	segments := make([]string, 0, len(raw))
	for _, part := range raw {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			return nil, fmt.Errorf("path segment %q is not valid: %w", part, err)
		}
		if strings.TrimSpace(decoded) == "" {
			return nil, fmt.Errorf("path %q has an empty segment", path)
		}
		segments = append(segments, decoded)
	}
	return segments, nil
}

// decodeBody reads a JSON request body, rejecting unknown fields so a typo in a
// client payload fails loudly instead of being silently ignored.
func decodeBody(r *http.Request, out any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("parse request body: %w", err)
	}
	return nil
}

// versionParam reads the optional ?version=N selector; 0 means the current
// version, matching the store's convention.
func versionParam(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("version"))
	if raw == "" {
		return 0, nil
	}
	version, err := strconv.Atoi(raw)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("version %q must be an integer of 1 or greater", raw)
	}
	return version, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, `{"error":"encode response"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// writeStoreError maps a domain error to a status code: a missing project or
// requirement is a 404, anything else a rejected request.
func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusBadRequest, err)
}
