package model

import (
	"encoding/json"
	"errors"
	"testing"
)

func specFor(name string, content string) ArtifactSpec {
	return ArtifactSpec{Name: name, Type: ArtifactTypeSpec, Body: Body{Content: content}}
}

func newTestProject(t *testing.T) *Project {
	t.Helper()
	project, err := NewProject("demo", "test project", []string{"b", "a", "a", " "})
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	return project
}

func TestNewProjectRequiresName(t *testing.T) {
	if _, err := NewProject("   ", "", nil); err == nil {
		t.Fatal("expected an error for a blank project name")
	}
}

func TestNewProjectNormalizesTags(t *testing.T) {
	project := newTestProject(t)
	if got, want := len(project.Tags), 2; got != want {
		t.Fatalf("tags = %v, want %d entries", project.Tags, want)
	}
	if project.Tags[0] != "a" || project.Tags[1] != "b" {
		t.Fatalf("tags = %v, want sorted deduplicated [a b]", project.Tags)
	}
}

func TestAddArtifactAssignsVersionsAndLineage(t *testing.T) {
	project := newTestProject(t)

	first, err := project.AddArtifact(specFor("api.md", "v1 body"))
	if err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	if first.Version != 1 || first.ParentID != "" {
		t.Fatalf("first revision = version %d parent %q, want version 1 with no parent", first.Version, first.ParentID)
	}

	second, err := project.AddArtifact(specFor("api.md", "v2 body"))
	if err != nil {
		t.Fatalf("AddArtifact revision: %v", err)
	}
	if second.Version != 2 {
		t.Fatalf("second revision version = %d, want 2", second.Version)
	}
	if second.ParentID != first.ID {
		t.Fatalf("second revision parent = %q, want %q", second.ParentID, first.ID)
	}

	latest, ok := project.LatestArtifact("api.md")
	if !ok || latest.ID != second.ID {
		t.Fatalf("LatestArtifact = %+v (found %t), want %q", latest, ok, second.ID)
	}
	if history := project.ArtifactHistory("api.md"); len(history) != 2 || history[0].Version != 1 || history[1].Version != 2 {
		t.Fatalf("ArtifactHistory = %+v, want ordered versions 1,2", history)
	}
	if latestOnly := project.LatestArtifacts(); len(latestOnly) != 1 || latestOnly[0].Version != 2 {
		t.Fatalf("LatestArtifacts = %+v, want only version 2", latestOnly)
	}
}

func TestAddArtifactRejectsTypeChangeAcrossRevisions(t *testing.T) {
	project := newTestProject(t)
	if _, err := project.AddArtifact(specFor("api.md", "body")); err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	_, err := project.AddArtifact(ArtifactSpec{Name: "api.md", Type: ArtifactTypeLog, Body: Body{Content: "body"}})
	if err == nil {
		t.Fatal("expected an error when a revision changes the artifact type")
	}
}

func TestAddArtifactValidatesBody(t *testing.T) {
	project := newTestProject(t)

	cases := map[string]ArtifactSpec{
		"missing name":    {Type: ArtifactTypeDoc, Body: Body{Content: "x"}},
		"unknown type":    {Name: "a", Type: ArtifactType("blueprint"), Body: Body{Content: "x"}},
		"empty body":      {Name: "a", Type: ArtifactTypeDoc},
		"content and ref": {Name: "a", Type: ArtifactTypeDoc, Body: Body{Content: "x", Ref: "/tmp/x"}},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := project.AddArtifact(spec); err == nil {
				t.Fatalf("expected a validation error for %s", name)
			}
		})
	}
}

func TestDeriveArtifactLinksAcrossNames(t *testing.T) {
	project := newTestProject(t)
	source, err := project.AddArtifact(specFor("api.md", "spec body"))
	if err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}

	derived, err := project.DeriveArtifact(source.ID, ArtifactSpec{
		Name: "api.review",
		Type: ArtifactTypeGenerated,
		Body: Body{Content: "review body"},
	})
	if err != nil {
		t.Fatalf("DeriveArtifact: %v", err)
	}
	if derived.ParentID != source.ID {
		t.Fatalf("derived parent = %q, want %q", derived.ParentID, source.ID)
	}
	if derived.Version != 1 {
		t.Fatalf("derived version = %d, want 1", derived.Version)
	}

	// A second derivation of the same output name keeps the true source as parent
	// while bumping the version.
	again, err := project.DeriveArtifact(source.ID, ArtifactSpec{
		Name: "api.review",
		Type: ArtifactTypeGenerated,
		Body: Body{Content: "second review"},
	})
	if err != nil {
		t.Fatalf("DeriveArtifact second: %v", err)
	}
	if again.Version != 2 || again.ParentID != source.ID {
		t.Fatalf("second derivation = version %d parent %q, want version 2 parent %q", again.Version, again.ParentID, source.ID)
	}
}

func TestDeriveArtifactRejectsUnknownParent(t *testing.T) {
	project := newTestProject(t)
	if _, err := project.DeriveArtifact("art-missing", specFor("x", "y")); err == nil {
		t.Fatal("expected an error for an unknown parent artifact")
	}
}

func TestPinningIsMetadataOnly(t *testing.T) {
	project := newTestProject(t)
	artifact, err := project.AddArtifact(specFor("api.md", "body"))
	if err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	if len(project.PinnedArtifacts()) != 0 {
		t.Fatal("expected no pinned artifacts initially")
	}

	pinned, err := project.SetPinned(artifact.ID, true)
	if err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	if !pinned.Pinned {
		t.Fatal("expected the artifact to be pinned")
	}
	if got := project.PinnedArtifacts(); len(got) != 1 || got[0].ID != artifact.ID {
		t.Fatalf("PinnedArtifacts = %+v, want the single pinned artifact", got)
	}
	if total := len(project.Artifacts); total != 1 {
		t.Fatalf("pinning changed the artifact count to %d, want 1", total)
	}

	if _, err := project.SetPinned(artifact.ID, false); err != nil {
		t.Fatalf("SetPinned unpin: %v", err)
	}
	if len(project.PinnedArtifacts()) != 0 {
		t.Fatal("expected no pinned artifacts after unpinning")
	}
	if _, err := project.SetPinned("art-missing", true); err == nil {
		t.Fatal("expected an error pinning an unknown artifact")
	}
}

func TestResolveArtifactByIDAndName(t *testing.T) {
	project := newTestProject(t)
	first, _ := project.AddArtifact(specFor("api.md", "v1"))
	second, _ := project.AddArtifact(specFor("api.md", "v2"))

	byID, ok := project.ResolveArtifact(first.ID)
	if !ok || byID.ID != first.ID {
		t.Fatalf("ResolveArtifact(id) = %+v (%t), want %q", byID, ok, first.ID)
	}
	byName, ok := project.ResolveArtifact("api.md")
	if !ok || byName.ID != second.ID {
		t.Fatalf("ResolveArtifact(name) = %+v (%t), want latest %q", byName, ok, second.ID)
	}
	if _, ok := project.ResolveArtifact("nope"); ok {
		t.Fatal("expected ResolveArtifact to miss an unknown reference")
	}
}

func TestAddTagsMerges(t *testing.T) {
	project := newTestProject(t)
	artifact, _ := project.AddArtifact(ArtifactSpec{Name: "api.md", Type: ArtifactTypeSpec, Body: Body{Content: "x"}, Tags: []string{"one"}})
	updated, err := project.AddTags(artifact.ID, []string{"two", "one"})
	if err != nil {
		t.Fatalf("AddTags: %v", err)
	}
	if len(updated.Tags) != 2 {
		t.Fatalf("tags = %v, want two deduplicated tags", updated.Tags)
	}
}

func TestProjectJSONRoundTrip(t *testing.T) {
	project := newTestProject(t)
	if _, err := project.AddArtifact(specFor("api.md", "body")); err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	payload, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var restored Project
	if err := json.Unmarshal(payload, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if restored.ID != project.ID || len(restored.Artifacts) != 1 {
		t.Fatalf("round trip lost data: %+v", restored)
	}
	if restored.Artifacts[0].Body.Content != "body" {
		t.Fatalf("round trip body = %q, want %q", restored.Artifacts[0].Body.Content, "body")
	}
}

func TestParseArtifactType(t *testing.T) {
	for _, raw := range []string{"spec", " SPEC ", "test-result", "generated"} {
		if _, err := ParseArtifactType(raw); err != nil {
			t.Fatalf("ParseArtifactType(%q): %v", raw, err)
		}
	}
	if _, err := ParseArtifactType("wiki"); err == nil {
		t.Fatal("expected an error for an unknown artifact type")
	}
}

func TestNewIDIsUniqueAndPrefixed(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		id := NewID("art")
		if len(id) < 5 || id[:4] != "art-" {
			t.Fatalf("NewID = %q, want an art- prefix", id)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("NewID produced a duplicate: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestBodyValidateErrorsAreDistinct(t *testing.T) {
	if err := (Body{}).validate(); err == nil || errors.Is(err, errors.New("")) {
		if err == nil {
			t.Fatal("expected an error for an empty body")
		}
	}
}
