package projectimport

import (
	"fmt"
	"strings"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
)

type Request struct {
	Format   string   `json:"format"`
	Path     string   `json:"path"`
	Name     string   `json:"name,omitempty"`
	Features []string `json:"features,omitempty"`
}

type Feature struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Requirements  int    `json:"requirements"`
	Artifacts     int    `json:"artifacts"`
	TestDocuments int    `json:"testDocuments"`
}

type Preview struct {
	Format        string    `json:"format"`
	SourcePath    string    `json:"sourcePath"`
	Name          string    `json:"name"`
	Features      []Feature `json:"features"`
	Requirements  int       `json:"requirements"`
	Artifacts     int       `json:"artifacts"`
	TestDocuments int       `json:"testDocuments"`
	Warnings      []string  `json:"warnings"`
}

type Snapshot struct {
	Preview      Preview
	Requirements []model.RequirementSpec
	Artifacts    []model.ArtifactSpec
}

type Adapter interface {
	Read(path string, features []string) (*Snapshot, error)
}

type SnapshotStore interface {
	CreateWithRequirements(project *model.Project, requirements []model.RequirementSpec) error
}

type Service struct {
	store    SnapshotStore
	adapters map[string]Adapter
}

type Result struct {
	Project *model.Project `json:"project"`
	Preview Preview        `json:"preview"`
}

func New(store SnapshotStore) *Service {
	return &Service{store: store, adapters: map[string]Adapter{"spec-kit": SpecKit{}}}
}

func (s *Service) read(request Request) (*Snapshot, error) {
	format := strings.TrimSpace(request.Format)
	adapter, ok := s.adapters[format]
	if !ok {
		return nil, fmt.Errorf("unsupported project format %q", format)
	}
	snapshot, err := adapter.Read(strings.TrimSpace(request.Path), request.Features)
	if err != nil {
		return nil, err
	}
	snapshot.Preview.Format = format
	if name := strings.TrimSpace(request.Name); name != "" {
		snapshot.Preview.Name = name
	}
	return snapshot, nil
}

func (s *Service) Preview(request Request) (*Preview, error) {
	snapshot, err := s.read(request)
	if err != nil {
		return nil, err
	}
	return &snapshot.Preview, nil
}

func (s *Service) Import(request Request) (*Result, error) {
	if s.store == nil {
		return nil, fmt.Errorf("project import needs a store")
	}
	snapshot, err := s.read(request)
	if err != nil {
		return nil, err
	}
	project, err := model.NewProject(snapshot.Preview.Name, "Imported "+snapshot.Preview.Format+" snapshot", []string{snapshot.Preview.Format})
	if err != nil {
		return nil, err
	}
	features := make([]string, 0, len(snapshot.Preview.Features))
	for _, feature := range snapshot.Preview.Features {
		features = append(features, feature.ID)
	}
	project.Import = &model.ProjectImport{
		Format: snapshot.Preview.Format, SourcePath: snapshot.Preview.SourcePath, ImportedAt: time.Now().UTC(), Features: features, RequirementsReadOnly: true,
	}
	if _, err := project.AddSource(model.DocumentSource{Name: snapshot.Preview.Format, Type: model.SourceTypeOther, LocalPath: snapshot.Preview.SourcePath}); err != nil {
		return nil, err
	}
	for _, artifact := range snapshot.Artifacts {
		if _, err := project.AddArtifact(artifact); err != nil {
			return nil, err
		}
	}
	if err := s.store.CreateWithRequirements(project, snapshot.Requirements); err != nil {
		return nil, err
	}
	return &Result{Project: project, Preview: snapshot.Preview}, nil
}
