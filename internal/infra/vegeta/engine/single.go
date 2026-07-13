package engine

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
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
		if len(tg.CSVDataSets) > 0 || tg.CookieManager != nil || tg.CacheManager != nil || tg.DNSCacheManager != nil || tg.AuthManager != nil || len(tg.Counters) > 0 || len(tg.RandomVariables) > 0 || len(tg.Timers) > 0 {
			isStateful = true
		}
		for _, s := range tg.Samplers {
			if len(s.Extractors) > 0 || s.IsControlFlow {
				isStateful = true
				break
			}
		}
	}

	var resultsChan <-chan *vegeta.Result

	if isStateful {
		workers := uint64(10000)
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

		var executableTgs []*domain.ThreadGroup
		for _, tg := range plan.ThreadGroups {
			if tg.ActionType != "fragment" {
				executableTgs = append(executableTgs, tg)
			}
		}
		if len(executableTgs) == 0 {
			return nil
		}

		// Setup targeter
		var tgCounter uint64
		// Pre-calculate cumulative weights for faster selection
		type samplerDist struct {
			sampler    *domain.Sampler
			cumulative float64
		}
		tgDistributions := make([][]samplerDist, len(executableTgs))
		tgTotalWeights := make([]float64, len(executableTgs))

		for i, tg := range executableTgs {
			var current float64
			dist := make([]samplerDist, 0, len(tg.Samplers))
			for _, s := range tg.Samplers {
				if s.IfCondition != "" {
					if !eval.EvaluateLogic(s.IfCondition) {
						continue
					}
				}
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
			tgIdx := idx % uint64(len(executableTgs))

			tg := executableTgs[tgIdx]
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
	// [PERF] Lock-free histogram: 2000 buckets × 0.5ms = 0~999.5ms 범위 커버
	// 매 요청 mutex lock + 5초마다 sort(23000개) 제거
	const latBucketCount = 2000
	var latBuckets [latBucketCount]atomic.Int64

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	attackStart := time.Now()

	for {
		select {
		case res, ok := <-resultsChan:
			if !ok {
				resultsChan = nil
				break
			}
			_ = enc.Encode(res)

			intervalReqs.Add(1)
			totalReqs.Add(1)
			intervalLatency.Add(int64(res.Latency))

			latMs := float64(res.Latency) / 1e6
			bucketIdx := int(latMs * 2)
			if bucketIdx >= latBucketCount {
				bucketIdx = latBucketCount - 1
			}
			if bucketIdx < 0 {
				bucketIdx = 0
			}
			latBuckets[bucketIdx].Add(1)

			if res.Error != "" || res.Code >= 400 || res.Code == 0 {
				intervalErrors.Add(1)
				if intervalErrors.Load() == 1 {
					log.Printf("[DEBUG] First error encountered: code=%d, err=%s, url=%s", res.Code, res.Error, res.URL)
				}
			}

		case <-ticker.C:
			iReqs := intervalReqs.Swap(0)
			iLat := intervalLatency.Swap(0)
			iErrs := intervalErrors.Swap(0)
			tReqs := totalReqs.Load()

			// [PERF] Histogram 기반 P99/Max 계산 (lock-free, 정렬 불필요)
			var bucketSnapshot [latBucketCount]int64
			var bucketTotal int64
			maxBucketIdx := -1
			for i := 0; i < latBucketCount; i++ {
				c := latBuckets[i].Swap(0)
				bucketSnapshot[i] = c
				bucketTotal += c
				if c > 0 {
					maxBucketIdx = i
				}
			}
			p99 := 0.0
			maxLat := 0.0
			if bucketTotal > 0 {
				if maxBucketIdx >= 0 {
					maxLat = float64(maxBucketIdx) * 0.5
				}
				target := int64(float64(bucketTotal)*0.99) + 1
				var cumulative int64
				for i := 0; i < latBucketCount; i++ {
					cumulative += bucketSnapshot[i]
					if cumulative >= target {
						p99 = float64(i) * 0.5
						break
					}
				}
			}

			tps := float64(iReqs) / 5.0
			avgLatMs := 0.0
			errPct := 0.0
			if iReqs > 0 {
				avgLatMs = (float64(iLat) / float64(iReqs)) / 1e6
				errPct = (float64(iErrs) / float64(iReqs)) * 100.0
			}

			elapsed := time.Since(attackStart).Round(time.Second)
			tgName := "Global"
			if len(plan.ThreadGroups) > 0 {
				tgName = plan.ThreadGroups[0].Name
			}
			log.Printf("[Dashboard: %s] %02d:%02d | TPS: %5.1f | Avg: %5.1fms | P99: %5.1fms | Max: %5.1fms | Err: %3.1f%% | TotReq: %d",
				tgName, int(elapsed.Minutes()), int(elapsed.Seconds())%60, tps, avgLatMs, p99, maxLat, errPct, tReqs)
		}

		if resultsChan == nil {
			break
		}
	}

	elapsed := time.Since(attackStart).Round(time.Millisecond)
	log.Printf("[VegetaRunner] Step completed in %s.", elapsed)
	return nil
}
