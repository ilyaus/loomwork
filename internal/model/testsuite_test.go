package model

import (
	"strings"
	"testing"
)

func linkedCase(id string) TestCase {
	return TestCase{
		ID:             id,
		Name:           "lists the items of order 42",
		RequirementIDs: []string{"req-001"},
		Scenario:       ScenarioHappyPath,
		Request:        TestRequest{Method: MethodGET, Path: "/orders/42/items"},
		Expected:       TestExpectation{Status: 200},
	}
}

func emptyCollectionCase(id string) TestCase {
	return TestCase{
		ID:             id,
		Name:           "returns an empty basket for a missing order",
		RequirementIDs: []string{"req-002"},
		Scenario:       ScenarioMissingItem,
		Request:        TestRequest{Method: MethodGET, Path: "/orders/999/items"},
		Expected:       TestExpectation{Status: 200, EmptyCollection: true},
	}
}

func newSuite(cases ...TestCase) *TestSuite {
	return &TestSuite{SuiteID: "suite-orders-api", Origin: TestSuiteOriginGenerated, Cases: cases}
}

func activeRule(t *testing.T) OverrideRule {
	t.Helper()
	rule, err := NewOverrideRule(testRuleSpec())
	if err != nil {
		t.Fatalf("NewOverrideRule: %v", err)
	}
	return *rule
}

func TestNormalizeAssignsCaseIDsAndKeepsThemDense(t *testing.T) {
	suite := newSuite(linkedCase(""), linkedCase("tc-007"), linkedCase(""))
	if err := suite.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got := strings.Join(suite.CaseIDs, ","); got != "tc-001,tc-007,tc-002" {
		t.Errorf("CaseIDs = %s", got)
	}
}

func TestNormalizeRejectsBrokenCases(t *testing.T) {
	cases := map[string]func(*TestCase){
		"no name":          func(c *TestCase) { c.Name = "" },
		"unknown scenario": func(c *TestCase) { c.Scenario = "made-up" },
		"unknown method":   func(c *TestCase) { c.Request.Method = "FETCH" },
		"relative path":    func(c *TestCase) { c.Request.Path = "orders" },
		"no status":        func(c *TestCase) { c.Expected.Status = 0 },
		"bad requirement":  func(c *TestCase) { c.RequirementIDs = []string{"REQ_1"} },
		"bad override ref": func(c *TestCase) { c.OverridesApplied = []string{"empty-list-on-missing"} },
	}
	for name, mutate := range cases {
		testCase := linkedCase("tc-001")
		mutate(&testCase)
		suite := newSuite(testCase)
		if err := suite.Normalize(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestApplyOverrideRulesAnnotatesFollowedRules(t *testing.T) {
	rule := activeRule(t)
	suite := newSuite(linkedCase("tc-001"), emptyCollectionCase("tc-002"))
	if err := suite.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	audit := suite.ApplyOverrideRules([]OverrideRule{rule})

	if suite.Incomplete {
		t.Errorf("suite should be complete, reasons: %v", suite.IncompleteReasons)
	}
	if got := suite.Cases[1].OverridesApplied; len(got) != 1 || got[0] != rule.Ref() {
		t.Errorf("overrides_applied = %v, want [%s]", got, rule.Ref())
	}
	if len(suite.Cases[0].OverridesApplied) != 0 {
		t.Errorf("case the rule does not govern was annotated: %v", suite.Cases[0].OverridesApplied)
	}
	annotated := audit.Annotated()
	if len(annotated) != 1 || annotated[0].CaseID != "tc-002" || annotated[0].RuleRef != rule.Ref() {
		t.Errorf("Annotated = %+v", annotated)
	}
	if len(audit.Problems()) != 0 {
		t.Errorf("Problems = %+v", audit.Problems())
	}
}

func TestApplyOverrideRulesDoesNotDuplicateAnExistingCitation(t *testing.T) {
	rule := activeRule(t)
	governed := emptyCollectionCase("tc-001")
	governed.OverridesApplied = []string{rule.Ref()}
	suite := newSuite(governed)
	if err := suite.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	audit := suite.ApplyOverrideRules([]OverrideRule{rule})

	if got := suite.Cases[0].OverridesApplied; len(got) != 1 {
		t.Errorf("overrides_applied = %v, want one entry", got)
	}
	if len(audit.Annotated()) != 0 {
		t.Errorf("already-cited rule was reported as annotated: %+v", audit.Annotated())
	}
}

func TestApplyOverrideRulesFlagsUnlinkedCasesIncomplete(t *testing.T) {
	unlinked := linkedCase("tc-002")
	unlinked.RequirementIDs = nil
	suite := newSuite(linkedCase("tc-001"), unlinked)
	if err := suite.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	audit := suite.ApplyOverrideRules(nil)

	if !suite.Incomplete {
		t.Fatal("a suite with an unlinked case must be flagged incomplete, not silently accepted")
	}
	if got := strings.Join(audit.UnlinkedCases, ","); got != "tc-002" {
		t.Errorf("UnlinkedCases = %s", got)
	}
	if len(suite.IncompleteReasons) != 1 || !strings.Contains(suite.IncompleteReasons[0], "tc-002") {
		t.Errorf("IncompleteReasons = %v", suite.IncompleteReasons)
	}
	if len(suite.Cases) != 2 {
		t.Errorf("an incomplete suite keeps its cases; got %d", len(suite.Cases))
	}
}

func TestApplyOverrideRulesFlagsViolationsForbiddenAndUnknownRules(t *testing.T) {
	rule := activeRule(t)

	violating := emptyCollectionCase("tc-001")
	violating.Expected = TestExpectation{Status: 404}
	suite := newSuite(violating)
	if err := suite.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	audit := suite.ApplyOverrideRules([]OverrideRule{rule})
	if !suite.Incomplete || len(audit.Problems()) != 1 || audit.Problems()[0].Kind != OverrideFindingViolated {
		t.Errorf("expected one violation, got incomplete=%t problems=%+v", suite.Incomplete, audit.Problems())
	}

	skipSpec := testRuleSpec()
	skipSpec.Action = OverrideAction{Kind: OverrideActionSkipTest}
	skipRule, err := NewOverrideRule(skipSpec)
	if err != nil {
		t.Fatalf("NewOverrideRule: %v", err)
	}
	forbidden := newSuite(emptyCollectionCase("tc-001"))
	if err := forbidden.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	audit = forbidden.ApplyOverrideRules([]OverrideRule{*skipRule})
	if !forbidden.Incomplete || len(audit.Problems()) != 1 || audit.Problems()[0].Kind != OverrideFindingForbidden {
		t.Errorf("expected one forbidden finding, got incomplete=%t problems=%+v", forbidden.Incomplete, audit.Problems())
	}

	invented := linkedCase("tc-001")
	invented.OverridesApplied = []string{"invented-rule-v1"}
	citing := newSuite(invented)
	if err := citing.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	audit = citing.ApplyOverrideRules([]OverrideRule{rule})
	if !citing.Incomplete || len(audit.Problems()) != 1 || audit.Problems()[0].Kind != OverrideFindingUnknownRule {
		t.Errorf("expected one unknown-rule finding, got incomplete=%t problems=%+v", citing.Incomplete, audit.Problems())
	}
}

func TestApplyOverrideRulesClearsStaleIncompleteness(t *testing.T) {
	suite := newSuite(linkedCase("tc-001"))
	if err := suite.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	suite.Incomplete = true
	suite.IncompleteReasons = []string{"a reason from an earlier audit"}

	suite.ApplyOverrideRules(nil)

	if suite.Incomplete || len(suite.IncompleteReasons) != 0 {
		t.Errorf("stale incompleteness survived: %t %v", suite.Incomplete, suite.IncompleteReasons)
	}
}

func TestParseTestSuiteRejectsUnknownFields(t *testing.T) {
	payload := []byte(`{"suite_id":"suite-orders-api","cases":[],"unexpected":true}`)
	if _, err := ParseTestSuite(payload, ""); err == nil {
		t.Fatal("expected an error: a typo must not drop traceability data silently")
	}
}

func TestParseTestSuiteModelOutputAcceptsFencedJSON(t *testing.T) {
	text := "Here is the suite:\n```json\n{\"suite_id\":\"suite-the-agent-invented\",\"cases\":[{\"name\":\"lists items\"," +
		"\"requirement_ids\":[\"req-001\"],\"overrides_applied\":[],\"scenario\":\"happy-path\"," +
		"\"request\":{\"method\":\"GET\",\"path\":\"/orders/42/items\"},\"expected\":{\"status\":200}}]}\n```\n"
	suite, err := ParseTestSuiteModelOutput(text, "suite-orders-api")
	if err != nil {
		t.Fatalf("ParseTestSuiteModelOutput: %v", err)
	}
	if len(suite.Cases) != 1 || suite.Cases[0].ID != "tc-001" {
		t.Fatalf("cases = %+v", suite.Cases)
	}
	if suite.SuiteID != "suite-orders-api" {
		t.Errorf("SuiteID = %q: the caller's id must win over the agent's", suite.SuiteID)
	}
	if suite.Origin != TestSuiteOriginGenerated {
		t.Errorf("Origin = %q, want generated", suite.Origin)
	}
}

func TestManifestDropsCasesButKeepsTheirIDs(t *testing.T) {
	suite := newSuite(linkedCase("tc-001"), emptyCollectionCase("tc-002"))
	if err := suite.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	manifest := suite.Manifest()
	if len(manifest.Cases) != 0 {
		t.Errorf("Manifest kept %d inline case(s)", len(manifest.Cases))
	}
	if strings.Join(manifest.CaseIDs, ",") != "tc-001,tc-002" {
		t.Errorf("CaseIDs = %v", manifest.CaseIDs)
	}
}
