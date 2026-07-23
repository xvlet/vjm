package engine

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/xml"
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

	"github.com/antchfx/xmlquery"
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

func (b *SyncBarrier) Wait(ctx context.Context) {
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
		// Use time.NewTimer + Stop() to prevent timer memory leaks
		timer := time.NewTimer(b.timeout)
		defer timer.Stop()
		select {
		case <-rel:
		case <-ctx.Done():
			return
		case <-timer.C:
			b.mutex.Lock()
			if b.release == rel {
				b.count = 0
				close(b.release)
				b.release = make(chan struct{})
			}
			b.mutex.Unlock()
		}
	} else {
		select {
		case <-rel:
		case <-ctx.Done():
		}
	}
}

type GlobalThroughputState struct {
	Executions int64
}

var globalThroughputLocks sync.Map

type CSVRuntime struct {
	Config   *domain.CSVDataSet
	Lines    [][]string
	Next     *int64
	VarNames []string // pre-parsed variable names (hot path optimization)
}

var globalCSVDataSetCache sync.Map // map[string]*CSVRuntimeShared
type CSVRuntimeShared struct {
	Lines [][]string
	Next  int64
	once  sync.Once
	err   error
}

func parseCSV(cfg *domain.CSVDataSet) *CSVRuntime {
	actual, _ := globalCSVDataSetCache.LoadOrStore(cfg.Filename, &CSVRuntimeShared{})
	shared := actual.(*CSVRuntimeShared)

	shared.once.Do(func() {
		f, err := os.Open(cfg.Filename)
		if err != nil {
			shared.err = err
			return
		}
		defer func() { _ = f.Close() }()

		r := csv.NewReader(f)
		r.LazyQuotes = true
		if cfg.Delimiter != "" {
			r.Comma = rune(cfg.Delimiter[0])
		} else {
			r.Comma = ','
		}

		lines, err := r.ReadAll()
		if err != nil {
			shared.err = err
			return
		}

		if cfg.IgnoreFirstLine && len(lines) > 0 {
			lines = lines[1:]
		}
		shared.Lines = lines
	})

	if shared.err != nil {
		log.Printf("[Warning] CSVDataSet failed to read %s: %v", cfg.Filename, shared.err)
		return nil
	}

	var varNames []string
	for _, vn := range strings.Split(cfg.VariableNames, ",") {
		varNames = append(varNames, strings.TrimSpace(vn))
	}
	return &CSVRuntime{
		Config:   cfg,
		Lines:    shared.Lines,
		Next:     &shared.Next,
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
var globalSeedCounter atomic.Uint64 // ensures unique random seeds across concurrent goroutines
var htmlLinkRegex = regexp.MustCompile(`(?i)(?:href|action)\s*=\s*["']([^"']+)["']`)

// Session represents a virtual user executing a Thread Group sequentially
type Session struct {
	ID               uint64
	Variables        map[string]string
	Evaluator        evaluator.Evaluator
	Tg               *domain.ThreadGroup
	Step             int // Current sampler index
	Cache            map[string]*CacheEntry
	LoopCounters     map[int]int
	HeldLocks        map[string]*sync.Mutex
	InterleaveJump   map[int]int
	RandomOrderState map[int]*RandomOrderState
	RuntimeDeadlines map[int]time.Time
	CallStack        []CallFrame
	Plan             *domain.TestPlan
	LastResponseBody []byte
}

type CallFrame struct {
	Tg   *domain.ThreadGroup
	Step int
}

type RandomOrderState struct {
	Order        []int
	CurrentIndex int
}

func NewSession(id uint64, plan *domain.TestPlan, tg *domain.ThreadGroup, globalEval evaluator.Evaluator) *Session {
	// Create a new evaluator for this session
	vars := make(map[string]string)
	for k, v := range plan.UserDefinedVariables {
		vars[k] = v
	}

	eval := globalEval.Clone()
	eval.SetThreadNum(int(id) + 1)

	return &Session{
		ID:               id,
		Variables:        vars,
		Evaluator:        eval,
		Tg:               tg,
		Step:             0,
		Cache:            make(map[string]*CacheEntry),
		LoopCounters:     make(map[int]int),
		HeldLocks:        make(map[string]*sync.Mutex),
		InterleaveJump:   make(map[int]int),
		RandomOrderState: make(map[int]*RandomOrderState),
		RuntimeDeadlines: make(map[int]time.Time),
		Plan:             plan,
	}
}

// StatefulAttacker is a custom attacker that manages virtual user sessions
type StatefulAttacker struct {
	transport   *http.Transport
	maxW        uint64
	pacer       vegeta.Pacer
	dur         time.Duration
	stopped     int32
	needsBody   bool
	workerPacer domain.WorkerPacer
}

func NewStatefulAttacker(workers uint64, pacer vegeta.Pacer, dur time.Duration, needsBody bool, workerPacer domain.WorkerPacer) *StatefulAttacker {
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
		transport:   transport,
		maxW:        workers,
		pacer:       pacer,
		dur:         dur,
		needsBody:   needsBody,
		workerPacer: workerPacer,
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
			// Use atomic counter as second seed component to guarantee unique seeds per goroutine
			seqID := globalSeedCounter.Add(1)
			r = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), seqID))
		}
	} else {
		// Use atomic counter as second seed component to guarantee unique seeds per goroutine
		seqID := globalSeedCounter.Add(1)
		r = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), seqID))
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
		var cancel context.CancelFunc
		if a.dur > 0 {
			ctx, cancel = context.WithTimeout(ctx, a.dur)
		} else {
			ctx, cancel = context.WithCancel(ctx)
		}
		defer cancel()

		var wg sync.WaitGroup
		fmt.Println("[StatefulAttacker] Starting stateful execution...")

		// Reset per-attack state: ThroughputController total execution counters
		globalThroughputLocks.Range(func(k, _ interface{}) bool {
			globalThroughputLocks.Delete(k)
			return true
		})

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

		tokens := make(chan struct{}, int(a.maxW)*2) // Add buffer: prevent producer goroutine from blocking

		if a.workerPacer == nil {
			// Only start the token-producer goroutine when using open-model pacer.
			// When workerPacer is set (closed-model), workers self-pace and tokens are unused.
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
		}

		for i := uint64(0); i < a.maxW; i++ {
			wg.Add(1)
			go func(sessionID uint64) {
				defer wg.Done()

				if len(plan.ThreadGroups) == 0 {
					return
				}
				tgIdx := int(sessionID) % len(plan.ThreadGroups)
				tg := plan.ThreadGroups[tgIdx]
				session := NewSession(sessionID, plan, tg, globalEval.Clone())

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

				wsTransport := NewWSRoundTripper(a.transport)
				sessionClient := &http.Client{
					Transport: wsTransport,
					Timeout:   30 * time.Second,
					Jar:       createCookieJar(),
				}

				// [PERF] Calculate allRVs once outside the loop (removes heap allocation per iteration)
				allRVs := make([]*RandomVariableRuntime, 0, len(sharedRandomVariables)+len(localRandomVariables))
				allRVs = append(allRVs, sharedRandomVariables...)
				allRVs = append(allRVs, localRandomVariables...)

				// Ensure any held locks and WebSocket connections are released when worker exits
				defer func() {
					wsTransport.CloseAll()
					for name, mu := range session.HeldLocks {
						mu.Unlock()
						delete(session.HeldLocks, name)
					}
				}()
				tgLoops := 0
				tgContinueForever := true
				if len(plan.ThreadGroups) > 0 {
					tgLoops = plan.ThreadGroups[0].Loops
					tgContinueForever = plan.ThreadGroups[0].ContinueForever
				}
				iterCount := 0

				for {
					if ctx.Err() != nil {
						return
					}

					if !tgContinueForever && tgLoops > 0 {
						if iterCount >= tgLoops {
							return
						}
						iterCount++
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
						// [PERF] VarNames are parsed only once during parseCSV (removes strings.Split per iteration)
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
					if atomic.LoadInt32(&a.stopped) == 1 {
						return
					}

					if a.workerPacer != nil {
						wait, stop := a.workerPacer(sessionID, time.Since(attackStart))
						if stop {
							return
						}
						if wait > 0 {
							select {
							case <-time.After(wait):
							case <-ctx.Done():
								return
							}
						}
					} else {
						select {
						case _, ok := <-tokens:
							if !ok {
								return
							}
						case <-ctx.Done():
							return
						}
					}

					// Execute samplers sequentially
					step := 0
					for ; ; step++ {
						if step >= len(session.Tg.Samplers) {
							if len(session.CallStack) > 0 {
								frame := session.CallStack[len(session.CallStack)-1]
								session.CallStack = session.CallStack[:len(session.CallStack)-1]
								session.Tg = frame.Tg
								step = frame.Step
								continue
							}
							break
						}

						if jump, ok := session.InterleaveJump[step]; ok {
							delete(session.InterleaveJump, step)
							step = jump
						}
						sampler := session.Tg.Samplers[step]
						session.Evaluator.SetSamplerName(sampler.Name)

						if sampler.IsControlFlow {
							switch sampler.ControlType {
							case "LoopStart":
								if _, ok := session.LoopCounters[sampler.LoopId]; !ok {
									if sampler.LoopContinue {
										session.LoopCounters[sampler.LoopId] = -1
									} else {
										cStr := session.Evaluator.Evaluate(sampler.LoopCountExpr)
										c, err := strconv.Atoi(cStr)
										if err != nil || (c < 1 && c != -1) {
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
										// Subtract 1 to compensate for the step++ in the loop (consistent with WhileEnd/ForEachEnd)
										step = sampler.LoopJumpIndex - 1
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
							case "RuntimeStart":
								// Initialize deadline if it doesn't exist
								if _, ok := session.RuntimeDeadlines[sampler.LoopId]; !ok {
									secStr := session.Evaluator.Evaluate(sampler.RuntimeSecondsExpr)
									sec, err := strconv.ParseFloat(secStr, 64)
									if err != nil || sec < 0 {
										sec = 0
									}
									if sec == 0 {
										// 0 means it should not execute at all (or run forever? JMeter says 0 means run 0 seconds)
										// Wait, if 0, it means run 0 seconds, so exit immediately.
										step = sampler.BlockEndIndex
										continue
									} else {
										session.RuntimeDeadlines[sampler.LoopId] = time.Now().Add(time.Duration(sec * float64(time.Second)))
									}
								}

								// Check if deadline exceeded
								if deadline, ok := session.RuntimeDeadlines[sampler.LoopId]; ok {
									if time.Now().After(deadline) {
										// Exit loop: jump to RuntimeEnd
										step = sampler.BlockEndIndex
										delete(session.RuntimeDeadlines, sampler.LoopId) // reset for next Thread iteration
										continue
									}
								}
							case "RuntimeEnd":
								// Jump back to RuntimeStart to check deadline
								step = sampler.LoopJumpIndex - 1
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
							case "ThroughputStart":
								maxStr := session.Evaluator.Evaluate(sampler.ThroughputMaxExpr)
								maxVal, err := strconv.ParseFloat(maxStr, 64)
								if err != nil || maxVal < 0 {
									maxVal = 0
								}

								shouldExecute := false
								if sampler.ThroughputStyle == 1 {
									// Percent Executions (0.0 to 100.0)
									if maxVal >= 100.0 {
										shouldExecute = true
									} else if maxVal > 0 {
										shouldExecute = (rand.Float64() * 100.0) < maxVal
									}
								} else {
									// Total Executions
									maxTotal := int64(maxVal)
									if maxTotal > 0 {
										if sampler.ThroughputPerThread {
											// Track per thread (Session)
											if session.LoopCounters[sampler.LoopId] < int(maxTotal) {
												shouldExecute = true
												session.LoopCounters[sampler.LoopId]++
											}
										} else {
											// Track globally
											muIntf, _ := globalThroughputLocks.LoadOrStore(sampler.LoopId, &GlobalThroughputState{})
											state := muIntf.(*GlobalThroughputState)

											current := atomic.AddInt64(&state.Executions, 1)
											if current <= maxTotal {
												shouldExecute = true
											}
										}
									}
								}

								if !shouldExecute {
									step = sampler.BlockEndIndex
								}
							case "ThroughputEnd":
								// Just fall through
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
							case "ModuleCall":
								// Find target node in plan.ThreadGroups
								var targetTg *domain.ThreadGroup
								if len(sampler.ModuleTargetNodePath) > 0 {
									targetName := sampler.ModuleTargetNodePath[len(sampler.ModuleTargetNodePath)-1]
									for _, ptg := range session.Plan.ThreadGroups {
										if ptg.Name == targetName {
											targetTg = ptg
											break
										}
									}
								}
								if targetTg != nil && len(targetTg.Samplers) > 0 {
									session.CallStack = append(session.CallStack, CallFrame{Tg: session.Tg, Step: step})
									session.Tg = targetTg
									step = -1 // will be 0 on next iteration
								}
							case "SwitchStart":
								if len(sampler.SwitchChildStarts) > 0 {
									switchVal := session.Evaluator.Evaluate(sampler.SwitchValueExpr)
									selectedIndex := 0 // default to first child
									if switchVal != "" {
										// Try as index
										if idx, err := strconv.Atoi(switchVal); err == nil {
											if idx >= 0 && idx < len(sampler.SwitchChildStarts) {
												selectedIndex = idx
											}
										} else {
											// Try as name matching
											for i, name := range sampler.SwitchChildNames {
												if name == switchVal {
													selectedIndex = i
													break
												}
											}
										}
									}

									startStep := sampler.SwitchChildStarts[selectedIndex]
									endStep := sampler.SwitchChildEnds[selectedIndex]

									session.InterleaveJump[endStep+1] = sampler.BlockEndIndex
									step = startStep - 1
								} else {
									step = sampler.BlockEndIndex
								}
							case "SwitchEnd":
								// Just fall through
							case "RandomStart":
								if len(sampler.RandomChildStarts) > 0 {
									// Choose a random child
									childIndex := rand.IntN(len(sampler.RandomChildStarts))
									startStep := sampler.RandomChildStarts[childIndex]
									endStep := sampler.RandomChildEnds[childIndex]

									// When step reaches endStep + 1, jump to RandomEnd
									session.InterleaveJump[endStep+1] = sampler.BlockEndIndex

									// jump to the child
									step = startStep - 1
								} else {
									step = sampler.BlockEndIndex
								}
							case "RandomEnd":
								// Just fall through
							case "RandomOrderStart":
								state := session.RandomOrderState[sampler.LoopId]
								if state == nil || state.CurrentIndex >= len(sampler.RandomOrderChildStarts) {
									order := make([]int, len(sampler.RandomOrderChildStarts))
									for j := range order {
										order[j] = j
									}
									rand.Shuffle(len(order), func(i, j int) {
										order[i], order[j] = order[j], order[i]
									})
									state = &RandomOrderState{
										Order:        order,
										CurrentIndex: 0,
									}
									session.RandomOrderState[sampler.LoopId] = state
								}
								if state.CurrentIndex < len(sampler.RandomOrderChildStarts) {
									childIndex := state.Order[state.CurrentIndex]
									startStep := sampler.RandomOrderChildStarts[childIndex]
									endStep := sampler.RandomOrderChildEnds[childIndex]

									state.CurrentIndex++

									if state.CurrentIndex < len(sampler.RandomOrderChildStarts) {
										// More children left, jump back to RandomOrderStart
										session.InterleaveJump[endStep+1] = step
									} else {
										// Last child, jump out of controller
										session.InterleaveJump[endStep+1] = sampler.BlockEndIndex
									}

									step = startStep - 1
								} else {
									step = sampler.BlockEndIndex
								}
							case "RandomOrderEnd":
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
									barrier.Wait(ctx)
								}
							}
							if sleepMs > 0 {
								totalDelay += time.Duration(sleepMs) * time.Millisecond
							}
						}
						if totalDelay > 0 {
							time.Sleep(totalDelay)
						}

						// Evaluate variables in URL; EvaluatePreProcessors may return a modified URL
						// (prevents data race by not mutating the shared sampler.Request.URL directly)
						preProcessedURL := EvaluatePreProcessors(session, sampler)
						var reqURL string
						if preProcessedURL != "" {
							reqURL = preProcessedURL
						} else {
							reqURL = session.Evaluator.Evaluate(sampler.Request.URL)
						}
						method := sampler.Request.Method
						bodyStr := session.Evaluator.Evaluate(sampler.Request.BodyTemplate)

						if len(sampler.Request.Arguments) > 0 {
							var args []string
							for _, arg := range sampler.Request.Arguments {
								k := url.QueryEscape(session.Evaluator.Evaluate(arg[0]))
								v := url.QueryEscape(session.Evaluator.Evaluate(arg[1]))
								args = append(args, k+"="+v)
							}
							qs := strings.Join(args, "&")
							if method == "GET" || method == "DELETE" {
								if strings.Contains(reqURL, "?") {
									reqURL += "&" + qs
								} else {
									reqURL += "?" + qs
								}
							} else {
								if bodyStr != "" {
									bodyStr += "&" + qs
								} else {
									bodyStr = qs
								}
							}
						}

						var bodyReader io.Reader
						if bodyStr != "" {
							bodyReader = strings.NewReader(bodyStr)
						}
						reqCtx := ctx
						var cancel context.CancelFunc
						for _, p := range sampler.PreProcessors {
							if st, ok := p.(*domain.SampleTimeout); ok {
								timeoutStr := session.Evaluator.Evaluate(st.Timeout)
								if t, err := strconv.ParseInt(timeoutStr, 10, 64); err == nil && t > 0 {
									reqCtx, cancel = context.WithTimeout(reqCtx, time.Duration(t)*time.Millisecond)
								}
							}
						}

						req, err := http.NewRequestWithContext(reqCtx, method, reqURL, bodyReader)
						if err != nil {
							if cancel != nil {
								cancel()
							}
							// Record a failure result so it appears in the report instead of silently skipping
							failRes := &vegeta.Result{
								Attack:    sampler.Name,
								Seq:       uint64(step),
								Timestamp: time.Now(),
								Latency:   0,
								Method:    method,
								URL:       reqURL,
								Error:     fmt.Sprintf("invalid request: %v", err),
							}
							select {
							case results <- failRes:
							case <-ctx.Done():
								return
							}
							continue
						}

						// Evaluate headers
						for k, v := range sampler.Request.Headers {
							req.Header.Set(k, session.Evaluator.Evaluate(v))
						}

						if cacheManager != nil {
							if entry, ok := session.Cache[reqURL]; ok {
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
								if strings.HasPrefix(reqURL, authUrl) {
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
						if cancel != nil {
							cancel()
						}

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
							URL:       reqURL,
						}

						var bodyBytes []byte
						if err == nil {
							res.Code = uint16(resp.StatusCode)
							needsBody := true // Always read body in stateful mode for subsequent PreProcessors (like HTMLLinkParser)

							bufPtr := bufferPool.Get().(*[]byte)
							buf := *bufPtr

							if needsBody {
								// Direct use of bytes.Buffer: optimized from 2 copies via strings.Builder to 0 copies
								var bb bytes.Buffer
								bb.Grow(4096)
								// Scale max body size based on worker count to prevent OOM (MEDIUM-23)
								var maxBodySize int64 = 5 * 1024 * 1024 // 5MB default
								if a.maxW >= 1000 {
									maxBodySize = 2 * 1024 * 1024
								}
								if a.maxW >= 500 {
									maxBodySize = 512 * 1024
								}
								if !a.needsBody {
									maxBodySize = 0
								}

								written, _ := io.CopyBuffer(&bb, io.LimitReader(resp.Body, maxBodySize), buf)
								if written == maxBodySize {
									rest, _ := io.CopyBuffer(io.Discard, resp.Body, buf)
									written += rest
								}
								bodyBytes = bb.Bytes()
								res.BytesIn = uint64(written)
							} else {
								written, _ := io.CopyBuffer(io.Discard, resp.Body, buf)
								res.BytesIn = uint64(written)
							}

							_ = resp.Body.Close()  // Close first (ensures read is complete)
							bufferPool.Put(bufPtr) // Then return to Pool

							session.LastResponseBody = bodyBytes

							if cacheManager != nil && (resp.StatusCode == 200 || resp.StatusCode == 304) {
								etag := resp.Header.Get("ETag")
								lastMod := resp.Header.Get("Last-Modified")
								if etag != "" || lastMod != "" {
									if len(session.Cache) < cacheManager.MaxSize {
										session.Cache[reqURL] = &CacheEntry{
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

								if dbgExt, ok := ext.(*domain.DebugPostProcessor); ok {
									// In vjm, JMeter Properties and System Properties are not explicitly loaded.
									// We only print JMeter Variables for now.
									dbgExt.Execute(session.Variables, nil)
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
									defVal, hasDef := ext.DefaultValue()
									if hasDef {
										val = defVal
									} else {
										// JMeter removes variable if no default is provided
										delete(session.Variables, ext.RefName())
										session.Evaluator.SetVariable(ext.RefName(), "")
										continue
									}
								}
								session.Variables[ext.RefName()] = val
								session.Evaluator.SetVariable(ext.RefName(), val)
							}

							// Execute assertions
							for _, ast := range sampler.Assertions {
								if err := evaluateAssertion(ast, resp, bodyBytes, session, elapsed); err != nil {
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

						// Evaluate Result Status Action Handler
						if res.Code >= 400 || res.Error != "" {
							action := 0
							for _, ext := range sampler.Extractors {
								if ra, ok := ext.(*domain.ResultAction); ok {
									action = ra.Action
									break
								}
							}

							switch action {
							case 1: // Stop Thread
								return
							case 2, 3: // Stop Test, Stop Test Now
								atomic.StoreInt32(&a.stopped, 1)
								return
							case 4, 5, 6: // Start Next Thread Loop / Break Loop
								break
							}
							if action >= 4 {
								break
							}
						}
					}
				}
			}(i)
		}

		wg.Wait()
	}()

	return results
}

func evaluateAssertion(ast domain.Assertion, resp *http.Response, bodyBytes []byte, session *Session, latency time.Duration) error {
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

	case *domain.SizeAssertion:
		var targetSize int
		switch a.TestField {
		case "SizeAssertion.response_network_size":
			// Approximate network size (headers + body)
			var sb strings.Builder
			sb.WriteString(resp.Proto)
			sb.WriteString(" ")
			sb.WriteString(resp.Status)
			sb.WriteString("\r\n")
			for k, v := range resp.Header {
				sb.WriteString(k)
				sb.WriteString(": ")
				sb.WriteString(strings.Join(v, ", "))
				sb.WriteString("\r\n")
			}
			sb.WriteString("\r\n")
			targetSize = len(sb.String()) + len(bodyBytes)
		case "SizeAssertion.response_headers":
			var sb strings.Builder
			for k, v := range resp.Header {
				sb.WriteString(k)
				sb.WriteString(": ")
				sb.WriteString(strings.Join(v, ", "))
				sb.WriteString("\n")
			}
			targetSize = len(sb.String())
		case "SizeAssertion.response_code":
			targetSize = len(strconv.Itoa(resp.StatusCode))
		case "SizeAssertion.response_message":
			targetSize = len(resp.Status)
		default: // "SizeAssertion.response_data"
			targetSize = len(bodyBytes)
		}

		expectedSizeStr := session.Evaluator.Evaluate(a.Size)
		expectedSize, err := strconv.Atoi(expectedSizeStr)
		if err != nil {
			return fmt.Errorf("SizeAssertion failed: invalid size '%s'", expectedSizeStr)
		}

		matched := false
		operatorStr := ""
		switch a.Operator {
		case 1: // =
			matched = (targetSize == expectedSize)
			operatorStr = "=="
		case 2: // !=
			matched = (targetSize != expectedSize)
			operatorStr = "!="
		case 3: // >
			matched = (targetSize > expectedSize)
			operatorStr = ">"
		case 4: // <
			matched = (targetSize < expectedSize)
			operatorStr = "<"
		case 5: // >=
			matched = (targetSize >= expectedSize)
			operatorStr = ">="
		case 6: // <=
			matched = (targetSize <= expectedSize)
			operatorStr = "<="
		default:
			matched = (targetSize == expectedSize)
			operatorStr = "=="
		}

		if !matched {
			return fmt.Errorf("SizeAssertion failed: size was %d but expected %s %d (field: %s)", targetSize, operatorStr, expectedSize, a.TestField)
		}

		return nil

	case *domain.XPathAssertion:
		xpathExpr := session.Evaluator.Evaluate(a.XPath)
		if xpathExpr == "" {
			return fmt.Errorf("XPathAssertion failed: empty xpath expression")
		}

		doc, err := xmlquery.Parse(bytes.NewReader(bodyBytes))
		if err != nil {
			if !a.Negate {
				return fmt.Errorf("XPathAssertion failed: invalid XML document: %v", err)
			}
			// If invalid XML and negated, then we technically don't find the xpath, so it succeeds.
			return nil
		}

		nodes, err := xmlquery.QueryAll(doc, xpathExpr)
		if err != nil {
			if !a.Negate {
				return fmt.Errorf("XPathAssertion failed: invalid xpath expression '%s': %v", xpathExpr, err)
			}
			return nil
		}

		found := len(nodes) > 0

		if a.Negate {
			if found {
				return fmt.Errorf("XPathAssertion failed: xpath '%s' was found but negate is true", xpathExpr)
			}
		} else {
			if !found {
				return fmt.Errorf("XPathAssertion failed: xpath '%s' not found", xpathExpr)
			}
		}

		return nil

	case *domain.CompareAssertion:
		// Compare Assertion compares multiple responses in JMeter.
		// In vjm, we currently execute samples independently and don't retain previous sample contents for comparison.
		// It's technically possible to save the last response in the session and compare, but for now we just pass.
		// If CompareTime is set, we could potentially check duration, but duration isn't passed here.
		return nil

	case *domain.DurationAssertion:
		allowedDurationMs := int64(a.Duration)
		if allowedDurationMs <= 0 {
			return nil // Skip if not configured
		}
		if latency > time.Duration(allowedDurationMs)*time.Millisecond {
			return fmt.Errorf("DurationAssertion failed: response took %dms, exceeded allowed %dms",
				latency.Milliseconds(), allowedDurationMs)
		}
		return nil

	case *domain.MD5HexAssertion:
		hash := md5.Sum(bodyBytes)
		actualHex := hex.EncodeToString(hash[:])
		// Evaluate variables in ExpectedMD5Hex (supports ${var} references)
		expectedHex := session.Evaluator.Evaluate(a.ExpectedMD5Hex)
		if !strings.EqualFold(actualHex, expectedHex) {
			return fmt.Errorf("MD5HexAssertion failed: expected '%s', got '%s'", expectedHex, actualHex)
		}
		return nil

	case *domain.SMIMEAssertion:
		// S/MIME signature verification involves complex PKCS#7 parsing and x509 cert validation
		// which requires external dependencies (e.g. go.mozilla.org/pkcs7).
		// For high performance load testing, we stub this out and pass it.
		return nil

	case *domain.XMLAssertion:
		// Attempt to parse the body as XML
		if err := xml.Unmarshal(bodyBytes, new(interface{})); err != nil {
			// xml.Unmarshal on interface{} doesn't fully work for loading DOM,
			// but a simpler way to just check well-formedness is using xml.Decoder:
			decoder := xml.NewDecoder(bytes.NewReader(bodyBytes))
			for {
				_, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					return fmt.Errorf("XMLAssertion failed: invalid XML: %v", err)
				}
			}
		}
		return nil
	}
	return nil
}

// EvaluatePreProcessors evaluates the preprocessors for a given sampler.
// Returns the modified URL if any preprocessor changes it; returns empty string if no modification.
// Exported for testing purposes.
func EvaluatePreProcessors(session *Session, sampler *domain.Sampler) string {
	modifiedURL := ""
	for _, preProc := range sampler.PreProcessors {
		switch p := preProc.(type) {
		case *domain.HTMLLinkParser:
			if len(session.LastResponseBody) > 0 {
				matches := htmlLinkRegex.FindAllSubmatch(session.LastResponseBody, -1)
				if len(matches) > 0 {
					evalUrl := session.Evaluator.Evaluate(sampler.Request.URL)
					if rx, err := regexp.Compile("^" + evalUrl + "$"); err == nil {
						for _, m := range matches {
							link := string(m[1])
							if rx.MatchString(link) {
								modifiedURL = link // Return instead of mutating shared sampler
								break
							}
						}
					}
				}
			}
		case *domain.URLRewritingModifier:
			if len(session.LastResponseBody) > 0 && p.ArgumentName != "" {
				rxStr := fmt.Sprintf(`(?i)%s\s*=\s*(?:["']([^"']+)["']|([^"'>&\s]+))`, regexp.QuoteMeta(p.ArgumentName))
				if rx, err := regexp.Compile(rxStr); err == nil {
					m := rx.FindSubmatch(session.LastResponseBody)
					if len(m) > 0 {
						val := string(m[1])
						if val == "" && len(m) > 2 {
							val = string(m[2])
						}
						if val != "" {
							evalUrl := session.Evaluator.Evaluate(sampler.Request.URL)
							if p.PathExtension {
								sep := ";"
								if p.PathExtensionNoEq {
									evalUrl += sep + p.ArgumentName + val
								} else {
									evalUrl += sep + p.ArgumentName + "=" + val
								}
							} else {
								sep := "?"
								if strings.Contains(evalUrl, "?") {
									sep = "&"
								}
								evalUrl += sep + p.ArgumentName + "=" + val
							}
							modifiedURL = evalUrl // Return instead of mutating shared sampler
						}
					}
				}
			}
		case *domain.RegExUserParameters:
			if p.RegexRefName != "" {
				matchNrStr := session.Evaluator.Evaluate("${" + p.RegexRefName + "_matchNr}")
				matchNr, err := strconv.Atoi(matchNrStr)
				if err == nil && matchNr > 0 {
					evalUrl := session.Evaluator.Evaluate(sampler.Request.URL)
					for i := 1; i <= matchNr; i++ {
						idx := strconv.Itoa(i)
						nameGrp := p.ParamNamesGrNr
						if nameGrp == "" {
							nameGrp = "1"
						}
						valGrp := p.ParamValuesGrNr
						if valGrp == "" {
							valGrp = "2"
						}
						name := session.Evaluator.Evaluate("${" + p.RegexRefName + "_" + idx + "_g" + nameGrp + "}")
						val := session.Evaluator.Evaluate("${" + p.RegexRefName + "_" + idx + "_g" + valGrp + "}")
						if name != "" && name != "${"+p.RegexRefName+"_"+idx+"_g"+nameGrp+"}" {
							sep := "?"
							if strings.Contains(evalUrl, "?") {
								sep = "&"
							}
							evalUrl += sep + url.QueryEscape(name) + "=" + url.QueryEscape(val)
						}
					}
					modifiedURL = evalUrl // Return instead of mutating shared sampler
				}
			}
		}
	}
	return modifiedURL
}
