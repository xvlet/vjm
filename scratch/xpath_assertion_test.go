package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestXPathAssertionParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/assertions/test_xpath_assertion.jmx")
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

	// Req1 XPath Assertion
	req1 := tg.Samplers[0]
	if len(req1.Assertions) == 0 {
		t.Fatalf("Req1 missing assertions")
	}
	sa1, ok := req1.Assertions[0].(*domain.XPathAssertion)
	if !ok {
		t.Fatalf("Req1 assertion is not XPathAssertion")
	}
	if sa1.XPath != "/root/book[@id='1']" || sa1.Negate {
		t.Errorf("Req1 XPathAssertion mismatched: %+v", sa1)
	}

	// Req2 XPath Assertion
	req2 := tg.Samplers[1]
	if len(req2.Assertions) == 0 {
		t.Fatalf("Req2 missing assertions")
	}
	sa2, ok := req2.Assertions[0].(*domain.XPathAssertion)
	if !ok {
		t.Fatalf("Req2 assertion is not XPathAssertion")
	}
	if sa2.XPath != "/root/book[@id='2']" || sa2.Negate {
		t.Errorf("Req2 XPathAssertion mismatched: %+v", sa2)
	}
}
