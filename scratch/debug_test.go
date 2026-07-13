package scratch

import (
	"fmt"
	"testing"

	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestDebugForEach(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/logic-controller/test_foreach_controller.jmx")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	globalEval := evaluator.NewDefaultEvaluator(nil)
	if plan.UserDefinedVariables != nil {
		t.Logf("UserDefinedVariables found: %d", len(plan.UserDefinedVariables))
		for k, v := range plan.UserDefinedVariables {
			t.Logf("  %s = %q", k, v)
		}
		globalEval.AddVariables(plan.UserDefinedVariables)
	} else {
		t.Log("UserDefinedVariables is nil!")
	}

	tg := plan.ThreadGroups[0]
	t.Logf("Thread Group Samplers count: %d", len(tg.Samplers))

	sessionEval := globalEval.Clone()

	for step := 0; step < len(tg.Samplers); step++ {
		sampler := tg.Samplers[step]

		t.Logf("\n--- Step %d: %s ---", step, sampler.ControlType)
		if sampler.IsControlFlow {
			if sampler.ControlType == "ForEachStart" {
				t.Logf("ForEachStart config: InputVal=%q, ReturnVal=%q, UseSep=%v, StartIndex=%q",
					sampler.ForEachInputVal, sampler.ForEachReturnVal, sampler.ForEachUseSeparator, sampler.ForEachStartIndex)

				startIdx := 0
				if sampler.ForEachStartIndex != "" {
					t.Logf("Evaluating StartIndex: %q", sampler.ForEachStartIndex)
				}
				idx := startIdx + 1

				sep := ""
				if sampler.ForEachUseSeparator {
					sep = "_"
				}

				inputVarName := fmt.Sprintf("%s%s%d", sessionEval.Evaluate(sampler.ForEachInputVal), sep, idx)
				t.Logf("Evaluated inputVarName: %q", inputVarName)

				valStr := sessionEval.Evaluate("${" + inputVarName + "}")
				t.Logf("Evaluated valStr for ${%s}: %q", inputVarName, valStr)

				if valStr == "${"+inputVarName+"}" {
					t.Logf("Variable does not exist, exiting loop (LoopJumpIndex = %d)", sampler.LoopJumpIndex)
					step = sampler.LoopJumpIndex
					continue
				}

				returnVar := sessionEval.Evaluate(sampler.ForEachReturnVal)
				t.Logf("Evaluated returnVar: %q", returnVar)

				if returnVar != "" {
					sessionEval.SetVariable(returnVar, valStr)
					t.Logf("SetVariable called: %s = %q", returnVar, valStr)
				}
			}
			continue
		}

		if sampler.Request != nil {
			t.Logf("Request Name: %s", sampler.Name)
			url := sessionEval.Evaluate(sampler.Request.URL)
			t.Logf("Evaluated URL: %s", url)
		}
	}
}
