package cuenote

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestTemplateVariablesAreDistinctAndSorted(t *testing.T) {
	got := TemplateVariables("{{ topic }} and {{artifact.name}} and {{topic}} and {{ run_id }}")
	want := []string{"artifact.name", "run_id", "topic"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TemplateVariables = %v, want %v", got, want)
	}
	if got := TemplateVariables("no placeholders"); len(got) != 0 {
		t.Fatalf("TemplateVariables = %v, want none", got)
	}
}

func TestCueRender(t *testing.T) {
	cue := Cue{Name: "summarize", Body: "Summarize {{ artifact }} for {{audience}}."}
	rendered, err := cue.Render(map[string]string{"artifact": "the log", "audience": "on-call"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if rendered != "Summarize the log for on-call." {
		t.Fatalf("rendered = %q, want the substituted body", rendered)
	}
}

func TestCueRenderReportsMissingVariables(t *testing.T) {
	cue := Cue{Name: "summarize", Body: "{{ artifact }} for {{audience}}"}
	_, err := cue.Render(map[string]string{"audience": "on-call"})
	if err == nil {
		t.Fatal("expected an error for an unresolved variable")
	}
	if !strings.Contains(err.Error(), "summarize") || !strings.Contains(err.Error(), "artifact") {
		t.Fatalf("error = %v, want it to name the cue and the missing variable", err)
	}
}

func TestMemoryClientImplementsClient(t *testing.T) {
	var client Client = NewMemoryClient()
	if client == nil {
		t.Fatal("MemoryClient must satisfy Client")
	}
}

func TestMemoryClientCreateAndGetCue(t *testing.T) {
	ctx := context.Background()
	client := NewMemoryClient()

	created, err := client.CreateCue(ctx, Cue{Name: "analyze-log", Body: "Analyze {{log}} for {{severity}}", Tags: []string{"log"}})
	if err != nil {
		t.Fatalf("CreateCue: %v", err)
	}
	if created.ID != "cue-0001" {
		t.Errorf("ID = %q, want a generated cue-0001", created.ID)
	}
	if created.CreatedAt.IsZero() || !created.CreatedAt.Equal(created.UpdatedAt) {
		t.Errorf("timestamps = %v/%v, want both set and equal", created.CreatedAt, created.UpdatedAt)
	}
	if !reflect.DeepEqual(created.Variables, []string{"log", "severity"}) {
		t.Errorf("Variables = %v, want them derived from the body", created.Variables)
	}

	fetched, err := client.GetCue(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetCue: %v", err)
	}
	if fetched.Name != "analyze-log" {
		t.Errorf("fetched = %+v, want the stored cue", fetched)
	}

	if _, err := client.GetCue(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetCue error = %v, want ErrNotFound", err)
	}
}

func TestMemoryClientCreateCueValidation(t *testing.T) {
	ctx := context.Background()
	client := NewMemoryClient()

	if _, err := client.CreateCue(ctx, Cue{Body: "body"}); err == nil {
		t.Error("expected an error without a name")
	}
	if _, err := client.CreateCue(ctx, Cue{Name: "n"}); err == nil {
		t.Error("expected an error without a body")
	}
	if _, err := client.CreateCue(ctx, Cue{ID: "fixed", Name: "n", Body: "b"}); err != nil {
		t.Fatalf("CreateCue with an explicit id: %v", err)
	}
	if _, err := client.CreateCue(ctx, Cue{ID: "fixed", Name: "n", Body: "b"}); err == nil {
		t.Error("expected an error for a duplicate id")
	}
}

func TestMemoryClientListCuesFiltersAndOrders(t *testing.T) {
	ctx := context.Background()
	client := NewMemoryClient()
	for _, cue := range []Cue{
		{Name: "zeta", Body: "about logs", Tags: []string{"Log", "triage"}},
		{Name: "alpha", Body: "about specs", Tags: []string{"spec"}},
		{Name: "mid", Body: "about logs too", Tags: []string{"log"}},
	} {
		if _, err := client.CreateCue(ctx, cue); err != nil {
			t.Fatalf("CreateCue(%s): %v", cue.Name, err)
		}
	}

	all, err := client.ListCues(ctx, CueFilter{})
	if err != nil {
		t.Fatalf("ListCues: %v", err)
	}
	if names := cueNames(all); !reflect.DeepEqual(names, []string{"alpha", "mid", "zeta"}) {
		t.Errorf("names = %v, want them sorted by name", names)
	}

	tagged, err := client.ListCues(ctx, CueFilter{Tags: []string{"log"}})
	if err != nil {
		t.Fatalf("ListCues by tag: %v", err)
	}
	if names := cueNames(tagged); !reflect.DeepEqual(names, []string{"mid", "zeta"}) {
		t.Errorf("names = %v, want case-insensitive tag matching", names)
	}

	searched, err := client.ListCues(ctx, CueFilter{Search: "SPECS"})
	if err != nil {
		t.Fatalf("ListCues by search: %v", err)
	}
	if names := cueNames(searched); !reflect.DeepEqual(names, []string{"alpha"}) {
		t.Errorf("names = %v, want a case-insensitive body search", names)
	}

	limited, err := client.ListCues(ctx, CueFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListCues with a limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("len = %d, want the limit applied", len(limited))
	}
}

func TestMemoryClientNotes(t *testing.T) {
	ctx := context.Background()
	client := NewMemoryClient()

	first, err := client.CreateNote(ctx, Note{ProjectID: "p1", Title: "first", Body: "about retries", Tags: []string{"idea"}})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if _, err := client.CreateNote(ctx, Note{ProjectID: "p2", Title: "other"}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if _, err := client.CreateNote(ctx, Note{Body: "no title"}); err == nil {
		t.Error("expected an error without a title")
	}

	scoped, err := client.ListNotes(ctx, NoteFilter{ProjectID: "p1"})
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != first.ID {
		t.Errorf("notes = %+v, want only the p1 note", scoped)
	}

	searched, err := client.ListNotes(ctx, NoteFilter{Search: "retries", Tags: []string{"idea"}})
	if err != nil {
		t.Fatalf("ListNotes with filters: %v", err)
	}
	if len(searched) != 1 {
		t.Errorf("notes = %+v, want the tag and search filters combined", searched)
	}

	fetched, err := client.GetNote(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if fetched.Title != "first" {
		t.Errorf("fetched = %+v, want the stored note", fetched)
	}
	if _, err := client.GetNote(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetNote error = %v, want ErrNotFound", err)
	}
}

func cueNames(cues []Cue) []string {
	names := make([]string, 0, len(cues))
	for _, cue := range cues {
		names = append(names, cue.Name)
	}
	return names
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	client := NewMemoryClient()
	triage, err := client.CreateCue(ctx, Cue{Name: "triage", Body: "Summarize {{service}}"})
	if err != nil {
		t.Fatalf("CreateCue: %v", err)
	}
	if _, err := client.CreateCue(ctx, Cue{Name: "review", Body: "Review"}); err != nil {
		t.Fatalf("CreateCue: %v", err)
	}

	byID, err := Resolve(ctx, client, triage.ID)
	if err != nil || byID.ID != triage.ID {
		t.Fatalf("Resolve by id = %+v, %v", byID, err)
	}
	byName, err := Resolve(ctx, client, "TrIaGe")
	if err != nil || byName.ID != triage.ID {
		t.Fatalf("Resolve by name = %+v, %v", byName, err)
	}

	if _, err := Resolve(ctx, client, "  "); err == nil {
		t.Error("expected an error for an empty reference")
	}
	if _, err := Resolve(ctx, client, "absent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Resolve error = %v, want ErrNotFound", err)
	}
}

func TestResolveRejectsAmbiguousNames(t *testing.T) {
	ctx := context.Background()
	client := NewMemoryClient()
	// The memory client rejects duplicate names, so use a client that does not.
	duplicates := ambiguousClient{Client: client, cues: []Cue{
		{ID: "cue-1", Name: "triage"},
		{ID: "cue-2", Name: "Triage"},
	}}
	_, err := Resolve(ctx, duplicates, "triage")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("Resolve error = %v, want an ambiguity error", err)
	}
	if !strings.Contains(err.Error(), "cue-1") || !strings.Contains(err.Error(), "cue-2") {
		t.Errorf("error = %v, want both candidate ids listed", err)
	}
}

// ambiguousClient serves a fixed cue list so duplicate names can be exercised.
type ambiguousClient struct {
	Client
	cues []Cue
}

func (a ambiguousClient) ListCues(context.Context, CueFilter) ([]Cue, error) { return a.cues, nil }

func (a ambiguousClient) GetCue(_ context.Context, id string) (Cue, error) {
	return Cue{}, fmt.Errorf("cue %q: %w", id, ErrNotFound)
}
