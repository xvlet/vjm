package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestSaveResponsesToFileParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/listeners/test_save_responses_to_file.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ResultSavers) == 0 {
		t.Fatalf("Expected at least 1 ResultSaver at plan level")
	}

	found := false
	for _, saver := range plan.ResultSavers {
		if saver.FilenamePrefix == "response_prefix_" {
			found = true
			if saver.ErrorsOnly {
				t.Errorf("Expected ErrorsOnly false")
			}
			if !saver.SuccessOnly {
				t.Errorf("Expected SuccessOnly true")
			}
			if saver.SkipSuffix {
				t.Errorf("Expected SkipSuffix false")
			}
			if saver.SkipAutoNumber {
				t.Errorf("Expected SkipAutoNumber false")
			}
		}
	}

	if !found {
		t.Fatalf("Save Responses to a file listener not found in plan.ResultSavers")
	}
}
