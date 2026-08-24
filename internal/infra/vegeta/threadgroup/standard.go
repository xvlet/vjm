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
	var dur time.Duration
	var err error
	if config.Duration != "" {
		dur, err = time.ParseDuration(config.Duration)
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
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

		if config.Rate == 0 {
			if tt != nil {
				val := eval.Evaluate(tt.Throughput)
				throughputPerMin, _ := strconv.ParseFloat(val, 64)
				if throughputPerMin > 0 {
					freq := throughputPerMin / 60.0
					if freq <= 0 {
						freq = 1.0 // Minimum 1 RPS if defined
					}

					if tt.Type == "PreciseThroughputTimer" {
						pacer = PoissonPacer{Freq: freq, Per: time.Second}
					} else {
						pacer = vegeta.ConstantPacer{Freq: int(freq), Per: time.Second}
					}
				}
			} else {
				pacer = vegeta.ConstantPacer{Freq: 0, Per: time.Second}
			}
		}

		if len(plan.ThreadGroups) > 0 {
			tg := plan.ThreadGroups[0]
			if tg.NumThreads > 0 && config.Workers == 0 {
				config.Workers = tg.NumThreads
			}

			if tg.Scheduler {
				if tg.Delay > 0 {
					select {
					case <-time.After(time.Duration(tg.Delay) * time.Second):
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				if tg.Duration > 0 && config.Duration == "" {
					dur = time.Duration(tg.Duration) * time.Second
				}
			} else if !tg.ContinueForever && tg.Loops > 0 && config.Duration == "" {
				dur = 0 // Run until thread iteration limits are reached
			}
		}
	}

	return engine.RunSingle(ctx, plan, config, eval, pacer, dur)
}
