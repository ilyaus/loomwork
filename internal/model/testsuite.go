package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// HTTPMethod is the request method of a test case.
type HTTPMethod string

const (
	MethodGET     HTTPMethod = "GET"
	MethodPOST    HTTPMethod = "POST"
	MethodPUT     HTTPMethod = "PUT"
	MethodPATCH   HTTPMethod = "PATCH"
	MethodDELETE  HTTPMethod = "DELETE"
	MethodHEAD    HTTPMethod = "HEAD"
	MethodOPTIONS HTTPMethod = "OPTIONS"
)

// HTTPMethods lists every supported method.
func HTTPMethods() []HTTPMethod {
	return []HTTPMethod{MethodGET, MethodPOST, MethodPUT, MethodPATCH, MethodDELETE, MethodHEAD, MethodOPTIONS}
}

// ParseHTTPMethod validates a raw method string.
func ParseHTTPMethod(raw string) (HTTPMethod, error) {
	candidate := HTTPMethod(strings.ToUpper(strings.TrimSpace(raw)))
	for _, known := range HTTPMethods() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown http method %q: supported methods are %s", raw, joinStrings(HTTPMethods()))
}

// TestScenario is the intent of a test case. The set is closed so an override
// rule's condition and a generated case can be matched mechanically instead of
// by prose similarity.
type TestScenario string

const (
	ScenarioHappyPath             TestScenario = "happy-path"
	ScenarioMissingItem           TestScenario = "missing-item"
	ScenarioInvalidInput          TestScenario = "invalid-input"
	ScenarioMissingAuthentication TestScenario = "missing-authentication"
	ScenarioUnauthorized          TestScenario = "unauthorized"
	ScenarioConflict              TestScenario = "conflict"
	ScenarioRateLimit             TestScenario = "rate-limit"
	ScenarioServerError           TestScenario = "server-error"
	ScenarioOther                 TestScenario = "other"
)

// TestScenarios lists every supported scenario.
func TestScenarios() []TestScenario {
	return []TestScenario{
		ScenarioHappyPath,
		ScenarioMissingItem,
		ScenarioInvalidInput,
		ScenarioMissingAuthentication,
		ScenarioUnauthorized,
		ScenarioConflict,
		ScenarioRateLimit,
		ScenarioServerError,
		ScenarioOther,
	}
}

// ParseTestScenario validates a raw scenario string.
func ParseTestScenario(raw string) (TestScenario, error) {
	candidate := TestScenario(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range TestScenarios() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown test scenario %q: supported scenarios are %s", raw, joinStrings(TestScenarios()))
}

var (
	suiteIDPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	testCaseIDPattern    = regexp.MustCompile(`^tc-[0-9]{3,}$`)
	requirementIDPattern = regexp.MustCompile(`^req-[0-9]{3,}$`)
)

// TestRequest is the request definition handed to an external executor.
// Loom-work never issues it.
type TestRequest struct {
	Method        HTTPMethod        `json:"method"`
	Path          string            `json:"path"`
	Query         map[string]string `json:"query,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          string            `json:"body,omitempty"`
	BodyMediaType string            `json:"body_media_type,omitempty"`
}

// TestExpectation is the outcome the executor asserts. Structural only: value
// level comparison is out of scope for this phase.
type TestExpectation struct {
	Status          int      `json:"status"`
	EmptyCollection bool     `json:"empty_collection,omitempty"`
	BodyFields      []string `json:"body_fields,omitempty"`
	MaxLatencyMs    int      `json:"max_latency_ms,omitempty"`
	Notes           string   `json:"notes,omitempty"`
}

// TestCase is one REST API test case. Its JSON shape is fixed by
// docs/schemas/test-case.schema.json. RequirementIDs and OverridesApplied are
// always encoded, even when empty, so an unlinked or unannotated case is visible
// in the stored document rather than inferred from a missing key.
type TestCase struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	RequirementIDs   []string          `json:"requirement_ids"`
	OverridesApplied []string          `json:"overrides_applied"`
	Scenario         TestScenario      `json:"scenario"`
	Request          TestRequest       `json:"request"`
	Expected         TestExpectation   `json:"expected"`
	Tags             []string          `json:"tags,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// CitesOverride reports whether the case already records a rule version.
func (c TestCase) CitesOverride(ref string) bool {
	for _, applied := range c.OverridesApplied {
		if strings.EqualFold(applied, ref) {
			return true
		}
	}
	return false
}

// normalize validates and cleans one case in place. A missing id is left empty
// for the suite to assign.
func (c *TestCase) normalize(position int) error {
	c.ID = strings.TrimSpace(strings.ToLower(c.ID))
	if c.ID != "" && !testCaseIDPattern.MatchString(c.ID) {
		return fmt.Errorf("test case %d id %q must look like tc-001", position, c.ID)
	}
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return fmt.Errorf("test case %d needs a name", position)
	}
	scenario, err := ParseTestScenario(string(c.Scenario))
	if err != nil {
		return fmt.Errorf("test case %q: %w", c.Name, err)
	}
	c.Scenario = scenario

	method, err := ParseHTTPMethod(string(c.Request.Method))
	if err != nil {
		return fmt.Errorf("test case %q: %w", c.Name, err)
	}
	c.Request.Method = method
	c.Request.Path = strings.TrimSpace(c.Request.Path)
	if !strings.HasPrefix(c.Request.Path, "/") {
		return fmt.Errorf("test case %q request path %q must start with /", c.Name, c.Request.Path)
	}
	if c.Expected.Status < 100 || c.Expected.Status > 599 {
		return fmt.Errorf("test case %q expected status %d is not a status code", c.Name, c.Expected.Status)
	}

	requirements := make([]string, 0, len(c.RequirementIDs))
	seen := map[string]bool{}
	for _, id := range c.RequirementIDs {
		normalized := strings.TrimSpace(strings.ToLower(id))
		if normalized == "" {
			continue
		}
		if !requirementIDPattern.MatchString(normalized) {
			return fmt.Errorf("test case %q requirement link %q must look like req-001", c.Name, id)
		}
		if !seen[normalized] {
			seen[normalized] = true
			requirements = append(requirements, normalized)
		}
	}
	c.RequirementIDs = requirements

	overrides := make([]string, 0, len(c.OverridesApplied))
	seen = map[string]bool{}
	for _, ref := range c.OverridesApplied {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		id, version, err := ParseOverrideRef(ref)
		if err != nil {
			return fmt.Errorf("test case %q: %w", c.Name, err)
		}
		normalized := fmt.Sprintf("%s-v%d", id, version)
		if !seen[normalized] {
			seen[normalized] = true
			overrides = append(overrides, normalized)
		}
	}
	c.OverridesApplied = overrides
	c.Tags = normalizeTags(c.Tags)
	return nil
}

// TestSuiteOrigin records how a suite version was produced. Both origins are
// versioned identically, so an imported suite is a first-class execution target.
type TestSuiteOrigin string

const (
	// TestSuiteOriginGenerated is agent generation through the adapter layer.
	TestSuiteOriginGenerated TestSuiteOrigin = "generated"
	// TestSuiteOriginImported is a suite authored outside Loom-work.
	TestSuiteOriginImported TestSuiteOrigin = "imported"
)

// TestSuiteOrigins lists every supported origin.
func TestSuiteOrigins() []TestSuiteOrigin {
	return []TestSuiteOrigin{TestSuiteOriginGenerated, TestSuiteOriginImported}
}

// ParseTestSuiteOrigin validates a raw origin string.
func ParseTestSuiteOrigin(raw string) (TestSuiteOrigin, error) {
	candidate := TestSuiteOrigin(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range TestSuiteOrigins() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown test suite origin %q: supported origins are %s", raw, joinStrings(TestSuiteOrigins()))
}

// TestSuite is one immutable version of a test suite: the manifest plus, in
// memory and in an interchange document, its cases. Its JSON shape is fixed by
// the suite definition in docs/schemas/test-case.schema.json. The stored
// manifest carries CaseIDs and the cases live in their own files; the same
// struct with Cases populated is the generation output and import format.
type TestSuite struct {
	SuiteID             string            `json:"suite_id"`
	Version             int               `json:"version"`
	Origin              TestSuiteOrigin   `json:"origin"`
	Title               string            `json:"title,omitempty"`
	Description         string            `json:"description,omitempty"`
	Cases               []TestCase        `json:"cases,omitempty"`
	CaseIDs             []string          `json:"case_ids,omitempty"`
	Incomplete          bool              `json:"incomplete"`
	IncompleteReasons   []string          `json:"incomplete_reasons,omitempty"`
	AgentDefinition     string            `json:"agent_definition,omitempty"`
	RequirementVersions map[string]int    `json:"requirement_versions,omitempty"`
	OverrideRules       []string          `json:"override_rules,omitempty"`
	SpecRef             string            `json:"spec_ref,omitempty"`
	Tags                []string          `json:"tags,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
}

// ParseTestSuite decodes an interchange document: a suite authored outside
// Loom-work or produced by a generator. Unknown fields are rejected so a typo in
// a hand-written suite surfaces instead of dropping traceability data silently.
// A non-empty suiteID replaces the document's own id, which is how a foreign
// suite is filed under a local one.
func ParseTestSuite(raw []byte, suiteID string) (*TestSuite, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var suite TestSuite
	if err := decoder.Decode(&suite); err != nil {
		return nil, fmt.Errorf("parse test suite: %w", err)
	}
	if strings.TrimSpace(suiteID) != "" {
		suite.SuiteID = suiteID
	}
	if err := suite.Normalize(); err != nil {
		return nil, fmt.Errorf("invalid test suite: %w", err)
	}
	return &suite, nil
}

// ParseTestSuiteModelOutput decodes a suite from raw agent text. Agents wrap
// JSON in prose or code fences, so the outermost object is decoded and unknown
// fields are tolerated. The suite id comes from the caller rather than the
// response: identity and provenance are the workbench's to assign, and an agent
// must not be able to write into a suite the run was not for.
func ParseTestSuiteModelOutput(text, suiteID string) (*TestSuite, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("agent response contains no JSON object")
	}
	var suite TestSuite
	if err := json.Unmarshal([]byte(text[start:end+1]), &suite); err != nil {
		return nil, fmt.Errorf("parse agent response as test suite: %w", err)
	}
	suite.SuiteID = suiteID
	if err := suite.Normalize(); err != nil {
		return nil, fmt.Errorf("agent response is not a valid test suite: %w", err)
	}
	return &suite, nil
}

// NormalizeSuiteID lowercases and validates a suite id. Read paths that build a
// filesystem path from a caller's id go through it, so identity is as strict on
// the way out of the store as Normalize makes it on the way in.
func NormalizeSuiteID(raw string) (string, error) {
	id := strings.TrimSpace(strings.ToLower(raw))
	if !suiteIDPattern.MatchString(id) {
		return "", fmt.Errorf("test suite id %q must be lowercase letters, digits, and dashes", id)
	}
	return id, nil
}

// Normalize validates the suite and its cases in place and assigns ids to cases
// that arrived without one, keeping the tc-NNN sequence dense and unique.
func (s *TestSuite) Normalize() error {
	suiteID, err := NormalizeSuiteID(s.SuiteID)
	if err != nil {
		return err
	}
	s.SuiteID = suiteID
	if s.Origin == "" {
		s.Origin = TestSuiteOriginGenerated
	}
	origin, err := ParseTestSuiteOrigin(string(s.Origin))
	if err != nil {
		return err
	}
	s.Origin = origin
	s.Title = strings.TrimSpace(s.Title)
	s.Description = strings.TrimSpace(s.Description)
	s.SpecRef = strings.TrimSpace(s.SpecRef)
	s.Tags = normalizeTags(s.Tags)

	if s.AgentDefinition != "" {
		if _, _, err := ParseVersionedRef(s.AgentDefinition); err != nil {
			return fmt.Errorf("test suite agent_definition %q must look like agent-name-v1", s.AgentDefinition)
		}
	}
	rules := make([]string, 0, len(s.OverrideRules))
	for _, ref := range s.OverrideRules {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		id, version, err := ParseOverrideRef(ref)
		if err != nil {
			return err
		}
		rules = append(rules, fmt.Sprintf("%s-v%d", id, version))
	}
	sort.Strings(rules)
	s.OverrideRules = rules

	for id, version := range s.RequirementVersions {
		if !requirementIDPattern.MatchString(id) {
			return fmt.Errorf("test suite requirement_versions key %q must look like req-001", id)
		}
		if version < 1 {
			return fmt.Errorf("test suite requirement_versions[%s] must be 1 or greater", id)
		}
	}

	used := map[string]bool{}
	for position := range s.Cases {
		if err := s.Cases[position].normalize(position + 1); err != nil {
			return err
		}
		if id := s.Cases[position].ID; id != "" {
			if used[id] {
				return fmt.Errorf("test case id %q appears twice in suite %s", id, s.SuiteID)
			}
			used[id] = true
		}
	}
	next := 1
	for position := range s.Cases {
		if s.Cases[position].ID != "" {
			continue
		}
		for {
			candidate := fmt.Sprintf("tc-%03d", next)
			next++
			if !used[candidate] {
				s.Cases[position].ID = candidate
				used[candidate] = true
				break
			}
		}
	}
	if len(s.Cases) > 0 {
		ids := make([]string, 0, len(s.Cases))
		for _, testCase := range s.Cases {
			ids = append(ids, testCase.ID)
		}
		s.CaseIDs = ids
	}
	return nil
}

// OverrideFindingKind classifies one audit outcome.
type OverrideFindingKind string

const (
	// OverrideFindingAnnotated means the case carried the expectation a rule
	// requires but did not cite it, so the citation was added.
	OverrideFindingAnnotated OverrideFindingKind = "annotated"
	// OverrideFindingViolated means a rule governs the case but the case does
	// not carry the expectation the rule requires.
	OverrideFindingViolated OverrideFindingKind = "violated"
	// OverrideFindingForbidden means a skip-test rule governs the case, so the
	// case should not exist.
	OverrideFindingForbidden OverrideFindingKind = "forbidden"
	// OverrideFindingUnknownRule means the case cites a rule version that was
	// not supplied as an input to the suite.
	OverrideFindingUnknownRule OverrideFindingKind = "unknown-rule"
)

// OverrideFinding is one result of auditing a case against the override rules.
type OverrideFinding struct {
	Kind    OverrideFindingKind `json:"kind"`
	CaseID  string              `json:"case_id"`
	RuleRef string              `json:"rule_ref,omitempty"`
	Detail  string              `json:"detail"`
}

// TestSuiteAudit reports what auditing a suite found. Annotations are recorded
// as findings too: adding a citation changes the stored suite, so the change has
// to be visible to a reviewer.
type TestSuiteAudit struct {
	UnlinkedCases []string          `json:"unlinked_cases,omitempty"`
	Findings      []OverrideFinding `json:"findings,omitempty"`
}

// Annotated returns the citations the audit added.
func (a TestSuiteAudit) Annotated() []OverrideFinding {
	return a.byKind(OverrideFindingAnnotated)
}

// Problems returns the findings that make a suite incomplete.
func (a TestSuiteAudit) Problems() []OverrideFinding {
	problems := make([]OverrideFinding, 0, len(a.Findings))
	for _, finding := range a.Findings {
		if finding.Kind != OverrideFindingAnnotated {
			problems = append(problems, finding)
		}
	}
	return problems
}

func (a TestSuiteAudit) byKind(kind OverrideFindingKind) []OverrideFinding {
	matched := make([]OverrideFinding, 0, len(a.Findings))
	for _, finding := range a.Findings {
		if finding.Kind == kind {
			matched = append(matched, finding)
		}
	}
	return matched
}

// ApplyOverrideRules audits every case against the supplied rules and sets the
// suite's incompleteness flag. It is the deterministic half of the hybrid
// override-rule design: where a rule's structured condition governs a case, the
// suite cannot claim to follow the spec silently.
//
//   - A case that carries a rule's required expectation without citing it is
//     annotated: the rule version is added to overrides_applied, so an auditor
//     never has to re-read agent reasoning to learn which rules shaped which
//     tests.
//   - A case a rule governs whose expectation contradicts the rule, a case a
//     skip-test rule forbids, a case citing a rule that was not an input, and a
//     case with no requirement link each make the suite incomplete.
//
// The suite is still storable when incomplete; the flag exists so the UI
// surfaces the problem rather than silently accepting the suite.
func (s *TestSuite) ApplyOverrideRules(rules []OverrideRule) TestSuiteAudit {
	byRef := make(map[string]OverrideRule, len(rules))
	for _, rule := range rules {
		byRef[rule.Ref()] = rule
	}

	audit := TestSuiteAudit{}
	for position := range s.Cases {
		testCase := &s.Cases[position]
		if len(testCase.RequirementIDs) == 0 {
			audit.UnlinkedCases = append(audit.UnlinkedCases, testCase.ID)
		}
		for _, ref := range testCase.OverridesApplied {
			if _, known := byRef[ref]; !known {
				audit.Findings = append(audit.Findings, OverrideFinding{
					Kind:    OverrideFindingUnknownRule,
					CaseID:  testCase.ID,
					RuleRef: ref,
					Detail:  fmt.Sprintf("cites override rule %s, which was not an input to this suite", ref),
				})
			}
		}
		for _, ref := range sortedRuleRefs(byRef) {
			rule := byRef[ref]
			if !rule.Condition.Matches(*testCase) {
				continue
			}
			switch {
			case rule.Action.Kind == OverrideActionSkipTest:
				audit.Findings = append(audit.Findings, OverrideFinding{
					Kind:    OverrideFindingForbidden,
					CaseID:  testCase.ID,
					RuleRef: ref,
					Detail:  fmt.Sprintf("override rule %s (%s) forbids testing this case", ref, rule.Title),
				})
			case rule.Satisfied(*testCase):
				if !testCase.CitesOverride(ref) {
					testCase.OverridesApplied = append(testCase.OverridesApplied, ref)
					audit.Findings = append(audit.Findings, OverrideFinding{
						Kind:    OverrideFindingAnnotated,
						CaseID:  testCase.ID,
						RuleRef: ref,
						Detail:  fmt.Sprintf("case follows override rule %s (%s); citation added", ref, rule.Title),
					})
				}
			default:
				audit.Findings = append(audit.Findings, OverrideFinding{
					Kind:    OverrideFindingViolated,
					CaseID:  testCase.ID,
					RuleRef: ref,
					Detail: fmt.Sprintf("override rule %s (%s) requires %s, but the case expects %d",
						ref, rule.Title, describeAction(rule.Action), testCase.Expected.Status),
				})
			}
		}
	}

	s.Incomplete = false
	s.IncompleteReasons = nil
	if len(s.Cases) == 0 {
		s.markIncomplete("the suite has no test cases")
	}
	if len(audit.UnlinkedCases) > 0 {
		s.markIncomplete(fmt.Sprintf("%d test case(s) have no requirement link: %s",
			len(audit.UnlinkedCases), strings.Join(audit.UnlinkedCases, ", ")))
	}
	for _, finding := range audit.Problems() {
		s.markIncomplete(fmt.Sprintf("%s: %s", finding.CaseID, finding.Detail))
	}
	return audit
}

// FlagIncomplete records an additional reason a suite cannot be trusted as
// complete. It exists for checks that need context the model layer does not have,
// such as whether a linked requirement exists in the project.
func (s *TestSuite) FlagIncomplete(reason string) {
	if strings.TrimSpace(reason) == "" {
		return
	}
	s.markIncomplete(reason)
}

func (s *TestSuite) markIncomplete(reason string) {
	s.Incomplete = true
	s.IncompleteReasons = append(s.IncompleteReasons, reason)
}

func describeAction(action OverrideAction) string {
	switch action.Kind {
	case OverrideActionExpectEmptyCollection:
		return fmt.Sprintf("status %d with an empty collection body", action.Status())
	case OverrideActionSkipTest:
		return "no test at all"
	default:
		return "status " + strconv.Itoa(action.Status())
	}
}

func sortedRuleRefs(rules map[string]OverrideRule) []string {
	refs := make([]string, 0, len(rules))
	for ref := range rules {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

// Ref is the citation form of one suite version.
func (s TestSuite) Ref() string {
	return fmt.Sprintf("%s-v%d", s.SuiteID, s.Version)
}

// Manifest returns a copy of the suite without its cases, for storing as
// suite.json alongside the individual case files.
func (s TestSuite) Manifest() TestSuite {
	manifest := s
	manifest.Cases = nil
	return manifest
}
