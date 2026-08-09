// Package cuenote provides Loomwork's client for the ilyaus/cue-note service:
// the system of record for reusable prompts ("cues") and notes. The interface
// has an HTTP implementation targeting the contract documented in
// docs/cue-note-contract.md and an in-memory implementation, so Loomwork is
// never blocked on cue-note being finished.
package cuenote

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Cue is a reusable, optionally templated prompt.
type Cue struct {
	ID        string            `json:"id,omitempty"`
	Name      string            `json:"name"`
	Body      string            `json:"body"`
	Tags      []string          `json:"tags,omitempty"`
	Variables []string          `json:"variables,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt,omitempty"`
	UpdatedAt time.Time         `json:"updatedAt,omitempty"`
}

// Note is free-form text attached to a project.
type Note struct {
	ID        string            `json:"id,omitempty"`
	ProjectID string            `json:"projectId,omitempty"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Tags      []string          `json:"tags,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt,omitempty"`
	UpdatedAt time.Time         `json:"updatedAt,omitempty"`
}

// CueFilter narrows a cue listing.
type CueFilter struct {
	Tags   []string
	Search string
	Limit  int
}

// NoteFilter narrows a note listing.
type NoteFilter struct {
	ProjectID string
	Tags      []string
	Search    string
	Limit     int
}

// ErrNotFound is returned (possibly wrapped) when a cue or note does not exist.
var ErrNotFound = errors.New("cuenote: resource not found")

// Client is the single cue-note interface every implementation satisfies.
type Client interface {
	ListCues(ctx context.Context, filter CueFilter) ([]Cue, error)
	GetCue(ctx context.Context, id string) (Cue, error)
	CreateCue(ctx context.Context, cue Cue) (Cue, error)
	ListNotes(ctx context.Context, filter NoteFilter) ([]Note, error)
	GetNote(ctx context.Context, id string) (Note, error)
	CreateNote(ctx context.Context, note Note) (Note, error)
}

var templateVariable = regexp.MustCompile(`{{\s*([a-zA-Z0-9_.-]+)\s*}}`)

// TemplateVariables lists the distinct `{{var}}` placeholders in a body, sorted.
// It lives in the shared layer so every implementation behaves identically.
func TemplateVariables(body string) []string {
	matches := templateVariable.FindAllStringSubmatch(body, -1)
	seen := map[string]struct{}{}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		name := match[1]
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Render substitutes `{{var}}` placeholders in the cue body. Unresolved
// variables are an error: a silently empty prompt segment is never acceptable.
func (c Cue) Render(values map[string]string) (string, error) {
	missing := make([]string, 0, 2)
	rendered := templateVariable.ReplaceAllStringFunc(c.Body, func(match string) string {
		name := templateVariable.FindStringSubmatch(match)[1]
		value, ok := values[name]
		if !ok {
			missing = append(missing, name)
			return match
		}
		return value
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("cue %q: unresolved template variables: %s", displayName(c), strings.Join(missing, ", "))
	}
	return rendered, nil
}

func displayName(c Cue) string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	return c.ID
}

func matchesFilters(tags []string, haystack []string, search string, corpus ...string) bool {
	for _, required := range tags {
		found := false
		for _, candidate := range haystack {
			if strings.EqualFold(candidate, required) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if search != "" {
		lowered := strings.ToLower(search)
		matched := false
		for _, text := range corpus {
			if strings.Contains(strings.ToLower(text), lowered) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
