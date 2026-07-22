package scratch

import (
	"context"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestResultActionParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/post-processors/test_result_action.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ThreadGroups) == 0 {
		t.Fatalf("Expected ThreadGroup, got 0")
	}

	tg := plan.ThreadGroups[0]
	if len(tg.Samplers) != 2 {
		t.Fatalf("Expected 2 samplers, got %d", len(tg.Samplers))
	}

	sampler := tg.Samplers[0]
	var ra *domain.ResultAction
	for _, ext := range sampler.Extractors {
		if r, ok := ext.(*domain.ResultAction); ok {
			ra = r
			break
		}
	}

	if ra == nil {
		t.Fatalf("Expected ResultAction to be parsed")
		return
	}

	if ra.Action != 4 {
		t.Fatalf("Expected Action=4, got %d", ra.Action)
	}
}

func TestResultActionExecution(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/post-processors/test_result_action.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	eval := evaluator.NewDefaultEvaluator(nil)
	attacker := engine.NewStatefulAttacker(1, vegeta.ConstantPacer{Freq: 1, Per: time.Second}, 5*time.Second)

	results := attacker.Attack(context.Background(), plan, eval)

	var successReqCount int
	var errorReqCount int

	// Let it run for max 2 attacks (1 loop) since duration is 5s but test is short.
	// Actually, the loop is 2. So we should get 2 error requests.
	count := 0
	for res := range results {
		if res.Attack == "Error Request" {
			errorReqCount++
			if res.Code != 500 && res.Error == "" && res.Code < 400 {
				t.Errorf("Expected code >= 400, got %d", res.Code)
			}
		}
		if res.Attack == "Success Request" {
			successReqCount++
		}
		count++
		if count >= 2 {
			break
		}
	}

	if errorReqCount != 2 {
		t.Errorf("Expected Error Request to execute 2 times, got %d", errorReqCount)
	}

	if successReqCount != 0 {
		t.Errorf("Expected Success Request to be skipped, but executed %d times", successReqCount)
	}
}
