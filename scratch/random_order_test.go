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

func TestRandomOrderController(t *testing.T) {
	absPath, err := filepath.Abs("../tests/logic-controller/test_random_order_controller.jmx")
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

	for i, s := range tg.Samplers {
		if s.ControlType == "RandomOrderStart" {
			t.Logf("RandomOrderStart at %d: ChildStarts=%v, ChildEnds=%v, BlockEndIndex=%d", i, s.RandomOrderChildStarts, s.RandomOrderChildEnds, s.BlockEndIndex)
			if len(s.RandomOrderChildStarts) != 3 {
				t.Errorf("Expected 3 children, got %d", len(s.RandomOrderChildStarts))
			}
		}
	}

	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)
	session := engine.NewSession(0, tg, eval)

	var executionURLs []string

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
				case "RandomOrderStart":
					state := session.RandomOrderState[sampler.LoopId]
					if state == nil || state.CurrentIndex >= len(sampler.RandomOrderChildStarts) {
						order := make([]int, len(sampler.RandomOrderChildStarts))
						for j := range order {
							order[j] = j
						}
						rand.Shuffle(len(order), func(i, j int) {
							order[i], order[j] = order[j], order[i]
						})
						state = &engine.RandomOrderState{
							Order:        order,
							CurrentIndex: 0,
						}
						session.RandomOrderState[sampler.LoopId] = state
					}
					if state.CurrentIndex < len(sampler.RandomOrderChildStarts) {
						childIndex := state.Order[state.CurrentIndex]
						startStep := sampler.RandomOrderChildStarts[childIndex]
						endStep := sampler.RandomOrderChildEnds[childIndex]

						state.CurrentIndex++

						if state.CurrentIndex < len(sampler.RandomOrderChildStarts) {
							session.InterleaveJump[endStep+1] = step
						} else {
							session.InterleaveJump[endStep+1] = sampler.BlockEndIndex
						}

						step = startStep - 1
					} else {
						step = sampler.BlockEndIndex
					}
				case "RandomOrderEnd":
					// ignore
				}
				continue
			}

			// Actual Request
			url := session.Evaluator.Evaluate(sampler.Request.URL)
			executionURLs = append(executionURLs, url)
		}
	}

	// For 3 loops with 3 children each, we expect exactly 9 executions
	if len(executionURLs) != 9 {
		t.Fatalf("Expected 9 executions, got %d", len(executionURLs))
	}

	for i := 0; i < 3; i++ {
		batch := executionURLs[i*3 : (i+1)*3]
		t.Logf("Iter %d order: %v", i+1, batch)

		counts := make(map[string]int)
		for _, u := range batch {
			if !strings.HasPrefix(u, "http://127.0.0.1:58080/test/controller/randomorder/req") {
				t.Errorf("Unexpected URL format: %s", u)
			}
			counts[u]++
		}
		if len(counts) != 3 {
			t.Errorf("Expected all 3 distinct requests in iter %d, got %v", i+1, counts)
		}
	}
}
