package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	fileStore, err := NewFileStore(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	var _ ProjectStore = fileStore
	return fileStore
}

func newProject(t *testing.T, name string) *model.Project {
	t.Helper()
	project, err := model.NewProject(name, "", nil)
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	return project
}

func TestNewFileStoreRequiresDirectory(t *testing.T) {
	if _, err := NewFileStore("  "); err == nil {
		t.Fatal("expected an error for an empty directory")
	}
}

func TestFileStoreCreateLoadAndRoundTripArtifacts(t *testing.T) {
	fileStore := newStore(t)
	project := newProject(t, "Log Triage")
	if _, err := project.AddArtifact(model.ArtifactSpec{
		Name:   "api.log",
		Type:   model.ArtifactTypeLog,
		Body:   model.Body{Content: "line one", MediaType: "text/plain"},
		Tags:   []string{"prod"},
		Pinned: true,
	}); err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}

	if err := fileStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}

	loaded, err := fileStore.Load(project.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Name != "Log Triage" || len(loaded.Artifacts) != 1 {
		t.Fatalf("loaded = %+v, want the persisted project with one artifact", loaded)
	}
	artifact := loaded.Artifacts[0]
	if artifact.Body.Content != "line one" || artifact.Type != model.ArtifactTypeLog || !artifact.Pinned || artifact.Version != 1 {
		t.Errorf("artifact = %+v, want the persisted artifact fields", artifact)
	}
	if !loaded.CreatedAt.Equal(project.CreatedAt) {
		t.Errorf("createdAt = %v, want %v", loaded.CreatedAt, project.CreatedAt)
	}
}

func TestFileStoreCreateRejectsDuplicateNames(t *testing.T) {
	fileStore := newStore(t)
	if err := fileStore.Create(newProject(t, "alpha")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := fileStore.Create(newProject(t, "ALPHA"))
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("error = %v, want a case-insensitive duplicate name rejection", err)
	}
	if err := fileStore.Create(nil); err == nil {
		t.Fatal("expected an error creating a nil project")
	}
}

func TestFileStoreSaveOverwrites(t *testing.T) {
	fileStore := newStore(t)
	project := newProject(t, "alpha")
	if err := fileStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := project.AddArtifact(model.ArtifactSpec{
		Name: "spec.md", Type: model.ArtifactTypeSpec, Body: model.Body{Content: "spec"},
	}); err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	if err := fileStore.Save(project); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := fileStore.Load(project.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want the saved artifact", len(loaded.Artifacts))
	}
	if err := fileStore.Save(nil); err == nil {
		t.Fatal("expected an error saving a nil project")
	}
}

func TestFileStoreResolveByIDAndName(t *testing.T) {
	fileStore := newStore(t)
	project := newProject(t, "Alpha")
	if err := fileStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}

	byID, err := fileStore.Resolve(project.ID)
	if err != nil {
		t.Fatalf("Resolve by id: %v", err)
	}
	byName, err := fileStore.Resolve("alpha")
	if err != nil {
		t.Fatalf("Resolve by name: %v", err)
	}
	if byID.ID != project.ID || byName.ID != project.ID {
		t.Errorf("resolved = %q/%q, want %q", byID.ID, byName.ID, project.ID)
	}
	if _, err := fileStore.Resolve("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Resolve error = %v, want ErrNotFound", err)
	}
	if _, err := fileStore.Load("prj-missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load error = %v, want ErrNotFound", err)
	}
}

func TestFileStoreListIgnoresNonProjectFilesAndSortsByName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	fileStore, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	for _, name := range []string{"zeta", "alpha", "mid"} {
		if err := fileStore.Create(newProject(t, name)); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("make stray dir: %v", err)
	}

	projects, err := fileStore.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make([]string, 0, len(projects))
	for _, project := range projects {
		names = append(names, project.Name)
	}
	if strings.Join(names, ",") != "alpha,mid,zeta" {
		t.Fatalf("names = %v, want them sorted by name", names)
	}
}

func TestFileStoreLoadReportsMalformedJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	fileStore, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prj-bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write malformed project: %v", err)
	}
	if _, err := fileStore.Load("prj-bad"); err == nil || !strings.Contains(err.Error(), "parse project") {
		t.Fatalf("error = %v, want a parse failure", err)
	}
}

func TestFileStoreWriteLeavesNoTempFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	fileStore, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := fileStore.Create(newProject(t, "alpha")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".json") {
		t.Fatalf("entries = %v, want only the project document", entries)
	}
}
