package vegeta

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/vegeta/threadgroup"
)

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error {
	if plan == nil || len(plan.ThreadGroups) == 0 {
		return fmt.Errorf("test plan is empty or has no thread groups")
	}

	totalSamplers := 0
	for _, tg := range plan.ThreadGroups {
		totalSamplers += len(tg.Samplers)
	}
	if totalSamplers == 0 {
		return fmt.Errorf("no HTTP requests found in thread groups")
	}

	// Remove result file if exists to start fresh
	_ = os.Remove(config.ResultBinPath)

	tg := plan.ThreadGroups[0]
	tgRunner := threadgroup.GetRunner(tg)

	if config.ForceCLI {
		log.Printf("[VegetaRunner] -force-cli flag enabled. Ignoring Thread Group config and using Rate=%d, Duration=%s", config.Rate, config.Duration)
		tgRunner = &threadgroup.StandardRunner{}
	}

	return tgRunner.Run(ctx, plan, config, eval)
}
