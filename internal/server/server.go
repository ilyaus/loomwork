// Package server exposes the orchestration layer over HTTP: a JSON API plus the
// embedded web UI. It contains request decoding and response encoding only; all
// behavior lives in the packages the CLI already uses, so the two transports
// stay equivalent.
package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/ilyaus/loomwork/internal/config"
	"github.com/ilyaus/loomwork/internal/cuenote"
	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/orchestrator"
	"github.com/ilyaus/loomwork/internal/preset"
	"github.com/ilyaus/loomwork/internal/provider"
	"github.com/ilyaus/loomwork/internal/store"
)

//go:embed ui
var uiFiles embed.FS

// Server handles the JSON API and serves the embedded UI.
type Server struct {
	home    string
	config  config.Config
	store   store.ProjectStore
	presets *preset.Registry
	cues    cuenote.Client
	engine  *orchestrator.Orchestrator
	mux     *http.ServeMux
}

// New wires a server from an opened workspace.
func New(home string, cfg config.Config, projects store.ProjectStore, presets *preset.Registry, cues cuenote.Client) *Server {
	s := &Server{
		home:    home,
		config:  cfg,
		store:   projects,
		presets: presets,
		cues:    cues,
		engine:  orchestrator.New(cfg, projects, presets, nil),
		mux:     http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/workspace", s.handleWorkspace)
	s.mux.HandleFunc("/api/projects", s.handleProjects)
	s.mux.HandleFunc("/api/projects/", s.handleProjectSubtree)
	s.mux.HandleFunc("/api/cues", s.handleCues)
	s.mux.HandleFunc("/api/cues/", s.handleCue)
	s.mux.HandleFunc("/api/run", s.handleRun)

	ui, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		panic(fmt.Sprintf("embedded ui filesystem: %v", err))
	}
	s.mux.Handle("/", http.FileServer(http.FS(ui)))
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

/* ── helpers ─────────────────────────────────────────────── */

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, apiError{Error: err.Error()})
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, cuenote.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, provider.ErrNotImplemented):
		return http.StatusNotImplemented
	default:
		return http.StatusBadRequest
	}
}

func decodeBody(r *http.Request, into any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
}

/* ── workspace ───────────────────────────────────────────── */

type providerView struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	BaseURL      string   `json:"baseUrl,omitempty"`
	DefaultModel string   `json:"defaultModel,omitempty"`
	Status       string   `json:"status"`
	Presets      []string `json:"presets,omitempty"`
	Reachable    *bool    `json:"reachable,omitempty"`
	Models       []string `json:"models,omitempty"`
}

func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	views := make([]providerView, 0, len(s.config.Providers))
	for _, name := range s.config.ProviderNames() {
		declared := s.config.Providers[name]
		status := "configured"
		switch declared.Kind {
		case provider.KindAzure, provider.KindBedrock:
			status = "scaffold"
		}
		view := providerView{
			Name:         name,
			Kind:         string(declared.Kind),
			BaseURL:      declared.BaseURL,
			DefaultModel: declared.DefaultModel,
			Status:       status,
			Presets:      s.presets.PresetNames(declared.Kind, preset.WildcardModel),
		}
		if r.URL.Query().Get("probe") == "true" && status == "configured" {
			if generator, err := provider.BuildTextGenerator(declared); err == nil {
				ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				models, listErr := generator.Models(ctx)
				cancel()
				reachable := listErr == nil
				view.Reachable = &reachable
				for _, m := range models {
					view.Models = append(view.Models, m.ID)
				}
			}
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"home":          s.home,
		"providers":     views,
		"presetGroups":  s.presets.Keys(),
		"artifactTypes": model.ArtifactTypes(),
	})
}

/* ── projects ────────────────────────────────────────────── */

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projects, err := s.store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		summaries := make([]map[string]any, 0, len(projects))
		for _, project := range projects {
			summaries = append(summaries, projectSummary(project))
		}
		writeJSON(w, http.StatusOK, map[string]any{"projects": summaries})
	case http.MethodPost:
		var body struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Tags        []string `json:"tags"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		project, err := model.NewProject(body.Name, body.Description, body.Tags)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.store.Create(project); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, http.StatusCreated, project)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func projectSummary(project *model.Project) map[string]any {
	pinned := 0
	for _, artifact := range project.Artifacts {
		if artifact.Pinned {
			pinned++
		}
	}
	return map[string]any{
		"id":          project.ID,
		"name":        project.Name,
		"description": project.Description,
		"tags":        project.Tags,
		"createdAt":   project.CreatedAt,
		"updatedAt":   project.UpdatedAt,
		"artifacts":   len(project.Artifacts),
		"names":       len(project.LatestArtifacts()),
		"pinned":      pinned,
	}
}

// handleProjectSubtree routes /api/projects/{ref}[/artifacts[/{ref}[/pin]]].
func (s *Server) handleProjectSubtree(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	segments := strings.Split(strings.Trim(rest, "/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		writeError(w, http.StatusNotFound, fmt.Errorf("project reference is required"))
		return
	}
	projectRef := segments[0]

	switch {
	case len(segments) == 1:
		s.handleProject(w, r, projectRef)
	case len(segments) == 2 && segments[1] == "artifacts":
		s.handleArtifacts(w, r, projectRef)
	case len(segments) == 3 && segments[1] == "artifacts":
		s.handleArtifact(w, r, projectRef, segments[2])
	case len(segments) == 4 && segments[1] == "artifacts" && segments[3] == "pin":
		s.handlePin(w, r, projectRef, segments[2])
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("no such resource"))
	}
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request, ref string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	project, err := s.store.Resolve(ref)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request, projectRef string) {
	switch r.Method {
	case http.MethodGet:
		project, err := s.store.Resolve(projectRef)
		if err != nil {
			writeError(w, statusFor(err), err)
			return
		}
		if r.URL.Query().Get("all") == "true" {
			writeJSON(w, http.StatusOK, map[string]any{"artifacts": project.Artifacts})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"artifacts": project.LatestArtifacts()})
	case http.MethodPost:
		var body struct {
			Name     string            `json:"name"`
			Type     string            `json:"type"`
			Content  string            `json:"content"`
			Ref      string            `json:"ref"`
			Tags     []string          `json:"tags"`
			Pinned   bool              `json:"pinned"`
			Metadata map[string]string `json:"metadata"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		artifactType, err := model.ParseArtifactType(body.Type)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		var created model.Artifact
		_, err = s.store.Update(projectRef, func(project *model.Project) error {
			artifact, addErr := project.AddArtifact(model.ArtifactSpec{
				Name:     body.Name,
				Type:     artifactType,
				Body:     model.Body{Content: body.Content, Ref: body.Ref},
				Tags:     body.Tags,
				Pinned:   body.Pinned,
				Metadata: body.Metadata,
			})
			created = artifact
			return addErr
		})
		if err != nil {
			writeError(w, statusFor(err), err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request, projectRef, artifactRef string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	project, err := s.store.Resolve(projectRef)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	artifact, ok := project.ResolveArtifact(artifactRef)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("artifact %q not found in project %q", artifactRef, project.Name))
		return
	}
	content, err := orchestrator.ArtifactContent(artifact)
	if err != nil {
		content = fmt.Sprintf("(unavailable: %v)", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"artifact": artifact,
		"content":  content,
		"history":  project.ArtifactHistory(artifact.Name),
	})
}

func (s *Server) handlePin(w http.ResponseWriter, r *http.Request, projectRef, artifactID string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var body struct {
		Pinned bool `json:"pinned"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var updated model.Artifact
	_, err := s.store.Update(projectRef, func(project *model.Project) error {
		artifact, pinErr := project.SetPinned(artifactID, body.Pinned)
		updated = artifact
		return pinErr
	})
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

/* ── cues ────────────────────────────────────────────────── */

func (s *Server) handleCues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	query := r.URL.Query()
	cues, err := s.cues.ListCues(r.Context(), cuenote.CueFilter{
		Search: query.Get("search"),
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"cues": []cuenote.Cue{}, "unavailable": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cues": cues})
}

func (s *Server) handleCue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	ref := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/cues/"), "/")
	cue, err := cuenote.Resolve(r.Context(), s.cues, ref)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, cue)
}

/* ── runs ────────────────────────────────────────────────── */

type runBody struct {
	ProjectRef    string            `json:"projectRef"`
	ArtifactRef   string            `json:"artifactRef"`
	Selector      string            `json:"selector"`
	Prompt        string            `json:"prompt"`
	CueRef        string            `json:"cueRef"`
	Variables     map[string]string `json:"variables"`
	SystemPrompt  string            `json:"systemPrompt"`
	OutputName    string            `json:"outputName"`
	OutputType    string            `json:"outputType"`
	Tags          []string          `json:"tags"`
	Pin           bool              `json:"pin"`
	IncludePinned bool              `json:"includePinned"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var body runBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	hasPrompt := strings.TrimSpace(body.Prompt) != ""
	hasCue := strings.TrimSpace(body.CueRef) != ""
	if hasPrompt == hasCue {
		writeError(w, http.StatusBadRequest, fmt.Errorf("supply exactly one of prompt or cueRef"))
		return
	}

	prompt := body.Prompt
	metadata := map[string]string{}
	if hasCue {
		cue, err := cuenote.Resolve(r.Context(), s.cues, body.CueRef)
		if err != nil {
			writeError(w, statusFor(err), err)
			return
		}
		rendered, err := cue.Render(body.Variables)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		prompt = rendered
		metadata["cue"] = cue.Name
		metadata["cueId"] = cue.ID
	}

	var outputType model.ArtifactType
	if strings.TrimSpace(body.OutputType) != "" {
		parsed, err := model.ParseArtifactType(body.OutputType)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		outputType = parsed
	}

	result, err := s.engine.RunPrompt(r.Context(), orchestrator.RunRequest{
		ProjectRef:    body.ProjectRef,
		ArtifactRef:   body.ArtifactRef,
		Selector:      body.Selector,
		Prompt:        prompt,
		SystemPrompt:  body.SystemPrompt,
		OutputName:    body.OutputName,
		OutputType:    outputType,
		Tags:          body.Tags,
		Pin:           body.Pin,
		IncludePinned: body.IncludePinned,
		Metadata:      metadata,
	})
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
