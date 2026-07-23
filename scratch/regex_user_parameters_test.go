package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestRegExUserParametersParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/pre-processors/test_regex_user_parameters.jmx")
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
	mod, ok := req1.PreProcessors[0].(*domain.RegExUserParameters)
	if !ok {
		t.Fatalf("Req1 PreProcessor is not RegExUserParameters")
	}

	if mod.RegexRefName != "REG_VAR" {
		t.Errorf("Expected RegexRefName 'REG_VAR', got '%s'", mod.RegexRefName)
	}
	if mod.ParamNamesGrNr != "1" {
		t.Errorf("Expected ParamNamesGrNr '1', got '%s'", mod.ParamNamesGrNr)
	}
	if mod.ParamValuesGrNr != "2" {
		t.Errorf("Expected ParamValuesGrNr '2', got '%s'", mod.ParamValuesGrNr)
	}
}

func TestRegExUserParametersEngine(t *testing.T) {
	eval := evaluator.NewDefaultEvaluator(map[string]string{
		"REG_VAR_matchNr": "2",
		"REG_VAR_1_g1":    "hiddenField1",
		"REG_VAR_1_g2":    "value1",
		"REG_VAR_2_g1":    "hiddenField2",
		"REG_VAR_2_g2":    "value2",
	})
	session := engine.NewSession(1, &domain.TestPlan{}, &domain.ThreadGroup{}, eval)

	sampler := &domain.Sampler{
		Name: "Req2",
		Request: &domain.RequestTemplate{
			URL: "/submit",
		},
		PreProcessors: []domain.PreProcessor{
			&domain.RegExUserParameters{
				Name:            "RegExUserParams",
				RegexRefName:    "REG_VAR",
				ParamNamesGrNr:  "1",
				ParamValuesGrNr: "2",
			},
		},
	}

	modifiedURL := engine.EvaluatePreProcessors(session, sampler)

	expected := "/submit?hiddenField1=value1&hiddenField2=value2"
	if modifiedURL != expected {
		t.Errorf("Expected URL to be mutated to %s, got %s", expected, modifiedURL)
	}
}
