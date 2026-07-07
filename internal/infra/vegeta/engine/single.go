package engine

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
)

// RunSingle executes a single, constant-rate Vegeta attack stage.
func RunSingle(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator, pacer vegeta.Pacer, dur time.Duration) error {
	isStateful := len(plan.CSVDataSets) > 0 || plan.CookieManager != nil || plan.CacheManager != nil || plan.DNSCacheManager != nil || plan.AuthManager != nil || len(plan.Counters) > 0 || len(plan.RandomVariables) > 0
	for _, tg := range plan.ThreadGroups {
		if len(tg.CSVDataSets) > 0 || tg.CookieManager != nil || tg.CacheManager != nil || tg.DNSCacheManager != nil || tg.AuthManager != nil || len(tg.Counters) > 0 || len(tg.RandomVariables) > 0 {
			isStateful = true
		}
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
			evalURL = strings.ReplaceAll(evalURL, "\n", "%0A")
			evalURL = strings.ReplaceAll(evalURL, "\r", "%0D")
			evalURL = strings.ReplaceAll(evalURL, " ", "%20")
			evalBody := eval.Evaluate(reqTemplate.BodyTemplate)
			
			tgt.Method = reqTemplate.Method
			tgt.URL = evalURL
			tgt.Body = nil // MUST RESET because tgt is reused by vegeta worker
			
			if len(reqTemplate.Headers) > 0 {
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
			if intervalErrors.Load() == 1 {
				log.Printf("[DEBUG] First error encountered: code=%d, err=%s, url=%s", res.Code, res.Error, res.URL)
			}
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
