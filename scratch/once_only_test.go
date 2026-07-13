package scratch

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestOnceOnlyController(t *testing.T) {
	absPath, err := filepath.Abs("../tests/logic-controller/test_once_only_controller.jmx")
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
	// Expect: OnceOnlyStart, Req1, OnceOnlyEnd, Req2

	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)
	session := engine.NewSession(0, tg, eval)

	// Since the ThreadGroup has 3 loops, we expect:
	// Iter 1: Req1, Req2
	// Iter 2: Req2
	// Iter 3: Req2
	expectedURLs := []string{
		"http://127.0.0.1:58080/test/controller/onceonly/req1",
		"http://127.0.0.1:58080/test/controller/onceonly/req2",
		"http://127.0.0.1:58080/test/controller/onceonly/req2",
		"http://127.0.0.1:58080/test/controller/onceonly/req2",
	}

	var executionURLs []string

	// Mocking execution loop manually like a single worker
	ctx := context.Background()
	_ = ctx

	for iter := 0; iter < 3; iter++ {
		// simulate 1 loop iteration
		for step := 0; step < len(tg.Samplers); step++ {
			if jump, ok := session.InterleaveJump[step]; ok {
				delete(session.InterleaveJump, step)
				step = jump
			}
			if step >= len(tg.Samplers) {
				break
			}
			sampler := tg.Samplers[step]

			if sampler.IsControlFlow {
				switch sampler.ControlType {
				case "OnceOnlyStart":
					if session.LoopCounters[sampler.LoopId] > 0 {
						step = sampler.BlockEndIndex
					} else {
						session.LoopCounters[sampler.LoopId]++
					}
				case "OnceOnlyEnd":
					// ignore
				}
				continue
			}

			// Actual Request
			url := session.Evaluator.Evaluate(sampler.Request.URL)
			executionURLs = append(executionURLs, url)
		}
	}

	if len(executionURLs) != len(expectedURLs) {
		t.Fatalf("Expected %d executions, got %d", len(expectedURLs), len(executionURLs))
	}

	for i, u := range executionURLs {
		if u != expectedURLs[i] {
			t.Errorf("Mismatch at iter %d: expected %s, got %s", i, expectedURLs[i], u)
		}
	}
}
