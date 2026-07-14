package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestURLRewritingModifierParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/pre-processors/test_http_url_rewriting_modifier.jmx")
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
	mod, ok := req1.PreProcessors[0].(*domain.URLRewritingModifier)
	if !ok {
		t.Fatalf("Req1 PreProcessor is not URLRewritingModifier")
	}

	if mod.ArgumentName != "jsessionid" {
		t.Errorf("Expected ArgumentName 'jsessionid', got '%s'", mod.ArgumentName)
	}
}

func TestURLRewritingModifierEngine(t *testing.T) {
	eval := evaluator.NewDefaultEvaluator(map[string]string{})
	session := engine.NewSession(1, &domain.TestPlan{}, &domain.ThreadGroup{}, eval)

	session.LastResponseBody = []byte(`
		<html>
		<body>
			<a href="/dashboard?jsessionid=ABCD1234EFGH">Click here</a>
		</body>
		</html>
	`)

	sampler := &domain.Sampler{
		Name: "Req2",
		Request: &domain.RequestTemplate{
			URL: "/dashboard",
		},
		PreProcessors: []domain.PreProcessor{
			&domain.URLRewritingModifier{
				Name:          "URLRewrite",
				ArgumentName:  "jsessionid",
				PathExtension: false,
			},
		},
	}

	engine.EvaluatePreProcessors(session, sampler)

	expected := "/dashboard?jsessionid=ABCD1234EFGH"
	if sampler.Request.URL != expected {
		t.Errorf("Expected URL to be mutated to %s, got %s", expected, sampler.Request.URL)
	}
}
