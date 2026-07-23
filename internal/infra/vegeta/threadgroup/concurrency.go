package threadgroup

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

type ConcurrencyRunner struct{}

func (r *ConcurrencyRunner) Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error {
	var concCfg *domain.ConcurrencyConfig
	for _, tg := range plan.ThreadGroups {
		if tg.ConcurrencyConfig != nil {
			concCfg = tg.ConcurrencyConfig
			break
		}
	}

	targetLevel, _ := strconv.Atoi(eval.Evaluate(concCfg.TargetLevel))
	rampUp, _ := strconv.Atoi(eval.Evaluate(concCfg.RampUp))
	steps, _ := strconv.Atoi(eval.Evaluate(concCfg.Steps))
	hold, _ := strconv.Atoi(eval.Evaluate(concCfg.Hold))
	unit := eval.Evaluate(concCfg.Unit)

	mult := 1
	switch unit {
	case "M":
		mult = 60
	case "H":
		mult = 3600
	}

	rampUpSec := rampUp * mult
	holdSec := hold * mult

	log.Printf("[VegetaRunner] Found ConcurrencyThreadGroup config. TargetRate: %d, RampUp: %ds, Steps: %d, Hold: %ds", targetLevel, rampUpSec, steps, holdSec)

	if steps <= 0 {
		// Use open model pacer
		schedule := fmt.Sprintf("rate(0/s) random_arrivals(%ds) rate(%d/s) random_arrivals(%ds) rate(%d/s)", rampUpSec, targetLevel, holdSec, targetLevel)
		log.Printf("[VegetaRunner] Concurrency: Translating linear ramp to OpenModelSchedule: %s", schedule)
		plan.ThreadGroups[0].OpenModelSchedule = schedule

		pacer, err := engine.ParseOpenModelSchedule(schedule)
		if err != nil {
			return fmt.Errorf("failed to parse open model schedule: %w", err)
		}
		return engine.RunSingle(ctx, plan, config, eval, pacer, pacer.TotalDur)
	}

	if steps > targetLevel && targetLevel > 0 {
		steps = targetLevel // Cap steps to prevent fractional users per step
	}
	if steps <= 0 {
		steps = 1
	}
	stepDurSec := rampUpSec / steps
	stepRate := targetLevel / steps
	if stepRate <= 0 {
		stepRate = 1
	}

	totalDurSec := rampUpSec + holdSec
	totalDur := time.Duration(totalDurSec) * time.Second

	stepConfig := *config
	stepConfig.Workers = targetLevel

	stepConfig.WorkerPacer = func(workerID uint64, elapsed time.Duration) (time.Duration, bool) {
		if elapsed >= totalDur {
			return 0, true
		}
		stepIdx := int(workerID) / stepRate
		if stepIdx >= steps {
			stepIdx = steps - 1
		}
		startTime := time.Duration(stepIdx*stepDurSec) * time.Second

		if elapsed < startTime {
			return startTime - elapsed, false
		}
		return 0, false
	}

	pacer := vegeta.ConstantPacer{Freq: 0, Per: time.Second}
	log.Printf("[VegetaRunner] Concurrency: Running dynamic Closed Model with Max %d Users for %s", targetLevel, totalDur)

	return engine.RunSingle(ctx, plan, &stepConfig, eval, pacer, totalDur)
}
