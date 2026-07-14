package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestHTMLLinkParserParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/pre-processors/test_html_link_parser.jmx")
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

	// Req1
	req1 := tg.Samplers[0]
	if len(req1.PreProcessors) == 0 {
		t.Fatalf("Req1 missing PreProcessors")
	}
	_, ok := req1.PreProcessors[0].(*domain.HTMLLinkParser)
	if !ok {
		t.Fatalf("Req1 PreProcessor is not HTMLLinkParser")
	}
}
