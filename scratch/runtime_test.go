package scratch

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestRuntimeController(t *testing.T) {
	absPath, err := filepath.Abs("../tests/logic-controller/test_runtime_controller.jmx")
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

	// Expect RuntimeStart, Req1, Req2, RuntimeEnd
	if len(tg.Samplers) != 4 {
		t.Fatalf("Expected 4 samplers, got %d", len(tg.Samplers))
	}

	if tg.Samplers[0].ControlType != "RuntimeStart" {
		t.Errorf("Expected RuntimeStart, got %s", tg.Samplers[0].ControlType)
	}
	if tg.Samplers[3].ControlType != "RuntimeEnd" {
		t.Errorf("Expected RuntimeEnd, got %s", tg.Samplers[3].ControlType)
	}

	if tg.Samplers[0].RuntimeSecondsExpr != "1" {
		t.Errorf("Expected RuntimeSecondsExpr to be 1, got %s", tg.Samplers[0].RuntimeSecondsExpr)
	}

	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)
	session := engine.NewSession(0, tg, eval)

	var executionURLs []string

	// Mocking execution loop manually
	ctx := context.Background()
	_ = ctx

	startTime := time.Now()

	for iter := 0; iter < 1; iter++ {
		// simulate 1 thread iteration
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
				case "RuntimeStart":
					if _, ok := session.RuntimeDeadlines[sampler.LoopId]; !ok {
						// 1 second deadline
						session.RuntimeDeadlines[sampler.LoopId] = time.Now().Add(1 * time.Second)
					}
					deadline := session.RuntimeDeadlines[sampler.LoopId]
					if time.Now().After(deadline) {
						step = sampler.BlockEndIndex
						delete(session.RuntimeDeadlines, sampler.LoopId)
					}
				case "RuntimeEnd":
					step = sampler.LoopJumpIndex - 1
				}
				continue
			}

			// Actual Request
			url := session.Evaluator.Evaluate(sampler.Request.URL)
			executionURLs = append(executionURLs, url)

			// Simulate request delay so that loop takes time
			time.Sleep(300 * time.Millisecond)
		}
	}

	elapsed := time.Since(startTime)
	t.Logf("Elapsed time: %v, total executions: %d", elapsed, len(executionURLs))

	if len(executionURLs) < 2 {
		t.Errorf("Expected at least 2 executions, got %d", len(executionURLs))
	}

	for i, u := range executionURLs {
		if !strings.HasPrefix(u, "http://127.0.0.1:58080/test/controller/runtime/") {
			t.Errorf("Unexpected URL format at iter %d: %s", i, u)
		}
	}
}
