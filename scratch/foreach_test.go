package scratch

import (
	"fmt"
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestForEach(t *testing.T) {
	globalEval := evaluator.NewDefaultEvaluator(nil)

	// Add user defined variable
	udv := map[string]string{
		"myArr_1": "apple",
		"myArr_2": "banana",
	}
	globalEval.AddVariables(udv)

	// Create thread group with ForEach controller
	tg := &domain.ThreadGroup{
		Samplers: []*domain.Sampler{
			{
				IsControlFlow:       true,
				ControlType:         "ForEachStart",
				LoopId:              1,
				ForEachInputVal:     "myArr",
				ForEachReturnVal:    "myOut",
				ForEachUseSeparator: true,
				LoopJumpIndex:       2, // jump to end
			},
			{
				Request: &domain.RequestTemplate{
					URL: "http://test?item=${myOut}",
				},
			},
			{
				IsControlFlow: true,
				ControlType:   "ForEachEnd",
				LoopId:        1,
				LoopJumpIndex: 0, // jump to start
			},
		},
	}

	session := engine.NewSession(0, nil, tg, globalEval)

	// manually run step 0 (ForEachStart)
	sampler := tg.Samplers[0]

	// let's manually execute ForEachStart logic
	startIdx := 1
	session.LoopCounters[sampler.LoopId] = startIdx
	idx := session.LoopCounters[sampler.LoopId]
	inputVarName := fmt.Sprintf("%s_%d", session.Evaluator.Evaluate(sampler.ForEachInputVal), idx)
	valStr := session.Evaluator.Evaluate("${" + inputVarName + "}")
	returnVar := session.Evaluator.Evaluate(sampler.ForEachReturnVal)
	session.Evaluator.SetVariable(returnVar, valStr)

	fmt.Printf("inputVarName: %s\n", inputVarName)
	fmt.Printf("valStr: %s\n", valStr)
	fmt.Printf("returnVar: %s\n", returnVar)

	// manually run step 1 (URL evaluation)
	reqSampler := tg.Samplers[1]
	url := session.Evaluator.Evaluate(reqSampler.Request.URL)
	fmt.Printf("Evaluated URL: %s\n", url)

	if url != "http://test?item=apple" {
		t.Errorf("Expected url to be http://test?item=apple, got %s", url)
	}
}
