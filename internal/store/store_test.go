package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
)

func newStore(t *testing.T) *DirStore {
	t.Helper()
	dirStore, err := NewDirStore(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	var _ ProjectStore = dirStore
	return dirStore
}

func newProject(t *testing.T, name string) *model.Project {
	t.Helper()
	project, err := model.NewProject(name, "", nil)
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	return project
}

func TestNewDirStoreRequiresDirectory(t *testing.T) {
	if _, err := NewDirStore("  "); err == nil {
		t.Fatal("expected an error for an empty directory")
	}
}

func TestDirStoreCreateLoadAndRoundTripArtifacts(t *testing.T) {
	dirStore := newStore(t)
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

	if err := dirStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}

	loaded, err := dirStore.Load(project.ID)
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

func TestDirStoreCreateRejectsDuplicateNames(t *testing.T) {
	dirStore := newStore(t)
	if err := dirStore.Create(newProject(t, "alpha")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := dirStore.Create(newProject(t, "ALPHA"))
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("error = %v, want a case-insensitive duplicate name rejection", err)
	}
	if err := dirStore.Create(nil); err == nil {
		t.Fatal("expected an error creating a nil project")
	}
}

func TestDirStoreSaveOverwrites(t *testing.T) {
	dirStore := newStore(t)
	project := newProject(t, "alpha")
	if err := dirStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := project.AddArtifact(model.ArtifactSpec{
		Name: "spec.md", Type: model.ArtifactTypeSpec, Body: model.Body{Content: "spec"},
	}); err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	if err := dirStore.Save(project); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := dirStore.Load(project.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want the saved artifact", len(loaded.Artifacts))
	}
	if err := dirStore.Save(nil); err == nil {
		t.Fatal("expected an error saving a nil project")
	}
}

func TestDirStoreResolveByIDAndName(t *testing.T) {
	dirStore := newStore(t)
	project := newProject(t, "Alpha")
	if err := dirStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}

	byID, err := dirStore.Resolve(project.ID)
	if err != nil {
		t.Fatalf("Resolve by id: %v", err)
	}
	byName, err := dirStore.Resolve("alpha")
	if err != nil {
		t.Fatalf("Resolve by name: %v", err)
	}
	if byID.ID != project.ID || byName.ID != project.ID {
		t.Errorf("resolved = %q/%q, want %q", byID.ID, byName.ID, project.ID)
	}
	if _, err := dirStore.Resolve("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Resolve error = %v, want ErrNotFound", err)
	}
	if _, err := dirStore.Load("prj-missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load error = %v, want ErrNotFound", err)
	}
}

func TestDirStoreListIgnoresNonProjectFilesAndSortsByName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	dirStore, err := NewDirStore(dir)
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	for _, name := range []string{"zeta", "alpha", "mid"} {
		if err := dirStore.Create(newProject(t, name)); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("make stray dir: %v", err)
	}

	projects, err := dirStore.List()
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

func TestDirStoreLoadReportsMalformedJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	dirStore, err := NewDirStore(dir)
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prj-bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write malformed project: %v", err)
	}
	if _, err := dirStore.Load("prj-bad"); err == nil || !strings.Contains(err.Error(), "parse project") {
		t.Fatalf("error = %v, want a parse failure", err)
	}
}

func TestDirStoreWriteLeavesNoTempFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	dirStore, err := NewDirStore(dir)
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	project := newProject(t, "alpha")
	if err := dirStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != project.ID || !entries[0].IsDir() {
		t.Fatalf("entries = %v, want only the project directory", entries)
	}
	projectEntries, err := os.ReadDir(filepath.Join(dir, project.ID))
	if err != nil {
		t.Fatalf("ReadDir project: %v", err)
	}
	for _, entry := range projectEntries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("entry %q is a leftover temp file", entry.Name())
		}
	}
}

// separateStores models independent processes: each gets its own DirStore over
// the same directory, so the in-process mutex cannot serialize them and only the
// lock file can.
func separateStores(t *testing.T, dir string, count int) []*DirStore {
	t.Helper()
	stores := make([]*DirStore, 0, count)
	for i := 0; i < count; i++ {
		dirStore, err := NewDirStore(dir)
		if err != nil {
			t.Fatalf("NewDirStore: %v", err)
		}
		stores = append(stores, dirStore)
	}
	return stores
}

func TestUpdateSerializesConcurrentWriters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	writers := separateStores(t, dir, 8)
	if err := writers[0].Create(newProject(t, "conc")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var group sync.WaitGroup
	errs := make(chan error, len(writers))
	for i, writer := range writers {
		group.Add(1)
		go func(index int, dirStore *DirStore) {
			defer group.Done()
			_, err := dirStore.Update("conc", func(project *model.Project) error {
				_, addErr := project.AddArtifact(model.ArtifactSpec{
					Name: fmt.Sprintf("a%d", index),
					Type: model.ArtifactTypeDoc,
					Body: model.Body{Content: "body"},
				})
				return addErr
			})
			errs <- err
		}(i, writer)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
	}

	project, err := writers[0].FindByName("conc")
	if err != nil {
		t.Fatalf("FindByName: %v", err)
	}
	if len(project.Artifacts) != len(writers) {
		t.Fatalf("artifacts = %d, want every concurrent write persisted (%d)", len(project.Artifacts), len(writers))
	}
}

func TestCreateRejectsDuplicateNamesUnderConcurrency(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	writers := separateStores(t, dir, 6)

	var group sync.WaitGroup
	var created int64
	for _, writer := range writers {
		group.Add(1)
		go func(dirStore *DirStore) {
			defer group.Done()
			if err := dirStore.Create(newProject(t, "dup")); err == nil {
				atomic.AddInt64(&created, 1)
			}
		}(writer)
	}
	group.Wait()

	if created != 1 {
		t.Fatalf("successful creates = %d, want exactly one", created)
	}
	projects, err := writers[0].List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want a single project named dup", len(projects))
	}
}

func TestUpdateLeavesProjectUntouchedWhenMutateFails(t *testing.T) {
	dirStore := newStore(t)
	if err := dirStore.Create(newProject(t, "alpha")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	sentinel := errors.New("nope")
	if _, err := dirStore.Update("alpha", func(project *model.Project) error {
		_, _ = project.AddArtifact(model.ArtifactSpec{Name: "a", Type: model.ArtifactTypeDoc, Body: model.Body{Content: "x"}})
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want the mutate error", err)
	}
	project, err := dirStore.FindByName("alpha")
	if err != nil {
		t.Fatalf("FindByName: %v", err)
	}
	if len(project.Artifacts) != 0 {
		t.Fatalf("artifacts = %d, want the stored project untouched", len(project.Artifacts))
	}
	if _, err := dirStore.Update("missing", func(*model.Project) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if _, err := dirStore.Update("alpha", nil); err == nil {
		t.Fatal("Update(nil mutate) = nil, want an error")
	}
}

func TestLockIsReleasedAfterEveryWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	dirStore, err := NewDirStore(dir)
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	if err := dirStore.Create(newProject(t, "alpha")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := dirStore.Update("alpha", func(*model.Project) error { return nil }); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, lockFileName)); !os.IsNotExist(err) {
		t.Fatalf("stat lock file = %v, want it removed", err)
	}
}

func TestLockBreaksStaleLockFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	dirStore, err := NewDirStore(dir)
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	lockPath := filepath.Join(dir, lockFileName)
	if err := os.WriteFile(lockPath, []byte("999999"), 0o600); err != nil {
		t.Fatalf("write lock file: %v", err)
	}
	stale := time.Now().Add(-2 * lockStaleAfter)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if err := dirStore.Create(newProject(t, "alpha")); err != nil {
		t.Fatalf("Create over a stale lock: %v", err)
	}
}
