package scratch

import (
	"path/filepath"
	"testing"

	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestThroughputControllerParsing(t *testing.T) {
	absPath, err := filepath.Abs("../tests/logic-controller/test_throughput_controller.jmx")
	if err != nil {
		t.Fatalf("Failed to get absolute path: %v", err)
	}

	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse(absPath)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(plan.ThreadGroups) == 0 {
		t.Fatalf("No thread groups found")
	}

	tg := plan.ThreadGroups[0]

	if len(tg.Samplers) != 2 {
		t.Fatalf("Expected exactly 2 samplers, got %d", len(tg.Samplers))
	}

	req70 := tg.Samplers[0]
	req30 := tg.Samplers[1]

	if req70.Name != "Req70" || req30.Name != "Req30" {
		t.Errorf("Unexpected sampler sequence: %s, %s", req70.Name, req30.Name)
	}

	if req70.Weight != 70.0 {
		t.Errorf("Expected Req70 weight to be 70.0, got %f", req70.Weight)
	}
	if req30.Weight != 30.0 {
		t.Errorf("Expected Req30 weight to be 30.0, got %f", req30.Weight)
	}

	t.Logf("Successfully verified Throughput Controller parsing (Req70: %f%%, Req30: %f%%)", req70.Weight, req30.Weight)
}
