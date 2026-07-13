package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
)

func poissonDelay(lambda float64) float64 {
	if lambda <= 0 {
		return 0
	}
	L := math.Exp(-lambda)
	k := 0
	p := 1.0
	for p > L {
		k++
		p *= rand.Float64()
	}
	return float64(k - 1)
}

type SyncBarrier struct {
	groupSize int
	timeout   time.Duration
	mutex     sync.Mutex
	count     int
	release   chan struct{}
}

func newSyncBarrier(size int, timeoutMs int64) *SyncBarrier {
	return &SyncBarrier{
		groupSize: size,
		timeout:   time.Duration(timeoutMs) * time.Millisecond,
		release:   make(chan struct{}),
	}
}

func (b *SyncBarrier) Wait() {
	if b.groupSize <= 1 {
		return
	}
	b.mutex.Lock()
	b.count++
	if b.count >= b.groupSize {
		b.count = 0
		close(b.release)
		b.release = make(chan struct{})
		b.mutex.Unlock()
		return
	}
	rel := b.release
	b.mutex.Unlock()

	if b.timeout > 0 {
		select {
		case <-rel:
		case <-time.After(b.timeout):
			b.mutex.Lock()
			if b.release == rel {
				b.count = 0
				close(b.release)
				b.release = make(chan struct{})
			}
			b.mutex.Unlock()
		}
	} else {
		<-rel
	}
}

type CSVRuntime struct {
	Config   *domain.CSVDataSet
	Lines    [][]string
	Next     *int64
	VarNames []string // pre-parsed variable names (hot path 최적화)
}

func parseCSV(cfg *domain.CSVDataSet) *CSVRuntime {
	content, err := os.ReadFile(cfg.Filename)
	if err != nil {
		log.Printf("[Warning] CSVDataSet failed to read %s: %v", cfg.Filename, err)
		return nil
	}
	var lines [][]string
	linesRaw := strings.Split(string(content), "\n")
	for i, line := range linesRaw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i == 0 && cfg.IgnoreFirstLine {
			continue
		}
		delim := cfg.Delimiter
		if delim == "" {
			delim = ","
		}
		lines = append(lines, strings.Split(line, delim))
	}
	var next int64 = 0
	var varNames []string
	for _, vn := range strings.Split(cfg.VariableNames, ",") {
		varNames = append(varNames, strings.TrimSpace(vn))
	}
	return &CSVRuntime{
		Config:   cfg,
		Lines:    lines,
		Next:     &next,
		VarNames: varNames,
	}
}

type CacheEntry struct {
	ETag         string
	LastModified string
}

type CounterRuntime struct {
	Config  *domain.Counter
	Current *int64
	Start   int64
	End     int64
	Incr    int64
}

var bufferPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

var globalLocks sync.Map

// Session represents a virtual user executing a Thread Group sequentially
type Session struct {
	ID             uint64
	Variables      map[string]string
	Evaluator      evaluator.Evaluator
	Tg             *domain.ThreadGroup
	Step           int // Current sampler index
	Cache          map[string]*CacheEntry
	LoopCounters   map[int]int
	HeldLocks      map[string]*sync.Mutex
	InterleaveJump map[int]int
}

func NewSession(id uint64, tg *domain.ThreadGroup, globalEval evaluator.Evaluator) *Session {
	// Create a new evaluator for this session
	return &Session{
		ID:             id,
		Variables:      make(map[string]string),
		Evaluator:      globalEval.Clone(),
		Tg:             tg,
		Step:           0,
		Cache:          make(map[string]*CacheEntry),
		LoopCounters:   make(map[int]int),
		HeldLocks:      make(map[string]*sync.Mutex),
		InterleaveJump: make(map[int]int),
	}
}

// StatefulAttacker is a custom attacker that manages virtual user sessions
type StatefulAttacker struct {
	transport *http.Transport
	maxW      uint64
	pacer     vegeta.Pacer
	dur       time.Duration
}

func NewStatefulAttacker(workers uint64, pacer vegeta.Pacer, dur time.Duration) *StatefulAttacker {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10000,
		MaxIdleConnsPerHost:   10000,
		MaxConnsPerHost:       10000,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &StatefulAttacker{
		transport: transport,
		maxW:      workers,
		pacer:     pacer,
		dur:       dur,
	}
}

type RandomVariableRuntime struct {
	Config *domain.RandomVariable
	Rand   *rand.Rand
	Mutex  *sync.Mutex
	Min    int64
	Max    int64
}

func parseRandomVariable(cfg *domain.RandomVariable) *RandomVariableRuntime {
	min, _ := strconv.ParseInt(cfg.MinimumValue, 10, 64)
	max, _ := strconv.ParseInt(cfg.MaximumValue, 10, 64)
	if min > max {
		min, max = max, min
	}

	var r *rand.Rand
	if cfg.RandomSeed != "" {
		seed, err := strconv.ParseUint(cfg.RandomSeed, 10, 64)
		if err == nil {
			r = rand.New(rand.NewPCG(seed, seed))
		} else {
			r = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
		}
	} else {
		r = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
	}

	return &RandomVariableRuntime{
		Config: cfg,
		Rand:   r,
		Mutex:  &sync.Mutex{},
		Min:    min,
		Max:    max,
	}
}

func formatRandomVariable(val int64, format string) string {
	zeros := 0
	for i := len(format) - 1; i >= 0; i-- {
		if format[i] == '0' {
			zeros++
		} else {
			break
		}
	}
	if zeros > 0 {
		prefix := format[:len(format)-zeros]
		return fmt.Sprintf("%s%0*d", prefix, zeros, val)
	}
	return fmt.Sprintf("%s%d", format, val)
}

// Attack starts the stateful attack and returns a channel of results
func (a *StatefulAttacker) Attack(ctx context.Context, plan *domain.TestPlan, globalEval evaluator.Evaluator) <-chan *vegeta.Result {
	results := make(chan *vegeta.Result, 10000)

	go func() {
		defer close(results)

		var wg sync.WaitGroup
		fmt.Println("[StatefulAttacker] Starting stateful execution (Phase 5)...")

		var sharedCSVs []*CSVRuntime
		for _, csv := range plan.CSVDataSets {
			if rt := parseCSV(csv); rt != nil {
				sharedCSVs = append(sharedCSVs, rt)
			}
		}

		var sharedCounters []*CounterRuntime
		parseCounter := func(c *domain.Counter) *CounterRuntime {
			startVal, _ := strconv.ParseInt(globalEval.Evaluate(c.Start), 10, 64)
			endVal, _ := strconv.ParseInt(globalEval.Evaluate(c.End), 10, 64)
			incrVal, _ := strconv.ParseInt(globalEval.Evaluate(c.Incr), 10, 64)
			if incrVal == 0 {
				incrVal = 1
			}
			curr := startVal
			return &CounterRuntime{
				Config:  c,
				Current: &curr,
				Start:   startVal,
				End:     endVal,
				Incr:    incrVal,
			}
		}

		for _, c := range plan.Counters {
			sharedCounters = append(sharedCounters, parseCounter(c))
		}

		var sharedRandomVariables []*RandomVariableRuntime
		for _, rv := range plan.RandomVariables {
			if !rv.PerThread {
				sharedRandomVariables = append(sharedRandomVariables, parseRandomVariable(rv))
			}
		}

		if len(plan.ThreadGroups) > 0 {
			for _, csv := range plan.ThreadGroups[0].CSVDataSets {
				if rt := parseCSV(csv); rt != nil {
					sharedCSVs = append(sharedCSVs, rt)
				}
			}
			for _, c := range plan.ThreadGroups[0].Counters {
				sharedCounters = append(sharedCounters, parseCounter(c))
			}
			for _, rv := range plan.ThreadGroups[0].RandomVariables {
				if !rv.PerThread {
					sharedRandomVariables = append(sharedRandomVariables, parseRandomVariable(rv))
				}
			}
		}

		syncBarriers := make(map[*domain.Timer]*SyncBarrier)
		if len(plan.ThreadGroups) > 0 {
			for _, timer := range plan.ThreadGroups[0].Timers {
				if timer.Type == "SyncTimer" {
					sizeStr := globalEval.Evaluate(timer.GroupSize)
					size, _ := strconv.Atoi(sizeStr)
					if size == 0 {
						size = int(a.maxW) // Default to number of workers
					}
					timeoutStr := globalEval.Evaluate(timer.TimeoutInMs)
					timeoutMs, _ := strconv.ParseInt(timeoutStr, 10, 64)
					syncBarriers[timer] = newSyncBarrier(size, timeoutMs)
				}
			}
		}

		var dnsManager *domain.DNSCacheManager
		if plan.DNSCacheManager != nil {
			dnsManager = plan.DNSCacheManager
		} else if len(plan.ThreadGroups) > 0 && plan.ThreadGroups[0].DNSCacheManager != nil {
			dnsManager = plan.ThreadGroups[0].DNSCacheManager
		}

		if dnsManager != nil {
			dialer := &net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}

			resolver := net.DefaultResolver
			if dnsManager.IsCustomResolver && len(dnsManager.Servers) > 0 {
				resolver = &net.Resolver{
					PreferGo: true,
					Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
						dnsServer := dnsManager.Servers[0]
						if !strings.Contains(dnsServer, ":") {
							dnsServer = net.JoinHostPort(dnsServer, "53")
						}
						return dialer.DialContext(ctx, "udp", dnsServer)
					},
				}
			}

			a.transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err == nil {
					if ip, ok := dnsManager.Hosts[host]; ok {
						addr = net.JoinHostPort(ip, port)
					} else if dnsManager.IsCustomResolver && len(dnsManager.Servers) > 0 {
						ips, err := resolver.LookupIPAddr(ctx, host)
						if err == nil && len(ips) > 0 {
							addr = net.JoinHostPort(ips[0].IP.String(), port)
						}
					}
				}
				return dialer.DialContext(ctx, network, addr)
			}
		}

		attackStart := time.Now()

		tokens := make(chan struct{}, int(a.maxW)*2) // 버퍼 추가: 생산자 goroutine 블로킹 방지
		go func() {
			var hits uint64
			for {
				if ctx.Err() != nil {
					close(tokens)
					return
				}
				elapsed := time.Since(attackStart)
				if a.dur > 0 && elapsed >= a.dur {
					close(tokens)
					return
				}
				wait, stop := a.pacer.Pace(elapsed, hits)
				if stop {
					close(tokens)
					return
				}
				if wait > 5*time.Millisecond {
					time.Sleep(wait)
				} else if wait > 0 {
					target := time.Now().Add(wait)
					for time.Now().Before(target) {
						// spin-wait for precise pacing
					}
				}
				select {
				case tokens <- struct{}{}:
					hits++
				case <-ctx.Done():
					close(tokens)
					return
				}
			}
		}()

		for i := uint64(0); i < a.maxW; i++ {
			wg.Add(1)
			go func(sessionID uint64) {
				defer wg.Done()

				if len(plan.ThreadGroups) == 0 {
					return
				}
				tgIdx := int(sessionID) % len(plan.ThreadGroups)
				tg := plan.ThreadGroups[tgIdx]
				session := NewSession(sessionID, tg, globalEval.Clone())

				var localRandomVariables []*RandomVariableRuntime
				for _, rv := range plan.RandomVariables {
					if rv.PerThread {
						localRandomVariables = append(localRandomVariables, parseRandomVariable(rv))
					}
				}
				for _, rv := range tg.RandomVariables {
					if rv.PerThread {
						localRandomVariables = append(localRandomVariables, parseRandomVariable(rv))
					}
				}

				var localCSVs []*CSVRuntime
				for _, scsv := range sharedCSVs {
					if scsv.Config.ShareMode == "shareMode.thread" {
						var next int64 = 0
						localCSVs = append(localCSVs, &CSVRuntime{
							Config:   scsv.Config,
							Lines:    scsv.Lines,
							Next:     &next,
							VarNames: scsv.VarNames,
						})
					} else {
						localCSVs = append(localCSVs, scsv)
					}
				}

				var localCounters []*CounterRuntime
				for _, sc := range sharedCounters {
					if sc.Config.PerUser {
						curr := sc.Start
						localCounters = append(localCounters, &CounterRuntime{
							Config:  sc.Config,
							Current: &curr,
							Start:   sc.Start,
							End:     sc.End,
							Incr:    sc.Incr,
						})
					} else {
						localCounters = append(localCounters, sc)
					}
				}

				var cookieManager *domain.CookieManager
				if plan.CookieManager != nil {
					cookieManager = plan.CookieManager
				}
				if tg.CookieManager != nil {
					cookieManager = tg.CookieManager
				}

				var cacheManager *domain.CacheManager
				if plan.CacheManager != nil {
					cacheManager = plan.CacheManager
				}
				if tg.CacheManager != nil {
					cacheManager = tg.CacheManager
				}

				var authManager *domain.AuthManager
				if plan.AuthManager != nil {
					authManager = plan.AuthManager
				}
				if tg.AuthManager != nil {
					authManager = tg.AuthManager
				}

				createCookieJar := func() http.CookieJar {
					jar, _ := cookiejar.New(nil)
					if cookieManager != nil {
						for _, c := range cookieManager.Cookies {
							domainStr := session.Evaluator.Evaluate(c.Domain)
							pathStr := session.Evaluator.Evaluate(c.Path)
							if pathStr == "" {
								pathStr = "/"
							}
							u := &url.URL{
								Scheme: "http",
								Host:   domainStr,
								Path:   pathStr,
							}
							if c.Secure {
								u.Scheme = "https"
							}
							hc := &http.Cookie{
								Name:   session.Evaluator.Evaluate(c.Name),
								Value:  session.Evaluator.Evaluate(c.Value),
								Domain: domainStr,
								Path:   pathStr,
								Secure: c.Secure,
							}
							jar.SetCookies(u, []*http.Cookie{hc})
						}
					}
					return jar
				}

				sessionClient := &http.Client{
					Transport: a.transport,
					Timeout:   30 * time.Second,
					Jar:       createCookieJar(),
				}

				// [PERF] allRVs를 루프 밖에서 1회만 계산 (매 이터레이션 heap 할당 제거)
				allRVs := make([]*RandomVariableRuntime, 0, len(sharedRandomVariables)+len(localRandomVariables))
				allRVs = append(allRVs, sharedRandomVariables...)
				allRVs = append(allRVs, localRandomVariables...)

				// Ensure any held locks are released when worker exits
				defer func() {
					for name, mu := range session.HeldLocks {
						mu.Unlock()
						delete(session.HeldLocks, name)
					}
				}()

				for {
					if ctx.Err() != nil {
						return
					}

					if cookieManager != nil && cookieManager.ClearEachIteration {
						sessionClient.Jar = createCookieJar()
					}
					if cacheManager != nil && cacheManager.ClearEachIteration {
						session.Cache = make(map[string]*CacheEntry)
					}

					for _, rv := range allRVs {
						var val int64
						if rv.Config.PerThread {
							val = rv.Min + rv.Rand.Int64N(rv.Max-rv.Min+1)
						} else {
							rv.Mutex.Lock()
							val = rv.Min + rv.Rand.Int64N(rv.Max-rv.Min+1)
							rv.Mutex.Unlock()
						}
						formatStr := rv.Config.Format
						if formatStr == "" {
							session.Evaluator.SetVariable(rv.Config.Name, strconv.FormatInt(val, 10))
						} else {
							session.Evaluator.SetVariable(rv.Config.Name, formatRandomVariable(val, formatStr))
						}
					}

					// Bind CSV variables at the start of each iteration
					for _, csv := range localCSVs {
						if len(csv.Lines) == 0 {
							continue
						}
						idx := atomic.AddInt64(csv.Next, 1) - 1
						if !csv.Config.Recycle && int(idx) >= len(csv.Lines) {
							if csv.Config.StopThread {
								return // Stop this thread
							}
							continue
						}
						row := csv.Lines[int(idx)%len(csv.Lines)]
						// [PERF] VarNames는 parseCSV 시 1회만 파싱됨 (매 이터레이션 strings.Split 제거)
						for i, vName := range csv.VarNames {
							if vName != "" && i < len(row) {
								session.Variables[vName] = row[i]
								session.Evaluator.SetVariable(vName, row[i])
							}
						}
					}

					// Evaluate Counters
					for _, c := range localCounters {
						var val int64
						if c.End != 0 {
							for {
								curr := atomic.LoadInt64(c.Current)
								val = curr
								next := curr + c.Incr
								if next > c.End {
									next = c.Start
								}
								if atomic.CompareAndSwapInt64(c.Current, curr, next) {
									break
								}
							}
						} else {
							val = atomic.AddInt64(c.Current, c.Incr) - c.Incr
						}

						var valStr string
						if c.Config.Format != "" {
							formatLen := len(c.Config.Format)
							valStr = fmt.Sprintf("%0*d", formatLen, val)
						} else {
							valStr = strconv.FormatInt(val, 10)
						}
						session.Variables[c.Config.Name] = valStr
						session.Evaluator.SetVariable(c.Config.Name, valStr)
					}

					elapsed := time.Since(attackStart)
					if a.dur > 0 && elapsed >= a.dur {
						return
					}

					select {
					case _, ok := <-tokens:
						if !ok {
							return
						}
					case <-ctx.Done():
						return
					}

					// Execute samplers sequentially
					for step := 0; step < len(tg.Samplers); step++ {
						if jump, ok := session.InterleaveJump[step]; ok {
							delete(session.InterleaveJump, step)
							step = jump
						}
						sampler := tg.Samplers[step]

						if sampler.IsControlFlow {
							switch sampler.ControlType {
							case "LoopStart":
								if _, ok := session.LoopCounters[sampler.LoopId]; !ok {
									if sampler.LoopContinue {
										session.LoopCounters[sampler.LoopId] = -1
									} else {
										cStr := session.Evaluator.Evaluate(sampler.LoopCountExpr)
										c, err := strconv.Atoi(cStr)
										if err != nil || c < 1 {
											c = 1 // default or invalid
										}
										session.LoopCounters[sampler.LoopId] = c
									}
								}
							case "LoopEnd":
								if count, ok := session.LoopCounters[sampler.LoopId]; ok {
									if count == -1 || count > 1 {
										if count > 1 {
											session.LoopCounters[sampler.LoopId] = count - 1
										}
										step = sampler.LoopJumpIndex // jump back to first item inside loop
									} else {
										delete(session.LoopCounters, sampler.LoopId) // loop done
									}
								}
							case "WhileStart":
								condStr := sampler.WhileCondition
								if condStr != "" && strings.ToUpper(condStr) != "LAST" {
									// EvaluateLogic returns false if expression resolves to false
									if !session.Evaluator.EvaluateLogic(condStr) {
										// Exit loop: jump to WhileEnd (step++ will advance to the next sampler)
										step = sampler.LoopJumpIndex
									}
								}
							case "WhileEnd":
								// Jump back to WhileStart so condition is evaluated again
								step = sampler.LoopJumpIndex - 1
							case "CriticalStart":
								lockName := session.Evaluator.Evaluate(sampler.CriticalLockName)
								if lockName == "" {
									lockName = "global_lock"
								}
								// If we don't already hold it
								if _, held := session.HeldLocks[lockName]; !held {
									muIntf, _ := globalLocks.LoadOrStore(lockName, &sync.Mutex{})
									mu := muIntf.(*sync.Mutex)
									mu.Lock()
									session.HeldLocks[lockName] = mu
								}
							case "CriticalEnd":
								lockName := session.Evaluator.Evaluate(sampler.CriticalLockName)
								if lockName == "" {
									lockName = "global_lock"
								}
								if mu, held := session.HeldLocks[lockName]; held {
									mu.Unlock()
									delete(session.HeldLocks, lockName)
								}
							case "ForEachStart":
								// Initialize if not exists
								if _, ok := session.LoopCounters[sampler.LoopId]; !ok {
									startIdx := 0
									if sampler.ForEachStartIndex != "" {
										if val, err := strconv.Atoi(session.Evaluator.Evaluate(sampler.ForEachStartIndex)); err == nil {
											startIdx = val
										}
									}
									session.LoopCounters[sampler.LoopId] = startIdx + 1
								}

								idx := session.LoopCounters[sampler.LoopId]

								// Check endIndex if specified
								if sampler.ForEachEndIndex != "" {
									if endIdx, err := strconv.Atoi(session.Evaluator.Evaluate(sampler.ForEachEndIndex)); err == nil {
										if idx > endIdx {
											// exit loop
											delete(session.LoopCounters, sampler.LoopId)
											step = sampler.LoopJumpIndex
											continue
										}
									}
								}

								// Construct var name
								sep := ""
								if sampler.ForEachUseSeparator {
									sep = "_"
								}
								inputVarName := fmt.Sprintf("%s%s%d", session.Evaluator.Evaluate(sampler.ForEachInputVal), sep, idx)

								valStr := session.Evaluator.Evaluate("${" + inputVarName + "}")
								if valStr == "${"+inputVarName+"}" {
									// Variable does not exist, exit loop
									delete(session.LoopCounters, sampler.LoopId)
									step = sampler.LoopJumpIndex
									continue
								}

								// Set return variable
								returnVar := session.Evaluator.Evaluate(sampler.ForEachReturnVal)
								if returnVar != "" {
									session.Variables[returnVar] = valStr
									session.Evaluator.SetVariable(returnVar, valStr)
								}
							case "ForEachEnd":
								session.LoopCounters[sampler.LoopId]++
								step = sampler.LoopJumpIndex - 1 // jump back to ForEachStart
							case "InterleaveStart":
								session.LoopCounters[sampler.LoopId]++
								if len(sampler.InterleaveChildStarts) > 0 {
									childIndex := (session.LoopCounters[sampler.LoopId] - 1) % len(sampler.InterleaveChildStarts)
									startStep := sampler.InterleaveChildStarts[childIndex]
									endStep := sampler.InterleaveChildEnds[childIndex]

									// When step reaches endStep + 1, jump to InterleaveEnd
									session.InterleaveJump[endStep+1] = sampler.BlockEndIndex

									// jump to the child
									step = startStep - 1
								} else {
									// No children, just skip to end
									step = sampler.BlockEndIndex
								}
							case "InterleaveEnd":
								// Just fall through
							case "OnceOnlyStart":
								if session.LoopCounters[sampler.LoopId] > 0 {
									step = sampler.BlockEndIndex
								} else {
									session.LoopCounters[sampler.LoopId]++
								}
							case "OnceOnlyEnd":
								// Just fall through
							}
							continue
						}

						if sampler.IfCondition != "" {
							if !session.Evaluator.EvaluateLogic(sampler.IfCondition) {
								continue
							}
						}

						if ctx.Err() != nil || (a.dur > 0 && time.Since(attackStart) >= a.dur) {
							break
						}

						// Apply timers BEFORE the sampler runs
						var totalDelay time.Duration
						for _, timer := range tg.Timers {
							delayStr := session.Evaluator.Evaluate(timer.Delay)
							rangeStr := session.Evaluator.Evaluate(timer.Range)

							delayMs, _ := strconv.ParseFloat(delayStr, 64)
							rangeMs, _ := strconv.ParseFloat(rangeStr, 64)

							var sleepMs float64
							switch timer.Type {
							case "ConstantTimer":
								sleepMs = delayMs
							case "UniformRandomTimer":
								sleepMs = delayMs + rand.Float64()*rangeMs
							case "GaussianRandomTimer":
								sleepMs = delayMs + math.Abs(rand.NormFloat64())*rangeMs
							case "PoissonRandomTimer":
								sleepMs = delayMs + poissonDelay(rangeMs)
							case "SyncTimer":
								if barrier, ok := syncBarriers[timer]; ok {
									barrier.Wait()
								}
							}
							if sleepMs > 0 {
								totalDelay += time.Duration(sleepMs) * time.Millisecond
							}
						}
						if totalDelay > 0 {
							time.Sleep(totalDelay)
						}

						// Evaluate variables in URL
						url := session.Evaluator.Evaluate(sampler.Request.URL)
						method := sampler.Request.Method
						bodyStr := session.Evaluator.Evaluate(sampler.Request.BodyTemplate)

						var bodyReader io.Reader
						if bodyStr != "" {
							bodyReader = strings.NewReader(bodyStr)
						}
						req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
						if err != nil {
							continue
						}

						// Evaluate headers
						for k, v := range sampler.Request.Headers {
							req.Header.Set(k, session.Evaluator.Evaluate(v))
						}

						if cacheManager != nil {
							if entry, ok := session.Cache[url]; ok {
								if entry.ETag != "" {
									req.Header.Set("If-None-Match", entry.ETag)
								}
								if entry.LastModified != "" {
									req.Header.Set("If-Modified-Since", entry.LastModified)
								}
							}
						}

						if authManager != nil {
							for _, auth := range authManager.AuthList {
								authUrl := session.Evaluator.Evaluate(auth.URL)
								if strings.HasPrefix(url, authUrl) {
									user := session.Evaluator.Evaluate(auth.Username)
									pass := session.Evaluator.Evaluate(auth.Password)
									mech := session.Evaluator.Evaluate(auth.Mechanism)
									if mech == "" || mech == "BASIC_DIGEST" || mech == "BASIC" {
										authStr := user + ":" + pass
										b64 := base64.StdEncoding.EncodeToString([]byte(authStr))
										req.Header.Set("Authorization", "Basic "+b64)
									}
									break
								}
							}
						}

						start := time.Now()
						resp, err := sessionClient.Do(req)
						elapsed := time.Since(start)

						attackName := sampler.Name
						if sampler.TransactionName != "" && sampler.TransactionParent {
							attackName = sampler.TransactionName
						}

						res := &vegeta.Result{
							Attack:    attackName,
							Seq:       uint64(step),
							Timestamp: start,
							Latency:   elapsed,
							Method:    method,
							URL:       url,
						}

						var bodyBytes []byte
						if err == nil {
							res.Code = uint16(resp.StatusCode)
							needsBody := len(sampler.Extractors) > 0 || len(sampler.Assertions) > 0

							bufPtr := bufferPool.Get().(*[]byte)
							buf := *bufPtr

							if needsBody {
								// bytes.Buffer 직접 사용: strings.Builder 경유 시 2번 복사 → 0번으로 최적화
								var bb bytes.Buffer
								bb.Grow(4096)
								written, _ := io.CopyBuffer(&bb, resp.Body, buf)
								bodyBytes = bb.Bytes()
								res.BytesIn = uint64(written)
							} else {
								written, _ := io.CopyBuffer(io.Discard, resp.Body, buf)
								res.BytesIn = uint64(written)
							}

							_ = resp.Body.Close()  // Close 먼저 (읽기 완료 보장)
							bufferPool.Put(bufPtr) // 그 다음 Pool 반환

							if cacheManager != nil && (resp.StatusCode == 200 || resp.StatusCode == 304) {
								etag := resp.Header.Get("ETag")
								lastMod := resp.Header.Get("Last-Modified")
								if etag != "" || lastMod != "" {
									if len(session.Cache) < cacheManager.MaxSize {
										session.Cache[url] = &CacheEntry{
											ETag:         etag,
											LastModified: lastMod,
										}
									}
								}
							}

							// Execute extractors
							for _, ext := range sampler.Extractors {
								if ext == nil {
									continue
								}

								if multiExt, ok := ext.(domain.MultiExtractor); ok {
									vals, extractOk := multiExt.ExtractMulti(bodyBytes)
									if extractOk && len(vals) > 0 {
										for k, v := range vals {
											session.Variables[k] = v
											session.Evaluator.SetVariable(k, v)
										}
										continue
									}
									// Fallback to default below if not found
								}

								val, extractOk := ext.Extract(bodyBytes)
								if !extractOk {
									val = ext.DefaultValue()
								}
								session.Variables[ext.RefName()] = val
								session.Evaluator.SetVariable(ext.RefName(), val)
							}

							// Execute assertions
							for _, ast := range sampler.Assertions {
								if err := evaluateAssertion(ast, resp, bodyBytes, session); err != nil {
									res.Error = err.Error()
									break // fail fast on first assertion error
								}
							}

						} else {
							res.Error = err.Error()
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

func evaluateAssertion(ast domain.Assertion, resp *http.Response, bodyBytes []byte, session *Session) error {
	switch a := ast.(type) {
	case *domain.ResponseAssertion:
		var target string
		switch a.TestField {
		case "Assertion.response_code":
			target = strconv.Itoa(resp.StatusCode)
		case "Assertion.response_message":
			target = resp.Status
		case "Assertion.response_headers":
			var sb strings.Builder
			for k, v := range resp.Header {
				sb.WriteString(k)
				sb.WriteString(": ")
				sb.WriteString(strings.Join(v, ", "))
				sb.WriteString("\n")
			}
			target = sb.String()
		default: // "Assertion.response_data"
			target = string(bodyBytes)
		}

		isNot := (a.TestType & 32) != 0
		isOr := (a.TestType & 64) != 0

		matchCount := 0
		for _, testStr := range a.TestStrings {
			evalStr := session.Evaluator.Evaluate(testStr)

			matched := false

			// Extract base test type by masking out Not and Or flags
			baseType := a.TestType &^ (32 | 64)

			switch baseType {
			case 1: // Matches (regex exact)
				matched, _ = regexp.MatchString("^"+evalStr+"$", target)
			case 2: // Contains (regex contains)
				matched, _ = regexp.MatchString(evalStr, target)
			case 8: // Equals (exact match)
				matched = (target == evalStr)
			case 16: // Substring (literal contains)
				matched = strings.Contains(target, evalStr)
			default:
				matched = strings.Contains(target, evalStr)
			}

			if isNot {
				matched = !matched
			}

			if matched {
				matchCount++
				if isOr {
					return nil // Fast success for OR
				}
			} else {
				if !isOr {
					failMsg := a.CustomFailure
					if failMsg == "" {
						failMsg = fmt.Sprintf("ResponseAssertion failed: expected '%s' in %s", evalStr, a.TestField)
					}
					return fmt.Errorf("%s", session.Evaluator.Evaluate(failMsg))
				}
			}
		}

		if isOr && matchCount == 0 && len(a.TestStrings) > 0 {
			failMsg := a.CustomFailure
			if failMsg == "" {
				failMsg = "ResponseAssertion failed: none of the OR conditions met"
			}
			return fmt.Errorf("%s", session.Evaluator.Evaluate(failMsg))
		}

		return nil

	case *domain.JSONAssertion:
		actualValue, found := domain.EvaluateJSONPath(bodyBytes, a.JSONPath)
		if !found {
			if a.ExpectNull {
				return nil
			}
			return fmt.Errorf("JSONAssertion failed: path '%s' not found", a.JSONPath)
		}

		if a.ExpectNull {
			return fmt.Errorf("JSONAssertion failed: expected null but found value for '%s'", a.JSONPath)
		}

		if a.JSONValidation {
			expectedVal := session.Evaluator.Evaluate(a.ExpectedValue)
			matched := false
			if a.IsRegex {
				matched, _ = regexp.MatchString(expectedVal, actualValue)
			} else {
				matched = (actualValue == expectedVal)
			}

			if a.Invert {
				matched = !matched
			}

			if !matched {
				if a.Invert {
					return fmt.Errorf("JSONAssertion failed: value for '%s' matched '%s' but Invert is true", a.JSONPath, expectedVal)
				}
				return fmt.Errorf("JSONAssertion failed: expected '%s' for '%s', got '%s'", expectedVal, a.JSONPath, actualValue)
			}
		}
		return nil
	}
	return nil
}
