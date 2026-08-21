package model

import (
	"fmt"
	"strings"
	"time"
)

// SourceType names the system of record a requirement or project document comes
// from.
type SourceType string

const (
	SourceTypeADO        SourceType = "ado"
	SourceTypeConfluence SourceType = "confluence"
	SourceTypeGitHub     SourceType = "github"
	SourceTypeOther      SourceType = "other"
)

// SourceTypes lists every supported source system.
func SourceTypes() []SourceType {
	return []SourceType{SourceTypeADO, SourceTypeConfluence, SourceTypeGitHub, SourceTypeOther}
}

// ParseSourceType validates a raw source type string.
func ParseSourceType(raw string) (SourceType, error) {
	candidate := SourceType(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range SourceTypes() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown source type %q: supported types are %s", raw, joinStrings(SourceTypes()))
}

// RequirementStatus tracks whether a requirement version is still testable.
type RequirementStatus string

const (
	// RequirementStatusActive marks the version QA tests against.
	RequirementStatusActive RequirementStatus = "active"
	// RequirementStatusObsolete marks a requirement that no longer applies. It
	// is retained for audit rather than deleted.
	RequirementStatusObsolete RequirementStatus = "obsolete"
	// RequirementStatusSuperseded marks a version replaced by a later one.
	RequirementStatusSuperseded RequirementStatus = "superseded"
)

// RequirementStatuses lists every supported status.
func RequirementStatuses() []RequirementStatus {
	return []RequirementStatus{RequirementStatusActive, RequirementStatusObsolete, RequirementStatusSuperseded}
}

// ParseRequirementStatus validates a raw status string.
func ParseRequirementStatus(raw string) (RequirementStatus, error) {
	candidate := RequirementStatus(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range RequirementStatuses() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown requirement status %q: supported statuses are %s", raw, joinStrings(RequirementStatuses()))
}

// RequirementOrigin records how a requirement version was produced. Both origins
// write the same schema to the same store, so an LLM extraction path can be
// added without changing readers.
type RequirementOrigin string

const (
	// RequirementOriginAuthored is direct QA entry.
	RequirementOriginAuthored RequirementOrigin = "authored"
	// RequirementOriginExtracted is LLM/agent extraction from source docs.
	RequirementOriginExtracted RequirementOrigin = "extracted"
)

// RequirementOrigins lists every supported origin.
func RequirementOrigins() []RequirementOrigin {
	return []RequirementOrigin{RequirementOriginAuthored, RequirementOriginExtracted}
}

// ParseRequirementOrigin validates a raw origin string.
func ParseRequirementOrigin(raw string) (RequirementOrigin, error) {
	candidate := RequirementOrigin(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range RequirementOrigins() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown requirement origin %q: supported origins are %s", raw, joinStrings(RequirementOrigins()))
}

// Requirement is one immutable version snapshot of a tester-facing requirement.
// Its JSON shape is fixed by docs/schemas/requirement.schema.json, which the
// browser UI and future agent integrations share.
type Requirement struct {
	ID         string            `json:"id"`
	Version    int               `json:"version"`
	Text       string            `json:"text"`
	SourceType SourceType        `json:"source_type,omitempty"`
	SourceRef  string            `json:"source_ref,omitempty"`
	Status     RequirementStatus `json:"status"`
	Origin     RequirementOrigin `json:"origin"`
	Tags       []string          `json:"tags,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

// RequirementSpec describes a requirement version to be created. The store
// assigns id, version, and timestamp.
type RequirementSpec struct {
	Text       string
	SourceType SourceType
	SourceRef  string
	Status     RequirementStatus
	Origin     RequirementOrigin
	Tags       []string
	Metadata   map[string]string
}

func (s RequirementSpec) normalize() (RequirementSpec, error) {
	s.Text = strings.TrimSpace(s.Text)
	if s.Text == "" {
		return RequirementSpec{}, fmt.Errorf("requirement text is required")
	}
	s.SourceRef = strings.TrimSpace(s.SourceRef)
	if s.SourceType != "" {
		parsed, err := ParseSourceType(string(s.SourceType))
		if err != nil {
			return RequirementSpec{}, err
		}
		s.SourceType = parsed
	}
	if s.SourceRef != "" && s.SourceType == "" {
		return RequirementSpec{}, fmt.Errorf("requirement source reference %q needs a source type (%s)", s.SourceRef, joinStrings(SourceTypes()))
	}
	if s.Status == "" {
		s.Status = RequirementStatusActive
	}
	status, err := ParseRequirementStatus(string(s.Status))
	if err != nil {
		return RequirementSpec{}, err
	}
	s.Status = status
	if s.Origin == "" {
		s.Origin = RequirementOriginAuthored
	}
	origin, err := ParseRequirementOrigin(string(s.Origin))
	if err != nil {
		return RequirementSpec{}, err
	}
	s.Origin = origin
	return s, nil
}

// NewRequirement builds the first version of a requirement.
func NewRequirement(id string, spec RequirementSpec) (*Requirement, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("requirement id is required")
	}
	normalized, err := spec.normalize()
	if err != nil {
		return nil, err
	}
	return &Requirement{
		ID:         strings.TrimSpace(id),
		Version:    1,
		Text:       normalized.Text,
		SourceType: normalized.SourceType,
		SourceRef:  normalized.SourceRef,
		Status:     normalized.Status,
		Origin:     normalized.Origin,
		Tags:       normalizeTags(normalized.Tags),
		Metadata:   copyMetadata(normalized.Metadata),
		CreatedAt:  nowFunc().UTC(),
	}, nil
}

// NextVersion returns the next version of a requirement. Fields the spec leaves
// empty are inherited from the current version, so a text-only edit keeps the
// source back-reference. The receiver is not modified; the caller decides when
// to mark it superseded.
func (r *Requirement) NextVersion(spec RequirementSpec) (*Requirement, error) {
	if spec.SourceType == "" && spec.SourceRef == "" {
		spec.SourceType = r.SourceType
		spec.SourceRef = r.SourceRef
	}
	if strings.TrimSpace(spec.Text) == "" {
		spec.Text = r.Text
	}
	if spec.Origin == "" {
		spec.Origin = r.Origin
	}
	if len(spec.Tags) == 0 {
		spec.Tags = r.Tags
	}
	if len(spec.Metadata) == 0 {
		spec.Metadata = r.Metadata
	}
	next, err := NewRequirement(r.ID, spec)
	if err != nil {
		return nil, err
	}
	next.Version = r.Version + 1
	return next, nil
}

// SetStatus changes the status of a stored version. A superseded version is
// frozen: a newer version already carries the current text, so reactivating an
// older snapshot would give a requirement id two active versions.
func (r *Requirement) SetStatus(status RequirementStatus) error {
	parsed, err := ParseRequirementStatus(string(status))
	if err != nil {
		return err
	}
	if r.Status == parsed {
		return nil
	}
	if r.Status == RequirementStatusSuperseded {
		return fmt.Errorf("requirement %s v%d is superseded: its status is fixed because a newer version exists", r.ID, r.Version)
	}
	r.Status = parsed
	return nil
}

// DocumentSource links a project to a document in a system of record, optionally
// alongside a copy kept locally or in S3.
type DocumentSource struct {
	Name      string     `json:"name"`
	Type      SourceType `json:"type"`
	URL       string     `json:"url,omitempty"`
	LocalPath string     `json:"localPath,omitempty"`
	S3URI     string     `json:"s3Uri,omitempty"`
}

func (d DocumentSource) normalize() (DocumentSource, error) {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" {
		return DocumentSource{}, fmt.Errorf("document source name is required")
	}
	sourceType, err := ParseSourceType(string(d.Type))
	if err != nil {
		return DocumentSource{}, fmt.Errorf("document source %q: %w", d.Name, err)
	}
	d.Type = sourceType
	d.URL = strings.TrimSpace(d.URL)
	d.LocalPath = strings.TrimSpace(d.LocalPath)
	d.S3URI = strings.TrimSpace(d.S3URI)
	if d.URL == "" && d.LocalPath == "" && d.S3URI == "" {
		return DocumentSource{}, fmt.Errorf("document source %q needs a url, a local copy, or an s3 copy", d.Name)
	}
	return d, nil
}

func joinStrings[T ~string](values []T) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, ", ")
}
