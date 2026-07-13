package scratch

import (
	"context"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestRandomController(t *testing.T) {
	absPath, err := filepath.Abs("../tests/logic-controller/test_random_controller.jmx")
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
	// Expect: RandomStart, Req1, Req2, Req3, RandomEnd

	for i, s := range tg.Samplers {
		if s.ControlType == "RandomStart" {
			t.Logf("RandomStart at %d: ChildStarts=%v, ChildEnds=%v, BlockEndIndex=%d", i, s.RandomChildStarts, s.RandomChildEnds, s.BlockEndIndex)
			if len(s.RandomChildStarts) != 3 {
				t.Errorf("Expected 3 children, got %d", len(s.RandomChildStarts))
			}
		}
	}

	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)
	session := engine.NewSession(0, plan, tg, eval)

	var executionURLs []string

	// Mocking execution loop manually like a single worker
	ctx := context.Background()
	_ = ctx

	for iter := 0; iter < 5; iter++ {
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
				case "RandomStart":
					if len(sampler.RandomChildStarts) > 0 {
						childIndex := rand.IntN(len(sampler.RandomChildStarts))
						startStep := sampler.RandomChildStarts[childIndex]
						endStep := sampler.RandomChildEnds[childIndex]

						session.InterleaveJump[endStep+1] = sampler.BlockEndIndex
						step = startStep - 1
					} else {
						step = sampler.BlockEndIndex
					}
				case "RandomEnd":
					// ignore
				}
				continue
			}

			// Actual Request
			url := session.Evaluator.Evaluate(sampler.Request.URL)
			executionURLs = append(executionURLs, url)
		}
	}

	// For 5 loops, we expect exactly 5 executions
	if len(executionURLs) != 5 {
		t.Fatalf("Expected 5 executions, got %d", len(executionURLs))
	}

	for i, u := range executionURLs {
		if !strings.HasPrefix(u, "http://127.0.0.1:58080/test/controller/random/req") {
			t.Errorf("Unexpected URL format at iter %d: %s", i, u)
		}
		t.Logf("Iter %d executed: %s", i, u)
	}
}
