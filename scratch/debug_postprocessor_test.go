package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestDebugPostProcessorParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/post-processors/test_debug_postprocessor.jmx")
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

	req1 := tg.Samplers[0]
	if len(req1.Extractors) == 0 {
		t.Fatalf("Req1 missing Extractors")
	}

	ext, ok := req1.Extractors[0].(*domain.DebugPostProcessor)
	if !ok {
		t.Fatalf("Req1 Extractor is not DebugPostProcessor")
	}

	if ext.Name != "Debug PostProcessor" {
		t.Errorf("Expected Name 'Debug PostProcessor', got '%s'", ext.Name)
	}
	if ext.DisplayJMeterProperties != false {
		t.Errorf("Expected DisplayJMeterProperties false, got %v", ext.DisplayJMeterProperties)
	}
	if ext.DisplayJMeterVariables != true {
		t.Errorf("Expected DisplayJMeterVariables true, got %v (Name=%s)", ext.DisplayJMeterVariables, ext.Name)
	}
	if ext.DisplaySamplerProperties != true {
		t.Errorf("Expected DisplaySamplerProperties true, got %v", ext.DisplaySamplerProperties)
	}
	if ext.DisplaySystemProperties != false {
		t.Errorf("Expected DisplaySystemProperties false, got %v", ext.DisplaySystemProperties)
	}

	// Verify it acts as a No-Op extractor
	if ext.RefName() != "__DEBUG__" {
		t.Errorf("Expected RefName '__DEBUG__', got '%s'", ext.RefName())
	}
	v, _ := ext.DefaultValue()
	if v != "" {
		t.Errorf("Expected empty string, got '%s'", v)
	}

	val, ok := ext.Extract([]byte("body"))
	if ok || val != "" {
		t.Errorf("Expected Extract to return ('', false), got ('%s', %v)", val, ok)
	}
}
