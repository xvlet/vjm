package scratch

import (
	"path/filepath"
	"testing"

	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestSimpleControllerParsing(t *testing.T) {
	absPath, err := filepath.Abs("../tests/logic-controller/test_simple_controller.jmx")
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

	// Simple Controller (GenericController) is completely transparent.
	// It doesn't create any control flow objects.
	// We should just expect exactly 3 HTTP samplers (Req1, Req2, Req3) inside the thread group.

	if len(tg.Samplers) != 3 {
		t.Errorf("Expected exactly 3 samplers, got %d", len(tg.Samplers))
	}

	for i, s := range tg.Samplers {
		t.Logf("Parsed Sampler %d: %s (URL: %s)", i, s.Name, s.Request.URL)
		if s.IsControlFlow {
			t.Errorf("Sampler %s should not be a control flow node", s.Name)
		}
	}

	if len(tg.Samplers) == 3 {
		if tg.Samplers[0].Name != "Req1" || tg.Samplers[1].Name != "Req2" || tg.Samplers[2].Name != "Req3" {
			t.Errorf("Samplers are not in expected sequence: %v", tg.Samplers)
		}
	}
}
