package threadgroup

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

type FreeFormArrivalsRunner struct{}

func (r *FreeFormArrivalsRunner) Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error {
	var ffCfg *domain.FreeFormArrivalsConfig
	for _, tg := range plan.ThreadGroups {
		if tg.FreeFormArrivalsConfig != nil {
			ffCfg = tg.FreeFormArrivalsConfig
			break
		}
	}

	unit := eval.Evaluate(ffCfg.Unit)
	concurrencyLimit, _ := strconv.Atoi(eval.Evaluate(ffCfg.ConcurrencyLimit))

	mult := 1.0
	switch unit {
	case "M":
		mult = 60.0
	case "H":
		mult = 3600.0
	}

	if concurrencyLimit > 0 {
		config.Workers = concurrencyLimit
	}

	log.Printf("[VegetaRunner] Found FreeFormArrivalsThreadGroup config. Schedule Rows: %d, ConcurrencyLimit: %d", len(ffCfg.Schedule), concurrencyLimit)

	var scheduleParts []string
	prevEndTPS := -1.0

	for i, item := range ffCfg.Schedule {
		start, _ := strconv.ParseFloat(eval.Evaluate(item.Start), 64)
		end, _ := strconv.ParseFloat(eval.Evaluate(item.End), 64)
		dur, _ := strconv.ParseFloat(eval.Evaluate(item.Duration), 64)

		durSec := int(dur * mult)

		startTPS := start
		endTPS := end
		switch unit {
		case "M":
			startTPS = start / 60.0
			endTPS = end / 60.0
		case "H":
			startTPS = start / 3600.0
			endTPS = end / 3600.0
		}

		if i == 0 {
			scheduleParts = append(scheduleParts, fmt.Sprintf("rate(%.2f/s)", startTPS))
		} else {
			if startTPS != prevEndTPS {
				scheduleParts = append(scheduleParts, fmt.Sprintf("random_arrivals(0s) rate(%.2f/s)", startTPS))
			}
		}

		scheduleParts = append(scheduleParts, fmt.Sprintf("random_arrivals(%ds) rate(%.2f/s)", durSec, endTPS))
		prevEndTPS = endTPS
	}

	schedule := strings.Join(scheduleParts, " ")
	log.Printf("[VegetaRunner] Free-Form Arrivals: Translating to OpenModelSchedule: %s", schedule)

	pacer, err := engine.ParseOpenModelSchedule(schedule)
	if err != nil {
		return fmt.Errorf("failed to parse open model schedule: %w", err)
	}

	return engine.RunSingle(ctx, plan, config, eval, pacer, pacer.TotalDur)
}
