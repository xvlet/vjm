package scratch

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestModuleController(t *testing.T) {
	absPath, err := filepath.Abs("../tests/logic-controller/test_module_controller.jmx")
	if err != nil {
		t.Fatalf("Failed to get absolute path: %v", err)
	}

	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse(absPath)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(plan.ThreadGroups) < 2 {
		t.Fatalf("Expected at least 2 thread groups (1 main, 1 fragment), got %d", len(plan.ThreadGroups))
	}

	tg := plan.ThreadGroups[0]

	// Expect ModuleCall
	if len(tg.Samplers) != 1 {
		t.Fatalf("Expected 1 sampler (ModuleCall), got %d", len(tg.Samplers))
	}

	if tg.Samplers[0].ControlType != "ModuleCall" {
		t.Errorf("Expected ModuleCall, got %s", tg.Samplers[0].ControlType)
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
			case "ModuleCall":
				if len(sampler.ModuleTargetNodePath) > 0 {
					targetName := sampler.ModuleTargetNodePath[len(sampler.ModuleTargetNodePath)-1]
					for _, ptg := range session.Plan.ThreadGroups {
						if ptg.Name == targetName {
							session.CallStack = append(session.CallStack, engine.CallFrame{Tg: session.Tg, Step: step})
							session.Tg = ptg
							step = -1
							break
						}
					}
				}
			}
			continue
		}

		// Actual Request
		url := session.Evaluator.Evaluate(sampler.Request.URL)
		executionURLs = append(executionURLs, url)
	}

	if len(executionURLs) != 2 {
		t.Fatalf("Expected exactly 2 executions (Req1, Req2 from Target Fragment), got %d", len(executionURLs))
	}

	if !strings.HasSuffix(executionURLs[0], "/req1") || !strings.HasSuffix(executionURLs[1], "/req2") {
		t.Errorf("Unexpected execution: %v", executionURLs)
	}
	t.Logf("Executed properly: %v", executionURLs)
}
