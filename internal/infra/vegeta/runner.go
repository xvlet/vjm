package vegeta

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
)

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error {
	if plan == nil || len(plan.ThreadGroups) == 0 {
		return fmt.Errorf("test plan is empty or has no thread groups")
	}

	totalSamplers := 0
	var steppingCfg *domain.SteppingConfig
	for _, tg := range plan.ThreadGroups {
		totalSamplers += len(tg.Samplers)
		if tg.SteppingConfig != nil && steppingCfg == nil {
			steppingCfg = tg.SteppingConfig
		}
	}
	if totalSamplers == 0 {
		return fmt.Errorf("no HTTP requests found in thread groups")
	}

	// Remove result file if exists to start fresh
	_ = os.Remove(config.ResultBinPath)

	if steppingCfg != nil && !config.ForceCLI {
		return r.runStepping(ctx, plan, config, eval, steppingCfg)
	}

	if steppingCfg != nil && config.ForceCLI {
		log.Printf("[VegetaRunner] -force-cli flag enabled. Ignoring Thread Group config and using Rate=%d, Duration=%s", config.Rate, config.Duration)
	}

	return r.runSingle(ctx, plan, config, eval, config.Rate, config.Duration)
}

func (r *Runner) runStepping(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator, stepCfg *domain.SteppingConfig) error {
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

		err := r.runSingle(ctx, plan, config, eval, currentRate, durationStr)
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

		err := r.runSingle(ctx, plan, config, eval, maxRate, durationStr)
		if err != nil {
			return err
		}
	}

	config.ResultBinPath = strings.Join(binPaths, ",")
	return nil
}

func (r *Runner) runSingle(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator, rate int, durationStr string) error {
	dur, err := time.ParseDuration(durationStr)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	var pacer vegeta.Pacer
	pacer = vegeta.ConstantPacer{Freq: rate, Per: time.Second}

	// Check for OpenModelThreadGroup schedule
	for _, tg := range plan.ThreadGroups {
		if tg.OpenModelSchedule != "" {
			op, err := ParseOpenModelSchedule(tg.OpenModelSchedule)
			if err != nil {
				return fmt.Errorf("failed to parse open model schedule: %w", err)
			}
			pacer = op
			dur = op.totalDur
			log.Printf("[VegetaRunner] Using OpenModelPacer with duration %s", dur)
			break
		}
	}

	// Check if this plan requires stateful execution (e.g. Extractors)
	isStateful := false
	for _, tg := range plan.ThreadGroups {
		for _, s := range tg.Samplers {
			if len(s.Extractors) > 0 {
				isStateful = true
				break
			}
		}
	}

	var resultsChan <-chan *vegeta.Result

	if isStateful {
		workers := uint64(10)
		if config.Workers > 0 {
			workers = uint64(config.Workers)
		}
		statefulAttacker := NewStatefulAttacker(workers, pacer, dur)
		resultsChan = statefulAttacker.Attack(ctx, plan, eval)
	} else {
		var atkOpts []func(*vegeta.Attacker)
		if config.Workers > 0 {
			// CLI vegeta uses -max-workers which sets max workers but leaves initial workers at 10.
			atkOpts = append(atkOpts, vegeta.Workers(10))
			atkOpts = append(atkOpts, vegeta.MaxWorkers(uint64(config.Workers)))
		}
		atkOpts = append(atkOpts, vegeta.KeepAlive(true))
		atkOpts = append(atkOpts, vegeta.Connections(10000))
		atkOpts = append(atkOpts, vegeta.MaxConnections(10000))
		atkOpts = append(atkOpts, vegeta.Timeout(30*time.Second))

		standardAttacker := vegeta.NewAttacker(atkOpts...)
		
		// Setup targeter
		var tgCounter uint64
		// Pre-calculate cumulative weights for faster selection
		type samplerDist struct {
			sampler    *domain.Sampler
			cumulative float64
		}
		tgDistributions := make([][]samplerDist, len(plan.ThreadGroups))
		tgTotalWeights := make([]float64, len(plan.ThreadGroups))

		for i, tg := range plan.ThreadGroups {
			var current float64
			dist := make([]samplerDist, 0, len(tg.Samplers))
			for _, s := range tg.Samplers {
				current += s.Weight
				dist = append(dist, samplerDist{
					sampler:    s,
					cumulative: current,
				})
			}
			tgDistributions[i] = dist
			tgTotalWeights[i] = current
		}

		// Use global math/rand for thread-safe concurrent access without manual mutex
		// Go 1.20+ automatically seeds the global random generator.
		targeter := func(tgt *vegeta.Target) error {
			if tgt == nil {
				return vegeta.ErrNilTarget
			}

			idx := atomic.AddUint64(&tgCounter, 1) - 1
			tgIdx := idx % uint64(len(plan.ThreadGroups))
			
			tg := plan.ThreadGroups[tgIdx]
			dist := tgDistributions[tgIdx]
			
			var sampler *domain.Sampler
			if len(dist) == 0 {
				return fmt.Errorf("no samplers in thread group %s", tg.Name)
			} else {
				r := rand.Float64() * tgTotalWeights[tgIdx]
				for _, d := range dist {
					if r <= d.cumulative {
						sampler = d.sampler
						break
					}
				}
				if sampler == nil {
					sampler = tg.Samplers[len(tg.Samplers)-1]
				}
			}

			if sampler == nil || sampler.Request == nil {
				return fmt.Errorf("no sampler found")
			}

			reqTemplate := sampler.Request
			evalURL := eval.Evaluate(reqTemplate.URL)
			evalBody := eval.Evaluate(reqTemplate.BodyTemplate)
			
			tgt.Method = reqTemplate.Method
			tgt.URL = evalURL
			tgt.Body = nil // MUST RESET because tgt is reused by vegeta worker
			
			if len(reqTemplate.Headers) > 0 {
				// Bypass http.Header.Set to prevent canonicalization (e.g. changing interface-id to Interface-Id)
				// This matches the old behavior of json.Unmarshal into map[string][]string
				tgt.Header = make(http.Header)
				for k, v := range reqTemplate.Headers {
					tgt.Header[k] = []string{eval.Evaluate(v)}
				}
			} else {
				tgt.Header = nil
			}

			if evalBody != "" {
				tgt.Body = []byte(evalBody)
			}

			return nil
		}
		
		resultsChan = standardAttacker.Attack(targeter, pacer, dur, "vjm-attack")
	}

	outFile, err := os.OpenFile(config.ResultBinPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open result file: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	bufferedOut := bufio.NewWriterSize(outFile, 1024*1024) // 1MB buffer to prevent disk I/O bottleneck
	defer func() { _ = bufferedOut.Flush() }()

	enc := vegeta.NewEncoder(bufferedOut)

	var (
		intervalReqs    atomic.Int64
		intervalLatency atomic.Int64
		intervalErrors  atomic.Int64
		totalReqs       atomic.Int64
	)
	var mu sync.Mutex
	latencies := make([]float64, 0, 10000)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	attackStart := time.Now()

	for res := range resultsChan {
		_ = enc.Encode(res)

		intervalReqs.Add(1)
		totalReqs.Add(1)
		intervalLatency.Add(int64(res.Latency))

		latMs := float64(res.Latency) / 1e6
		mu.Lock()
		latencies = append(latencies, latMs)
		mu.Unlock()

		if res.Error != "" || res.Code >= 400 || res.Code == 0 {
			intervalErrors.Add(1)
		}

		select {
		case <-ticker.C:
			iReqs := intervalReqs.Swap(0)
			iLat := intervalLatency.Swap(0)
			iErrs := intervalErrors.Swap(0)
			tReqs := totalReqs.Load()

			var currentLatencies []float64
			mu.Lock()
			currentLatencies = latencies
			latencies = make([]float64, 0, 10000)
			mu.Unlock()

			p99 := 0.0
			maxLat := 0.0
			if len(currentLatencies) > 0 {
				sort.Float64s(currentLatencies)
				maxLat = currentLatencies[len(currentLatencies)-1]
				p99Idx := int(float64(len(currentLatencies)) * 0.99)
				if p99Idx >= len(currentLatencies) {
					p99Idx = len(currentLatencies) - 1
				}
				p99 = currentLatencies[p99Idx]
			}

			tps := float64(iReqs) / 5.0
			avgLatMs := 0.0
			errPct := 0.0
			if iReqs > 0 {
				avgLatMs = (float64(iLat) / float64(iReqs)) / 1e6
				errPct = (float64(iErrs) / float64(iReqs)) * 100.0
			}

			elapsed := time.Since(attackStart).Round(time.Second)
			log.Printf("[Dashboard] %02d:%02d | TPS: %5.1f | Avg: %5.1fms | P99: %5.1fms | Max: %5.1fms | Err: %3.1f%% | TotReq: %d",
				int(elapsed.Minutes()), int(elapsed.Seconds())%60, tps, avgLatMs, p99, maxLat, errPct, tReqs)
		default:
		}
	}

	elapsed := time.Since(attackStart).Round(time.Millisecond)
	log.Printf("[VegetaRunner] Step completed in %s.", elapsed)
	return nil
}
