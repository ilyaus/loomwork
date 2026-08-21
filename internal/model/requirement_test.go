package model

import (
	"strings"
	"testing"
)

func TestNewRequirementNormalizesAndDefaults(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		spec       RequirementSpec
		wantErr    string
		wantStatus RequirementStatus
		wantOrigin RequirementOrigin
		wantSource SourceType
	}{
		{
			name:       "defaults to an authored active requirement",
			id:         "req-001",
			spec:       RequirementSpec{Text: "  Login rejects an expired password  "},
			wantStatus: RequirementStatusActive,
			wantOrigin: RequirementOriginAuthored,
		},
		{
			name: "keeps an extracted origin and source link",
			id:   "req-002",
			spec: RequirementSpec{
				Text:       "Session expires after 15 idle minutes",
				SourceType: SourceTypeConfluence,
				SourceRef:  "https://wiki/pages/42",
				Origin:     RequirementOriginExtracted,
				Status:     RequirementStatusObsolete,
			},
			wantStatus: RequirementStatusObsolete,
			wantOrigin: RequirementOriginExtracted,
			wantSource: SourceTypeConfluence,
		},
		{name: "rejects empty text", id: "req-003", spec: RequirementSpec{Text: "   "}, wantErr: "text is required"},
		{name: "rejects an empty id", id: " ", spec: RequirementSpec{Text: "anything"}, wantErr: "id is required"},
		{
			name:    "rejects an unknown source type",
			id:      "req-004",
			spec:    RequirementSpec{Text: "anything", SourceType: SourceType("jira")},
			wantErr: "unknown source type",
		},
		{
			name:    "rejects an unknown status",
			id:      "req-005",
			spec:    RequirementSpec{Text: "anything", Status: RequirementStatus("draft")},
			wantErr: "unknown requirement status",
		},
		{
			name:    "rejects a source reference without a type",
			id:      "req-006",
			spec:    RequirementSpec{Text: "anything", SourceRef: "AB#12"},
			wantErr: "needs a source type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requirement, err := NewRequirement(test.id, test.spec)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewRequirement: %v", err)
			}
			if requirement.Version != 1 {
				t.Errorf("version = %d, want the first version", requirement.Version)
			}
			if requirement.Text != strings.TrimSpace(test.spec.Text) {
				t.Errorf("text = %q, want it trimmed", requirement.Text)
			}
			if requirement.Status != test.wantStatus {
				t.Errorf("status = %q, want %q", requirement.Status, test.wantStatus)
			}
			if requirement.Origin != test.wantOrigin {
				t.Errorf("origin = %q, want %q", requirement.Origin, test.wantOrigin)
			}
			if requirement.SourceType != test.wantSource {
				t.Errorf("sourceType = %q, want %q", requirement.SourceType, test.wantSource)
			}
			if requirement.CreatedAt.IsZero() {
				t.Error("createdAt is zero, want a timestamp")
			}
		})
	}
}

func TestNextVersionInheritsOmittedFields(t *testing.T) {
	first, err := NewRequirement("req-001", RequirementSpec{
		Text:       "Login rejects an expired password",
		SourceType: SourceTypeADO,
		SourceRef:  "AB#1234",
		Origin:     RequirementOriginExtracted,
		Tags:       []string{"auth"},
		Metadata:   map[string]string{"doc": "spec.md"},
	})
	if err != nil {
		t.Fatalf("NewRequirement: %v", err)
	}

	second, err := first.NextVersion(RequirementSpec{Text: "Login rejects an expired password with a clear message"})
	if err != nil {
		t.Fatalf("NextVersion: %v", err)
	}
	if second.Version != 2 || second.ID != first.ID {
		t.Fatalf("next = %s v%d, want v2 of %s", second.ID, second.Version, first.ID)
	}
	if second.SourceType != SourceTypeADO || second.SourceRef != "AB#1234" {
		t.Errorf("source = %q/%q, want it inherited", second.SourceType, second.SourceRef)
	}
	if second.Origin != RequirementOriginExtracted {
		t.Errorf("origin = %q, want it inherited", second.Origin)
	}
	if strings.Join(second.Tags, ",") != "auth" || second.Metadata["doc"] != "spec.md" {
		t.Errorf("tags/metadata = %v/%v, want them inherited", second.Tags, second.Metadata)
	}
	// The earlier snapshot is untouched: every version is retrievable as it was.
	if first.Version != 1 || first.Text != "Login rejects an expired password" {
		t.Errorf("first = v%d %q, want the original snapshot unchanged", first.Version, first.Text)
	}

	// Omitting the text keeps the previous text rather than failing validation.
	third, err := second.NextVersion(RequirementSpec{SourceType: SourceTypeGitHub, SourceRef: "org/repo#7"})
	if err != nil {
		t.Fatalf("NextVersion: %v", err)
	}
	if third.Text != second.Text || third.SourceType != SourceTypeGitHub {
		t.Errorf("third = %+v, want inherited text and the new source", third)
	}
}

func TestRequirementSetStatusTransitions(t *testing.T) {
	tests := []struct {
		name    string
		from    RequirementStatus
		to      RequirementStatus
		wantErr string
	}{
		{name: "active to obsolete", from: RequirementStatusActive, to: RequirementStatusObsolete},
		{name: "obsolete back to active", from: RequirementStatusObsolete, to: RequirementStatusActive},
		{name: "active to superseded", from: RequirementStatusActive, to: RequirementStatusSuperseded},
		{name: "no-op on the same status", from: RequirementStatusSuperseded, to: RequirementStatusSuperseded},
		{
			name:    "superseded version is frozen",
			from:    RequirementStatusSuperseded,
			to:      RequirementStatusActive,
			wantErr: "is superseded",
		},
		{
			name:    "unknown status is rejected",
			from:    RequirementStatusActive,
			to:      RequirementStatus("retired"),
			wantErr: "unknown requirement status",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requirement, err := NewRequirement("req-001", RequirementSpec{Text: "text", Status: test.from})
			if err != nil {
				t.Fatalf("NewRequirement: %v", err)
			}
			err = requirement.SetStatus(test.to)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, test.wantErr)
				}
				if requirement.Status != test.from {
					t.Errorf("status = %q, want it unchanged after a rejected transition", requirement.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetStatus: %v", err)
			}
			if requirement.Status != test.to {
				t.Errorf("status = %q, want %q", requirement.Status, test.to)
			}
		})
	}
}

func TestDocumentSourceValidation(t *testing.T) {
	tests := []struct {
		name    string
		source  DocumentSource
		wantErr string
	}{
		{name: "github link", source: DocumentSource{Name: "spec", Type: SourceTypeGitHub, URL: "https://github.com/org/repo"}},
		{name: "local copy only", source: DocumentSource{Name: "spec", Type: SourceTypeOther, LocalPath: "/tmp/spec.md"}},
		{name: "s3 copy only", source: DocumentSource{Name: "spec", Type: SourceTypeADO, S3URI: "s3://bucket/spec.md"}},
		{name: "missing name", source: DocumentSource{Type: SourceTypeADO, URL: "https://ado"}, wantErr: "name is required"},
		{name: "missing type", source: DocumentSource{Name: "spec", URL: "https://ado"}, wantErr: "unknown source type"},
		{name: "no location", source: DocumentSource{Name: "spec", Type: SourceTypeADO}, wantErr: "needs a url"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := test.source.normalize()
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if normalized.Name != test.source.Name {
				t.Errorf("name = %q, want %q", normalized.Name, test.source.Name)
			}
		})
	}
}

func TestProjectAddSourceReplacesByName(t *testing.T) {
	project, err := NewProject("alpha", "", nil)
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	if _, err := project.AddSource(DocumentSource{Name: "spec", Type: SourceTypeGitHub, URL: "https://github.com/org/repo"}); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if _, err := project.AddSource(DocumentSource{Name: "SPEC", Type: SourceTypeConfluence, URL: "https://wiki/spec", S3URI: "s3://bucket/spec.md"}); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if len(project.Sources) != 1 {
		t.Fatalf("sources = %+v, want the same-named source replaced", project.Sources)
	}
	if project.Sources[0].Type != SourceTypeConfluence || project.Sources[0].S3URI != "s3://bucket/spec.md" {
		t.Errorf("source = %+v, want the updated link and its S3 copy", project.Sources[0])
	}
	if _, err := project.AddSource(DocumentSource{Name: "bad", Type: SourceType("jira"), URL: "https://x"}); err == nil {
		t.Error("AddSource with an unknown type = nil, want an error")
	}
}
