package scratch

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestSwitchController(t *testing.T) {
	absPath, err := filepath.Abs("../tests/logic-controller/test_switch_controller.jmx")
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

	// Expect SwitchStart, Req1, Req2, Req3, SwitchEnd
	if len(tg.Samplers) != 5 {
		t.Fatalf("Expected 5 samplers, got %d", len(tg.Samplers))
	}

	if tg.Samplers[0].ControlType != "SwitchStart" {
		t.Errorf("Expected SwitchStart, got %s", tg.Samplers[0].ControlType)
	}
	if tg.Samplers[0].SwitchValueExpr != "Req2" {
		t.Errorf("Expected SwitchValueExpr=Req2, got %s", tg.Samplers[0].SwitchValueExpr)
	}

	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)
	session := engine.NewSession(0, plan, tg, eval)

	var executionURLs []string

	// Mocking execution loop manually
	step := 0
	for ; ; step++ {
		if step >= len(session.Tg.Samplers) {
			if len(session.CallStack) > 0 {
				frame := session.CallStack[len(session.CallStack)-1]
				session.CallStack = session.CallStack[:len(session.CallStack)-1]
				session.Tg = frame.Tg
				step = frame.Step
				continue
			}
			break
		}

		if jump, ok := session.InterleaveJump[step]; ok {
			delete(session.InterleaveJump, step)
			step = jump
		}
		sampler := session.Tg.Samplers[step]

		if sampler.IsControlFlow {
			switch sampler.ControlType {
			case "SwitchStart":
				if len(sampler.SwitchChildStarts) > 0 {
					switchVal := session.Evaluator.Evaluate(sampler.SwitchValueExpr)
					selectedIndex := 0 // default to first child
					if switchVal != "" {
						// name matching
						for i, name := range sampler.SwitchChildNames {
							if name == switchVal {
								selectedIndex = i
								break
							}
						}
					}
					startStep := sampler.SwitchChildStarts[selectedIndex]
					endStep := sampler.SwitchChildEnds[selectedIndex]
					session.InterleaveJump[endStep+1] = sampler.BlockEndIndex
					step = startStep - 1
				} else {
					step = sampler.BlockEndIndex
				}
			case "SwitchEnd":
				// pass
			}
			continue
		}

		// Actual Request
		url := session.Evaluator.Evaluate(sampler.Request.URL)
		executionURLs = append(executionURLs, url)
	}

	if len(executionURLs) != 1 {
		t.Fatalf("Expected exactly 1 execution (Req2), got %d", len(executionURLs))
	}

	if !strings.HasSuffix(executionURLs[0], "/req2") {
		t.Errorf("Unexpected execution: %s", executionURLs[0])
	}
	t.Logf("Executed properly: %v", executionURLs)
}
