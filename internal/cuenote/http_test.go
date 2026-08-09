package cuenote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPClientListCuesBuildsQuery(t *testing.T) {
	var request *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		request = incoming
		_, _ = io.WriteString(writer, `{"cues":[{"id":"cue-1","name":"analyze"}]}`)
	}))
	defer server.Close()

	client := NewHTTPClient(Config{BaseURL: server.URL + "/"})
	cues, err := client.ListCues(context.Background(), CueFilter{Tags: []string{"log", "triage"}, Search: "retry", Limit: 5})
	if err != nil {
		t.Fatalf("ListCues: %v", err)
	}
	if len(cues) != 1 || cues[0].ID != "cue-1" {
		t.Fatalf("cues = %+v, want the single decoded cue", cues)
	}
	if request.URL.Path != "/api/v1/cues" {
		t.Errorf("path = %q, want /api/v1/cues", request.URL.Path)
	}
	query := request.URL.Query()
	if got := query["tag"]; len(got) != 2 || got[0] != "log" || got[1] != "triage" {
		t.Errorf("tag = %v, want repeated tag parameters", got)
	}
	if query.Get("q") != "retry" || query.Get("limit") != "5" {
		t.Errorf("query = %v, want the search and limit parameters", query)
	}
	if request.Header.Get("Accept") != "application/json" {
		t.Errorf("Accept = %q, want application/json", request.Header.Get("Accept"))
	}
}

func TestHTTPClientCreateCueSendsBodyAndToken(t *testing.T) {
	t.Setenv(EnvAPIToken, "token-value")
	var received Cue
	var authorization, contentType string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.Method != http.MethodPost || incoming.URL.Path != "/api/v1/cues" {
			t.Errorf("request = %s %s, want POST /api/v1/cues", incoming.Method, incoming.URL.Path)
		}
		authorization = incoming.Header.Get("Authorization")
		contentType = incoming.Header.Get("Content-Type")
		if err := json.NewDecoder(incoming.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = io.WriteString(writer, `{"id":"cue-9","name":"analyze","body":"b"}`)
	}))
	defer server.Close()

	created, err := NewHTTPClient(Config{BaseURL: server.URL}).CreateCue(context.Background(), Cue{Name: "analyze", Body: "b"})
	if err != nil {
		t.Fatalf("CreateCue: %v", err)
	}
	if created.ID != "cue-9" {
		t.Errorf("created = %+v, want the server-assigned id", created)
	}
	if received.Name != "analyze" || received.Body != "b" {
		t.Errorf("received = %+v, want the request cue", received)
	}
	if authorization != "Bearer token-value" || contentType != "application/json" {
		t.Errorf("headers = %q/%q, want a bearer token and JSON content type", authorization, contentType)
	}
}

func TestHTTPClientGetCueMapsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.URL.Path != "/api/v1/cues/cue%20one" && incoming.URL.EscapedPath() != "/api/v1/cues/cue%20one" {
			t.Errorf("escaped path = %q, want the id percent-escaped", incoming.URL.EscapedPath())
		}
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := NewHTTPClient(Config{BaseURL: server.URL}).GetCue(context.Background(), "cue one")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestHTTPClientReportsUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(writer, "upstream down")
	}))
	defer server.Close()

	_, err := NewHTTPClient(Config{BaseURL: server.URL}).ListNotes(context.Background(), NoteFilter{})
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "upstream down") {
		t.Fatalf("error = %v, want it to report the status and body", err)
	}
}

func TestHTTPClientNotes(t *testing.T) {
	var request *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		request = incoming
		switch incoming.URL.Path {
		case "/api/v1/notes":
			if incoming.Method == http.MethodPost {
				_, _ = io.WriteString(writer, `{"id":"note-1","title":"t"}`)
				return
			}
			_, _ = io.WriteString(writer, `{"notes":[{"id":"note-1","title":"t"}]}`)
		case "/api/v1/notes/note-1":
			_, _ = io.WriteString(writer, `{"id":"note-1","title":"t"}`)
		default:
			t.Errorf("unexpected path %q", incoming.URL.Path)
		}
	}))
	defer server.Close()

	client := NewHTTPClient(Config{BaseURL: server.URL, TimeoutSeconds: 3})
	notes, err := client.ListNotes(context.Background(), NoteFilter{ProjectID: "p1", Tags: []string{"idea"}, Search: "x", Limit: 2})
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("notes = %+v, want one decoded note", notes)
	}
	query := request.URL.Query()
	if query.Get("projectId") != "p1" || query.Get("tag") != "idea" || query.Get("q") != "x" || query.Get("limit") != "2" {
		t.Errorf("query = %v, want every note filter mapped", query)
	}

	if _, err := client.GetNote(context.Background(), "note-1"); err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if _, err := client.CreateNote(context.Background(), Note{Title: "t"}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
}

func TestNewHTTPClientDefaults(t *testing.T) {
	client := NewHTTPClient(Config{})
	if client.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", client.baseURL, DefaultBaseURL)
	}
	if client.client.Timeout != DefaultTimeout {
		t.Errorf("timeout = %v, want %v", client.client.Timeout, DefaultTimeout)
	}
	var _ Client = client
}
