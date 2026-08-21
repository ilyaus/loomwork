package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// nowFunc is indirected so tests can produce deterministic timestamps.
var nowFunc = time.Now

// Project is a named container of versioned artifacts, requirements, and the
// documentation sources they were derived from.
type Project struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Tags        []string         `json:"tags,omitempty"`
	Sources     []DocumentSource `json:"sources,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	Artifacts   []Artifact       `json:"artifacts"`
	// Index caches counts derived from the project directory so a landing view
	// can summarize many projects without scanning each one's subfolders.
	Index *ProjectIndex `json:"index,omitempty"`
}

// ProjectIndex is the cached summary a store maintains in the project document.
type ProjectIndex struct {
	Requirements       int `json:"requirements"`
	ActiveRequirements int `json:"activeRequirements"`
}

// NewProject creates an empty project.
func NewProject(name, description string, tags []string) (*Project, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("project name is required")
	}
	now := nowFunc().UTC()
	return &Project{
		ID:          NewID("prj"),
		Name:        strings.TrimSpace(name),
		Description: description,
		Tags:        normalizeTags(tags),
		CreatedAt:   now,
		UpdatedAt:   now,
		Artifacts:   []Artifact{},
	}, nil
}

// AddSource attaches a document source link, replacing an existing source with
// the same name so re-adding one updates it instead of duplicating it.
func (p *Project) AddSource(source DocumentSource) (DocumentSource, error) {
	normalized, err := source.normalize()
	if err != nil {
		return DocumentSource{}, err
	}
	for i := range p.Sources {
		if strings.EqualFold(p.Sources[i].Name, normalized.Name) {
			p.Sources[i] = normalized
			p.touch()
			return normalized, nil
		}
	}
	p.Sources = append(p.Sources, normalized)
	p.touch()
	return normalized, nil
}

// AddArtifact appends a new artifact. If an artifact with the same name already
// exists, the new one becomes the next revision of that name and its parent is
// the previous latest revision.
func (p *Project) AddArtifact(spec ArtifactSpec) (Artifact, error) {
	if err := spec.validate(); err != nil {
		return Artifact{}, fmt.Errorf("invalid artifact %q: %w", spec.Name, err)
	}

	name := strings.TrimSpace(spec.Name)
	version := 1
	parentID := ""
	if previous, ok := p.LatestArtifact(name); ok {
		if previous.Type != spec.Type {
			return Artifact{}, fmt.Errorf("artifact %q already exists with type %q: cannot add revision with type %q", name, previous.Type, spec.Type)
		}
		version = previous.Version + 1
		parentID = previous.ID
	}

	artifact := Artifact{
		ID:        NewID("art"),
		Name:      name,
		Type:      spec.Type,
		Version:   version,
		Tags:      normalizeTags(spec.Tags),
		Pinned:    spec.Pinned,
		ParentID:  parentID,
		Body:      spec.Body,
		Metadata:  copyMetadata(spec.Metadata),
		CreatedAt: nowFunc().UTC(),
	}
	p.Artifacts = append(p.Artifacts, artifact)
	p.touch()
	return artifact, nil
}

// DeriveArtifact appends an artifact produced from an existing one (for example
// the result of a prompt run). The parent is the source artifact regardless of
// name, so lineage crosses artifact names.
func (p *Project) DeriveArtifact(parentID string, spec ArtifactSpec) (Artifact, error) {
	parent, ok := p.ArtifactByID(parentID)
	if !ok {
		return Artifact{}, fmt.Errorf("parent artifact %q not found in project %q", parentID, p.Name)
	}
	if _, err := p.AddArtifact(spec); err != nil {
		return Artifact{}, err
	}
	// AddArtifact links to the previous revision of the same name; an explicit
	// derivation overrides that with the true source artifact.
	index := len(p.Artifacts) - 1
	p.Artifacts[index].ParentID = parent.ID
	p.touch()
	return p.Artifacts[index], nil
}

// ArtifactByID looks up a single artifact revision.
func (p *Project) ArtifactByID(id string) (Artifact, bool) {
	for _, artifact := range p.Artifacts {
		if artifact.ID == id {
			return artifact, true
		}
	}
	return Artifact{}, false
}

// LatestArtifact returns the highest-versioned artifact for a name.
func (p *Project) LatestArtifact(name string) (Artifact, bool) {
	name = strings.TrimSpace(name)
	var latest Artifact
	found := false
	for _, artifact := range p.Artifacts {
		if artifact.Name != name {
			continue
		}
		if !found || artifact.Version > latest.Version {
			latest = artifact
			found = true
		}
	}
	return latest, found
}

// ArtifactHistory returns every revision of a name ordered by version.
func (p *Project) ArtifactHistory(name string) []Artifact {
	name = strings.TrimSpace(name)
	history := make([]Artifact, 0, 4)
	for _, artifact := range p.Artifacts {
		if artifact.Name == name {
			history = append(history, artifact)
		}
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Version < history[j].Version })
	return history
}

// LatestArtifacts returns the newest revision of every artifact name, ordered by
// name.
func (p *Project) LatestArtifacts() []Artifact {
	byName := map[string]Artifact{}
	for _, artifact := range p.Artifacts {
		current, ok := byName[artifact.Name]
		if !ok || artifact.Version > current.Version {
			byName[artifact.Name] = artifact
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	latest := make([]Artifact, 0, len(names))
	for _, name := range names {
		latest = append(latest, byName[name])
	}
	return latest
}

// ResolveArtifact finds an artifact by id, or by name (returning its latest
// revision).
func (p *Project) ResolveArtifact(ref string) (Artifact, bool) {
	if artifact, ok := p.ArtifactByID(ref); ok {
		return artifact, true
	}
	return p.LatestArtifact(ref)
}

// SetPinned pins or unpins a specific artifact revision.
func (p *Project) SetPinned(id string, pinned bool) (Artifact, error) {
	for i := range p.Artifacts {
		if p.Artifacts[i].ID != id {
			continue
		}
		p.Artifacts[i].Pinned = pinned
		p.touch()
		return p.Artifacts[i], nil
	}
	return Artifact{}, fmt.Errorf("artifact %q not found in project %q", id, p.Name)
}

// PinnedArtifacts returns every pinned artifact revision, oldest first. These
// form the standing context supplied to prompt runs.
func (p *Project) PinnedArtifacts() []Artifact {
	pinned := make([]Artifact, 0, 4)
	for _, artifact := range p.Artifacts {
		if artifact.Pinned {
			pinned = append(pinned, artifact)
		}
	}
	sort.Slice(pinned, func(i, j int) bool {
		if pinned[i].Name != pinned[j].Name {
			return pinned[i].Name < pinned[j].Name
		}
		return pinned[i].Version < pinned[j].Version
	})
	return pinned
}

// AddTags merges tags into an artifact revision.
func (p *Project) AddTags(id string, tags []string) (Artifact, error) {
	for i := range p.Artifacts {
		if p.Artifacts[i].ID != id {
			continue
		}
		p.Artifacts[i].Tags = normalizeTags(append(p.Artifacts[i].Tags, tags...))
		p.touch()
		return p.Artifacts[i], nil
	}
	return Artifact{}, fmt.Errorf("artifact %q not found in project %q", id, p.Name)
}

func (p *Project) touch() {
	p.UpdatedAt = nowFunc().UTC()
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, duplicate := seen[tag]; duplicate {
			continue
		}
		seen[tag] = struct{}{}
		normalized = append(normalized, tag)
	}
	sort.Strings(normalized)
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func copyMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	copied := make(map[string]string, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	return copied
}
