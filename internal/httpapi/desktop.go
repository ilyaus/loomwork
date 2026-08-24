package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/ilyaus/loomwork/internal/orchestrator"
)

// DesktopService supplies provider/model discovery and chat independently of
// HTTP so handler tests can use a deterministic fake.
type DesktopService interface {
	ListModels(context.Context) []orchestrator.ProviderModels
	Chat(context.Context, orchestrator.ChatRequest) (orchestrator.ChatResponse, error)
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	if s.desktop == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("desktop model service is unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, s.desktop.ListModels(r.Context()))
}

type chatRequest struct {
	Selector     string                     `json:"selector"`
	Messages     []orchestrator.ChatMessage `json:"messages"`
	ArtifactRefs []string                   `json:"artifactRefs"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request, projectRef string) {
	var request chatRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(request.Selector) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("chat selector is required"))
		return
	}
	if len(request.Messages) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("chat needs at least one message"))
		return
	}
	if s.desktop == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("desktop chat service is unavailable"))
		return
	}
	if _, err := s.store.Resolve(projectRef); err != nil {
		writeStoreError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming is unsupported by this server"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	writeSSE(w, "ready", map[string]string{"status": "ready"})
	flusher.Flush()

	response, err := s.desktop.Chat(r.Context(), orchestrator.ChatRequest{
		ProjectRef: projectRef, Selector: request.Selector,
		Messages: request.Messages, ArtifactRefs: request.ArtifactRefs,
	})
	if err != nil {
		writeSSE(w, "error", map[string]string{"error": err.Error()})
		flusher.Flush()
		return
	}
	for _, delta := range textChunks(response.Text, 48) {
		writeSSE(w, "message", map[string]string{"delta": delta})
		flusher.Flush()
	}
	writeSSE(w, "done", response)
	flusher.Flush()
}

func writeSSE(w http.ResponseWriter, event string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"error":"encode event"}`)
		event = "error"
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
}

func textChunks(text string, maximumRunes int) []string {
	if text == "" {
		return []string{""}
	}
	chunks := make([]string, 0, utf8.RuneCountInString(text)/maximumRunes+1)
	runes := []rune(text)
	for len(runes) > 0 {
		end := maximumRunes
		if end > len(runes) {
			end = len(runes)
		}
		if end < len(runes) {
			for end > maximumRunes/2 && !strings.ContainsRune(" \n\t", runes[end-1]) {
				end--
			}
		}
		chunks = append(chunks, string(runes[:end]))
		runes = runes[end:]
	}
	return chunks
}
