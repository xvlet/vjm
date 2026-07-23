package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestHTMLLinkParserEngine(t *testing.T) {
	eval := evaluator.NewDefaultEvaluator(map[string]string{})
	session := engine.NewSession(1, &domain.TestPlan{}, &domain.ThreadGroup{}, eval)

	session.LastResponseBody = []byte(`
		<html>
		<body>
			<a href="/test/item/12345">Click here</a>
			<form action="/login/submit">...</form>
		</body>
		</html>
	`)

	sampler := &domain.Sampler{
		Name: "Req2",
		Request: &domain.RequestTemplate{
			URL: "/test/item/.*",
		},
		PreProcessors: []domain.PreProcessor{
			&domain.HTMLLinkParser{Name: "HTMLLinkParser"},
		},
	}

	modifiedURL := engine.EvaluatePreProcessors(session, sampler)

	if modifiedURL != "/test/item/12345" {
		t.Errorf("Expected URL to be mutated to /test/item/12345, got %s", modifiedURL)
	}
}
