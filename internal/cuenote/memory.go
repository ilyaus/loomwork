package cuenote

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryClient is a thread-safe in-memory Client. It keeps the foundation and
// its tests independent of a running cue-note service.
type MemoryClient struct {
	mu       sync.RWMutex
	cues     map[string]Cue
	notes    map[string]Note
	sequence int
	now      func() time.Time
}

// NewMemoryClient builds an empty in-memory client.
func NewMemoryClient() *MemoryClient {
	return &MemoryClient{
		cues:  map[string]Cue{},
		notes: map[string]Note{},
		now:   time.Now,
	}
}

// ListCues returns cues matching the filter, ordered by name.
func (m *MemoryClient) ListCues(_ context.Context, filter CueFilter) ([]Cue, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	matched := make([]Cue, 0, len(m.cues))
	for _, cue := range m.cues {
		if matchesFilters(filter.Tags, cue.Tags, filter.Search, cue.Name, cue.Body) {
			matched = append(matched, cue)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Name < matched[j].Name })
	return applyLimit(matched, filter.Limit), nil
}

// GetCue returns a cue by id.
func (m *MemoryClient) GetCue(_ context.Context, id string) (Cue, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cue, ok := m.cues[id]
	if !ok {
		return Cue{}, fmt.Errorf("cue %q: %w", id, ErrNotFound)
	}
	return cue, nil
}

// CreateCue stores a cue, assigning an id and timestamps.
func (m *MemoryClient) CreateCue(_ context.Context, cue Cue) (Cue, error) {
	if strings.TrimSpace(cue.Name) == "" {
		return Cue{}, fmt.Errorf("cue name is required")
	}
	if strings.TrimSpace(cue.Body) == "" {
		return Cue{}, fmt.Errorf("cue %q: body is required", cue.Name)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if cue.ID == "" {
		cue.ID = m.nextIDLocked("cue")
	}
	if _, exists := m.cues[cue.ID]; exists {
		return Cue{}, fmt.Errorf("cue %q already exists", cue.ID)
	}
	timestamp := m.now().UTC()
	cue.CreatedAt = timestamp
	cue.UpdatedAt = timestamp
	if len(cue.Variables) == 0 {
		cue.Variables = TemplateVariables(cue.Body)
	}
	m.cues[cue.ID] = cue
	return cue, nil
}

// ListNotes returns notes matching the filter, newest first.
func (m *MemoryClient) ListNotes(_ context.Context, filter NoteFilter) ([]Note, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	matched := make([]Note, 0, len(m.notes))
	for _, note := range m.notes {
		if filter.ProjectID != "" && note.ProjectID != filter.ProjectID {
			continue
		}
		if matchesFilters(filter.Tags, note.Tags, filter.Search, note.Title, note.Body) {
			matched = append(matched, note)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
			return matched[i].CreatedAt.After(matched[j].CreatedAt)
		}
		return matched[i].ID < matched[j].ID
	})
	return applyLimit(matched, filter.Limit), nil
}

// GetNote returns a note by id.
func (m *MemoryClient) GetNote(_ context.Context, id string) (Note, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	note, ok := m.notes[id]
	if !ok {
		return Note{}, fmt.Errorf("note %q: %w", id, ErrNotFound)
	}
	return note, nil
}

// CreateNote stores a note, assigning an id and timestamps.
func (m *MemoryClient) CreateNote(_ context.Context, note Note) (Note, error) {
	if strings.TrimSpace(note.Title) == "" {
		return Note{}, fmt.Errorf("note title is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if note.ID == "" {
		note.ID = m.nextIDLocked("note")
	}
	if _, exists := m.notes[note.ID]; exists {
		return Note{}, fmt.Errorf("note %q already exists", note.ID)
	}
	timestamp := m.now().UTC()
	note.CreatedAt = timestamp
	note.UpdatedAt = timestamp
	m.notes[note.ID] = note
	return note, nil
}

func (m *MemoryClient) nextIDLocked(prefix string) string {
	m.sequence++
	return fmt.Sprintf("%s-%04d", prefix, m.sequence)
}

func applyLimit[T any](items []T, limit int) []T {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}
