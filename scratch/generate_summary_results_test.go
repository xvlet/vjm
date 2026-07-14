package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestGenerateSummaryResultsParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/listeners/test_generate_summary_results.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.Summarisers) == 0 {
		t.Fatalf("Expected at least 1 Summariser at plan level")
	}

	found := false
	for _, sum := range plan.Summarisers {
		if sum.Name == "Generate Summary Results" {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("Generate Summary Results listener not found in plan.Summarisers")
	}
}
