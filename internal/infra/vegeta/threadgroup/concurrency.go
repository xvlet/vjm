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

	stepDurSec := rampUpSec / steps
	stepRate := targetLevel / steps

	var binPaths []string
	stepIndex := 1
	baseBinPath := config.ResultBinPath

	currentRate := 0
	for currentRate < targetLevel {
		currentRate += stepRate
		if currentRate > targetLevel {
			currentRate = targetLevel
		}

		durationStr := fmt.Sprintf("%ds", stepDurSec)
		log.Printf("[VegetaRunner] --- Concurrency: Running step %d with %d Concurrent Users for %s ---", stepIndex, currentRate, durationStr)

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

	if holdSec > 0 {
		durationStr := fmt.Sprintf("%ds", holdSec)
		log.Printf("[VegetaRunner] --- Concurrency: Holding Target %d Concurrent Users for %s ---", targetLevel, durationStr)

		stepBinPath := fmt.Sprintf("%s.%d", baseBinPath, stepIndex)
		binPaths = append(binPaths, stepBinPath)

		stepConfig := *config
		stepConfig.ResultBinPath = stepBinPath
		stepConfig.Workers = targetLevel

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
