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

type ArrivalsRunner struct{}

func (r *ArrivalsRunner) Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error {
	var arrCfg *domain.ArrivalsConfig
	for _, tg := range plan.ThreadGroups {
		if tg.ArrivalsConfig != nil {
			arrCfg = tg.ArrivalsConfig
			break
		}
	}

	targetLevel, _ := strconv.ParseFloat(eval.Evaluate(arrCfg.TargetLevel), 64)
	rampUp, _ := strconv.ParseFloat(eval.Evaluate(arrCfg.RampUp), 64)
	steps, _ := strconv.Atoi(eval.Evaluate(arrCfg.Steps))
	hold, _ := strconv.ParseFloat(eval.Evaluate(arrCfg.Hold), 64)
	unit := eval.Evaluate(arrCfg.Unit)
	concurrencyLimit, _ := strconv.Atoi(eval.Evaluate(arrCfg.ConcurrencyLimit))

	mult := 1.0
	switch unit {
	case "M":
		mult = 60.0
	case "H":
		mult = 3600.0
	}

	// Calculate target TPS
	targetRateTPS := targetLevel
	switch unit {
	case "M":
		targetRateTPS = targetLevel / 60.0
	case "H":
		targetRateTPS = targetLevel / 3600.0
	}

	if concurrencyLimit > 0 {
		config.Workers = concurrencyLimit
	}

	rampUpSec := int(rampUp * mult)
	holdSec := int(hold * mult)

	log.Printf("[VegetaRunner] Found ArrivalsThreadGroup config. TargetRate: %.2f TPS, RampUp: %ds, Steps: %d, Hold: %ds, ConcurrencyLimit: %d", targetRateTPS, rampUpSec, steps, holdSec, concurrencyLimit)

	if steps <= 0 {
		// Use open model pacer for linear ramp up
		schedule := fmt.Sprintf("rate(0/s) random_arrivals(%ds) rate(%.2f/s) random_arrivals(%ds) rate(%.2f/s)", rampUpSec, targetRateTPS, holdSec, targetRateTPS)
		log.Printf("[VegetaRunner] Arrivals: Translating linear ramp to OpenModelSchedule: %s", schedule)
		plan.ThreadGroups[0].OpenModelSchedule = schedule

		pacer, err := engine.ParseOpenModelSchedule(schedule)
		if err != nil {
			return fmt.Errorf("failed to parse open model schedule: %w", err)
		}
		return engine.RunSingle(ctx, plan, config, eval, pacer, pacer.TotalDur)
	}

	stepDurSec := rampUpSec / steps
	stepRate := targetRateTPS / float64(steps)

	var binPaths []string
	stepIndex := 1
	baseBinPath := config.ResultBinPath

	currentRate := 0.0
	for stepIndex <= steps {
		currentRate += stepRate
		if currentRate > targetRateTPS {
			currentRate = targetRateTPS
		}

		durationStr := fmt.Sprintf("%ds", stepDurSec)
		log.Printf("[VegetaRunner] --- Arrivals: Running step %d at %.2f TPS for %s ---", stepIndex, currentRate, durationStr)

		stepBinPath := fmt.Sprintf("%s.%d", baseBinPath, stepIndex)
		binPaths = append(binPaths, stepBinPath)
		config.ResultBinPath = stepBinPath

		dur, _ := time.ParseDuration(durationStr)
		pacer := vegeta.ConstantPacer{Freq: int(currentRate), Per: time.Second}
		if currentRate < 1.0 {
			// fallback for fractional rate if needed, or precise pacer
			pacer = vegeta.ConstantPacer{Freq: int(currentRate * 1000), Per: 1000 * time.Second}
		}

		err := engine.RunSingle(ctx, plan, config, eval, pacer, dur)
		if err != nil {
			return err
		}
		stepIndex++
	}

	if holdSec > 0 {
		durationStr := fmt.Sprintf("%ds", holdSec)
		log.Printf("[VegetaRunner] --- Arrivals: Holding Target Rate %.2f TPS for %s ---", targetRateTPS, durationStr)

		stepBinPath := fmt.Sprintf("%s.%d", baseBinPath, stepIndex)
		binPaths = append(binPaths, stepBinPath)
		config.ResultBinPath = stepBinPath

		dur, _ := time.ParseDuration(durationStr)
		pacer := vegeta.ConstantPacer{Freq: int(targetRateTPS), Per: time.Second}
		if targetRateTPS < 1.0 {
			pacer = vegeta.ConstantPacer{Freq: int(targetRateTPS * 1000), Per: 1000 * time.Second}
		}

		err := engine.RunSingle(ctx, plan, config, eval, pacer, dur)
		if err != nil {
			return err
		}
	}

	config.ResultBinPath = strings.Join(binPaths, ",")
	return nil
}
