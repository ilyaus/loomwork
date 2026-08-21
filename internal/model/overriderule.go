package model

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OverrideRuleStatus tracks whether an override rule version still shapes new
// generations. Obsolete rules are retained so past suites remain auditable.
type OverrideRuleStatus string

const (
	OverrideRuleStatusActive     OverrideRuleStatus = "active"
	OverrideRuleStatusObsolete   OverrideRuleStatus = "obsolete"
	OverrideRuleStatusSuperseded OverrideRuleStatus = "superseded"
)

// OverrideRuleStatuses lists every supported status.
func OverrideRuleStatuses() []OverrideRuleStatus {
	return []OverrideRuleStatus{OverrideRuleStatusActive, OverrideRuleStatusObsolete, OverrideRuleStatusSuperseded}
}

// ParseOverrideRuleStatus validates a raw status string.
func ParseOverrideRuleStatus(raw string) (OverrideRuleStatus, error) {
	candidate := OverrideRuleStatus(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range OverrideRuleStatuses() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown override rule status %q: supported statuses are %s", raw, joinStrings(OverrideRuleStatuses()))
}

// errOverrideSupersededNotSettable mirrors the requirement rule: only creating a
// successor version may mark a version superseded.
var errOverrideSupersededNotSettable = fmt.Errorf(
	"override rule status %q is set only by creating a new version: choose %s or %s",
	OverrideRuleStatusSuperseded, OverrideRuleStatusActive, OverrideRuleStatusObsolete,
)

// OverrideActionKind names what a rule requires instead of the literal spec.
type OverrideActionKind string

const (
	// OverrideActionExpectStatus requires a specific status code.
	OverrideActionExpectStatus OverrideActionKind = "expect-status"
	// OverrideActionExpectEmptyCollection requires a success status with an
	// empty collection body — the confirmed "GET of a missing item returns an
	// empty list" case.
	OverrideActionExpectEmptyCollection OverrideActionKind = "expect-empty-collection"
	// OverrideActionSkipTest forbids covering the matched condition at all —
	// the confirmed "do not test missing authentication" case.
	OverrideActionSkipTest OverrideActionKind = "skip-test"
)

// OverrideActionKinds lists every supported action kind.
func OverrideActionKinds() []OverrideActionKind {
	return []OverrideActionKind{OverrideActionExpectStatus, OverrideActionExpectEmptyCollection, OverrideActionSkipTest}
}

// ParseOverrideActionKind validates a raw action kind.
func ParseOverrideActionKind(raw string) (OverrideActionKind, error) {
	candidate := OverrideActionKind(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range OverrideActionKinds() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown override action kind %q: supported kinds are %s", raw, joinStrings(OverrideActionKinds()))
}

// DefaultEmptyCollectionStatus is the success status assumed by an
// expect-empty-collection action that does not name one.
const DefaultEmptyCollectionStatus = 200

// OverrideCondition selects the test cases a rule governs. Every populated field
// must match; an empty condition matches every case.
type OverrideCondition struct {
	Methods     []HTTPMethod `json:"methods,omitempty"`
	PathPattern string       `json:"path_pattern,omitempty"`
	Scenario    TestScenario `json:"scenario,omitempty"`
	SpecStatus  int          `json:"spec_status,omitempty"`
}

// IsEmpty reports whether the condition constrains nothing.
func (c OverrideCondition) IsEmpty() bool {
	return len(c.Methods) == 0 && c.PathPattern == "" && c.Scenario == "" && c.SpecStatus == 0
}

// Matches reports whether the condition selects a test case. spec_status is not
// compared here: it documents which literal spec reading the rule corrects, and
// a generated case records the corrected expectation, not the original one.
func (c OverrideCondition) Matches(testCase TestCase) bool {
	if len(c.Methods) > 0 {
		found := false
		for _, method := range c.Methods {
			if method == testCase.Request.Method {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if c.Scenario != "" && c.Scenario != testCase.Scenario {
		return false
	}
	if c.PathPattern != "" && !matchPath(c.PathPattern, testCase.Request.Path) {
		return false
	}
	return true
}

// matchPath matches a request path against a glob. `*` stays inside one path
// segment (path.Match semantics); `**` matches across segments.
func matchPath(pattern, target string) bool {
	if strings.Contains(pattern, "**") {
		prefix, suffix, _ := strings.Cut(pattern, "**")
		if !strings.HasPrefix(target, prefix) {
			return false
		}
		return suffix == "" || strings.HasSuffix(target, suffix)
	}
	matched, err := path.Match(pattern, target)
	return err == nil && matched
}

// OverrideAction is the expectation a matched case must carry.
type OverrideAction struct {
	Kind         OverrideActionKind `json:"kind"`
	ExpectStatus int                `json:"expect_status,omitempty"`
}

// Status returns the status the action requires, filling in the default success
// status for an empty-collection action.
func (a OverrideAction) Status() int {
	if a.Kind == OverrideActionExpectEmptyCollection && a.ExpectStatus == 0 {
		return DefaultEmptyCollectionStatus
	}
	return a.ExpectStatus
}

func (a OverrideAction) normalize() (OverrideAction, error) {
	kind, err := ParseOverrideActionKind(string(a.Kind))
	if err != nil {
		return OverrideAction{}, err
	}
	a.Kind = kind
	switch kind {
	case OverrideActionExpectStatus:
		if a.ExpectStatus < 100 || a.ExpectStatus > 599 {
			return OverrideAction{}, fmt.Errorf("override action %q requires expect_status between 100 and 599", kind)
		}
	case OverrideActionSkipTest:
		if a.ExpectStatus != 0 {
			return OverrideAction{}, fmt.Errorf("override action %q must not set expect_status", kind)
		}
	case OverrideActionExpectEmptyCollection:
		if a.ExpectStatus == 0 {
			a.ExpectStatus = DefaultEmptyCollectionStatus
		}
		if a.ExpectStatus < 200 || a.ExpectStatus > 299 {
			return OverrideAction{}, fmt.Errorf("override action %q requires a 2xx expect_status, got %d", kind, a.ExpectStatus)
		}
	}
	return a, nil
}

// versionedRefPattern parses the "<id>-v<version>" citation form.
var versionedRefPattern = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*)-v([0-9]+)$`)

// OverrideRule is one immutable version of an override rule. The shape is
// hybrid by design: condition and action are structured so the workbench can
// audit deterministically which tests a rule should have shaped, while rationale
// carries the business reasoning the agent needs to generalize the rule to cases
// the condition does not name. Its JSON shape is fixed by the override_rule
// definition in docs/schemas/agent-definition.schema.json.
type OverrideRule struct {
	ID        string             `json:"id"`
	Version   int                `json:"version"`
	Title     string             `json:"title"`
	Condition OverrideCondition  `json:"condition"`
	Action    OverrideAction     `json:"action"`
	Rationale string             `json:"rationale"`
	Status    OverrideRuleStatus `json:"status"`
	Tags      []string           `json:"tags,omitempty"`
	Metadata  map[string]string  `json:"metadata,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
}

// OverrideRuleSpec describes a rule version to be created. The store assigns the
// id sequence position, version, and timestamp.
type OverrideRuleSpec struct {
	ID        string
	Title     string
	Condition OverrideCondition
	Action    OverrideAction
	Rationale string
	Status    OverrideRuleStatus
	Tags      []string
	Metadata  map[string]string
}

func (s OverrideRuleSpec) normalize() (OverrideRuleSpec, error) {
	s.ID = strings.TrimSpace(strings.ToLower(s.ID))
	if !agentNamePattern.MatchString(s.ID) {
		return OverrideRuleSpec{}, fmt.Errorf("override rule id %q must be lowercase letters, digits, and dashes", s.ID)
	}
	s.Title = strings.TrimSpace(s.Title)
	if s.Title == "" {
		return OverrideRuleSpec{}, fmt.Errorf("override rule %q needs a title", s.ID)
	}
	s.Rationale = strings.TrimSpace(s.Rationale)
	if s.Rationale == "" {
		return OverrideRuleSpec{}, fmt.Errorf("override rule %q needs a rationale: the agent reasons over it, so a structured condition alone is not enough", s.ID)
	}
	action, err := s.Action.normalize()
	if err != nil {
		return OverrideRuleSpec{}, fmt.Errorf("override rule %q: %w", s.ID, err)
	}
	s.Action = action

	condition := s.Condition
	methods := make([]HTTPMethod, 0, len(condition.Methods))
	for _, raw := range condition.Methods {
		method, err := ParseHTTPMethod(string(raw))
		if err != nil {
			return OverrideRuleSpec{}, fmt.Errorf("override rule %q: %w", s.ID, err)
		}
		methods = append(methods, method)
	}
	condition.Methods = methods
	condition.PathPattern = strings.TrimSpace(condition.PathPattern)
	if condition.Scenario != "" {
		scenario, err := ParseTestScenario(string(condition.Scenario))
		if err != nil {
			return OverrideRuleSpec{}, fmt.Errorf("override rule %q: %w", s.ID, err)
		}
		condition.Scenario = scenario
	}
	if condition.SpecStatus != 0 && (condition.SpecStatus < 100 || condition.SpecStatus > 599) {
		return OverrideRuleSpec{}, fmt.Errorf("override rule %q spec_status %d is not a status code", s.ID, condition.SpecStatus)
	}
	s.Condition = condition

	if s.Status == "" {
		s.Status = OverrideRuleStatusActive
	}
	status, err := ParseOverrideRuleStatus(string(s.Status))
	if err != nil {
		return OverrideRuleSpec{}, err
	}
	if status == OverrideRuleStatusSuperseded {
		return OverrideRuleSpec{}, errOverrideSupersededNotSettable
	}
	s.Status = status
	return s, nil
}

// NewOverrideRule builds the first version of an override rule.
func NewOverrideRule(spec OverrideRuleSpec) (*OverrideRule, error) {
	normalized, err := spec.normalize()
	if err != nil {
		return nil, err
	}
	return &OverrideRule{
		ID:        normalized.ID,
		Version:   1,
		Title:     normalized.Title,
		Condition: normalized.Condition,
		Action:    normalized.Action,
		Rationale: normalized.Rationale,
		Status:    normalized.Status,
		Tags:      normalizeTags(normalized.Tags),
		Metadata:  copyMetadata(normalized.Metadata),
		CreatedAt: nowFunc().UTC(),
	}, nil
}

// NextVersion returns the next version of a rule, inheriting every field the
// spec leaves empty. The receiver is not modified.
func (r *OverrideRule) NextVersion(spec OverrideRuleSpec) (*OverrideRule, error) {
	spec.ID = r.ID
	if strings.TrimSpace(spec.Title) == "" {
		spec.Title = r.Title
	}
	if strings.TrimSpace(spec.Rationale) == "" {
		spec.Rationale = r.Rationale
	}
	if spec.Condition.IsEmpty() {
		spec.Condition = r.Condition
	}
	if strings.TrimSpace(string(spec.Action.Kind)) == "" {
		spec.Action = r.Action
	}
	if len(spec.Tags) == 0 {
		spec.Tags = r.Tags
	}
	if len(spec.Metadata) == 0 {
		spec.Metadata = r.Metadata
	}
	next, err := NewOverrideRule(spec)
	if err != nil {
		return nil, err
	}
	next.Version = r.Version + 1
	return next, nil
}

// SetStatus changes the status of a stored version. A superseded version is
// frozen, matching Requirement.SetStatus.
func (r *OverrideRule) SetStatus(status OverrideRuleStatus) error {
	parsed, err := ParseOverrideRuleStatus(string(status))
	if err != nil {
		return err
	}
	if parsed == OverrideRuleStatusSuperseded {
		return errOverrideSupersededNotSettable
	}
	if r.Status == parsed {
		return nil
	}
	if r.Status == OverrideRuleStatusSuperseded {
		return fmt.Errorf("override rule %s v%d is superseded: its status is fixed because a newer version exists", r.ID, r.Version)
	}
	r.Status = parsed
	return nil
}

// Ref is the citation form a test case records in overrides_applied.
func (r OverrideRule) Ref() string {
	return fmt.Sprintf("%s-v%d", r.ID, r.Version)
}

// Satisfied reports whether a case that the rule governs carries the
// expectation the rule requires. A skip-test rule is never satisfied by an
// existing case: the case should not have been generated at all.
func (r OverrideRule) Satisfied(testCase TestCase) bool {
	switch r.Action.Kind {
	case OverrideActionSkipTest:
		return false
	case OverrideActionExpectEmptyCollection:
		return testCase.Expected.Status == r.Action.Status() && testCase.Expected.EmptyCollection
	default:
		return testCase.Expected.Status == r.Action.Status()
	}
}

// ParseVersionedRef splits an "<id>-v<version>" citation, the form every
// versioned entity is cited by: an override rule in overrides_applied, an agent
// definition in a suite's provenance.
func ParseVersionedRef(raw string) (string, int, error) {
	match := versionedRefPattern.FindStringSubmatch(strings.TrimSpace(strings.ToLower(raw)))
	if match == nil {
		return "", 0, fmt.Errorf("reference %q must look like name-v1", raw)
	}
	version, err := strconv.Atoi(match[2])
	if err != nil || version < 1 {
		return "", 0, fmt.Errorf("reference %q has an invalid version", raw)
	}
	return match[1], version, nil
}

// ParseOverrideRef splits a "<rule-id>-v<version>" citation.
func ParseOverrideRef(raw string) (string, int, error) {
	id, version, err := ParseVersionedRef(raw)
	if err != nil {
		return "", 0, fmt.Errorf("override reference %q must look like rule-id-v1", raw)
	}
	return id, version, nil
}
