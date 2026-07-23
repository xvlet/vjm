package threadgroup

import (
	"context"
	"log"
	"strconv"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

type SteppingRunner struct{}

func (r *SteppingRunner) Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error {
	var stepCfg *domain.SteppingConfig
	for _, tg := range plan.ThreadGroups {
		if tg.SteppingConfig != nil {
			stepCfg = tg.SteppingConfig
			break
		}
	}

	maxRate, _ := strconv.Atoi(eval.Evaluate(stepCfg.MaxRate))
	stepRate, _ := strconv.Atoi(eval.Evaluate(stepCfg.StepRate))

	initDelaySec, _ := strconv.Atoi(eval.Evaluate(stepCfg.InitialDelay))
	stepDurSec, _ := strconv.Atoi(eval.Evaluate(stepCfg.StepDuration))
	holdDurSec, _ := strconv.Atoi(eval.Evaluate(stepCfg.HoldDuration))

	log.Printf("[VegetaRunner] Found SteppingThreadGroup config. MaxRate: %d, StepRate: %d", maxRate, stepRate)

	if stepRate <= 0 {
		stepRate = maxRate
	}

	steps := maxRate / stepRate
	if maxRate%stepRate != 0 {
		steps++
	}

	totalRampSec := steps * stepDurSec
	totalDurSec := initDelaySec + totalRampSec + holdDurSec
	totalDur := time.Duration(totalDurSec) * time.Second

	stepConfig := *config
	stepConfig.Workers = maxRate

	stepConfig.WorkerPacer = func(workerID uint64, elapsed time.Duration) (time.Duration, bool) {
		if elapsed >= totalDur {
			return 0, true
		}

		stepIdx := int(workerID) / stepRate
		if stepIdx >= steps {
			stepIdx = steps - 1
		}

		startTime := time.Duration(initDelaySec+stepIdx*stepDurSec) * time.Second
		if elapsed < startTime {
			return startTime - elapsed, false
		}
		return 0, false
	}

	pacer := vegeta.ConstantPacer{Freq: 0, Per: time.Second}
	log.Printf("[VegetaRunner] Stepping: Running dynamic Closed Model with Max %d Users for %s", maxRate, totalDur)

	return engine.RunSingle(ctx, plan, &stepConfig, eval, pacer, totalDur)
}
