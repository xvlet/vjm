package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestVerifyPlan(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/logic-controller/test_foreach_controller.jmx")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	tg := plan.ThreadGroups[0]
	for step := 0; step < len(tg.Samplers); step++ {
		sampler := tg.Samplers[step]
		if sampler.IsControlFlow && sampler.ControlType == "ForEachStart" {
			t.Logf("ForEachStart: InputVal=%q, ReturnVal=%q, UseSep=%v, StartIndex=%q",
				sampler.ForEachInputVal, sampler.ForEachReturnVal, sampler.ForEachUseSeparator, sampler.ForEachStartIndex)
		}
	}
}
