package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestResponseTimeGraphParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/listeners/test_response_time_graph.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ResultCollectors) == 0 {
		t.Fatalf("Expected at least 1 ResultCollector at plan level")
	}

	found := false
	for _, rc := range plan.ResultCollectors {
		if rc.Filename == "response_time_graph.jtl" && rc.Name == "Response Time Graph" {
			found = true
			if rc.ErrorLogging {
				t.Errorf("Expected ErrorLogging false")
			}
		}
	}

	if !found {
		t.Fatalf("Response Time Graph listener not found in plan.ResultCollectors")
	}
}
