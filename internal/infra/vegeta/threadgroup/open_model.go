package threadgroup

import (
	"context"
	"fmt"
	"log"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

type OpenModelRunner struct{}

func (r *OpenModelRunner) Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error {
	scheduleStr := ""
	for _, tg := range plan.ThreadGroups {
		if tg.OpenModelSchedule != "" {
			scheduleStr = tg.OpenModelSchedule
			break
		}
	}

	pacer, err := engine.ParseOpenModelSchedule(scheduleStr)
	if err != nil {
		return fmt.Errorf("failed to parse open model schedule: %w", err)
	}

	log.Printf("[VegetaRunner] Using OpenModelPacer with duration %s", pacer.TotalDur)

	return engine.RunSingle(ctx, plan, config, eval, pacer, pacer.TotalDur)
}
