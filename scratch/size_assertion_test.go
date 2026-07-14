package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestSizeAssertionParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/assertions/test_size_assertion.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ThreadGroups) == 0 {
		t.Fatalf("No thread groups found")
	}

	tg := plan.ThreadGroups[0]
	if len(tg.Samplers) < 2 {
		t.Fatalf("Expected at least 2 samplers, got %d", len(tg.Samplers))
	}

	// Req1 Size Assertion
	req1 := tg.Samplers[0]
	if len(req1.Assertions) == 0 {
		t.Fatalf("Req1 missing assertions")
	}
	sa1, ok := req1.Assertions[0].(*domain.SizeAssertion)
	if !ok {
		t.Fatalf("Req1 assertion is not SizeAssertion")
	}
	if sa1.TestField != "SizeAssertion.response_network_size" || sa1.Size != "0" || sa1.Operator != 3 {
		t.Errorf("Req1 SizeAssertion mismatched: %+v", sa1)
	}

	// Req2 Size Assertion
	req2 := tg.Samplers[1]
	if len(req2.Assertions) == 0 {
		t.Fatalf("Req2 missing assertions")
	}
	sa2, ok := req2.Assertions[0].(*domain.SizeAssertion)
	if !ok {
		t.Fatalf("Req2 assertion is not SizeAssertion")
	}
	if sa2.TestField != "SizeAssertion.response_data" || sa2.Size != "0" || sa2.Operator != 1 {
		t.Errorf("Req2 SizeAssertion mismatched: %+v", sa2)
	}
}
