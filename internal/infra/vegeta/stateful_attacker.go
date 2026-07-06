package vegeta

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
)

// Session represents a virtual user executing a Thread Group sequentially
type Session struct {
	ID        uint64
	Variables map[string]string
	Evaluator evaluator.Evaluator
	Tg        *domain.ThreadGroup
	Step      int // Current sampler index
}

func NewSession(id uint64, tg *domain.ThreadGroup, globalEval evaluator.Evaluator) *Session {
	// Create a new evaluator for this session
	// We need to extend Evaluator to support cloning or passing variables
	return &Session{
		ID:        id,
		Variables: make(map[string]string),
		Evaluator: globalEval, // TODO: Make evaluator session-aware
		Tg:        tg,
		Step:      0,
	}
}

// StatefulAttacker is a custom attacker that manages virtual user sessions
type StatefulAttacker struct {
	client *http.Client
	maxW   uint64
	pacer  vegeta.Pacer
	dur    time.Duration
}

func NewStatefulAttacker(workers uint64, pacer vegeta.Pacer, dur time.Duration) *StatefulAttacker {
	transport := &http.Transport{
		MaxIdleConns:        10000,
		MaxIdleConnsPerHost: 10000,
		MaxConnsPerHost:     10000,
		IdleConnTimeout:     30 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
	return &StatefulAttacker{
		client: client,
		maxW:   workers,
		pacer:  pacer,
		dur:    dur,
	}
}

// Attack starts the stateful attack and returns a channel of results
func (a *StatefulAttacker) Attack(ctx context.Context, plan *domain.TestPlan, globalEval evaluator.Evaluator) <-chan *vegeta.Result {
	results := make(chan *vegeta.Result, 10000)

	go func() {
		defer close(results)

		var wg sync.WaitGroup
		fmt.Println("[StatefulAttacker] Starting stateful execution (Phase 5)...")

		attackStart := time.Now()
		var hits uint64
		var hitsMutex sync.Mutex

		for i := uint64(0); i < a.maxW; i++ {
			wg.Add(1)
			go func(sessionID uint64) {
				defer wg.Done()

				if len(plan.ThreadGroups) == 0 {
					return
				}
				tg := plan.ThreadGroups[0]
				session := NewSession(sessionID, tg, globalEval.Clone())

				for {
					if ctx.Err() != nil {
						return
					}
					
					elapsed := time.Since(attackStart)
					if a.dur > 0 && elapsed >= a.dur {
						return
					}

					hitsMutex.Lock()
					wait, stop := a.pacer.Pace(elapsed, hits)
					hits++
					hitsMutex.Unlock()

					if stop {
						return
					}
					if wait > 0 {
						time.Sleep(wait)
					}

					// Execute samplers sequentially
					for step, sampler := range tg.Samplers {
						if ctx.Err() != nil || (a.dur > 0 && time.Since(attackStart) >= a.dur) {
							break
						}

					// Evaluate variables in URL
					url := session.Evaluator.Evaluate(sampler.Request.URL)
					method := sampler.Request.Method
					bodyStr := session.Evaluator.Evaluate(sampler.Request.BodyTemplate)

					req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(bodyStr))
					if err != nil {
						continue
					}

					// Evaluate headers
					for k, v := range sampler.Request.Headers {
						req.Header.Set(k, session.Evaluator.Evaluate(v))
					}

					start := time.Now()
					resp, err := a.client.Do(req)
					elapsed := time.Since(start)

					res := &vegeta.Result{
						Attack:    "Stateful",
						Seq:       uint64(step),
						Timestamp: start,
						Latency:   elapsed,
						Method:    method,
						URL:       url,
					}

					var bodyBytes []byte
					if err == nil {
						res.Code = uint16(resp.StatusCode)
						bodyBytes, _ = io.ReadAll(resp.Body)
						_ = resp.Body.Close()
						res.BytesIn = uint64(len(bodyBytes))

						// Execute extractors
						log.Printf("[Stateful] Sampler %d has %d extractors", step, len(sampler.Extractors))
						for _, ext := range sampler.Extractors {
							if ext == nil {
								continue
							}
							val, ok := ext.Extract(bodyBytes)
							if !ok {
								val = ext.DefaultValue()
							}
							log.Printf("[Stateful] Extracted %s = %s", ext.RefName(), val)
							session.Variables[ext.RefName()] = val
							session.Evaluator.SetVariable(ext.RefName(), val)
						}
					} else {
						res.Error = err.Error()
						log.Printf("[Stateful] Sampler error: %v", err)
					}

					select {
					case results <- res:
					case <-ctx.Done():
						return
					}
				}
				}
			}(i)
		}

		wg.Wait()
	}()

	return results
}
