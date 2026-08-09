package cuenote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// EnvAPIToken names the environment variable holding the cue-note API token.
// Tokens are never read from config files.
const EnvAPIToken = "CUENOTE_API_TOKEN"

// DefaultBaseURL is the assumed local cue-note endpoint.
const DefaultBaseURL = "http://localhost:8090"

// DefaultTimeout bounds each cue-note call.
const DefaultTimeout = 15 * time.Second

const maxErrorBodyBytes = 2048

// Config declares how to reach cue-note.
type Config struct {
	BaseURL        string `json:"baseUrl,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

// HTTPClient talks to a cue-note service over the REST contract documented in
// docs/cue-note-contract.md. The contract is assumed, not verified, because
// cue-note is still under construction; keep this file and that document in
// lockstep once the service ships.
type HTTPClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewHTTPClient builds a cue-note HTTP client.
func NewHTTPClient(cfg Config) *HTTPClient {
	timeout := DefaultTimeout
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	return &HTTPClient{
		baseURL: strings.TrimRight(base, "/"),
		token:   strings.TrimSpace(os.Getenv(EnvAPIToken)),
		client:  &http.Client{Timeout: timeout},
	}
}

type cueListResponse struct {
	Cues []Cue `json:"cues"`
}

type noteListResponse struct {
	Notes []Note `json:"notes"`
}

// ListCues issues GET /api/v1/cues.
func (h *HTTPClient) ListCues(ctx context.Context, filter CueFilter) ([]Cue, error) {
	query := url.Values{}
	for _, tag := range filter.Tags {
		query.Add("tag", tag)
	}
	if filter.Search != "" {
		query.Set("q", filter.Search)
	}
	if filter.Limit > 0 {
		query.Set("limit", strconv.Itoa(filter.Limit))
	}
	var decoded cueListResponse
	if err := h.do(ctx, http.MethodGet, "/api/v1/cues", query, nil, &decoded); err != nil {
		return nil, fmt.Errorf("cuenote list cues: %w", err)
	}
	return decoded.Cues, nil
}

// GetCue issues GET /api/v1/cues/{id}.
func (h *HTTPClient) GetCue(ctx context.Context, id string) (Cue, error) {
	var cue Cue
	if err := h.do(ctx, http.MethodGet, "/api/v1/cues/"+url.PathEscape(id), nil, nil, &cue); err != nil {
		return Cue{}, fmt.Errorf("cuenote get cue %q: %w", id, err)
	}
	return cue, nil
}

// CreateCue issues POST /api/v1/cues.
func (h *HTTPClient) CreateCue(ctx context.Context, cue Cue) (Cue, error) {
	var created Cue
	if err := h.do(ctx, http.MethodPost, "/api/v1/cues", nil, cue, &created); err != nil {
		return Cue{}, fmt.Errorf("cuenote create cue %q: %w", cue.Name, err)
	}
	return created, nil
}

// ListNotes issues GET /api/v1/notes.
func (h *HTTPClient) ListNotes(ctx context.Context, filter NoteFilter) ([]Note, error) {
	query := url.Values{}
	if filter.ProjectID != "" {
		query.Set("projectId", filter.ProjectID)
	}
	for _, tag := range filter.Tags {
		query.Add("tag", tag)
	}
	if filter.Search != "" {
		query.Set("q", filter.Search)
	}
	if filter.Limit > 0 {
		query.Set("limit", strconv.Itoa(filter.Limit))
	}
	var decoded noteListResponse
	if err := h.do(ctx, http.MethodGet, "/api/v1/notes", query, nil, &decoded); err != nil {
		return nil, fmt.Errorf("cuenote list notes: %w", err)
	}
	return decoded.Notes, nil
}

// GetNote issues GET /api/v1/notes/{id}.
func (h *HTTPClient) GetNote(ctx context.Context, id string) (Note, error) {
	var note Note
	if err := h.do(ctx, http.MethodGet, "/api/v1/notes/"+url.PathEscape(id), nil, nil, &note); err != nil {
		return Note{}, fmt.Errorf("cuenote get note %q: %w", id, err)
	}
	return note, nil
}

// CreateNote issues POST /api/v1/notes.
func (h *HTTPClient) CreateNote(ctx context.Context, note Note) (Note, error) {
	var created Note
	if err := h.do(ctx, http.MethodPost, "/api/v1/notes", nil, note, &created); err != nil {
		return Note{}, fmt.Errorf("cuenote create note %q: %w", note.Title, err)
	}
	return created, nil
}

func (h *HTTPClient) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	endpoint := h.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode body for %s: %w", endpoint, err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", endpoint, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", endpoint, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("call %s: %w", endpoint, ErrNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("call %s: unexpected status %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response from %s: %w", endpoint, err)
	}
	return nil
}
