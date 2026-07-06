package threadgroup

import (
	"context"
	"fmt"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

type StandardRunner struct{}

func (r *StandardRunner) Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error {
	dur, err := time.ParseDuration(config.Duration)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	pacer := vegeta.ConstantPacer{Freq: config.Rate, Per: time.Second}

	return engine.RunSingle(ctx, plan, config, eval, pacer, dur)
}
