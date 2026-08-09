// Package model defines the Loomwork domain: projects and the versioned
// artifacts they contain. It depends on the standard library only and knows
// nothing about transport, storage, or model providers.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// ArtifactType classifies an artifact's role inside a project.
type ArtifactType string

const (
	ArtifactTypeSpec       ArtifactType = "spec"
	ArtifactTypeLog        ArtifactType = "log"
	ArtifactTypeTestResult ArtifactType = "test-result"
	ArtifactTypeDiagram    ArtifactType = "diagram"
	ArtifactTypeDoc        ArtifactType = "doc"
	ArtifactTypeGenerated  ArtifactType = "generated"
)

// ArtifactTypes lists every supported artifact type.
func ArtifactTypes() []ArtifactType {
	return []ArtifactType{
		ArtifactTypeSpec,
		ArtifactTypeLog,
		ArtifactTypeTestResult,
		ArtifactTypeDiagram,
		ArtifactTypeDoc,
		ArtifactTypeGenerated,
	}
}

// ParseArtifactType validates a raw artifact type string.
func ParseArtifactType(raw string) (ArtifactType, error) {
	candidate := ArtifactType(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range ArtifactTypes() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown artifact type %q: supported types are %s", raw, joinTypes(ArtifactTypes()))
}

func joinTypes(types []ArtifactType) string {
	parts := make([]string, 0, len(types))
	for _, t := range types {
		parts = append(parts, string(t))
	}
	return strings.Join(parts, ", ")
}

// Body holds artifact payload: either inline content or an external reference.
// Exactly one of the two must be set.
type Body struct {
	Content   string `json:"content,omitempty"`
	Ref       string `json:"ref,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
}

func (b Body) validate() error {
	hasContent := b.Content != ""
	hasRef := strings.TrimSpace(b.Ref) != ""
	switch {
	case hasContent && hasRef:
		return fmt.Errorf("artifact body must carry either inline content or a reference, not both")
	case !hasContent && !hasRef:
		return fmt.Errorf("artifact body must carry inline content or a reference")
	}
	return nil
}

// Artifact is an immutable-content, versioned unit of project material.
// Metadata such as tags and the pinned flag may change; Body never does.
type Artifact struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      ArtifactType      `json:"type"`
	Version   int               `json:"version"`
	Tags      []string          `json:"tags,omitempty"`
	Pinned    bool              `json:"pinned"`
	ParentID  string            `json:"parentId,omitempty"`
	Body      Body              `json:"body"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
}

// ArtifactSpec describes an artifact to be created. The project assigns id,
// version, parent, and timestamp.
type ArtifactSpec struct {
	Name     string
	Type     ArtifactType
	Body     Body
	Tags     []string
	Pinned   bool
	Metadata map[string]string
}

func (s ArtifactSpec) validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("artifact name is required")
	}
	if _, err := ParseArtifactType(string(s.Type)); err != nil {
		return err
	}
	return s.Body.validate()
}

// NewID returns a random 128-bit identifier with the given prefix.
func NewID(prefix string) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failure is unrecoverable for identity generation; fall
		// back to a time-derived value rather than panicking.
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}
