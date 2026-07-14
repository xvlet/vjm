package scratch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestSampleTimeoutParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/pre-processors/test_sample_timeout.jmx")
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
	if len(req1.PreProcessors) == 0 {
		t.Fatalf("Req1 missing PreProcessors")
	}
	mod, ok := req1.PreProcessors[0].(*domain.SampleTimeout)
	if !ok {
		t.Fatalf("Req1 PreProcessor is not SampleTimeout")
	}

	if mod.Timeout != "10" {
		t.Errorf("Expected Timeout '10', got '%s'", mod.Timeout)
	}
}

func TestSampleTimeoutEngine(t *testing.T) {
	// Create a mock server that sleeps for 50ms
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	eval := evaluator.NewDefaultEvaluator(nil)

	plan := &domain.TestPlan{
		ThreadGroups: []*domain.ThreadGroup{
			{
				Samplers: []*domain.Sampler{
					{
						Name: "TimeoutReq",
						Request: &domain.RequestTemplate{
							URL:    ts.URL,
							Method: "GET",
						},
						PreProcessors: []domain.PreProcessor{
							&domain.SampleTimeout{
								Name:    "Timeout",
								Timeout: "10", // 10ms timeout
							},
						},
					},
				},
				NumThreads: 1,
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	attacker := engine.NewStatefulAttacker(1, vegeta.ConstantPacer{Freq: 1, Per: time.Second}, 0)
	ch := attacker.Attack(ctx, plan, eval)

	res := <-ch
	if res.Error == "" {
		t.Errorf("Expected timeout error, got none. Latency: %v", res.Latency)
	}
}
