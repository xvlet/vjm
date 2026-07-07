package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
)

type CSVRuntime struct {
	Config *domain.CSVDataSet
	Lines  [][]string
	Next   *int64
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
	return &CSVRuntime{
		Config: cfg,
		Lines:  lines,
		Next:   &next,
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

// Session represents a virtual user executing a Thread Group sequentially
type Session struct {
	ID        uint64
	Variables map[string]string
	Evaluator evaluator.Evaluator
	Tg        *domain.ThreadGroup
	Step      int // Current sampler index
	Cache     map[string]*CacheEntry
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
		Cache:     make(map[string]*CacheEntry),
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
		MaxIdleConns:        10000,
		MaxIdleConnsPerHost: 10000,
		MaxConnsPerHost:     10000,
		IdleConnTimeout:     30 * time.Second,
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

				var localRandomVariables []*RandomVariableRuntime
				for _, rv := range plan.RandomVariables {
					if rv.PerThread {
						localRandomVariables = append(localRandomVariables, parseRandomVariable(rv))
					}
				}
				if len(plan.ThreadGroups) > 0 {
					for _, rv := range plan.ThreadGroups[0].RandomVariables {
						if rv.PerThread {
							localRandomVariables = append(localRandomVariables, parseRandomVariable(rv))
						}
					}
				}

				var localCSVs []*CSVRuntime
				for _, scsv := range sharedCSVs {
					if scsv.Config.ShareMode == "shareMode.thread" {
						var next int64 = 0
						localCSVs = append(localCSVs, &CSVRuntime{
							Config: scsv.Config,
							Lines:  scsv.Lines,
							Next:   &next,
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

					allRVs := append(sharedRandomVariables, localRandomVariables...)
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
						vars := strings.Split(csv.Config.VariableNames, ",")
						for i, vName := range vars {
							vName = strings.TrimSpace(vName)
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
