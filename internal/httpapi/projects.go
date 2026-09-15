package httpapi

import (
	"net/http"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
)

// ProjectSummary is one row of the directory-of-projects landing view. Counts
// come from the project document (DirStore caches them in project.json); the
// testability rollup reads the project's suites and reports, which is cheap on a
// local disk and keeps the landing view honest about coverage.
type ProjectSummary struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Description        string      `json:"description,omitempty"`
	Tags               []string    `json:"tags,omitempty"`
	Sources            int         `json:"sources"`
	Requirements       int         `json:"requirements"`
	ActiveRequirements int         `json:"activeRequirements"`
	Artifacts          int         `json:"artifacts"`
	CreatedAt          time.Time   `json:"createdAt"`
	UpdatedAt          time.Time   `json:"updatedAt"`
	Testability        Testability `json:"testability"`
}

func summarize(project *model.Project) ProjectSummary {
	summary := ProjectSummary{
		ID:          project.ID,
		Name:        project.Name,
		Description: project.Description,
		Tags:        project.Tags,
		Sources:     len(project.Sources),
		Artifacts:   len(project.Artifacts),
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
	}
	if project.Index != nil {
		summary.Requirements = project.Index.Requirements
		summary.ActiveRequirements = project.Index.ActiveRequirements
	}
	return summary
}

func (s *Server) listProjects(w http.ResponseWriter, _ *http.Request) {
	projects, err := s.store.List()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	summaries := make([]ProjectSummary, 0, len(projects))
	for _, project := range projects {
		summary := summarize(project)
		// A project whose suites or reports cannot be read still lists; its
		// rollup just stays unavailable.
		if report, err := s.testability(project.ID); err == nil {
			summary.Testability = report.Testability
		}
		summaries = append(summaries, summary)
	}
	writeJSON(w, http.StatusOK, summaries)
}

// createProjectRequest is the landing view's "new project" form.
type createProjectRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Tags        []string               `json:"tags"`
	Sources     []model.DocumentSource `json:"sources"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var request createProjectRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	project, err := model.NewProject(request.Name, request.Description, request.Tags)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	for _, source := range request.Sources {
		if _, err := project.AddSource(source); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if err := s.store.Create(project); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

// getProject returns the stored project document: metadata, document sources,
// cached counts, and artifacts.
func (s *Server) getProject(w http.ResponseWriter, _ *http.Request, ref string) {
	project, err := s.store.Resolve(ref)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) listSources(w http.ResponseWriter, _ *http.Request, ref string) {
	project, err := s.store.Resolve(ref)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	sources := project.Sources
	if sources == nil {
		sources = []model.DocumentSource{}
	}
	writeJSON(w, http.StatusOK, sources)
}

// addSource attaches a document source link. Posting a source whose name already
// exists updates that link, matching model.Project.AddSource and the CLI.
func (s *Server) addSource(w http.ResponseWriter, r *http.Request, ref string) {
	var source model.DocumentSource
	if err := decodeBody(r, &source); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	project, err := s.store.Update(ref, func(project *model.Project) error {
		_, err := project.AddSource(source)
		return err
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project.Sources)
}
