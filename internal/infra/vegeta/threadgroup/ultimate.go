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

type UltimateRunner struct{}

func (r *UltimateRunner) Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error {
	var ultCfg *domain.UltimateConfig
	for _, tg := range plan.ThreadGroups {
		if tg.UltimateConfig != nil {
			ultCfg = tg.UltimateConfig
			break
		}
	}

	var rows []UltimateRow
	maxDur := time.Duration(0)
	totalWorkers := 0

	for _, rec := range ultCfg.Records {
		targetRateFloat, _ := strconv.ParseFloat(eval.Evaluate(rec.StartThreads), 64)
		targetRate := int(targetRateFloat)
		initDelaySec, _ := strconv.Atoi(eval.Evaluate(rec.InitialDelay))
		startUpSec, _ := strconv.Atoi(eval.Evaluate(rec.StartupTime))
		holdForSec, _ := strconv.Atoi(eval.Evaluate(rec.HoldLoadFor))
		shutDownSec, _ := strconv.Atoi(eval.Evaluate(rec.ShutdownTime))

		row := UltimateRow{
			TargetRate:   targetRate,
			InitialDelay: time.Duration(initDelaySec) * time.Second,
			StartupTime:  time.Duration(startUpSec) * time.Second,
			HoldTime:     time.Duration(holdForSec) * time.Second,
			ShutdownTime: time.Duration(shutDownSec) * time.Second,
		}
		rows = append(rows, row)
		totalWorkers += targetRate

		rowEnd := row.InitialDelay + row.StartupTime + row.HoldTime + row.ShutdownTime
		if rowEnd > maxDur {
			maxDur = rowEnd
		}
	}

	log.Printf("[VegetaRunner] Found UltimateThreadGroup config. Total Duration: %s, Records: %d, Max Users: %d", maxDur, len(rows), totalWorkers)

	stepConfig := *config
	stepConfig.Workers = totalWorkers

	stepConfig.WorkerPacer = func(workerID uint64, elapsed time.Duration) (time.Duration, bool) {
		if elapsed >= maxDur {
			return 0, true
		}

		var wRow *UltimateRow
		var wIdxInRow int
		accum := 0
		for i := range rows {
			if int(workerID) < accum+rows[i].TargetRate {
				wRow = &rows[i]
				wIdxInRow = int(workerID) - accum
				break
			}
			accum += rows[i].TargetRate
		}

		if wRow == nil {
			return 0, true
		}

		startTime := wRow.InitialDelay
		if wRow.TargetRate > 0 && wRow.StartupTime > 0 {
			startTime += time.Duration(wIdxInRow) * (wRow.StartupTime / time.Duration(wRow.TargetRate))
		}

		stopTime := wRow.InitialDelay + wRow.StartupTime + wRow.HoldTime
		if wRow.TargetRate > 0 && wRow.ShutdownTime > 0 {
			stopTime += time.Duration(wIdxInRow) * (wRow.ShutdownTime / time.Duration(wRow.TargetRate))
		}

		if elapsed >= stopTime {
			return 0, true
		}

		if elapsed < startTime {
			return startTime - elapsed, false
		}

		return 0, false
	}

	pacer := vegeta.ConstantPacer{Freq: 0, Per: time.Second}
	return engine.RunSingle(ctx, plan, &stepConfig, eval, pacer, maxDur)
}

type UltimateRow struct {
	TargetRate   int
	InitialDelay time.Duration
	StartupTime  time.Duration
	HoldTime     time.Duration
	ShutdownTime time.Duration
}
