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

	pacer := &UltimatePacer{}
	maxDur := time.Duration(0)

	for _, rec := range ultCfg.Records {
		targetRate, _ := strconv.ParseFloat(eval.Evaluate(rec.StartThreads), 64)
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
		pacer.rows = append(pacer.rows, row)

		rowEnd := row.InitialDelay + row.StartupTime + row.HoldTime + row.ShutdownTime
		if rowEnd > maxDur {
			maxDur = rowEnd
		}
	}
	pacer.totalDur = maxDur

	log.Printf("[VegetaRunner] Found UltimateThreadGroup config. Total Duration: %s, Records: %d", maxDur, len(pacer.rows))

	return engine.RunSingle(ctx, plan, config, eval, pacer, maxDur)
}

type UltimateRow struct {
	TargetRate   float64
	InitialDelay time.Duration
	StartupTime  time.Duration
	HoldTime     time.Duration
	ShutdownTime time.Duration
}

func (r *UltimateRow) hitsAt(t time.Duration) float64 {
	if t <= r.InitialDelay {
		return 0
	}
	t -= r.InitialDelay
	hits := 0.0

	// 1. Startup phase
	if t <= r.StartupTime {
		if r.StartupTime > 0 {
			ratio := float64(t) / float64(r.StartupTime)
			rate := ratio * r.TargetRate
			hits += 0.5 * rate * t.Seconds()
		} else {
			hits += r.TargetRate * t.Seconds()
		}
		return hits
	}
	if r.StartupTime > 0 {
		hits += 0.5 * r.TargetRate * r.StartupTime.Seconds()
	}
	t -= r.StartupTime

	// 2. Hold phase
	if t <= r.HoldTime {
		hits += r.TargetRate * t.Seconds()
		return hits
	}
	hits += r.TargetRate * r.HoldTime.Seconds()
	t -= r.HoldTime

	// 3. Shutdown phase
	if t <= r.ShutdownTime {
		if r.ShutdownTime > 0 {
			ratio := float64(t) / float64(r.ShutdownTime)
			rate := (1 - ratio) * r.TargetRate
			hits += 0.5 * (r.TargetRate + rate) * t.Seconds()
		}
		return hits
	}
	if r.ShutdownTime > 0 {
		hits += 0.5 * r.TargetRate * r.ShutdownTime.Seconds()
	}

	return hits
}

func (r *UltimateRow) rateAt(t time.Duration) float64 {
	if t < r.InitialDelay {
		return 0
	}
	t -= r.InitialDelay

	if t < r.StartupTime {
		if r.StartupTime > 0 {
			return r.TargetRate * (float64(t) / float64(r.StartupTime))
		}
		return r.TargetRate
	}
	t -= r.StartupTime

	if t < r.HoldTime {
		return r.TargetRate
	}
	t -= r.HoldTime

	if t < r.ShutdownTime {
		if r.ShutdownTime > 0 {
			return r.TargetRate * (1 - (float64(t) / float64(r.ShutdownTime)))
		}
	}
	return 0
}

type UltimatePacer struct {
	rows     []UltimateRow
	totalDur time.Duration
}

func (p *UltimatePacer) hitsAt(t time.Duration) float64 {
	hits := 0.0
	for _, r := range p.rows {
		hits += r.hitsAt(t)
	}
	return hits
}

func (p *UltimatePacer) Rate(t time.Duration) float64 {
	rate := 0.0
	for _, r := range p.rows {
		rate += r.rateAt(t)
	}
	return rate
}

func (p *UltimatePacer) Pace(elapsed time.Duration, hits uint64) (time.Duration, bool) {
	if elapsed >= p.totalDur {
		return 0, true
	}

	expectedHits := p.hitsAt(elapsed)
	if float64(hits) < expectedHits {
		return 0, false
	}

	currentRate := p.Rate(elapsed)
	if currentRate <= 0.001 {
		return 100 * time.Millisecond, false
	}

	return time.Duration(float64(time.Second) / currentRate), false
}

var _ vegeta.Pacer = &UltimatePacer{}
