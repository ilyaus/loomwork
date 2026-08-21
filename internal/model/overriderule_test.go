package model

import "testing"

func testRuleSpec() OverrideRuleSpec {
	return OverrideRuleSpec{
		ID:    "empty-list-on-missing",
		Title: "Missing collections return an empty list",
		Condition: OverrideCondition{
			Methods:     []HTTPMethod{MethodGET},
			PathPattern: "/orders/*/items",
			Scenario:    ScenarioMissingItem,
			SpecStatus:  404,
		},
		Action:    OverrideAction{Kind: OverrideActionExpectEmptyCollection},
		Rationale: "The storefront treats an absent order as an empty basket, so a 404 would break checkout.",
	}
}

func TestNewOverrideRuleRequiresRationale(t *testing.T) {
	spec := testRuleSpec()
	spec.Rationale = "   "
	if _, err := NewOverrideRule(spec); err == nil {
		t.Fatal("expected an error: the hybrid rule shape requires free-text reasoning")
	}
}

func TestNewOverrideRuleDefaultsEmptyCollectionStatus(t *testing.T) {
	rule, err := NewOverrideRule(testRuleSpec())
	if err != nil {
		t.Fatalf("NewOverrideRule: %v", err)
	}
	if rule.Version != 1 || rule.Ref() != "empty-list-on-missing-v1" {
		t.Errorf("Ref = %q, version = %d", rule.Ref(), rule.Version)
	}
	if rule.Status != OverrideRuleStatusActive {
		t.Errorf("Status = %q, want active", rule.Status)
	}
	if rule.Action.Status() != DefaultEmptyCollectionStatus {
		t.Errorf("Action.Status = %d, want %d", rule.Action.Status(), DefaultEmptyCollectionStatus)
	}
}

func TestOverrideRuleNextVersionSupersedesNothingOnTheReceiver(t *testing.T) {
	first, err := NewOverrideRule(testRuleSpec())
	if err != nil {
		t.Fatalf("NewOverrideRule: %v", err)
	}
	second, err := first.NextVersion(OverrideRuleSpec{Rationale: "Checkout now tolerates a 404, but QA still asserts the empty basket."})
	if err != nil {
		t.Fatalf("NextVersion: %v", err)
	}
	if second.Version != 2 {
		t.Errorf("Version = %d, want 2", second.Version)
	}
	if second.Title != first.Title || second.Action.Kind != first.Action.Kind {
		t.Errorf("unchanged fields were lost: %+v", second)
	}
	if second.Rationale == first.Rationale {
		t.Error("rationale was not replaced")
	}
	if first.Status != OverrideRuleStatusActive {
		t.Errorf("receiver status = %q; superseding is the store's job", first.Status)
	}
}

func TestOverrideRuleSetStatusRejectsSuperseded(t *testing.T) {
	rule, err := NewOverrideRule(testRuleSpec())
	if err != nil {
		t.Fatalf("NewOverrideRule: %v", err)
	}
	if err := rule.SetStatus(OverrideRuleStatusSuperseded); err == nil {
		t.Fatal("expected an error: superseded is set by publishing a new version")
	}
	if err := rule.SetStatus(OverrideRuleStatusObsolete); err != nil {
		t.Fatalf("SetStatus(obsolete): %v", err)
	}
	if rule.Status != OverrideRuleStatusObsolete {
		t.Errorf("Status = %q, want obsolete", rule.Status)
	}
}

func TestOverrideConditionMatchesOnMethodPathAndScenario(t *testing.T) {
	rule, err := NewOverrideRule(testRuleSpec())
	if err != nil {
		t.Fatalf("NewOverrideRule: %v", err)
	}
	governed := TestCase{
		Scenario: ScenarioMissingItem,
		Request:  TestRequest{Method: MethodGET, Path: "/orders/42/items"},
		Expected: TestExpectation{Status: 200, EmptyCollection: true},
	}
	if !rule.Condition.Matches(governed) {
		t.Error("rule should govern the matching case")
	}
	if !rule.Satisfied(governed) {
		t.Error("case follows the rule, so it should be satisfied")
	}

	for name, testCase := range map[string]TestCase{
		"other method":   {Scenario: ScenarioMissingItem, Request: TestRequest{Method: MethodPOST, Path: "/orders/42/items"}},
		"other path":     {Scenario: ScenarioMissingItem, Request: TestRequest{Method: MethodGET, Path: "/orders/42"}},
		"other scenario": {Scenario: ScenarioHappyPath, Request: TestRequest{Method: MethodGET, Path: "/orders/42/items"}},
	} {
		if rule.Condition.Matches(testCase) {
			t.Errorf("%s: rule should not govern this case", name)
		}
	}
}

func TestOverrideRuleSatisfiedRequiresTheEmptyCollectionMarker(t *testing.T) {
	rule, err := NewOverrideRule(testRuleSpec())
	if err != nil {
		t.Fatalf("NewOverrideRule: %v", err)
	}
	rightStatusOnly := TestCase{
		Scenario: ScenarioMissingItem,
		Request:  TestRequest{Method: MethodGET, Path: "/orders/42/items"},
		Expected: TestExpectation{Status: 200},
	}
	if rule.Satisfied(rightStatusOnly) {
		t.Error("a 200 without the empty-collection expectation does not satisfy the rule")
	}
}

func TestParseOverrideRef(t *testing.T) {
	id, version, err := ParseOverrideRef(" Empty-List-On-Missing-v3 ")
	if err != nil {
		t.Fatalf("ParseOverrideRef: %v", err)
	}
	if id != "empty-list-on-missing" || version != 3 {
		t.Errorf("id = %q, version = %d", id, version)
	}
	for _, raw := range []string{"empty-list-on-missing", "empty-list-on-missing-v0", "-v1", "rule-vx"} {
		if _, _, err := ParseOverrideRef(raw); err == nil {
			t.Errorf("ParseOverrideRef(%q): expected an error", raw)
		}
	}
}
