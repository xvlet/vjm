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

		durationStr := fmt.Sprintf("%ds", stepDurSec)
		log.Printf("[VegetaRunner] --- Stepping: Running at %d TPS for %s ---", currentRate, durationStr)

		stepBinPath := fmt.Sprintf("%s.%d", baseBinPath, stepIndex)
		binPaths = append(binPaths, stepBinPath)
		config.ResultBinPath = stepBinPath

		dur, _ := time.ParseDuration(durationStr)
		pacer := vegeta.ConstantPacer{Freq: currentRate, Per: time.Second}
		err := engine.RunSingle(ctx, plan, config, eval, pacer, dur)
		if err != nil {
			return err
		}
		stepIndex++
	}

	if holdDurSec > 0 {
		durationStr := fmt.Sprintf("%ds", holdDurSec)
		log.Printf("[VegetaRunner] --- Stepping: Holding Max Rate %d TPS for %s ---", maxRate, durationStr)

		stepBinPath := fmt.Sprintf("%s.%d", baseBinPath, stepIndex)
		binPaths = append(binPaths, stepBinPath)
		config.ResultBinPath = stepBinPath

		dur, _ := time.ParseDuration(durationStr)
		pacer := vegeta.ConstantPacer{Freq: maxRate, Per: time.Second}
		err := engine.RunSingle(ctx, plan, config, eval, pacer, dur)
		if err != nil {
			return err
		}
	}

	config.ResultBinPath = strings.Join(binPaths, ",")
	return nil
}
