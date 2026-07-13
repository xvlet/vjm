package scratch

import (
	"path/filepath"
	"testing"

	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestIncludeController(t *testing.T) {
	absPath, err := filepath.Abs("../tests/logic-controller/test_include_controller.jmx")
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
	if len(tg.Samplers) != 3 {
		t.Errorf("Expected 3 samplers (Req1, FragmentReq, Req3), got %d", len(tg.Samplers))
		for i, s := range tg.Samplers {
			name := "Unknown"
			if s.Request != nil {
				name = s.Request.URL
			}
			t.Logf("  Sampler %d: %s", i, name)
		}
	} else {
		expectedURLs := []string{
			"http://127.0.0.1:58080/test/controller/include/req1",
			"http://127.0.0.1:58080/test/controller/include/fragment_req",
			"http://127.0.0.1:58080/test/controller/include/req2",
		}
		for i, s := range tg.Samplers {
			if s.Request == nil {
				t.Errorf("Sampler %d is missing Request", i)
				continue
			}
			if s.Request.URL != expectedURLs[i] {
				t.Errorf("Sampler %d URL mismatch. Expected %s, got %s", i, expectedURLs[i], s.Request.URL)
			}
		}
	}
}
