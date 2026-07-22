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

	if len(tg.Samplers) != 6 {
		t.Fatalf("Expected exactly 6 samplers, got %d", len(tg.Samplers))
	}

	start70 := tg.Samplers[0]
	req70 := tg.Samplers[1]
	end70 := tg.Samplers[2]
	start30 := tg.Samplers[3]
	req30 := tg.Samplers[4]
	end30 := tg.Samplers[5]

	if start70.ControlType != "ThroughputStart" || start70.ThroughputStyle != 1 || start70.ThroughputMaxExpr != "70.0" {
		t.Errorf("Unexpected ThroughputStart 70: %+v", start70)
	}
	if req70.Name != "Req70" {
		t.Errorf("Unexpected sampler name: %s", req70.Name)
	}
	if end70.ControlType != "ThroughputEnd" {
		t.Errorf("Unexpected ThroughputEnd: %+v", end70)
	}

	if start30.ControlType != "ThroughputStart" || start30.ThroughputStyle != 1 || start30.ThroughputMaxExpr != "30.0" {
		t.Errorf("Unexpected ThroughputStart 30: %+v", start30)
	}
	if req30.Name != "Req30" {
		t.Errorf("Unexpected sampler name: %s", req30.Name)
	}
	if end30.ControlType != "ThroughputEnd" {
		t.Errorf("Unexpected ThroughputEnd: %+v", end30)
	}

	t.Logf("Successfully verified Throughput Controller parsing (Req70: %s%%, Req30: %s%%)", start70.ThroughputMaxExpr, start30.ThroughputMaxExpr)
}
