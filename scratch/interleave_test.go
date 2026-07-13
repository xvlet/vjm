package scratch

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestInterleaveController(t *testing.T) {
	absPath, err := filepath.Abs("../tests/logic-controller/test_interleave_controller.jmx")
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
	// Expect: LoopStart, InterleaveStart, Req1, Req2, Req3, InterleaveEnd, LoopEnd

	for i, s := range tg.Samplers {
		if s.ControlType == "InterleaveStart" {
			t.Logf("InterleaveStart at %d: ChildStarts=%v, ChildEnds=%v, BlockEndIndex=%d", i, s.InterleaveChildStarts, s.InterleaveChildEnds, s.BlockEndIndex)
			if len(s.InterleaveChildStarts) != 3 {
				t.Errorf("Expected 3 children, got %d", len(s.InterleaveChildStarts))
			}
		}
	}

	// Test Engine stateful execution
	// We'll mock the pacer to just run 3 hits
	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)
	session := engine.NewSession(0, tg, eval)

	expectedURLs := []string{
		"http://127.0.0.1:58080/test/controller/interleave/req1",
		"http://127.0.0.1:58080/test/controller/interleave/req2",
		"http://127.0.0.1:58080/test/controller/interleave/req3",
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
				case "LoopStart":
					session.LoopCounters[sampler.LoopId]++
				case "LoopEnd":
					// ignore
				case "InterleaveStart":
					session.LoopCounters[sampler.LoopId]++
					if len(sampler.InterleaveChildStarts) > 0 {
						childIndex := (session.LoopCounters[sampler.LoopId] - 1) % len(sampler.InterleaveChildStarts)
						startStep := sampler.InterleaveChildStarts[childIndex]
						endStep := sampler.InterleaveChildEnds[childIndex]
						session.InterleaveJump[endStep+1] = sampler.BlockEndIndex
						step = startStep - 1
					} else {
						step = sampler.BlockEndIndex
					}
				case "InterleaveEnd":
					// ignore
				}
				continue
			}

			// Actual Request
			url := session.Evaluator.Evaluate(sampler.Request.URL)
			executionURLs = append(executionURLs, url)
		}
	}

	if len(executionURLs) != 3 {
		t.Fatalf("Expected 3 executions, got %d", len(executionURLs))
	}

	for i, u := range executionURLs {
		if u != expectedURLs[i] {
			t.Errorf("Mismatch at iter %d: expected %s, got %s", i, expectedURLs[i], u)
		}
	}
}
