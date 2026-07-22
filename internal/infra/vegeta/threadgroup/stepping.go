package threadgroup

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
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

	if initDelaySec > 0 {
		log.Printf("[VegetaRunner] Initial delay for %ds", initDelaySec)
		select {
		case <-time.After(time.Duration(initDelaySec) * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	currentRate := 0
	if stepRate <= 0 {
		stepRate = maxRate
	}

	var binPaths []string
	stepIndex := 1
	baseBinPath := config.ResultBinPath

	for currentRate < maxRate {
		currentRate += stepRate
		if currentRate > maxRate {
			currentRate = maxRate
		}

		if stepDurSec > 0 {
			durationStr := fmt.Sprintf("%ds", stepDurSec)
			log.Printf("[VegetaRunner] --- Stepping: Running step %d with %d Concurrent Users for %s ---", stepIndex, currentRate, durationStr)

			stepBinPath := fmt.Sprintf("%s.%d", baseBinPath, stepIndex)
			binPaths = append(binPaths, stepBinPath)

			stepConfig := *config
			stepConfig.ResultBinPath = stepBinPath
			stepConfig.Workers = currentRate

			dur, _ := time.ParseDuration(durationStr)
			pacer := vegeta.ConstantPacer{Freq: 0, Per: time.Second}
			err := engine.RunSingle(ctx, plan, &stepConfig, eval, pacer, dur)
			if err != nil {
				return err
			}
			stepIndex++
		}
	}

	if holdDurSec > 0 {
		durationStr := fmt.Sprintf("%ds", holdDurSec)
		log.Printf("[VegetaRunner] --- Stepping: Holding Max %d Concurrent Users for %s ---", maxRate, durationStr)

		stepBinPath := fmt.Sprintf("%s.%d", baseBinPath, stepIndex)
		binPaths = append(binPaths, stepBinPath)

		stepConfig := *config
		stepConfig.ResultBinPath = stepBinPath
		stepConfig.Workers = maxRate

		dur, _ := time.ParseDuration(durationStr)
		pacer := vegeta.ConstantPacer{Freq: 0, Per: time.Second}
		err := engine.RunSingle(ctx, plan, &stepConfig, eval, pacer, dur)
		if err != nil {
			return err
		}
	}

	config.ResultBinPath = strings.Join(binPaths, ",")
	return nil
}
