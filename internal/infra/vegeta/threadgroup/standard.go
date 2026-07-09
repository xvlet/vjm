package threadgroup

import (
	"context"
	"fmt"
	"strconv"
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

	var pacer vegeta.Pacer
	pacer = vegeta.ConstantPacer{Freq: config.Rate, Per: time.Second}

	if !config.ForceCLI {
		var tt *domain.ThroughputTimer
		if len(plan.ThroughputTimers) > 0 {
			tt = plan.ThroughputTimers[0]
		}
		if len(plan.ThreadGroups) > 0 && len(plan.ThreadGroups[0].ThroughputTimers) > 0 {
			tt = plan.ThreadGroups[0].ThroughputTimers[0] // ThreadGroup overrides Plan
		}

		if tt != nil {
			val := eval.Evaluate(tt.Throughput)
			throughputPerMin, _ := strconv.ParseFloat(val, 64)
			if throughputPerMin > 0 {
				freq := throughputPerMin / 60.0
				if freq <= 0 {
					freq = 1.0 // Minimum 1 RPS if defined
				}

				if tt.Type == "PreciseThroughputTimer" {
					// PoissonPacer models randomized arrivals with a mean rate
					pacer = PoissonPacer{Freq: freq, Per: time.Second}
				} else {
					pacer = vegeta.ConstantPacer{Freq: int(freq), Per: time.Second}
				}
			}
		}
	}

	return engine.RunSingle(ctx, plan, config, eval, pacer, dur)
}
