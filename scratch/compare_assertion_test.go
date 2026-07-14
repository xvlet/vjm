package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestCompareAssertionParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/assertions/test_compare_assertion.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ThreadGroups) == 0 {
		t.Fatalf("No thread groups found")
	}

	tg := plan.ThreadGroups[0]
	if len(tg.Samplers) < 1 {
		t.Fatalf("Expected at least 1 sampler, got %d", len(tg.Samplers))
	}

	// Req1 Compare Assertion
	req1 := tg.Samplers[0]
	if len(req1.Assertions) == 0 {
		t.Fatalf("Req1 missing assertions")
	}
	ca, ok := req1.Assertions[0].(*domain.CompareAssertion)
	if !ok {
		t.Fatalf("Req1 assertion is not CompareAssertion")
	}
	if !ca.CompareContent {
		t.Errorf("Req1 CompareAssertion CompareContent is false, expected true")
	}
	if ca.CompareTime != -1 {
		t.Errorf("Req1 CompareAssertion CompareTime mismatched: %d", ca.CompareTime)
	}
}
